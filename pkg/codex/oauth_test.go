package codex

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBuildAuthorizationURLIncludesPKCEAndCodexParameters(t *testing.T) {
	raw := BuildAuthorizationURL("state-1", "challenge-1")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorization url: %v", err)
	}
	if got := parsed.Scheme + "://" + parsed.Host + parsed.Path; got != AuthorizeURL {
		t.Fatalf("authorize url = %q, want %q", got, AuthorizeURL)
	}

	query := parsed.Query()
	want := map[string]string{
		"response_type":              "code",
		"client_id":                  ClientID,
		"redirect_uri":               RedirectURI,
		"scope":                      Scope,
		"code_challenge":             "challenge-1",
		"code_challenge_method":      "S256",
		"state":                      "state-1",
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"originator":                 DefaultOriginator,
	}
	for key, expected := range want {
		if got := query.Get(key); got != expected {
			t.Errorf("query %s = %q, want %q", key, got, expected)
		}
	}
}

func TestGenerateCodeVerifierIsRFC7636Compliant(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("generate verifier: %v", err)
	}
	if len(verifier) != 43 {
		t.Fatalf("verifier length = %d, want 43", len(verifier))
	}
	if strings.ContainsAny(verifier, "+/=") {
		t.Fatalf("verifier %q is not base64url encoded", verifier)
	}

	other, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("generate second verifier: %v", err)
	}
	if verifier == other {
		t.Fatal("two verifiers must differ")
	}

	challenge := GenerateCodeChallenge(verifier)
	if len(challenge) != 43 || strings.ContainsAny(challenge, "+/=") {
		t.Fatalf("challenge %q is not a base64url sha256 digest", challenge)
	}
	if challenge != GenerateCodeChallenge(verifier) {
		t.Fatal("the challenge must be derived deterministically")
	}
}

func TestGenerateStateAndIDAreHex(t *testing.T) {
	state, err := GenerateState()
	if err != nil {
		t.Fatalf("generate state: %v", err)
	}
	if len(state) != 32 || strings.ContainsAny(state, "ghijklmnopqrstuvwxyz") {
		t.Fatalf("state %q is not a 128-bit hex value", state)
	}

	id, err := GenerateID()
	if err != nil {
		t.Fatalf("generate id: %v", err)
	}
	if len(id) != 32 {
		t.Fatalf("id %q is not 32 hex characters", id)
	}
}

func TestSessionStoreLifecycle(t *testing.T) {
	store := NewSessionStoreWithTTL(time.Minute)
	store.Set("session-1", &Session{State: "state-1", CodeVerifier: "verifier-1"})

	session, ok := store.Get("session-1")
	if !ok || session.State != "state-1" {
		t.Fatalf("get session = %+v, ok = %v", session, ok)
	}
	if store.Len() != 1 {
		t.Fatalf("len = %d, want 1", store.Len())
	}

	store.Delete("session-1")
	if _, ok := store.Get("session-1"); ok {
		t.Fatal("a deleted session must not be readable")
	}

	expired := NewSessionStoreWithTTL(time.Nanosecond)
	expired.Set("session-2", &Session{State: "state-2"})
	time.Sleep(2 * time.Millisecond)
	if _, ok := expired.Get("session-2"); ok {
		t.Fatal("an expired session must not be readable")
	}
	if removed := expired.Cleanup(); removed != 1 {
		t.Fatalf("cleanup removed %d sessions, want 1", removed)
	}
}

func TestOriginatorFallsBackToTheCodexCLIValue(t *testing.T) {
	t.Setenv(OriginatorEnv, "")
	if got := Originator(); got != DefaultOriginator {
		t.Fatalf("originator = %q, want %q", got, DefaultOriginator)
	}

	t.Setenv(OriginatorEnv, "custom-agent")
	if got := Originator(); got != "custom-agent" {
		t.Fatalf("originator = %q, want %q", got, "custom-agent")
	}
}
