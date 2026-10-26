package workbuddy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// channelKey builds a channel key the way the credential import writes it: one
// compact JSON object on a single line.
func channelKey(realm, product string) string {
	return fmt.Sprintf(`{"type":"workbuddy","realm":%q,"product":%q,"uid":"uid-1",`+
		`"access_token":"access-token","refresh_token":"refresh-token",`+
		`"expires_at":"2030-01-01T00:00:00Z"}`, realm, product)
}

// newRelayInfo builds a RelayInfo carrying the promoted ChannelMeta fields the
// adaptor reads. Promoted fields cannot appear in a composite literal before
// Go 1.27, so the embedded struct is set explicitly.
func newRelayInfo(realm, product, baseURL string, stream bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeWorkBuddy,
			ChannelBaseUrl:    baseURL,
			ApiKey:            channelKey(realm, product),
			UpstreamModelName: "deepseek-v4.1-flash",
		},
		OriginModelName: "deepseek-v4.1-flash",
		IsStream:        stream,
	}
}

// thinkingDefaultInfo builds the RelayInfo a WorkBuddy channel carries for a
// given thinking default.
func thinkingDefaultInfo(defaultThinking bool) *relaycommon.RelayInfo {
	info := newRelayInfo("intl", "cli", constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy), false)
	info.ChannelSetting.WorkBuddyDefaultThinking = lo.ToPtr(defaultThinking)
	return info
}

func newTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func TestGetRequestURLUsesTheCredentialRealm(t *testing.T) {
	cases := []struct {
		name    string
		realm   string
		product string
		want    string
	}{
		{name: "cn cli", realm: "cn", product: "cli", want: "https://copilot.tencent.com/v2/chat/completions"},
		{name: "intl cli", realm: "intl", product: "cli", want: "https://www.workbuddy.ai/v2/chat/completions"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			adaptor := &Adaptor{}
			info := newRelayInfo(testCase.realm, testCase.product,
				constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy), false)
			got, err := adaptor.GetRequestURL(info)
			if err != nil {
				t.Fatalf("GetRequestURL() error = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("GetRequestURL() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// An operator who typed an explicit API address overrides the realm-derived
// host, which is how a channel is pointed at an alternate exit.
func TestGetRequestURLHonoursAnOperatorBaseURL(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("cn", "cli", "https://gateway.example.com/", false)
	got, err := adaptor.GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL() error = %v", err)
	}
	if want := "https://gateway.example.com/v2/chat/completions"; got != want {
		t.Fatalf("GetRequestURL() = %q, want %q", got, want)
	}
}

func TestGetRequestURLRejectsAnInvalidCredential(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "{not json"},
	}
	if _, err := adaptor.GetRequestURL(info); err == nil {
		t.Fatal("GetRequestURL() error = nil, want a credential error")
	}
}

func TestSetupRequestHeaderCarriesTheIdentity(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("intl", "cli", constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy), true)
	header := http.Header{}
	header.Set("Cookie", "session=stale")

	if err := adaptor.SetupRequestHeader(newTestContext(t), &header, info); err != nil {
		t.Fatalf("SetupRequestHeader() error = %v", err)
	}
	if got := header.Get("Authorization"); got != "Bearer access-token" {
		t.Fatalf("Authorization = %q, want the credential access token", got)
	}
	if header.Get("X-Machine-ID") == "" || header.Get("X-Session-ID") == "" {
		t.Fatalf("device headers missing: %v", header)
	}
	if header.Get("User-Agent") == "" {
		t.Fatal("User-Agent is empty")
	}
	if got := header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q, want text/event-stream", got)
	}
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := header.Get("Cookie"); got != "" {
		t.Fatalf("Cookie = %q, want it dropped", got)
	}
}

// The upstream answers a buffered chat request with HTTP 400, so the relay
// always asks for a stream and folds the answer back itself.
func TestFinalizeForcesStreaming(t *testing.T) {
	adaptor := &Adaptor{}
	info := thinkingDefaultInfo(true)
	request := &dto.GeneralOpenAIRequest{Model: "deepseek-v4.1-flash"}

	converted, err := adaptor.finalize(request, info)
	if err != nil {
		t.Fatalf("finalize() error = %v", err)
	}
	chat, ok := converted.(*dto.GeneralOpenAIRequest)
	if !ok {
		t.Fatalf("finalize() returned %T, want *dto.GeneralOpenAIRequest", converted)
	}
	if chat.Stream == nil || !*chat.Stream {
		t.Fatalf("Stream = %v, want true", chat.Stream)
	}
	if chat.StreamOptions == nil || !chat.StreamOptions.IncludeUsage {
		t.Fatalf("StreamOptions = %+v, want include_usage", chat.StreamOptions)
	}

	if _, err := adaptor.finalize(&dto.ClaudeRequest{}, info); err == nil {
		t.Fatal("finalize() accepted a Claude request, want an error")
	}
}

