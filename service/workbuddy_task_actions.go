package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// workbuddyTaskClient builds the client the task runner uses. It is a variable so
// tests can aim the runner at an httptest server.
var workbuddyTaskClient = newWorkBuddyOAuthClient

// runWorkBuddyAction runs one action against one account and records it. The
// bookkeeping lives here so every action lands in the state row and the history
// table the same way.
func runWorkBuddyAction(ctx context.Context, account *WorkBuddyAccount, action, trigger string, budget *workBuddyBudget, now time.Time) WorkBuddyAccountResult {
	result := WorkBuddyAccountResult{
		ChannelId:  account.ChannelId,
		AccountUid: account.UID(),
		Nickname:   account.Nickname(),
		Realm:      string(account.Realm()),
		Action:     action,
	}
	skip := func(message string) WorkBuddyAccountResult {
		result.OK = true
		result.Skipped = true
		result.Message = message
		return result
	}
	fail := func(message string) WorkBuddyAccountResult {
		result.Message = message
		recordWorkBuddyActionResult(account, result, trigger, now)
		return result
	}

	if err := checkWorkBuddyActionRealm(action, account); err != nil {
		return skip(err.Error())
	}
	if trigger != WorkBuddyTriggerManual && workBuddyActionDoneToday(account, action, now) {
		return skip("already done today")
	}
	if action == WorkBuddyTaskNightCat && !wbcore.InNightWindow(now) {
		return skip("the night-owl task only counts between 23:00 and 08:00")
	}

	client, err := workbuddyTaskClient(string(account.Realm()), string(account.Credential.Identity().Product), account.Proxy)
	if err != nil {
		return fail("build client: " + err.Error())
	}
	identity := account.Credential.Identity()
	state := workBuddyStateFor(account)

	var credit float64
	switch action {
	case WorkBuddyTaskCheckin:
		if !budget.spend() {
			return skip("daily request budget spent")
		}
		res, err := client.Checkin(ctx, identity)
		if err != nil || !res.OK {
			return fail("check-in failed: " + errorText(err, res.Message))
		}
		state.LastCheckinAt = now.Unix()
		if res.Duplicate {
			result.Message = "already checked in upstream"
		} else {
			result.Message = "checked in"
		}
	case WorkBuddyTaskActivityReport:
		if !budget.spend() {
			return skip("daily request budget spent")
		}
		event := wbcore.ChatActivityEvent(account.UID(), now)
		if err := client.ReportEvents(ctx, identity, []wbcore.GrowthEvent{event}); err != nil {
			return fail("activity report failed: " + err.Error())
		}
		state.LastActivityReportAt = now.Unix()
		result.Message = "activity reported"
		if budget.spend() {
			if streak, err := client.FetchStreak(ctx, identity); err == nil {
				state.StreakDays = streak.Days
				result.Message = fmt.Sprintf("activity reported (streak %d days)", streak.Days)
			}
		}
	case WorkBuddyTaskTravel:
		if !budget.spend() {
			return skip("daily request budget spent")
		}
		res, err := client.CatTravel(ctx, identity)
		if err != nil {
			return fail("cat travel failed: " + err.Error())
		}
		credit = res.Credit
		result.Message = res.Message
	case WorkBuddyTaskStreakBonus:
		if !budget.spend() {
			return skip("daily request budget spent")
		}
		res := client.StreakBonus(ctx, identity)
		if len(res.Logs) == 0 {
			result.Message = "streak state is fine"
		} else {
			result.Message = strings.Join(res.Logs, "; ")
		}
	case WorkBuddyTaskGrowth:
		earned, message, err := runWorkBuddyGrowth(ctx, client, account, budget, now)
		if err != nil {
			return fail(err.Error())
		}
		credit = earned
		result.Message = message
	case WorkBuddyTaskNightCat:
		earned, message, err := runWorkBuddyNightCat(ctx, client, account, budget, now)
		if err != nil {
			return fail(err.Error())
		}
		credit = earned
		result.Message = message
	case WorkBuddyTaskRefreshCredits:
		if !budget.spend() {
			return skip("daily request budget spent")
		}
		credits, err := client.FetchCredits(ctx, identity, now)
		if err != nil {
			return fail("refresh credits failed: " + err.Error())
		}
		state.RemainCredit = credits.Remain
		if credits.Checkin != nil {
			state.StreakDays = credits.Checkin.StreakDays
			state.DailyCredit = credits.Checkin.DailyCredit
			state.TodayCredit = credits.Checkin.TodayCredit
		}
		result.Message = fmt.Sprintf("credits refreshed (remaining %v)", credits.Remain)
	case WorkBuddyTaskDailyChat:
		if !budget.spend() {
			return skip("daily request budget spent")
		}
		setting := operation_setting.GetWorkBuddyTaskSetting()
		res := client.DailyChat(ctx, identity, setting != nil && setting.DailyChatWeb)
		if !res.OK {
			return fail("daily chat failed: " + res.Message)
		}
		state.LastDailyChatAt = now.Unix()
		result.Message = res.Message
	default:
		return skip("unknown action")
	}

	result.OK = true
	result.Credit = credit
	recordWorkBuddyActionResult(account, result, trigger, now)
	return result
}

