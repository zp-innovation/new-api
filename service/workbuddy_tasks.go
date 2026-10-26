package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// The task names travel through the API, the scheduler and the history table, so
// they are constants rather than literals.
const (
	WorkBuddyTaskCheckin        = "checkin"
	WorkBuddyTaskActivityReport = "activity_report"
	WorkBuddyTaskTravel         = "travel"
	WorkBuddyTaskStreakBonus    = "streak_bonus"
	WorkBuddyTaskGrowth         = "growth"
	WorkBuddyTaskNightCat       = "night_cat"
	WorkBuddyTaskDailyChat      = "daily_chat"
	// WorkBuddyTaskRefreshCredits re-reads one account's balance and streak. It
	// is manual-only: the scheduled pass must not multiply its upstream calls by
	// four just to keep a cached number fresh.
	WorkBuddyTaskRefreshCredits = "refresh_credits"
)

// Trigger sources, recorded with every attempt.
const (
	WorkBuddyTriggerManual    = "manual"
	WorkBuddyTriggerScheduler = "scheduler"
)

// WorkBuddyAccountsActions lists every action the runner understands, in the
// order a scheduled pass runs them. Refreshing the cached balance is part of the
// set but never of the schedule: it is requested from the panel.
var WorkBuddyAccountsActions = []string{
	WorkBuddyTaskCheckin,
	WorkBuddyTaskActivityReport,
	WorkBuddyTaskTravel,
	WorkBuddyTaskStreakBonus,
	WorkBuddyTaskGrowth,
	WorkBuddyTaskDailyChat,
	WorkBuddyTaskRefreshCredits,
}

// WorkBuddyAccount is one credential inside one channel. A channel key can hold
// several accounts, one compact credential per line, so the panel and the runner
// work per account rather than per channel.
type WorkBuddyAccount struct {
	ChannelId     int
	ChannelName   string
	ChannelStatus int
	Proxy         string
	Credential    *wbcore.Credential
	State         *model.WorkBuddyAccountState
}

func (a *WorkBuddyAccount) UID() string {
	if a == nil || a.Credential == nil {
		return ""
	}
	return strings.TrimSpace(a.Credential.UID)
}

func (a *WorkBuddyAccount) Nickname() string {
	if a == nil || a.Credential == nil {
		return ""
	}
	return strings.TrimSpace(a.Credential.Nickname)
}

func (a *WorkBuddyAccount) Realm() wbcore.Realm {
	if a == nil || a.Credential == nil {
		return wbcore.RealmIntl
	}
	return a.Credential.Identity().Realm
}

func (a *WorkBuddyAccount) IsCN() bool { return a.Realm() == wbcore.RealmCN }

// TasksEnabled reports whether scheduled welfare work includes this account.
// Old credentials without the field keep following the channel's API state,
// while an explicit false allows an account to keep serving traffic without
// checking in or running daily tasks.
func (a *WorkBuddyAccount) TasksEnabled() bool {
	if a == nil || a.Credential == nil || a.Credential.TasksEnabled == nil {
		return a != nil && a.ChannelStatus == common.ChannelStatusEnabled
	}
	return *a.Credential.TasksEnabled
}

// IsEnterprise reports whether the account belongs to a team space, which has no
// growth centre and cannot sign in.
func (a *WorkBuddyAccount) IsEnterprise() bool {
	if a == nil || a.Credential == nil {
		return false
	}
	identity := a.Credential.Identity()
	return identity.EnterpriseID != "" || wbcore.TokenEnterpriseID(identity.AccessToken) != ""
}

// The realm filter the console passes through: empty or "all" means every
// realm, anything else selects one.
const (
	WorkBuddyRealmAll  = "all"
	WorkBuddyRealmCN   = "cn"
	WorkBuddyRealmIntl = "intl"
)

// NormalizeWorkBuddyRealm collapses the accepted spellings; an unknown value
// reads as "every realm" rather than silently matching nothing.
func NormalizeWorkBuddyRealm(realm string) string {
	switch strings.ToLower(strings.TrimSpace(realm)) {
	case WorkBuddyRealmCN:
		return WorkBuddyRealmCN
	case WorkBuddyRealmIntl:
		return WorkBuddyRealmIntl
	}
	return WorkBuddyRealmAll
}

// WorkBuddyAccountMatchesRealm reports whether an account belongs to a realm
// filter; "all" (or anything unrecognised) matches every account.
func WorkBuddyAccountMatchesRealm(account *WorkBuddyAccount, realm string) bool {
	switch NormalizeWorkBuddyRealm(realm) {
	case WorkBuddyRealmCN:
		return account.IsCN()
	case WorkBuddyRealmIntl:
		return !account.IsCN()
	}
	return true
}