// The international edge answers a conversation whose first message is not a
// system prompt with HTTP 400 code 11-128, and it rejects a developer role the
// same way. A channel test carries a single user message, so it used to fail
// before the upstream ever looked at the content.
func TestFinalizeNormalizesTheMessageShape(t *testing.T) {
	cases := []struct {
		name     string
		messages []dto.Message
		want     []string
		opening  string
	}{
		{
			name:     "a user message opens the conversation",
			messages: []dto.Message{{Role: "user", Content: "hi"}},
			want:     []string{"system", "user"},
			opening:  defaultSystemPrompt,
		},
		{
			name:     "an assistant message opens the conversation",
			messages: []dto.Message{{Role: "assistant", Content: "hi"}},
			want:     []string{"system", "assistant"},
			opening:  defaultSystemPrompt,
		},
		{
			name: "an existing system message is kept",
			messages: []dto.Message{
				{Role: "system", Content: "be terse"},
				{Role: "user", Content: "hi"},
			},
			want:    []string{"system", "user"},
			opening: "be terse",
		},
		{
			name: "a developer message becomes the system message",
			messages: []dto.Message{
				{Role: "developer", Content: "be terse"},
				{Role: "user", Content: "hi"},
			},
			want:    []string{"system", "user"},
			opening: "be terse",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			converted, err := (&Adaptor{}).finalize(&dto.GeneralOpenAIRequest{Messages: testCase.messages}, thinkingDefaultInfo(true))
			require.NoError(t, err)
			chat, ok := converted.(*dto.GeneralOpenAIRequest)
			require.True(t, ok, "finalize() returned %T, want *dto.GeneralOpenAIRequest", converted)

			roles := make([]string, 0, len(chat.Messages))
			for _, message := range chat.Messages {
				roles = append(roles, message.Role)
			}
			assert.Equal(t, testCase.want, roles)
			require.NotEmpty(t, chat.Messages)
			assert.Equal(t, testCase.opening, chat.Messages[0].Content)
		})
	}
}

// toolCallObject renders one tool call; the repair only reads the id and must
// pass everything else through byte for byte.
func toolCallObject(id string) string {
	return `{"id":"` + id + `","type":"function","function":{"name":"get_weather","arguments":"{}"}}`
}

func toolCalls(ids ...string) json.RawMessage {
	encoded := make([]string, 0, len(ids))
	for _, id := range ids {
		encoded = append(encoded, toolCallObject(id))
	}
	return json.RawMessage("[" + strings.Join(encoded, ",") + "]")
}

// A call that never got a result, a result with no call, a parallel batch split
// over adjacent assistant messages and a result separated from its batch are
// all rejected by the upstream (11133 / 11-128), and because the history is
// replayed on every later turn one bad pair retires the conversation.
func TestFinalizeRepairsToolCallPairing(t *testing.T) {
	system := dto.Message{Role: "system", Content: "be terse"}
	cases := []struct {
		name     string
		messages []dto.Message
		want     []dto.Message
	}{
		{
			name: "an unanswered call is dropped",
			messages: []dto.Message{
				system,
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "", ToolCalls: toolCalls("call_1")},
			},
			want: []dto.Message{
				system,
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: ""},
			},
		},
		{
			name: "a result with no call is dropped",
			messages: []dto.Message{
				system,
				{Role: "user", Content: "hi"},
				{Role: "tool", ToolCallId: "call_9", Content: "18C"},
			},
			want: []dto.Message{
				system,
				{Role: "user", Content: "hi"},
			},
		},
		{
			name: "the unpaired half of a batch is dropped",
			messages: []dto.Message{
				system,
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "", ToolCalls: toolCalls("call_1", "call_2")},
				{Role: "tool", ToolCallId: "call_1", Content: "18C"},
			},
			want: []dto.Message{
				system,
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "", ToolCalls: toolCalls("call_1")},
				{Role: "tool", ToolCallId: "call_1", Content: "18C"},
			},
		},
		{
			name: "a split parallel batch is merged",
			messages: []dto.Message{
				system,
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "", ToolCalls: toolCalls("call_1")},
				{Role: "assistant", Content: "", ToolCalls: toolCalls("call_2")},
				{Role: "tool", ToolCallId: "call_1", Content: "18C"},
				{Role: "tool", ToolCallId: "call_2", Content: "11C"},
			},
			want: []dto.Message{
				system,
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "", ToolCalls: toolCalls("call_1", "call_2")},
				{Role: "tool", ToolCallId: "call_1", Content: "18C"},
				{Role: "tool", ToolCallId: "call_2", Content: "11C"},
			},
		},
		{
			name: "a result separated from its batch is repacked",
			messages: []dto.Message{
				system,
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "", ToolCalls: toolCalls("call_1", "call_2")},
				{Role: "tool", ToolCallId: "call_1", Content: "18C"},
				{Role: "user", Content: "a note"},
				{Role: "tool", ToolCallId: "call_2", Content: "11C"},
			},
			want: []dto.Message{
				system,
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "", ToolCalls: toolCalls("call_1", "call_2")},
				{Role: "tool", ToolCallId: "call_1", Content: "18C"},
				{Role: "tool", ToolCallId: "call_2", Content: "11C"},
				{Role: "user", Content: "a note"},
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			converted, err := (&Adaptor{}).finalize(&dto.GeneralOpenAIRequest{
				Model:    "hy4-preview",
				Messages: testCase.messages,
			}, thinkingDefaultInfo(true))
			require.NoError(t, err)
			chat, ok := converted.(*dto.GeneralOpenAIRequest)
			require.True(t, ok, "finalize() returned %T", converted)
			assert.Equal(t, testCase.want, chat.Messages)
		})
	}
}

