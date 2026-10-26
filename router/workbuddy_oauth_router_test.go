package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/service/authz"
)

// TestWorkBuddyOAuthRoutesUseSensitiveWritePermission pins the permission of
// the login endpoints: a completed poll hands out an account credential, so the
// routes must stay behind the sensitive-write permission rather than a read.
func TestWorkBuddyOAuthRoutesUseSensitiveWritePermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodPost, "/oauth/workbuddy/auth-url", authz.ChannelSensitiveWrite, controller.StartWorkBuddyLogin)
	assertChannelRoutePermission(t, http.MethodPost, "/oauth/workbuddy/poll-token", authz.ChannelSensitiveWrite, controller.PollWorkBuddyLogin)
	assertChannelRoutePermission(t, http.MethodPost, "/oauth/workbuddy/refresh-token", authz.ChannelSensitiveWrite, controller.RefreshWorkBuddyCredential)
}

// TestWorkBuddyCreditsRouteUsesReadPermission pins the balance query behind the
// read permission: it only reads the account's credit state.
func TestWorkBuddyCreditsRouteUsesReadPermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodGet, "/:id/workbuddy/credits", authz.ChannelRead, controller.GetWorkBuddyChannelCredits)
}
