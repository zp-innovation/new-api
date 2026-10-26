package antigravity

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// v1internalEnvelope wraps a Gemini generateContent response the way the
// Antigravity backend does, so the adaptor must unwrap it before the Gemini
// handlers run.
const v1internalEnvelope = `{"response":{"candidates":[{"content":{"role":"model",` +
	`"parts":[{"text":"hello from antigravity"}]},"finishReason":"STOP"}],` +
	`"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":5,"totalTokenCount":16}},` +
	`"responseId":"resp-1","modelVersion":"gemini-3-pro-high"}`

func newDoResponseContext(t *testing.T, stream bool) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if stream {
		c.Request.Header.Set("Accept", "text/event-stream")
	}
	return c, recorder
}

func newEnvelopeResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestDoResponseUnwrapsEnvelopeIntoOpenAIResponse proves the adaptor feeds the
// upstream Gemini handler a plain Gemini payload: the client must receive a
// normal OpenAI chat completion carrying the unwrapped text and usage.
func TestDoResponseUnwrapsEnvelopeIntoOpenAIResponse(t *testing.T) {
	c, recorder := newDoResponseContext(t, false)
	info := newRelayInfo("", "gemini-3-pro-high")
	info.RelayFormat = types.RelayFormatOpenAI

	adaptor := &Adaptor{}
	usage, apiErr := adaptor.DoResponse(c, newEnvelopeResponse(v1internalEnvelope), info)
	if apiErr != nil {
		t.Fatalf("DoResponse returned an error: %v", apiErr)
	}
	if usage == nil {
		t.Fatal("DoResponse returned no usage")
	}

	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode client response %q: %v", recorder.Body.String(), err)
	}
	if len(payload.Choices) == 0 || payload.Choices[0].Message.Content != "hello from antigravity" {
		t.Fatalf("client response lost the unwrapped text: %s", recorder.Body.String())
	}
	if payload.Usage.PromptTokens != 11 || payload.Usage.CompletionTokens != 5 {
		t.Fatalf("client usage = %+v, want prompt=11 completion=5", payload.Usage)
	}
}

// TestDoResponseUnwrapsEnvelopeIntoClaudeResponse covers the Anthropic entry:
// the same envelope must reach the client as a Claude Messages response.
func TestDoResponseUnwrapsEnvelopeIntoClaudeResponse(t *testing.T) {
	c, recorder := newDoResponseContext(t, false)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := newRelayInfo("", "claude-sonnet-4-6")
	info.RelayFormat = types.RelayFormatClaude

	adaptor := &Adaptor{}
	if _, apiErr := adaptor.DoResponse(c, newEnvelopeResponse(v1internalEnvelope), info); apiErr != nil {
		t.Fatalf("DoResponse returned an error: %v", apiErr)
	}

	var payload struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode claude response %q: %v", recorder.Body.String(), err)
	}
	if payload.Type != "message" {
		t.Fatalf("claude response type = %q, want message: %s", payload.Type, recorder.Body.String())
	}
	if len(payload.Content) == 0 || payload.Content[0].Text != "hello from antigravity" {
		t.Fatalf("claude response lost the text: %s", recorder.Body.String())
	}
	if payload.Usage.InputTokens != 11 || payload.Usage.OutputTokens != 5 {
		t.Fatalf("claude usage = %+v, want input=11 output=5", payload.Usage)
	}
}

// TestDoResponseStreamUnwrapsSSE covers the streaming path: each SSE frame is
// enveloped, and the adaptor must rewrite them so the Gemini stream handler can
// parse the deltas.
func TestDoResponseStreamUnwrapsSSE(t *testing.T) {
	// StreamScannerHandler builds a ticker from the configured streaming
	// timeout, which is zero in a bare test process.
	previousStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 60
	t.Cleanup(func() { constant.StreamingTimeout = previousStreamingTimeout })

	c, recorder := newDoResponseContext(t, true)
	info := newRelayInfo("", "gemini-3-pro-high")
	info.RelayFormat = types.RelayFormatOpenAI
	info.IsStream = true

	frame := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"chunk"}]}}]},"modelVersion":"gemini-3-pro-high"}` + "\n\n" +
		`data: {"response":{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}],` +
		`"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}}` + "\n\n" +
		"data: [DONE]\n\n"

	adaptor := &Adaptor{}
	usage, apiErr := adaptor.DoResponse(c, newEnvelopeResponse(frame), info)
	if apiErr != nil {
		t.Fatalf("DoResponse returned an error: %v", apiErr)
	}
	if usage == nil {
		t.Fatal("DoResponse returned no usage")
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "chunk") {
		t.Fatalf("streamed client response lost the delta: %s", body)
	}
	if strings.Contains(body, `"response":{`) {
		t.Fatalf("streamed client response still contains the raw envelope: %s", body)
	}
}

// TestGetRequestURLUsesChannelBaseURL pins that a custom channel base URL is
// honoured, so operators can point the channel at a mirror.
func TestGetRequestURLUsesChannelBaseURL(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("", "gemini-3-pro-high")
	info.ChannelBaseUrl = "https://mirror.example.com"
	info.ChannelType = constant.ChannelTypeAntigravity

	url, err := adaptor.GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL: %v", err)
	}
	want := "https://mirror.example.com/v1internal:generateContent"
	if url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}
}
