package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	codexcore "github.com/QuantumNous/new-api/pkg/codex"
)

const (
	defaultHTTPTimeout = 20 * time.Second

	// codexOAuthResponseLimit bounds how much of a token response body is read.
	codexOAuthResponseLimit = 8 << 10

	// codexOAuthBodyRunes bounds the upstream error text kept for diagnostics.
	codexOAuthBodyRunes = 512
)

// codexOAuthSessions holds in-flight authorization attempts. A session is
// addressed by a random id that never leaves the server.
var codexOAuthSessions = codexcore.NewSessionStore()

type CodexOAuthTokenResult struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresAt    time.Time
}

type codexOAuthTokenPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func RefreshCodexOAuthToken(ctx context.Context, refreshToken string) (*CodexOAuthTokenResult, error) {
	return RefreshCodexOAuthTokenWithProxy(ctx, refreshToken, "")
}

func RefreshCodexOAuthTokenWithProxy(ctx context.Context, refreshToken string, proxyURL string) (*CodexOAuthTokenResult, error) {
	client, err := getCodexOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return refreshCodexOAuthToken(ctx, client, codexcore.TokenURL, codexcore.ClientID, refreshToken)
}

func refreshCodexOAuthToken(
	ctx context.Context,
	client *http.Client,
	tokenURL string,
	clientID string,
	refreshToken string,
) (*CodexOAuthTokenResult, error) {
	rt := strings.TrimSpace(refreshToken)
	if rt == "" {
		return nil, errors.New("empty refresh_token")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", rt)
	form.Set("client_id", clientID)

	return requestCodexOAuthToken(ctx, client, tokenURL, form, "refresh")
}

// exchangeCodexOAuthCode trades an authorization code plus its PKCE verifier for
// a token set.
func exchangeCodexOAuthCode(
	ctx context.Context,
	client *http.Client,
	tokenURL string,
	clientID string,
	code string,
	codeVerifier string,
) (*CodexOAuthTokenResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", codeVerifier)
	form.Set("redirect_uri", codexcore.RedirectURI)

	return requestCodexOAuthToken(ctx, client, tokenURL, form, "exchange")
}

// requestCodexOAuthToken posts an OAuth token request and decodes the response.
// The upstream error body is kept in the error so a failed login can be
// diagnosed without another round trip.
func requestCodexOAuthToken(
	ctx context.Context,
	client *http.Client,
	tokenURL string,
	form url.Values,
	operation string,
) (*CodexOAuthTokenResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, codexOAuthResponseLimit))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("codex oauth %s failed: status=%d body=%s", operation, resp.StatusCode, truncateCodexOAuthBody(body))
	}

	var payload codexOAuthTokenPayload
	if err := common.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.AccessToken) == "" || strings.TrimSpace(payload.RefreshToken) == "" || payload.ExpiresIn <= 0 {
		return nil, fmt.Errorf("codex oauth %s response missing fields", operation)
	}

	return &CodexOAuthTokenResult{
		AccessToken:  strings.TrimSpace(payload.AccessToken),
		RefreshToken: strings.TrimSpace(payload.RefreshToken),
		IDToken:      strings.TrimSpace(payload.IDToken),
		ExpiresAt:    time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second),
	}, nil
}

func truncateCodexOAuthBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	runes := []rune(text)
	if len(runes) > codexOAuthBodyRunes {
		return string(runes[:codexOAuthBodyRunes]) + "..."
	}
	return text
}

// CodexAuthURLResult is what the admin console needs to start a login.
type CodexAuthURLResult struct {
	AuthURL     string `json:"auth_url"`
	SessionID   string `json:"session_id"`
	State       string `json:"state"`
	RedirectURI string `json:"redirect_uri"`
}

// GenerateCodexAuthURL starts an authorization attempt and returns the OpenAI
// consent URL the operator must open.
func GenerateCodexAuthURL(proxyURL string) (*CodexAuthURLResult, error) {
	// Expired sessions are collected here rather than by a background goroutine:
	// the store only grows when an operator starts a login.
	codexOAuthSessions.Cleanup()

	state, err := codexcore.GenerateState()
	if err != nil {
		return nil, err
	}
	verifier, err := codexcore.GenerateCodeVerifier()
	if err != nil {
		return nil, err
	}
	sessionID, err := codexcore.GenerateID()
	if err != nil {
		return nil, err
	}

	codexOAuthSessions.Set(sessionID, &codexcore.Session{
		State:        state,
		CodeVerifier: verifier,
		ProxyURL:     strings.TrimSpace(proxyURL),
		CreatedAt:    time.Now(),
	})

	return &CodexAuthURLResult{
		AuthURL:     codexcore.BuildAuthorizationURL(state, codexcore.GenerateCodeChallenge(verifier)),
		SessionID:   sessionID,
		State:       state,
		RedirectURI: codexcore.RedirectURI,
	}, nil
}

