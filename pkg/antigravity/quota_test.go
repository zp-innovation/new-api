package antigravity

import (
	"slices"
	"testing"
)

func TestModelNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response *FetchAvailableModelsResponse
		want     []string
	}{
		{name: "nil response yields no models", response: nil, want: nil},
		{
			name:     "empty map yields no models",
			response: &FetchAvailableModelsResponse{},
			want:     nil,
		},
		{
			name: "sorts model ids and trims blanks",
			response: &FetchAvailableModelsResponse{Models: map[string]ModelInfo{
				"gemini-3.8-flash-high": {},
				"claude-opus-4-8":       {},
				"   ":                   {},
				" gemini-3-pro-high ":   {},
			}},
			want: []string{"claude-opus-4-8", "gemini-3-pro-high", "gemini-3.8-flash-high"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ModelNames(tc.response)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("ModelNames() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTierIDToPlanTypeNormalizesPaidSubTiers(t *testing.T) {
	tests := map[string]string{
		"free-tier":          "Free",
		"standard-tier":      "Standard",
		"g1-pro-tier":        "Pro",
		"g1-ultra-tier":      "Ultra",
		"g1-pro-plus-tier":   "Pro",
		"g1-ultra-lite-tier": "Ultra",
		" G1-ULTRA-pro-tier": "Ultra",
		"":                   "Free",
		"enterprise-tier":    "enterprise-tier",
	}
	for tierID, want := range tests {
		if got := TierIDToPlanType(tierID); got != want {
			t.Errorf("TierIDToPlanType(%q) = %q, want %q", tierID, got, want)
		}
	}
}
