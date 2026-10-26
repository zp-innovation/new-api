package model

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WorkBuddyAccountState is one WorkBuddy account's task state. A channel key can
// carry several accounts, so the row is keyed by the channel and the
// credential's uid. Rows are created lazily: a channel that was just imported
// shows up in the panel before anything has run, and the first query or task
// fills the row in.
type WorkBuddyAccountState struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId  int    `json:"channel_id" gorm:"not null;uniqueIndex:idx_wb_account_state"`
	AccountUid string `json:"account_uid" gorm:"type:varchar(128);not null;uniqueIndex:idx_wb_account_state"`
	Realm      string `json:"realm" gorm:"type:varchar(16);not null"`
	Product    string `json:"product" gorm:"type:varchar(32);not null"`
	Nickname   string `json:"nickname" gorm:"type:varchar(255);not null"`

	// The four daily gates. Each is the local timestamp of the last successful
	// attempt, which is what keeps a second run on the same day a no-op.
	LastCheckinAt        int64 `json:"last_checkin_at" gorm:"bigint;not null"`
	LastDailyChatAt      int64 `json:"last_daily_chat_at" gorm:"bigint;not null"`
	LastActivityReportAt int64 `json:"last_activity_report_at" gorm:"bigint;not null"`

	LastRunAt   int64  `json:"last_run_at" gorm:"bigint;not null"`
	LastTask    string `json:"last_task" gorm:"type:varchar(64);not null"`
	LastStatus  string `json:"last_status" gorm:"type:varchar(16);not null"`
	LastMessage string `json:"last_message" gorm:"type:varchar(512);not null"`

	StreakDays   int     `json:"streak_days" gorm:"not null"`
	DailyCredit  float64 `json:"daily_credit" gorm:"not null"`
	TodayCredit  float64 `json:"today_credit" gorm:"not null"`
	RemainCredit float64 `json:"remain_credit" gorm:"not null"`

	// RequestsDate/RequestsUsed bound one account's upstream calls per local
	// day, so a runaway task cannot hammer the upstream.
	RequestsDate string `json:"requests_date" gorm:"type:varchar(10);not null"`
	RequestsUsed int    `json:"requests_used" gorm:"not null"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (WorkBuddyAccountState) TableName() string {
	return "workbuddy_account_states"
}

// GetWorkBuddyAccountState reads one account's state; a missing row is not an
// error because the state is created on demand.
func GetWorkBuddyAccountState(channelId int, uid string) (*WorkBuddyAccountState, error) {
	var state WorkBuddyAccountState
	err := DB.Where("channel_id = ? AND account_uid = ?", channelId, uid).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// SaveWorkBuddyAccountState upserts one account row. The unique key makes this
// safe to call from several instances at once.
func SaveWorkBuddyAccountState(state *WorkBuddyAccountState) error {
	if state == nil {
		return errors.New("workbuddy account state is nil")
	}
	now := time.Now().Unix()
	if state.CreatedAt == 0 {
		state.CreatedAt = now
	}
	state.UpdatedAt = now
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}, {Name: "account_uid"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"realm", "product", "nickname",
			"last_checkin_at", "last_daily_chat_at", "last_activity_report_at",
			"last_run_at", "last_task", "last_status", "last_message",
			"streak_days", "daily_credit", "today_credit", "remain_credit",
			"requests_date", "requests_used", "updated_at",
		}),
	}).Create(state).Error
}

// ListWorkBuddyAccountStates returns the stored states for the given channels.
func ListWorkBuddyAccountStates(channelIds []int) ([]WorkBuddyAccountState, error) {
	var states []WorkBuddyAccountState
	if len(channelIds) == 0 {
		return states, nil
	}
	err := DB.Where("channel_id IN ?", channelIds).Find(&states).Error
	return states, err
}

// ConsumeWorkBuddyRequestQuota books one upstream call against the account's
// daily budget. It returns false when the budget is already spent. The counter
// rolls over when the local date changes, and the guarded UPDATE keeps the check
// and the increment atomic without a dialect-specific lock.
func ConsumeWorkBuddyRequestQuota(channelId int, uid, date string, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	// A brand-new account has no row yet and the guarded increment below only
	// matches existing rows, so the key is created first.
	if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&WorkBuddyAccountState{
		ChannelId:  channelId,
		AccountUid: uid,
	}).Error; err != nil {
		return false, err
	}
	err := DB.Model(&WorkBuddyAccountState{}).
		Where("channel_id = ? AND account_uid = ? AND requests_date <> ?", channelId, uid, date).
		Updates(map[string]any{"requests_date": date, "requests_used": 0}).Error
	if err != nil {
		return false, err
	}
	result := DB.Model(&WorkBuddyAccountState{}).
		Where("channel_id = ? AND account_uid = ? AND requests_date = ? AND requests_used < ?", channelId, uid, date, limit).
		UpdateColumn("requests_used", gorm.Expr("requests_used + 1"))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// WorkBuddyTaskRun is one recorded task attempt, kept for the panel history and
// for auditing what ran against an account.
type WorkBuddyTaskRun struct {
	Id          int     `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId   int     `json:"channel_id" gorm:"not null;index:idx_wb_task_run_channel"`
	AccountUid  string  `json:"account_uid" gorm:"type:varchar(128);not null;index:idx_wb_task_run_channel"`
	Task        string  `json:"task" gorm:"type:varchar(64);not null"`
	Trigger     string  `json:"trigger" gorm:"type:varchar(16);not null"`
	OK          bool    `json:"ok" gorm:"not null"`
	Message     string  `json:"message" gorm:"type:varchar(512);not null"`
	CreditDelta float64 `json:"credit_delta" gorm:"not null"`
	CreatedAt   int64   `json:"created_at" gorm:"bigint;not null;index:idx_wb_task_run_created"`
}