func errorText(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	if fallback != "" {
		return fallback
	}
	return "unknown error"
}

// checkWorkBuddyActionRealm keeps an action from being sent to a realm that does
// not have it: sign-in and the growth centre are CN-only, the daily activity chat
// is INTL-only, and enterprise accounts have no growth centre at all.
func checkWorkBuddyActionRealm(action string, account *WorkBuddyAccount) error {
	switch action {
	case WorkBuddyTaskDailyChat:
		if account.IsCN() {
			return errors.New("the daily activity chat is international-only")
		}
		return nil
	case WorkBuddyTaskCheckin, WorkBuddyTaskActivityReport, WorkBuddyTaskTravel,
		WorkBuddyTaskStreakBonus, WorkBuddyTaskGrowth, WorkBuddyTaskNightCat:
		if !account.IsCN() {
			return errors.New("this action is China-realm only")
		}
		if account.IsEnterprise() {
			return errors.New("enterprise accounts have no growth centre")
		}
		return nil
	}
	return nil
}

// workBuddyActionDoneToday is the scheduled run's idempotency gate.
func workBuddyActionDoneToday(account *WorkBuddyAccount, action string, now time.Time) bool {
	state := account.State
	if state == nil {
		return false
	}
	today := now.Format("2006-01-02")
	sameDay := func(unix int64) bool {
		if unix == 0 {
			return false
		}
		return time.Unix(unix, 0).Format("2006-01-02") == today
	}
	switch action {
	case WorkBuddyTaskCheckin:
		return sameDay(state.LastCheckinAt)
	case WorkBuddyTaskActivityReport:
		return sameDay(state.LastActivityReportAt)
	case WorkBuddyTaskDailyChat:
		return sameDay(state.LastDailyChatAt)
	}
	return false
}

// workBuddyStateFor returns the row to update, creating it in memory when the
// account has never run.
func workBuddyStateFor(account *WorkBuddyAccount) *model.WorkBuddyAccountState {
	if account.State != nil {
		return account.State
	}
	identity := account.Credential.Identity()
	account.State = &model.WorkBuddyAccountState{
		ChannelId:  account.ChannelId,
		AccountUid: account.UID(),
		Realm:      string(identity.Realm),
		Product:    string(identity.Product),
		Nickname:   account.Nickname(),
	}
	return account.State
}

// recordWorkBuddyActionResult writes the state row and the history entry. A
// storage failure is logged, never returned: the upstream action already
// happened and losing the bookkeeping must not turn it into a failure.
func recordWorkBuddyActionResult(account *WorkBuddyAccount, result WorkBuddyAccountResult, trigger string, now time.Time) {
	state := workBuddyStateFor(account)
	state.LastRunAt = now.Unix()
	state.LastTask = result.Action
	if result.Skipped {
		state.LastStatus = "skipped"
	} else if result.OK {
		state.LastStatus = "ok"
	} else {
		state.LastStatus = "failed"
	}
	state.LastMessage = result.Message
	if err := model.SaveWorkBuddyAccountState(state); err != nil {
		logWorkBuddyTaskError("save account state", err)
	}
	run := &model.WorkBuddyTaskRun{
		ChannelId:   account.ChannelId,
		AccountUid:  account.UID(),
		Task:        result.Action,
		Trigger:     trigger,
		OK:          result.OK,
		Message:     result.Message,
		CreditDelta: result.Credit,
		CreatedAt:   now.Unix(),
	}
	if err := model.RecordWorkBuddyTaskRun(run); err != nil {
		logWorkBuddyTaskError("record task run", err)
	}
}

