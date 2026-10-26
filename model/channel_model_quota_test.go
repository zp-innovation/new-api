package model

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// resetChannelModelQuotaCache isolates a test from the package-level mirror.
func resetChannelModelQuotaCache(t *testing.T) {
	t.Helper()
	channelModelQuotaMu.Lock()
	previous := channelModelQuotaCache
	channelModelQuotaCache = map[string]ChannelModelQuota{}
	channelModelQuotaMu.Unlock()
	t.Cleanup(func() {
		channelModelQuotaMu.Lock()
		channelModelQuotaCache = previous
		channelModelQuotaMu.Unlock()
	})
}

// newQuotaTestDB swaps in a throwaway SQLite database for the package global.
func newQuotaTestDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ChannelModelQuota{}, &ChannelModelQuotaHistory{}, &Channel{}))
	DB = db
	t.Cleanup(func() { DB = previousDB })
}

func TestListChannelModelQuotaHistoryKeepsTheNewestRows(t *testing.T) {
	newQuotaTestDB(t)
	base := time.Now().Add(-time.Hour).Unix()
	rows := []ChannelModelQuotaHistory{
		{ChannelId: 1, Model: "gpt-5", RemainingFraction: 0.1, FetchedAt: base},
		{ChannelId: 1, Model: "gpt-5", RemainingFraction: 0.2, FetchedAt: base + 60},
		{ChannelId: 1, Model: "gpt-5", RemainingFraction: 0.3, FetchedAt: base + 120},
	}
	require.NoError(t, InsertChannelModelQuotaHistory(rows))

	limited, err := ListChannelModelQuotaHistory("gpt-5", time.Unix(base, 0), 2)
	require.NoError(t, err)
	require.Len(t, limited, 2)
	assert.Equal(t, base+60, limited[0].FetchedAt, "the older head of the window is trimmed first")
	assert.Equal(t, base+120, limited[1].FetchedAt)
	assert.True(t, limited[0].FetchedAt < limited[1].FetchedAt, "rows stay chronological")

	all, err := ListChannelModelQuotaHistory("gpt-5", time.Unix(base, 0), 0)
	require.NoError(t, err)
	assert.Len(t, all, 3, "a non-positive limit falls back to the default row limit")
}

func seedQuota(t *testing.T, row ChannelModelQuota) {
	t.Helper()
	channelModelQuotaMu.Lock()
	channelModelQuotaCache[channelModelQuotaKey(row.ChannelId, row.Model)] = row
	channelModelQuotaMu.Unlock()
}

func TestChannelModelQuotaExhaustedDetectsDrainedAccount(t *testing.T) {
	resetChannelModelQuotaCache(t)
	now := time.Now()
	reset := now.Add(2 * time.Hour)

	seedQuota(t, ChannelModelQuota{
		ChannelId:         40,
		Model:             "claude-opus-4-6-thinking",
		RemainingFraction: 0,
		ResetTime:         reset.Unix(),
		Status:            ChannelModelQuotaStatusOK,
		FetchedAt:         now.Unix(),
	})

	exhausted, got := ChannelModelQuotaExhausted(40, "claude-opus-4-6-thinking", now)
	require.True(t, exhausted)
	assert.WithinDuration(t, reset, got, time.Second)

	// The rejection is scoped to the pair, so the same account still serves its
	// other models.
	exhausted, _ = ChannelModelQuotaExhausted(40, "gemini-3.8-flash-high", now)
	assert.False(t, exhausted, "another model on the same account must stay available")
	exhausted, _ = ChannelModelQuotaExhausted(41, "claude-opus-4-6-thinking", now)
	assert.False(t, exhausted, "another account must stay available")
}

