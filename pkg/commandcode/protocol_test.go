package commandcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveProtocolSetUsesTheCatalog(t *testing.T) {
	UpdateCatalog([]ModelInfo{
		{ID: "claude-sonnet-4-6", SupportedEndpoints: []string{"/messages"}},
		{ID: "gpt-5.6-luna", SupportedEndpoints: []string{"/chat/completions", "/responses"}},
		{ID: "glm-5.3-flash", SupportedEndpoints: []string{"/chat/completions"}},
	})
	t.Cleanup(func() { UpdateCatalog(nil) })

	set, ok := ResolveProtocolSet("claude-sonnet-4-6", nil)
	require.True(t, ok)
	assert.Equal(t, []string{ProtocolAnthropic}, set)

	set, ok = ResolveProtocolSet("gpt-5.6-luna", nil)
	require.True(t, ok)
	assert.Equal(t, []string{ProtocolResponses, ProtocolChatCompletions}, set)

	set, ok = ResolveProtocolSet("glm-5.3-flash", nil)
	require.True(t, ok)
	assert.Equal(t, []string{ProtocolChatCompletions}, set)
}

func TestResolveProtocolSetPrefersTheCatalogOverThePrefixDefaults(t *testing.T) {
	// UpdateCatalog is the source of truth: a claude-*/gpt-* model the upstream
	// mapped to another endpoint must not be routed by the prefix defaults.
	UpdateCatalog([]ModelInfo{
		{ID: "claude-sonnet-4-6", SupportedEndpoints: []string{"/chat/completions"}},
		{ID: "gpt-5.6-luna", SupportedEndpoints: []string{"/messages"}},
	})
	t.Cleanup(func() { UpdateCatalog(nil) })

	assert.Equal(t, ProtocolChatCompletions, ResolveProtocol("claude-sonnet-4-6", nil))
	assert.Equal(t, ProtocolAnthropic, ResolveProtocol("gpt-5.6-luna", nil))

	// A model the catalog does not list still falls back to the defaults.
	assert.Equal(t, ProtocolResponses, ResolveProtocol("gpt-5.6-sol", nil))
}

func TestPickProtocolPrefersTheInboundProtocol(t *testing.T) {
	set := []string{ProtocolResponses, ProtocolChatCompletions}
	assert.Equal(t, ProtocolChatCompletions, PickProtocol(set, ProtocolChatCompletions))
	assert.Equal(t, ProtocolResponses, PickProtocol(set, ProtocolResponses))
	assert.Equal(t, ProtocolResponses, PickProtocol(set, ProtocolAnthropic))
}

func TestIsUnsupportedModelRejectsSystemOne(t *testing.T) {
	assert.True(t, IsUnsupportedModel("typesafe/jev"))
	assert.True(t, IsUnsupportedModel("jev-1.14.0"))
	assert.False(t, IsUnsupportedModel("gpt-5.6-luna"))
}

func TestUpdateCatalogNormalizesModelIDs(t *testing.T) {
	UpdateCatalog([]ModelInfo{{ID: "claude-opus-4-6", SupportedEndpoints: []string{"/messages"}}})
	t.Cleanup(func() { UpdateCatalog(nil) })
	assert.Contains(t, CurrentModelList(), "claude-opus-4-6")
}