// The delegate moves max_tokens onto max_completion_tokens for the reasoning
// models, but this upstream does not read the alias: it answered a request with
// max_completion_tokens=64 with 141 completion tokens.
func TestFinalizeTranslatesMaxCompletionTokens(t *testing.T) {
	cases := []struct {
		name                string
		maxTokens           *uint
		maxCompletionTokens *uint
		want                *uint
	}{
		{
			name:                "the alias moves over",
			maxCompletionTokens: lo.ToPtr(uint(64)),
			want:                lo.ToPtr(uint(64)),
		},
		{
			name:                "an explicit max_tokens wins",
			maxTokens:           lo.ToPtr(uint(8)),
			maxCompletionTokens: lo.ToPtr(uint(64)),
			want:                lo.ToPtr(uint(8)),
		},
		{
			name:                "a zero alias is dropped",
			maxCompletionTokens: lo.ToPtr(uint(0)),
		},
		{
			name: "neither stays unset",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			converted, err := (&Adaptor{}).finalize(&dto.GeneralOpenAIRequest{
				Model:               "hy4-preview",
				Messages:            []dto.Message{{Role: "user", Content: "hi"}},
				MaxTokens:           testCase.maxTokens,
				MaxCompletionTokens: testCase.maxCompletionTokens,
			}, thinkingDefaultInfo(true))
			require.NoError(t, err)
			chat, ok := converted.(*dto.GeneralOpenAIRequest)
			require.True(t, ok, "finalize() returned %T", converted)
			assert.Equal(t, testCase.want, chat.MaxTokens)
			assert.Nil(t, chat.MaxCompletionTokens)
		})
	}
}

// The upstream declares tool_choice as a string, so the object form fails to
// unmarshal with code 11101. The tools declarations stay either way: removing
// them makes an agent emit its call as unparseable text instead.
func TestFinalizeNormalizesToolChoice(t *testing.T) {
	cases := []struct {
		name   string
		choice any
		want   any
	}{
		{
			name:   "a function object becomes the tool name",
			choice: map[string]any{"type": "function", "function": map[string]any{"name": "get_weather"}},
			want:   "get_weather",
		},
		{
			name:   "a top-level function name is read too",
			choice: map[string]any{"type": "function", "name": "get_weather"},
			want:   "get_weather",
		},
		{
			name:   "an object none becomes the string",
			choice: map[string]any{"type": "none"},
			want:   "none",
		},
		{
			name:   "an object auto becomes the string",
			choice: map[string]any{"type": "auto"},
			want:   "auto",
		},
		{
			name:   "an object required becomes the string",
			choice: map[string]any{"type": "required"},
			want:   "required",
		},
		{
			name:   "an unknown object is dropped",
			choice: map[string]any{"type": "banana"},
		},
		{
			name:   "a plain string is kept",
			choice: "auto",
			want:   "auto",
		},
		{
			name:   "a number is dropped",
			choice: 7,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			converted, err := (&Adaptor{}).finalize(&dto.GeneralOpenAIRequest{
				Model:      "hy4-preview",
				Messages:   []dto.Message{{Role: "user", Content: "hi"}},
				ToolChoice: testCase.choice,
			}, thinkingDefaultInfo(true))
			require.NoError(t, err)
			chat, ok := converted.(*dto.GeneralOpenAIRequest)
			require.True(t, ok, "finalize() returned %T", converted)
			assert.Equal(t, testCase.want, chat.ToolChoice)
		})
	}
}