func TestChannelModelQuotaExhaustedTreatsTinyFractionAsDrained(t *testing.T) {
	resetChannelModelQuotaCache(t)
	now := time.Now()

	// Antigravity reports a drained account with a tiny non-zero fraction and
	// then answers 429, so an exact-zero test would keep routing into it.
	seedQuota(t, ChannelModelQuota{
		ChannelId:         38,
		Model:             "claude-opus-4-6-thinking",
		RemainingFraction: 0.0091824,
		ResetTime:         now.Add(time.Hour).Unix(),
		Status:            ChannelModelQuotaStatusOK,
		FetchedAt:         now.Unix(),
	})
	exhausted, _ := ChannelModelQuotaExhausted(38, "claude-opus-4-6-thinking", now)
	assert.True(t, exhausted)
}

func TestChannelModelQuotaExhaustedIsFailOpen(t *testing.T) {
	resetChannelModelQuotaCache(t)
	now := time.Now()
	const model = "claude-opus-4-6-thinking"

	tests := []struct {
		name string
		row  ChannelModelQuota
	}{
		{
			name: "unknown pair",
			row:  ChannelModelQuota{ChannelId: 999},
		},
		{
			name: "healthy remaining",
			row: ChannelModelQuota{ChannelId: 1, Model: model, RemainingFraction: 1,
				ResetTime: now.Add(time.Hour).Unix(), Status: ChannelModelQuotaStatusOK, FetchedAt: now.Unix()},
		},
		{
			name: "errored poll keeps the account in rotation",
			row: ChannelModelQuota{ChannelId: 1, Model: model, RemainingFraction: 0,
				ResetTime: now.Add(time.Hour).Unix(), Status: ChannelModelQuotaStatusError, FetchedAt: now.Unix()},
		},
		{
			name: "stale reading is ignored",
			row: ChannelModelQuota{ChannelId: 1, Model: model, RemainingFraction: 0,
				ResetTime: now.Add(time.Hour).Unix(), Status: ChannelModelQuotaStatusOK,
				FetchedAt: now.Add(-ChannelModelQuotaMaxAge - time.Minute).Unix()},
		},
		{
			name: "window already rolled over",
			row: ChannelModelQuota{ChannelId: 1, Model: model, RemainingFraction: 0,
				ResetTime: now.Add(-time.Minute).Unix(), Status: ChannelModelQuotaStatusOK, FetchedAt: now.Unix()},
		},
		{
			name: "no reset clock never parks an account forever",
			row: ChannelModelQuota{ChannelId: 1, Model: model, RemainingFraction: 0,
				ResetTime: 0, Status: ChannelModelQuotaStatusOK, FetchedAt: now.Unix()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seedQuota(t, tc.row)
			exhausted, _ := ChannelModelQuotaExhausted(tc.row.ChannelId, tc.row.Model, now)
			assert.False(t, exhausted)
			ClearChannelModelCooldown(tc.row.ChannelId, tc.row.Model)
		})
	}
}

func TestUpsertChannelModelQuotasWritesAndMirrors(t *testing.T) {
	newQuotaTestDB(t)
	resetChannelModelQuotaCache(t)
	now := time.Now().Unix()

	rows := []ChannelModelQuota{
		{ChannelId: 26, Model: "claude-opus-4-6-thinking", RemainingFraction: 0,
			ResetTime: now + 3600, Status: ChannelModelQuotaStatusOK, FetchedAt: now},
		{ChannelId: 26, Model: "gemini-3.8-flash-high", RemainingFraction: 1,
			ResetTime: now + 7200, Status: ChannelModelQuotaStatusOK, FetchedAt: now},
	}
	require.NoError(t, UpsertChannelModelQuotas(rows))

	stored, err := ListChannelModelQuotas()
	require.NoError(t, err)
	require.Len(t, stored, 2)

	// The mirror must serve the hot path without a query.
	exhausted, reset := ChannelModelQuotaExhausted(26, "claude-opus-4-6-thinking", time.Now())
	require.True(t, exhausted)
	assert.Equal(t, now+3600, reset.Unix())

	// A second batch updates in place instead of duplicating rows.
	updated := []ChannelModelQuota{
		{ChannelId: 26, Model: "claude-opus-4-6-thinking", RemainingFraction: 1,
			ResetTime: now + 9000, Status: ChannelModelQuotaStatusOK, FetchedAt: now + 60},
	}
	require.NoError(t, UpsertChannelModelQuotas(updated))
	stored, err = ListChannelModelQuotas()
	require.NoError(t, err)
	require.Len(t, stored, 2, "the upsert must update, not insert")

	exhausted, _ = ChannelModelQuotaExhausted(26, "claude-opus-4-6-thinking", time.Now())
	assert.False(t, exhausted, "the refreshed reading must take effect")
}

