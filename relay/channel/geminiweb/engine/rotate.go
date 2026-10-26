package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Gemini rotates two families of session cookies:
//
//   - __Secure-1PSIDTS / __Secure-3PSIDTS are short-lived (~30 min) tickets.
//     Without a refresh the origin treats the session as anonymous.
//   - SIDCC / __Secure-1PSIDCC / __Secure-3PSIDCC are refreshed by ordinary
//     traffic; every response carries new ones, which is why callers must merge
//     Set-Cookie back into the stored account.
//
// The sentinel payload below is the JSPB literal the rotate endpoint expects;
// the leading zeros are significant and json.Marshal would emit [0,"..."] which
// the endpoint rejects.
const rotateSentinelBody = "[000,\"-0000000000000000000\"]"

const (
	rotateURL     = "https://accounts.google.com/RotateCookies"
	rotatePageURL = "https://accounts.google.com/RotateCookiesPage" +
		"?og_pid=658&rot=3&origin=https%3A%2F%2Fgemini.google.com&exp_id=0"
	rotateProductID       = 658
	DefaultRotateInterval = 10 * time.Minute
)

// MinRotateInterval keeps the rotate endpoint from answering 429.
const MinRotateInterval = 60 * time.Second

var (
	rotateInitRe = regexp.MustCompile(`init\('([^']{4,64})'\s*,\s*([0-9.]+)\s*,[^)]*?([0-9.]+)\s*\)`)
	psidtsMu     sync.Mutex
	psidtsLastAt = map[int]time.Time{}
)

// cookieSubset keeps only the named cookies of a Cookie header value.
func cookieSubset(cookie string, names []string) string {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	var kept []string
	for _, pair := range strings.Split(cookie, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		name, _, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		if _, ok := wanted[strings.TrimSpace(name)]; ok {
			kept = append(kept, pair)
		}
	}
	return strings.Join(kept, "; ")
}

// MergeSetCookie folds Set-Cookie response headers into a stored cookie value,
// replacing same-named cookies and keeping the rest as they were. Deletion
// directives are ignored: the credential is the last valid value, not an empty
// tombstone that would make the next request anonymous.
func MergeSetCookie(cookie string, setCookies []string) string {
	if len(setCookies) == 0 {
		return cookie
	}
	order := make([]string, 0, 32)
	values := make(map[string]string, 32)
	for _, pair := range strings.Split(cookie, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		name, value, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if _, seen := values[name]; !seen {
			order = append(order, name)
		}
		values[name] = value
	}
	for _, header := range setCookies {
		if setCookieDeletes(header) {
			continue
		}
		pair := header
		if idx := strings.Index(pair, ";"); idx >= 0 {
			pair = pair[:idx]
		}
		name, value, found := strings.Cut(strings.TrimSpace(pair), "=")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, seen := values[name]; !seen {
			order = append(order, name)
		}
		values[name] = value
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, name+"="+values[name])
	}
	return strings.Join(parts, "; ")
}

// setCookieDeletes reports whether a Set-Cookie header removes a cookie.
// Deletion headers carry an empty value with Max-Age=0 or an expiry in the
// past; merging them as ordinary values turns a deliberate deletion into an
// empty credential.
func setCookieDeletes(header string) bool {
	pair, _, _ := strings.Cut(header, ";")
	_, value, _ := strings.Cut(pair, "=")
	if strings.TrimSpace(value) != "" {
		return false
	}
	parts := strings.Split(header, ";")
	for _, part := range parts[1:] {
		name, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "max-age":
			seconds, err := strconv.Atoi(strings.TrimSpace(value))
			if err == nil && seconds <= 0 {
				return true
			}
		case "expires":
			expires, err := http.ParseTime(strings.TrimSpace(value))
			if err == nil && !expires.After(time.Now()) {
				return true
			}
		}
	}
	return false
}

// cookieValue reads one cookie from a Cookie header value.
func cookieValue(cookie, name string) string {
	for _, pair := range strings.Split(cookie, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(pair), "=")
		if !found || strings.TrimSpace(key) != name {
			continue
		}
		return value
	}
	return ""
}