// thinking.type=enabled on its own leaves the reasoning trace off upstream: the
// same prompt answered with reasoning_tokens 0 without an effort level and 50
// with reasoning_effort=high. The client's own choice always wins.
func TestFinalizeInjectsDeepseekThinking(t *testing.T) {
	cases := []struct {
		name         string
		model        string
		thinking     json.RawMessage
		effort       string
		wantThinking string
		wantEffort   string
	}{
		{
			name:         "a deepseek request without the field gets both",
			model:        "deepseek-v4.1-flash",
			wantThinking: `{"type":"enabled"}`,
			wantEffort:   "high",
		},
		{
			name:         "a deepseek request keeps the effort it chose",
			model:        "deepseek-v4.1-flash",
			effort:       "low",
			wantThinking: `{"type":"enabled"}`,
			wantEffort:   "low",
		},
		{
			name:         "enabled alone still gets an effort",
			model:        "deepseek-v4.1-flash",
			thinking:     json.RawMessage(`{"type":"enabled"}`),
			wantThinking: `{"type":"enabled"}`,
			wantEffort:   "high",
		},
		{
			name:         "an explicit opt-out is respected",
			model:        "deepseek-v4.1-flash",
			thinking:     json.RawMessage(`{"type":"disabled"}`),
			wantThinking: `{"type":"disabled"}`,
		},
		{
			name:         "reasoning_effort none really turns the trace off",
			model:        "deepseek-v4.1-flash",
			effort:       "none",
			wantThinking: `{"type":"disabled"}`,
		},
		{
			name:         "disabled beside an effort drops the effort",
			model:        "deepseek-v4.1-flash",
			thinking:     json.RawMessage(`{"type":"disabled"}`),
			effort:       "high",
			wantThinking: `{"type":"disabled"}`,
		},
		{
			name:  "another model is untouched",
			model: "hy4-preview",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			converted, err := (&Adaptor{}).finalize(&dto.GeneralOpenAIRequest{
				Model:           testCase.model,
				Messages:        []dto.Message{{Role: "user", Content: "hi"}},
				THINKING:        testCase.thinking,
				ReasoningEffort: testCase.effort,
			}, thinkingDefaultInfo(true))
			require.NoError(t, err)
			chat, ok := converted.(*dto.GeneralOpenAIRequest)
			require.True(t, ok, "finalize() returned %T", converted)
			assert.Equal(t, testCase.wantThinking, string(chat.THINKING))
			assert.Equal(t, testCase.wantEffort, chat.ReasoningEffort)
		})
	}
}

// The channel setting decides what a request that never mentions thinking gets.
// An explicit ask or an explicit opt-out always wins over the default, so an
// operator can turn the reference behaviour off without touching clients.
func TestFinalizeHonoursTheChannelThinkingDefault(t *testing.T) {
	cases := []struct {
		name         string
		effort       string
		defaultOn    bool
		wantThinking string
		wantEffort   string
	}{
		{
			name:         "the default on injects a trace",
			defaultOn:    true,
			wantThinking: `{"type":"enabled"}`,
			wantEffort:   "high",
		},
		{
			name:      "the default off leaves the request alone",
			defaultOn: false,
		},
		{
			name:         "an explicit ask survives the default off",
			effort:       "low",
			defaultOn:    false,
			wantThinking: `{"type":"enabled"}`,
			wantEffort:   "low",
		},
		{
			name:         "an explicit opt-out wins over the default on",
			effort:       "none",
			defaultOn:    true,
			wantThinking: `{"type":"disabled"}`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			converted, err := (&Adaptor{}).finalize(&dto.GeneralOpenAIRequest{
				Model:           "deepseek-v4.1-flash",
				Messages:        []dto.Message{{Role: "user", Content: "hi"}},
				ReasoningEffort: testCase.effort,
			}, thinkingDefaultInfo(testCase.defaultOn))
			require.NoError(t, err)
			chat, ok := converted.(*dto.GeneralOpenAIRequest)
			require.True(t, ok, "finalize() returned %T", converted)
			assert.Equal(t, testCase.wantThinking, string(chat.THINKING))
			assert.Equal(t, testCase.wantEffort, chat.ReasoningEffort)
		})
	}
}