func TestUpsertChannelModelQuotasDefaultsStatus(t *testing.T) {
	newQuotaTestDB(t)
	resetChannelModelQuotaCache(t)
	now := time.Now().Unix()

	require.NoError(t, UpsertChannelModelQuotas([]ChannelModelQuota{
		{ChannelId: 27, Model: "gemini-3.8-flash-high", RemainingFraction: 1,
			ResetTime: now + 600, FetchedAt: now},
	}))
	stored, err := ListChannelModelQuotas()
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, ChannelModelQuotaStatusOK, stored[0].Status)
	assert.NotZero(t, stored[0].UpdatedAt)
}

func TestLoadAndDeleteChannelModelQuotaCache(t *testing.T) {
	newQuotaTestDB(t)
	resetChannelModelQuotaCache(t)
	now := time.Now().Unix()

	require.NoError(t, UpsertChannelModelQuotas([]ChannelModelQuota{
		{ChannelId: 36, Model: "claude-opus-4-6-thinking", RemainingFraction: 0,
			ResetTime: now + 3600, Status: ChannelModelQuotaStatusOK, FetchedAt: now},
		{ChannelId: 37, Model: "claude-opus-4-6-thinking", RemainingFraction: 0,
			ResetTime: now + 3600, Status: ChannelModelQuotaStatusOK, FetchedAt: now},
	}))

	// Wipe the mirror and prove the table can rebuild it after a restart.
	channelModelQuotaMu.Lock()
	channelModelQuotaCache = map[string]ChannelModelQuota{}
	channelModelQuotaMu.Unlock()
	require.NoError(t, LoadChannelModelQuotaCache())
	exhausted, _ := ChannelModelQuotaExhausted(36, "claude-opus-4-6-thinking", time.Now())
	assert.True(t, exhausted)

	require.NoError(t, DeleteChannelModelQuotas(36))
	exhausted, _ = ChannelModelQuotaExhausted(36, "claude-opus-4-6-thinking", time.Now())
	assert.False(t, exhausted, "a deleted channel must not keep stale quota")
	exhausted, _ = ChannelModelQuotaExhausted(37, "claude-opus-4-6-thinking", time.Now())
	assert.True(t, exhausted, "other channels are untouched")
}

func TestBuildChannelModelQuotaSummary(t *testing.T) {
	resetChannelModelQuotaCache(t)
	now := time.Now()
	early := now.Add(time.Hour)
	late := now.Add(5 * time.Hour)

	seedQuota(t, ChannelModelQuota{ChannelId: 26, Model: "claude-opus-4-6-thinking", RemainingFraction: 0,
		ResetTime: late.Unix(), Status: ChannelModelQuotaStatusOK, FetchedAt: now.Unix()})
	seedQuota(t, ChannelModelQuota{ChannelId: 28, Model: "claude-opus-4-6-thinking", RemainingFraction: 0,
		ResetTime: early.Unix(), Status: ChannelModelQuotaStatusOK, FetchedAt: now.Unix()})
	seedQuota(t, ChannelModelQuota{ChannelId: 26, Model: "gemini-3.8-flash-high", RemainingFraction: 1,
		ResetTime: late.Unix(), Status: ChannelModelQuotaStatusOK, FetchedAt: now.Unix()})

	names := map[int]string{26: "jack", 28: "steven"}
	summaries := BuildChannelModelQuotaSummary(now, names)
	require.Len(t, summaries, 2)

	byModel := map[string]ChannelModelQuotaSummary{}
	for _, summary := range summaries {
		byModel[summary.Model] = summary
	}

	claude := byModel["claude-opus-4-6-thinking"]
	assert.Equal(t, 2, claude.TotalAccounts)
	assert.Equal(t, 0, claude.AvailableAccounts)
	assert.Equal(t, early.Unix(), claude.EarliestReset, "the board needs the soonest recovery")
	require.Len(t, claude.Accounts, 2)
	assert.Equal(t, "steven", claude.Accounts[0].ChannelName)

	gemini := byModel["gemini-3.8-flash-high"]
	assert.Equal(t, 1, gemini.TotalAccounts)
	assert.Equal(t, 1, gemini.AvailableAccounts)
	assert.Zero(t, gemini.EarliestReset)
}

