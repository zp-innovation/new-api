package codex

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// OpenAI Codex (ChatGPT subscription) OAuth endpoints and client identity.
const (
	AuthorizeURL = "https://auth.openai.com/oauth/authorize"
	TokenURL     = "https://auth.openai.com/oauth/token"

	// ClientID identifies the Codex CLI OAuth client to OpenAI.
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// RedirectURI is the loopback callback the Codex CLI registers. Nothing
	// listens on it here, so the browser lands on a connection error page and the
	// operator copies the authorization code from the address bar.
	RedirectURI = "http://localhost:1455/auth/callback"

	// Scope is the scope set requested during authorization. offline_access is
	// what makes the token endpoint return a refresh token.
	Scope = "openid profile email offline_access"

	// JWTClaimPath is the access-token claim that carries chatgpt_account_id.
	JWTClaimPath = "https://api.openai.com/auth"

	// SessionTTL bounds how long an authorization session stays usable.
	SessionTTL = 30 * time.Minute

	// DefaultOriginator mirrors the originator parameter the Codex CLI sends.
	// The client id above belongs to the Codex CLI, so the authorization request
	// carries the same originator; deployments may override it through
	// OriginatorEnv.
	DefaultOriginator = "codex_cli_rs"

	// OriginatorEnv overrides DefaultOriginator.
	OriginatorEnv = "CODEX_OAUTH_ORIGINATOR"
)

// Originator returns the effective originator parameter.
func Originator() string {
	if value := strings.TrimSpace(os.Getenv(OriginatorEnv)); value != "" {
		return value
	}
	return DefaultOriginator
}

// GenerateState returns a random 128-bit hex state parameter.
func GenerateState() (string, error) {
	raw, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// GenerateCodeVerifier returns an RFC 7636 code verifier (43 characters).
func GenerateCodeVerifier() (string, error) {
	raw, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	return base64URLEncode(raw), nil
}

// GenerateCodeChallenge derives the S256 PKCE challenge for a verifier.
func GenerateCodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64URLEncode(sum[:])
}

// GenerateID returns a random hex identifier, used for authorization session
// ids.
func GenerateID() (string, error) {
	raw, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// BuildAuthorizationURL renders the OpenAI consent URL for the given state and
// PKCE challenge.
func BuildAuthorizationURL(state, codeChallenge string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", ClientID)
	params.Set("redirect_uri", RedirectURI)
	params.Set("scope", Scope)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	params.Set("originator", Originator())
	return AuthorizeURL + "?" + params.Encode()
}

func randomBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}
	return buf, nil
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

// Session holds the server-side state of one authorization attempt.
type Session struct {
	State        string
	CodeVerifier string
	ProxyURL     string
	CreatedAt    time.Time
}

// SessionStore keeps authorization sessions in memory. Sessions are addressed
// by a random id that never reaches the upstream provider.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewSessionStore creates an empty store using SessionTTL.
func NewSessionStore() *SessionStore {
	return NewSessionStoreWithTTL(SessionTTL)
}

// NewSessionStoreWithTTL creates an empty store with an explicit lifetime.
func NewSessionStoreWithTTL(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
}

// Set stores a session, replacing any previous session with the same id.
func (s *SessionStore) Set(id string, session *Session) {
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = session
}

// Get returns a live session without removing it. Callers delete the session
// only after the exchange succeeds, so a mistyped code can be retried without
// restarting the whole authorization flow.
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	if s.expired(session) {
		return nil, false
	}
	return session, true
}

// Delete drops a session without returning it.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// Len reports the number of stored sessions, including expired ones that have
// not been collected yet.
func (s *SessionStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// Cleanup removes expired sessions and returns how many were dropped.
func (s *SessionStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, session := range s.sessions {
		if s.expired(session) {
			delete(s.sessions, id)
			removed++
		}
	}
	return removed
}

func (s *SessionStore) expired(session *Session) bool {
	return time.Since(session.CreatedAt) > s.ttl
}