// While thinking is on the upstream requires a reasoning trace on every
// assistant turn and rejects an empty one, so reasoning mirrors a non-empty
// value beside reasoning_content.
func TestFinalizeBackfillsReasoningContent(t *testing.T) {
	system := dto.Message{Role: "system", Content: "be terse"}
	cases := []struct {
		name                 string
		model                string
		effort               string
		assistant            dto.Message
		wantReasoningContent *string
		wantReasoning        *string
	}{
		{
			name:                 "a deepseek turn without a trace gets a placeholder",
			model:                "deepseek-v4.1-flash",
			effort:               "high",
			assistant:            dto.Message{Role: "assistant", Content: "18"},
			wantReasoningContent: lo.ToPtr(""),
			wantReasoning:        lo.ToPtr(" "),
		},
		{
			name:                 "an existing trace is mirrored onto reasoning",
			model:                "deepseek-v4.1-flash",
			effort:               "high",
			assistant:            dto.Message{Role: "assistant", Content: "18", ReasoningContent: lo.ToPtr("17*23")},
			wantReasoningContent: lo.ToPtr("17*23"),
			wantReasoning:        lo.ToPtr("17*23"),
		},
		{
			name:      "another model keeps no reasoning fields",
			model:     "hy4-preview",
			effort:    "high",
			assistant: dto.Message{Role: "assistant", Content: "18"},
		},
		{
			name:      "an opt-out without a trace is left alone",
			model:     "deepseek-v4.1-flash",
			effort:    "none",
			assistant: dto.Message{Role: "assistant", Content: "18"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			converted, err := (&Adaptor{}).finalize(&dto.GeneralOpenAIRequest{
				Model:           testCase.model,
				ReasoningEffort: testCase.effort,
				Messages:        []dto.Message{system, testCase.assistant},
			}, thinkingDefaultInfo(true))
			require.NoError(t, err)
			chat, ok := converted.(*dto.GeneralOpenAIRequest)
			require.True(t, ok, "finalize() returned %T", converted)
			require.Len(t, chat.Messages, 2)
			assert.Equal(t, testCase.wantReasoningContent, chat.Messages[1].ReasoningContent)
			assert.Equal(t, testCase.wantReasoning, chat.Messages[1].Reasoning)
		})
	}
}

func TestFinalizeSanitizesTheCNHarnessIdentityOnly(t *testing.T) {
	const (
		harnessIdentity = "You are an AI agent powered by DeepSeek Harness."
		codingIdentity  = "You are a coding agent powered by the deepseek-v4.1-flash model."
		sanitized       = "You are a helpful AI assistant."
	)
	cases := []struct {
		name    string
		realm   string
		role    string
		content any
		want    any
	}{
		{
			name:    "CN system prompt",
			realm:   "cn",
			role:    "system",
			content: harnessIdentity,
			want:    sanitized,
		},
		{
			name:    "CN identity embedded in a larger prompt",
			realm:   "cn",
			role:    "system",
			content: harnessIdentity + "\nBe concise.",
			want:    sanitized + "\nBe concise.",
		},
		{
			name:    "CN coding identity",
			realm:   "cn",
			role:    "system",
			content: codingIdentity,
			want:    sanitized,
		},
		{
			name:    "CN developer prompt",
			realm:   "cn",
			role:    "developer",
			content: harnessIdentity,
			want:    sanitized,
		},
		{
			name:    "CN user content is untouched",
			realm:   "cn",
			role:    "user",
			content: harnessIdentity,
			want:    harnessIdentity,
		},
		{
			name:    "INTL system prompt is untouched",
			realm:   "intl",
			role:    "system",
			content: harnessIdentity,
			want:    harnessIdentity,
		},
		{
			name:  "CN text block content",
			realm: "cn",
			role:  "system",
			content: []any{
				map[string]any{"type": "text", "text": harnessIdentity},
			},
			want: []any{
				map[string]any{"type": "text", "text": sanitized},
			},
		},
		{
			name:    "CN unrelated system prompt is byte-identical",
			realm:   "cn",
			role:    "system",
			content: "Be precise and concise.",
			want:    "Be precise and concise.",
		},
		{
			name:    "CN runtime-context marker",
			realm:   "cn",
			role:    "system",
			content: "Current DeepSeek Harness implementation checkout: C:\\work.",
			want:    "Current agent platform implementation checkout: C:\\work.",
		},
		{
			name:    "CN GUI paragraph marker",
			realm:   "cn",
			role:    "system",
			content: "You are interacting with the user through the DeepSeek Harness Web GUI whose URL the runtime context names.",
			want:    "You are interacting with the user through the agent platform Web GUI whose URL the runtime context names.",
		},
		{
			name:    "CN user content with the marker is untouched",
			realm:   "cn",
			role:    "user",
			content: "Current DeepSeek Harness implementation checkout: C:\\work.",
			want:    "Current DeepSeek Harness implementation checkout: C:\\work.",
		},
		{
			name:    "INTL marker is untouched",
			realm:   "intl",
			role:    "system",
			content: "Current DeepSeek Harness implementation checkout: C:\\work.",
			want:    "Current DeepSeek Harness implementation checkout: C:\\work.",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			info := newRelayInfo(testCase.realm, "cli",
				constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy), false)
			converted, err := (&Adaptor{}).finalize(&dto.GeneralOpenAIRequest{
				Model:    "hy4-preview",
				Messages: []dto.Message{{Role: testCase.role, Content: testCase.content}},
			}, info)
			require.NoError(t, err)
			chat, ok := converted.(*dto.GeneralOpenAIRequest)
			require.True(t, ok, "finalize() returned %T", converted)

			if testCase.role == "user" {
				require.Len(t, chat.Messages, 2)
				assert.Equal(t, testCase.want, chat.Messages[1].Content)
				return
			}
			require.NotEmpty(t, chat.Messages)
			assert.Equal(t, "system", chat.Messages[0].Role)
			assert.Equal(t, testCase.want, chat.Messages[0].Content)
		})
	}
}

