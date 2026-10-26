package protocolshim

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/QuantumNous/new-api/relaykit/types"
)

func TestBuildURLAppendsTheProtocolEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		protocol string
		want     string
	}{
		{
			name:     "chat completions on a bare base",
			base:     "https://opencode.ai/zen",
			protocol: ProtocolChatCompletions,
			want:     "https://opencode.ai/zen/v1/chat/completions",
		},
		{
			name:     "anthropic on a bare base",
			base:     "https://opencode.ai/zen/go",
			protocol: ProtocolAnthropic,
			want:     "https://opencode.ai/zen/go/v1/messages",
		},
		{
			name:     "anthropic on a base that already carries v1",
			base:     "https://api.commandcode.ai/provider/v1",
			protocol: ProtocolAnthropic,
			want:     "https://api.commandcode.ai/provider/v1/messages",
		},
		{
			name:     "responses on a base with a trailing slash",
			base:     "https://api.commandcode.ai/provider/v1/",
			protocol: ProtocolResponses,
			want:     "https://api.commandcode.ai/provider/v1/responses",
		},
		{
			name:     "unknown protocol falls back to chat completions",
			base:     "https://example.test",
			protocol: "legacy",
			want:     "https://example.test/v1/chat/completions",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, BuildURL(test.base, test.protocol))
		})
	}
}

func TestRelayFormatForProtocol(t *testing.T) {
	assert.Equal(t, string(types.RelayFormatClaude), string(RelayFormatForProtocol(ProtocolAnthropic)))
	assert.Equal(t, string(types.RelayFormatOpenAIResponses), string(RelayFormatForProtocol(ProtocolResponses)))
	assert.Equal(t, string(types.RelayFormatOpenAI), string(RelayFormatForProtocol(ProtocolChatCompletions)))
	assert.Equal(t, string(types.RelayFormatOpenAI), string(RelayFormatForProtocol("legacy")))
}
