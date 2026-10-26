package commandcode

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Credential is the Command Code account stored as a channel key.
//
// A channel key is split on newlines for multi-key channels, so Marshal always
// emits a compact single-line JSON object. A bare API key is also accepted and
// normalized on parse, which keeps the manual paste flow usable without
// requiring the operator to hand-build JSON.
type Credential struct {
	Type            string         `json:"type,omitempty"`
	AuthType        string         `json:"auth_type,omitempty"`
	APIKey          string         `json:"api_key"`
	UserID          string         `json:"user_id,omitempty"`
	UserName        string         `json:"user_name,omitempty"`
	KeyName         string         `json:"key_name,omitempty"`
	OrgID           string         `json:"org_id,omitempty"`
	OrgName         string         `json:"org_name,omitempty"`
	AuthenticatedAt string         `json:"authenticated_at,omitempty"`
	ProtocolRules   []ProtocolRule `json:"protocol_rules,omitempty"`
}

// ParseCredential decodes and validates one compact channel key. The camelCase
// field names written by ~/.commandcode/auth.json are accepted as aliases.
func ParseCredential(raw string) (*Credential, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("command_code: credential is empty")
	}
	if !strings.HasPrefix(trimmed, "{") {
		if strings.ContainsAny(trimmed, "\r\n") {
			return nil, errors.New("command_code: API key must be a single line")
		}
		return &Credential{
			Type:     CredentialType,
			AuthType: AuthTypeAPIKey,
			APIKey:   trimmed,
		}, nil
	}

	var input struct {
		Credential
		APIKeyCamel          string `json:"apiKey"`
		UserIDCamel          string `json:"userId"`
		UserNameCamel        string `json:"userName"`
		KeyNameCamel         string `json:"keyName"`
		AuthenticatedAtCamel string `json:"authenticatedAt"`
	}
	if err := common.Unmarshal([]byte(trimmed), &input); err != nil {
		return nil, errors.New("command_code: invalid credential json")
	}
	credential := input.Credential
	if strings.TrimSpace(credential.APIKey) == "" {
		credential.APIKey = input.APIKeyCamel
	}
	if strings.TrimSpace(credential.UserID) == "" {
		credential.UserID = input.UserIDCamel
	}
	if strings.TrimSpace(credential.UserName) == "" {
		credential.UserName = input.UserNameCamel
	}
	if strings.TrimSpace(credential.KeyName) == "" {
		credential.KeyName = input.KeyNameCamel
	}
	if strings.TrimSpace(credential.AuthenticatedAt) == "" {
		credential.AuthenticatedAt = input.AuthenticatedAtCamel
	}
	if declared := strings.TrimSpace(credential.Type); declared != "" && !strings.EqualFold(declared, CredentialType) {
		return nil, fmt.Errorf("command_code: credential type %q is not %q", declared, CredentialType)
	}
	if strings.TrimSpace(credential.APIKey) == "" {
		return nil, errors.New("command_code: api_key is required")
	}
	credential.Type = CredentialType
	credential.AuthType = AuthTypeAPIKey
	credential.APIKey = strings.TrimSpace(credential.APIKey)
	credential.UserID = strings.TrimSpace(credential.UserID)
	credential.UserName = strings.TrimSpace(credential.UserName)
	credential.KeyName = strings.TrimSpace(credential.KeyName)
	credential.OrgID = strings.TrimSpace(credential.OrgID)
	credential.OrgName = strings.TrimSpace(credential.OrgName)
	credential.AuthenticatedAt = strings.TrimSpace(credential.AuthenticatedAt)
	return &credential, nil
}

// ValidateForStorage requires the API key the relay injects as a bearer token.
func (c *Credential) ValidateForStorage() error {
	if c == nil {
		return errors.New("command_code: credential is nil")
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return errors.New("command_code: api_key is required")
	}
	return nil
}

// Marshal renders the credential as compact single-line JSON.
func (c *Credential) Marshal() (string, error) {
	if c == nil {
		return "", errors.New("command_code: credential is nil")
	}
	clone := *c
	clone.Type = CredentialType
	clone.AuthType = AuthTypeAPIKey
	payload, err := common.Marshal(clone)
	if err != nil {
		return "", fmt.Errorf("command_code: encode credential: %w", err)
	}
	return string(payload), nil
}

// MaskedAPIKey is the safe representation shown by the console.
func (c *Credential) MaskedAPIKey() string {
	if c == nil {
		return ""
	}
	key := strings.TrimSpace(c.APIKey)
	if len(key) <= 8 {
		if key == "" {
			return ""
		}
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
