package engine

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// A signed-in batchexecute call must carry the at form field (the XSRF token)
// or the origin answers 400. The token lives in the /app HTML as
// "SNlM0e":"<token>:<millisecond stamp>" and is bound to the cookie session.
const xsrfTTL = 20 * time.Minute

// The build label (bl) is a front-end version string that rides on every
// StreamGenerate URL. It is embedded in the same page as "cfb2h".
const blTTL = 6 * time.Hour

// FallbackBL is used until the first successful page fetch.
const FallbackBL = "boq_gemini-web-uiserver_20261007.01_p0"

// ErrSessionExpired marks a cookie that Google served anonymously.
// Callers can refresh the account's __Secure-1PSIDTS ticket once and retry.
var ErrSessionExpired = errors.New("no SNlM0e in page (cookie expired or not signed in)")

var (
	snlm0eRe  = regexp.MustCompile("\"SNlM0e\":\"([^\"]{10,200})\"")
	cfb2hRe   = regexp.MustCompile("\"cfb2h\":\"([^\"]{10,120})\"")
	blShapeRe = regexp.MustCompile("^boq_[a-z0-9-]+_[0-9]{8}\\.[0-9]{2}_p[0-9]+$")
	sapisidRe = regexp.MustCompile("(?:^|;\\s*)SAPISID=([^;]+)")
)

type pageTokens struct {
	xsrf    string
	fetched time.Time
}

var (
	tokenMu    sync.Mutex
	tokenCache = map[string]pageTokens{}

	blMu     sync.Mutex
	blValue  string
	blAt     time.Time
	blFlight bool
)

func cookieKey(cookie string) string {
	sum := sha1.Sum([]byte(cookie))
	return hex.EncodeToString(sum[:8])
}

// ExtractSAPISID pulls SAPISID out of a Cookie header value.
func ExtractSAPISID(cookie string) string {
	if m := sapisidRe.FindStringSubmatch(cookie); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// SAPISIDHash builds the Authorization value the origin expects for
// authenticated RPC calls.
func SAPISIDHash(sapisid string) string {
	ts := time.Now().Unix()
	sum := sha1.Sum([]byte(fmt.Sprintf("%d %s %s", ts, sapisid, geminiOrigin)))
	return fmt.Sprintf("SAPISIDHASH %d_%s", ts, hex.EncodeToString(sum[:]))
}

// fetchAppPage loads the web app shell. An empty cookie fetches it anonymously.
func fetchAppPage(ctx context.Context, cookie, proxyURL string, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geminiAppURL, nil)
	if err != nil {
		return nil, err
	}
	applyBrowserHeaders(req)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := Client(proxyURL, timeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch /app: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// InvalidateXSRF drops the cached token so the next call refetches the page.
// Remove both the legacy full-cookie key and the identity-scoped key.
func InvalidateXSRF(cookie string) {
	if cookie == "" {
		return
	}
	tokenMu.Lock()
	delete(tokenCache, cookieKey(cookie))
	delete(tokenCache, cookieCacheKey(cookie))
	tokenMu.Unlock()
}

// cookieCacheKey keeps the XSRF cache stable across SIDCC / *PSIDTS rotation.
// The token belongs to the signed-in identity; keying on the whole cookie made
// every response that refreshed a ticket discard a still-valid token.
func cookieCacheKey(cookie string) string {
	identity := cookieSubset(cookie, []string{"SAPISID", "__Secure-1PSID"})
	return "identity:" + cookieKey(identity)
}

// XSRFToken returns the at token for this cookie, fetching and caching it as
// needed. Anonymous callers get an empty token, which is what they must send.
func XSRFToken(ctx context.Context, cookie, proxyURL string, timeout time.Duration) (string, error) {
	if cookie == "" {
		return "", nil
	}
	key := cookieCacheKey(cookie)
	tokenMu.Lock()
	if e, ok := tokenCache[key]; ok && time.Since(e.fetched) < xsrfTTL {
		tokenMu.Unlock()
		return e.xsrf, nil
	}
	tokenMu.Unlock()

	body, err := fetchAppPage(ctx, cookie, proxyURL, timeout)
	if err != nil {
		return "", err
	}
	m := snlm0eRe.FindSubmatch(body)
	if m == nil {
		// A page without SNlM0e while a cookie was sent means the session is
		// gone and the origin served the anonymous shell.
		return "", ErrSessionExpired
	}
	token := string(m[1])
	tokenMu.Lock()
	tokenCache[key] = pageTokens{xsrf: token, fetched: time.Now()}
	tokenMu.Unlock()

	blMu.Lock()
	if fresh := cfb2hRe.FindSubmatch(body); fresh != nil {
		if candidate := string(fresh[1]); blShapeRe.MatchString(candidate) {
			blValue = candidate
			blAt = time.Now()
		}
	}
	blMu.Unlock()

	return token, nil
}

// BuildLabel returns the bl to use, refreshing it in the background when stale.
func BuildLabel(proxyURL string, timeout time.Duration) string {
	blMu.Lock()
	value, fetchedAt, flight := blValue, blAt, blFlight
	if !flight && time.Since(fetchedAt) > blTTL {
		blFlight = true
		blMu.Unlock()
		go refreshBL(proxyURL, timeout)
	} else {
		blMu.Unlock()
	}
	if value != "" {
		return value
	}
	return FallbackBL
}

func refreshBL(proxyURL string, timeout time.Duration) {
	defer func() {
		blMu.Lock()
		blFlight = false
		blAt = time.Now()
		blMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	body, err := fetchAppPage(ctx, "", proxyURL, timeout)
	if err != nil {
		return
	}
	m := cfb2hRe.FindSubmatch(body)
	if m == nil {
		return
	}
	candidate := string(m[1])
	if !blShapeRe.MatchString(candidate) {
		return
	}
	blMu.Lock()
	blValue = candidate
	blMu.Unlock()
}
