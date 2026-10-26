package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/service/authz"
)

func TestOpenCodeOAuthRoutesUseSensitiveWritePermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodPost, "/oauth/opencode/auth-url", authz.ChannelSensitiveWrite, controller.StartOpenCodeLogin)
	assertChannelRoutePermission(t, http.MethodPost, "/oauth/opencode/poll-token", authz.ChannelSensitiveWrite, controller.PollOpenCodeLogin)
	assertChannelRoutePermission(t, http.MethodPost, "/oauth/opencode/refresh-token", authz.ChannelSensitiveWrite, controller.RefreshOpenCodeCredential)
}

func TestOpenCodeChannelOperationRoutes(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodPost, "/:id/opencode/refresh", authz.ChannelSensitiveWrite, controller.RefreshOpenCodeChannelCredential)
	assertChannelRoutePermission(t, http.MethodGet, "/:id/opencode/usage", authz.ChannelRead, controller.GetOpenCodeChannelUsage)
}
