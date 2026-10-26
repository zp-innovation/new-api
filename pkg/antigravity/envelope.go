package antigravity

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// V1InternalRequest is the Antigravity relay envelope. Request carries a
// standard Gemini generateContent payload.
type V1InternalRequest struct {
	Project     string `json:"project"`
	RequestID   string `json:"requestId,omitempty"`
	UserAgent   string `json:"userAgent,omitempty"`
	RequestType string `json:"requestType,omitempty"`
	Model       string `json:"model"`
	Request     any    `json:"request"`
}

// V1InternalResponse is the Antigravity response envelope.
type V1InternalResponse struct {
	Response     json.RawMessage `json:"response"`
	ResponseID   string          `json:"responseId,omitempty"`
	ModelVersion string          `json:"modelVersion,omitempty"`
}

// WrapRequest builds the relay envelope for one Gemini request.
func WrapRequest(projectID, model string, request any) (*V1InternalRequest, error) {
	requestID, err := GenerateID()
	if err != nil {
		return nil, fmt.Errorf("generate request id: %w", err)
	}
	return &V1InternalRequest{
		Project:   projectID,
		RequestID: requestID,
		UserAgent: "antigravity",
		Model:     model,
		Request:   request,
	}, nil
}

// UnwrapResponse reduces an Antigravity response body to the plain Gemini shape
// the host handlers already understand. Bodies that are not enveloped are
// returned unchanged so a direct Gemini payload keeps working.
//
// The envelope carries responseId and modelVersion next to the inner response;
// both are merged into the inner object because the host reads them from the
// top level of the unwrapped payload.
func UnwrapResponse(body []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return body, nil
	}

	var envelope V1InternalResponse
	if err := unmarshalJSON(trimmed, &envelope); err != nil {
		return nil, fmt.Errorf("decode antigravity envelope: %w", err)
	}
	inner := bytes.TrimSpace(envelope.Response)
	if len(inner) == 0 || string(inner) == "null" {
		return body, nil
	}
	return mergeEnvelopeMetadata(inner, envelope.ResponseID, envelope.ModelVersion)
}

func mergeEnvelopeMetadata(inner json.RawMessage, responseID, modelVersion string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := unmarshalJSON(inner, &fields); err != nil {
		// The inner payload is not an object; nothing to merge into.
		return inner, nil
	}
	if responseID != "" {
		if _, exists := fields["responseId"]; !exists {
			fields["responseId"] = mustEncodeString(responseID)
		}
	}
	if modelVersion != "" {
		if _, exists := fields["modelVersion"]; !exists {
			fields["modelVersion"] = mustEncodeString(modelVersion)
		}
	}
	merged, err := marshalJSON(fields)
	if err != nil {
		return nil, fmt.Errorf("encode unwrapped response: %w", err)
	}
	return merged, nil
}

func mustEncodeString(value string) json.RawMessage {
	encoded, err := marshalJSON(value)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return encoded
}