// WorkBuddyRealmSummary is the per-realm headline the console shows above the
// account table.
type WorkBuddyRealmSummary struct {
	Realm         string  `json:"realm"`
	Accounts      int     `json:"accounts"`
	DoneToday     int     `json:"done_today"`
	StreakAverage float64 `json:"streak_average"`
	RemainCredit  float64 `json:"remain_credit"`
	FailedLastRun int     `json:"failed_last_run"`
	LastRunAt     int64   `json:"last_run_at"`
}

// SummarizeWorkBuddyAccounts aggregates one realm's accounts. It runs on the
// server so the numbers always agree with the filtered table.
func SummarizeWorkBuddyAccounts(accounts []*WorkBuddyAccount, now time.Time) map[string]WorkBuddyRealmSummary {
	summaries := map[string]WorkBuddyRealmSummary{
		WorkBuddyRealmCN:   {Realm: WorkBuddyRealmCN},
		WorkBuddyRealmIntl: {Realm: WorkBuddyRealmIntl},
	}
	streakTotals := map[string]int{}
	today := now.Format("2006-01-02")
	for _, account := range accounts {
		realm := WorkBuddyRealmIntl
		if account.IsCN() {
			realm = WorkBuddyRealmCN
		}
		summary := summaries[realm]
		summary.Accounts++
		if account.State != nil {
			state := account.State
			summary.RemainCredit += state.RemainCredit
			if state.LastRunAt > summary.LastRunAt {
				summary.LastRunAt = state.LastRunAt
			}
			if state.LastStatus == "failed" {
				summary.FailedLastRun++
			}
			if realm == WorkBuddyRealmCN {
				streakTotals[realm] += state.StreakDays
				if isSameLocalDay(state.LastCheckinAt, today) {
					summary.DoneToday++
				}
			} else if isSameLocalDay(state.LastDailyChatAt, today) {
				summary.DoneToday++
			}
		}
		summaries[realm] = summary
	}
	for realm, summary := range summaries {
		if summary.Accounts > 0 && realm == WorkBuddyRealmCN {
			summary.StreakAverage = float64(streakTotals[realm]) / float64(summary.Accounts)
		}
		summaries[realm] = summary
	}
	return summaries
}

func isSameLocalDay(unix int64, day string) bool {
	return unix > 0 && time.Unix(unix, 0).Format("2006-01-02") == day
}

// WorkBuddyTaskOptions selects what a run does.
type WorkBuddyTaskOptions struct {
	// ChannelIds limits the run; empty means every WorkBuddy channel.
	ChannelIds []int
	// Realm limits the run to one realm; empty or "all" means every realm.
	Realm string
	// Actions limits the run; empty means every action the settings enable.
	Actions []string
	// AccountUid limits the run to one account of the selected channels. The
	// console's "run this account" action sets it; empty runs every account in
	// scope.
	AccountUid string
	// Trigger is WorkBuddyTriggerManual or WorkBuddyTriggerScheduler. A manual
	// run ignores the "already done today" gates, a scheduled run honours them.
	Trigger string
	// IgnoreTaskParticipation is reserved for an explicit single-account manual
	// run: the operator chose that account, so the automatic task switch must
	// not silently discard the request.
	IgnoreTaskParticipation bool
	// Now is injectable for tests.
	Now time.Time
}

// WorkBuddyAccountResult is one action against one account.
type WorkBuddyAccountResult struct {
	ChannelId  int     `json:"channel_id"`
	AccountUid string  `json:"account_uid"`
	Nickname   string  `json:"nickname"`
	Realm      string  `json:"realm"`
	Action     string  `json:"action"`
	OK         bool    `json:"ok"`
	Skipped    bool    `json:"skipped"`
	Message    string  `json:"message"`
	Credit     float64 `json:"credit"`
}

// WorkBuddySkippedAccount reports an account a run deliberately left alone, so
// a caller does not see a "succeeded" task with no trace of what it asked for.
type WorkBuddySkippedAccount struct {
	ChannelId  int    `json:"channel_id"`
	AccountUid string `json:"account_uid"`
	Nickname   string `json:"nickname"`
	Realm      string `json:"realm"`
	Reason     string `json:"reason"`
}

