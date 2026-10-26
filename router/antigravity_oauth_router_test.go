package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/service/authz"
)

// TestAntigravityOAuthRoutesUseSensitiveWritePermission pins the permission of
// the credential endpoints: they hand out account credentials, so they must
// stay behind the sensitive-write permission rather than a plain read.
func TestAntigravityOAuthRoutesUseSensitiveWritePermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodPost, "/oauth/antigravity/auth-url", authz.ChannelSensitiveWrite, controller.GenerateAntigravityAuthURL)
	assertChannelRoutePermission(t, http.MethodPost, "/oauth/antigravity/exchange-code", authz.ChannelSensitiveWrite, controller.ExchangeAntigravityCode)
	assertChannelRoutePermission(t, http.MethodPost, "/oauth/antigravity/refresh-token", authz.ChannelSensitiveWrite, controller.RefreshAntigravityCredential)
}

// TestAntigravityChannelOperationRoutes pins the permission split between the
// credential refresh (sensitive write) and the quota lookup (read).
func TestAntigravityChannelOperationRoutes(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodPost, "/:id/antigravity/refresh", authz.ChannelSensitiveWrite, controller.RefreshAntigravityChannelCredential)
	assertChannelRoutePermission(t, http.MethodGet, "/:id/antigravity/quota", authz.ChannelRead, controller.GetAntigravityChannelQuota)
}
