package opencode

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Credential is the OpenCode Console credential stored as a channel key.
//
// A channel key is split on newlines for multi-key channels, so Marshal always
// emits a compact single-line JSON object.
type Credential struct {
	Type         string `json:"type,omitempty"`
	AuthType     string `json:"auth_type,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Server       string `json:"server,omitempty"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	LastRefresh  string `json:"last_refresh,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	Email        string `json:"email,omitempty"`
	OrgID        string `json:"org_id,omitempty"`
	OrgName      string `json:"org_name,omitempty"`
	// ProtocolRules optionally overrides the built-in Zen/Go model routing
	// table, matching the sub2api account format.
	ProtocolRules []ProtocolRule `json:"protocol_rules,omitempty"`
}

const (
	// AuthTypeOAuth is a Console device-flow credential with a refresh token.
	AuthTypeOAuth = "oauth"
	// AuthTypeAPIKey is a service-account API key created in Console. It has no
	// refresh token and is used verbatim as the bearer token.
	AuthTypeAPIKey = "api_key"
)

// ParseCredential decodes and validates one compact channel key.
func ParseCredential(raw string) (*Credential, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("opencode: credential is empty")
	}
	if !strings.HasPrefix(trimmed, "{") {
		if strings.ContainsAny(trimmed, "\r\n") {
			return nil, errors.New("opencode: API key must be a single line")
		}
		return &Credential{
			Type:        CredentialType,
			AuthType:    AuthTypeAPIKey,
			AccessToken: trimmed,
		}, nil
	}
	var input struct {
		Credential
		APIKey      string `json:"api_key"`
		AccountMode string `json:"account_mode"`
		BaseURL     string `json:"base_url"`
	}
	if err := common.Unmarshal([]byte(trimmed), &input); err != nil {
		return nil, errors.New("opencode: invalid credential json")
	}
	credential := input.Credential
	if strings.TrimSpace(credential.AccessToken) == "" && strings.TrimSpace(input.APIKey) != "" {
		credential.AccessToken = strings.TrimSpace(input.APIKey)
		credential.AuthType = AuthTypeAPIKey
	}
	if strings.TrimSpace(credential.Mode) == "" && strings.TrimSpace(input.AccountMode) != "" {
		credential.Mode = input.AccountMode
	}
	if strings.TrimSpace(credential.Mode) == "" && strings.TrimSpace(input.BaseURL) != "" {
		credential.Mode = modeFromBaseURL(input.BaseURL)
	}
	if declared := strings.TrimSpace(credential.Type); declared != "" && !strings.EqualFold(declared, CredentialType) {
		return nil, fmt.Errorf("opencode: credential type %q is not %q", declared, CredentialType)
	}
	if strings.TrimSpace(credential.AccessToken) == "" {
		return nil, errors.New("opencode: access_token is required")
	}
	if strings.TrimSpace(credential.AuthType) == "" {
		if strings.TrimSpace(credential.RefreshToken) == "" &&
			strings.HasPrefix(strings.TrimSpace(credential.AccessToken), "oc_") {
			credential.AuthType = AuthTypeAPIKey
		} else {
			credential.AuthType = AuthTypeOAuth
		}
	}
	return &credential, nil
}

func modeFromBaseURL(baseURL string) string {
	if strings.Contains(strings.ToLower(baseURL), "/zen/go") {
		return ModeGo
	}
	return ModeZen
}

// ValidateForStorage additionally requires a refresh token. OpenCode rotates
// refresh tokens, so a credential without one is a one-shot token.
func (c *Credential) ValidateForStorage() error {
	if c == nil {
		return errors.New("opencode: credential is nil")
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return errors.New("opencode: access_token is required")
	}
	if c.IsAPIKey() {
		return nil
	}
	if strings.TrimSpace(c.RefreshToken) == "" {
		return errors.New("opencode: refresh_token is required")
	}
	return nil
}

// Marshal renders the credential as compact single-line JSON.
func (c *Credential) Marshal() (string, error) {
	if c == nil {
		return "", errors.New("opencode: credential is nil")
	}
	clone := *c
	if strings.TrimSpace(clone.Type) == "" {
		clone.Type = CredentialType
	}
	clone.Mode = NormalizeMode(clone.Mode)
	if clone.IsAPIKey() {
		clone.AuthType = AuthTypeAPIKey
		clone.RefreshToken = ""
	} else if strings.TrimSpace(clone.AuthType) == "" {
		clone.AuthType = AuthTypeOAuth
	}
	payload, err := common.Marshal(clone)
	if err != nil {
		return "", fmt.Errorf("opencode: encode credential: %w", err)
	}
	return string(payload), nil
}

// IsAPIKey reports whether the credential is a service-account API key.
func (c *Credential) IsAPIKey() bool {
	return c != nil && strings.EqualFold(strings.TrimSpace(c.AuthType), AuthTypeAPIKey)
}

// ModeOrDefault returns a supported mode, defaulting to Zen.
func (c *Credential) ModeOrDefault() string {
	if c == nil {
		return ModeZen
	}
	return NormalizeMode(c.Mode)
}

// NormalizeMode trims and validates a mode.
func NormalizeMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == ModeGo {
		return ModeGo
	}
	return ModeZen
}

// Expiry returns the stored deadline, or the zero time when it does not parse.
func (c *Credential) Expiry() time.Time {
	if c == nil {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(c.ExpiresAt))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// NeedsRefresh reports whether the credential is missing an access token or
// expires inside the window.
func (c *Credential) NeedsRefresh(now time.Time, window time.Duration) bool {
	if c == nil || strings.TrimSpace(c.AccessToken) == "" {
		return true
	}
	if c.IsAPIKey() {
		return false
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

// WithToken merges a login or refresh response into a credential. A missing
// refresh token keeps the previous value because the upstream rotates it.
func (c *Credential) WithToken(token *TokenResponse, now time.Time) *Credential {
	if c == nil || token == nil {
		return c
	}
	updated := *c
	updated.Type = CredentialType
	updated.AuthType = AuthTypeOAuth
	updated.Mode = NormalizeMode(updated.Mode)
	if strings.TrimSpace(updated.Server) == "" {
		updated.Server = DefaultServer
	}
	if value := strings.TrimSpace(token.AccessToken); value != "" {
		updated.AccessToken = value
	}
	if value := strings.TrimSpace(token.RefreshToken); value != "" {
		updated.RefreshToken = value
	}
	if token.ExpiresIn > 0 {
		updated.ExpiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	if value := strings.TrimSpace(token.OrgID); value != "" {
		if updated.OrgID != "" && updated.OrgID != value {
			// The organization changed; the old display name belongs to a
			// different organization and is cleared until the next Console
			// config refresh reports the new one.
			updated.OrgName = ""
		}
		updated.OrgID = value
	}
	if now.IsZero() {
		now = time.Now()
	}
	updated.LastRefresh = now.UTC().Format(time.RFC3339)
	return &updated
}

// BaseURL returns the inference base for the credential's mode.
func (c *Credential) BaseURL() string {
	if c != nil && NormalizeMode(c.Mode) == ModeGo {
		return BaseURLGo
	}
	return BaseURLZen
}

// AnthropicBaseURL returns the Anthropic inference base for the mode.
func (c *Credential) AnthropicBaseURL() string {
	if c != nil && NormalizeMode(c.Mode) == ModeGo {
		return BaseURLGoAnthropic
	}
	return BaseURLZenAnthropic
}

// ServerOrDefault returns the Console origin used for device authorization.
func (c *Credential) ServerOrDefault() string {
	if c == nil || strings.TrimSpace(c.Server) == "" {
		return DefaultServer
	}
	return strings.TrimRight(strings.TrimSpace(c.Server), "/")
}