// WorkBuddyTaskSummary is one run's outcome.
type WorkBuddyTaskSummary struct {
	Trigger     string                    `json:"trigger"`
	Accounts    int                       `json:"accounts"`
	Actions     int                       `json:"actions"`
	CreditDelta float64                   `json:"credit_delta"`
	Results     []WorkBuddyAccountResult  `json:"results"`
	Skipped     []WorkBuddySkippedAccount `json:"skipped,omitempty"`
	StartedAt   int64                     `json:"started_at"`
	FinishedAt  int64                     `json:"finished_at"`
}

// ListWorkBuddyAccounts returns every account of every WorkBuddy channel with its
// stored state, optionally limited to one realm. The list is derived from the
// channels themselves, so a channel that was just imported appears without any
// registration step.
func ListWorkBuddyAccounts(channelIds []int, realm string) ([]*WorkBuddyAccount, error) {
	realm = NormalizeWorkBuddyRealm(realm)
	query := model.DB.Where("type = ?", constant.ChannelTypeWorkBuddy)
	if len(channelIds) > 0 {
		query = query.Where("id IN ?", channelIds)
	}
	var channels []model.Channel
	if err := query.Find(&channels).Error; err != nil {
		return nil, err
	}

	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.Id)
	}
	states, err := model.ListWorkBuddyAccountStates(ids)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]*model.WorkBuddyAccountState, len(states))
	for index := range states {
		state := &states[index]
		byKey[accountStateKey(state.ChannelId, state.AccountUid)] = state
	}

	accounts := make([]*WorkBuddyAccount, 0, len(channels))
	for _, channel := range channels {
		setting := channel.GetSetting()
		for _, credential := range workBuddyCredentials(channel.Key) {
			account := &WorkBuddyAccount{
				ChannelId:     channel.Id,
				ChannelName:   channel.Name,
				ChannelStatus: channel.Status,
				Proxy:         setting.Proxy,
				Credential:    credential,
			}
			account.State = byKey[accountStateKey(channel.Id, account.UID())]
			if !WorkBuddyAccountMatchesRealm(account, realm) {
				continue
			}
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func accountStateKey(channelId int, uid string) string {
	return fmt.Sprintf("%d|%s", channelId, uid)
}

// workBuddyCredentials parses every credential line of a channel key, skipping
// the ones that do not parse: a half-edited key must not hide the rest.
func workBuddyCredentials(rawKey string) []*wbcore.Credential {
	credentials := make([]*wbcore.Credential, 0, 1)
	for line := range strings.SplitSeq(rawKey, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		credential, err := wbcore.ParseCredential(trimmed)
		if err != nil {
			continue
		}
		credentials = append(credentials, credential)
	}
	return credentials
}

// SetWorkBuddyAccountTasksEnabled writes the account-level task switch back
// into its compact credential line. The UID identifies the line because a
// channel key may hold several accounts.
func SetWorkBuddyAccountTasksEnabled(channelId int, uid string, enabled bool) (*WorkBuddyAccount, error) {
	channelId = int(channelId)
	uid = strings.TrimSpace(uid)
	if channelId <= 0 || uid == "" {
		return nil, errors.New("workbuddy account is required")
	}
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		return nil, err
	}
	if channel == nil || channel.Type != constant.ChannelTypeWorkBuddy {
		return nil, errors.New("workbuddy channel not found")
	}
	lines := strings.Split(channel.Key, "\n")
	found := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		credential, parseErr := wbcore.ParseCredential(trimmed)
		if parseErr != nil || strings.TrimSpace(credential.UID) != uid {
			continue
		}
		credential.TasksEnabled = &enabled
		encoded, marshalErr := credential.Marshal()
		if marshalErr != nil {
			return nil, marshalErr
		}
		lines[index] = encoded
		found = true
		break
	}
	if !found {
		return nil, errors.New("workbuddy account not found")
	}
	updated, err := model.UpdateChannelKeyIfUnchanged(channelId, channel.Key, strings.Join(lines, "\n"))
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, model.ErrChannelKeyChanged
	}
	model.InitChannelCache()
	accounts, err := ListWorkBuddyAccounts([]int{channelId}, WorkBuddyRealmAll)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		if account.UID() == uid {
			return account, nil
		}
	}
	return nil, errors.New("workbuddy account disappeared after update")
}