func TestConvertOpenAIRequestForcesStreaming(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("intl", "cli", constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy), false)
	request := &dto.GeneralOpenAIRequest{
		Model:    "deepseek-v4.1-flash",
		Messages: []dto.Message{{Role: "user", Content: "hi"}},
	}
	converted, err := adaptor.ConvertOpenAIRequest(newTestContext(t), info, request)
	if err != nil {
		t.Fatalf("ConvertOpenAIRequest() error = %v", err)
	}
	chat, ok := converted.(*dto.GeneralOpenAIRequest)
	if !ok {
		t.Fatalf("ConvertOpenAIRequest() returned %T, want *dto.GeneralOpenAIRequest", converted)
	}
	if chat.Stream == nil || !*chat.Stream {
		t.Fatalf("Stream = %v, want true", chat.Stream)
	}
	if chat.StreamOptions == nil || !chat.StreamOptions.IncludeUsage {
		t.Fatalf("StreamOptions = %+v, want include_usage", chat.StreamOptions)
	}
}

// A Responses request reaches the adaptor through /v1/responses, which the
// upstream cannot serve: the relay rewrites it as the chat body WorkBuddy
// speaks and then translates the answer back.
func TestConvertOpenAIResponsesRequestProducesAStreamingChatBody(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("intl", "cli", constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy), false)
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-6-luna",
		Input: json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]`),
	}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(newTestContext(t), info, request)
	require.NoError(t, err)
	chat, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok, "ConvertOpenAIResponsesRequest() returned %T, want *dto.GeneralOpenAIRequest", converted)
	require.NotEmpty(t, chat.Messages)
	assert.Equal(t, "system", chat.Messages[0].Role)
	require.NotNil(t, chat.Stream)
	assert.True(t, *chat.Stream)
	require.NotNil(t, chat.StreamOptions)
	assert.True(t, chat.StreamOptions.IncludeUsage)
}

func TestDoResponseRejectsTheResponsesCompactEndpoint(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("intl", "cli", constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy), false)
	info.RelayMode = relayconstant.RelayModeResponsesCompact

	if _, err := adaptor.DoResponse(newTestContext(t), &http.Response{Body: io.NopCloser(strings.NewReader(""))}, info); err == nil {
		t.Fatal("DoResponse() error = nil, want the Responses compact endpoint rejected")
	}
}

// The upstream answers a Responses request with a chat.completion stream, so a
// buffered client must receive one Responses object instead of raw frames.
func TestDoResponseFoldsTheUpstreamStreamForABufferedResponsesClient(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyIsStream, false)

	info := newRelayInfo("intl", "cli", constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy), false)
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RelayMode = relayconstant.RelayModeResponses
	info.IsStream = true

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(foldStream)),
	}

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	body := recorder.Body.Bytes()
	assert.Equal(t, "response", gjson.GetBytes(body, "object").String())
	assert.Equal(t, "Hello", gjson.GetBytes(body, `output.#(type=="message").content.0.text`).String(), "body=%s", body)
}

// A streaming Responses client gets Responses events translated from the chat
// frames the upstream sent.
func TestDoResponseStreamsTheUpstreamStreamAsResponsesEvents(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyIsStream, true)

	info := newRelayInfo("intl", "cli", constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy), false)
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RelayMode = relayconstant.RelayModeResponses
	info.IsStream = true

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(foldStream)),
	}

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	stream := recorder.Body.String()
	assert.Contains(t, stream, "response.output_text.delta")
	assert.Contains(t, stream, "Hello")
}

