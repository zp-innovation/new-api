package service

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	agcore "github.com/QuantumNous/new-api/pkg/antigravity"
)

func TestGenerateAntigravityAuthURLStoresSession(t *testing.T) {
	result, err := GenerateAntigravityAuthURL("")
	if err != nil {
		t.Fatalf("GenerateAntigravityAuthURL: %v", err)
	}

	parsed, err := url.Parse(result.AuthURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	query := parsed.Query()
	if got := query.Get("client_id"); got != agcore.ClientID {
		t.Errorf("client_id = %q, want %q", got, agcore.ClientID)
	}
	if got := query.Get("redirect_uri"); got != agcore.RedirectURI {
		t.Errorf("redirect_uri = %q, want %q", got, agcore.RedirectURI)
	}
	if got := query.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	if got := query.Get("access_type"); got != "offline" {
		t.Errorf("access_type = %q, want offline", got)
	}
	if query.Get("code_challenge") == "" {
		t.Error("code_challenge is empty")
	}
	if result.RedirectURI != agcore.RedirectURI {
		t.Errorf("reported redirect uri = %q", result.RedirectURI)
	}

	// The state echoed to the browser must be the one bound to the session.
	session, ok := antigravityOAuthSessions.Get(result.SessionID)
	if !ok {
		t.Fatal("authorization session was not stored")
	}
	if session.State != result.State {
		t.Fatalf("session state = %q, want %q", session.State, result.State)
	}
	if session.CodeVerifier == "" {
		t.Fatal("session has no code verifier")
	}
}

func TestGenerateAntigravityAuthURLKeepsProxy(t *testing.T) {
	result, err := GenerateAntigravityAuthURL("http://127.0.0.1:8888")
	if err != nil {
		t.Fatalf("GenerateAntigravityAuthURL: %v", err)
	}

	session, ok := antigravityOAuthSessions.Get(result.SessionID)
	if !ok {
		t.Fatal("authorization session was not stored")
	}
	if session.ProxyURL != "http://127.0.0.1:8888" {
		t.Fatalf("session proxy = %q", session.ProxyURL)
	}
}

func TestExchangeAntigravityCodeRejectsUnknownSession(t *testing.T) {
	_, err := ExchangeAntigravityCode(context.Background(), "does-not-exist", "state", "code", "")
	if err == nil {
		t.Fatal("ExchangeAntigravityCode accepted an unknown session")
	}
}

func TestExchangeAntigravityCodeRejectsStateMismatch(t *testing.T) {
	antigravityOAuthSessions.Set("state-mismatch", &agcore.Session{
		State:        "expected-state",
		CodeVerifier: "verifier",
		CreatedAt:    time.Now(),
	})

	_, err := ExchangeAntigravityCode(context.Background(), "state-mismatch", "other-state", "code", "")
	if err == nil {
		t.Fatal("ExchangeAntigravityCode accepted a mismatched state")
	}
}

func TestExchangeAntigravityCodeRejectsEmptyCode(t *testing.T) {
	antigravityOAuthSessions.Set("empty-code", &agcore.Session{
		State:        "state",
		CodeVerifier: "verifier",
		CreatedAt:    time.Now(),
	})

	_, err := ExchangeAntigravityCode(context.Background(), "empty-code", "state", "   ", "")
	if err == nil {
		t.Fatal("ExchangeAntigravityCode accepted an empty code")
	}
}

func TestRefreshAntigravityCredentialRejectsEmptyToken(t *testing.T) {
	_, err := RefreshAntigravityCredential(context.Background(), "   ", "")
	if err == nil {
		t.Fatal("RefreshAntigravityCredential accepted an empty refresh token")
	}
	if !strings.Contains(err.Error(), "refresh token") {
		t.Fatalf("error = %q, want it to mention the refresh token", err)
	}
}
func TestAntigravityCredentialNeedsRefresh(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	window := 15 * time.Minute

	cases := []struct {
		name       string
		credential *agcore.Credential
		want       bool
	}{
		{name: "nil credential", credential: nil, want: false},
		{name: "missing expiry", credential: &agcore.Credential{}, want: true},
		{name: "malformed expiry", credential: &agcore.Credential{ExpiresAt: "not-a-time"}, want: true},
		{
			name:       "expires well after the window",
			credential: &agcore.Credential{ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)},
			want:       false,
		},
		{
			name:       "expires inside the window",
			credential: &agcore.Credential{ExpiresAt: now.Add(5 * time.Minute).Format(time.RFC3339)},
			want:       true,
		},
		{
			name:       "already expired",
			credential: &agcore.Credential{ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339)},
			want:       true,
		},
	}

	for _, tc := range cases {
		if got := AntigravityCredentialNeedsRefresh(tc.credential, now, window); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
func TestNormalizeAntigravityAuthorizationCode(t *testing.T) {
	const code = "4/0AXlqoi7cOA6JGECmfnefcaaDTPbu169KiOhVyrVEnZTOikz9Ox_27kbc9CrUl5EQEktTw"

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "bare code", raw: code, want: code},
		{name: "blank", raw: "   ", want: ""},
		{
			name: "full callback url",
			raw:  "http://localhost:8085/callback?state=lYJ1qelCiE8-0mvtgT21mQC4-tFvJmaVEvM9MpcagLo&iss=https://accounts.google.com&code=" + code + "&scope=email%20profile&authuser=1&prompt=consent",
			want: code,
		},
		{name: "query string only", raw: "code=" + code, want: code},
		{name: "percent escaped code", raw: "http://localhost:8085/callback?code=" + url.QueryEscape(code), want: code},
		{name: "trailing fragment", raw: "http://localhost:8085/callback?code=" + code + "#tab", want: code},
		{
			name: "no code parameter",
			raw:  "http://localhost:8085/callback?error=access_denied",
			want: "http://localhost:8085/callback?error=access_denied",
		},
	}

	for _, tc := range cases {
		if got := normalizeAntigravityAuthorizationCode(tc.raw); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
