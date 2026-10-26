package geminiweb

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/geminiweb/engine"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBuildPromptFlattensRoles(t *testing.T) {
	messages := []dto.Message{
		{Role: "system", Content: "be terse"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "count to two"},
	}
	assert.Equal(t,
		"be terse\n\nhello\n\nAssistant: hi\n\ncount to two",
		buildPrompt(messages))
}

func TestBuildPromptKeepsMultimodalText(t *testing.T) {
	messages := []dto.Message{{
		Role: "user",
		Content: []any{
			map[string]any{"type": "text", "text": "describe"},
			map[string]any{"type": "image_url", "image_url": "data:image/png;base64,AAAA"},
		},
	}}
	assert.Equal(t, "describe", buildPrompt(messages))
}

func TestBuildPromptSkipsEmptyContent(t *testing.T) {
	messages := []dto.Message{
		{Role: "user", Content: "  "},
		{Role: "assistant", Content: "previous"},
	}
	assert.Equal(t, "Assistant: previous", buildPrompt(messages))
}

func TestEstimateUsageSumsBothSides(t *testing.T) {
	usage := estimateUsage("gemini-3.6-flash", "hello world", "the answer is 42")
	require.NotNil(t, usage)
	assert.Positive(t, usage.PromptTokens)
	assert.Positive(t, usage.CompletionTokens)
	assert.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
}

func TestGetModelListMatchesEngineCatalogue(t *testing.T) {
	adaptor := &Adaptor{}
	assert.ElementsMatch(t, engine.ModelNames(), adaptor.GetModelList())
	for _, name := range adaptor.GetModelList() {
		_, _, ok := engine.ResolveModel(name)
		assert.True(t, ok, "catalogue entry %q must resolve", name)
	}
}

func TestParseToolCallsExtractsFenceAndArguments(t *testing.T) {
	reply := "我先写文件。\n\n" + toolCallFence + "\n{\"name\": \"write_file\", \"arguments\": {\"path\": \"a.txt\", \"content\": \"hi\"}}\n" + codeFence + "\n"
	content, calls := parseToolCalls(reply)
	require.Len(t, calls, 1)
	assert.Equal(t, "write_file", calls[0].Function.Name)
	assert.JSONEq(t, "{\"path\":\"a.txt\",\"content\":\"hi\"}", calls[0].Function.Arguments)
	assert.NotEmpty(t, calls[0].ID)
	assert.Equal(t, "我先写文件。", content)
}

func TestParseToolCallsWithoutFenceReturnsText(t *testing.T) {
	content, calls := parseToolCalls("纯文本回答")
	assert.Empty(t, calls)
	assert.Equal(t, "纯文本回答", content)
}

func TestAugmentPromptWithToolsInjectsProtocol(t *testing.T) {
	specs := []toolSpec{{Name: "write_file", Description: "write a file", Parameters: map[string]any{"type": "object"}}}
	prompt := augmentPromptWithTools("hello", specs, nil)
	assert.Contains(t, prompt, toolCallFence)
	assert.Contains(t, prompt, "write_file")
	assert.Contains(t, prompt, "hello")
	assert.Contains(t, prompt, "Remember: the task cannot be completed")
	assert.Contains(t, prompt, "agent harness")
}

func TestAugmentPromptWithToolsHonoursToolChoiceNone(t *testing.T) {
	specs := []toolSpec{{Name: "write_file"}}
	assert.Equal(t, "hello", augmentPromptWithTools("hello", specs, "none"))
}

func TestAugmentPromptWithToolsForcesNamedTool(t *testing.T) {
	specs := []toolSpec{{Name: "first"}, {Name: "second"}}
	prompt := augmentPromptWithTools("go", specs, map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "second"},
	})
	assert.Contains(t, prompt, "You MUST call the tool \"second\"")
	assert.NotContains(t, prompt, "\"first\"")
}

func TestBuildPromptRendersToolTurns(t *testing.T) {
	callArgs := "{\"path\":\"a.txt\"}"
	encoded, err := common.Marshal([]dto.ToolCallResponse{{
		ID:       "call_1",
		Type:     "function",
		Function: dto.FunctionResponse{Name: "write_file", Arguments: callArgs},
	}})
	require.NoError(t, err)
	messages := []dto.Message{
		{Role: "user", Content: "写文件"},
		{Role: "assistant", ToolCalls: encoded},
		{Role: "tool", Name: ptr("write_file"), Content: "Process exited with code 0\nOutput:\n"},
	}
	prompt := buildPrompt(messages)
	assert.Contains(t, prompt, toolCallFence)
	assert.Contains(t, prompt, "write_file")
	assert.Contains(t, prompt, "executed it successfully (exit 0)")
}