// runWorkBuddyGrowth ports the reference gateway's growth-centre pass: progress
// the auto tasks first, accept the rest, then advance and claim each one
// (wb_tasks.py:634-708).
func runWorkBuddyGrowth(ctx context.Context, client *wbcore.Client, account *WorkBuddyAccount, budget *workBuddyBudget, now time.Time) (float64, string, error) {
	if !budget.spend() {
		return 0, "", errors.New("daily request budget spent")
	}
	tasks, err := client.FetchGrowthTasks(ctx, account.Credential.Identity())
	if err != nil {
		return 0, "", fmt.Errorf("list growth tasks: %w", err)
	}
	if len(tasks) == 0 {
		return 0, "no growth tasks available", nil
	}
	identity := account.Credential.Identity()
	logs := make([]string, 0, 8)
	total := 0.0

	// 0. auto tasks need no acceptance and are the prerequisite of the rest.
	for _, task := range tasks {
		if task.Type != "auto" {
			continue
		}
		total += progressWorkBuddyTask(ctx, client, identity, task, budget, now, &logs)
	}

	// 1. accept whatever is still unaccepted.
	pending := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task.NeedsAcceptance() {
			pending = append(pending, task.Code)
		}
	}
	if len(pending) > 0 {
		if !budget.spend() {
			return total, strings.Join(logs, "; "), nil
		}
		accepted, err := client.AcceptGrowthTasks(ctx, identity, pending)
		if err != nil {
			logs = append(logs, "accept: "+err.Error())
		} else {
			if len(accepted.Accepted) > 0 {
				logs = append(logs, fmt.Sprintf("accepted %d task(s)", len(accepted.Accepted)))
			}
			if len(accepted.Failed) > 0 {
				logs = append(logs, fmt.Sprintf("accept failed for %d task(s)", len(accepted.Failed)))
			}
		}
		if budget.spend() {
			if refreshed, err := client.FetchGrowthTasks(ctx, identity); err == nil {
				tasks = refreshed
			}
		}
	}

	// 2. advance and claim every remaining task.
	for _, task := range tasks {
		if task.Type == "auto" {
			continue
		}
		total += progressWorkBuddyTask(ctx, client, identity, task, budget, now, &logs)
	}

	// 3. the cat travels as part of the same routine.
	if budget.spend() {
		if travel, err := client.CatTravel(ctx, identity); err == nil {
			total += travel.Credit
			if travel.Message != "" {
				logs = append(logs, "cat: "+travel.Message)
			}
		}
	}
	if budget.exhausted() {
		logs = append(logs, "daily request budget spent")
	}
	if len(logs) == 0 {
		logs = append(logs, "nothing to do")
	}
	return total, strings.Join(logs, "; "), nil
}