func TestChannelModelUnavailableCombinesBothSources(t *testing.T) {
	resetChannelModelQuotaCache(t)
	now := time.Now()
	const model = "claude-opus-4-6-thinking"

	// Source 1: a drained reading from the poller.
	seedQuota(t, ChannelModelQuota{ChannelId: 26, Model: model, RemainingFraction: 0,
		ResetTime: now.Add(time.Hour).Unix(), Status: ChannelModelQuotaStatusOK, FetchedAt: now.Unix()})
	assert.True(t, channelModelUnavailable(26, model, now))

	// Source 2: a 429 seen at request time, even with no snapshot.
	channelModelCooldowns.Store(channelModelCooldownKey(27, model), now.Add(time.Hour))
	t.Cleanup(func() { ClearChannelModelCooldown(27, model) })
	assert.True(t, channelModelUnavailable(27, model, now))

	// A pair that neither source flags stays available.
	assert.False(t, channelModelUnavailable(28, model, now))
}

func TestDropCoolingDownCandidatesSkipsDrainedPairs(t *testing.T) {
	resetChannelModelQuotaCache(t)
	now := time.Now()
	const model = "claude-opus-4-6-thinking"
	ids := []int{26, 27, 28}

	seedQuota(t, ChannelModelQuota{ChannelId: 26, Model: model, RemainingFraction: 0,
		ResetTime: now.Add(time.Hour).Unix(), Status: ChannelModelQuotaStatusOK, FetchedAt: now.Unix()})
	seedQuota(t, ChannelModelQuota{ChannelId: 27, Model: model, RemainingFraction: 1,
		ResetTime: now.Add(time.Hour).Unix(), Status: ChannelModelQuotaStatusOK, FetchedAt: now.Unix()})
	// 28 has no reading at all: routing must fail open and keep it.

	got, until := dropCoolingDownChannels(ids, model)
	assert.Equal(t, []int{27, 28}, got)
	assert.True(t, until.IsZero())

	abilities := []Ability{{ChannelId: 26}, {ChannelId: 27}, {ChannelId: 28}}
	gotAbilities, _ := dropCoolingDownAbilities(abilities, model)
	assert.Equal(t, []Ability{{ChannelId: 27}, {ChannelId: 28}}, gotAbilities)

	// Other models on the drained account are unaffected.
	got, until = dropCoolingDownChannels(ids, "gemini-3.8-flash-high")
	assert.Equal(t, ids, got)
	assert.True(t, until.IsZero())
}

func TestFilterQuotaSummariesDefaultsToModelsInUse(t *testing.T) {
	// tx101's upstream catalogue reports 31 models while its channels route 3.
	summaries := []ChannelModelQuotaSummary{
		{Model: "chat_20706", TotalAccounts: 10, AvailableAccounts: 10},
		{Model: "claude-opus-4-6-thinking", TotalAccounts: 10, AvailableAccounts: 0},
		{Model: "gemini-3.8-flash-high", TotalAccounts: 10, AvailableAccounts: 10},
		{Model: "chat_23310", TotalAccounts: 10, AvailableAccounts: 10},
		{Model: "claude-sonnet-4-6", TotalAccounts: 10, AvailableAccounts: 0},
	}
	served := map[string]struct{}{
		"claude-opus-4-6-thinking": {},
		"claude-sonnet-4-6":        {},
		"gemini-3.8-flash-high":    {},
	}

	visible, hidden := FilterQuotaSummaries(summaries, served, false)
	require.Len(t, visible, 3)
	assert.Equal(t, 2, hidden)
	for _, summary := range visible {
		assert.True(t, summary.InUse, summary.Model)
	}
	// Drained models lead so an operator sees the problem first.
	assert.Equal(t, "claude-opus-4-6-thinking", visible[0].Model)
	assert.Equal(t, "claude-sonnet-4-6", visible[1].Model)
	assert.Equal(t, "gemini-3.8-flash-high", visible[2].Model)

	// The explicit toggle returns the full catalogue with upstream-only models
	// after the routed ones.
	all, hiddenAll := FilterQuotaSummaries(summaries, served, true)
	require.Len(t, all, 5)
	assert.Zero(t, hiddenAll)
	assert.True(t, all[0].InUse)
	assert.True(t, all[2].InUse)
	assert.False(t, all[3].InUse)
	assert.False(t, all[4].InUse)
}

