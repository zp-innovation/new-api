package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The daily rollup must attribute usage to the UTC+8 calendar day the request
// happened on, not to the day of the newest request in the batch. Grouping by
// the newest timestamp would have swept two days into one row.
func TestSyncWorkBuddyUsageDailyAttributesEachDay(t *testing.T) {
	db := setupWorkBuddyTaskDB(t)
	seedWorkBuddyChannel(t, db, workBuddyCredential("token-daily", "intl", "cli"))

	zone := workBuddyZone()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, zone)
	yesterday := now.AddDate(0, 0, -1)

	write := func(at time.Time, prompt, completion int) {
		require.NoError(t, model.LOG_DB.Create(&model.Log{
			Type:             model.LogTypeConsume,
			CreatedAt:        at.Unix(),
			ChannelId:        101,
			ModelName:        "deepseek-v4.1-flash",
			PromptTokens:     prompt,
			CompletionTokens: completion,
		}).Error)
	}
	write(yesterday, 30_000_000, 1_000_000)
	write(now.Add(-1*time.Hour), 20_000_000, 500_000)

	require.NoError(t, SyncWorkBuddyUsageDaily(context.Background(), now))

	rows, err := model.ListWorkBuddyUsageDaily([]int{101}, "2026-10-10")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byDay := map[string]model.WorkBuddyUsageDaily{}
	for _, row := range rows {
		byDay[row.Day] = row
	}
	require.Contains(t, byDay, "2026-10-11")
	require.Contains(t, byDay, "2026-10-12")
	assert.Equal(t, int64(31_000_000), byDay["2026-10-11"].TotalTokens)
	assert.Equal(t, int64(1), byDay["2026-10-11"].Requests)
	assert.Equal(t, int64(20_500_000), byDay["2026-10-12"].TotalTokens)
	assert.Equal(t, int64(1), byDay["2026-10-12"].Requests)
}

// A second pass must be idempotent: it rewrites the same absolute totals, so a
// restart never doubles a day's counters and never leaves a stale row behind.
func TestSyncWorkBuddyUsageDailyIsIdempotent(t *testing.T) {
	db := setupWorkBuddyTaskDB(t)
	seedWorkBuddyChannel(t, db, workBuddyCredential("token-idem", "intl", "cli"))

	zone := workBuddyZone()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, zone)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		Type:             model.LogTypeConsume,
		CreatedAt:        now.Add(-2 * time.Hour).Unix(),
		ChannelId:        101,
		ModelName:        "deepseek-v4.1-flash",
		PromptTokens:     5_000_000,
		CompletionTokens: 100,
	}).Error)

	require.NoError(t, SyncWorkBuddyUsageDaily(context.Background(), now))
	require.NoError(t, SyncWorkBuddyUsageDaily(context.Background(), now))

	rows, err := model.ListWorkBuddyUsageDaily([]int{101}, "2026-10-12")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(5_000_100), rows[0].TotalTokens)
	assert.Equal(t, int64(1), rows[0].Requests)
}
