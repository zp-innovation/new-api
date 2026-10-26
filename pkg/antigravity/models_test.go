package antigravity

import "testing"

func TestDefaultModelMappingContainsNewUpstreamModels(t *testing.T) {
	want := map[string]string{
		"claude-opus-5-5":          "claude-opus-5-5-high",
		"claude-opus-5-5-high":     "claude-opus-5-5-high",
		"claude-opus-5-5-medium":   "claude-opus-5-5-medium",
		"claude-opus-5-5-low":      "claude-opus-5-5-low",
		"claude-sonnet-5-5":        "claude-sonnet-5-5-high",
		"claude-sonnet-5-5-high":   "claude-sonnet-5-5-high",
		"claude-sonnet-5-5-medium": "claude-sonnet-5-5-medium",
		"claude-sonnet-5-5-low":    "claude-sonnet-5-5-low",
		"gemini-4-argon":           "gemini-4-argon-high",
		"gemini-4-argon-high":      "gemini-4-argon-high",
		"gemini-4-argon-low":       "gemini-4-argon-low",
		"gemini-4-argon-medium":    "gemini-4-argon-medium",
	}
	for model, wantUpstream := range want {
		got, ok := DefaultModelMapping[model]
		if !ok || got != wantUpstream {
			t.Errorf("DefaultModelMapping[%q] = %q, %v; want %q, true", model, got, ok, wantUpstream)
		}
	}
}

func TestResolveThinkingVariant(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		level     string
		wantModel string
		wantOK    bool
	}{
		{name: "preferred medium", model: "gemini-3.8-flash", level: "medium", wantModel: "gemini-3.8-flash-medium", wantOK: true},
		{name: "preferred low", model: "gemini-3.7-flash", level: "low", wantModel: "gemini-3.7-flash-low", wantOK: true},
		{name: "empty level falls back to high", model: "gemini-3.8-flash", level: "", wantModel: "gemini-3.8-flash-high", wantOK: true},
		{name: "unknown level falls back to high", model: "gemini-3.8-flash", level: "bogus", wantModel: "gemini-3.8-flash-high", wantOK: true},
		{name: "models prefix is stripped", model: "models/gemini-3.6-flash", level: "medium", wantModel: "gemini-3.6-flash-medium", wantOK: true},
		{name: "already suffixed is untouched", model: "gemini-3.8-flash-high", level: "low"},
		{name: "bare model without variants is untouched", model: "gemini-2.5-flash", level: "high"},
		{name: "explicit alias is untouched", model: "gemini-3.1-pro", level: "high"},
		{name: "non gemini model is untouched", model: "claude-opus-4-6", level: "high"},
		{name: "unknown model is untouched", model: "operator-custom-model", level: "high"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveThinkingVariant(tc.model, tc.level)
			if ok != tc.wantOK || got != tc.wantModel {
				t.Fatalf("ResolveThinkingVariant(%q, %q) = %q, %v; want %q, %v",
					tc.model, tc.level, got, ok, tc.wantModel, tc.wantOK)
			}
		})
	}
}

func TestHasThinkingVariantSuffix(t *testing.T) {
	for _, model := range []string{"gemini-3.8-flash-high", "gemini-3.8-flash-medium", "gemini-3.8-flash-low", "gemini-3.8-flash-tiered", "GEMINI-3.8-FLASH-HIGH"} {
		if !HasThinkingVariantSuffix(model) {
			t.Errorf("HasThinkingVariantSuffix(%q) = false, want true", model)
		}
	}
	for _, model := range []string{"gemini-3.8-flash", "gemini-2.5-pro", "claude-opus-4-6-thinking"} {
		if HasThinkingVariantSuffix(model) {
			t.Errorf("HasThinkingVariantSuffix(%q) = true, want false", model)
		}
	}
}
