// Package protocolshim holds the protocol routing shared by the credential-based
// channels. pkg/opencode and pkg/commandcode each declare the same identifiers
// for the upstream dialects (chat completions, Responses, Anthropic Messages),
// so the helpers below take the identifier as a string and neither channel has
// to import the other's constants.
package protocolshim

import (
	"strings"

	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// Protocol identifiers shared by the credential-based channels. They mirror the
// values in pkg/opencode and pkg/commandcode.
const (
	ProtocolChatCompletions = "chat_completions"
	ProtocolResponses       = "responses"
	ProtocolAnthropic       = "anthropic"
)

// BuildURL appends the endpoint path for protocol to baseURL. A base that
// already carries the /v1 segment keeps a single one; Anthropic Messages always
// lives under /v1/messages.
func BuildURL(baseURL, protocol string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	switch protocol {
	case ProtocolAnthropic:
		baseURL = strings.TrimSuffix(baseURL, "/v1")
		return baseURL + "/v1/messages"
	case ProtocolResponses:
		if !strings.HasSuffix(baseURL, "/v1") {
			baseURL += "/v1"
		}
		return baseURL + "/responses"
	default:
		if !strings.HasSuffix(baseURL, "/v1") {
			baseURL += "/v1"
		}
		return baseURL + "/chat/completions"
	}
}

// RelayFormatForProtocol maps an upstream protocol to the relay format used to
// convert a client request for it.
func RelayFormatForProtocol(protocol string) types.RelayFormat {
	switch protocol {
	case ProtocolAnthropic:
		return types.RelayFormatClaude
	case ProtocolResponses:
		return types.RelayFormatOpenAIResponses
	default:
		return types.RelayFormatOpenAI
	}
}

// OpenAIDelegate returns an OpenAI adaptor initialized for info, so a channel
// that speaks chat completions reuses the project's request normalization and
// response handlers instead of a private copy.
func OpenAIDelegate(info *relaycommon.RelayInfo) *openai.Adaptor {
	adaptor := &openai.Adaptor{}
	adaptor.Init(info)
	return adaptor
}

// ClaudeDelegate returns a Claude adaptor initialized for info.
func ClaudeDelegate(info *relaycommon.RelayInfo) *claude.Adaptor {
	adaptor := &claude.Adaptor{}
	adaptor.Init(info)
	return adaptor
}