// RotatePSIDTS refreshes the short-lived session ticket and returns the merged
// cookie. It must be called well before the ticket expires, roughly every ten
// minutes, for the account to stay usable.
func RotatePSIDTS(ctx context.Context, cookie, proxyURL string, timeout time.Duration) (string, error) {
	if cookieValue(cookie, "__Secure-1PSID") == "" {
		return cookie, fmt.Errorf("cookie has no __Secure-1PSID")
	}
	if cookieValue(cookie, "__Secure-1PSIDTS") == "" {
		return cookie, fmt.Errorf("cookie has no __Secure-1PSIDTS")
	}
	headers := map[string]string{
		"Accept":         "*/*",
		"Content-Type":   "application/json",
		"Origin":         "https://accounts.google.com",
		"Cookie":         cookieSubset(cookie, []string{"__Secure-1PSID", "__Secure-1PSIDTS"}),
		"Cache-Control":  "no-cache",
		"Pragma":         "no-cache",
		"Sec-Fetch-Dest": "empty",
		"Sec-Fetch-Mode": "cors",
		"Sec-Fetch-Site": "same-origin",
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rotateURL, strings.NewReader(rotateSentinelBody))
	if err != nil {
		return cookie, err
	}
	applyBrowserHeaders(req)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := Client(proxyURL, timeout).Do(req)
	if err != nil {
		return cookie, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return cookie, fmt.Errorf("%w: RotateCookies returned HTTP %d: %s", ErrSessionExpired, resp.StatusCode, truncate(string(body), 120))
	}
	if resp.StatusCode != http.StatusOK {
		return cookie, fmt.Errorf("RotateCookies returned HTTP %d: %s", resp.StatusCode, truncate(string(body), 120))
	}
	merged := MergeSetCookie(cookie, resp.Header.Values("Set-Cookie"))
	before := cookieValue(cookie, "__Secure-1PSIDTS")
	after := cookieValue(merged, "__Secure-1PSIDTS")
	if after == "" || after == before {
		return cookie, fmt.Errorf("RotateCookies did not refresh __Secure-1PSIDTS")
	}
	return merged, nil
}

// EnsureSession validates the signed-in session and, if Google served the
// anonymous shell, refreshes __Secure-1PSIDTS once before probing again.
func EnsureSession(ctx context.Context, accountID int, cookie, proxyURL string, timeout time.Duration) (string, string, error) {
	xsrf, err := XSRFToken(ctx, cookie, proxyURL, timeout)
	if err == nil {
		return cookie, xsrf, nil
	}
	if !errors.Is(err, ErrSessionExpired) {
		return cookie, "", err
	}
	refreshed, rotateErr := rotateForAccount(ctx, accountID, cookie, proxyURL, timeout)
	if rotateErr == nil && refreshed != cookie {
		cookie = refreshed
		InvalidateXSRF(cookie)
	}
	xsrf, err = XSRFToken(ctx, cookie, proxyURL, timeout)
	return cookie, xsrf, err
}

// RotatePSIDTSForAccount refreshes the short-lived session ticket while
// throttling attempts per account. Both the request path and the background
// keep-alive must use this entry point so multiple callers cannot hammer
// RotateCookies within the same minute.
func RotatePSIDTSForAccount(ctx context.Context, accountID int, cookie, proxyURL string, timeout time.Duration) (string, error) {
	if !allowPSIDTSRotate(accountID) {
		return cookie, nil
	}
	refreshed, err := RotatePSIDTS(ctx, cookie, proxyURL, timeout)
	notePSIDTSAttempt(accountID, err)
	if err != nil {
		return cookie, err
	}
	return refreshed, nil
}

func rotateForAccount(ctx context.Context, accountID int, cookie, proxyURL string, timeout time.Duration) (string, error) {
	return RotatePSIDTSForAccount(ctx, accountID, cookie, proxyURL, timeout)
}

