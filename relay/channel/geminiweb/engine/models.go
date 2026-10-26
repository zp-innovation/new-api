package engine

import "strings"

// Gemini Web identifies models on the wire by 16-hex ids taken from the
// batchexecute?rpcids=otAQ7b catalogue the web client loads at startup.
const (
	hexFlash36   = "fbb127bbb056c959" // 3.6 Flash
	hexFlashLite = "cf41b0e0dd7d53e5" // 3.5 Flash-Lite
	hexPro31     = "9d8ca3786ebdfbea" // 3.1 Pro
	hexFlash38   = "56fdd199312815e2" // 3.8 Flash (Google renamed the 3.7 hex in place)
	hexArgon4    = "95a4c0abcba24364" // 4 Argon
)

// Thinking tiers occupy slot 80 of the inner payload and index 15 of the
// model header.
const (
	thinkingNormal   = 1
	thinkingExtended = 2
	thinkingMedium   = 4
)

// innerSlots is the length of the inner payload array. The web client sends
// 97-98 slots; slot 80 (thinking tier) must exist or it cannot be set.
const innerSlots = 97

// ModelConfig is the server-side identity of one exposed model.
type ModelConfig struct {
	HexID string
	// Mode is the legacy MODE_CATEGORY value: identical to inner[79] and to
	// index 14 of the x-goog-ext-525001261-jspb header. It is the real
	// selector for 4 Argon, which ignores the hex id.
	Mode int
	// Thinking fills slot 80 with the extended tier.
	Thinking bool
	// ThinkWire overrides the thinking slot when > 0.
	ThinkWire int
	Desc      string
}

func (mc ModelConfig) thinkWire() int {
	if mc.ThinkWire > 0 {
		return mc.ThinkWire
	}
	if mc.Thinking {
		return thinkingExtended
	}
	return thinkingNormal
}

// needsLogin reports whether the model only behaves as advertised with a
// signed-in Google account. Anonymous requests are silently downgraded.
func (mc ModelConfig) NeedsLogin() bool {
	return mc.HexID == hexPro31 || mc.HexID == hexFlash38 || mc.HexID == hexArgon4 ||
		mc.Thinking || mc.ThinkWire > 0
}

// Models is the catalogue of text models this engine can drive.
var Models = map[string]ModelConfig{
	"gemini-3.6-flash":      {HexID: hexFlash36, Mode: 1, Desc: "Latest all-around model"},
	"gemini-3.5-flash-lite": {HexID: hexFlashLite, Mode: 6, Desc: "Fastest, lightweight"},
	"gemini-3.1-pro":        {HexID: hexPro31, Mode: 3, Desc: "Most capable; needs a signed-in cookie"},

	"gemini-3.8-flash": {HexID: hexFlash38, Mode: 1, Desc: "3.8 Flash; needs a signed-in paid account"},
	"gemini-3.7-flash": {HexID: hexFlash38, Mode: 1, Desc: "Alias of gemini-3.8-flash (same hex id)"},
	"gemini-4-argon":   {HexID: hexArgon4, Mode: 8, Desc: "4 Argon; needs a signed-in paid/Ultra account"},

	"gemini-3.6-flash-thinking":      {HexID: hexFlash36, Mode: 1, Thinking: true, Desc: "3.6 Flash with extended thinking"},
	"gemini-3.5-flash-lite-thinking": {HexID: hexFlashLite, Mode: 6, Thinking: true, Desc: "3.5 Flash-Lite with extended thinking"},
	"gemini-3.1-pro-thinking":        {HexID: hexPro31, Mode: 3, Thinking: true, Desc: "3.1 Pro with extended thinking"},
	"gemini-3.8-flash-thinking":      {HexID: hexFlash38, Mode: 1, Thinking: true, Desc: "3.8 Flash with extended thinking"},
	"gemini-3.7-flash-thinking":      {HexID: hexFlash38, Mode: 1, Thinking: true, Desc: "Alias of gemini-3.8-flash-thinking"},
	"gemini-4-argon-thinking":        {HexID: hexArgon4, Mode: 8, Thinking: true, Desc: "4 Argon with extended thinking"},

	"gemini-4-argon-thinking-medium": {HexID: hexArgon4, Mode: 8, ThinkWire: thinkingMedium, Desc: "4 Argon at the medium thinking tier"},
}

// ModelNames lists every text model, sorted for stable output.
func ModelNames() []string {
	names := make([]string, 0, len(Models))
	for name := range Models {
		names = append(names, name)
	}
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// PublicModelNames lists the models available without a signed-in cookie.
func PublicModelNames() []string {
	names := make([]string, 0, len(Models))
	for name, mc := range Models {
		if mc.NeedsLogin() {
			continue
		}
		names = append(names, name)
	}
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// ResolveModel maps a requested model name to its config.
func ResolveModel(name string) (string, ModelConfig, bool) {
	if idx := strings.Index(name, "@think="); idx >= 0 {
		name = name[:idx]
	}
	mc, ok := Models[name]
	return name, mc, ok
}
