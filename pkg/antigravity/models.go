package antigravity

import (
	"slices"
	"strings"
)

// AgentModel is the upstream id of the Gemini 3.1 Pro agent variant.
const AgentModel = "gemini-pro-agent"

// DefaultModelMapping maps operator-facing model names to the upstream model
// ids Antigravity accepts. It is only consulted when a channel has no explicit
// model mapping configured.
var DefaultModelMapping = map[string]string{
	// Claude family.
	"claude-fable-5-1":           "claude-fable-5-1",
	"claude-fable-5":             "claude-fable-5",
	"claude-opus-5-5":            "claude-opus-5-5-high",
	"claude-opus-5-5-high":       "claude-opus-5-5-high",
	"claude-opus-5-5-medium":     "claude-opus-5-5-medium",
	"claude-opus-5-5-low":        "claude-opus-5-5-low",
	"claude-sonnet-5-5":          "claude-sonnet-5-5-high",
	"claude-sonnet-5-5-high":     "claude-sonnet-5-5-high",
	"claude-sonnet-5-5-medium":   "claude-sonnet-5-5-medium",
	"claude-sonnet-5-5-low":      "claude-sonnet-5-5-low",
	"claude-opus-4-8":            "claude-opus-4-8",
	"claude-opus-4-7":            "claude-opus-4-7",
	"claude-opus-4-6-thinking":   "claude-opus-4-6-thinking",
	"claude-opus-4-6":            "claude-opus-4-6-thinking",
	"claude-opus-4-5-thinking":   "claude-opus-4-6-thinking",
	"claude-sonnet-4-6":          "claude-sonnet-4-6",
	"claude-sonnet-4-5":          "claude-sonnet-4-5",
	"claude-sonnet-4-5-thinking": "claude-sonnet-4-6",
	"claude-opus-4-5-20251101":   "claude-opus-4-6-thinking",
	"claude-sonnet-4-5-20250929": "claude-sonnet-4-6",
	// Antigravity has no Haiku tier.
	"claude-haiku-4-5":          "claude-sonnet-4-6",
	"claude-haiku-4-5-20251001": "claude-sonnet-4-6",

	// Gemini 2.5 family.
	"gemini-2.5-flash":               "gemini-2.5-flash",
	"gemini-2.5-flash-image":         "gemini-2.5-flash-image",
	"gemini-2.5-flash-image-preview": "gemini-2.5-flash-image",
	"gemini-2.5-flash-lite":          "gemini-2.5-flash-lite",
	"gemini-2.5-flash-thinking":      "gemini-2.5-flash-thinking",
	"gemini-2.5-pro":                 "gemini-2.5-pro",

	// Gemini 3 family.
	"gemini-3-flash":         "gemini-3-flash",
	"gemini-3-flash-preview": "gemini-3-flash",
	"gemini-3-pro-high":      "gemini-3-pro-high",
	"gemini-3-pro-low":       "gemini-3-pro-low",
	"gemini-3-pro-preview":   "gemini-3-pro-high",

	// Gemini 3.1 family.
	"gemini-3.1-pro":                 AgentModel,
	"gemini-3.1-pro-high":            AgentModel,
	"gemini-3.1-pro-preview":         AgentModel,
	"gemini-3.1-pro-low":             "gemini-3.1-pro-low",
	"gemini-3.1-flash-image":         "gemini-3.1-flash-image",
	"gemini-3.1-flash-image-preview": "gemini-3.1-flash-image",

	// Gemini 3.6 / 3.7 / 3.8 flash tiers.
	"gemini-3.6-flash":        "gemini-3.6-flash",
	"gemini-3.6-flash-high":   "gemini-3.6-flash-high",
	"gemini-3.6-flash-low":    "gemini-3.6-flash-low",
	"gemini-3.6-flash-medium": "gemini-3.6-flash-medium",
	"gemini-3.6-flash-tiered": "gemini-3.6-flash-tiered",
	"gemini-3.7-flash":        "gemini-3.7-flash",
	"gemini-3.7-flash-high":   "gemini-3.7-flash-high",
	"gemini-3.7-flash-low":    "gemini-3.7-flash-low",
	"gemini-3.7-flash-medium": "gemini-3.7-flash-medium",
	"gemini-3.7-flash-tiered": "gemini-3.7-flash-tiered",
	"gemini-3.8-flash":        "gemini-3.8-flash",
	"gemini-3.8-flash-high":   "gemini-3.8-flash-high",
	"gemini-3.8-flash-low":    "gemini-3.8-flash-low",
	"gemini-3.8-flash-medium": "gemini-3.8-flash-medium",
	"gemini-3.8-flash-tiered": "gemini-3.8-flash-tiered",

	// Gemini 4 Argon tiered models.
	"gemini-4-argon":        "gemini-4-argon-high",
	"gemini-4-argon-high":   "gemini-4-argon-high",
	"gemini-4-argon-low":    "gemini-4-argon-low",
	"gemini-4-argon-medium": "gemini-4-argon-medium",

	// Gemini 3 image compatibility shims.
	"gemini-3-pro-image":         "gemini-3.1-flash-image",
	"gemini-3-pro-image-preview": "gemini-3.1-flash-image",

	// Non-Gemini models exposed by the same endpoint.
	"gpt-oss-120b-medium":    "gpt-oss-120b-medium",
	"tab_flash_lite_preview": "tab_flash_lite_preview",
}

