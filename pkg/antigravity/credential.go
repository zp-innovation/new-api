package antigravity

import (
	"errors"
	"strings"
)

// Credential is the Antigravity account credential produced by the OAuth flow.
// The host stores it as the channel key, so channel grouping, weighting, proxy
// and audit handling keep working unchanged.
type Credential struct {
	Type         string `json:"type,omitempty"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	LastRefresh  string `json:"last_refresh,omitempty"`
	Email        string `json:"email,omitempty"`
	ProjectID    string `json:"project_id"`
	PlanType     string `json:"plan_type,omitempty"`
}

// ParseCredential decodes and validates a channel key. Unknown fields are
// ignored, so keys written with fields later retired by the gateway still parse.
func ParseCredential(raw string) (*Credential, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("antigravity: empty credential")
	}

	var credential Credential
	if err := unmarshalJSON([]byte(raw), &credential); err != nil {
		return nil, errors.New("antigravity: invalid credential json")
	}
	if strings.TrimSpace(credential.AccessToken) == "" {
		return nil, errors.New("antigravity: access_token is required")
	}
	if strings.TrimSpace(credential.ProjectID) == "" {
		return nil, errors.New("antigravity: project_id is required")
	}
	return &credential, nil
}
