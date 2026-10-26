package service

import (
	"context"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// workBuddyTaskPayload is the selection a queued pass carries. An empty
// selection means every account and every action the settings enable.
type workBuddyTaskPayload struct {
	Trigger    string   `json:"trigger,omitempty"`
	ChannelIds []int    `json:"channel_ids,omitempty"`
	Realm      string   `json:"realm,omitempty"`
	AccountUid string   `json:"account_uid,omitempty"`
	Actions    []string `json:"actions,omitempty"`
}

// workBuddyTaskHandler is the scheduled pass that drives every enabled WorkBuddy
// action. The scheduler treats a handler as due when Interval() has elapsed, so
// the handler reports itself enabled only inside the configured hours: that keeps
// a fixed local schedule without anchoring a 24 hour interval to process start.
type workBuddyTaskHandler struct{}

func (workBuddyTaskHandler) Type() string { return model.SystemTaskTypeWorkBuddyTask }

func (workBuddyTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	setting := operation_setting.GetWorkBuddyTaskSetting()
	if setting == nil || !setting.Enabled {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded,
			map[string]any{"skipped": "workbuddy tasks are disabled"}, "")
		return
	}
	trimWorkBuddyTaskHistory(setting.RunRetentionDays)

	// A manual batch carries its own selection; a scheduled pass leaves both
	// fields empty and takes whatever the settings enable.
	var payload workBuddyTaskPayload
	if task != nil && task.Payload != "" {
		if err := common.UnmarshalJsonStr(task.Payload, &payload); err != nil {
			logger.LogWarn(ctx, common.LogText("workbuddy task: decode payload: %s", err.Error()))
		}
	}
	// A manual batch is queued with its own trigger; only a missing value falls
	// back to the scheduler. Forcing the scheduler here made every manual batch
	// honour the "already done today" gates and skip the accounts it was
	// explicitly asked to run.
	trigger := strings.TrimSpace(payload.Trigger)
	if trigger == "" {
		trigger = WorkBuddyTriggerScheduler
	}
	summary, err := RunWorkBuddyTasks(ctx, WorkBuddyTaskOptions{
		ChannelIds: payload.ChannelIds,
		Realm:      payload.Realm,
		AccountUid: payload.AccountUid,
		Actions:    payload.Actions,
		Trigger:    trigger,
	})
	if err != nil {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, err.Error())
		return
	}
	logger.LogInfo(context.Background(), common.LogText(
		"workbuddy task pass finished: accounts=%d actions=%d skipped=%d credit=%v",
		summary.Accounts, summary.Actions, len(summary.Skipped), summary.CreditDelta))
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, summary, "")
}

func (workBuddyTaskHandler) Enabled() bool {
	setting := operation_setting.GetWorkBuddyTaskSetting()
	if setting == nil || !setting.Enabled {
		return false
	}
	hour := time.Now().Hour()
	if setting.AutoNightCat && hour == setting.NightHour {
		return true
	}
	cnDue := len(EnabledWorkBuddyActions(setting, true)) > 0 && setting.RunsAt(hour, true)
	intlDue := len(EnabledWorkBuddyActions(setting, false)) > 0 && setting.RunsAt(hour, false)
	return cnDue || intlDue
}

// HasPendingWork reports whether the scheduled pass has any channel to visit.
// Enabled() already folds in the configured hours, so this only removes the
// empty passes a deployment without WorkBuddy channels would otherwise record.
func (workBuddyTaskHandler) HasPendingWork() bool {
	return scheduledChannelsHaveWork(constant.ChannelTypeWorkBuddy)
}

// Interval keeps one scheduled pass per hour, which is what the configured hours
// are: a second pass inside the same hour is never due.
func (workBuddyTaskHandler) Interval() time.Duration { return time.Hour }

func (workBuddyTaskHandler) NewPayload() any {
	return map[string]any{"trigger": WorkBuddyTriggerScheduler}
}

// trimWorkBuddyTaskHistory drops history rows older than the retention window.
func trimWorkBuddyTaskHistory(retentionDays int) {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	if _, err := model.DeleteWorkBuddyTaskRunsBefore(cutoff); err != nil {
		logger.LogWarn(context.Background(), common.LogText("workbuddy task: trim history: %s", err.Error()))
	}
}

func init() {
	RegisterSystemTaskHandler(workBuddyTaskHandler{})
}
