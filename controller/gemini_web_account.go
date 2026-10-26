package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// geminiWebAccountView is the console-facing view of an account. The cookie is
// an account credential and never leaves the server.
type geminiWebAccountView struct {
	Id         int    `json:"id"`
	ChannelId  int    `json:"channel_id"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	Note       string `json:"note"`
	FailCount  int    `json:"fail_count"`
	LastUsedAt int64  `json:"last_used_at"`
	LastOkAt   int64  `json:"last_ok_at"`
	LastError  string `json:"last_error"`
	CreatedAt  int64  `json:"created_at"`
	CookieSize int    `json:"cookie_size"`
}

func newGeminiWebAccountView(account *model.GeminiWebAccount) geminiWebAccountView {
	return geminiWebAccountView{
		Id:         account.Id,
		ChannelId:  account.ChannelId,
		Label:      account.Label,
		Status:     account.Status,
		Note:       account.Note,
		FailCount:  account.FailCount,
		LastUsedAt: account.LastUsedAt,
		LastOkAt:   account.LastOkAt,
		LastError:  account.LastError,
		CreatedAt:  account.CreatedAt,
		CookieSize: len(account.Cookie),
	}
}

// GetGeminiWebAccounts lists the account pool of one Gemini Web channel.
func GetGeminiWebAccounts(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	accounts, err := model.ListGeminiWebAccounts(channelId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]geminiWebAccountView, 0, len(accounts))
	for _, account := range accounts {
		views = append(views, newGeminiWebAccountView(account))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": views})
}

// AddGeminiWebAccount stores one captured cookie in the channel's pool.
func AddGeminiWebAccount(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var request struct {
		Label  string `json:"label"`
		Cookie string `json:"cookie"`
		Note   string `json:"note"`
		Proxy  string `json:"proxy"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	cookie, proxy, err := parseGeminiWebCredential(request.Cookie, request.Proxy, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	id, err := model.AddGeminiWebAccount(channelId, request.Label, cookie, request.Note, proxy)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": id}})
}

// DeleteGeminiWebAccount removes one account from the pool.
func DeleteGeminiWebAccount(c *gin.Context) {
	accountId, err := strconv.Atoi(c.Param("account_id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteGeminiWebAccount(accountId); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// SetGeminiWebAccountStatus enables or disables one account.
func SetGeminiWebAccountStatus(c *gin.Context) {
	accountId, err := strconv.Atoi(c.Param("account_id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var request struct {
		Status string `json:"status"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.SetGeminiWebAccountStatus(accountId, request.Status); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

type geminiWebCredential struct {
	Cookie string `json:"cookie"`
	Proxy  string `json:"proxy"`
}

// parseGeminiWebCredential accepts captured credential JSON or a full Cookie
// string. It always returns the raw Cookie value because the relay and cookie
// rotation task send it directly in the Cookie request header.
func parseGeminiWebCredential(raw, fallbackProxy string, requireJSON bool) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", geminiWebCredentialError()
	}
	credential := geminiWebCredential{Proxy: fallbackProxy}
	if strings.HasPrefix(raw, "{") {
		if err := common.UnmarshalJsonStr(raw, &credential); err != nil {
			return "", "", geminiWebCredentialError()
		}
	} else if requireJSON {
		return "", "", geminiWebCredentialError()
	} else {
		credential.Cookie = raw
	}
	cookie := strings.TrimSpace(credential.Cookie)
	if !looksLikeGeminiCookie(cookie) {
		return "", "", geminiWebCredentialError()
	}
	proxy := strings.TrimSpace(credential.Proxy)
	if proxy != "" {
		parsedProxy, err := common.ParseProxyURLStrict(proxy)
		if err != nil {
			return "", "", fmt.Errorf("invalid Gemini Web proxy: %w", err)
		}
		proxy = parsedProxy.String()
	}
	return cookie, proxy, nil
}

func geminiWebCredentialError() error {
	return common.NewMessage("Gemini Web credential must be a full cookie string, or JSON with a cookie field, containing __Secure-1PSID, __Secure-1PSIDTS, and SAPISID")
}

// looksLikeGeminiCookie rejects cookie blobs that carry no signed-in session,
// which is the most common import mistake. Parse cookie names instead of
// substring matching so mentioning a name in an unrelated value is rejected.
func looksLikeGeminiCookie(cookie string) bool {
	if strings.ContainsAny(cookie, "\r\n") {
		return false
	}
	found := make(map[string]bool, 3)
	for _, pair := range strings.Split(cookie, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		name = strings.TrimSpace(name)
		switch name {
		case "__Secure-1PSID", "__Secure-1PSIDTS", "SAPISID":
			found[name] = true
		}
	}
	return found["__Secure-1PSID"] && found["__Secure-1PSIDTS"] && found["SAPISID"]
}
