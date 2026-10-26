package antigravity

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/constant"
	agcore "github.com/QuantumNous/new-api/pkg/antigravity"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const validKey = `{"type":"antigravity","access_token":"at-1","refresh_token":"rt-1",` +
	`"project_id":"proj-1","email":"user@example.com"}`

// newRelayInfo builds a RelayInfo carrying the promoted ChannelMeta fields the
// adaptor reads. Promoted fields cannot appear in a composite literal before
// Go 1.27, so the embedded struct is set explicitly.
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
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func TestParseCredential(t *testing.T) {
	key, err := agcore.ParseCredential(validKey)
	if err != nil {
		t.Fatalf("ParseCredential: %v", err)
	}
	if key.AccessToken != "at-1" || key.ProjectID != "proj-1" || key.Email != "user@example.com" {
		t.Fatalf("key = %+v", key)
	}

	rejected := map[string]string{
		"empty":            "",
		"blank":            "   ",
		"invalid json":     "{not json",
		"missing token":    `{"project_id":"proj-1"}`,
		"missing project":  `{"access_token":"at-1"}`,
		"blank token":      `{"access_token":"  ","project_id":"proj-1"}`,
		"blank project id": `{"access_token":"at-1","project_id":"  "}`,
	}
	for name, raw := range rejected {
		if _, err := agcore.ParseCredential(raw); err == nil {
			t.Errorf("%s: ParseCredential accepted %q", name, raw)
		}
	}
}

func TestSetupRequestHeaderUsesBearerAndAntigravityUserAgent(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo(validKey, "gemini-3-pro-high")
	info.IsStream = true
	header := http.Header{}

	if err := adaptor.SetupRequestHeader(newTestContext(t), &header, info); err != nil {
		t.Fatalf("SetupRequestHeader: %v", err)
	}
	if got := header.Get("Authorization"); got != "Bearer at-1" {
		t.Errorf("Authorization = %q, want Bearer at-1", got)
	}
	if got := header.Get("User-Agent"); got != agcore.UserAgent() {
		t.Errorf("User-Agent = %q, want %q", got, agcore.UserAgent())
	}
	// v1internal rejects media types with parameters, so the header is pinned
	// instead of inheriting the client value.
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
}

func TestSetupRequestHeaderRejectsInvalidKey(t *testing.T) {
	adaptor := &Adaptor{}
	header := http.Header{}

	if err := adaptor.SetupRequestHeader(newTestContext(t), &header, newRelayInfo("not-json", "gemini-3-pro-high")); err == nil {
		t.Fatal("SetupRequestHeader accepted an invalid channel key")
	}
}

func TestGetRequestURLSelectsV1InternalAction(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("", "gemini-3-pro-high")
	info.ChannelBaseUrl = agcore.ProdBaseURL
	info.ChannelType = constant.ChannelTypeAntigravity

	streamURL, err := adaptor.GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL: %v", err)
	}
	if want := agcore.ProdBaseURL + "/v1internal:generateContent"; streamURL != want {
		t.Errorf("buffered url = %q, want %q", streamURL, want)
	}

	info.IsStream = true
	streamURL, err = adaptor.GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL: %v", err)
	}
	if want := agcore.ProdBaseURL + "/v1internal:streamGenerateContent?alt=sse"; streamURL != want {
		t.Errorf("stream url = %q, want %q", streamURL, want)
	}
}

func TestWrapUsesCredentialProjectAndDefaultModelMapping(t *testing.T) {
	adaptor := &Adaptor{cred: &agcore.Credential{AccessToken: "at-1", ProjectID: "proj-9"}}

	wrapped, err := adaptor.wrap(newRelayInfo("", "claude-opus-4-6"), &dto.GeminiChatRequest{})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	envelope, ok := wrapped.(*agcore.V1InternalRequest)
	if !ok {
		t.Fatalf("wrap returned %T, want *V1InternalRequest", wrapped)
	}
	if envelope.Project != "proj-9" {
		t.Errorf("project = %q, want proj-9", envelope.Project)
	}
	// The default table rewrites the short Claude name to the upstream id.
	if envelope.Model != "claude-opus-4-6-thinking" {
		t.Errorf("model = %q, want claude-opus-4-6-thinking", envelope.Model)
	}
}

