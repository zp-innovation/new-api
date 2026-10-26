package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupWorkBuddyTaskDB gives the runner an isolated sqlite database with the
// channel table and the two WorkBuddy tables.
func setupWorkBuddyTaskDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := t.TempDir() + "/workbuddy-tasks.db"
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)

	savedDB := model.DB
	savedLogDB := model.LOG_DB
	savedPath := common.SQLitePath
	savedMaster := common.IsMasterNode
	model.DB = db
	common.SQLitePath = path
	// Only the master node migrates, and the test database has to be migrated.
	common.IsMasterNode = true
	t.Setenv("SQL_DSN", "local")
	t.Cleanup(func() {
		// InitDB opens its own connection to the same file, so both have to be
		// closed before the temporary directory can go away.
		for _, handle := range []*gorm.DB{model.DB, db} {
			if handle == nil {
				continue
			}
			if sqlDB, err := handle.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		model.DB = savedDB
		model.LOG_DB = savedLogDB
		common.SQLitePath = savedPath
		common.IsMasterNode = savedMaster
	})
	require.NoError(t, model.InitDB())
	// The log handle is a package global too, so it must be pointed at this
	// test's database; otherwise rows written by an earlier test in the same
	// package leak into every later aggregation.
	require.NoError(t, model.InitLogDB())
	return db
}

// stubWorkBuddyTaskClient aims the runner at a test upstream.
func stubWorkBuddyTaskClient(t *testing.T, server *httptest.Server) {
	t.Helper()
	original := workbuddyTaskClient
	workbuddyTaskClient = func(realm, product, proxyURL string) (*wbcore.Client, error) {
		client := wbcore.NewClient(server.Client(), server.URL, wbcore.NormalizeRealm(realm), wbcore.NormalizeProduct(product))
		client.BillingBaseURL = server.URL
		client.WebBaseURL = server.URL
		return client, nil
	}
	t.Cleanup(func() { workbuddyTaskClient = original })
}

func seedWorkBuddyChannel(t *testing.T, db *gorm.DB, key string) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Id:     101,
		Name:   "WorkBuddy test",
		Type:   constant.ChannelTypeWorkBuddy,
		Status: common.ChannelStatusEnabled,
		Key:    key,
	}
	require.NoError(t, db.Create(channel).Error)
	return channel
}

// The runner resolves the accounts from the channels, signs the CN account in,
// stores the day and records the attempt. A second scheduled pass the same day
// is a no-op, which is what keeps the upstream from seeing a second sign-in.
func TestRunWorkBuddyTasksSignsInAndRecordsTheDay(t *testing.T) {
	db := setupWorkBuddyTaskDB(t)
	var checkinCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wbcore.CheckinPath {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		checkinCalls++
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer server.Close()
	stubWorkBuddyTaskClient(t, server)

	seedWorkBuddyChannel(t, db, workBuddyCredential("token-1", "cn", "cli"))
	now := time.Now()

	summary, err := RunWorkBuddyTasks(context.Background(), WorkBuddyTaskOptions{
		Actions: []string{WorkBuddyTaskCheckin},
		Trigger: WorkBuddyTriggerScheduler,
		Now:     now,
	})
	require.NoError(t, err)
	require.Len(t, summary.Results, 1)
	assert.True(t, summary.Results[0].OK)
	assert.False(t, summary.Results[0].Skipped)
	assert.Equal(t, 1, checkinCalls)

	state, err := model.GetWorkBuddyAccountState(101, "uid-1")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, now.Unix(), state.LastCheckinAt)
	assert.Equal(t, "checkin", state.LastTask)
	assert.Equal(t, "ok", state.LastStatus)

	runs, total, err := model.ListWorkBuddyTaskRuns(model.WorkBuddyTaskRunFilter{ChannelId: 101})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, runs, 1)
	assert.True(t, runs[0].OK)

	// The same scheduled day does not sign in twice.
	second, err := RunWorkBuddyTasks(context.Background(), WorkBuddyTaskOptions{
		Actions: []string{WorkBuddyTaskCheckin},
		Trigger: WorkBuddyTriggerScheduler,
		Now:     now,
	})
	require.NoError(t, err)
	require.Len(t, second.Results, 1)
	assert.True(t, second.Results[0].Skipped)
	assert.Equal(t, 1, checkinCalls, "the scheduled gate must keep the upstream call from repeating")

	// A manual run deliberately ignores the gate: the operator asked for it.
	manual, err := RunWorkBuddyTasks(context.Background(), WorkBuddyTaskOptions{
		Actions: []string{WorkBuddyTaskCheckin},
		Trigger: WorkBuddyTriggerManual,
		Now:     now,
	})
	require.NoError(t, err)
	require.Len(t, manual.Results, 1)
	assert.False(t, manual.Results[0].Skipped)
	assert.Equal(t, 2, checkinCalls)
}

