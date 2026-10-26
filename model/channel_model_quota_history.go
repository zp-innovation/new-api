package model

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// ChannelModelQuotaHistory is one observation of a (channel, model) quota
// window. Rows are only written when the reading actually changed, so a steady
// window does not fill the table with identical samples.
type ChannelModelQuotaHistory struct {
	Id                int64   `json:"id" gorm:"primaryKey"`
	ChannelId         int     `json:"channel_id" gorm:"index:idx_qh_pair,priority:1"`
	Model             string  `json:"model" gorm:"type:varchar(128);index:idx_qh_pair,priority:2;index:idx_qh_time,priority:2"`
	RemainingFraction float64 `json:"remaining_fraction" gorm:"not null"`
	ResetTime         int64   `json:"reset_time" gorm:"bigint"`
	FetchedAt         int64   `json:"fetched_at" gorm:"bigint;index:idx_qh_time,priority:1;index"`
}

// quotaHistoryMinDelta is the smallest remaining-fraction change worth
// recording. A 5-hour window sampled every 15 minutes moves in small steps, so
// this keeps roughly one row per real movement instead of per poll.
const quotaHistoryMinDelta = 0.005

// buildQuotaHistoryRows keeps only the readings that moved enough to matter.
func buildQuotaHistoryRows(rows []ChannelModelQuota, previous map[string]ChannelModelQuota) []ChannelModelQuotaHistory {
	history := make([]ChannelModelQuotaHistory, 0, len(rows))
	for _, row := range rows {
		old, seen := previous[channelModelQuotaKey(row.ChannelId, row.Model)]
		if seen {
			delta := old.RemainingFraction - row.RemainingFraction
			if delta < 0 {
				delta = -delta
			}
			if delta < quotaHistoryMinDelta && old.ResetTime == row.ResetTime {
				continue
			}
		}
		history = append(history, ChannelModelQuotaHistory{
			ChannelId:         row.ChannelId,
			Model:             row.Model,
			RemainingFraction: row.RemainingFraction,
			ResetTime:         row.ResetTime,
			FetchedAt:         row.FetchedAt,
		})
	}
	return history
}

// InsertChannelModelQuotaHistory appends observations in one batch.
func InsertChannelModelQuotaHistory(rows []ChannelModelQuotaHistory) error {
	if len(rows) == 0 {
		return nil
	}
	return DB.CreateInBatches(rows, 200).Error
}

// PruneChannelModelQuotaHistory drops observations older than the cutoff.
func PruneChannelModelQuotaHistory(before time.Time) (int64, error) {
	result := DB.Where("fetched_at < ?", before.Unix()).Delete(&ChannelModelQuotaHistory{})
	return result.RowsAffected, result.Error
}

// channelModelQuotaHistoryRowLimit bounds one trend query. The poll loop that
// writes these rows covers every model on the same cadence, so dropping the
// oldest rows shortens the window for every model instead of removing a whole
// model from the trend.
const channelModelQuotaHistoryRowLimit = 50000

// ListChannelModelQuotaHistory returns the newest observations since the given
// instant, oldest first. A non-positive limit uses the row limit above.
func ListChannelModelQuotaHistory(modelName string, since time.Time, limit int) ([]ChannelModelQuotaHistory, error) {
	if limit <= 0 {
		limit = channelModelQuotaHistoryRowLimit
	}
	var rows []ChannelModelQuotaHistory
	query := DB.Where("fetched_at >= ?", since.Unix())
	if trimmed := strings.TrimSpace(modelName); trimmed != "" {
		query = query.Where("model = ?", trimmed)
	}
	if err := query.Order("fetched_at desc, id desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	slices.Reverse(rows)
	return rows, nil
}

// QuotaHistoryPoint is one 30-minute sample of a model's pool.
type QuotaHistoryPoint struct {
	At        int64   `json:"at"`
	Available int     `json:"available"`
	Average   float64 `json:"average"`
}

// QuotaHistorySeries is the trend of one model over the requested window.
type QuotaHistorySeries struct {
	Model  string              `json:"model"`
	Points []QuotaHistoryPoint `json:"points"`
}

// BuildQuotaHistorySeries buckets raw observations into 30-minute samples and
// counts how many accounts still had quota at each bucket.
func BuildQuotaHistorySeries(rows []ChannelModelQuotaHistory, bucket time.Duration) []QuotaHistorySeries {
	if bucket <= 0 {
		bucket = 30 * time.Minute
	}
	type acc struct {
		available int
		total     int
		sum       float64
	}
	buckets := make(map[string]map[int64]*acc)
	for _, row := range rows {
		key := int64(bucket.Seconds())
		slot := row.FetchedAt / key * key
		if buckets[row.Model] == nil {
			buckets[row.Model] = make(map[int64]*acc)
		}
		entry := buckets[row.Model][slot]
		if entry == nil {
			entry = &acc{}
			buckets[row.Model][slot] = entry
		}
		entry.total++
		entry.sum += row.RemainingFraction
		if row.RemainingFraction > ChannelModelQuotaExhaustedBelow {
			entry.available++
		}
	}

	models := make([]string, 0, len(buckets))
	for modelName := range buckets {
		models = append(models, modelName)
	}
	sort.Strings(models)

	series := make([]QuotaHistorySeries, 0, len(models))
	for _, modelName := range models {
		points := make([]QuotaHistoryPoint, 0, len(buckets[modelName]))
		slots := make([]int64, 0, len(buckets[modelName]))
		for slot := range buckets[modelName] {
			slots = append(slots, slot)
		}
		sort.Slice(slots, func(i, j int) bool { return slots[i] < slots[j] })
		for _, slot := range slots {
			entry := buckets[modelName][slot]
			average := 0.0
			if entry.total > 0 {
				average = entry.sum / float64(entry.total)
			}
			points = append(points, QuotaHistoryPoint{At: slot, Available: entry.available, Average: average})
		}
		series = append(series, QuotaHistorySeries{Model: modelName, Points: points})
	}
	return series
}
