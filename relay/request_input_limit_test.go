package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const inputLimitTestModel = "gpt-4o"

// newInputLimitContext builds the request context a relay attempt has: the
// channel selected for this attempt has already been written to the context by
// SetupContextForSelectedChannel. A nil setting leaves the key unset.
func newInputLimitContext(t *testing.T, setting *dto.ChannelSettings) *gin.Context {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{\"model\":\""+inputLimitTestModel+"\"}"))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, inputLimitTestModel)
	if setting != nil {
		common.SetContextKey(c, constant.ContextKeyChannelSetting, *setting)
	}
	return c
}

func TestGuardInputTokenLimitAllowsDisabledAndWithinLimit(t *testing.T) {
	// No limit configured: the channel is unguarded.
	c := newInputLimitContext(t, &dto.ChannelSettings{})
	info := &relaycommon.RelayInfo{}
	info.SetEstimatePromptTokens(1_000_000)
	assert.Nil(t, GuardInputTokenLimit(c, info))

	// Exactly at the limit is allowed: the guard only rejects what is over.
	c = newInputLimitContext(t, &dto.ChannelSettings{MaxInputTokens: 100})
	info = &relaycommon.RelayInfo{}
	info.SetEstimatePromptTokens(100)
	assert.Nil(t, GuardInputTokenLimit(c, info))
}

func TestGuardInputTokenLimitRejectsOverLimit(t *testing.T) {
	c := newInputLimitContext(t, &dto.ChannelSettings{MaxInputTokens: 100})
	info := &relaycommon.RelayInfo{}
	info.SetEstimatePromptTokens(101)

	apiErr := GuardInputTokenLimit(c, info)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeContextLengthExceeded, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr), "retrying a too-long prompt on another channel is pointless")
	assert.Contains(t, apiErr.Error(), "101")
	assert.Contains(t, apiErr.Error(), "100")
}

func TestGuardInputTokenLimitFallsBackToChannelMeta(t *testing.T) {
	// The transport handlers run after this guard, so info.ChannelMeta is only
	// populated on later code paths; the guard must still honour it when set.
	c := newInputLimitContext(t, nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{MaxInputTokens: 50}}}
	info.SetEstimatePromptTokens(51)
	require.NotNil(t, GuardInputTokenLimit(c, info))
}

func TestGuardInputTokenLimitCountsWhenEstimateIsMissing(t *testing.T) {
	// Billing leaves the estimate at zero when request-token estimation is
	// switched off globally, but a configured guard still needs a number.
	longBody := &dto.GeneralOpenAIRequest{
		Model:    inputLimitTestModel,
		Messages: []dto.Message{{Role: "user", Content: strings.Repeat("context ", 200)}},
	}

	c := newInputLimitContext(t, &dto.ChannelSettings{MaxInputTokens: 10})
	info := &relaycommon.RelayInfo{Request: longBody, RelayFormat: types.RelayFormatOpenAI}
	apiErr := GuardInputTokenLimit(c, info)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeContextLengthExceeded, apiErr.GetErrorCode())

	c = newInputLimitContext(t, &dto.ChannelSettings{MaxInputTokens: 100000})
	info = &relaycommon.RelayInfo{Request: longBody, RelayFormat: types.RelayFormatOpenAI}
	assert.Nil(t, GuardInputTokenLimit(c, info))
}

func TestGuardInputTokenLimitIsNilSafe(t *testing.T) {
	assert.Nil(t, GuardInputTokenLimit(nil, nil))
	c := newInputLimitContext(t, &dto.ChannelSettings{MaxInputTokens: 1})
	assert.Nil(t, GuardInputTokenLimit(c, nil))
}
