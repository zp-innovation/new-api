package controller

import (
	"context"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const antigravityOAuthRequestTimeout = 60 * time.Second

type antigravityAuthURLRequest struct {
	Proxy string `json:"proxy"`
}

type antigravityExchangeCodeRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	State     string `json:"state" binding:"required"`
	Code      string `json:"code" binding:"required"`
	Proxy     string `json:"proxy"`
}

type antigravityRefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
	Proxy        string `json:"proxy"`
}

// GenerateAntigravityAuthURL returns the Google consent URL and the session id
// needed to exchange the resulting code.
func GenerateAntigravityAuthURL(c *gin.Context) {
	var request antigravityAuthURLRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.SysError(common.LogText("failed to bind the antigravity authorization link request: %s", err.Error()))
		common.ApiErrorT(c, "Invalid request")
		return
	}

	result, err := service.GenerateAntigravityAuthURL(request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to generate antigravity auth url: %s", err.Error()))
		common.ApiErrorT(c, "Failed to generate the Antigravity authorization link")
		return
	}
	common.ApiSuccessT(c, "Antigravity authorization link generated", result)
}

// ExchangeAntigravityCode turns a pasted authorization code into a channel
// credential. The caller stores the returned credential as the channel key.
func ExchangeAntigravityCode(c *gin.Context) {
	var request antigravityExchangeCodeRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.SysError(common.LogText("failed to bind the antigravity code exchange request: %s", err.Error()))
		common.ApiErrorT(c, "Invalid request")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), antigravityOAuthRequestTimeout)
	defer cancel()

	credential, err := service.ExchangeAntigravityCode(ctx, request.SessionID, request.State, request.Code, request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to exchange antigravity authorization code: %s", err.Error()))
		common.ApiErrorT(c, "Failed to exchange the Antigravity authorization code")
		return
	}
	common.ApiSuccessT(c, "Antigravity credentials created", credential)
}

// RefreshAntigravityCredential validates a refresh token and returns a fresh
// access token. Account fields such as project_id are not part of the response
// and must be merged from the stored credential by the caller.
func RefreshAntigravityCredential(c *gin.Context) {
	var request antigravityRefreshTokenRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.SysError(common.LogText("failed to bind the antigravity refresh request: %s", err.Error()))
		common.ApiErrorT(c, "Invalid request")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), antigravityOAuthRequestTimeout)
	defer cancel()

	credential, err := service.RefreshAntigravityCredential(ctx, request.RefreshToken, request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to refresh antigravity credential: %s", err.Error()))
		common.ApiErrorT(c, "Failed to refresh credentials. Please try again later")
		return
	}
	common.ApiSuccessT(c, "Antigravity credentials refreshed", credential)
}

// RefreshAntigravityChannelCredential renews the stored credential of an
// existing Antigravity channel and persists it back to the channel key.
func RefreshAntigravityChannelCredential(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid channel ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), antigravityOAuthRequestTimeout)
	defer cancel()

	credential, _, err := service.RefreshAntigravityChannelCredential(ctx, channelID, service.AntigravityCredentialRefreshOptions{ResetCaches: true})
	if err != nil {
		common.SysError(common.LogText("failed to refresh antigravity channel credential: %s", err.Error()))
		common.ApiErrorT(c, "Failed to refresh credentials. Please try again later")
		return
	}
	common.ApiSuccessT(c, "Antigravity credentials refreshed", credential)
}

// GetAntigravityChannelQuota reports the models and remaining quota of an
// existing Antigravity channel.
func GetAntigravityChannelQuota(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid channel ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), antigravityOAuthRequestTimeout)
	defer cancel()

	quota, err := service.GetAntigravityChannelQuota(ctx, channelID)
	if err != nil {
		common.SysError(common.LogText("failed to query antigravity channel quota: %s", err.Error()))
		common.ApiErrorT(c, "Failed to query the Antigravity quota. Please try again later")
		return
	}
	common.ApiSuccessT(c, "Antigravity quota fetched", quota)
}

// GetAntigravityQuotaOverview returns the stored per-(account, model) quota
// snapshot that backs the console board: which accounts can still serve each
// model, and when the drained ones come back.
//
// It reads the snapshot table rather than the upstream catalogue, so the board
// can poll freely and still render when an account is unreachable.
func GetAntigravityQuotaOverview(c *gin.Context) {
	names, err := model.ChannelNamesByType(constant.ChannelTypeAntigravity)
	if err != nil {
		common.SysError(common.LogText("failed to list antigravity channels: %s", err.Error()))
		common.ApiErrorT(c, "Failed to load the Antigravity quota overview. Please try again later")
		return
	}
	served, err := model.ModelsServedByType(constant.ChannelTypeAntigravity)
	if err != nil {
		common.SysError(common.LogText("failed to list antigravity channel models: %s", err.Error()))
		common.ApiErrorT(c, "Failed to load the Antigravity quota overview. Please try again later")
		return
	}

	// Models that actually serve traffic come first; within that group, the ones
	// with drained accounts lead so an operator sees a problem without scrolling.
	visible, hidden := model.FilterQuotaSummaries(
		model.BuildChannelModelQuotaSummary(time.Now(), names),
		served,
		c.Query("all") == "1",
	)

	common.ApiSuccessT(c, "Antigravity quota overview", gin.H{
		"models":        visible,
		"hidden_models": hidden,
		"in_use_count":  len(served),
		"generated_at":  time.Now().Unix(),
	})
}

// GetAntigravityQuotaHistory returns the trend of the quota snapshot: one
// 30-minute bucket per model with the average remaining fraction and how many
// accounts still had quota.
func GetAntigravityQuotaHistory(c *gin.Context) {
	hours := 24
	if raw := c.Query("hours"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 168 {
			hours = parsed
		}
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	rows, err := model.ListChannelModelQuotaHistory(c.Query("model"), since, 0)
	if err != nil {
		common.SysError(common.LogText("failed to read antigravity quota history: %s", err.Error()))
		common.ApiErrorT(c, "Failed to load the Antigravity quota history. Please try again later")
		return
	}
	common.ApiSuccessT(c, "Antigravity quota history", gin.H{
		"models":       model.BuildQuotaHistorySeries(rows, 30*time.Minute),
		"hours":        hours,
		"generated_at": time.Now().Unix(),
	})
}

// GetChannelAccountQuotas returns the account-level quota snapshots used by
// channels that do not report per-model limits.
func GetChannelAccountQuotas(c *gin.Context) {
	channelType := 0
	if raw := c.Query("type"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			channelType = parsed
		}
	}
	rows, err := model.ListChannelAccountQuotaViews(channelType)
	if err != nil {
		common.SysError(common.LogText("failed to list channel account quotas: %s", err.Error()))
		common.ApiErrorT(c, "Failed to load the channel account quotas. Please try again later")
		return
	}
	common.ApiSuccessT(c, "Channel account quotas", gin.H{
		"accounts":     rows,
		"generated_at": time.Now().Unix(),
	})
}
