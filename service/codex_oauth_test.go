package service

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	codexcore "github.com/QuantumNous/new-api/pkg/codex"
)

func TestNormalizeCodexAuthorizationCode(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "bare code is returned unchanged", raw: "ac_abc123", want: "ac_abc123"},
		{name: "blank stays blank", raw: "   ", want: ""},
		{
			name: "full callback url",
			raw:  "http://localhost:1455/auth/callback?code=ac_abc123&state=state-1&scope=openid+profile",
			want: "ac_abc123",
		},
		{name: "code only query", raw: "code=ac_abc123", want: "ac_abc123"},
		{name: "trailing fragment is dropped", raw: "http://localhost:1455/auth/callback?code=ac_abc123#tab", want: "ac_abc123"},
		{
			name: "percent escaped code is decoded",
			raw:  "http://localhost:1455/auth/callback?code=" + url.QueryEscape("ac_a/b+c"),
			want: "ac_a/b+c",
		},
		{
			name: "url without a code parameter is untouched",
			raw:  "http://localhost:1455/auth/callback?state=state-1",
			want: "http://localhost:1455/auth/callback?state=state-1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeCodexAuthorizationCode(tc.raw); got != tc.want {
				t.Fatalf("normalizeCodexAuthorizationCode(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestGenerateCodexAuthURLBuildsConsentURL(t *testing.T) {
	result, err := GenerateCodexAuthURL("")
	if err != nil {
		t.Fatalf("GenerateCodexAuthURL returned an error: %v", err)
	}
	if !strings.HasPrefix(result.AuthURL, codexcore.AuthorizeURL+"?") {
		t.Fatalf("unexpected auth url %q", result.AuthURL)
	}
	if result.SessionID == "" || result.State == "" {
		t.Fatalf("session id and state must be set: %+v", result)
	}
	if result.RedirectURI != codexcore.RedirectURI {
		t.Fatalf("redirect uri = %q, want %q", result.RedirectURI, codexcore.RedirectURI)
	}

	parsed, err := url.Parse(result.AuthURL)
	if err != nil {
		t.Fatalf("failed to parse the auth url: %v", err)
	}
	query := parsed.Query()
	expected := map[string]string{
		"response_type":              "code",
		"client_id":                  codexcore.ClientID,
		"redirect_uri":               codexcore.RedirectURI,
		"scope":                      codexcore.Scope,
		"code_challenge_method":      "S256",
		"state":                      result.State,
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"originator":                 codexcore.Originator(),
	}
	for key, want := range expected {
		if got := query.Get(key); got != want {
			t.Fatalf("auth url query %s = %q, want %q", key, got, want)
		}
	}
	if got := len(query.Get("code_challenge")); got != 43 {
		t.Fatalf("code_challenge length = %d, want 43", got)
	}

	// a mismatched state must be rejected before any network call happens
	if _, err := ExchangeCodexCode(context.Background(), result.SessionID, "wrong-state", "ac_abc123", ""); err == nil || !strings.Contains(err.Error(), "state does not match") {
		t.Fatalf("expected a state mismatch error, got %v", err)
	}
	codexOAuthSessions.Delete(result.SessionID)
}

func TestExchangeCodexCodeRejectsUnknownSession(t *testing.T) {
	_, err := ExchangeCodexCode(context.Background(), "no-such-session", "state-1", "ac_abc123", "")
	if err == nil || !strings.Contains(err.Error(), "authorization session is missing or expired") {
		t.Fatalf("expected a missing session error, got %v", err)
	}
}

func TestExchangeCodexCodeStoresTheCredentialReturnedByTheTokenEndpoint(t *testing.T) {
	accessToken := buildCodexTestJWT(t, map[string]any{
		"email":                "jack@example.com",
		codexcore.JWTClaimPath: map[string]any{"chatgpt_account_id": "acc-1"},
	})
	idToken := buildCodexTestJWT(t, map[string]any{"email": "jack@example.com"})

	var gotGrantType, gotVerifier, gotRedirect string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("failed to parse the token request: %v", err)
		}
		gotGrantType = r.PostForm.Get("grant_type")
		gotVerifier = r.PostForm.Get("code_verifier")
		gotRedirect = r.PostForm.Get("redirect_uri")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"access_token\":\"" + accessToken + "\",\"refresh_token\":\"refresh-1\",\"id_token\":\"" + idToken + "\",\"expires_in\":3600}"))
	}))
	defer server.Close()

	codexOAuthSessions.Set("session-ok", &codexcore.Session{State: "state-1", CodeVerifier: "verifier-1", CreatedAt: time.Now()})
	defer codexOAuthSessions.Delete("session-ok")

	key, err := exchangeCodexCode(context.Background(), "session-ok", "state-1", "  ac_abc123  ", "", server.URL, codexcore.ClientID)
	if err != nil {
		t.Fatalf("exchangeCodexCode returned an error: %v", err)
	}
	if gotGrantType != "authorization_code" {
		t.Fatalf("grant_type = %q, want authorization_code", gotGrantType)
	}
	if gotVerifier != "verifier-1" {
		t.Fatalf("code_verifier = %q, want verifier-1", gotVerifier)
	}
	if gotRedirect != codexcore.RedirectURI {
		t.Fatalf("redirect_uri = %q, want %q", gotRedirect, codexcore.RedirectURI)
	}
	if key.Type != "codex" {
		t.Fatalf("key type = %q, want codex", key.Type)
	}
	if key.AccountID != "acc-1" {
		t.Fatalf("account id = %q, want acc-1", key.AccountID)
	}
	if key.Email != "jack@example.com" {
		t.Fatalf("email = %q, want jack@example.com", key.Email)
	}
	if key.AccessToken != accessToken || key.RefreshToken != "refresh-1" || key.IDToken != idToken {
		t.Fatalf("unexpected tokens: %+v", key)
	}
	if key.LastRefresh == "" || key.Expired == "" {
		t.Fatalf("last refresh and expiry must be stamped: %+v", key)
	}
	if _, ok := codexOAuthSessions.Get("session-ok"); ok {
		t.Fatal("the session must be dropped after a successful exchange")
	}
}

func TestExchangeCodexCodeKeepsTheSessionWhenTheTokenEndpointFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("{\"error\":\"invalid_grant\"}"))
	}))
	defer server.Close()

	codexOAuthSessions.Set("session-fail", &codexcore.Session{State: "state-1", CodeVerifier: "verifier-1", CreatedAt: time.Now()})
	defer codexOAuthSessions.Delete("session-fail")

	if _, err := exchangeCodexCode(context.Background(), "session-fail", "state-1", "ac_abc123", "", server.URL, codexcore.ClientID); err == nil || !strings.Contains(err.Error(), "status=400") {
		t.Fatalf("expected an upstream status error, got %v", err)
	}
	if _, ok := codexOAuthSessions.Get("session-fail"); !ok {
		t.Fatal("the session must survive a failed exchange so the code can be retried")
	}
}

// buildCodexTestJWT builds an unsigned JWT whose payload carries the given claims.
func buildCodexTestJWT(t *testing.T, claims map[string]any) string {
	t.Helper()

	payload, err := common.Marshal(claims)
	if err != nil {
		t.Fatalf("failed to marshal the jwt claims: %v", err)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte("{\"alg\":\"none\",\"typ\":\"JWT\"}"))
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
