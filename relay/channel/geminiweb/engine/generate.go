package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Credential is one Gemini Web account plus the egress used for it.
type Credential struct {
	// AccountID scopes the rotate throttle to one account.
	AccountID int
	// Cookie is the full Cookie header value captured from a signed-in browser.
	Cookie string
	// Proxy is an optional http(s)/socks5 proxy URL; empty means direct.
	Proxy string
	// Timeout bounds every upstream call; zero means 180s.
	Timeout time.Duration
}

func (c Credential) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 180 * time.Second
}

// Result is the outcome of one StreamGenerate call.
type Result struct {
	Text          string
	Reasoning     string
	UpstreamModel string
	Raw           string
	SetCookies    []string
	TTFB          time.Duration
	Total         time.Duration
	// Cookie keeps any refreshed session ticket so the account pool can persist it.
	Cookie string
}

const (
	maxAttempts    = 3
	retryDelay     = 2 * time.Second
	maxXSRFRefetch = 1
	statusOK       = 200
)

// Generate runs one chat turn against Gemini Web.
//
// onDelta and onReasoning may be nil for a non-streaming call. When set, each
// incremental slice is delivered as soon as the upstream frame carrying it
// arrives; the returned Result still holds the complete text.
func Generate(ctx context.Context, prompt, modelName string, cred Credential, onDelta, onReasoning func(string)) (*Result, error) {
	_, mc, ok := ResolveModel(modelName)
	if !ok {
		return nil, fmt.Errorf("unknown Gemini Web model: %s", modelName)
	}
	if mc.NeedsLogin() && strings.TrimSpace(cred.Cookie) == "" {
		return nil, fmt.Errorf("%s needs a signed-in Google account cookie", modelName)
	}

	timeout := cred.timeout()
	if cred.Cookie != "" {
		// Validate and refresh the signed-in ticket once per request. The account
		// pool then either uses this refreshed cookie or skips to another account.
		refreshed, _, sessionErr := EnsureSession(ctx, cred.AccountID, cred.Cookie, cred.Proxy, timeout)
		cred.Cookie = refreshed
		if sessionErr != nil {
			return nil, sessionErr
		}
	}
	outerJSON, requestUUID, err := buildInner(prompt, mc)
	if err != nil {
		return nil, err
	}
	modelHeader := buildModelHeader(mc.HexID, mc.Mode, mc.thinkWire(), newUUID())
	sessionHeader := fmt.Sprintf("[\"%s\",1]", requestUUID)

	xsrf, err := XSRFToken(ctx, cred.Cookie, cred.Proxy, timeout)
	if err != nil {
		return nil, err
	}

	sapisid := ExtractSAPISID(cred.Cookie)
	body := buildBody(outerJSON, xsrf)
	endpoint := buildEndpoint(BuildLabel(cred.Proxy, timeout))
	headers := buildHeaders(cred.Cookie, sapisid, mc.HexID, modelHeader, sessionHeader)

	textTracker := &deltaTracker{}
	reasonTracker := &deltaTracker{}
	var onLine func(string)
	if onDelta != nil || onReasoning != nil {
		onLine = func(line string) {
			// The thinking chain finishes before the reply starts, so deliver it first.
			if onReasoning != nil {
				if full := reasoningInLine(line); full != "" {
					if delta := reasonTracker.push(full); delta != "" {
						onReasoning(delta)
					}
				}
			}
			if onDelta == nil {
				return
			}
			for _, full := range textsInLine(line) {
				if delta := textTracker.push(full); delta != "" {
					onDelta(delta)
				}
			}
		}
	}

	started := time.Now()
	var lastErr error
	xsrfRefetched := 0
	for attempt := 0; attempt < maxAttempts; attempt++ {
		begin := time.Now()
		status, raw, setCookies, reqErr := doRequest(ctx, endpoint, body, headers, cred.Proxy, timeout, onLine)
		ttfb := time.Since(begin)

		if reqErr != nil {
			lastErr = reqErr
			// Never retry once content was delivered: the client already has it.
			if textTracker.emitted != "" || reasonTracker.emitted != "" {
				break
			}
			if attempt < maxAttempts-1 {
				time.Sleep(retryDelay)
			}
			continue
		}

		if status == 400 && strings.Contains(string(raw), "\"xsrf\"") &&
			cred.Cookie != "" && xsrfRefetched < maxXSRFRefetch {
			// The cached token expired. This self-heal does not consume a retry.
			xsrfRefetched++
			InvalidateXSRF(cred.Cookie)
			if fresh, fetchErr := XSRFToken(ctx, cred.Cookie, cred.Proxy, timeout); fetchErr == nil {
				body = buildBody(outerJSON, fresh)
				headers = buildHeaders(cred.Cookie, sapisid, mc.HexID, modelHeader, sessionHeader)
				attempt--
				continue
			}
		}

		if status != statusOK {
			lastErr = fmt.Errorf("upstream HTTP %d: %s", status, truncate(string(raw), 200))
			if textTracker.emitted != "" || reasonTracker.emitted != "" {
				break
			}
			if attempt < maxAttempts-1 {
				time.Sleep(retryDelay)
			}
			continue
		}

		// A 200 without any content frame is a transient upstream refusal.
		if !hasContentFrame(string(raw)) {
			lastErr = fmt.Errorf("upstream returned no content frame (%d bytes)", len(raw))
			if textTracker.emitted != "" || reasonTracker.emitted != "" {
				break
			}
			if attempt < maxAttempts-1 {
				time.Sleep(retryDelay)
			}
			continue
		}

		result := &Result{
			Text:          ExtractResponseText(string(raw)),
			Reasoning:     ExtractReasoning(string(raw)),
			UpstreamModel: ExtractUpstreamModel(string(raw)),
			Raw:           string(raw),
			SetCookies:    setCookies,
			TTFB:          ttfb,
			Total:         time.Since(started),
			Cookie:        cred.Cookie,
		}
		if result.Text == "" {
			result.Text = textTracker.emitted
		}
		if result.Reasoning == "" {
			result.Reasoning = reasonTracker.emitted
		}
		return result, nil
	}

	if lastErr == nil {
		lastErr = errors.New("Gemini Web request failed")
	}
	return nil, lastErr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
