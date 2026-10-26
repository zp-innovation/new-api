package service

import (
	"regexp"
	"sync"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

// Chat→Responses upgrade policy is host routing logic (it decides *whether*
// to convert, reading host settings), so it lives here, not in relayconvert.

var chatResponsesRegexCache sync.Map // map[string]*regexp.Regexp

func matchAnyModelPattern(patterns []string, model string) bool {
	if len(patterns) == 0 || model == "" {
		return false
	}
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		re, ok := chatResponsesRegexCache.Load(pattern)
		if !ok {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				// Treat invalid patterns as non-matching to avoid breaking runtime traffic.
				continue
			}
			re = compiled
			chatResponsesRegexCache.Store(pattern, re)
		}
		if re.(*regexp.Regexp).MatchString(model) {
			return true
		}
	}
	return false
}

func ShouldChatCompletionsUseResponsesPolicy(policy model_setting.ChatCompletionsToResponsesPolicy, channelID int, channelType int, model string) bool {
	if !policy.IsChannelEnabled(channelID, channelType) {
		return false
	}
	return matchAnyModelPattern(policy.ModelPatterns, model)
}

func ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool {
	return ShouldChatCompletionsUseResponsesPolicy(
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy,
		channelID,
		channelType,
		model,
	)
}

// ChannelRequiresResponsesForChat reports whether the channel's upstream API
// has no chat completions endpoint, so text requests must be upgraded to the
// Responses protocol before the adaptor sees them. The ChatGPT subscription
// (Codex) backend is Responses-only.
func ChannelRequiresResponsesForChat(channelType int) bool {
	return channelType == constant.ChannelTypeCodex
}

// ResponsesUpstreamAlwaysStreams reports whether the channel's Responses
// endpoint streams unconditionally. The ChatGPT subscription (Codex) backend
// answers /responses with a chunked SSE body but omits the Content-Type
// header, so header-based stream detection cannot be relied on.
func ResponsesUpstreamAlwaysStreams(channelType int) bool {
	return channelType == constant.ChannelTypeCodex
}

// ShouldChatCompletionsUseResponses reports whether a text request entering
// through /v1/chat/completions or /v1/messages should be relayed through the
// Responses protocol. Responses-only channels always take this route;
// otherwise the administrator-controlled upgrade policy decides.
func ShouldChatCompletionsUseResponses(channelID int, channelType int, model string) bool {
	if ChannelRequiresResponsesForChat(channelType) {
		return true
	}
	return ShouldChatCompletionsUseResponsesGlobal(channelID, channelType, model)
}
