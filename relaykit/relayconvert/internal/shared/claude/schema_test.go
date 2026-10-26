package claude

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFunctionParametersToInputSchemaKeepsTheClientBytesWithoutNullableUnions(t *testing.T) {
	parameters := []byte(`{"type":"object","properties":{"b":{"type":"string"},"a":{"type":"integer"}}}`)

	schema, ok := FunctionParametersToInputSchema(parameters)

	require.True(t, ok)
	assert.Equal(t, string(parameters), string(schema))
}

func TestFunctionParametersToInputSchemaRewritesOnlyNullableUnions(t *testing.T) {
	parameters := []byte(`{"type":"object","properties":{
		"plain":{"anyOf":[{"type":"string"},{"type":"integer"}]},
		"value":{"anyOf":[{"type":"string"},{"type":"null"}]},
		"items":{"type":"array","items":{"anyOf":[{"type":"number"},{"type":"null"}]}}
	}}`)

	schema, ok := FunctionParametersToInputSchema(parameters)

	require.True(t, ok)
	var decoded map[string]any
	require.NoError(t, kitutil.Unmarshal(schema, &decoded))
	properties, isMap := decoded["properties"].(map[string]any)
	require.True(t, isMap)
	// A union that is not the nullable form keeps the combinator: this rewrite
	// only removes the form upstream validators reject, it does not flatten.
	assert.Equal(t, map[string]any{
		"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}},
	}, properties["plain"])
	assert.Equal(t, map[string]any{"type": []any{"string", "null"}}, properties["value"])
	items, isMap := properties["items"].(map[string]any)
	require.True(t, isMap)
	assert.Equal(t, map[string]any{"type": []any{"number", "null"}}, items["items"])
}
