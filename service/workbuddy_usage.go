package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
)

// The console can ask for three windows. "24h" is a rolling day and is always
// answerable from the logs; "today" is the local calendar day; "current" aligns
// to the upstream window whenever a cap has named its reset clock.
const (
	WorkBuddyUsageWindow24h     = "24h"
	WorkBuddyUsageWindowToday   = "today"
	WorkBuddyUsageWindowCurrent = "current"
)

// workBuddyUsageRetentionDays bounds the cap history: a sample older than the
// estimation window cannot influence a budget again.
const workBuddyUsageRetentionDays = 90

// workBuddyUsageRollupDays re-sums the last few days on every pass, so a late
// log write or a restart still lands on the right day key.
const workBuddyUsageRollupDays = 3

// workBuddyLimitWindow is the length of one upstream quota window.
const workBuddyLimitWindow = 24 * 60 * 60

// WorkBuddyUsageQuery is one console request: which channels and models to
// summarize, and over which window.
type WorkBuddyUsageQuery struct {
	ChannelIds []int
	Models     []string
	Window     string
	Now        time.Time
}

// WorkBuddyModelUsage is one account-model cell.
type WorkBuddyModelUsage struct {
	ChannelId        int     `json:"channel_id"`
	AccountUid       string  `json:"account_uid"`
	AccountName      string  `json:"account_name"`
	Realm            string  `json:"realm"`
	Model            string  `json:"model"`
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	LastUsedAt       int64   `json:"last_used_at"`
	WindowStart      int64   `json:"window_start"`
	WindowResetAt    int64   `json:"window_reset_at"`
	BudgetTokens     int64   `json:"budget_tokens"`
	RemainingTokens  int64   `json:"remaining_tokens"`
	BudgetSource     string  `json:"budget_source"`
	RemainingRatio   float64 `json:"remaining_ratio"`
	Cooling          bool    `json:"cooling"`
}

// WorkBuddyUsageSummary is the per-realm headline.
type WorkBuddyUsageSummary struct {
	Realm             string `json:"realm"`
	Accounts          int    `json:"accounts"`
	Requests          int64  `json:"requests"`
	TotalTokens       int64  `json:"total_tokens"`
	PromptTokens      int64  `json:"prompt_tokens"`
	CompletionTokens  int64  `json:"completion_tokens"`
	CoolingAccounts   int    `json:"cooling_accounts"`
	NearLimitAccounts int    `json:"near_limit_accounts"`
	UnknownBudget     int    `json:"unknown_budget"`
}

// WorkBuddyUsageReport is the console payload: per-model rows plus the realm
// rollups the headline cards show.
type WorkBuddyUsageReport struct {
	Window    string                           `json:"window"`
	Since     int64                            `json:"since"`
	Rows      []WorkBuddyModelUsage            `json:"rows"`
	Summaries map[string]WorkBuddyUsageSummary `json:"summaries"`
}

// WorkBuddyBudgetCell is one (realm, model) budget estimate. The upstream never
// publishes the budget, so the value comes either from a measured cap sample or
// from a published seed.
type WorkBuddyBudgetCell struct {
	Budget int64
	Source string
}

