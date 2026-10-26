package antigravity

import (
	"encoding/json"
	"testing"
)

func TestWrapRequestCarriesEnvelopeFields(t *testing.T) {
	inner := map[string]any{"contents": []any{}}
	envelope, err := WrapRequest("proj-1", "gemini-3-pro-high", inner)
	if err != nil {
		t.Fatalf("WrapRequest: %v", err)
	}
	if envelope.Project != "proj-1" {
		t.Errorf("Project = %q, want proj-1", envelope.Project)
	}
	if envelope.Model != "gemini-3-pro-high" {
		t.Errorf("Model = %q, want gemini-3-pro-high", envelope.Model)
	}
	if envelope.RequestID == "" {
		t.Error("RequestID is empty")
	}
	if envelope.UserAgent != "antigravity" {
		t.Errorf("UserAgent = %q, want antigravity", envelope.UserAgent)
	}

	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	for _, key := range []string{"project", "requestId", "userAgent", "model", "request"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("envelope JSON is missing %q", key)
		}
	}
}

func TestUnwrapResponseMergesEnvelopeMetadata(t *testing.T) {
	body := []byte(`{"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7}},"responseId":"resp-1","modelVersion":"gemini-3-pro-high-001"}`)

	unwrapped, err := UnwrapResponse(body)
	if err != nil {
		t.Fatalf("UnwrapResponse: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(unwrapped, &decoded); err != nil {
		t.Fatalf("unmarshal unwrapped body: %v", err)
	}
	// The host handlers read responseId and modelVersion from the top level of
	// the payload, so both must survive the unwrap.
	if _, ok := decoded["candidates"]; !ok {
		t.Error("unwrapped body lost candidates")
	}
	if _, ok := decoded["usageMetadata"]; !ok {
		t.Error("unwrapped body lost usageMetadata")
	}
	var responseID, modelVersion string
	if err := json.Unmarshal(decoded["responseId"], &responseID); err != nil || responseID != "resp-1" {
		t.Errorf("responseId = %q (err %v), want resp-1", responseID, err)
	}
	if err := json.Unmarshal(decoded["modelVersion"], &modelVersion); err != nil || modelVersion != "gemini-3-pro-high-001" {
		t.Errorf("modelVersion = %q (err %v), want gemini-3-pro-high-001", modelVersion, err)
	}
}

func TestUnwrapResponseKeepsInnerMetadata(t *testing.T) {
	// An inner responseId must win over the envelope value.
	body := []byte(`{"response":{"responseId":"inner"},"responseId":"outer"}`)

	unwrapped, err := UnwrapResponse(body)
	if err != nil {
		t.Fatalf("UnwrapResponse: %v", err)
	}
	var decoded struct {
		ResponseID string `json:"responseId"`
	}
	if err := json.Unmarshal(unwrapped, &decoded); err != nil {
		t.Fatalf("unmarshal unwrapped body: %v", err)
	}
	if decoded.ResponseID != "inner" {
		t.Fatalf("responseId = %q, want inner", decoded.ResponseID)
	}
}

func TestUnwrapResponsePassesThroughDirectGemini(t *testing.T) {
	body := []byte(`{"candidates":[{"finishReason":"STOP"}]}`)

	unwrapped, err := UnwrapResponse(body)
	if err != nil {
		t.Fatalf("UnwrapResponse: %v", err)
	}
	if string(unwrapped) != string(body) {
		t.Fatalf("direct payload was rewritten: %s", unwrapped)
	}
}

func TestUnwrapResponsePassesThroughNonObject(t *testing.T) {
	body := []byte("not json at all")
	unwrapped, err := UnwrapResponse(body)
	if err != nil {
		t.Fatalf("UnwrapResponse: %v", err)
	}
	if string(unwrapped) != string(body) {
		t.Fatalf("non-object payload was rewritten: %s", unwrapped)
	}
}

func TestUnwrapResponseRejectsMalformedEnvelope(t *testing.T) {
	if _, err := UnwrapResponse([]byte(`{"response":`)); err == nil {
		t.Fatal("malformed envelope was accepted")
	}
}