func TestWorkBuddyAccountTaskSwitchIsIndependentFromChannelRouting(t *testing.T) {
	db := setupWorkBuddyTaskDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer server.Close()
	stubWorkBuddyTaskClient(t, server)
	seedWorkBuddyChannel(t, db, workBuddyCredential("token-1", "cn", "cli"))

	account, err := SetWorkBuddyAccountTasksEnabled(101, "uid-1", false)
	require.NoError(t, err)
	assert.False(t, account.TasksEnabled())
	assert.Equal(t, common.ChannelStatusEnabled, account.ChannelStatus, "API routing state is untouched")

	skipped, err := RunWorkBuddyTasks(context.Background(), WorkBuddyTaskOptions{
		Actions: []string{WorkBuddyTaskCheckin},
		Trigger: WorkBuddyTriggerScheduler,
	})
	require.NoError(t, err)
	assert.Empty(t, skipped.Results, "a scheduled pass honours the account task switch")

	manual, err := RunWorkBuddyTasks(context.Background(), WorkBuddyTaskOptions{
		Actions:                 []string{WorkBuddyTaskCheckin},
		Trigger:                 WorkBuddyTriggerManual,
		ChannelIds:              []int{101},
		IgnoreTaskParticipation: true,
	})
	require.NoError(t, err)
	require.Len(t, manual.Results, 1)
	assert.True(t, manual.Results[0].OK, "an explicit single-account manual run still executes")
}

// Sign-in is CN-only: an international account must be skipped before any
// upstream call is made.
func TestRunWorkBuddyTasksSkipsTheInternationalCheckin(t *testing.T) {
	db := setupWorkBuddyTaskDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no upstream call expected, got %s", r.URL.Path)
	}))
	defer server.Close()
	stubWorkBuddyTaskClient(t, server)

	seedWorkBuddyChannel(t, db, workBuddyCredential("token-1", "intl", "cli"))
	summary, err := RunWorkBuddyTasks(context.Background(), WorkBuddyTaskOptions{
		Actions: []string{WorkBuddyTaskCheckin},
		Trigger: WorkBuddyTriggerManual,
	})
	require.NoError(t, err)
	require.Len(t, summary.Results, 1)
	assert.True(t, summary.Results[0].Skipped)
	assert.Contains(t, summary.Results[0].Message, "China-realm")
}

