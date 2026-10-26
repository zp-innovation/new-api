package commandcode

import (
	"slices"
	"strings"
	"sync"
)

// ProtocolRule maps one model pattern to the upstream protocols it accepts.
// Pattern is an exact id or a trailing-* prefix match. The first match wins.
type ProtocolRule struct {
	Pattern  string `json:"pattern"`
	Protocol string `json:"protocol"`
	// Protocols lists every native protocol the model accepts. The first entry
	// is the preferred one. An empty list means only Protocol is supported.
	Protocols []string `json:"protocols,omitempty"`
}

// ModelInfo is one entry of the public /provider/v1/models catalog.
type ModelInfo struct {
	ID                 string   `json:"id"`
	Object             string   `json:"object"`
	Name               string   `json:"name"`
	ContextLength      int64    `json:"context_length"`
	SupportedEndpoints []string `json:"supported_endpoints"`
}

// DefaultProtocolRules is the conservative fallback used before the first
// successful catalog fetch. The upstream catalog remains the source of truth.
func DefaultProtocolRules() []ProtocolRule {
	return []ProtocolRule{
		{Pattern: "claude-*", Protocol: ProtocolAnthropic, Protocols: []string{ProtocolAnthropic}},
		{Pattern: "gpt-*", Protocol: ProtocolResponses, Protocols: []string{ProtocolResponses, ProtocolChatCompletions}},
	}
}

// DefaultModelIDs is a conservative fallback used before the first successful
// /models fetch.
func DefaultModelIDs() []string {
	return []string{
		"claude-sonnet-4-6",
		"claude-opus-4-6",
		"gpt-5.6-luna",
		"gpt-5.6-sol",
		"deepseek/deepseek-v4-flash",
		"deepseek/deepseek-v4-pro",
		"glm-5.3-flash",
		"kimi-k3",
		"minimax-m3",
		"qwen3.8-max",
	}
}

// NormalizeModelID strips a provider prefix from a catalog id.
func NormalizeModelID(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, prefix := range []string{"command_code/", "command-code/", "commandcode/"} {
		model = strings.TrimPrefix(model, prefix)
	}
	return model
}

// IsUnsupportedModel reports models that only work on a dedicated endpoint the
// relay does not implement (the System One decision API).
func IsUnsupportedModel(model string) bool {
	normalized := NormalizeModelID(model)
	return strings.HasPrefix(normalized, "typesafe/jev") || strings.HasPrefix(normalized, "jev-")
}

// ResolveProtocolSet returns every native protocol the model supports. Custom
// rules take precedence over the cached upstream catalog, and the catalog takes
// precedence over the conservative defaults: a fetched catalog entry is what the
// upstream actually serves, so a claude-*/gpt-* model the catalog maps elsewhere
// must not be routed by the prefix defaults.
func ResolveProtocolSet(model string, customRules []ProtocolRule) ([]string, bool) {
	rules := customRules
	if len(rules) == 0 {
		rules = append(CurrentProtocolRules(), DefaultProtocolRules()...)
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

// ResolveProtocol returns the preferred upstream protocol for a model.
func ResolveProtocol(model string, customRules []ProtocolRule) string {
	if set, ok := ResolveProtocolSet(model, customRules); ok {
		return set[0]
	}
	return ProtocolChatCompletions
}

// PickProtocol returns the native protocol matching the inbound protocol when
// the model supports it, and otherwise the first supported protocol.
func PickProtocol(set []string, inbound string) string {
	if len(set) == 0 {
		return ProtocolChatCompletions
	}
	inbound = normalizeProtocol(inbound)
	if inbound != "" && slices.Contains(set, inbound) {
		return inbound
	}
	return set[0]
}

// ProtocolForEndpoint maps one catalog endpoint to the relay protocol.
func ProtocolForEndpoint(endpoint string) string {
	switch strings.ToLower(strings.TrimSpace(endpoint)) {
	case MessagesPath:
		return ProtocolAnthropic
	case ResponsesPath:
		return ProtocolResponses
	case ChatCompletionsPath:
		return ProtocolChatCompletions
	default:
		return ""
	}
}

func protocolsForEndpoints(endpoints []string) []string {
	order := []string{ResponsesPath, ChatCompletionsPath, MessagesPath}
	set := make([]string, 0, len(order))
	for _, endpoint := range order {
		for _, candidate := range endpoints {
			if strings.EqualFold(strings.TrimSpace(candidate), endpoint) {
				if protocol := ProtocolForEndpoint(endpoint); protocol != "" && !slices.Contains(set, protocol) {
					set = append(set, protocol)
				}
				break
			}
		}
	}
	return set
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

var (
	catalogMu     sync.RWMutex
	catalogRules  []ProtocolRule
	catalogModels []string
)

// UpdateCatalog publishes the latest upstream model catalog to the relay.
// Rules are exact ids so a model that only supports /messages can never be
// routed to /chat/completions.
func UpdateCatalog(models []ModelInfo) {
	rules := make([]ProtocolRule, 0, len(models))
	ids := make([]string, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
		set := protocolsForEndpoints(model.SupportedEndpoints)
		if len(set) == 0 {
			continue
		}
		rules = append(rules, ProtocolRule{
			Pattern:   id,
			Protocol:  set[0],
			Protocols: set,
		})
	}
	catalogMu.Lock()
	catalogRules = rules
	catalogModels = ids
	catalogMu.Unlock()
}

// CurrentProtocolRules returns a copy of the cached upstream rules.
func CurrentProtocolRules() []ProtocolRule {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	return append([]ProtocolRule(nil), catalogRules...)
}

// CurrentModelList returns a copy of the cached upstream model ids.
func CurrentModelList() []string {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	return append([]string(nil), catalogModels...)
}