// SummarizeWorkBuddyUsage folds the consume logs into per-(channel, account,
// model) counters. It is the passive source: no upstream call is made, so a
// fresh installation shows real numbers immediately and an upstream outage
// never blanks the console.
//
// The row window is the upstream one whenever a cap named its reset clock: a
// still-future reset means the window is spent (cooling, remaining zero), a
// past reset means a new window began at that clock. Without an anchor the
// window is the requested rolling one, which is a lower bound on real usage.
func SummarizeWorkBuddyUsage(ctx context.Context, query WorkBuddyUsageQuery) (*WorkBuddyUsageReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := query.Now
	if now.IsZero() {
		now = time.Now()
	}
	window := normalizeWorkBuddyUsageWindow(query.Window)
	baseSince, until := workBuddyUsageBounds(window, now)

	accounts, err := ListWorkBuddyAccounts(query.ChannelIds, WorkBuddyRealmAll)
	if err != nil {
		return nil, err
	}
	channelIds := distinctWorkBuddyChannelIds(accounts)
	if len(channelIds) == 0 {
		return &WorkBuddyUsageReport{
			Window:    window,
			Since:     baseSince,
			Rows:      []WorkBuddyModelUsage{},
			Summaries: workBuddyEmptyUsageSummaries(),
		}, nil
	}

	totals, err := model.SumWorkBuddyConsumeLogs(model.WorkBuddyUsageFilter{
		ChannelIds: channelIds,
		Models:     query.Models,
		StartUnix:  baseSince,
		EndUnix:    until,
	})
	if err != nil {
		return nil, err
	}
	events, err := model.ListWorkBuddyLimitEvents(channelIds, now.Unix()-2*workBuddyLimitWindow)
	if err != nil {
		return nil, err
	}
	budgets := EstimateWorkBuddyBudgets(events, now)
	latest := latestWorkBuddyLimitEvents(events)

	accountsByChannel := accountsByWorkBuddyChannel(accounts)
	rows := make([]WorkBuddyModelUsage, 0, len(totals))
	seen := map[string]bool{}
	for _, total := range totals {
		if len(query.Models) > 0 && !containsWorkBuddyModel(query.Models, total.Model) {
			continue
		}
		credentials := accountsByChannel[total.ChannelId]
		if len(credentials) == 0 {
			continue
		}
		start, reset, cooling := workBuddyCellWindow(baseSince, latest[workBuddyCellKey(total.ChannelId, total.Model)], now)
		usage := total
		if start != baseSince {
			anchored, err := model.SumWorkBuddyConsumeLogs(model.WorkBuddyUsageFilter{
				ChannelIds: []int{total.ChannelId},
				Models:     []string{total.Model},
				StartUnix:  start,
				EndUnix:    until,
			})
			if err != nil {
				return nil, err
			}
			if len(anchored) > 0 {
				usage = anchored[0]
			} else {
				usage = model.WorkBuddyChannelModelUsage{ChannelId: total.ChannelId, Model: total.Model}
			}
		}
		for _, account := range credentials {
			row := WorkBuddyModelUsage{
				ChannelId:        total.ChannelId,
				AccountUid:       account.UID(),
				AccountName:      account.Nickname(),
				Realm:            workBuddyRealmOf(account),
				Model:            total.Model,
				Requests:         usage.Requests,
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				TotalTokens:      usage.TotalTokens,
				LastUsedAt:       usage.LastUsedAt,
				WindowStart:      start,
				WindowResetAt:    reset,
				Cooling:          cooling,
				BudgetSource:     "passive",
				RemainingRatio:   1,
			}
			applyWorkBuddyBudget(&row, budgets)
			rows = append(rows, row)
			seen[workBuddyRowKey(row)] = true
		}
	}

	// A cap is worth showing even when the window has logged no usage yet, so
	// every anchored cell that produced no row gets a zero-usage one.
	for _, event := range events {
		for _, account := range accounts {
			if account.ChannelId != event.ChannelId || account.UID() != event.AccountUid {
				continue
			}
			key := fmt.Sprintf("%d|%s|%s", event.ChannelId, event.AccountUid, event.Model)
			if seen[key] {
				continue
			}
			seen[key] = true
			start, reset, cooling := workBuddyCellWindow(baseSince, &event, now)
			row := WorkBuddyModelUsage{
				ChannelId:      event.ChannelId,
				AccountUid:     event.AccountUid,
				AccountName:    account.Nickname(),
				Realm:          event.Realm,
				Model:          event.Model,
				WindowStart:    start,
				WindowResetAt:  reset,
				Cooling:        cooling,
				BudgetSource:   "passive",
				RemainingRatio: 1,
			}
			usage, err := model.SumWorkBuddyConsumeLogs(model.WorkBuddyUsageFilter{
				ChannelIds: []int{event.ChannelId},
				Models:     []string{event.Model},
				StartUnix:  start,
				EndUnix:    until,
			})
			if err == nil && len(usage) > 0 {
				row.Requests = usage[0].Requests
				row.PromptTokens = usage[0].PromptTokens
				row.CompletionTokens = usage[0].CompletionTokens
				row.TotalTokens = usage[0].TotalTokens
				row.LastUsedAt = usage[0].LastUsedAt
			}
			applyWorkBuddyBudget(&row, budgets)
			rows = append(rows, row)
		}
	}

	report := &WorkBuddyUsageReport{Window: window, Since: baseSince, Rows: rows}
	report.Summaries = summarizeWorkBuddyUsageRows(rows, accounts)
	return report, nil
}

