package claude

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/schemacompat"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/tidwall/sjson"
)

// FunctionParametersToInputSchema returns function parameters as a Claude
// input_schema, adding type and properties when they are missing or null.
// Otherwise the client's bytes are kept, so the properties keep their order.
// ok is false when parameters are not a JSON object; the result is then an
// empty object schema.
//
// The nullable-union rewrite this fork needs for Anthropic-compatible upstreams
// runs last, and only rebuilds the document when a combinator actually matches,
// so an ordinary schema keeps the client's byte order.
func FunctionParametersToInputSchema(parameters json.RawMessage) (schema json.RawMessage, ok bool) {
	empty := json.RawMessage(`{"type":"object","properties":{}}`)
	switch kitutil.GetJsonType(parameters) {
	case "unknown", "null":
		return empty, true
	case "object":
	default:
		return empty, false
	}
	var fields struct {
		Type       json.RawMessage `json:"type"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := kitutil.Unmarshal(parameters, &fields); err != nil {
		return empty, false
	}
	schema = parameters
	var err error
	if len(fields.Type) == 0 || kitutil.GetJsonType(fields.Type) == "null" {
		if schema, err = sjson.SetRawBytes(schema, "type", []byte(`"object"`)); err != nil {
			return empty, false
		}
	}
	if len(fields.Properties) == 0 || kitutil.GetJsonType(fields.Properties) == "null" {
		if schema, err = sjson.SetRawBytes(schema, "properties", []byte(`{}`)); err != nil {
			return empty, false
		}
	}
	return normalizeNullableUnions(schema), true
}

// normalizeNullableUnions turns the nullable anyOf form upstream validators
// reject into its type-array equivalent, and keeps the original bytes when
// nothing needed rewriting.
//
// Scope: every tool schema that reaches FunctionParametersToInputSchema, which
// is the only caller, so this runs for all Claude-compatible channels. It only
// rewrites an anyOf whose two branches are one typed schema and
// {"type":"null"}; other combinators are left alone because flattening is not
// requested here. The rewrite is unconditional on purpose: whether an upstream
// accepts a type array cannot be verified from this host, so a per-channel
// switch would replace a working default with an unverified risk.
func normalizeNullableUnions(schema json.RawMessage) json.RawMessage {
	var decoded any
	if err := kitutil.Unmarshal(schema, &decoded); err != nil {
		return schema
	}
	normalized, changed := schemacompat.ForUpstream(decoded, false)
	if !changed {
		return schema
	}
	rewritten, err := kitutil.Marshal(normalized)
	if err != nil {
		return schema
	}
	return rewritten
}