func ptr(s string) *string {
	return &s
}

func TestChannelTypeMapsToGeminiWebAPIType(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeGeminiWeb)
	require.True(t, ok)
	assert.Equal(t, constant.APITypeGeminiWeb, apiType)
}

func TestRequirePromptRejectsEmptyText(t *testing.T) {
	assert.Error(t, requirePrompt(""))
	assert.NoError(t, requirePrompt("hello"))
}

func TestMergeSetCookieIgnoresDeletionDirectives(t *testing.T) {
	base := "__Secure-1PSID=psid; __Secure-1PSIDTS=keep; SIDCC=keepcc"
	got := engine.MergeSetCookie(base, []string{
		"__Secure-1PSIDTS=; Max-Age=0; Path=/",
		"SIDCC=; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Path=/",
	})
	assert.Equal(t, base, got)
}

func TestMergeSetCookieRefreshesExistingValues(t *testing.T) {
	base := "__Secure-1PSID=psid; __Secure-1PSIDTS=old; SAPISID=sapisid; SIDCC=oldcc"
	got := engine.MergeSetCookie(base, []string{
		"__Secure-1PSIDTS=new; Secure; HttpOnly; Path=/",
		"SIDCC=newcc; Path=/",
	})
	assert.Contains(t, got, "__Secure-1PSIDTS=new")
	assert.Contains(t, got, "SIDCC=newcc")
	assert.Contains(t, got, "SAPISID=sapisid")
	assert.NotContains(t, got, "__Secure-1PSIDTS=old")
}

// setupGeminiWebAccountDB points model.DB at a throwaway in-memory database so
// the account write path can be exercised without the real store.
func setupGeminiWebAccountDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&model.GeminiWebAccount{}))
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		_ = sqlDB.Close()
	})
}

func TestMarkAccountResultSkipsCleanSuccessesAndRecordsFailures(t *testing.T) {
	setupGeminiWebAccountDB(t)
	account := &model.GeminiWebAccount{
		ChannelId: 1,
		Label:     "test",
		Cookie:    "cookie",
		Status:    model.GeminiWebAccountEnabled,
	}
	require.NoError(t, model.DB.Create(account).Error)
	recent := common.GetTimestamp()
	require.NoError(t, model.DB.Model(&model.GeminiWebAccount{}).Where("id = ?", account.Id).
		Updates(map[string]any{"last_ok_at": recent, "updated_at": int64(333)}).Error)
	account.LastOkAt = recent

	// A success on a clean account stamped inside the heartbeat window writes nothing.
	markAccountResult(account, http.StatusOK, "")
	reloaded := &model.GeminiWebAccount{}
	require.NoError(t, model.DB.First(reloaded, account.Id).Error)
	assert.Equal(t, recent, reloaded.LastOkAt)
	assert.Equal(t, int64(333), reloaded.UpdatedAt)

	// A success on a clean account whose stamp is stale still refreshes it.
	stale := recent - geminiWebAccountHeartbeatSeconds - 1
	require.NoError(t, model.DB.Model(&model.GeminiWebAccount{}).Where("id = ?", account.Id).
		Updates(map[string]any{"last_ok_at": stale}).Error)
	account.LastOkAt = stale
	markAccountResult(account, http.StatusOK, "")
	require.NoError(t, model.DB.First(reloaded, account.Id).Error)
	assert.Greater(t, reloaded.LastOkAt, stale)

	// A success after a recorded failure clears it.
	require.NoError(t, model.DB.Model(&model.GeminiWebAccount{}).Where("id = ?", account.Id).
		Updates(map[string]any{"fail_count": 1, "last_error": "cookie expired"}).Error)
	account.FailCount = 1
	account.LastError = "cookie expired"
	markAccountResult(account, http.StatusOK, "")
	require.NoError(t, model.DB.First(reloaded, account.Id).Error)
	assert.Zero(t, reloaded.FailCount)
	assert.Empty(t, reloaded.LastError)
	assert.Greater(t, reloaded.LastOkAt, stale)

	// A rejected session is always recorded.
	markAccountResult(account, http.StatusUnauthorized, "cookie expired")
	require.NoError(t, model.DB.First(reloaded, account.Id).Error)
	assert.Equal(t, 1, reloaded.FailCount)
	assert.Equal(t, "cookie expired", reloaded.LastError)
}