// EnabledWorkBuddyActions returns the actions the settings currently allow.
func EnabledWorkBuddyActions(setting *operation_setting.WorkBuddyTaskSetting, cn bool) []string {
	if setting == nil || !setting.Enabled {
		return nil
	}
	actions := make([]string, 0, len(WorkBuddyAccountsActions))
	if cn {
		if setting.AutoCheckin {
			actions = append(actions, WorkBuddyTaskCheckin)
		}
		if setting.AutoActivityReport {
			actions = append(actions, WorkBuddyTaskActivityReport)
		}
		if setting.AutoTravel {
			actions = append(actions, WorkBuddyTaskTravel)
		}
		if setting.AutoStreakBonus {
			actions = append(actions, WorkBuddyTaskStreakBonus)
		}
		if setting.AutoGrowthTasks {
			actions = append(actions, WorkBuddyTaskGrowth)
		}
		if setting.AutoNightCat {
			actions = append(actions, WorkBuddyTaskNightCat)
		}
		return actions
	}
	if setting.AutoDailyChat {
		actions = append(actions, WorkBuddyTaskDailyChat)
	}
	return actions
}

// RunWorkBuddyTasks executes the selected actions for the selected accounts.
func RunWorkBuddyTasks(ctx context.Context, options WorkBuddyTaskOptions) (*WorkBuddyTaskSummary, error) {
	setting := operation_setting.GetWorkBuddyTaskSetting()
	if setting == nil || !setting.Enabled {
		return nil, errors.New("workbuddy tasks are disabled")
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	trigger := options.Trigger
	if trigger != WorkBuddyTriggerManual {
		trigger = WorkBuddyTriggerScheduler
	}
	summary := &WorkBuddyTaskSummary{Trigger: trigger, StartedAt: now.Unix()}

	accounts, err := ListWorkBuddyAccounts(options.ChannelIds, options.Realm)
	if err != nil {
		return nil, err
	}
	if uid := strings.TrimSpace(options.AccountUid); uid != "" {
		selected := make([]*WorkBuddyAccount, 0, 1)
		for _, account := range accounts {
			if account.UID() == uid {
				selected = append(selected, account)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("workbuddy account %s is not part of the selection", uid)
		}
		accounts = selected
	}
	requested := options.Actions
	date := now.Format("2006-01-02")

	for _, account := range accounts {
		actions := requested
		if len(actions) == 0 {
			actions = EnabledWorkBuddyActions(setting, account.IsCN())
		}
		if len(actions) == 0 {
			continue
		}
		if !account.TasksEnabled() && trigger == WorkBuddyTriggerScheduler && !options.IgnoreTaskParticipation {
			// The account switched automatic participation off. Reporting it keeps
			// a scheduled pass from looking like it covered an account it skipped.
			summary.Skipped = append(summary.Skipped, WorkBuddySkippedAccount{
				ChannelId:  account.ChannelId,
				AccountUid: account.UID(),
				Nickname:   account.Nickname(),
				Realm:      string(account.Realm()),
				Reason:     "automatic task participation is disabled for this account",
			})
			continue
		}
		summary.Accounts++
		budget := &workBuddyBudget{channelId: account.ChannelId, uid: account.UID(), date: date, limit: setting.RequestLimit(account.IsCN())}
		for _, action := range actions {
			if err := ctx.Err(); err != nil {
				summary.FinishedAt = time.Now().Unix()
				return summary, err
			}
			result := runWorkBuddyAction(ctx, account, action, trigger, budget, now)
			summary.Results = append(summary.Results, result)
			summary.Actions++
			summary.CreditDelta += result.Credit
			if setting.AccountGapSeconds > 0 {
				time.Sleep(time.Duration(setting.AccountGapSeconds) * time.Second)
			}
		}
	}
	summary.FinishedAt = time.Now().Unix()
	return summary, nil
}

// IsWorkBuddyAction reports whether the name is one the runner understands.
func IsWorkBuddyAction(action string) bool {
	for _, known := range append(append([]string{}, WorkBuddyAccountsActions...), WorkBuddyTaskNightCat) {
		if action == known {
			return true
		}
	}
	return false
}

// workBuddyBudget books upstream calls against one account's daily allowance.
type workBuddyBudget struct {
	channelId int
	uid       string
	date      string
	limit     int
	used      int
	exceeded  bool
}

func (b *workBuddyBudget) spend() bool {
	if b == nil {
		return true
	}
	if b.exceeded {
		return false
	}
	if b.limit <= 0 {
		b.used++
		return true
	}
	allowed, err := model.ConsumeWorkBuddyRequestQuota(b.channelId, b.uid, b.date, b.limit)
	if err != nil {
		logger.LogWarn(context.Background(), common.LogText("workbuddy: book a request for %s: %s", b.uid, err.Error()))
		return false
	}
	if !allowed {
		b.exceeded = true
		return false
	}
	b.used++
	return true
}

func (b *workBuddyBudget) exhausted() bool { return b != nil && b.exceeded }
