package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGeminiWebChannelTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(
		&model.Channel{},
		&model.Ability{},
		&model.GeminiWebAccount{},
		&model.Log{},
		&model.User{},
	))
	model.DB = database
	model.LOG_DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedisEnabled
		_ = sqlDB.Close()
	})
}

func TestAddGeminiWebChannelCreatesAccountInSameRequest(t *testing.T) {
	setupGeminiWebChannelTest(t)
	const cookie = "__Secure-1PSID=psid; __Secure-1PSIDTS=psidts; SAPISID=sapisid; SID=sid"
	credential, err := common.Marshal(map[string]string{
		"cookie": cookie,
		"proxy":  "http://127.0.0.1:7890",
	})
	require.NoError(t, err)
	body, err := common.Marshal(map[string]any{
		"mode": "single",
		"channel": map[string]any{
			"type":   constant.ChannelTypeGeminiWeb,
			"name":   "Gemini Web",
			"key":    string(credential),
			"models": "gemini-4-argon",
			"group":  "default",
		},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Set("username", "root")
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	AddChannel(ctx)

	require.Contains(t, recorder.Body.String(), `"success":true`)
	var channel model.Channel
	require.NoError(t, model.DB.Where("name = ?", "Gemini Web").First(&channel).Error)
	assert.Empty(t, channel.Key)
	if channel.BaseURL != nil {
		assert.Empty(t, *channel.BaseURL, "native Gemini Web must not retain a sidecar URL")
	}
	var account model.GeminiWebAccount
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).First(&account).Error)
	assert.Equal(t, cookie, account.Cookie)
	assert.Equal(t, "http://127.0.0.1:7890", account.Proxy)
}

func TestAddGeminiWebChannelAcceptsRawCookie(t *testing.T) {
	setupGeminiWebChannelTest(t)
	const cookie = "__Secure-1PSID=psid; __Secure-1PSIDTS=psidts; SAPISID=sapisid; SID=sid"
	body, err := common.Marshal(map[string]any{
		"mode": "single",
		"channel": map[string]any{
			"type":   constant.ChannelTypeGeminiWeb,
			"name":   "Gemini Web raw",
			"key":    cookie,
			"models": "gemini-4-argon",
			"group":  "default",
		},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	AddChannel(ctx)

	require.Contains(t, recorder.Body.String(), `"success":true`)
	var channel model.Channel
	require.NoError(t, model.DB.Where("name = ?", "Gemini Web raw").First(&channel).Error)
	assert.Empty(t, channel.Key)
	var account model.GeminiWebAccount
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).First(&account).Error)
	assert.Equal(t, cookie, account.Cookie)
}

func TestAddGeminiWebChannelRejectsInvalidKeyAndBatchMode(t *testing.T) {
	setupGeminiWebChannelTest(t)
	for _, body := range []string{
		`{"mode":"single","channel":{"type":68,"name":"raw","key":"sk-gemini","models":"gemini-4-argon","group":"default"}}`,
		`{"mode":"batch","channel":{"type":68,"name":"batch","key":"__Secure-1PSID=psid; __Secure-1PSIDTS=psidts; SAPISID=sapisid","models":"gemini-4-argon","group":"default"}}`,
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", 1)
		ctx.Set("role", common.RoleRootUser)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		AddChannel(ctx)
		require.Contains(t, recorder.Body.String(), `"success":false`)
	}
}
