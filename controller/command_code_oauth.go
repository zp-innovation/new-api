package controller

import (
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	ccore "github.com/QuantumNous/new-api/pkg/commandcode"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	commandCodeOAuthRequestTimeout = 60 * time.Second
	commandCodeCallbackPath        = "/api/channel/oauth/command-code/callback"
)

type commandCodeStartLoginRequest struct {
	CallbackURL string `json:"callback_url"`
	Proxy       string `json:"proxy"`
}

type commandCodePollLoginRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Proxy     string `json:"proxy"`
}

type commandCodeValidateRequest struct {
	Credential string `json:"credential" binding:"required"`
	Proxy      string `json:"proxy"`
}

// StartCommandCodeLogin creates a Studio handoff for a loopback console.
func StartCommandCodeLogin(c *gin.Context) {
	var request commandCodeStartLoginRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	callbackURL := strings.TrimSpace(request.CallbackURL)
	if callbackURL == "" {
		callbackURL = commandCodeCallbackURL(c)
	}
	if !commandCodeLoopbackURL(callbackURL) {
		common.ApiErrorT(c, "Command Code browser login is only available when the console is opened on localhost; paste an API key instead")
		return
	}
	result, err := service.StartCommandCodeLogin(callbackURL, request.Proxy)
	if err != nil {
		common.ApiErrorT(c, "Failed to start the Command Code login: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	common.ApiSuccessT(c, "Command Code login started", result)
}

// CommandCodeCallback receives the API key Studio hands back to the browser.
// It is intentionally outside the admin auth middleware: the state token is the
// credential that authenticates this one-time handoff.
func CommandCodeCallback(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	if errorCode := strings.TrimSpace(c.Query("error")); errorCode != "" {
		_ = service.FailCommandCodeLogin(state, errorCode, c.Query("error_description"))
		commandCodeRenderCallbackPage(c, false, "Authorization failed", "Return to the console and start the login again.")
		return
	}
	if err := service.CompleteCommandCodeLogin(
		state,
		c.Query("apiKey"),
		c.Query("userId"),
		c.Query("userName"),
		c.Query("keyName"),
	); err != nil {
		commandCodeRenderCallbackPage(c, false, "Authorization failed", "The login could not be completed. Return to the console and try again.")
		return
	}
	commandCodeRenderCallbackPage(c, true, "Authorization complete", "You can close this tab and return to the console.")
}

// PollCommandCodeLogin performs one poll of an in-flight browser handoff.
func PollCommandCodeLogin(c *gin.Context) {
	var request commandCodePollLoginRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), commandCodeOAuthRequestTimeout)
	defer cancel()
	status, err := service.PollCommandCodeLogin(ctx, request.SessionID, request.Proxy)
	if err != nil {
		common.ApiErrorT(c, "Failed to poll the Command Code login: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	response := gin.H{"status": status.Status}
	if status.Credential != nil {
		encoded, err := status.Credential.Marshal()
		if err != nil {
			common.ApiErrorT(c, "Failed to encode the Command Code credential")
			return
		}
		response["credential"] = encoded
	}
	common.ApiSuccessT(c, "Command Code login status fetched", response)
}

// ValidateCommandCodeCredential validates a pasted API key or auth.json block.
func ValidateCommandCodeCredential(c *gin.Context) {
	var request commandCodeValidateRequest
	if err := bindOptionalRequestJSON(c, &request); err != nil {
		common.ApiErrorT(c, "Invalid request: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	credential, err := ccore.ParseCredential(request.Credential)
	if err != nil {
		common.ApiErrorT(c, "Invalid Command Code credential: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), commandCodeOAuthRequestTimeout)
	defer cancel()
	validated, err := service.ValidateCommandCodeCredential(ctx, credential, request.Proxy)
	if err != nil {
		common.ApiErrorT(c, "Failed to validate the Command Code credential: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	encoded, err := validated.Marshal()
	if err != nil {
		common.ApiErrorT(c, "Failed to encode the Command Code credential")
		return
	}
	common.ApiSuccessT(c, "Command Code credential validated", gin.H{
		"credential": encoded,
		"user_id":    validated.UserID,
		"user_name":  validated.UserName,
		"key_name":   validated.KeyName,
		"org_id":     validated.OrgID,
		"org_name":   validated.OrgName,
	})
}

// ValidateCommandCodeChannelCredential re-checks a stored channel credential.
func ValidateCommandCodeChannelCredential(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorT(c, "Invalid channel ID")
		return
	}
	ch, err := model.GetChannelById(channelID, true)
	if err != nil {
		common.ApiErrorT(c, "Failed to load the channel: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	if ch == nil {
		common.ApiErrorT(c, "Channel not found")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), commandCodeOAuthRequestTimeout)
	defer cancel()
	credential, err := service.ValidateCommandCodeChannelCredential(ctx, ch, ch.GetSetting().Proxy)
	if err != nil {
		common.ApiErrorT(c, "Failed to validate the Command Code channel credential: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	encoded, err := credential.Marshal()
	if err != nil {
		common.ApiErrorT(c, "Failed to encode the Command Code credential")
		return
	}
	common.ApiSuccessT(c, "Command Code channel credential validated", gin.H{"credential": encoded})
}

// GetCommandCodeChannelUsage returns the credits view for a channel.
func GetCommandCodeChannelUsage(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorT(c, "Invalid channel ID")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), commandCodeOAuthRequestTimeout)
	defer cancel()
	usage, err := service.GetCommandCodeChannelUsage(ctx, channelID)
	if err != nil {
		common.ApiErrorT(c, "Failed to fetch the Command Code usage: {{error}}", map[string]any{"error": err.Error()})
		return
	}
	common.ApiSuccessT(c, "Command Code usage fetched", usage)
}

func commandCodeCallbackURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.ToLower(strings.TrimSpace(strings.Split(forwarded, ",")[0]))
	}
	if scheme != "http" && scheme != "https" {
		scheme = "http"
	}
	return scheme + "://" + c.Request.Host + commandCodeCallbackPath
}

func commandCodeLoopbackURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func commandCodeRenderCallbackPage(c *gin.Context, success bool, title, detail string) {
	status := http.StatusOK
	if !success {
		status = http.StatusBadRequest
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(status, fmt.Sprintf(
		"<!doctype html><html><head><meta charset=\"utf-8\"><title>%s</title></head><body style=\"font-family: system-ui; padding: 40px; text-align: center;\"><h1>%s</h1><p>%s</p></body></html>",
		html.EscapeString(title),
		html.EscapeString(title),
		html.EscapeString(detail),
	))
}