// The compatible handler promotes info.IsStream whenever the upstream answers
// with text/event-stream, but WorkBuddy always streams upstream. A buffered
// client must still receive the folded chat.completion object, not raw frames.
func TestDoResponseFoldsTheUpstreamStreamForABufferedClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyIsStream, false)

	info := newRelayInfo("intl", "cli", constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy), false)
	info.RelayFormat = types.RelayFormatOpenAI
	// compatible_handler already flipped the flag because the upstream
	// Content-Type is text/event-stream.
	info.IsStream = true

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(foldStream)),
	}

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
	if apiErr != nil {
		t.Fatalf("DoResponse() error = %v", apiErr)
	}
	if usage == nil {
		t.Fatal("DoResponse() usage = nil, want the folded usage")
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	body := recorder.Body.Bytes()
	if got := gjson.GetBytes(body, "object").String(); got != "chat.completion" {
		t.Fatalf("object = %q, want chat.completion (body %s)", got, body)
	}
	if got := gjson.GetBytes(body, "choices.0.message.content").String(); got != "Hello" {
		t.Fatalf("content = %q, want Hello (body %s)", got, body)
	}
}

const foldStream = `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"content":"lo","reasoning_content":"think"}}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}

data: [DONE]
`

func TestFoldChatCompletionsChunks(t *testing.T) {
	folded, err := foldChatCompletionsChunks([]byte(foldStream))
	if err != nil {
		t.Fatalf("foldChatCompletionsChunks() error = %v", err)
	}
	cases := map[string]string{
		"object":                              "chat.completion",
		"id":                                  "chatcmpl-1",
		"model":                               "deepseek-v4.1-flash",
		"choices.0.message.role":              "assistant",
		"choices.0.message.content":           "Hello",
		"choices.0.message.reasoning_content": "think",
		"choices.0.finish_reason":             "stop",
	}
	for path, want := range cases {
		if got := gjson.GetBytes(folded, path).String(); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if got := gjson.GetBytes(folded, "usage.total_tokens").Int(); got != 5 {
		t.Errorf("usage.total_tokens = %d, want 5", got)
	}
	if got := gjson.GetBytes(folded, "created").Int(); got != 1 {
		t.Errorf("created = %d, want 1", got)
	}
}

const foldToolStream = `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"ci"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"SF\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":""}}]},"finish_reason":"tool_calls"}]}

data: [DONE]
`

func TestFoldChatCompletionsChunksMergesToolCallFragments(t *testing.T) {
	folded, err := foldChatCompletionsChunks([]byte(foldToolStream))
	if err != nil {
		t.Fatalf("foldChatCompletionsChunks() error = %v", err)
	}
	calls := gjson.GetBytes(folded, "choices.0.message.tool_calls")
	if !calls.IsArray() || len(calls.Array()) != 1 {
		t.Fatalf("tool_calls = %s, want exactly one merged call", calls.Raw)
	}
	if got := calls.Array()[0].Get("id").String(); got != "call_1" {
		t.Errorf("tool call id = %q, want call_1", got)
	}
	if got := calls.Array()[0].Get("function.name").String(); got != "get_weather" {
		t.Errorf("function.name = %q, want get_weather", got)
	}
	if got := calls.Array()[0].Get("function.arguments").String(); got != `{"city":"SF"}` {
		t.Errorf("function.arguments = %q, want the concatenated fragments", got)
	}
	if calls.Array()[0].Get("index").Exists() {
		t.Errorf("a buffered tool call must not carry a stream index: %s", calls.Raw)
	}
	if got := gjson.GetBytes(folded, "choices.0.finish_reason").String(); got != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", got)
	}
}

func TestFoldChatCompletionsChunksRejectsAnErrorFrame(t *testing.T) {
	stream := `data: {"error":{"message":"upstream exploded","code":"boom"}}`
	_, err := foldChatCompletionsChunks([]byte(stream))
	if err == nil || !strings.Contains(err.Error(), "upstream exploded") {
		t.Fatalf("foldChatCompletionsChunks() error = %v, want the upstream message", err)
	}
}

func TestFoldChatCompletionsChunksRejectsAnEmptyStream(t *testing.T) {
	_, err := foldChatCompletionsChunks([]byte("data: [DONE]\n"))
	if err == nil || !strings.Contains(err.Error(), "no completion chunk") {
		t.Fatalf("foldChatCompletionsChunks() error = %v, want an empty-stream error", err)
	}
}

