package service

import (
	"net/http"
	"time"
)

// oauthHTTPClient returns a copy of base with the OAuth timeout applied. Shared
// proxy clients are cached and reused, so the timeout must be set on a copy
// rather than on the cached value. A nil base means the channel has no proxy
// client, which degrades to a plain client with the same timeout.
func oauthHTTPClient(base *http.Client, timeout time.Duration) *http.Client {
	client := http.Client{Timeout: timeout}
	if base != nil {
		client = *base
		client.Timeout = timeout
	}
	return &client
}