// normalizeCodexAuthorizationCode reduces whatever the operator pasted to the
// bare authorization code. OpenAI redirects to a localhost callback that is not
// running, so the address bar is normally copied in full (a URL carrying code,
// state and scope); only the code parameter may reach the token endpoint. A
// bare code is returned unchanged.
func normalizeCodexAuthorizationCode(raw string) string {
	code := strings.TrimSpace(raw)
	if index := strings.Index(code, "code="); index >= 0 {
		code = code[index+len("code="):]
		if end := strings.IndexAny(code, "&#"); end >= 0 {
			code = code[:end]
		}
		if decoded, err := url.QueryUnescape(code); err == nil {
			code = decoded
		}
	}
	return strings.TrimSpace(code)
}

// ExchangeCodexCode trades the pasted authorization code for credentials and
// assembles the channel key.
func ExchangeCodexCode(ctx context.Context, sessionID, state, code, proxyURL string) (*CodexOAuthKey, error) {
	return exchangeCodexCode(ctx, sessionID, state, code, proxyURL, codexcore.TokenURL, codexcore.ClientID)
}

func exchangeCodexCode(
	ctx context.Context,
	sessionID string,
	state string,
	code string,
	proxyURL string,
	tokenURL string,
	clientID string,
) (*CodexOAuthKey, error) {
	session, ok := codexOAuthSessions.Get(strings.TrimSpace(sessionID))
	if !ok {
		return nil, errors.New("authorization session is missing or expired, generate a new link")
	}
	if strings.TrimSpace(state) == "" || state != session.State {
		return nil, errors.New("state does not match the authorization session")
	}
	normalizedCode := normalizeCodexAuthorizationCode(code)
	if normalizedCode == "" {
		return nil, errors.New("authorization code is required")
	}

	effectiveProxy := strings.TrimSpace(proxyURL)
	if effectiveProxy == "" {
		effectiveProxy = session.ProxyURL
	}
	client, err := getCodexOAuthHTTPClient(effectiveProxy)
	if err != nil {
		return nil, err
	}

	token, err := exchangeCodexOAuthCode(ctx, client, tokenURL, clientID, normalizedCode, session.CodeVerifier)
	if err != nil {
		return nil, err
	}
	// The session survives a failed exchange so a mistyped code can be retried;
	// it is dropped only once OpenAI accepted the code.
	codexOAuthSessions.Delete(strings.TrimSpace(sessionID))

	accountID, ok := ExtractCodexAccountIDFromJWT(token.AccessToken)
	if !ok {
		accountID, ok = ExtractCodexAccountIDFromJWT(token.IDToken)
	}
	if !ok {
		return nil, errors.New("codex oauth: the account id is missing from the returned token")
	}

	email := ""
	if value, ok := ExtractEmailFromJWT(token.IDToken); ok {
		email = value
	} else if value, ok := ExtractEmailFromJWT(token.AccessToken); ok {
		email = value
	}

	return &CodexOAuthKey{
		Type:         "codex",
		IDToken:      token.IDToken,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		AccountID:    accountID,
		Email:        email,
		LastRefresh:  time.Now().Format(time.RFC3339),
		Expired:      token.ExpiresAt.Format(time.RFC3339),
	}, nil
}

func getCodexOAuthHTTPClient(proxyURL string) (*http.Client, error) {
	baseClient, err := GetHttpClientWithProxy(strings.TrimSpace(proxyURL))
	if err != nil {
		return nil, err
	}
	return oauthHTTPClient(baseClient, defaultHTTPTimeout), nil
}

func ExtractCodexAccountIDFromJWT(token string) (string, bool) {
	claims, ok := decodeJWTClaims(token)
	if !ok {
		return "", false
	}
	raw, ok := claims[codexcore.JWTClaimPath]
	if !ok {
		return "", false
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return "", false
	}
	v, ok := obj["chatgpt_account_id"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
}

func ExtractEmailFromJWT(token string) (string, bool) {
	claims, ok := decodeJWTClaims(token)
	if !ok {
		return "", false
	}
	v, ok := claims["email"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
}

func decodeJWTClaims(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if err := common.Unmarshal(payloadRaw, &claims); err != nil {
		return nil, false
	}
	return claims, true
}