// SyncWorkBuddyUsageDaily rolls the consume logs up into the daily table. It is
// idempotent and writes absolute per-day totals, so a repeated run is a no-op
// and the console keeps a trend after the raw logs are trimmed.
func SyncWorkBuddyUsageDaily(ctx context.Context, now time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now()
	}
	accounts, err := ListWorkBuddyAccounts(nil, WorkBuddyRealmAll)
	if err != nil {
		return err
	}
	channelIds := distinctWorkBuddyChannelIds(accounts)
	if len(channelIds) == 0 {
		return nil
	}
	zone := workBuddyZone()
	local := now.In(zone)
	realmByChannel := workBuddyRealmByChannel(accounts)
	accountsByChannel := accountsByWorkBuddyChannel(accounts)

	// Each day is summed from its own bounds instead of grouping inside SQL:
	// the day boundary is a UTC+8 midnight, and expressing that in SQL would
	// need a different date function per engine. The window is small (a couple
	// of days) so the extra queries are cheap and the arithmetic stays portable.
	for daysAgo := 0; daysAgo < workBuddyUsageRollupDays; daysAgo++ {
		day := local.AddDate(0, 0, -daysAgo)
		start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, zone)
		end := start.AddDate(0, 0, 1)
		if end.Unix() > now.Unix() {
			end = now
		}
		totals, err := model.SumWorkBuddyConsumeLogs(model.WorkBuddyUsageFilter{
			ChannelIds: channelIds,
			StartUnix:  start.Unix(),
			EndUnix:    end.Unix(),
		})
		if err != nil {
			return err
		}
		dayKey := start.Format("2006-01-02")
		dayRows := make([]model.WorkBuddyUsageDaily, 0, len(totals))
		for _, total := range totals {
			credentials := accountsByChannel[total.ChannelId]
			if len(credentials) != 1 {
				// A multi-key channel cannot be attributed from the log alone,
				// so the daily table stays silent rather than inventing a split.
				continue
			}
			dayRows = append(dayRows, model.WorkBuddyUsageDaily{
				ChannelId:        total.ChannelId,
				AccountUid:       credentials[0].UID(),
				Model:            total.Model,
				Day:              dayKey,
				Realm:            realmByChannel[total.ChannelId],
				Requests:         total.Requests,
				PromptTokens:     total.PromptTokens,
				CompletionTokens: total.CompletionTokens,
				TotalTokens:      total.TotalTokens,
			})
		}
		// The day is replaced in one transaction so a concurrent read never
		// sees the delete without the write-back that follows it.
		if err := model.ReplaceWorkBuddyUsageDaily(channelIds, dayKey, dayRows); err != nil {
			return err
		}
	}

	cutoff := now.AddDate(0, 0, -workBuddyUsageRetentionDays-1)
	if _, err := model.DeleteWorkBuddyLimitEventsBefore(cutoff.Unix()); err != nil {
		logger.LogWarn(ctx, common.LogText("workbuddy usage: trim limit events: %s", err.Error()))
	}
	return nil
}

