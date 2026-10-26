package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A WorkBuddy quota cap is only evidence when the body names the reset clock,
// so the reset parser is the gate that keeps a plain frequency throttle out of
// the budget samples.
func TestParseWorkBuddyCapResetGate(t *testing.T) {
	const intlCap = `{"code":6004,"msg":"usage will reset at 2026-10-09 14:44:59 UTC+8"}`
	const cnCap = `{"code":6004,"msg":"将在 2026-10-09 14:44:59 UTC+8 重置"}`
	const throttle = `{"code":429,"msg":"too many requests"}`

	for _, message := range []string{intlCap, cnCap} {
		resetAt, ok := wbcore.ParseRateLimitReset(message)
		require.True(t, ok, message)
		assert.Equal(t, int64(1791528299), resetAt, message)
	}
	_, ok := wbcore.ParseRateLimitReset(throttle)
	assert.False(t, ok, "a throttle without a reset clock must not be a cap")
}

// The budget estimate weights today 6, yesterday 3 and the day before 1, so a
// fresh measurement dominates an older one instead of being averaged away.
func TestEstimateWorkBuddyBudgetsWeightsByRecency(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, workBuddyZone())
	makeEvent := func(daysAgo int, sample int64) model.WorkBuddyLimitEvent {
		at := now.AddDate(0, 0, -daysAgo)
		return model.WorkBuddyLimitEvent{
			ChannelId: 1, AccountUid: "uid", Model: "deepseek-v4.1-flash", Realm: "intl",
			CappedAt: at.Unix(), SampleTokens: sample,
		}
	}
	budgets := EstimateWorkBuddyBudgets([]model.WorkBuddyLimitEvent{
		makeEvent(0, 200_000_000),
		makeEvent(1, 100_000_000),
		makeEvent(2, 50_000_000),
	}, now)

	cell, ok := budgets["intl\x1fdeepseek-v4.1-flash"]
	require.True(t, ok)
	assert.Equal(t, "cap", cell.Source)
	// (200*6 + 100*3 + 50*1) / 10 = 155M
	assert.Equal(t, int64(155_000_000), cell.Budget)

	// A sample older than the estimation window contributes nothing.
	budgets = EstimateWorkBuddyBudgets([]model.WorkBuddyLimitEvent{makeEvent(5, 300_000_000)}, now)
	assert.Empty(t, budgets)
}

// Without any sample the published seed is what the console shows, and a model
// with no seed at all reports no percentage rather than a fabricated one.
func TestApplyWorkBuddyBudgetFallsBackToSeed(t *testing.T) {
	row := WorkBuddyModelUsage{Model: "deepseek-v4.1-flash-sg", Realm: "intl", TotalTokens: 50_000_000}
	applyWorkBuddyBudget(&row, nil)
	assert.Equal(t, int64(200_000_000), row.BudgetTokens)
	assert.Equal(t, "seed", row.BudgetSource)
	assert.Equal(t, int64(150_000_000), row.RemainingTokens)
	assert.InDelta(t, 0.75, row.RemainingRatio, 0.0001)

	unknown := WorkBuddyModelUsage{Model: "gpt-6-astra", Realm: "intl", TotalTokens: 1234}
	applyWorkBuddyBudget(&unknown, nil)
	assert.Zero(t, unknown.BudgetTokens)
	assert.Equal(t, "unknown", unknown.BudgetSource)
	assert.Zero(t, unknown.RemainingRatio)
}

// A cooling window is spent by definition: the capped reset clock is still in
// the future, so the remaining figure must be forced to zero even when the
// measured usage sits below the budget.
func TestApplyWorkBuddyBudgetForcesCoolingToZero(t *testing.T) {
	row := WorkBuddyModelUsage{
		Model: "deepseek-v4.1-flash", Realm: "intl",
		TotalTokens: 10_000_000, Cooling: true,
	}
	applyWorkBuddyBudget(&row, map[string]WorkBuddyBudgetCell{
		"intl\x1fdeepseek-v4.1-flash": {Budget: 200_000_000, Source: "cap"},
	})
	assert.Zero(t, row.RemainingTokens)
	assert.Zero(t, row.RemainingRatio)
}

// The current window starts at the reset clock once it has passed, and is the
// 24h segment ending at that clock while it is still in the future.
func TestWorkBuddyCellWindowAnchoring(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, workBuddyZone())
	base := now.Unix() - 24*60*60

	cooling := &model.WorkBuddyLimitEvent{ResetAt: now.Unix() + 2*60*60}
	start, reset, coolingFlag := workBuddyCellWindow(base, cooling, now)
	assert.True(t, coolingFlag)
	assert.Equal(t, cooling.ResetAt, reset)
	assert.Equal(t, cooling.ResetAt-24*60*60, start)

	finished := &model.WorkBuddyLimitEvent{ResetAt: now.Unix() - 60*60}
	start, reset, coolingFlag = workBuddyCellWindow(base, finished, now)
	assert.False(t, coolingFlag)
	assert.Equal(t, finished.ResetAt, start)
	assert.Zero(t, reset)

	start, reset, coolingFlag = workBuddyCellWindow(base, nil, now)
	assert.False(t, coolingFlag)
	assert.Equal(t, base, start)
	assert.Zero(t, reset)
}

// The passive fold reads the gateway's own consume logs, attributes the channel
// to its single credential, and shows real numbers with no upstream call.
func TestSummarizeWorkBuddyUsageFoldsConsumeLogs(t *testing.T) {
	db := setupWorkBuddyTaskDB(t)
	seedWorkBuddyChannel(t, db, workBuddyCredential("token-1", "intl", "cli"))

	now := time.Date(2026, 10, 12, 12, 0, 0, 0, workBuddyZone())
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		Type:             model.LogTypeConsume,
		CreatedAt:        now.Add(-1 * time.Hour).Unix(),
		ChannelId:        101,
		ModelName:        "deepseek-v4.1-flash",
		PromptTokens:     30_000_000,
		CompletionTokens: 1_000_000,
	}).Error)
	// An error row must not be counted as usage.
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		Type:      model.LogTypeError,
		CreatedAt: now.Add(-30 * time.Minute).Unix(),
		ChannelId: 101,
		ModelName: "deepseek-v4.1-flash",
	}).Error)

	report, err := SummarizeWorkBuddyUsage(context.Background(), WorkBuddyUsageQuery{
		Window: WorkBuddyUsageWindow24h,
		Now:    now,
	})
	require.NoError(t, err)
	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	assert.Equal(t, 101, row.ChannelId)
	assert.Equal(t, "uid-1", row.AccountUid)
	assert.Equal(t, "deepseek-v4.1-flash", row.Model)
	assert.Equal(t, int64(1), row.Requests)
	assert.Equal(t, int64(31_000_000), row.TotalTokens)
	assert.Equal(t, int64(200_000_000), row.BudgetTokens)
	assert.Equal(t, "seed", row.BudgetSource)
	assert.False(t, row.Cooling)
	assert.NotZero(t, report.Summaries["intl"].TotalTokens)
}
