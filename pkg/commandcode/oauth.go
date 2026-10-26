package commandcode

import (
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"strings"
)

// GenerateState returns a fresh base64url state token for one login attempt.
func GenerateState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// BuildAuthURL builds the Studio handoff URL the official CLI opens. The
// callback is where Studio sends the freshly minted API key.
func BuildAuthURL(callbackURL, state, client string) string {
	params := url.Values{}
	params.Set("callback", strings.TrimSpace(callbackURL))
	params.Set("state", strings.TrimSpace(state))
	params.Set("mode", OAuthModeRedirect)
	if client = strings.TrimSpace(client); client != "" {
		params.Set("client", client)
	}
	return OAuthAuthorizeURL + "?" + params.Encode()
}
