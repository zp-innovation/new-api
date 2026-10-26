package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const workBuddyOAuthRequestTimeout = 60 * time.Second

type workBuddyStartLoginRequest struct {
	Realm   string `json:"realm"`
	Product string `json:"product"`
	Proxy   string `json:"proxy"`
}

type workBuddyPollLoginRequest struct {
	State string `json:"state" binding:"required"`
	Proxy string `json:"proxy"`
}

type workBuddyRefreshTokenRequest struct {
	Credential string `json:"credential" binding:"required"`
	Proxy      string `json:"proxy"`
}

// StartWorkBuddyLogin opens a device-authorization login and returns the URL
// the operator has to visit. WorkBuddy authorizes through a device code rather
// than a redirect, so there is no code to paste back: the panel polls instead.
func StartWorkBuddyLogin(c *gin.Context) {
	var request workBuddyStartLoginRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), workBuddyOAuthRequestTimeout)
	defer cancel()

	result, err := service.StartWorkBuddyLogin(ctx, request.Realm, request.Product, request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to start workbuddy login: %s", err.Error()))
		common.ApiErrorT(c, "Failed to start the WorkBuddy login: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	common.ApiSuccessT(c, "WorkBuddy login started", result)
}

// PollWorkBuddyLogin performs one poll of an in-flight login and, once the
// operator finished in the browser, returns the channel key to store.
//
// The credential is handed over as the compact single-line JSON the channel key
// column requires: the column is split on newlines for multi-account channels,
// so a pretty-printed credential would be read back as several broken keys.
func PollWorkBuddyLogin(c *gin.Context) {
	var request workBuddyPollLoginRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), workBuddyOAuthRequestTimeout)
	defer cancel()

	status, err := service.PollWorkBuddyLogin(ctx, request.State, request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to poll workbuddy login: %s", err.Error()))
		common.ApiErrorT(c, "Failed to poll the WorkBuddy login: {{error}}", map[string]any{"error": err.Error()})
		return
	}

	response := gin.H{"status": status.Status}
	if status.Credential != nil {
		encoded, err := status.Credential.Marshal()
		if err != nil {
			common.SysError(common.LogText("failed to encode workbuddy credential: %s", err.Error()))
			common.ApiErrorT(c, "Failed to encode the WorkBuddy credential")
			return
		}
		response["credential"] = encoded
	}

	if status.Status == service.WorkBuddyLoginReady {
		common.ApiSuccessT(c, "WorkBuddy credentials created", response)
		return
	}
	common.ApiSuccessT(c, "WorkBuddy login status fetched", response)
}

// RefreshWorkBuddyCredential renews a credential and returns it in the same
// compact form. The upstream rotates the refresh token, so the caller has to
// store the returned value; the old one is already spent.
func RefreshWorkBuddyCredential(c *gin.Context) {
	var request workBuddyRefreshTokenRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), workBuddyOAuthRequestTimeout)
	defer cancel()

	credential, err := service.RefreshWorkBuddyCredential(ctx, request.Credential, request.Proxy)
	if err != nil {
		common.SysError(common.LogText("failed to refresh workbuddy credential: %s", err.Error()))
		common.ApiErrorT(c, "Failed to refresh the WorkBuddy credential: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	encoded, err := credential.Marshal()
	if err != nil {
		common.SysError(common.LogText("failed to encode workbuddy credential: %s", err.Error()))
		common.ApiErrorT(c, "Failed to encode the WorkBuddy credential")
		return
	}
	common.ApiSuccessT(c, "WorkBuddy credentials refreshed", gin.H{"credential": encoded})
}
