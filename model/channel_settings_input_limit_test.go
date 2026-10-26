package model

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelValidateSettingsRejectsNegativeMaxInputTokens(t *testing.T) {
	channel := &Channel{}
	channel.SetSetting(dto.ChannelSettings{MaxInputTokens: -1})
	err := channel.ValidateSettings()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_input_tokens")

	channel = &Channel{}
	channel.SetSetting(dto.ChannelSettings{MaxInputTokens: 128000})
	require.NoError(t, channel.ValidateSettings())
}
