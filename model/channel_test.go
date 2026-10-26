package model

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A single-key channel has no fallback credential, but that one credential can
// still be an account the caller excluded for the requested model.
func TestGetNextEnabledKeyExcludingBlockedSingleKeyCredential(t *testing.T) {
	channel := &Channel{Id: 48, Type: constant.ChannelTypeWorkBuddy, Key: `{"uid":"uid-1"}`}

	key, index, apiErr := channel.GetNextEnabledKeyExcluding(map[string]bool{"uid-1": true})
	require.NotNil(t, apiErr)
	assert.Empty(t, key)
	assert.Equal(t, 0, index)
	assert.Equal(t, types.ErrorCodeChannelNoAvailableKey, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.True(t, apiErr.IsModelScoped(), "a blocked account is a model-scoped refusal, not a broken channel")
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.Contains(t, apiErr.Error(), "no available key")
}

func TestGetNextEnabledKeyExcludingSingleKeyCredentialWhenNotBlocked(t *testing.T) {
	channel := &Channel{Id: 48, Type: constant.ChannelTypeWorkBuddy, Key: `{"uid":"uid-1"}`}

	key, index, apiErr := channel.GetNextEnabledKeyExcluding(map[string]bool{"uid-2": true})
	require.Nil(t, apiErr)
	assert.Equal(t, channel.Key, key)
	assert.Equal(t, 0, index)

	key, index, apiErr = channel.GetNextEnabledKeyExcluding(nil)
	require.Nil(t, apiErr)
	assert.Equal(t, channel.Key, key)
	assert.Equal(t, 0, index)
}

// Credentials that are not JSON account lines keep the old pass-through
// behavior even when the caller supplies exclusions.
func TestGetNextEnabledKeyExcludingSingleKeyPlainCredentialKeepsWorking(t *testing.T) {
	channel := &Channel{Id: 48, Type: constant.ChannelTypeWorkBuddy, Key: "sk-plain"}

	key, index, apiErr := channel.GetNextEnabledKeyExcluding(map[string]bool{"uid-1": true})
	require.Nil(t, apiErr)
	assert.Equal(t, "sk-plain", key)
	assert.Equal(t, 0, index)
}
