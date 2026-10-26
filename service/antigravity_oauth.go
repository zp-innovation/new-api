package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	agcore "github.com/QuantumNous/new-api/pkg/antigravity"
)

const (
	antigravityOAuthHTTPTimeout = 30 * time.Second
	antigravityCredentialType   = "antigravity"
)

// antigravityOAuthSessions holds in-flight authorization attempts. A session is
// addressed by a random id that never leaves the server.
var antigravityOAuthSessions = agcore.NewSessionStore()

// AntigravityAuthURLResult is what the admin console needs to start a login.
type AntigravityAuthURLResult struct {
	AuthURL     string `json:"auth_url"`
	SessionID   string `json:"session_id"`
	State       string `json:"state"`
	RedirectURI string `json:"redirect_uri"`
}

// GenerateAntigravityAuthURL starts an authorization attempt and returns the
// Google consent URL the operator must open.
func GenerateAntigravityAuthURL(proxyURL string) (*AntigravityAuthURLResult, error) {
	// Expired sessions are collected here rather than by a background
	// goroutine: the store only grows when an operator starts a login.
	antigravityOAuthSessions.Cleanup()

	state, err := agcore.GenerateState()
	if err != nil {
		return nil, err
	}
	verifier, err := agcore.GenerateCodeVerifier()
	if err != nil {
		return nil, err
	}
	sessionID, err := agcore.GenerateID()
	if err != nil {
		return nil, err
	}

	antigravityOAuthSessions.Set(sessionID, &agcore.Session{
		State:        state,
		CodeVerifier: verifier,
		ProxyURL:     strings.TrimSpace(proxyURL),
		CreatedAt:    time.Now(),
	})

	return &AntigravityAuthURLResult{
		AuthURL:     agcore.BuildAuthorizationURL(state, agcore.GenerateCodeChallenge(verifier)),
		SessionID:   sessionID,
		State:       state,
		RedirectURI: agcore.RedirectURI,
	}, nil
}

// normalizeAntigravityAuthorizationCode reduces whatever the operator pasted
// to the bare authorization code. Google redirects to a localhost callback that
// is not running, so the address bar is normally copied in full (a URL carrying
// code, state, scope and more); only the code parameter may reach the token
// endpoint. A bare code is returned unchanged.
func normalizeAntigravityAuthorizationCode(raw string) string {
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

// ExchangeAntigravityCode trades the pasted authorization code for credentials
// and resolves the account project id.
func ExchangeAntigravityCode(ctx context.Context, sessionID, state, code, proxyURL string) (*agcore.Credential, error) {
	session, ok := antigravityOAuthSessions.Get(strings.TrimSpace(sessionID))
	if !ok {
		return nil, errors.New("authorization session is missing or expired, generate a new link")
	}
	if strings.TrimSpace(state) == "" || state != session.State {
		return nil, errors.New("state does not match the authorization session")
	}
	normalizedCode := normalizeAntigravityAuthorizationCode(code)
	if normalizedCode == "" {
		return nil, errors.New("authorization code is required")
	}

	effectiveProxy := strings.TrimSpace(proxyURL)
	if effectiveProxy == "" {
		effectiveProxy = session.ProxyURL
	}
	client, err := newAntigravityOAuthClient(effectiveProxy)
	if err != nil {
		return nil, err
	}

	token, err := client.ExchangeCode(ctx, normalizedCode, session.CodeVerifier)
	if err != nil {
		return nil, err
	}
	// The session survives a failed exchange so a mistyped code can be retried;
	// it is dropped only once Google accepted the code.
	antigravityOAuthSessions.Delete(strings.TrimSpace(sessionID))

	credential := &agcore.Credential{
		Type:         antigravityCredentialType,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt().Format(time.RFC3339),
		LastRefresh:  time.Now().Format(time.RFC3339),
	}
	enrichAntigravityCredential(ctx, client, credential)
	return credential, nil
}

// RefreshAntigravityCredential exchanges a refresh token for a fresh access
// token. Google usually omits a new refresh token, so the caller's value is
// kept when the response has none. Account fields such as project_id are not
// part of the response; callers merge them from the stored credential.
func RefreshAntigravityCredential(ctx context.Context, refreshToken, proxyURL string) (*agcore.Credential, error) {
	client, err := newAntigravityOAuthClient(strings.TrimSpace(proxyURL))
	if err != nil {
		return nil, err
	}
	token, err := client.RefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}

	next := strings.TrimSpace(token.RefreshToken)
	if next == "" {
		next = strings.TrimSpace(refreshToken)
	}
	return &agcore.Credential{
		Type:         antigravityCredentialType,
		AccessToken:  token.AccessToken,
		RefreshToken: next,
		ExpiresAt:    token.ExpiresAt().Format(time.RFC3339),
		LastRefresh:  time.Now().Format(time.RFC3339),
	}, nil
}

// enrichAntigravityCredential fills in the account email, plan tier and project
// id. Failures are non-fatal: the operator still receives a usable access token
// and can see which fields are missing.
func enrichAntigravityCredential(ctx context.Context, client *agcore.Client, credential *agcore.Credential) {
	if info, err := client.GetUserInfo(ctx, credential.AccessToken); err != nil {
		logger.LogWarn(ctx, common.LogText("antigravity oauth: fetch user info failed: %v", err))
	} else {
		credential.Email = info.Email
	}

	assist, err := client.LoadCodeAssist(ctx, credential.AccessToken)
	if err != nil {
		logger.LogWarn(ctx, common.LogText("antigravity oauth: load code assist failed: %v", err))
		return
	}
	credential.PlanType = agcore.TierIDToPlanType(assist.TierID())
	credential.ProjectID = assist.ProjectID()
	if credential.ProjectID != "" {
		return
	}

	// Accounts that have never used Antigravity get their project provisioned
	// by onboardUser rather than loadCodeAssist.
	projectID, err := client.OnboardUser(ctx, credential.AccessToken, assist.PreferredTier())
	if err != nil {
		logger.LogWarn(ctx, common.LogText("antigravity oauth: onboard user failed: %v", err))
		return
	}
	credential.ProjectID = projectID
}

// newAntigravityOAuthClient builds a protocol client that honours the channel
// proxy.
func newAntigravityOAuthClient(proxyURL string) (*agcore.Client, error) {
	baseClient, err := GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return nil, err
	}
	if baseClient == nil {
		return agcore.NewClient(nil), nil
	}
	return agcore.NewClient(oauthHTTPClient(baseClient, antigravityOAuthHTTPTimeout)), nil
}
