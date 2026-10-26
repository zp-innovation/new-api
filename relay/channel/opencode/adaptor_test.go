package opencode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func newRelayInfo(apiKey, model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            apiKey,
			UpstreamModelName: model,
		},
	}
}

func newTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	context.Request.Header.Set("Content-Type", "application/json")
	return context
}

func TestGetRequestURLRoutesByModeAndModel(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		model   string
		wantURL string
	}{
		{
			name:    "zen responses",
			apiKey:  `{"type":"opencode","mode":"zen","access_token":"access"}`,
			model:   "gpt-5.6-luna",
			wantURL: "https://opencode.ai/zen/v1/responses",
		},
		{
			name:    "go anthropic",
			apiKey:  `{"type":"opencode","mode":"go","access_token":"access"}`,
			model:   "minimax-m3",
			wantURL: "https://opencode.ai/zen/go/v1/messages",
		},
		{
			name:    "zen chat",
			apiKey:  `{"type":"opencode","mode":"zen","access_token":"access"}`,
			model:   "glm-5.3",
			wantURL: "https://opencode.ai/zen/v1/chat/completions",
		},
		{
			name:    "go chat",
			apiKey:  `{"type":"opencode","mode":"go","access_token":"access"}`,
			model:   "glm-5.3",
			wantURL: "https://opencode.ai/zen/go/v1/chat/completions",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adaptor := &Adaptor{}
			requestURL, err := adaptor.GetRequestURL(newRelayInfo(test.apiKey, test.model))
			require.NoError(t, err)
			assert.Equal(t, test.wantURL, requestURL)
		})
	}
}

func TestGetRequestURLRejectsDedicatedModels(t *testing.T) {
	adaptor := &Adaptor{}
	_, err := adaptor.GetRequestURL(newRelayInfo(
		`{"type":"opencode","mode":"zen","access_token":"access"}`,
		"gemini-3-pro",
	))
	require.Error(t, err)
}

func TestGetRequestURLAcceptsRawAPIKey(t *testing.T) {
	adaptor := &Adaptor{}
	requestURL, err := adaptor.GetRequestURL(newRelayInfo("oc_sk_test", "glm-5.3"))
	require.NoError(t, err)
	assert.Equal(t, "https://opencode.ai/zen/v1/chat/completions", requestURL)
}

func TestGetRequestURLInfersGoModeFromBaseURL(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("oc_sk_test", "minimax-m3")
	info.ChannelBaseUrl = "https://opencode.ai/zen/go/v1"
	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://opencode.ai/zen/go/v1/messages", requestURL)
}

func TestGetRequestURLUsesCredentialProtocolRules(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo(
		`{"type":"opencode","mode":"zen","access_token":"access","protocol_rules":[{"pattern":"custom-*","protocol":"responses"}]}`,
		"custom-model",
	)
	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://opencode.ai/zen/v1/responses", requestURL)
}

func TestSetupRequestHeaderUsesRawAPIKey(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("oc_sk_test", "glm-5.3")
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(newTestContext(t), &header, info))
	assert.Equal(t, "Bearer oc_sk_test", header.Get("Authorization"))
	assert.Empty(t, header.Get("x-api-key"))
}

func TestSetupRequestHeaderUsesBearerForNonAnthropic(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo(`{"type":"opencode","mode":"zen","access_token":"access"}`, "glm-5.3")
	context := newTestContext(t)
	context.Request.Header.Set(openCodeSessionHeader, "session-1")
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(context, &header, info))
	assert.Equal(t, "Bearer access", header.Get("Authorization"))
	assert.Empty(t, header.Get("x-api-key"))
	assert.Equal(t, "opencode/1.0.0", header.Get("User-Agent"))
	assert.Equal(t, "session-1", header.Get(openCodeSessionHeader))
}

func TestSetupRequestHeaderUsesApiKeyForAnthropic(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo(`{"type":"opencode","mode":"go","access_token":"access"}`, "minimax-m3")
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(newTestContext(t), &header, info))
	assert.Equal(t, "access", header.Get("x-api-key"))
	assert.Empty(t, header.Get("Authorization"))
}

func TestSessionMetadataOnlyCarriesScalarStringIDs(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
		wantID   string
	}{
		{name: "object with session id", metadata: `{"session_id":"session-2"}`, wantID: "session-2"},
		{name: "object with user id", metadata: `{"user_id":"user-3"}`, wantID: "user-3"},
		{name: "object without a session key", metadata: `{"trace_id":"trace-4"}`},
		{name: "array", metadata: `["session-5"]`},
		{name: "nested object under the session key", metadata: `{"session_id":{"nested":"session-6"}}`},
		{name: "scalar string", metadata: `"session-7"`, wantID: "session-7"},
	}
	for _, test := range tests {
		t.Run("chat/"+test.name, func(t *testing.T) {
			adaptor := &Adaptor{}
			info := newRelayInfo(`{"type":"opencode","mode":"zen","access_token":"access"}`, "glm-5.3")
			request := &dto.GeneralOpenAIRequest{Model: "glm-5.3", Metadata: json.RawMessage(test.metadata)}
			_, err := adaptor.ConvertOpenAIRequest(newTestContext(t), info, request)
			require.NoError(t, err)
			assertCapturedSessionID(t, adaptor, info, test.wantID)
		})
		t.Run("responses/"+test.name, func(t *testing.T) {
			adaptor := &Adaptor{}
			info := newRelayInfo(`{"type":"opencode","mode":"zen","access_token":"access"}`, "gpt-5.6-luna")
			request := dto.OpenAIResponsesRequest{Model: "gpt-5.6-luna", Metadata: json.RawMessage(test.metadata)}
			_, err := adaptor.ConvertOpenAIResponsesRequest(newTestContext(t), info, request)
			require.NoError(t, err)
			assertCapturedSessionID(t, adaptor, info, test.wantID)
		})
	}
}

// assertCapturedSessionID checks the session id the adaptor sends upstream. An
// empty wantID means no session id may be derived from metadata, so the adaptor
// has to fall back to a generated id instead of forwarding metadata text.
func assertCapturedSessionID(t *testing.T, adaptor *Adaptor, info *relaycommon.RelayInfo, wantID string) {
	t.Helper()
	header := http.Header{}
	require.NoError(t, adaptor.SetupRequestHeader(newTestContext(t), &header, info))
	sessionID := header.Get(openCodeSessionHeader)
	if wantID == "" {
		_, err := uuid.Parse(sessionID)
		require.NoError(t, err, "session header must not carry structured metadata, got %q", sessionID)
		return
	}
	assert.Equal(t, wantID, sessionID)
}
