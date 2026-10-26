package controller

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	occore "github.com/QuantumNous/new-api/pkg/opencode"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const openCodeOAuthRequestTimeout = 60 * time.Second

type openCodeStartLoginRequest struct {
	Mode   string `json:"mode"`
	Server string `json:"server"`
	Proxy  string `json:"proxy"`
}

type openCodePollLoginRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Proxy     string `json:"proxy"`
}

type openCodeRefreshTokenRequest struct {
	Credential string `json:"credential" binding:"required"`
	Proxy      string `json:"proxy"`
}

// StartOpenCodeLogin opens a device authorization attempt.
func StartOpenCodeLogin(c *gin.Context) {
	var request openCodeStartLoginRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), openCodeOAuthRequestTimeout)
	defer cancel()
	result, err := service.StartOpenCodeLogin(ctx, request.Mode, request.Server, request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to start opencode login: %s", err.Error()))
		common.ApiErrorT(c, "Failed to start the OpenCode login: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	common.ApiSuccessT(c, "OpenCode login started", result)
}

// PollOpenCodeLogin performs one poll of an in-flight login.
func PollOpenCodeLogin(c *gin.Context) {
	var request openCodePollLoginRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), openCodeOAuthRequestTimeout)
	defer cancel()
	status, err := service.PollOpenCodeLogin(ctx, request.SessionID, request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to poll opencode login: %s", err.Error()))
		common.ApiErrorT(c, "Failed to poll the OpenCode login: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	response := gin.H{"status": status.Status}
	if status.Interval > 0 {
		response["interval"] = status.Interval
	}
	if status.Credential != nil {
		encoded, err := status.Credential.Marshal()
		if err != nil {
			common.ApiErrorT(c, "Failed to encode the OpenCode credential")
			return
		}
		response["credential"] = encoded
	}
	if status.Status == service.OpenCodeLoginReady {
		common.ApiSuccessT(c, "OpenCode credentials created", response)
		return
	}
	common.ApiSuccessT(c, "OpenCode login status fetched", response)
}

// RefreshOpenCodeCredential renews a credential and returns its compact JSON.
func RefreshOpenCodeCredential(c *gin.Context) {
	var request openCodeRefreshTokenRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), openCodeOAuthRequestTimeout)
	defer cancel()
	credential, err := service.RefreshOpenCodeCredential(ctx, request.Credential, request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to refresh opencode credential: %s", err.Error()))
		common.ApiErrorT(c, "Failed to refresh the OpenCode credential: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	encoded, err := credential.Marshal()
	if err != nil {
		common.ApiErrorT(c, "Failed to encode the OpenCode credential")
		return
	}
	common.ApiSuccessT(c, "OpenCode credentials refreshed", gin.H{"credential": encoded})
}

// RefreshOpenCodeChannelCredential refreshes every credential in a channel.
func RefreshOpenCodeChannelCredential(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorT(c, "Invalid channel ID")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), openCodeOAuthRequestTimeout)
	defer cancel()
	credential, _, err := service.RefreshOpenCodeChannelCredential(ctx, channelID, true)
	if err != nil {
		common.SysError(common.LogText("failed to refresh opencode channel credential: %s", err.Error()))
		common.ApiErrorT(c, "Failed to refresh the OpenCode channel credential: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	encoded, err := credential.Marshal()
	if err != nil {
		common.ApiErrorT(c, "Failed to encode the OpenCode credential")
		return
	}
	common.ApiSuccessT(c, "OpenCode channel credential refreshed", gin.H{"credential": encoded})
}

// GetOpenCodeChannelUsage returns the Go usage windows for a channel.
func GetOpenCodeChannelUsage(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorT(c, "Invalid channel ID")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), openCodeOAuthRequestTimeout)
	defer cancel()
	usage, err := service.GetOpenCodeChannelUsage(ctx, channelID)
	if err != nil {
		if errors.Is(err, occore.ErrUsageUnavailable) {
			common.ApiErrorT(c, "OpenCode usage is only available for Go subscriptions")
			return
		}
		common.ApiErrorT(c, "Failed to fetch the OpenCode usage: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	common.ApiSuccessT(c, "OpenCode usage fetched", usage)
}
