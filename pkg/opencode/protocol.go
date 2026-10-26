package opencode

import (
	"slices"
	"strings"
)

// ProtocolRule maps one model pattern to a preferred upstream protocol.
// Pattern is an exact id or a trailing-* prefix match. The first match wins.
type ProtocolRule struct {
	Pattern  string `json:"pattern"`
	Protocol string `json:"protocol"`
	// Protocols lists every native protocol the model accepts. The first entry
	// is the preferred one. An empty list means only Protocol is supported.
	Protocols []string `json:"protocols,omitempty"`
}

// DefaultGoProtocolRules mirrors the OpenCode Go endpoint table used by the
// reference gateway.
func DefaultGoProtocolRules() []ProtocolRule {
	return []ProtocolRule{
		{Pattern: "grok-*", Protocol: ProtocolResponses},
		{Pattern: "gpt-*", Protocol: ProtocolResponses},
		{Pattern: "muse-spark-*", Protocol: ProtocolResponses},
		{Pattern: "minimax-*", Protocol: ProtocolAnthropic},
		{Pattern: "qwen*", Protocol: ProtocolAnthropic},
	}
}

// DefaultZenProtocolRules mirrors the OpenCode Zen endpoint table.
func DefaultZenProtocolRules() []ProtocolRule {
	return []ProtocolRule{
		{Pattern: "grok-*", Protocol: ProtocolResponses},
		{Pattern: "gpt-*", Protocol: ProtocolResponses},
		{Pattern: "muse-spark-*", Protocol: ProtocolResponses},
		{Pattern: "claude-*", Protocol: ProtocolAnthropic},
		{Pattern: "qwen3.8-max", Protocol: ProtocolChatCompletions},
		{Pattern: "qwen*", Protocol: ProtocolAnthropic},
	}
}

// DefaultProtocolRules returns the rules for one mode.
func DefaultProtocolRules(mode string) []ProtocolRule {
	if NormalizeMode(mode) == ModeGo {
		return DefaultGoProtocolRules()
	}
	return DefaultZenProtocolRules()
}

// NormalizeModelID strips the OpenCode provider prefix from a catalog id.
func NormalizeModelID(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, prefix := range []string{"opencode-go/", "opencode_go/", "opencode/"} {
		model = strings.TrimPrefix(model, prefix)
	}
	return model
}

// IsUnsupportedModel reports models that only work on the dedicated Google or
// SystemOne endpoints, not on the standard Chat/Responses/Messages gateways.
func IsUnsupportedModel(model string) bool {
	normalized := NormalizeModelID(model)
	return strings.HasPrefix(normalized, "gemini-") || strings.HasPrefix(normalized, "jev-")
}

// ResolveProtocol returns the preferred upstream protocol for a model.
func ResolveProtocol(mode, model string, customRules []ProtocolRule) string {
	if set, ok := ResolveProtocolSet(mode, model, customRules); ok {
		return set[0]
	}
	return ProtocolChatCompletions
}

// ResolveProtocolSet returns every native protocol the model supports.
func ResolveProtocolSet(mode, model string, customRules []ProtocolRule) ([]string, bool) {
	rules := customRules
	if len(rules) == 0 {
		rules = DefaultProtocolRules(mode)
	}
	for _, rule := range rules {
		if !protocolRuleMatches(rule.Pattern, model) {
			continue
		}
		set := normalizeProtocolSet(rule.Protocol, rule.Protocols)
		if len(set) > 0 {
			return set, true
		}
	}
	return nil, false
}

// ProtocolBaseURL returns the inference base for one mode and protocol.
func ProtocolBaseURL(mode, protocol string) string {
	credential := &Credential{Mode: NormalizeMode(mode)}
	if protocol == ProtocolAnthropic {
		return credential.AnthropicBaseURL()
	}
	return credential.BaseURL()
}

func protocolRuleMatches(pattern, model string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	model = NormalizeModelID(model)
	if pattern == "" || model == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(model, strings.TrimSuffix(pattern, "*"))
	}
	return model == pattern
}

func normalizeProtocolSet(preferred string, protocols []string) []string {
	preferred = normalizeProtocol(preferred)
	if preferred == "" {
		return nil
	}
	set := []string{preferred}
	for _, protocol := range protocols {
		protocol = normalizeProtocol(protocol)
		if protocol == "" || slices.Contains(set, protocol) {
			continue
		}
		set = append(set, protocol)
	}
	return set
}

func normalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case ProtocolAnthropic:
		return ProtocolAnthropic
	case ProtocolResponses:
		return ProtocolResponses
	case ProtocolChatCompletions:
		return ProtocolChatCompletions
	default:
		return ""
	}
}
