package model

import (
	"time"

	"gorm.io/gorm/clause"
)

// ChannelAccountQuota is the account-level quota of a channel whose upstream
// does not report per-model limits (WorkBuddy, OpenCode, Command Code). Model
// level data stays in channel_model_quota; this table answers "how much is left
// on this account" even when the upstream has no model dimension.
type ChannelAccountQuota struct {
	Id        int     `json:"id" gorm:"primaryKey"`
	ChannelId int     `json:"channel_id" gorm:"not null;uniqueIndex"`
	Remain    float64 `json:"remain" gorm:"not null;default:0"`
	Total     float64 `json:"total" gorm:"not null;default:0"`
	// ResetTime is the absolute unix second the quota window rolls over; zero
	// means the upstream did not report one.
	ResetTime int64  `json:"reset_time" gorm:"bigint;index"`
	Status    string `json:"status" gorm:"type:varchar(16);not null"`
	Error     string `json:"error" gorm:"type:text"`
	FetchedAt int64  `json:"fetched_at" gorm:"bigint;index"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
}

const (
	ChannelAccountQuotaStatusOK    = "ok"
	ChannelAccountQuotaStatusError = "error"
)

// UpsertChannelAccountQuotas writes one poll batch.
func UpsertChannelAccountQuotas(rows []ChannelAccountQuota) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().Unix()
	for i := range rows {
		rows[i].UpdatedAt = now
		if rows[i].Status == "" {
			rows[i].Status = ChannelAccountQuotaStatusOK
		}
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"remain", "total", "reset_time", "status", "error", "fetched_at", "updated_at",
		}),
	}).CreateInBatches(rows, 100).Error
}

// ListChannelAccountQuotas returns every stored account snapshot.
func ListChannelAccountQuotas() ([]ChannelAccountQuota, error) {
	var rows []ChannelAccountQuota
	err := DB.Order("channel_id asc").Find(&rows).Error
	return rows, err
}

// ChannelAccountQuotaView adds the channel's identity to a snapshot row so the
// console can show a name instead of a bare id and filter by type.
type ChannelAccountQuotaView struct {
	ChannelAccountQuota
	ChannelName string `json:"channel_name"`
	ChannelType int    `json:"channel_type"`
}

// ListChannelAccountQuotaViews joins the snapshot with channel identity. A
// channelType of 0 returns every type.
func ListChannelAccountQuotaViews(channelType int) ([]ChannelAccountQuotaView, error) {
	var rows []ChannelAccountQuotaView
	query := DB.Table("channel_account_quota AS q").
		Select("q.*, c.name AS channel_name, c.type AS channel_type").
		Joins("JOIN channels AS c ON c.id = q.channel_id")
	if channelType > 0 {
		query = query.Where("c.type = ?", channelType)
	}
	err := query.Order("c.type asc, c.id asc").Scan(&rows).Error
	return rows, err
}