func TestFilterQuotaSummariesWithoutChannelsKeepsNothingRouted(t *testing.T) {
	summaries := []ChannelModelQuotaSummary{
		{Model: "a", TotalAccounts: 1, AvailableAccounts: 1},
	}
	visible, hidden := FilterQuotaSummaries(summaries, map[string]struct{}{}, false)
	assert.Empty(t, visible)
	assert.Equal(t, 1, hidden)

	all, _ := FilterQuotaSummaries(summaries, map[string]struct{}{}, true)
	require.Len(t, all, 1)
	assert.False(t, all[0].InUse)
}

func TestChannelModelQuotaExhaustedReloadsMirrorFromTable(t *testing.T) {
	newQuotaTestDB(t)
	resetChannelModelQuotaCache(t)
	previousRefresh := quotaCacheRefreshedAt.Swap(0)
	t.Cleanup(func() { quotaCacheRefreshedAt.Store(previousRefresh) })

	now := time.Now()
	require.NoError(t, UpsertChannelModelQuotas([]ChannelModelQuota{{
		ChannelId:         40,
		Model:             "claude-opus-4-6-thinking",
		RemainingFraction: 0,
		ResetTime:         now.Add(time.Hour).Unix(),
		Status:            ChannelModelQuotaStatusOK,
		FetchedAt:         now.Unix(),
	}}))

	// A node that never runs the poller starts with whatever it read at boot.
	// Simulate it missing the latest row and confirm the read path reloads.
	channelModelQuotaMu.Lock()
	channelModelQuotaCache = map[string]ChannelModelQuota{}
	channelModelQuotaMu.Unlock()
	quotaCacheRefreshedAt.Store(0)

	exhausted, reset := ChannelModelQuotaExhausted(40, "claude-opus-4-6-thinking", now)
	require.True(t, exhausted, "the read path must reload a missing row from the table")
	assert.WithinDuration(t, now.Add(time.Hour), reset, time.Second)
}

func TestResolveUpstreamModelNameFollowsAlias(t *testing.T) {
	mapping := `{"claude-opus-4-6":"claude-opus-4-6-thinking","a":"b","b":"c"}`
	channel := &Channel{ModelMapping: &mapping}
	assert.Equal(t, "claude-opus-4-6-thinking", channel.ResolveUpstreamModelName("claude-opus-4-6"))
	assert.Equal(t, "c", channel.ResolveUpstreamModelName("a"), "chains resolve to the tail")
	assert.Equal(t, "unmapped", channel.ResolveUpstreamModelName("unmapped"))

	empty := &Channel{}
	assert.Equal(t, "x", empty.ResolveUpstreamModelName("x"))

	cyclic := `{"x":"y","y":"x"}`
	cyclicChannel := &Channel{ModelMapping: &cyclic}
	assert.Equal(t, "x", cyclicChannel.ResolveUpstreamModelName("x"), "a cycle falls back to the origin")
}

