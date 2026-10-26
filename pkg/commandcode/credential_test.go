package commandcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCredentialAcceptsRawAPIKey(t *testing.T) {
	credential, err := ParseCredential("user_abc123")
	require.NoError(t, err)
	assert.Equal(t, CredentialType, credential.Type)
	assert.Equal(t, AuthTypeAPIKey, credential.AuthType)
	assert.Equal(t, "user_abc123", credential.APIKey)
}

func TestParseCredentialAcceptsAuthJSON(t *testing.T) {
	credential, err := ParseCredential(`{"apiKey":"user_abc123","userId":"u1","userName":"Alice","keyName":"cli","authenticatedAt":"2026-10-17T00:00:00Z"}`)
	require.NoError(t, err)
	assert.Equal(t, "user_abc123", credential.APIKey)
	assert.Equal(t, "u1", credential.UserID)
	assert.Equal(t, "Alice", credential.UserName)
	assert.Equal(t, "cli", credential.KeyName)
	assert.Equal(t, "2026-10-17T00:00:00Z", credential.AuthenticatedAt)
}

func TestParseCredentialRejectsWrongType(t *testing.T) {
	_, err := ParseCredential(`{"type":"opencode","api_key":"user_abc123"}`)
	require.Error(t, err)
}

func TestParseCredentialRejectsMultiLineRawKey(t *testing.T) {
	_, err := ParseCredential("user_abc123\nuser_def456")
	require.Error(t, err)
}

func TestMarshalCredentialIsCompactJSON(t *testing.T) {
	credential := &Credential{APIKey: "user_abc123", UserName: "Alice"}
	encoded, err := credential.Marshal()
	require.NoError(t, err)
	assert.NotContains(t, encoded, "\n")
	assert.Contains(t, encoded, `"type":"command_code"`)
	assert.Contains(t, encoded, `"auth_type":"api_key"`)
	assert.Contains(t, encoded, `"api_key":"user_abc123"`)
}

func TestMaskedAPIKeyHidesTheMiddle(t *testing.T) {
	credential := &Credential{APIKey: "user_1234567890abcdef"}
	assert.Equal(t, "user...cdef", credential.MaskedAPIKey())
}
