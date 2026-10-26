package relay

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// GuardInputTokenLimit rejects a request whose estimated input already exceeds
// the channel's max_input_tokens, before the upstream is called. The estimate
// comes from the billing pre-flight, so a guarded request pays no extra token
// counting and no upstream call: a prompt that would have failed upstream fails
// here with a context_length_exceeded error the client already understands.
//
// The limit is read from the request context rather than from info.ChannelMeta
// because the channel is picked per attempt inside the relay retry loop while
// InitChannelMeta runs later, inside the transport handler; the context carries
// the setting of the channel selected for this attempt.
func GuardInputTokenLimit(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if c == nil || info == nil {
		return nil
	}

	limit := 0
	if setting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting); ok {
		limit = setting.MaxInputTokens
	} else if info.ChannelMeta != nil {
		limit = info.ChannelMeta.ChannelSetting.MaxInputTokens
	}
	if limit <= 0 {
		return nil
	}

	tokens := info.GetEstimatePromptTokens()
	if tokens <= 0 && info.Request != nil {
		// Billing skips request-token estimation when the operator turned the
		// global switch off, but a configured guard still needs a number.
		counted, err := service.CountRequestToken(c, info.Request.GetTokenCountMeta(), info)
		if err != nil {
			// Never turn a counting failure into a rejected request.
			return nil
		}
		tokens = counted
	}
	if tokens <= limit {
		return nil
	}

	message := i18n.T(c, i18n.MsgRelayInputTokenLimitExceeded, map[string]any{
		"Tokens": tokens,
		"Limit":  limit,
	})
	return types.NewError(errors.New(message), types.ErrorCodeContextLengthExceeded,
		types.ErrOptionWithStatusCode(http.StatusBadRequest),
		types.ErrOptionWithSkipRetry())
}