// CaptureWorkBuddyCapEvent records one upstream quota cap seen on the relay. The
// sample is the successful usage the account had accumulated in the window that
// just closed, which is the only way the upstream budget is ever learned. The
// call is best effort: a bookkeeping failure must never change what the client
// receives.
func CaptureWorkBuddyCapEvent(ctx context.Context, channelId int, uid, realm, modelName, message string, now time.Time) {
	resetAt, ok := wbcore.ParseRateLimitReset(message)
	if !ok {
		// A throttle without a reset clock is not a spent window, so it is not
		// evidence of the budget and must not be recorded.
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	sample := int64(0)
	rows, err := model.SumWorkBuddyConsumeLogs(model.WorkBuddyUsageFilter{
		ChannelIds: []int{channelId},
		Models:     []string{modelName},
		StartUnix:  resetAt - workBuddyLimitWindow,
		EndUnix:    now.Unix(),
	})
	if err != nil {
		logger.LogWarn(ctx, common.LogText("workbuddy usage: sample window for channel %d: %s", channelId, err.Error()))
	} else if len(rows) > 0 {
		sample = rows[0].TotalTokens
	}
	event := model.WorkBuddyLimitEvent{
		ChannelId:    channelId,
		AccountUid:   uid,
		Model:        modelName,
		Realm:        realm,
		ResetAt:      resetAt,
		CappedAt:     now.Unix(),
		SampleTokens: sample,
		Source:       "relay",
	}
	if err := model.RecordWorkBuddyLimitEvent(&event); err != nil {
		logger.LogWarn(ctx, common.LogText("workbuddy usage: record limit event for channel %d: %s", channelId, err.Error()))
	}
}

// EstimateWorkBuddyBudgets turns the measured cap samples into per-(realm,
// model) budgets. Samples are weighted by recency (today 6, yesterday 3, the
// day before 1) so a fresh measurement dominates an old one.
func EstimateWorkBuddyBudgets(events []model.WorkBuddyLimitEvent, now time.Time) map[string]WorkBuddyBudgetCell {
	budgets := map[string]WorkBuddyBudgetCell{}
	if len(events) == 0 {
		return budgets
	}
	type weightSum struct {
		weighted float64
		weight   float64
	}
	byPair := map[string]*weightSum{}
	local := now.In(workBuddyZone())
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	for _, event := range events {
		if event.SampleTokens <= 0 {
			continue
		}
		at := time.Unix(event.CappedAt, 0).In(local.Location())
		age := int(today.Sub(time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())) / (24 * time.Hour))
		weight := 0.0
		switch age {
		case 0:
			weight = 6
		case 1:
			weight = 3
		case 2:
			weight = 1
		default:
			continue
		}
		key := event.Realm + "\x1f" + event.Model
		entry := byPair[key]
		if entry == nil {
			entry = &weightSum{}
			byPair[key] = entry
		}
		entry.weighted += float64(event.SampleTokens) * weight
		entry.weight += weight
	}
	for key, entry := range byPair {
		if entry.weight <= 0 || entry.weighted <= 0 {
			continue
		}
		budgets[key] = WorkBuddyBudgetCell{
			Budget: int64(entry.weighted / entry.weight),
			Source: "cap",
		}
	}
	return budgets
}

// ListWorkBuddyUsageHistory returns the stored daily rollup for the requested
// channels since a day key, newest first.
func ListWorkBuddyUsageHistory(channelIds []int, sinceDay string) ([]model.WorkBuddyUsageDaily, error) {
	return model.ListWorkBuddyUsageDaily(channelIds, sinceDay)
}

func normalizeWorkBuddyUsageWindow(window string) string {
	switch window {
	case WorkBuddyUsageWindowToday:
		return WorkBuddyUsageWindowToday
	case WorkBuddyUsageWindowCurrent:
		return WorkBuddyUsageWindowCurrent
	default:
		return WorkBuddyUsageWindow24h
	}
}

// workBuddyUsageBounds resolves the requested window in UTC+8, the zone the
// upstream reset clock uses and the zone the operators read the console in.
func workBuddyUsageBounds(window string, now time.Time) (int64, int64) {
	local := now.In(workBuddyZone())
	end := local.Unix()
	switch window {
	case WorkBuddyUsageWindowToday:
		midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
		return midnight.Unix(), end
	default:
		return end - workBuddyLimitWindow, end
	}
}

// workBuddyCellWindow resolves one cell's accounting window from its most recent
// cap. A future reset means the window is spent; a past reset means the new
// window began at that clock. No cap leaves the requested rolling window, which
// is a lower bound on real usage.
func workBuddyCellWindow(baseSince int64, event *model.WorkBuddyLimitEvent, now time.Time) (start, reset int64, cooling bool) {
	if event == nil {
		return baseSince, 0, false
	}
	if event.ResetAt > now.Unix() {
		return event.ResetAt - workBuddyLimitWindow, event.ResetAt, true
	}
	if event.ResetAt > 0 {
		return event.ResetAt, 0, false
	}
	return baseSince, 0, false
}

// latestWorkBuddyLimitEvents keeps only the newest cap per (channel, model).
// ListWorkBuddyLimitEvents orders by reset clock, so the map keeps the first
// occurrence seen.
func latestWorkBuddyLimitEvents(events []model.WorkBuddyLimitEvent) map[string]*model.WorkBuddyLimitEvent {
	out := map[string]*model.WorkBuddyLimitEvent{}
	for i := range events {
		key := workBuddyCellKey(events[i].ChannelId, events[i].Model)
		if out[key] == nil {
			event := events[i]
			out[key] = &event
		}
	}
	return out
}

