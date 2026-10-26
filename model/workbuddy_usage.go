package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WorkBuddyUsageDaily is one account-model pair's consumption for one local
// calendar day, folded out of the consume logs. The upstream quota window is a
// rolling 24 hours anchored to its own reset clock, which the rolling query
// answers directly; this table exists so the console keeps a trend after the
// logs themselves are trimmed.
type WorkBuddyUsageDaily struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId  int    `json:"channel_id" gorm:"not null;uniqueIndex:idx_wb_usage_daily"`
	AccountUid string `json:"account_uid" gorm:"type:varchar(128);not null;uniqueIndex:idx_wb_usage_daily"`
	Model      string `json:"model" gorm:"type:varchar(128);not null;uniqueIndex:idx_wb_usage_daily"`
	Day        string `json:"day" gorm:"type:varchar(10);not null;uniqueIndex:idx_wb_usage_daily"`
	Realm      string `json:"realm" gorm:"type:varchar(16);not null"`

	Requests         int64 `json:"requests" gorm:"not null"`
	PromptTokens     int64 `json:"prompt_tokens" gorm:"not null"`
	CompletionTokens int64 `json:"completion_tokens" gorm:"not null"`
	TotalTokens      int64 `json:"total_tokens" gorm:"not null"`

	UpdatedAt int64 `json:"updated_at" gorm:"bigint;not null"`
}

func (WorkBuddyUsageDaily) TableName() string {
	return "workbuddy_usage_daily"
}

// UpsertWorkBuddyUsageDaily writes one day's counters. The caller passes the
// absolute totals for the day (not a delta), so a retried sync overwrites the
// row with the same numbers instead of double counting.
func UpsertWorkBuddyUsageDaily(row *WorkBuddyUsageDaily) error {
	return upsertWorkBuddyUsageDaily(DB, row)
}

// ReplaceWorkBuddyUsageDaily rewrites one day for a channel set in a single
// transaction. The rollup recomputes a day from scratch, and a reader must
// never observe the moment between the delete and the write-back.
func ReplaceWorkBuddyUsageDaily(channelIds []int, day string, rows []WorkBuddyUsageDaily) error {
	if len(channelIds) == 0 || day == "" {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := deleteWorkBuddyUsageDaily(tx, channelIds, day); err != nil {
			return err
		}
		for i := range rows {
			if err := upsertWorkBuddyUsageDaily(tx, &rows[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func upsertWorkBuddyUsageDaily(tx *gorm.DB, row *WorkBuddyUsageDaily) error {
	if row == nil {
		return errors.New("workbuddy usage row is nil")
	}
	row.UpdatedAt = time.Now().Unix()
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "channel_id"}, {Name: "account_uid"}, {Name: "model"}, {Name: "day"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"realm", "requests", "prompt_tokens", "completion_tokens",
			"total_tokens", "updated_at",
		}),
	}).Create(row).Error
}

// GetWorkBuddyUsageDaily reads one stored day, so a caller can decide whether an
// upsert would be an improvement. A missing row is not an error.
func GetWorkBuddyUsageDaily(channelId int, uid, model, day string) (*WorkBuddyUsageDaily, error) {
	var row WorkBuddyUsageDaily
	err := DB.Where("channel_id = ? AND account_uid = ? AND model = ? AND day = ?",
		channelId, uid, model, day).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteWorkBuddyUsageDaily deletes one stored day for a channel set. The
// rollup rewrites a day from scratch, so a day whose usage disappeared (a log
// trim, or a correction) must not leave a stale row behind.
func DeleteWorkBuddyUsageDaily(channelIds []int, day string) error {
	return deleteWorkBuddyUsageDaily(DB, channelIds, day)
}

func deleteWorkBuddyUsageDaily(tx *gorm.DB, channelIds []int, day string) error {
	if len(channelIds) == 0 || day == "" {
		return nil
	}
	return tx.Where("channel_id IN ? AND day = ?", channelIds, day).
		Delete(&WorkBuddyUsageDaily{}).Error
}

// ListWorkBuddyUsageDaily returns every stored day for the given channels,
// newest first, so the console can draw a short trend without touching the log
// database.
func ListWorkBuddyUsageDaily(channelIds []int, sinceDay string) ([]WorkBuddyUsageDaily, error) {
	rows := make([]WorkBuddyUsageDaily, 0, len(channelIds))
	if len(channelIds) == 0 {
		return rows, nil
	}
	query := DB.Where("channel_id IN ?", channelIds)
	if sinceDay != "" {
		query = query.Where("day >= ?", sinceDay)
	}
	err := query.Order("day DESC, channel_id ASC, model ASC").Find(&rows).Error
	return rows, err
}

// WorkBuddyLimitEvent records one upstream quota cap: the moment the window was
// found spent, the reset wall clock the upstream named, and the usage that had
// accumulated in that window. The samples are the only way the budget is ever
// learned, because the upstream never publishes it.
type WorkBuddyLimitEvent struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId  int    `json:"channel_id" gorm:"not null;uniqueIndex:idx_wb_limit_event"`
	AccountUid string `json:"account_uid" gorm:"type:varchar(128);not null;uniqueIndex:idx_wb_limit_event"`
	Model      string `json:"model" gorm:"type:varchar(128);not null;uniqueIndex:idx_wb_limit_event"`
	Realm      string `json:"realm" gorm:"type:varchar(16);not null"`

	// ResetAt is the unique part: one event per (account, model, window), so a
	// retry storm against a spent window cannot inflate the sample set.
	ResetAt int64 `json:"reset_at" gorm:"bigint;not null;uniqueIndex:idx_wb_limit_event"`

	CappedAt     int64  `json:"capped_at" gorm:"bigint;not null"`
	SampleTokens int64  `json:"sample_tokens" gorm:"not null"`
	Source       string `json:"source" gorm:"type:varchar(16);not null"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;not null"`
}

func (WorkBuddyLimitEvent) TableName() string {
	return "workbuddy_limit_events"
}

// RecordWorkBuddyLimitEvent stores one cap. The unique key makes the write
// idempotent, which matters because a client retry re-sends the same rejection.
func RecordWorkBuddyLimitEvent(event *WorkBuddyLimitEvent) error {
	if event == nil {
		return errors.New("workbuddy limit event is nil")
	}
	if DB == nil {
		return nil
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = time.Now().Unix()
	}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(event).Error
}

// ListWorkBuddyLimitEvents returns the cap history for the given channels since
// a cutoff, newest first. A quiet window is what the caller wants here, so the
// cutoff is a plain lower bound on the reset clock.
func ListWorkBuddyLimitEvents(channelIds []int, sinceUnix int64) ([]WorkBuddyLimitEvent, error) {
	events := make([]WorkBuddyLimitEvent, 0, len(channelIds))
	if len(channelIds) == 0 {
		return events, nil
	}
	query := DB.Where("channel_id IN ?", channelIds)
	if sinceUnix > 0 {
		query = query.Where("reset_at >= ?", sinceUnix)
	}
	err := query.Order("capped_at DESC, id DESC").Find(&events).Error
	return events, err
}

// DeleteWorkBuddyLimitEventsBefore trims the cap history. Samples older than the
// estimation window cannot influence a budget again.
func DeleteWorkBuddyLimitEventsBefore(cutoff int64) (int64, error) {
	result := DB.Where("capped_at < ?", cutoff).Delete(&WorkBuddyLimitEvent{})
	return result.RowsAffected, result.Error
}
