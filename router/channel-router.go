package router

import (
	"net/http"
	"path"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

type permissionRoute struct {
	method     string
	path       string
	permission authz.Permission
	handler    gin.HandlerFunc
}

// handlePermissionRoute registers a Casbin-guarded route and declares the same
// permission as the access token scope that route requires.
func handlePermissionRoute(group *gin.RouterGroup, method, relativePath string, permission authz.Permission, handlers ...gin.HandlerFunc) {
	middleware.DeclareAccessTokenPermissionRoute(method, joinPaths(group.BasePath(), relativePath), permission)
	group.Handle(method, relativePath, append([]gin.HandlerFunc{middleware.RequirePermission(permission)}, handlers...)...)
}

// joinPaths matches gin's route path joining, which keeps a trailing slash
// from the relative path, so declared keys equal c.FullPath().
func joinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}
	finalPath := path.Join(absolutePath, relativePath)
	if relativePath[len(relativePath)-1] == '/' && finalPath[len(finalPath)-1] != '/' {
		return finalPath + "/"
	}
	return finalPath
}

func registerChannelRoutes(apiRouter *gin.RouterGroup) {
	channelRoute := apiRouter.Group("/channel")
	// The Studio handoff redirects the browser here without a console session;
	// the one-time state token authenticates the callback instead.
	channelRoute.GET("/oauth/command-code/callback", middleware.DisableCache(), controller.CommandCodeCallback)
	channelRoute.Use(middleware.AdminAuth())

	channelRoute.POST("/:id/key",
		middleware.RootAuth(),
		middleware.CriticalRateLimit(),
		middleware.DisableCache(),
		middleware.SecureVerificationRequired(),
		controller.GetChannelKey,
	)

	for _, route := range channelPermissionRoutes {
		handlePermissionRoute(channelRoute, route.method, route.path, route.permission, route.handler)
	}
}