// ModelList is the sorted set of model names the channel advertises. It is
// derived from DefaultModelMapping so the two cannot drift apart.
var ModelList = func() []string {
	names := make([]string, 0, len(DefaultModelMapping))
	for name := range DefaultModelMapping {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}()

// MapModel resolves a requested model name to the upstream id, reporting
// whether the default mapping knows the model.
func MapModel(requested string) (string, bool) {
	mapped, ok := DefaultModelMapping[strings.TrimSpace(requested)]
	return mapped, ok
}

// thinkingVariantSuffixes is the fallback order used when a bare Gemini model
// name has to be resolved to one of its thinking-depth variants. Antigravity's
// upstream catalog only serves the suffixed ids, so forwarding a bare name
// such as gemini-3.8-flash returns 404.
var thinkingVariantSuffixes = []string{"high", "medium", "low", "tiered"}

// HasThinkingVariantSuffix reports whether the model name already carries a
// thinking-depth suffix such as -high or -tiered.
func HasThinkingVariantSuffix(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, suffix := range thinkingVariantSuffixes {
		if strings.HasSuffix(model, "-"+suffix) {
			return true
		}
	}
	return false
}

// ResolveThinkingVariant picks the upstream id for a bare Gemini model name.
// It only rewrites names the default table passes through unchanged and for
// which the table defines at least one suffix variant, so explicit aliases
// (gemini-3.1-pro -> gemini-pro-agent) and real bare models such as
// gemini-2.5-flash stay untouched. The preferred depth is tried first, then
// high/medium/low/tiered. It reports false when nothing matched, which is the
// signal to keep the caller's model name.
func ResolveThinkingVariant(requested, preferredLevel string) (string, bool) {
	model := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(requested), "models/"))
	if model == "" || !strings.HasPrefix(model, "gemini-") || HasThinkingVariantSuffix(model) {
		return "", false
	}
	mapped, known := DefaultModelMapping[model]
	if !known || mapped != model {
		return "", false
	}
	// Try the requested depth first, then the remaining depths in a stable
	// order so a model that only offers a subset still resolves.
	order := make([]string, 0, len(thinkingVariantSuffixes))
	preferredLevel = strings.ToLower(strings.TrimSpace(preferredLevel))
	if slices.Contains(thinkingVariantSuffixes, preferredLevel) {
		order = append(order, preferredLevel)
	}
	for _, level := range thinkingVariantSuffixes {
		if level != preferredLevel {
			order = append(order, level)
		}
	}
	for _, level := range order {
		candidate := model + "-" + level
		if variant, ok := DefaultModelMapping[candidate]; ok && strings.TrimSpace(variant) != "" {
			return variant, true
		}
	}
	return "", false
}
