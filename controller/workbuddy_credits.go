package controller

import (
	"context"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// The CN realm walks several meter endpoints, so the panel gets the same
// budget as the other OAuth-backed channel queries.
const workBuddyCreditsRequestTimeout = 60 * time.Second

// GetWorkBuddyChannelCredits reports the credits, usage and daily check-in
// state of an existing WorkBuddy channel.
func GetWorkBuddyChannelCredits(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid channel ID")
		return
	}

	ch, err := model.GetChannelById(channelID, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if ch == nil {
		common.ApiErrorT(c, "Channel not found")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), workBuddyCreditsRequestTimeout)
	defer cancel()

	result, err := service.GetWorkBuddyChannelCredits(ctx, ch)
	if err != nil {
		common.SysError(common.LogText("failed to query workbuddy channel credits: %s", err.Error()))
		common.ApiErrorT(c, "Failed to query the WorkBuddy credits. Please try again later")
		return
	}
	common.ApiSuccessT(c, "WorkBuddy credits fetched", gin.H{
		"channel_id":    ch.Id,
		"channel_name":  ch.Name,
		"account_uid":   result.AccountUID,
		"account_name":  result.AccountName,
		"account_count": result.AccountCount,
		"credits":       result.Credits,
	})
}
