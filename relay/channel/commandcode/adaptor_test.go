package commandcode

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	ccore "github.com/QuantumNous/new-api/pkg/commandcode"
	"github.com/QuantumNous/new-api/relay/channel/protocolshim"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestGetRequestURLRoutesByModel(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		format  types.RelayFormat
		wantURL string
	}{
		{
			name:    "claude messages",
			model:   "claude-sonnet-4-6",
			wantURL: "https://api.commandcode.ai/provider/v1/messages",
		},
		{
			name:    "gpt responses",
			model:   "gpt-5.6-luna",
			format:  types.RelayFormatOpenAIResponses,
			wantURL: "https://api.commandcode.ai/provider/v1/responses",
		},
		{
			name:    "open model chat",
			model:   "deepseek/deepseek-v4-flash",
			wantURL: "https://api.commandcode.ai/provider/v1/chat/completions",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := newRelayInfo("user_test", test.model)
			info.RelayFormat = test.format
			adaptor := &Adaptor{}
			requestURL, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, test.wantURL, requestURL)
		})
	}
}

func TestGetRequestURLRejectsSystemOneModel(t *testing.T) {
	adaptor := &Adaptor{}
	_, err := adaptor.GetRequestURL(newRelayInfo("user_test", "typesafe/jev"))
	require.Error(t, err)
}

func TestSetupRequestHeaderUsesBearerToken(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("user_test", "deepseek/deepseek-v4-flash")
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(newTestContext(t), &header, info))
	assert.Equal(t, "Bearer user_test", header.Get("Authorization"))
	assert.Equal(t, ccore.DefaultUserAgent, header.Get("User-Agent"))
}

func TestSetupRequestHeaderAddsAnthropicHeaders(t *testing.T) {
	adaptor := &Adaptor{}
	info := newRelayInfo("user_test", "claude-sonnet-4-6")
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(newTestContext(t), &header, info))
	assert.Equal(t, "Bearer user_test", header.Get("Authorization"))
	assert.Equal(t, "2023-06-01", header.Get("anthropic-version"))
}

func TestBuildProtocolURLUsesTheDefaultBase(t *testing.T) {
	assert.Equal(t, ccore.DefaultProviderBaseURL+"/messages", protocolshim.BuildURL(ccore.DefaultProviderBaseURL, ccore.ProtocolAnthropic))
	assert.Equal(t, ccore.DefaultProviderBaseURL+"/responses", protocolshim.BuildURL(ccore.DefaultProviderBaseURL, ccore.ProtocolResponses))
	assert.Equal(t, ccore.DefaultProviderBaseURL+"/chat/completions", protocolshim.BuildURL(ccore.DefaultProviderBaseURL, ccore.ProtocolChatCompletions))
}

func TestChannelNameMatchesTheChannelTypeDisplayName(t *testing.T) {
	assert.Equal(t, constant.ChannelTypeNames[constant.ChannelTypeCommandCode], ChannelName)
	assert.Equal(t, "command_code", ccore.CredentialType, "the credential wire type keeps the slug")
}

func TestClientFacingErrorsUseTheChannelName(t *testing.T) {
	adaptor := &Adaptor{}
	_, err := adaptor.ConvertGeminiRequest(nil, nil, &dto.GeminiChatRequest{})
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), ChannelName+" channel: "), err.Error())
	assert.NotContains(t, err.Error(), "command_code")
}
