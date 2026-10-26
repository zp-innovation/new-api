package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// workBuddyManualTimeout bounds one synchronous manual run. A batch run is
// queued as a system task instead, so only a single account runs inline.
const workBuddyManualTimeout = 5 * time.Minute

// WorkBuddyAccountView is one row of the console's account table. The flags are
// resolved server side so the table does not have to know the daily gates.
type WorkBuddyAccountView struct {
	ChannelId     int    `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	ChannelStatus int    `json:"channel_status"`
	AccountUid    string `json:"account_uid"`
	Nickname      string `json:"nickname"`
	Realm         string `json:"realm"`
	Product       string `json:"product"`
	TasksEnabled  bool   `json:"tasks_enabled"`

	CheckedInToday      bool `json:"checked_in_today"`
	DailyChatToday      bool `json:"daily_chat_today"`
	ActivityReportToday bool `json:"activity_report_today"`

	LastCheckinAt        int64   `json:"last_checkin_at"`
	LastDailyChatAt      int64   `json:"last_daily_chat_at"`
	LastActivityReportAt int64   `json:"last_activity_report_at"`
	LastRunAt            int64   `json:"last_run_at"`
	LastTask             string  `json:"last_task"`
	LastStatus           string  `json:"last_status"`
	LastMessage          string  `json:"last_message"`
	StreakDays           int     `json:"streak_days"`
	DailyCredit          float64 `json:"daily_credit"`
	TodayCredit          float64 `json:"today_credit"`
	RemainCredit         float64 `json:"remain_credit"`
}

// GetWorkBuddyAccounts reports every WorkBuddy account and its task state. The
// rows are derived from the channels, so an account that was just imported shows
// up without any registration step.
func GetWorkBuddyAccounts(c *gin.Context) {
	channelIds, err := parseWorkBuddyChannelIds(c.Query("channel_ids"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	realm := service.NormalizeWorkBuddyRealm(c.Query("realm"))
	// One read serves both the table and the headline numbers: the rows are
	// filtered in memory so the summaries always cover every realm.
	accounts, err := service.ListWorkBuddyAccounts(channelIds, service.WorkBuddyRealmAll)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := time.Now()
	views := make([]WorkBuddyAccountView, 0, len(accounts))
	for _, account := range accounts {
		if !service.WorkBuddyAccountMatchesRealm(account, realm) {
			continue
		}
		views = append(views, workBuddyAccountView(account, now))
	}
	settings := operation_setting.GetWorkBuddyTaskSetting()
	common.ApiSuccess(c, gin.H{
		"accounts":  views,
		"settings":  settings,
		"realm":     realm,
		"summaries": service.SummarizeWorkBuddyAccounts(accounts, now),
	})
}

func workBuddyAccountView(account *service.WorkBuddyAccount, now time.Time) WorkBuddyAccountView {
	identity := account.Credential.Identity()
	view := WorkBuddyAccountView{
		ChannelId:     account.ChannelId,
		ChannelName:   account.ChannelName,
		ChannelStatus: account.ChannelStatus,
		AccountUid:    account.UID(),
		Nickname:      account.Nickname(),
		Realm:         string(identity.Realm),
		Product:       string(identity.Product),
		TasksEnabled:  account.TasksEnabled(),
	}
	if account.State == nil {
		return view
	}
	state := account.State
	view.LastCheckinAt = state.LastCheckinAt
	view.LastDailyChatAt = state.LastDailyChatAt
	view.LastActivityReportAt = state.LastActivityReportAt
	view.LastRunAt = state.LastRunAt
	view.LastTask = state.LastTask
	view.LastStatus = state.LastStatus
	view.LastMessage = state.LastMessage
	view.StreakDays = state.StreakDays
	view.DailyCredit = state.DailyCredit
	view.TodayCredit = state.TodayCredit
	view.RemainCredit = state.RemainCredit
	today := now.Format("2006-01-02")
	isToday := func(unix int64) bool {
		return unix > 0 && time.Unix(unix, 0).Format("2006-01-02") == today
	}
	view.CheckedInToday = isToday(state.LastCheckinAt)
	view.DailyChatToday = isToday(state.LastDailyChatAt)
	view.ActivityReportToday = isToday(state.LastActivityReportAt)
	return view
}

type workBuddyRunRequest struct {
	ChannelIds []int    `json:"channel_ids"`
	AccountUid string   `json:"account_uid"`
	Realm      string   `json:"realm"`
	Actions    []string `json:"actions"`
}

// RunWorkBuddyTasks runs the selected actions. A single account runs inline so
// the console gets the result immediately; anything wider is queued as a system
// task, because a batch would outlive an HTTP request.
func RunWorkBuddyTasks(c *gin.Context) {
	var request workBuddyRunRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validateWorkBuddyActions(request.Actions); err != nil {
		common.ApiError(c, err)
		return
	}

	accounts, err := service.ListWorkBuddyAccounts(request.ChannelIds, request.Realm)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	selected := filterWorkBuddyAccounts(accounts, request.AccountUid)
	if len(selected) == 0 {
		common.ApiErrorMsg(c, "no WorkBuddy account matched the selection")
		return
	}

	if len(selected) == 1 {
		ctx, cancel := context.WithTimeout(c.Request.Context(), workBuddyManualTimeout)
		defer cancel()
		summary, err := service.RunWorkBuddyTasks(ctx, service.WorkBuddyTaskOptions{
			ChannelIds:              []int{selected[0].ChannelId},
			Realm:                   request.Realm,
			AccountUid:              selected[0].UID(),
			Actions:                 request.Actions,
			Trigger:                 service.WorkBuddyTriggerManual,
			IgnoreTaskParticipation: true,
		})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"queued": false, "summary": summary})
		return
	}

	// The account selection and the manual trigger travel with the queued task:
	// the worker runs one account per request and would otherwise treat the batch
	// as a scheduled pass.
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeWorkBuddyTask, gin.H{
		"trigger":     service.WorkBuddyTriggerManual,
		"channel_ids": request.ChannelIds,
		"realm":       request.Realm,
		"account_uid": request.AccountUid,
		"actions":     request.Actions,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"queued": true, "created": created, "task_id": task.TaskID})
}

type workBuddyTaskToggleRequest struct {
	ChannelId  int    `json:"channel_id"`
	AccountUid string `json:"account_uid"`
	Enabled    bool   `json:"enabled"`
}

// SetWorkBuddyAccountTasks turns one account's automatic welfare participation
// on or off without changing whether the channel serves API traffic.
func SetWorkBuddyAccountTasks(c *gin.Context) {
	var request workBuddyTaskToggleRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.ChannelId <= 0 || strings.TrimSpace(request.AccountUid) == "" {
		common.ApiErrorMsg(c, "channel_id and account_uid are required")
		return
	}
	account, err := service.SetWorkBuddyAccountTasksEnabled(request.ChannelId, request.AccountUid, request.Enabled)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"account": workBuddyAccountView(account, time.Now())})
}

func filterWorkBuddyAccounts(accounts []*service.WorkBuddyAccount, uid string) []*service.WorkBuddyAccount {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return accounts
	}
	selected := make([]*service.WorkBuddyAccount, 0, 1)
	for _, account := range accounts {
		if account.UID() == uid {
			selected = append(selected, account)
		}
	}
	return selected
}

func validateWorkBuddyActions(actions []string) error {
	for _, action := range actions {
		if !service.IsWorkBuddyAction(action) {
			return fmt.Errorf("unknown WorkBuddy action: %s", action)
		}
	}
	return nil
}

// GetWorkBuddyTaskRuns lists the run history, newest first.
func GetWorkBuddyTaskRuns(c *gin.Context) {
	channelId, _ := strconv.Atoi(c.Query("channel_id"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	runs, total, err := model.ListWorkBuddyTaskRuns(model.WorkBuddyTaskRunFilter{
		ChannelId:  channelId,
		AccountUid: c.Query("account_uid"),
		Task:       c.Query("task"),
		Offset:     offset,
		Limit:      limit,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"runs": runs, "total": total})
}

// CheckinWorkBuddyChannel signs every account of one channel in right now. It
// backs the account dialog's button, where waiting for the next scheduled pass
// would be surprising.
func CheckinWorkBuddyChannel(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), workBuddyManualTimeout)
	defer cancel()
	summary, err := service.RunWorkBuddyTasks(ctx, service.WorkBuddyTaskOptions{
		ChannelIds: []int{channelId},
		Actions:    []string{service.WorkBuddyTaskCheckin},
		Trigger:    service.WorkBuddyTriggerManual,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

// parseWorkBuddyChannelIds reads a comma-separated channel id list; empty means
// every WorkBuddy channel.
func parseWorkBuddyChannelIds(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	ids := make([]int, 0, 4)
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid channel id: %s", part)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
