package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const codexOAuthRequestTimeout = 60 * time.Second

type codexAuthURLRequest struct {
	Proxy string `json:"proxy"`
}

type codexExchangeCodeRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	State     string `json:"state" binding:"required"`
	Code      string `json:"code" binding:"required"`
	Proxy     string `json:"proxy"`
}

// GenerateCodexAuthURL returns the OpenAI consent URL and the session id needed
// to exchange the resulting code.
func GenerateCodexAuthURL(c *gin.Context) {
	var request codexAuthURLRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}

	result, err := service.GenerateCodexAuthURL(request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to generate codex auth url: %s", err.Error()))
		common.ApiErrorT(c, "Failed to generate the Codex authorization link")
		return
	}
	common.ApiSuccessT(c, "OpenAI Codex authorization link generated", result)
}

// ExchangeCodexCode turns a pasted authorization code into a channel
// credential. The caller stores the returned credential as the channel key.
func ExchangeCodexCode(c *gin.Context) {
	var request codexExchangeCodeRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), codexOAuthRequestTimeout)
	defer cancel()

	credential, err := service.ExchangeCodexCode(ctx, request.SessionID, request.State, request.Code, request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to exchange codex authorization code: %s", err.Error()))
		common.ApiErrorT(c, "Failed to exchange the Codex authorization code: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	common.ApiSuccessT(c, "Codex credentials created", credential)
}
