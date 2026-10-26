package antigravity

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGenerateCodeVerifierLength(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier: %v", err)
	}
	// RFC 7636 requires 43..128 characters; 32 random bytes give exactly 43.
	if len(verifier) != 43 {
		t.Fatalf("verifier length = %d, want 43", len(verifier))
	}
	if strings.ContainsAny(verifier, "=+/") {
		t.Fatalf("verifier %q is not base64url without padding", verifier)
	}

	other, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier: %v", err)
	}
	if verifier == other {
		t.Fatal("two verifiers are identical, randomness looks broken")
	}
}

func TestGenerateCodeChallengeIsS256(t *testing.T) {
	verifier := "test-verifier-value"
	sum := sha256.Sum256([]byte(verifier))
	want := strings.TrimRight(base64.URLEncoding.EncodeToString(sum[:]), "=")
	if got := GenerateCodeChallenge(verifier); got != want {
		t.Fatalf("GenerateCodeChallenge = %q, want %q", got, want)
	}
}

func TestBuildAuthorizationURL(t *testing.T) {
	raw := BuildAuthorizationURL("state-value", "challenge-value")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorization url: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "accounts.google.com" {
		t.Fatalf("unexpected authorization host: %s", parsed.Host)
	}

	query := parsed.Query()
	expected := map[string]string{
		"client_id":              ClientID,
		"redirect_uri":           RedirectURI,
		"response_type":          "code",
		"scope":                  Scope,
		"state":                  "state-value",
		"code_challenge":         "challenge-value",
		"code_challenge_method":  "S256",
		"access_type":            "offline",
		"prompt":                 "consent",
		"include_granted_scopes": "true",
	}
	for key, want := range expected {
		if got := query.Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}
}

func TestClientSecretDefaultsToPublicValue(t *testing.T) {
	if ClientSecret() == "" {
		t.Fatal("ClientSecret() is empty; the default must keep the channel usable")
	}
}

// A mistyped code must stay retryable, so Get must not consume the session;
// Delete is what enforces single use, and callers invoke it after a successful
// exchange.
func TestSessionStoreGetIsRetryableAndDeleteIsSingleUse(t *testing.T) {
	store := NewSessionStore()
	store.Set("session-1", &Session{State: "s", CodeVerifier: "v"})

	session, ok := store.Get("session-1")
	if !ok || session.State != "s" {
		t.Fatalf("Get = %v, %v; want the stored session", session, ok)
	}
	if _, ok := store.Get("session-1"); !ok {
		t.Fatal("second Get failed; a mistyped code must be retryable")
	}

	store.Delete("session-1")
	if _, ok := store.Get("session-1"); ok {
		t.Fatal("Get succeeded after Delete; sessions must be single use")
	}
}

func TestSessionStoreExpiresSessions(t *testing.T) {
	store := NewSessionStoreWithTTL(time.Millisecond)
	store.Set("session-1", &Session{State: "s", CreatedAt: time.Now().Add(-time.Hour)})

	if _, ok := store.Get("session-1"); ok {
		t.Fatal("expired session was accepted")
	}
}

func TestSessionStoreCleanup(t *testing.T) {
	store := NewSessionStoreWithTTL(time.Millisecond)
	store.Set("expired", &Session{CreatedAt: time.Now().Add(-time.Hour)})
	store.Set("live", &Session{CreatedAt: time.Now()})

	if removed := store.Cleanup(); removed != 1 {
		t.Fatalf("Cleanup removed %d sessions, want 1", removed)
	}
	if store.Len() != 1 {
		t.Fatalf("Len = %d, want 1", store.Len())
	}
}

func TestURLAvailabilityPrefersLastSuccess(t *testing.T) {
	availability := NewURLAvailability(time.Minute)
	bases := []string{"https://prod", "https://daily"}

	if got := availability.Available(bases); got[0] != "https://prod" {
		t.Fatalf("initial order = %v, want prod first", got)
	}

	availability.MarkSuccess("https://daily")
	if got := availability.Available(bases); got[0] != "https://daily" {
		t.Fatalf("after success order = %v, want daily first", got)
	}
}

func TestURLAvailabilityDemotesAndFallsBack(t *testing.T) {
	availability := NewURLAvailability(time.Minute)
	bases := []string{"https://prod", "https://daily"}

	availability.MarkUnavailable("https://prod")
	if got := availability.Available(bases); len(got) != 1 || got[0] != "https://daily" {
		t.Fatalf("Available = %v, want only daily", got)
	}

	// A fully demoted set must still return the configured order so a stale
	// demotion cannot take the channel offline.
	availability.MarkUnavailable("https://daily")
	if got := availability.Available(bases); len(got) != 2 {
		t.Fatalf("Available = %v, want the configured order restored", got)
	}
}

func TestURLAvailabilityRecoversAfterTTL(t *testing.T) {
	availability := NewURLAvailability(time.Millisecond)
	bases := []string{"https://prod", "https://daily"}

	availability.MarkUnavailable("https://prod")
	time.Sleep(5 * time.Millisecond)

	if got := availability.Available(bases); len(got) != 2 || got[0] != "https://prod" {
		t.Fatalf("Available = %v, want prod recovered", got)
	}
}
