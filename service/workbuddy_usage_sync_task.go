package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

// workBuddyUsageSyncHandler rolls the consume logs up into the daily table once
// an hour. The rollup is cheap (two days of logs per channel) and idempotent, so
// a restart never double counts and a missed hour is caught up on the next pass.
type workBuddyUsageSyncHandler struct{}

func (workBuddyUsageSyncHandler) Type() string { return model.SystemTaskTypeWorkBuddyUsageSync }

func (workBuddyUsageSyncHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	if err := SyncWorkBuddyUsageDaily(ctx, time.Now()); err != nil {
		logger.LogWarn(ctx, common.LogText("workbuddy usage sync: rollup failed: %v", err))
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, err.Error())
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, map[string]any{"synced_at": time.Now().Unix()}, "")
}

func (workBuddyUsageSyncHandler) Enabled() bool { return true }

// HasPendingWork reports whether a WorkBuddy channel exists whose consume logs
// the hourly rollup would read.
func (workBuddyUsageSyncHandler) HasPendingWork() bool {
	return scheduledChannelsHaveWork(constant.ChannelTypeWorkBuddy)
}

// Interval keeps the rollup hourly. A shorter interval would re-sum the same
// two days for no benefit, since the numbers are absolute totals per day.
func (workBuddyUsageSyncHandler) Interval() time.Duration { return time.Hour }

func (workBuddyUsageSyncHandler) NewPayload() any { return map[string]any{} }

func init() {
	RegisterSystemTaskHandler(workBuddyUsageSyncHandler{})
}
