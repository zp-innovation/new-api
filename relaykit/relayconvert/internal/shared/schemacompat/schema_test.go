package schemacompat

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeSchema(t *testing.T, document string) any {
	t.Helper()
	var decoded any
	require.NoError(t, kitutil.UnmarshalJsonStr(document, &decoded))
	return decoded
}

// flatten runs ForUpstream with the flattening the Gemini tool encoder requests
// and returns the rewritten document as a decoded map.
func flatten(t *testing.T, document string) map[string]any {
	t.Helper()
	rewritten, changed := ForUpstream(decodeSchema(t, document), true)
	require.True(t, changed)
	encoded, err := kitutil.Marshal(rewritten)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, kitutil.Unmarshal(encoded, &decoded))
	return decoded
}

func TestFlattenKeepsAnEnumUnionAndTheKeywordsTheBranchesAgreeOn(t *testing.T) {
	flattened := flatten(t, `{"anyOf":[
		{"type":"string","enum":["a","b"],"pattern":"^[ab]$"},
		{"type":"string","enum":["c"],"pattern":"^[ab]$"}
	]}`)
	assert.Equal(t, map[string]any{
		"type":    "string",
		"enum":    []any{"a", "b", "c"},
		"pattern": "^[ab]$",
	}, flattened)
}

func TestFlattenUnionsTheBranchTypes(t *testing.T) {
	flattened := flatten(t, `{"anyOf":[{"type":"string"},{"type":"number"}]}`)
	assert.Equal(t, map[string]any{"type": []any{"string", "number"}}, flattened)
}

func TestFlattenTurnsBranchConstsIntoAnEnumUnion(t *testing.T) {
	flattened := flatten(t, `{"anyOf":[{"const":"a"},{"const":"b"}]}`)
	assert.Equal(t, map[string]any{"enum": []any{"a", "b"}}, flattened)
}

func TestFlattenKeepsAnAgreedRequiredListAndDropsADisagreedOne(t *testing.T) {
	agreed := flatten(t, `{"anyOf":[
		{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]},
		{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"}},"required":["id"]}
	]}`)
	assert.Equal(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":   map[string]any{"type": "string"},
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"id"},
	}, agreed)

	disagreed := flatten(t, `{"anyOf":[
		{"type":"object","properties":{"kind":{"const":"a"},"value":{"type":"string"}},"required":["kind","value"]},
		{"type":"object","properties":{"kind":{"const":"b"},"count":{"type":"integer"}},"required":["kind","count"]}
	]}`)
	assert.Equal(t, "object", disagreed["type"])
	assert.NotContains(t, disagreed, "required")
	assert.Equal(t, map[string]any{
		"kind":  map[string]any{"const": "a"},
		"value": map[string]any{"type": "string"},
		"count": map[string]any{"type": "integer"},
	}, disagreed["properties"])
}

func TestNullableUnionCollapsesToATypeArray(t *testing.T) {
	rewritten, changed := ForUpstream(decodeSchema(t, `{"properties":{"value":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`), false)
	require.True(t, changed)
	encoded, err := kitutil.Marshal(rewritten)
	require.NoError(t, err)
	assert.Equal(t, `{"properties":{"value":{"type":["string","null"]}}}`, string(encoded))
}

func TestCombinatorsSurviveWhenFlatteningIsNotRequested(t *testing.T) {
	rewritten, changed := ForUpstream(decodeSchema(t, `{"anyOf":[{"type":"string"},{"type":"number"}]}`), false)
	assert.False(t, changed)
	encoded, err := kitutil.Marshal(rewritten)
	require.NoError(t, err)
	assert.Equal(t, `{"anyOf":[{"type":"string"},{"type":"number"}]}`, string(encoded))
}
