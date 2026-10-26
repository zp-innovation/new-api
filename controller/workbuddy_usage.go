package controller

import (
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// GetWorkBuddyUsage reports each WorkBuddy account's token consumption for the
// selected window. It reads the gateway's own consume logs, so no upstream call
// is made and the numbers are always available.
func GetWorkBuddyUsage(c *gin.Context) {
	channelIds, err := parseWorkBuddyChannelIds(c.Query("channel_ids"))
	if err != nil {
		common.SysError(common.LogText("workbuddy usage: parse channel ids: %s", err.Error()))
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	models := make([]string, 0, 4)
	for part := range strings.SplitSeq(c.Query("models"), ",") {
		if model := strings.TrimSpace(part); model != "" {
			models = append(models, model)
		}
	}
	report, err := service.SummarizeWorkBuddyUsage(c.Request.Context(), service.WorkBuddyUsageQuery{
		ChannelIds: channelIds,
		Models:     models,
		Window:     c.Query("window"),
		Now:        time.Now(),
	})
	if err != nil {
		common.SysError(common.LogText("workbuddy usage: summarize usage: %s", err.Error()))
		common.ApiErrorT(c, "Failed to fetch usage")
		return
	}
	common.ApiSuccess(c, report)
}

// GetWorkBuddyUsageHistory returns the stored daily rollup for a short trend.
func GetWorkBuddyUsageHistory(c *gin.Context) {
	channelIds, err := parseWorkBuddyChannelIds(c.Query("channel_ids"))
	if err != nil {
		common.SysError(common.LogText("workbuddy usage: parse channel ids: %s", err.Error()))
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	days, _ := strconv.Atoi(c.Query("days"))
	if days <= 0 || days > 90 {
		days = 14
	}
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	rows, err := service.ListWorkBuddyUsageHistory(channelIds, since)
	if err != nil {
		common.SysError(common.LogText("workbuddy usage: list usage history: %s", err.Error()))
		common.ApiErrorT(c, "Failed to fetch usage")
		return
	}
	common.ApiSuccess(c, gin.H{"days": days, "rows": rows})
}