// A multi-account channel is two rows: the panel and the runner both work per
// account, not per channel.
func TestListWorkBuddyAccountsExpandsEveryCredential(t *testing.T) {
	db := setupWorkBuddyTaskDB(t)
	// The shared helper hard-codes uid-1, so this channel builds its two lines
	// with distinct uids: the state is keyed by (channel, uid).
	cnLine := `{"type":"workbuddy","realm":"cn","product":"cli","uid":"uid-1","access_token":"token-1","refresh_token":"refresh-1","expires_at":"2099-01-01T00:00:00Z"}`
	intlLine := `{"type":"workbuddy","realm":"intl","product":"cli","uid":"uid-2","access_token":"token-2","refresh_token":"refresh-2","expires_at":"2099-01-01T00:00:00Z"}`
	seedWorkBuddyChannel(t, db, cnLine+"\n"+intlLine)

	accounts, err := ListWorkBuddyAccounts(nil, "")
	require.NoError(t, err)
	require.Len(t, accounts, 2)
	assert.True(t, accounts[0].IsCN())
	assert.False(t, accounts[1].IsCN())

	// The realm filter splits the same channel's accounts, and the summary
	// counts each realm apart.
	cnOnly, err := ListWorkBuddyAccounts(nil, WorkBuddyRealmCN)
	require.NoError(t, err)
	require.Len(t, cnOnly, 1)
	assert.True(t, cnOnly[0].IsCN())

	intlOnly, err := ListWorkBuddyAccounts(nil, WorkBuddyRealmIntl)
	require.NoError(t, err)
	require.Len(t, intlOnly, 1)
	assert.False(t, intlOnly[0].IsCN())

	now := time.Now()
	require.NoError(t, model.SaveWorkBuddyAccountState(&model.WorkBuddyAccountState{
		ChannelId: 101, AccountUid: "uid-1", Realm: "cn", Product: "cli",
		StreakDays: 4, RemainCredit: 120, LastCheckinAt: now.Unix(),
	}))
	require.NoError(t, model.SaveWorkBuddyAccountState(&model.WorkBuddyAccountState{
		ChannelId: 101, AccountUid: "uid-2", Realm: "intl", Product: "cli",
		RemainCredit: 30, LastDailyChatAt: now.Unix(),
	}))
	accounts, err = ListWorkBuddyAccounts(nil, WorkBuddyRealmAll)
	require.NoError(t, err)
	summaries := SummarizeWorkBuddyAccounts(accounts, now)
	assert.Equal(t, 1, summaries[WorkBuddyRealmCN].Accounts)
	assert.Equal(t, 1, summaries[WorkBuddyRealmCN].DoneToday)
	assert.Equal(t, float64(120), summaries[WorkBuddyRealmCN].RemainCredit)
	assert.Equal(t, float64(4), summaries[WorkBuddyRealmCN].StreakAverage)
	assert.Equal(t, 1, summaries[WorkBuddyRealmIntl].Accounts)
	assert.Equal(t, 1, summaries[WorkBuddyRealmIntl].DoneToday)
	assert.Equal(t, float64(30), summaries[WorkBuddyRealmIntl].RemainCredit)
}

// The background refresh renews every account of a channel that is due and
// leaves the ones that are not alone, which is what keeps a lapsed token from
// turning into a 401 (and an auto-ban) on the relay path.
func TestRefreshWorkBuddyChannelLinesRenewsOnlyTheDueAccounts(t *testing.T) {
	db := setupWorkBuddyTaskDB(t)
	server := newWorkBuddyOAuthServer(t, "access-1", "refresh-1")
	defer server.Close()
	stubWorkBuddyOAuthClient(t, server)

	soon := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
	due := `{"type":"workbuddy","realm":"intl","product":"cli","uid":"uid-due","access_token":"access-1","refresh_token":"refresh-1","expires_at":"` + soon + `"}`
	fresh := `{"type":"workbuddy","realm":"intl","product":"cli","uid":"uid-fresh","access_token":"access-1","refresh_token":"refresh-1","expires_at":"2099-01-01T00:00:00Z"}`
	channel := &model.Channel{Id: 201, Name: "WorkBuddy multi", Type: constant.ChannelTypeWorkBuddy, Status: common.ChannelStatusEnabled, Key: due + "\n" + fresh}
	require.NoError(t, db.Create(channel).Error)

	count, err := refreshWorkBuddyChannelLines(context.Background(), channel, "", time.Now(), 30*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var stored model.Channel
	require.NoError(t, db.First(&stored, 201).Error)
	lines := strings.Split(stored.Key, "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "access-2", "the due account must carry the rotated token")
	assert.Contains(t, lines[1], "access-1", "the account that is not due must be left alone")
	assert.Contains(t, lines[0], "refresh-2", "the rotated refresh token has to be stored")
}

// The daily budget is the safety bound the growth centre runs behind.
func TestConsumeWorkBuddyRequestQuotaStopsAtTheLimit(t *testing.T) {
	setupWorkBuddyTaskDB(t)
	require.NoError(t, model.SaveWorkBuddyAccountState(&model.WorkBuddyAccountState{
		ChannelId: 101, AccountUid: "uid-1", Realm: "cn", Product: "cli",
	}))
	date := time.Now().Format("2006-01-02")
	for i := range 2 {
		allowed, err := model.ConsumeWorkBuddyRequestQuota(101, "uid-1", date, 2)
		require.NoError(t, err)
		assert.True(t, allowed, "call %d should be inside the budget", i+1)
	}
	allowed, err := model.ConsumeWorkBuddyRequestQuota(101, "uid-1", date, 2)
	require.NoError(t, err)
	assert.False(t, allowed, "the third call must be refused")

	// A new local day resets the counter.
	next := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	allowed, err = model.ConsumeWorkBuddyRequestQuota(101, "uid-1", next, 2)
	require.NoError(t, err)
	assert.True(t, allowed)
}
