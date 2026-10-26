package router

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDefaultBaseURLsRequireReadPermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodGet, "/default_base_urls", authz.ChannelRead, controller.GetChannelDefaultBaseURLs)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerChannelRoutes(engine.Group("/api"))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/channel/default_base_urls", nil))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestChannelStatusRoutesUseExpectedPermissions(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodGet, "/priority/order", authz.ChannelRead, controller.GetChannelPriorityOrder)
	assertChannelRoutePermission(t, http.MethodPost, "/priority/reorder", authz.ChannelWrite, controller.ReorderChannelPriority)
	assertChannelRoutePermission(t, http.MethodGet, "/:id/vllm/status", authz.ChannelRead, controller.GetVLLMChannelStatus)
	assertChannelRoutePermission(t, http.MethodGet, "/:id/sglang/status", authz.ChannelRead, controller.GetSGLangChannelStatus)
	assertChannelRoutePermission(t, http.MethodPost, "/:id/status", authz.ChannelOperate, controller.UpdateChannelStatus)
	assertChannelRoutePermission(t, http.MethodPost, "/status/batch", authz.ChannelOperate, controller.BatchUpdateChannelStatus)
	assertChannelRoutePermission(t, http.MethodPut, "/", authz.ChannelWrite, controller.UpdateChannel)
}

func TestChannelDeleteRoutesUseSensitiveWritePermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodDelete, "/:id", authz.ChannelSensitiveWrite, controller.DeleteChannel)
	assertChannelRoutePermission(t, http.MethodPost, "/batch", authz.ChannelSensitiveWrite, controller.DeleteChannelBatch)
	assertChannelRoutePermission(t, http.MethodDelete, "/disabled", authz.ChannelSensitiveWrite, controller.DeleteDisabledChannel)
	assertChannelRoutePermission(t, http.MethodPut, "/", authz.ChannelWrite, controller.UpdateChannel)
	assertChannelRoutePermission(t, http.MethodPut, "/tag", authz.ChannelWrite, controller.EditTagChannels)
	assertChannelRoutePermission(t, http.MethodPost, "/batch/tag", authz.ChannelWrite, controller.BatchSetChannelTag)
}

// The task endpoints mutate upstream state, so running them needs the operate
// permission while the account table and the history stay read-only.
func TestWorkBuddyTaskRoutesUseExpectedPermissions(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodGet, "/workbuddy/accounts", authz.ChannelRead, controller.GetWorkBuddyAccounts)
	assertChannelRoutePermission(t, http.MethodPost, "/workbuddy/accounts/tasks", authz.ChannelOperate, controller.SetWorkBuddyAccountTasks)
	assertChannelRoutePermission(t, http.MethodPost, "/workbuddy/run", authz.ChannelOperate, controller.RunWorkBuddyTasks)
	assertChannelRoutePermission(t, http.MethodGet, "/workbuddy/runs", authz.ChannelRead, controller.GetWorkBuddyTaskRuns)
	assertChannelRoutePermission(t, http.MethodPost, "/:id/workbuddy/checkin", authz.ChannelOperate, controller.CheckinWorkBuddyChannel)
}

func TestChannelStatusRoutesRegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api")

	require.NotPanics(t, func() {
		registerChannelRoutes(api)
	})
}

func assertChannelRoutePermission(t *testing.T, method string, path string, permission authz.Permission, handler any) {
	t.Helper()
	for _, route := range channelPermissionRoutes {
		if route.method == method && route.path == path {
			assert.Equal(t, permission, route.permission)
			assert.Equal(t, reflect.ValueOf(handler).Pointer(), reflect.ValueOf(route.handler).Pointer())
			return
		}
	}
	t.Fatalf("route %s %s not found", method, path)
}