var channelPermissionRoutes = []permissionRoute{
	{method: http.MethodGet, path: "/priority/order", permission: authz.ChannelRead, handler: controller.GetChannelPriorityOrder},
	{method: http.MethodPost, path: "/priority/reorder", permission: authz.ChannelWrite, handler: controller.ReorderChannelPriority},
	{method: http.MethodGet, path: "/", permission: authz.ChannelRead, handler: controller.GetAllChannels},
	{method: http.MethodGet, path: "/search", permission: authz.ChannelRead, handler: controller.SearchChannels},
	{method: http.MethodGet, path: "/models", permission: authz.ChannelRead, handler: controller.ChannelListModels},
	{method: http.MethodGet, path: "/default_base_urls", permission: authz.ChannelRead, handler: controller.GetChannelDefaultBaseURLs},
	{method: http.MethodGet, path: "/models_enabled", permission: authz.ChannelRead, handler: controller.EnabledListModels},
	{method: http.MethodGet, path: "/ops", permission: authz.ChannelRead, handler: controller.GetChannelOps},
	{method: http.MethodGet, path: "/:id", permission: authz.ChannelRead, handler: controller.GetChannel},
	{method: http.MethodGet, path: "/test", permission: authz.ChannelOperate, handler: controller.TestAllChannels},
	{method: http.MethodGet, path: "/test/:id", permission: authz.ChannelOperate, handler: controller.TestChannel},
	{method: http.MethodGet, path: "/update_balance", permission: authz.ChannelOperate, handler: controller.UpdateAllChannelsBalance},
	{method: http.MethodGet, path: "/update_balance/:id", permission: authz.ChannelOperate, handler: controller.UpdateChannelBalance},
	{method: http.MethodPost, path: "/", permission: authz.ChannelSensitiveWrite, handler: controller.AddChannel},
	{method: http.MethodPut, path: "/", permission: authz.ChannelWrite, handler: controller.UpdateChannel},
	{method: http.MethodPost, path: "/status/batch", permission: authz.ChannelOperate, handler: controller.BatchUpdateChannelStatus},
	{method: http.MethodPost, path: "/:id/status", permission: authz.ChannelOperate, handler: controller.UpdateChannelStatus},
	{method: http.MethodGet, path: "/:id/gemini-web/accounts", permission: authz.ChannelRead, handler: controller.GetGeminiWebAccounts},
	{method: http.MethodPost, path: "/:id/gemini-web/accounts", permission: authz.ChannelSensitiveWrite, handler: controller.AddGeminiWebAccount},
	{method: http.MethodPost, path: "/:id/gemini-web/accounts/:account_id/status", permission: authz.ChannelOperate, handler: controller.SetGeminiWebAccountStatus},
	{method: http.MethodDelete, path: "/:id/gemini-web/accounts/:account_id", permission: authz.ChannelSensitiveWrite, handler: controller.DeleteGeminiWebAccount},
	{method: http.MethodDelete, path: "/disabled", permission: authz.ChannelSensitiveWrite, handler: controller.DeleteDisabledChannel},
	{method: http.MethodPost, path: "/tag/disabled", permission: authz.ChannelOperate, handler: controller.DisableTagChannels},
	{method: http.MethodPost, path: "/tag/enabled", permission: authz.ChannelOperate, handler: controller.EnableTagChannels},
	{method: http.MethodPut, path: "/tag", permission: authz.ChannelWrite, handler: controller.EditTagChannels},
	{method: http.MethodDelete, path: "/:id", permission: authz.ChannelSensitiveWrite, handler: controller.DeleteChannel},
	{method: http.MethodPost, path: "/batch", permission: authz.ChannelSensitiveWrite, handler: controller.DeleteChannelBatch},
	{method: http.MethodPost, path: "/fix", permission: authz.ChannelOperate, handler: controller.FixChannelsAbilities},
	{method: http.MethodGet, path: "/fetch_models/:id", permission: authz.ChannelOperate, handler: controller.FetchUpstreamModels},
	{method: http.MethodGet, path: "/:id/vllm/status", permission: authz.ChannelRead, handler: controller.GetVLLMChannelStatus},
	{method: http.MethodGet, path: "/:id/sglang/status", permission: authz.ChannelRead, handler: controller.GetSGLangChannelStatus},
	{method: http.MethodPost, path: "/fetch_models", permission: authz.ChannelSensitiveWrite, handler: controller.FetchModels},
	{method: http.MethodPost, path: "/:id/codex/refresh", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshCodexChannelCredential},
	{method: http.MethodGet, path: "/:id/codex/usage", permission: authz.ChannelRead, handler: controller.GetCodexChannelUsage},
	{method: http.MethodGet, path: "/:id/codex/usage/reset-credits", permission: authz.ChannelRead, handler: controller.GetCodexChannelRateLimitResetCredits},
	{method: http.MethodPost, path: "/:id/codex/usage/reset", permission: authz.ChannelOperate, handler: controller.ResetCodexChannelUsage},
	{method: http.MethodPost, path: "/ollama/pull", permission: authz.ChannelSensitiveWrite, handler: controller.OllamaPullModel},
	{method: http.MethodPost, path: "/ollama/pull/stream", permission: authz.ChannelSensitiveWrite, handler: controller.OllamaPullModelStream},
	{method: http.MethodDelete, path: "/ollama/delete", permission: authz.ChannelSensitiveWrite, handler: controller.OllamaDeleteModel},
	{method: http.MethodGet, path: "/ollama/version/:id", permission: authz.ChannelSensitiveWrite, handler: controller.OllamaVersion},
	{method: http.MethodPost, path: "/batch/tag", permission: authz.ChannelWrite, handler: controller.BatchSetChannelTag},
	{method: http.MethodGet, path: "/tag/models", permission: authz.ChannelRead, handler: controller.GetTagModels},
	{method: http.MethodPost, path: "/copy/:id", permission: authz.ChannelSensitiveWrite, handler: controller.CopyChannel},
	{method: http.MethodPost, path: "/multi_key/manage", permission: authz.ChannelOperate, handler: controller.ManageMultiKeys},
	{method: http.MethodPost, path: "/upstream_updates/apply", permission: authz.ChannelWrite, handler: controller.ApplyChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/apply_all", permission: authz.ChannelWrite, handler: controller.ApplyAllChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/detect", permission: authz.ChannelOperate, handler: controller.DetectChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/detect_all", permission: authz.ChannelOperate, handler: controller.DetectAllChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/oauth/antigravity/auth-url", permission: authz.ChannelSensitiveWrite, handler: controller.GenerateAntigravityAuthURL},
	{method: http.MethodPost, path: "/oauth/antigravity/exchange-code", permission: authz.ChannelSensitiveWrite, handler: controller.ExchangeAntigravityCode},
	{method: http.MethodPost, path: "/oauth/antigravity/refresh-token", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshAntigravityCredential},
	{method: http.MethodPost, path: "/oauth/codex/auth-url", permission: authz.ChannelSensitiveWrite, handler: controller.GenerateCodexAuthURL},
	{method: http.MethodPost, path: "/oauth/codex/exchange-code", permission: authz.ChannelSensitiveWrite, handler: controller.ExchangeCodexCode},
	{method: http.MethodPost, path: "/:id/antigravity/refresh", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshAntigravityChannelCredential},
	{method: http.MethodGet, path: "/:id/antigravity/quota", permission: authz.ChannelRead, handler: controller.GetAntigravityChannelQuota},
	{method: http.MethodGet, path: "/antigravity/quotas", permission: authz.ChannelRead, handler: controller.GetAntigravityQuotaOverview},
	{method: http.MethodGet, path: "/antigravity/quotas/history", permission: authz.ChannelRead, handler: controller.GetAntigravityQuotaHistory}, {method: http.MethodGet, path: "/account-quotas", permission: authz.ChannelRead, handler: controller.GetChannelAccountQuotas},
	{method: http.MethodGet, path: "/:id/workbuddy/credits", permission: authz.ChannelRead, handler: controller.GetWorkBuddyChannelCredits},
	{method: http.MethodPost, path: "/:id/workbuddy/checkin", permission: authz.ChannelOperate, handler: controller.CheckinWorkBuddyChannel},
	{method: http.MethodGet, path: "/workbuddy/accounts", permission: authz.ChannelRead, handler: controller.GetWorkBuddyAccounts},
	{method: http.MethodPost, path: "/workbuddy/accounts/tasks", permission: authz.ChannelOperate, handler: controller.SetWorkBuddyAccountTasks},
	{method: http.MethodPost, path: "/workbuddy/run", permission: authz.ChannelOperate, handler: controller.RunWorkBuddyTasks},
	{method: http.MethodGet, path: "/workbuddy/runs", permission: authz.ChannelRead, handler: controller.GetWorkBuddyTaskRuns},
	{method: http.MethodGet, path: "/workbuddy/usage", permission: authz.ChannelRead, handler: controller.GetWorkBuddyUsage},
	{method: http.MethodGet, path: "/workbuddy/usage/history", permission: authz.ChannelRead, handler: controller.GetWorkBuddyUsageHistory},
	{method: http.MethodPost, path: "/oauth/workbuddy/auth-url", permission: authz.ChannelSensitiveWrite, handler: controller.StartWorkBuddyLogin},
	{method: http.MethodPost, path: "/oauth/workbuddy/poll-token", permission: authz.ChannelSensitiveWrite, handler: controller.PollWorkBuddyLogin},
	{method: http.MethodPost, path: "/oauth/workbuddy/refresh-token", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshWorkBuddyCredential},
	{method: http.MethodPost, path: "/oauth/opencode/auth-url", permission: authz.ChannelSensitiveWrite, handler: controller.StartOpenCodeLogin},
	{method: http.MethodPost, path: "/oauth/opencode/poll-token", permission: authz.ChannelSensitiveWrite, handler: controller.PollOpenCodeLogin},
	{method: http.MethodPost, path: "/oauth/opencode/refresh-token", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshOpenCodeCredential},
	{method: http.MethodPost, path: "/:id/opencode/refresh", permission: authz.ChannelSensitiveWrite, handler: controller.RefreshOpenCodeChannelCredential},
	{method: http.MethodGet, path: "/:id/opencode/usage", permission: authz.ChannelRead, handler: controller.GetOpenCodeChannelUsage},
	{method: http.MethodPost, path: "/oauth/command-code/auth-url", permission: authz.ChannelSensitiveWrite, handler: controller.StartCommandCodeLogin},
	{method: http.MethodPost, path: "/oauth/command-code/poll-token", permission: authz.ChannelSensitiveWrite, handler: controller.PollCommandCodeLogin},
	{method: http.MethodPost, path: "/oauth/command-code/validate", permission: authz.ChannelSensitiveWrite, handler: controller.ValidateCommandCodeCredential},
	{method: http.MethodPost, path: "/:id/command-code/validate", permission: authz.ChannelSensitiveWrite, handler: controller.ValidateCommandCodeChannelCredential},
	{method: http.MethodGet, path: "/:id/command-code/usage", permission: authz.ChannelRead, handler: controller.GetCommandCodeChannelUsage},
}