func TestChannelModelUnavailableUsesResolvedAlias(t *testing.T) {
	resetChannelModelQuotaCache(t)
	previous := channelsIDM
	mapping := `{"claude-opus-4-6":"claude-opus-4-6-thinking"}`
	channelsIDM = map[int]*Channel{777: {Id: 777, ModelMapping: &mapping}}
	t.Cleanup(func() { channelsIDM = previous })

	now := time.Now()
	// Only the upstream id has a drained reading; the client-facing alias does
	// not appear in the snapshot at all.
	seedQuota(t, ChannelModelQuota{
		ChannelId: 777, Model: "claude-opus-4-6-thinking", RemainingFraction: 0,
		ResetTime: now.Add(time.Hour).Unix(), Status: ChannelModelQuotaStatusOK, FetchedAt: now.Unix(),
	})

	assert.True(t, channelModelUnavailable(777, "claude-opus-4-6", now),
		"the alias must inherit the upstream model's quota state")
	assert.True(t, channelModelUnavailable(777, "claude-opus-4-6-thinking", now))
	assert.False(t, channelModelUnavailable(777, "gemini-3.8-flash-high", now))

	// The same alias resolution has to feed the recovery lookup, otherwise the
	// caller loses the reset and answers 503 without Retry-After.
	until, ok := ChannelModelUnavailableUntil(777, "claude-opus-4-6")
	require.True(t, ok, "the alias must report the upstream reset")
	assert.WithinDuration(t, now.Add(time.Hour), until, time.Second)
	_, ok = ChannelModelUnavailableUntil(777, "gemini-3.8-flash-high")
	assert.False(t, ok, "an unrelated model keeps no recovery window")
}

// The previous reading has to survive a restart: the row is the only state a
// new process can read, so a cache-only mirror would leave burn-rate and
// exhausted_at empty after every deploy.
func TestUpsertChannelModelQuotasPersistsPreviousReading(t *testing.T) {
	resetChannelModelQuotaCache(t)
	newQuotaTestDB(t)
	now := time.Now().Unix()
	require.NoError(t, UpsertChannelModelQuotas([]ChannelModelQuota{{
		ChannelId:         51,
		Model:             "claude-opus-4-6",
		RemainingFraction: 1,
		ResetTime:         now + 3600,
		Status:            ChannelModelQuotaStatusOK,
		FetchedAt:         now,
	}}))

	resetChannelModelQuotaCache(t)
	require.NoError(t, UpsertChannelModelQuotas([]ChannelModelQuota{{
		ChannelId:         51,
		Model:             "claude-opus-4-6",
		RemainingFraction: 0.5,
		ResetTime:         now + 3600,
		Status:            ChannelModelQuotaStatusOK,
		FetchedAt:         now + 60,
	}}))

	var stored ChannelModelQuota
	require.NoError(t, DB.Where("channel_id = ? AND model = ?", 51, "claude-opus-4-6").First(&stored).Error)
	assert.InDelta(t, 0.5, stored.RemainingFraction, 1e-9)
	assert.InDelta(t, 1, stored.PreviousRemainingFraction, 1e-9)
	assert.Equal(t, now, stored.PreviousFetchedAt)
}

// Deleting the last channel leaves the sweep with no surviving id to exclude,
// and GORM rejects an unconditional delete by default: the stale rows must go
// anyway, otherwise a recycled channel id inherits the old quota.
func TestPurgeStaleChannelModelQuotasHandlesAnEmptyChannelTable(t *testing.T) {
	resetChannelModelQuotaCache(t)
	newQuotaTestDB(t)
	now := time.Now().Unix()
	require.NoError(t, DB.Create(&ChannelModelQuota{
		ChannelId:         61,
		Model:             "claude-opus-4-6",
		RemainingFraction: 1,
		ResetTime:         now + 3600,
		Status:            ChannelModelQuotaStatusOK,
		FetchedAt:         now,
	}).Error)

	removed, err := PurgeStaleChannelModelQuotas()
	require.NoError(t, err)
	assert.EqualValues(t, 1, removed)

	var remaining int64
	require.NoError(t, DB.Model(&ChannelModelQuota{}).Count(&remaining).Error)
	assert.Zero(t, remaining)
}