// A folded answer is accepted only when the upstream proved the stream
// finished: the [DONE] terminator or a non-empty finish_reason must be present.
// Otherwise a truncated answer would be billed as a complete reply.
func TestFoldChatCompletionsChunksRequiresAFinishedStream(t *testing.T) {
	const deltas = `data: {"id":"chatcmpl-1","model":"deepseek-v4.1-flash","created":1,"choices":[{"index":0,"delta":{"role":"assistant","content":"half"}}]}

`

	tests := []struct {
		name       string
		stream     string
		wantFinish string
		wantError  string
	}{
		{
			name:       "done terminator",
			stream:     deltas + "data: [DONE]\n",
			wantFinish: "stop",
		},
		{
			name: "finish reason without the terminator",
			stream: deltas + `data: {"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}
`,
			wantFinish: "length",
		},
		{
			name:      "cut before any terminator or finish reason",
			stream:    deltas,
			wantError: "ended before completion",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			folded, err := foldChatCompletionsChunks([]byte(testCase.stream))
			if testCase.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantError)
				assert.Nil(t, folded)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "half", gjson.GetBytes(folded, "choices.0.message.content").String())
			assert.Equal(t, testCase.wantFinish, gjson.GetBytes(folded, "choices.0.finish_reason").String())
		})
	}
}

// A stream that fills the whole buffering allowance cannot be proven complete,
// so the fold must fail instead of forwarding a cut answer as a finished reply.
func TestFoldChatCompletionsStreamRejectsATruncatedBody(t *testing.T) {
	const chunk = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n"
	resp := &http.Response{
		Header: http.Header{},
		Body:   io.NopCloser(strings.NewReader(strings.Repeat(chunk, maxFoldBodyBytes/len(chunk)+2))),
	}

	err := foldChatCompletionsStream(resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fold limit")
}

func TestFoldChatCompletionsStreamRewritesTheBody(t *testing.T) {
	resp := &http.Response{
		Header:        http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:          io.NopCloser(strings.NewReader(foldStream)),
		ContentLength: int64(len(foldStream)),
	}
	if err := foldChatCompletionsStream(resp); err != nil {
		t.Fatalf("foldChatCompletionsStream() error = %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := gjson.GetBytes(body, "choices.0.message.content").String(); got != "Hello" {
		t.Fatalf("folded content = %q, want Hello", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if resp.ContentLength != int64(len(body)) {
		t.Fatalf("ContentLength = %d, want %d", resp.ContentLength, len(body))
	}
	if got := resp.Header.Get("Content-Length"); got != "" {
		t.Fatalf("Content-Length header = %q, want it dropped", got)
	}
}

// A model-scoped refusal must never echo the raw upstream body: it can be an
// HTML page or a JSON envelope that names the account or exposes internal
// fields, and the shared error handler would surface it verbatim.
func TestModelScopedRefusalErrorDropsTheRawUpstreamBody(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		statusCode int
		wantIn     []string
		wantNotIn  []string
	}{
		{
			name:       "html body falls back to a generic message",
			body:       "<html><body>account=secret@example.com internal_id=42</body></html>",
			statusCode: http.StatusBadRequest,
			wantIn:     []string{"upstream refused the model request (status 400)"},
			wantNotIn:  []string{"<html>", "secret@example.com", "internal_id"},
		},
		{
			name:       "declared error message is kept",
			body:       `{"error":{"code":"model_not_found","message":"model not allowed"}}`,
			statusCode: http.StatusForbidden,
			wantIn:     []string{"model not allowed"},
		},
		{
			name:       "empty body falls back to a generic message",
			body:       "",
			statusCode: http.StatusBadRequest,
			wantIn:     []string{"upstream refused the model request (status 400)"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			refusalErr := ModelScopedRefusalError([]byte(testCase.body), testCase.statusCode)
			require.NotNil(t, refusalErr)
			assert.True(t, refusalErr.IsModelScoped())
			assert.Equal(t, testCase.statusCode, refusalErr.StatusCode)
			for _, want := range testCase.wantIn {
				assert.Contains(t, refusalErr.Error(), want)
			}
			for _, unwanted := range testCase.wantNotIn {
				assert.NotContains(t, refusalErr.Error(), unwanted)
			}
		})
	}
}

func TestCredentialAndModelListAgree(t *testing.T) {
	credential, err := wbcore.ParseCredential(channelKey("intl", "cli"))
	if err != nil {
		t.Fatalf("ParseCredential() error = %v", err)
	}
	if credential.ChatBaseURL() != constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy) {
		t.Fatalf("the intl CLI host %q should match the channel default %q",
			credential.ChatBaseURL(), constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy))
	}
	if len((&Adaptor{}).GetModelList()) == 0 {
		t.Fatal("GetModelList() is empty")
	}
}