func TestWrapPassesThroughUnknownModel(t *testing.T) {
	adaptor := &Adaptor{cred: &agcore.Credential{AccessToken: "at-1", ProjectID: "proj-9"}}

	wrapped, err := adaptor.wrap(newRelayInfo("", "operator-custom-model"), &dto.GeminiChatRequest{})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	envelope := wrapped.(*agcore.V1InternalRequest)
	if envelope.Model != "operator-custom-model" {
		t.Errorf("model = %q, want the operator value unchanged", envelope.Model)
	}
}

func TestWrapRequiresProjectID(t *testing.T) {
	adaptor := &Adaptor{}

	if _, err := adaptor.wrap(newRelayInfo(`{"access_token":"at-1"}`, "gemini-3-pro-high"), &dto.GeminiChatRequest{}); err == nil {
		t.Fatal("wrap accepted a credential without project_id")
	}
}

func TestCredentialsParsesOnceAndCaches(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo(validKey, "gemini-3-pro-high")

	first, err := adaptor.credentials(info)
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	// A second lookup must reuse the parsed value even if the key changes,
	// because one adaptor instance serves exactly one request.
	info.ChannelMeta.ApiKey = "not-json"
	second, err := adaptor.credentials(info)
	if err != nil {
		t.Fatalf("second credentials: %v", err)
	}
	if first != second {
		t.Fatal("credentials were parsed twice for one request")
	}
}

// TestDefaultChannelBaseURLUsesRelayHost pins the channel type default to the
// host that serves relay traffic. pkg/antigravity reserves the production host
// for management calls; relay requests sent there fail with HTTP 429 for
// free-tier accounts, which silently breaks every channel test and relay call.
func TestDefaultChannelBaseURLUsesRelayHost(t *testing.T) {
	if got := constant.GetChannelBaseURL(constant.ChannelTypeAntigravity); got != agcore.DailyBaseURL {
		t.Fatalf("default base url = %q, want the relay host %q", got, agcore.DailyBaseURL)
	}
}

func TestWrapResolvesBareGeminiThinkingVariant(t *testing.T) {
	adaptor := &Adaptor{cred: &agcore.Credential{AccessToken: "at-1", ProjectID: "proj-9"}}
	request := &dto.GeminiChatRequest{
		GenerationConfig: dto.GeminiChatGenerationConfig{
			ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: "medium"},
		},
	}

	wrapped, err := adaptor.wrap(newRelayInfo("", "gemini-3.8-flash"), request)
	require.NoError(t, err)
	envelope, ok := wrapped.(*agcore.V1InternalRequest)
	require.True(t, ok)
	assert.Equal(t, "gemini-3.8-flash-medium", envelope.Model)
}

func TestWrapResolvesBareGeminiThinkingVariantFromBudget(t *testing.T) {
	adaptor := &Adaptor{cred: &agcore.Credential{AccessToken: "at-1", ProjectID: "proj-9"}}
	budget := 5000
	request := &dto.GeminiChatRequest{
		GenerationConfig: dto.GeminiChatGenerationConfig{
			ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingBudget: &budget},
		},
	}

	wrapped, err := adaptor.wrap(newRelayInfo("", "gemini-3.7-flash"), request)
	require.NoError(t, err)
	envelope := wrapped.(*agcore.V1InternalRequest)
	assert.Equal(t, "gemini-3.7-flash-medium", envelope.Model)
}

func TestWrapKeepsExplicitVariantsAndAliases(t *testing.T) {
	tests := map[string]string{
		"gemini-3.8-flash-low": "gemini-3.8-flash-low",
		"gemini-3.1-pro":       "gemini-pro-agent",
		"gemini-2.5-flash":     "gemini-2.5-flash",
	}
	for model, want := range tests {
		t.Run(model, func(t *testing.T) {
			adaptor := &Adaptor{cred: &agcore.Credential{AccessToken: "at-1", ProjectID: "proj-9"}}
			wrapped, err := adaptor.wrap(newRelayInfo("", model), &dto.GeminiChatRequest{})
			require.NoError(t, err)
			envelope := wrapped.(*agcore.V1InternalRequest)
			assert.Equal(t, want, envelope.Model)
		})
	}
}

