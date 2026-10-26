package antigravity

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

// Google OAuth endpoints and Antigravity client identity.
const (
	AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL     = "https://oauth2.googleapis.com/token"
	UserInfoURL  = "https://www.googleapis.com/oauth2/v2/userinfo"

	// ClientID identifies the Antigravity desktop client to Google.
	ClientID = "" // Redacted in this snapshot; supply your own Google OAuth client id.

	// DefaultClientSecret is the secret shipped inside the Antigravity desktop
	// client. Google does not treat installed-application secrets as
	// confidential, so keeping a working default makes the integration usable
	// out of the box; deployments may override it through ClientSecretEnv.
	DefaultClientSecret = "" // Redacted in this snapshot; supply your own or set ANTIGRAVITY_OAUTH_CLIENT_SECRET.

	// ClientSecretEnv overrides DefaultClientSecret at process start.
	ClientSecretEnv = "ANTIGRAVITY_OAUTH_CLIENT_SECRET"

	// RedirectURI is the fixed loopback callback. Nothing listens on it, so the
	// browser lands on a connection error page and the user copies the code
	// from the address bar.
	RedirectURI = "http://localhost:8085/callback"

	// Scope is the Google OAuth scope set requested during authorization.
	Scope = "https://www.googleapis.com/auth/cloud-platform " +
		"https://www.googleapis.com/auth/userinfo.email " +
		"https://www.googleapis.com/auth/userinfo.profile " +
		"https://www.googleapis.com/auth/cclog " +
		"https://www.googleapis.com/auth/experimentsandconfigs"

	// SessionTTL bounds how long an authorization session stays usable.
	SessionTTL = 30 * time.Minute

	// URLAvailabilityTTL is how long a failed base URL stays demoted.
	URLAvailabilityTTL = 5 * time.Minute
)

var defaultClientSecret = func() string {
	if secret := strings.TrimSpace(os.Getenv(ClientSecretEnv)); secret != "" {
		return secret
	}
	return DefaultClientSecret
}()

// ClientSecret returns the effective OAuth client secret.
func ClientSecret() string {
	return defaultClientSecret
}

// GenerateState returns a 32-byte base64url state parameter.
func GenerateState() (string, error) {
	raw, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	return base64URLEncode(raw), nil
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
// ids and for relay envelope request ids.
func GenerateID() (string, error) {
	raw, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// BuildAuthorizationURL renders the Google consent URL for the given state and
// PKCE challenge.
func BuildAuthorizationURL(state, codeChallenge string) string {
	params := url.Values{}
	params.Set("client_id", ClientID)
	params.Set("redirect_uri", RedirectURI)
	params.Set("response_type", "code")
	params.Set("scope", Scope)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("include_granted_scopes", "true")
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
	State        string    `json:"state"`
	CodeVerifier string    `json:"code_verifier"`
	ProxyURL     string    `json:"proxy_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
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

// URLAvailability tracks which base URLs are temporarily failing so requests can
// prefer the most recently successful host.
type URLAvailability struct {
	mu          sync.RWMutex
	unavailable map[string]time.Time
	ttl         time.Duration
	lastSuccess string
}

// NewURLAvailability creates a tracker with the given demotion TTL.
func NewURLAvailability(ttl time.Duration) *URLAvailability {
	return &URLAvailability{
		unavailable: make(map[string]time.Time),
		ttl:         ttl,
	}
}

// MarkUnavailable demotes a base URL for the configured TTL.
func (u *URLAvailability) MarkUnavailable(baseURL string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.unavailable[baseURL] = time.Now().Add(u.ttl)
}

// MarkSuccess promotes a base URL and clears its demotion.
func (u *URLAvailability) MarkSuccess(baseURL string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.lastSuccess = baseURL
	delete(u.unavailable, baseURL)
}

// Available returns the usable base URLs, most recently successful first.
// It never returns an empty slice unless the input is empty.
func (u *URLAvailability) Available(baseURLs []string) []string {
	u.mu.RLock()
	defer u.mu.RUnlock()

	now := time.Now()
	ordered := make([]string, 0, len(baseURLs))
	if u.lastSuccess != "" && containsString(baseURLs, u.lastSuccess) && !u.demoted(u.lastSuccess, now) {
		ordered = append(ordered, u.lastSuccess)
	}
	for _, baseURL := range baseURLs {
		if baseURL == u.lastSuccess {
			continue
		}
		if u.demoted(baseURL, now) {
			continue
		}
		ordered = append(ordered, baseURL)
	}
	if len(ordered) == 0 {
		// Every host is demoted: fall back to the configured order so a stale
		// demotion cannot take the channel offline permanently.
		return append([]string(nil), baseURLs...)
	}
	return ordered
}

func (u *URLAvailability) demoted(baseURL string, now time.Time) bool {
	expiry, exists := u.unavailable[baseURL]
	return exists && now.Before(expiry)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