// progressWorkBuddyTask advances one task and claims it when it reaches its
// target. Tasks that need a real desktop action or a real-world step are skipped
// honestly instead of being faked (wb_tasks.py:525-632).
func progressWorkBuddyTask(ctx context.Context, client *wbcore.Client, identity wbcore.Identity, task wbcore.GrowthTask, budget *workBuddyBudget, now time.Time, logs *[]string) float64 {
	if task.Unforgeable {
		*logs = append(*logs, fmt.Sprintf("%s needs a real action (%s)", task.Name, task.Reason))
		return 0
	}
	if task.Status == "claimed" {
		return 0
	}
	if task.Status == "completed" || task.Current >= task.Target {
		if !budget.spend() {
			return 0
		}
		res := client.ClaimGrowthTask(ctx, identity, task.Code)
		if res.OK {
			*logs = append(*logs, fmt.Sprintf("claimed %s (+%v)", task.Name, res.Credit))
			return res.Credit
		}
		*logs = append(*logs, fmt.Sprintf("claim %s failed: %s", task.Name, res.Message))
		return 0
	}
	if task.DesktopOnly != "" {
		*logs = append(*logs, fmt.Sprintf("%s needs the desktop client (%s)", task.Name, task.DesktopOnly))
		return 0
	}
	if task.Code == "black_cat" && !wbcore.InNightWindow(now) {
		*logs = append(*logs, "the night-owl task only counts between 23:00 and 08:00")
		return 0
	}
	if task.NeedsAcceptance() {
		*logs = append(*logs, fmt.Sprintf("%s is not accepted yet, skipped", task.Name))
		return 0
	}

	need := max(1, task.Target-task.Current)
	reported := false
	for i := range need {
		if !budget.spend() {
			break
		}
		expertID := ""
		if task.Kind == "expert" || task.Kind == "team" {
			expertID = wbcore.ExpertIDFor(task.Kind, task.Current+i)
		}
		event := wbcore.BuildGrowthEvent(identity.UID, task.Kind, i, expertID, now)
		if err := client.ReportEvents(ctx, identity, []wbcore.GrowthEvent{event}); err != nil {
			*logs = append(*logs, fmt.Sprintf("report %s failed: %s", task.Name, err.Error()))
			break
		}
		reported = true
	}
	if !reported {
		return 0
	}

	// The upstream books the progress a second or two later, so claim only after
	// it becomes visible.
	progress := task.Current
	for attempt := range 3 {
		if attempt > 0 {
			// Pause between reads, but never past a cancelled context so a
			// shutdown does not sit here for seconds.
			pause := time.NewTimer(1500 * time.Millisecond)
			select {
			case <-ctx.Done():
				pause.Stop()
				*logs = append(*logs, fmt.Sprintf("%s progress check cancelled", task.Name))
				return 0
			case <-pause.C:
			}
		}
		if !budget.spend() {
			break
		}
		fresh, err := client.FetchGrowthTasks(ctx, identity)
		if err != nil {
			break
		}
		current, status := taskProgressOf(fresh, task.Code)
		progress = current
		if current >= task.Target || status == "completed" || status == "claimed" {
			break
		}
	}
	if progress < task.Target {
		*logs = append(*logs, fmt.Sprintf("%s progress %d/%d, claim deferred", task.Name, progress, task.Target))
		return 0
	}
	if !budget.spend() {
		return 0
	}
	res := client.ClaimGrowthTask(ctx, identity, task.Code)
	if !res.OK {
		*logs = append(*logs, fmt.Sprintf("claim %s failed: %s", task.Name, res.Message))
		return 0
	}
	*logs = append(*logs, fmt.Sprintf("completed %s (+%v)", task.Name, res.Credit))
	return res.Credit
}

func taskProgressOf(tasks []wbcore.GrowthTask, code string) (int, string) {
	for _, task := range tasks {
		if task.Code == code {
			return task.Current, task.Status
		}
	}
	return 0, ""
}

// runWorkBuddyNightCat drives the night-owl task, which only counts inside its
// window and accumulates over three nights (wb_tasks.py:710-787).
func runWorkBuddyNightCat(ctx context.Context, client *wbcore.Client, account *WorkBuddyAccount, budget *workBuddyBudget, now time.Time) (float64, string, error) {
	if !budget.spend() {
		return 0, "", errors.New("daily request budget spent")
	}
	identity := account.Credential.Identity()
	tasks, err := client.FetchGrowthTasks(ctx, identity)
	if err != nil {
		return 0, "", fmt.Errorf("list growth tasks: %w", err)
	}
	var cat *wbcore.GrowthTask
	for index := range tasks {
		if tasks[index].Code == "black_cat" {
			cat = &tasks[index]
			break
		}
	}
	if cat == nil {
		return 0, "the night-owl task is not in the list", nil
	}
	if cat.Status == "claimed" {
		return 0, "the night-owl task is already claimed", nil
	}
	if cat.NeedsAcceptance() {
		if !budget.spend() {
			return 0, "", errors.New("daily request budget spent")
		}
		accepted, err := client.AcceptGrowthTasks(ctx, identity, []string{cat.Code})
		if err != nil || len(accepted.Accepted) == 0 {
			return 0, "the night-owl task could not be accepted", nil
		}
	}
	logs := make([]string, 0, 2)
	credit := progressWorkBuddyTask(ctx, client, identity, *cat, budget, now, &logs)
	if len(logs) == 0 {
		logs = append(logs, "the night-owl task is up to date")
	}
	return credit, strings.Join(logs, "; "), nil
}

func logWorkBuddyTaskError(what string, err error) {
	if err == nil {
		return
	}
	logger.LogWarn(context.Background(), common.LogText("workbuddy task: %s: %s", what, err.Error()))
}
