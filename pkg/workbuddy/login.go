package workbuddy

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DefaultPlatform is the platform query value the reference gateway sends by
// default (wb_accounts.py:3036).
const DefaultPlatform = "CLI"

// ErrLoginPending means the browser has not finished signing in yet; the caller
// should poll again (the upstream answers code 11217 while waiting).
var ErrLoginPending = errors.New("workbuddy: login pending")

// TokenResponse is a completed login or a refresh result.
//
// RefreshToken must be written back verbatim: the upstream rotates it, and the
// reference keeps the previous value when a response omits it
// (wb_accounts.py:1247). Losing it turns the credential into a one-shot token.
type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	Domain       string
	UID          string
	Nickname     string
	ExpiresAt    time.Time
}

// LoginSession is one in-flight device-authorization login. The upstream issues
// no credential before the user finishes in the browser; until then all we hold
// is the state and the authorization URL.
type LoginSession struct {
	State     string
	AuthURL   string
	Realm     Realm
	Product   Product
	Platform  string
	CreatedAt time.Time
}

// LoginStore keeps pending login sessions in memory, exactly like the reference
// gateway (wb_accounts.py:3054-3056): they are short-lived, single-use
// bookkeeping, not part of the credential store.
type LoginStore struct {
	mu       sync.Mutex
	sessions map[string]LoginSession
	ttl      time.Duration
	now      func() time.Time
}

// NewLoginStore returns a store with the upstream's 600 second window.
func NewLoginStore() *LoginStore {
	return &LoginStore{
		sessions: make(map[string]LoginSession),
		ttl:      LoginTTLSeconds * time.Second,
		now:      time.Now,
	}
}

// SetClock replaces the time source; it exists for tests.
func (s *LoginStore) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// Put records a pending session. A caller that only knows the state still gets
// a usable session: an unstamped CreatedAt would read as already expired, which
// would fail the login silently instead of loudly.
func (s *LoginStore) Put(session LoginSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = s.now()
	}
	s.sessions[session.State] = session
}

// Get returns a live session; an expired one is dropped and reported missing,
// mirroring poll_login's expiry handling (wb_accounts.py:3064-3066).
func (s *LoginStore) Get(state string) (LoginSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[state]
	if !ok {
		return LoginSession{}, false
	}
	if s.now().Sub(session.CreatedAt) > s.ttl {
		delete(s.sessions, state)
		return LoginSession{}, false
	}
	return session, true
}

// Delete drops a session and reports whether one was present.
func (s *LoginStore) Delete(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[state]; !ok {
		return false
	}
	delete(s.sessions, state)
	return true
}

// Cleanup removes expired sessions and returns how many were dropped. Expired
// sessions an operator walked away from are never read again, so without this
// the map would grow for the lifetime of the process.
func (s *LoginStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	removed := 0
	for state, session := range s.sessions {
		if now.Sub(session.CreatedAt) > s.ttl {
			delete(s.sessions, state)
			removed++
		}
	}
	return removed
}

// StartLogin asks the upstream for a fresh device-authorization state. The
// platform value is echoed back in the authorization URL the panel shows
// (wb_accounts.py:3036-3056).
func (c *Client) StartLogin(ctx context.Context, platform string) (*LoginSession, error) {
	if platform == "" {
		platform = DefaultPlatform
	}
	var data struct {
		State   string `json:"state"`
		AuthURL string `json:"authUrl"`
	}
	path := AuthStatePath + "?platform=" + url.QueryEscape(platform)
	if err := c.call(ctx, http.MethodPost, path, c.managementHeaders(), map[string]any{}, &data); err != nil {
		return nil, err
	}
	if data.State == "" || data.AuthURL == "" {
		return nil, errors.New("workbuddy: auth state response carried no state/authUrl")
	}
	return &LoginSession{
		State:     data.State,
		AuthURL:   data.AuthURL,
		Realm:     c.Realm,
		Product:   c.Product,
		Platform:  platform,
		CreatedAt: time.Now(),
	}, nil
}

