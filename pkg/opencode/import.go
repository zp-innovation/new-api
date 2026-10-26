package opencode

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type authExportEntry struct {
	ID            string         `json:"id"`
	IntegrationID string         `json:"integrationID"`
	Label         string         `json:"label"`
	Active        bool           `json:"active"`
	Value         map[string]any `json:"value"`
}

// ImportAuthExport converts `opencode auth export` output into channel
// credentials. It accepts both a JSON array and a single object.
func ImportAuthExport(raw string) ([]*Credential, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("opencode: auth export is empty")
	}
	var entries []authExportEntry
	if err := common.Unmarshal([]byte(trimmed), &entries); err != nil {
		var single authExportEntry
		if singleErr := common.Unmarshal([]byte(trimmed), &single); singleErr != nil {
			return nil, errors.New("opencode: invalid auth export json")
		}
		entries = []authExportEntry{single}
	}
	credentials := make([]*Credential, 0, len(entries))
	for _, entry := range entries {
		if integration := strings.TrimSpace(entry.IntegrationID); integration != "" && integration != AuthExportIntegrationID {
			continue
		}
		value := entry.Value
		if value == nil {
			continue
		}
		if credentialType := strings.TrimSpace(stringValue(value["type"])); credentialType != "" && credentialType != "oauth" {
			continue
		}
		access := strings.TrimSpace(stringValue(value["access"]))
		refresh := strings.TrimSpace(stringValue(value["refresh"]))
		if access == "" || refresh == "" {
			return nil, errors.New("opencode: auth export entry is missing access or refresh")
		}
		metadata := mapValue(value["metadata"])
		credential := &Credential{
			Type:         CredentialType,
			Mode:         NormalizeMode(stringValue(metadata["mode"])),
			Server:       strings.TrimSpace(stringValue(metadata["server"])),
			AccessToken:  access,
			RefreshToken: refresh,
			AccountID:    strings.TrimSpace(stringValue(metadata["accountID"])),
			Email:        strings.TrimSpace(stringValue(metadata["email"])),
			OrgID:        strings.TrimSpace(stringValue(metadata["orgID"])),
			OrgName:      strings.TrimSpace(stringValue(metadata["orgName"])),
		}
		if credential.Server == "" {
			credential.Server = DefaultServer
		}
		if expires := numberValue(value["expires"]); expires > 0 {
			credential.ExpiresAt = time.UnixMilli(expires).UTC().Format(time.RFC3339)
		}
		if err := credential.ValidateForStorage(); err != nil {
			return nil, fmt.Errorf("opencode: import credential: %w", err)
		}
		credentials = append(credentials, credential)
	}
	if len(credentials) == 0 {
		return nil, errors.New("opencode: auth export contained no usable OpenCode credentials")
	}
	return credentials, nil
}

func mapValue(value any) map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func stringValue(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func numberValue(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}
