package engine

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// UserAgent mirrors the Chrome build the web client ships, so the origin sees
// a consistent application-layer fingerprint.
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"

const (
	geminiOrigin = "https://gemini.google.com"
	geminiAppURL = geminiOrigin + "/app"
	streamRPCURL = geminiOrigin + "/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate"
)

var (
	clientMu    sync.Mutex
	clientCache = map[string]*http.Client{}
)

// Client returns an HTTP client bound to proxyURL ("" means direct).
// Redirects are surfaced instead of followed: the web client treats a 30x as a
// signal, not as a step in a chain.
func Client(proxyURL string, timeout time.Duration) *http.Client {
	key := proxyURL + "|" + timeout.String()
	clientMu.Lock()
	defer clientMu.Unlock()
	if c, ok := clientCache[key]; ok {
		return c
	}
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
		// Pin a browser-shaped TLS offer; the standard library profile otherwise
		// drifts independently from the browser headers sent below.
		TLSClientConfig: &tls.Config{
			MinVersion:       tls.VersionTLS12,
			CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
		},
	}
	if proxyURL != "" {
		if u, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	} else {
		// Fall back to the environment proxy: a host that cannot reach Google
		// directly exports HTTPS_PROXY for the container, and a channel that
		// never set its own proxy should still honour it.
		transport.Proxy = http.ProxyFromEnvironment
	}
	c := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	clientCache[key] = c
	return c
}

// applyBrowserHeaders adds the headers the web client sends on every request.
func applyBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Sec-CH-UA", "\"Chromium\";v=\"146\", \"Google Chrome\";v=\"146\", \"Not?A_Brand\";v=\"24\"")
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", "\"Windows\"")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
}