func workBuddyCellKey(channelId int, model string) string {
	return fmt.Sprintf("%d|%s", channelId, model)
}

func workBuddyRowKey(row WorkBuddyModelUsage) string {
	return fmt.Sprintf("%d|%s|%s", row.ChannelId, row.AccountUid, row.Model)
}

func workBuddyZone() *time.Location {
	return time.FixedZone("UTC+8", 8*60*60)
}

func workBuddyEmptyUsageSummaries() map[string]WorkBuddyUsageSummary {
	return map[string]WorkBuddyUsageSummary{
		WorkBuddyRealmIntl: {Realm: WorkBuddyRealmIntl},
		WorkBuddyRealmCN:   {Realm: WorkBuddyRealmCN},
	}
}

func distinctWorkBuddyChannelIds(accounts []*WorkBuddyAccount) []int {
	ids := make([]int, 0, len(accounts))
	seen := map[int]bool{}
	for _, account := range accounts {
		if seen[account.ChannelId] {
			continue
		}
		seen[account.ChannelId] = true
		ids = append(ids, account.ChannelId)
	}
	return ids
}

func accountsByWorkBuddyChannel(accounts []*WorkBuddyAccount) map[int][]*WorkBuddyAccount {
	out := map[int][]*WorkBuddyAccount{}
	for _, account := range accounts {
		out[account.ChannelId] = append(out[account.ChannelId], account)
	}
	return out
}

func workBuddyRealmByChannel(accounts []*WorkBuddyAccount) map[int]string {
	out := map[int]string{}
	for _, account := range accounts {
		out[account.ChannelId] = workBuddyRealmOf(account)
	}
	return out
}

func workBuddyRealmOf(account *WorkBuddyAccount) string {
	if account.IsCN() {
		return WorkBuddyRealmCN
	}
	return WorkBuddyRealmIntl
}

func containsWorkBuddyModel(models []string, model string) bool {
	for _, candidate := range models {
		if candidate == model {
			return true
		}
	}
	return false
}

// applyWorkBuddyBudget fills one row's budget figures: a measured per-realm
// estimate first, then the published seed for the model, then nothing (the
// console then shows absolute usage without a percentage). A cooling window is
// always fully spent, so its remaining is forced to zero.
func applyWorkBuddyBudget(row *WorkBuddyModelUsage, budgets map[string]WorkBuddyBudgetCell) {
	cell, ok := budgets[row.Realm+"\x1f"+row.Model]
	if !ok {
		if seed, seeded := wbcore.SeededTokenBudget(row.Model); seeded {
			cell = WorkBuddyBudgetCell{Budget: seed, Source: "seed"}
			ok = true
		}
	}
	if !ok || cell.Budget <= 0 {
		row.BudgetSource = "unknown"
		row.RemainingRatio = 0
		return
	}
	row.BudgetTokens = cell.Budget
	row.BudgetSource = cell.Source
	remaining := cell.Budget - row.TotalTokens
	if remaining < 0 {
		remaining = 0
	}
	if row.Cooling {
		remaining = 0
	}
	row.RemainingTokens = remaining
	row.RemainingRatio = float64(remaining) / float64(cell.Budget)
}

func summarizeWorkBuddyUsageRows(rows []WorkBuddyModelUsage, accounts []*WorkBuddyAccount) map[string]WorkBuddyUsageSummary {
	summaries := workBuddyEmptyUsageSummaries()
	for _, row := range rows {
		summary := summaries[row.Realm]
		summary.Realm = row.Realm
		summary.Requests += row.Requests
		summary.TotalTokens += row.TotalTokens
		summary.PromptTokens += row.PromptTokens
		summary.CompletionTokens += row.CompletionTokens
		if row.Cooling {
			summary.CoolingAccounts++
		}
		if row.BudgetTokens > 0 && !row.Cooling && row.RemainingRatio < 0.2 {
			summary.NearLimitAccounts++
		}
		if row.BudgetTokens <= 0 {
			summary.UnknownBudget++
		}
		summaries[row.Realm] = summary
	}
	for _, account := range accounts {
		realm := workBuddyRealmOf(account)
		summary := summaries[realm]
		summary.Accounts++
		summaries[realm] = summary
	}
	return summaries
}
