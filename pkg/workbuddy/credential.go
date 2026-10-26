package workbuddy

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"strings"
	"time"
)

// CredentialType is the "type" marker every WorkBuddy credential carries. The
// channel key column stores credentials verbatim, so the marker lets an import
// reject a credential pasted into the wrong channel.
const CredentialType = "workbuddy"

// CredentialRefreshWindow is how long before expiry a credential counts as due
// for a refresh. The reference gateway refreshes two minutes early
// (wb_accounts.py:1063-1096).
const CredentialRefreshWindow = 120 * time.Second

// DefaultProduct is the identity a credential uses when it names none. The CLI
// identity is the smallest surface the upstream accepts.
const DefaultProduct = ProductCLI

// Credential is the channel key of a WorkBuddy channel: one account of one
// realm. The JSON lives in the channel key column, which the relay splits on
// newlines, so it must stay on a single line (see Marshal).
type Credential struct {
	Type         string `json:"type"`
	Realm        string `json:"realm,omitempty"`
	Product      string `json:"product,omitempty"`
	UID          string `json:"uid"`
	Nickname     string `json:"nickname,omitempty"`
	Domain       string `json:"domain,omitempty"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	LastRefresh  string `json:"last_refresh,omitempty"`
	EnterpriseID string `json:"enterprise_id,omitempty"`
	// TasksEnabled separates automatic welfare participation from API routing.
	// A nil value preserves the behaviour of credentials written before the
	// switch existed: those accounts follow the channel's enabled state.
	TasksEnabled *bool `json:"tasks_enabled,omitempty"`
}

// ParseCredential decodes a channel key. The access token and the uid are
// required: without them no request can ever be served, and the failure has to
// surface when the channel is saved rather than on the first relay call.
func ParseCredential(raw string) (*Credential, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("workbuddy: credential is empty")
	}
	var credential Credential
	if err := common.Unmarshal([]byte(trimmed), &credential); err != nil {
		return nil, errors.New("workbuddy: invalid credential json")
	}
	if declared := strings.TrimSpace(credential.Type); declared != "" && !strings.EqualFold(declared, CredentialType) {
		return nil, fmt.Errorf("workbuddy: credential type %q is not %q", declared, CredentialType)
	}
	if strings.TrimSpace(credential.AccessToken) == "" {
		return nil, errors.New("workbuddy: access_token is required")
	}
	if strings.TrimSpace(credential.UID) == "" {
		return nil, errors.New("workbuddy: uid is required")
	}
	return &credential, nil
}

// ValidateForStorage additionally requires the refresh token. The upstream
// rotates refresh tokens, so a credential without one is a one-shot token that
// dies at the first expiry; the login and import paths reject it up front
// instead of failing hours later.
func (c *Credential) ValidateForStorage() error {
	if c == nil {
		return errors.New("workbuddy: credential is nil")
	}
	if strings.TrimSpace(c.RefreshToken) == "" {
		return errors.New("workbuddy: refresh_token is required")
	}
	return nil
}

// Identity is the outbound request identity of this account.
func (c *Credential) Identity() Identity {
	if c == nil {
		return Identity{}
	}
	product := strings.TrimSpace(c.Product)
	if product == "" {
		product = string(DefaultProduct)
	}
	return Identity{
		Realm:        NormalizeRealm(c.Realm),
		Product:      NormalizeProduct(product),
		UID:          strings.TrimSpace(c.UID),
		AccessToken:  strings.TrimSpace(c.AccessToken),
		Domain:       strings.TrimSpace(c.Domain),
		EnterpriseID: strings.TrimSpace(c.EnterpriseID),
	}
}

// ChatBaseURL is the host relay traffic must use. The realm and the product
// decide it, never the channel row (wb_identity.py:51-68).
func (c *Credential) ChatBaseURL() string {
	if c == nil {
		return ""
	}
	identity := c.Identity()
	baseURL, _ := Endpoint(identity.Realm, identity.Product)
	return baseURL
}

// Expiry returns the access token deadline: the stored expires_at when it
// parses, otherwise the exp claim of the token itself.
func (c *Credential) Expiry() time.Time {
	if c == nil {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(c.ExpiresAt)); err == nil {
		return parsed
	}
	return TokenExpiry(strings.TrimSpace(c.AccessToken))
}

// NeedsRefresh reports whether the access token is missing or expires within
// the window, mirroring the reference's ready() (wb_accounts.py:1063-1096).
func (c *Credential) NeedsRefresh(now time.Time, window time.Duration) bool {
	if c == nil {
		return true
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return true
	}
	expiry := c.Expiry()
	if expiry.IsZero() {
		return true
	}
	if now.IsZero() {
		now = time.Now()
	}
	return !expiry.After(now.Add(window))
}

// WithTokens merges a login or refresh result back into the credential. The
// refresh token is written back verbatim, and the previous one is kept when the
// response omits it: the upstream rotates it and the reference preserves the
// old value (wb_accounts.py:1247). Dropping it turns the credential into a
// one-shot token.
func (c *Credential) WithTokens(tokens *TokenResponse, now time.Time) *Credential {
	if c == nil || tokens == nil {
		return c
	}
	updated := *c
	updated.Type = CredentialType
	if value := strings.TrimSpace(tokens.AccessToken); value != "" {
		updated.AccessToken = value
	}
	if value := strings.TrimSpace(tokens.RefreshToken); value != "" {
		updated.RefreshToken = value
	}
	if value := strings.TrimSpace(tokens.Domain); value != "" {
		updated.Domain = value
	}
	if value := strings.TrimSpace(tokens.Nickname); value != "" {
		updated.Nickname = value
	}
	if value := strings.TrimSpace(tokens.UID); value != "" {
		updated.UID = value
	}
	if !tokens.ExpiresAt.IsZero() {
		updated.ExpiresAt = tokens.ExpiresAt.UTC().Format(time.RFC3339)
	} else if expiry := TokenExpiry(updated.AccessToken); !expiry.IsZero() {
		updated.ExpiresAt = expiry.UTC().Format(time.RFC3339)
	}
	if enterprise := TokenEnterpriseID(updated.AccessToken); enterprise != "" {
		updated.EnterpriseID = enterprise
	}
	if now.IsZero() {
		now = time.Now()
	}
	updated.LastRefresh = now.UTC().Format(time.RFC3339)
	return &updated
}

// Marshal renders the credential as the compact single-line JSON the channel
// key column requires. The column is split on newlines, so a pretty-printed
// credential would be read back as several broken keys.
func (c *Credential) Marshal() (string, error) {
	if c == nil {
		return "", errors.New("workbuddy: credential is nil")
	}
	clone := *c
	if strings.TrimSpace(clone.Type) == "" {
		clone.Type = CredentialType
	}
	if strings.TrimSpace(clone.Realm) == "" {
		clone.Realm = string(RealmIntl)
	}
	payload, err := common.Marshal(clone)
	if err != nil {
		return "", fmt.Errorf("workbuddy: encode credential: %w", err)
	}
	return string(payload), nil
}