func (WorkBuddyTaskRun) TableName() string {
	return "workbuddy_task_runs"
}

// RecordWorkBuddyTaskRun stores one attempt. The message is redacted and
// truncated first: an upstream error body can echo an authorization header.
func RecordWorkBuddyTaskRun(run *WorkBuddyTaskRun) error {
	if run == nil {
		return errors.New("workbuddy task run is nil")
	}
	run.Message = sanitizeWorkBuddyMessage(run.Message)
	if run.CreatedAt == 0 {
		run.CreatedAt = time.Now().Unix()
	}
	return DB.Create(run).Error
}

// WorkBuddyTaskRunFilter narrows the history query.
type WorkBuddyTaskRunFilter struct {
	ChannelId  int
	AccountUid string
	Task       string
	OK         *bool
	Offset     int
	Limit      int
}

// ListWorkBuddyTaskRuns returns the newest runs first plus the total count.
func ListWorkBuddyTaskRuns(filter WorkBuddyTaskRunFilter) ([]WorkBuddyTaskRun, int64, error) {
	query := DB.Model(&WorkBuddyTaskRun{})
	if filter.ChannelId > 0 {
		query = query.Where("channel_id = ?", filter.ChannelId)
	}
	if filter.AccountUid != "" {
		query = query.Where("account_uid = ?", filter.AccountUid)
	}
	if filter.Task != "" {
		query = query.Where("task = ?", filter.Task)
	}
	if filter.OK != nil {
		query = query.Where("ok = ?", *filter.OK)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	var runs []WorkBuddyTaskRun
	err := query.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&runs).Error
	return runs, total, err
}

// DeleteWorkBuddyTaskRunsBefore trims the history; the retention window is the
// caller's.
func DeleteWorkBuddyTaskRunsBefore(cutoff int64) (int64, error) {
	result := DB.Where("created_at < ?", cutoff).Delete(&WorkBuddyTaskRun{})
	return result.RowsAffected, result.Error
}

// The upstream error text is free-form and 401/403 bodies routinely echo the
// authorization header, so anything stored has to be truncated and stripped of
// the known credential shapes first.
var workBuddySecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\b(?:access[_-]?token|refresh[_-]?token|api[_-]?key|apikey|authorization|cookie|set-cookie|password|secret|token)\b\s*[:=]\s*\S+`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{4,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`),
	regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`),
	regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}={0,2}\b`),
}

const workBuddyMessageLimit = 200

func sanitizeWorkBuddyMessage(text string) string {
	line := strings.Join(strings.Fields(text), " ")
	for _, pattern := range workBuddySecretPatterns {
		line = pattern.ReplaceAllString(line, "[redacted]")
	}
	if runes := []rune(line); len(runes) > workBuddyMessageLimit {
		line = string(runes[:workBuddyMessageLimit])
	}
	return line
}