// PollLogin performs one poll. It returns ErrLoginPending while the browser
// login is unfinished and a TokenResponse once the upstream issued a token
// (wb_accounts.py:3058-3120).
func (c *Client) PollLogin(ctx context.Context, state string) (*TokenResponse, error) {
	path := AuthTokenPath + "?state=" + url.QueryEscape(state)

	// The pending code is a business code, so it arrives as an *APIError.
	var data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		Domain       string `json:"domain"`
		ExpiresAt    any    `json:"expiresAt"`
	}
	err := c.call(ctx, http.MethodGet, path, c.managementHeaders(), nil, &data)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Code == LoginPending {
			return nil, ErrLoginPending
		}
		return nil, err
	}
	if data.AccessToken == "" {
		return nil, ErrLoginPending
	}

	expiresAt := NormalizeEpoch(data.ExpiresAt)
	if expiresAt.IsZero() {
		expiresAt = TokenExpiry(data.AccessToken)
	}
	domain := data.Domain
	if domain == "" {
		domain = c.Realm.Config().Domain
	}
	return &TokenResponse{
		AccessToken:  data.AccessToken,
		RefreshToken: data.RefreshToken,
		Domain:       domain,
		UID:          TokenSubject(data.AccessToken),
		ExpiresAt:    expiresAt,
	}, nil
}

// FetchProfile returns the account nickname; it is best effort, like the
// reference's guarded profile lookup (wb_accounts.py:3093-3102).
func (c *Client) FetchProfile(ctx context.Context, state, accessToken string) (string, error) {
	headers := c.managementHeaders()
	headers["Authorization"] = "Bearer " + accessToken
	path := LoginAccountPath + "?state=" + url.QueryEscape(state)
	var data struct {
		Nickname string `json:"nickname"`
	}
	if err := c.call(ctx, http.MethodGet, path, headers, nil, &data); err != nil {
		return "", err
	}
	return data.Nickname, nil
}

// RefreshRequest carries everything the refresh endpoint needs.
type RefreshRequest struct {
	UID          string
	RefreshToken string
	Domain       string
	EnterpriseID string
}

// Refresh exchanges a refresh token for a new access token. The caller must
// serialise refreshes per credential: the upstream rotates the refresh token,
// so two concurrent refreshes can make the second send a value the first
// already consumed (wb_accounts.py:1205-1254).
func (c *Client) Refresh(ctx context.Context, request RefreshRequest) (*TokenResponse, error) {
	if request.RefreshToken == "" {
		return nil, errors.New("workbuddy: refresh token is required")
	}
	cfg := c.Realm.Config()
	headers := c.managementHeaders()
	headers["X-Refresh-Token"] = request.RefreshToken
	// The header names which client asked for the refresh; the upstream uses it
	// to decide which session family to reissue.
	if c.Realm == RealmCN {
		headers["X-Auth-Refresh-Source"] = "workbuddy"
	} else {
		headers["X-Auth-Refresh-Source"] = "plugin"
	}
	headers["X-User-Id"] = request.UID
	domain := request.Domain
	if domain == "" {
		domain = cfg.Domain
	}
	headers["X-Domain"] = domain
	headers["X-CodeBuddy-Request"] = "1"
	if c.Realm == RealmIntl {
		headers["Accept-Language"] = "en-US"
	} else {
		headers["Accept-Language"] = "zh-CN"
	}
	if request.EnterpriseID != "" {
		headers["X-Enterprise-Id"] = request.EnterpriseID
	}

	// Some deployments answer with {"data":{"data":{...}}}; the reference
	// unwraps one optional extra level (wb_accounts.py:1240-1241).
	var data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		Data         *struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodPost, RefreshPath, headers, map[string]any{}, &data); err != nil {
		return nil, err
	}
	if data.AccessToken == "" && data.Data != nil {
		data.AccessToken = data.Data.AccessToken
		data.RefreshToken = data.Data.RefreshToken
	}
	if data.AccessToken == "" {
		return nil, errors.New("workbuddy: refresh returned no access token")
	}

	// The refresh response carries no expiresAt; the only deadline is the new
	// token's own exp claim (wb_accounts.py:1248).
	expiresAt := TokenExpiry(data.AccessToken)
	refreshToken := data.RefreshToken
	if refreshToken == "" {
		refreshToken = request.RefreshToken
	}
	return &TokenResponse{
		AccessToken:  data.AccessToken,
		RefreshToken: refreshToken,
		Domain:       domain,
		UID:          request.UID,
		ExpiresAt:    expiresAt,
	}, nil
}