func allowPSIDTSRotate(accountID int) bool {
	if accountID <= 0 {
		return true
	}
	psidtsMu.Lock()
	defer psidtsMu.Unlock()
	last := psidtsLastAt[accountID]
	return last.IsZero() || time.Since(last) >= MinRotateInterval
}

func notePSIDTSAttempt(accountID int, err error) {
	if accountID <= 0 {
		return
	}
	if err != nil {
		message := err.Error()
		if !strings.Contains(message, "HTTP 401") &&
			!strings.Contains(message, "HTTP 403") &&
			!strings.Contains(message, "HTTP 429") {
			return
		}
	}
	psidtsMu.Lock()
	psidtsLastAt[accountID] = time.Now()
	psidtsMu.Unlock()
}

// RotateSIDCC walks the browser iframe flow. It refreshes the SIDCC family,
// which ordinary conversation traffic also rotates; idle accounts still need
// this probe. The returned duration is the server-provided next interval.
func RotateSIDCC(ctx context.Context, cookie, proxyURL string, timeout time.Duration) (string, time.Duration, error) {
	interval := DefaultRotateInterval
	cookie, interval, err := rotateSIDCC(ctx, cookie, proxyURL, timeout)
	return cookie, interval, err
}

func rotateSIDCC(ctx context.Context, cookie, proxyURL string, timeout time.Duration) (string, time.Duration, error) {
	sessionID, interval, pageCookies, err := fetchRotateParams(ctx, cookie, proxyURL, timeout)
	if err != nil {
		return cookie, 0, err
	}
	cookie = MergeSetCookie(cookie, pageCookies)
	body := fmt.Sprintf(`[%d,"%s"]`, rotateProductID, sessionID)
	headers := map[string]string{
		"Accept":         "*/*",
		"Content-Type":   "application/json",
		"Origin":         "https://accounts.google.com",
		"Referer":        rotatePageURL,
		"Cookie":         cookie,
		"Cache-Control":  "no-cache",
		"Pragma":         "no-cache",
		"Sec-Fetch-Dest": "empty",
		"Sec-Fetch-Mode": "same-origin",
		"Sec-Fetch-Site": "same-origin",
	}
	status, setCookies, responseBody, err := rotatePost(ctx, body, headers, proxyURL, timeout)
	if err != nil {
		return cookie, 0, err
	}
	if status != http.StatusOK {
		return cookie, 0, fmt.Errorf("RotateCookies SIDCC returned HTTP %d: %s", status, truncate(string(responseBody), 120))
	}
	return MergeSetCookie(cookie, setCookies), interval, nil
}

func fetchRotateParams(ctx context.Context, cookie, proxyURL string, timeout time.Duration) (string, time.Duration, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rotatePageURL, nil)
	if err != nil {
		return "", 0, nil, err
	}
	applyBrowserHeaders(req)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Referer", "https://gemini.google.com/")
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	resp, err := Client(proxyURL, timeout).Do(req)
	if err != nil {
		return "", 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", 0, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, nil, fmt.Errorf("RotateCookiesPage returned HTTP %d", resp.StatusCode)
	}
	m := rotateInitRe.FindSubmatch(body)
	if m == nil {
		return "", 0, nil, fmt.Errorf("RotateCookiesPage has no init(...) (cookie may be expired)")
	}
	interval := DefaultRotateInterval
	if seconds, parseErr := strconv.ParseFloat(string(m[3]), 64); parseErr == nil && seconds >= 60 && seconds <= 3600 {
		interval = time.Duration(seconds) * time.Second
	}
	return string(m[1]), interval, resp.Header.Values("Set-Cookie"), nil
}

func rotatePost(ctx context.Context, body string, headers map[string]string, proxyURL string, timeout time.Duration) (int, []string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rotateURL, strings.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	applyBrowserHeaders(req)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := Client(proxyURL, timeout).Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	return resp.StatusCode, resp.Header.Values("Set-Cookie"), payload, err
}