func TestWrapImageChannelTestAddsImageRequest(t *testing.T) {
	maxTokens := uint(16)
	adaptor := &Adaptor{cred: &agcore.Credential{AccessToken: "at-1", ProjectID: "proj-9"}}
	info := newRelayInfo(validKey, "gemini-3.1-flash-image")
	info.IsChannelTest = true
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Role: "user", Parts: []dto.GeminiPart{{Text: "hi"}}},
		},
		GenerationConfig: dto.GeminiChatGenerationConfig{MaxOutputTokens: &maxTokens},
	}

	wrapped, err := adaptor.wrap(info, request)
	require.NoError(t, err)
	envelope := wrapped.(*agcore.V1InternalRequest)
	assert.Equal(t, "gemini-3.1-flash-image", envelope.Model)

	inner, ok := envelope.Request.(*dto.GeminiChatRequest)
	require.True(t, ok)
	assert.Equal(t, []string{"TEXT", "IMAGE"}, inner.GenerationConfig.ResponseModalities)
	assert.Nil(t, inner.GenerationConfig.MaxOutputTokens)
	assert.JSONEq(t, `{"aspectRatio":"1:1"}`, string(inner.GenerationConfig.ImageConfig))
	require.Len(t, inner.Contents, 1)
	require.Len(t, inner.Contents[0].Parts, 1)
	assert.Equal(t, defaultImageTestPrompt, inner.Contents[0].Parts[0].Text)
}

func TestWrapImageModelOutsideChannelTestKeepsRequest(t *testing.T) {
	maxTokens := uint(16)
	adaptor := &Adaptor{cred: &agcore.Credential{AccessToken: "at-1", ProjectID: "proj-9"}}
	request := &dto.GeminiChatRequest{
		GenerationConfig: dto.GeminiChatGenerationConfig{MaxOutputTokens: &maxTokens},
	}

	wrapped, err := adaptor.wrap(newRelayInfo(validKey, "gemini-3.1-flash-image"), request)
	require.NoError(t, err)
	inner := wrapped.(*agcore.V1InternalRequest).Request.(*dto.GeminiChatRequest)
	assert.Empty(t, inner.GenerationConfig.ResponseModalities)
	require.NotNil(t, inner.GenerationConfig.MaxOutputTokens)
	assert.Equal(t, uint(16), *inner.GenerationConfig.MaxOutputTokens)
}

func TestDoResponseRetriesEmptyMalformedStream(t *testing.T) {
	adaptor := &Adaptor{cred: &agcore.Credential{AccessToken: "at-1", ProjectID: "proj-9"}}
	info := newRelayInfo(validKey, "gemini-4-argon")
	info.IsStream = true
	body := "data: {\"response\":{\"candidates\":[{\"finishReason\":\"MALFORMED_FUNCTION_CALL\"}]},\"modelVersion\":\"v1\"}\n\n" +
		"data: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := adaptor.DoResponse(newTestContext(t), resp, info)
	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
}

// A caller that disconnects while the stream is still being buffered must abort
// the guard at once instead of leaving the request waiting on the upstream.
func TestDoResponseAbortsAStalledStreamWhenTheCallerDisconnects(t *testing.T) {
	adaptor := &Adaptor{cred: &agcore.Credential{AccessToken: "at-1", ProjectID: "proj-9"}}
	info := newRelayInfo(validKey, "gemini-4-argon")
	info.IsStream = true
	body := newBlockingReadCloser()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}

	c := newTestContext(t)
	ctx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(ctx)

	start := time.Now()
	usage, guardErr := adaptor.DoResponse(c, resp, info)

	assert.Nil(t, usage)
	require.NotNil(t, guardErr)
	assert.ErrorIs(t, guardErr, context.Canceled)
	assert.True(t, body.isClosed())
	assert.Less(t, time.Since(start), time.Second)
}
