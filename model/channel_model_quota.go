package model

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Quota snapshot status values. "ok" carries a usable remaining fraction;
// "error" records that the last poll failed for this account.
const (
	ChannelModelQuotaStatusOK    = "ok"
	ChannelModelQuotaStatusError = "error"
)

const (
	// ChannelModelQuotaExhaustedBelow is the remaining fraction at or below
	// which an account is treated as having no usable quota for that model.
	// Antigravity reports a drained account with a tiny non-zero fraction
	// (observed 0.0001–0.009) and then answers 429, so an exact zero test would
	// keep routing traffic into an account that cannot serve it.
	ChannelModelQuotaExhaustedBelow = 0.01
	// ChannelModelQuotaMaxAge bounds how old a snapshot may be before routing
	// ignores it. A stalled poller must never park an account on its own.
	ChannelModelQuotaMaxAge = 45 * time.Minute
)

// ChannelModelQuota is the most recent quota reading for one (channel, model)
// pair, as reported by the upstream model catalogue. It backs both the console
// board and the router's "does this account still have quota for this model"
// check.
type ChannelModelQuota struct {
	Id                int     `json:"id" gorm:"primaryKey"`
	ChannelId         int     `json:"channel_id" gorm:"not null;uniqueIndex:idx_channel_model_quota"`
	Model             string  `json:"model" gorm:"type:varchar(128);not null;uniqueIndex:idx_channel_model_quota"`
	RemainingFraction float64 `json:"remaining_fraction" gorm:"not null"`
	// PreviousRemainingFraction / PreviousFetchedAt hold the reading before the
	// current one, so the console can derive a burn rate without a history
	// table. Both are zero until a second poll has happened.
	PreviousRemainingFraction float64 `json:"previous_remaining_fraction" gorm:"not null;default:0"`
	PreviousFetchedAt         int64   `json:"previous_fetched_at" gorm:"bigint;not null;default:0"`
	// ResetTime is the absolute unix second when the current window rolls over.
	// Zero means the upstream did not report one.
	ResetTime int64  `json:"reset_time" gorm:"bigint;index"`
	Status    string `json:"status" gorm:"type:varchar(16);not null"`
	Error     string `json:"error" gorm:"type:text"`
	FetchedAt int64  `json:"fetched_at" gorm:"bigint;index"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint;index"`
}

// channelModelQuotaCache mirrors the table in memory so the hot routing path
// never touches the database.
var (
	channelModelQuotaMu    sync.RWMutex
	channelModelQuotaCache = map[string]ChannelModelQuota{}
	// quotaCacheRefreshedAt rate-limits the on-demand reload below. Only the
	// node that runs the poller updates its own mirror, so every other node
	// would otherwise serve whatever it read at start-up forever.
	quotaCacheRefreshedAt atomic.Int64
)

// refreshQuotaCacheIfStale reloads the mirror from the table when it has not
// been reloaded recently. It is called from the read path only after a lookup
// missed or went stale, so a healthy single-node deployment never pays for it.
func refreshQuotaCacheIfStale(now time.Time) {
	last := quotaCacheRefreshedAt.Load()
	if last != 0 && now.Sub(time.Unix(last, 0)) < time.Minute {
		return
	}
	if !quotaCacheRefreshedAt.CompareAndSwap(last, now.Unix()) {
		return
	}
	_ = LoadChannelModelQuotaCache()
}

func channelModelQuotaKey(channelID int, model string) string {
	return strconv.Itoa(channelID) + "|" + strings.ToLower(strings.TrimSpace(model))
}

// UpsertChannelModelQuotas writes one poll batch and refreshes the in-memory
// mirror. An empty slice is a no-op.
func UpsertChannelModelQuotas(rows []ChannelModelQuota) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().Unix()
	for i := range rows {
		rows[i].Model = strings.TrimSpace(rows[i].Model)
		rows[i].Status = strings.TrimSpace(rows[i].Status)
		if rows[i].Status == "" {
			rows[i].Status = ChannelModelQuotaStatusOK
		}
		rows[i].UpdatedAt = now
	}

	// Carry the current reading into the "previous" slot before overwriting it,
	// so the board can show how fast a window is draining.
	channelIDs := make([]int, 0, len(rows))
	seen := make(map[int]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ChannelId]; ok {
			continue
		}
		seen[row.ChannelId] = struct{}{}
		channelIDs = append(channelIDs, row.ChannelId)
	}
	previous := make(map[string]ChannelModelQuota, len(rows))
	var existing []ChannelModelQuota
	if err := DB.Where("channel_id IN ?", channelIDs).Find(&existing).Error; err != nil {
		return fmt.Errorf("load previous channel model quotas: %w", err)
	}
	for _, row := range existing {
		previous[channelModelQuotaKey(row.ChannelId, row.Model)] = row
	}
	for i := range rows {
		if old, ok := previous[channelModelQuotaKey(rows[i].ChannelId, rows[i].Model)]; ok && old.FetchedAt > 0 {
			rows[i].PreviousRemainingFraction = old.RemainingFraction
			rows[i].PreviousFetchedAt = old.FetchedAt
		}
	}

	// GORM has no portable bulk upsert across SQLite, MySQL and PostgreSQL, so
	// the conflict clause names the unique index columns and updates only the
	// mutable measurement columns.
	err := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}, {Name: "model"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"remaining_fraction", "previous_remaining_fraction", "previous_fetched_at",
			"reset_time", "status", "error", "fetched_at", "updated_at",
		}),
	}).CreateInBatches(rows, 100).Error
	if err != nil {
		return err
	}

	channelModelQuotaMu.Lock()
	for _, row := range rows {
		channelModelQuotaCache[channelModelQuotaKey(row.ChannelId, row.Model)] = row
	}
	channelModelQuotaMu.Unlock()

	// Append the 24-hour trend samples. Only readings that actually moved are
	// stored, so a steady window costs nothing.
	return InsertChannelModelQuotaHistory(buildQuotaHistoryRows(rows, previous))
}

// LoadChannelModelQuotaCache fills the in-memory mirror from the table. It runs
// at start-up and after a repair, so a restart does not lose routing state.
func LoadChannelModelQuotaCache() error {
	var rows []ChannelModelQuota
	if err := DB.Find(&rows).Error; err != nil {
		return err
	}
	next := make(map[string]ChannelModelQuota, len(rows))
	for _, row := range rows {
		next[channelModelQuotaKey(row.ChannelId, row.Model)] = row
	}
	channelModelQuotaMu.Lock()
	channelModelQuotaCache = next
	channelModelQuotaMu.Unlock()
	return nil
}

// DeleteChannelModelQuotas drops every snapshot of a channel; call it when the
// channel is deleted so a recycled id cannot inherit stale quota.
func DeleteChannelModelQuotas(channelID int) error {
	if channelID <= 0 {
		return nil
	}
	if err := DB.Where("channel_id = ?", channelID).Delete(&ChannelModelQuota{}).Error; err != nil {
		return err
	}
	channelModelQuotaMu.Lock()
	for key, row := range channelModelQuotaCache {
		if row.ChannelId == channelID {
			delete(channelModelQuotaCache, key)
		}
	}
	channelModelQuotaMu.Unlock()
	return nil
}

// ListChannelModelQuotas returns every stored snapshot, newest reading first
// within a channel.
func ListChannelModelQuotas() ([]ChannelModelQuota, error) {
	var rows []ChannelModelQuota
	err := DB.Order("channel_id asc, model asc").Find(&rows).Error
	return rows, err
}

// ListChannelModelQuotasByChannel reads one channel's snapshot from the
// database rather than the in-memory mirror.
func ListChannelModelQuotasByChannel(channelID int) ([]ChannelModelQuota, error) {
	if channelID <= 0 {
		return nil, errors.New("channel id is required")
	}
	var rows []ChannelModelQuota
	err := DB.Where("channel_id = ?", channelID).Order("model asc").Find(&rows).Error
	return rows, err
}

// ChannelModelQuotaExhausted reports whether a fresh snapshot says this account
// has no usable quota for this model, and when the window next resets.
//
// It fails open in every ambiguous case: no snapshot, an errored poll, a stale
// reading, or an unknown reset time all return false so a broken or lagging
// poller can never park a healthy account.
func ChannelModelQuotaExhausted(channelID int, model string, now time.Time) (bool, time.Time) {
	model = strings.TrimSpace(model)
	if channelID <= 0 || model == "" {
		return false, time.Time{}
	}
	lookup := func() (ChannelModelQuota, bool) {
		channelModelQuotaMu.RLock()
		row, ok := channelModelQuotaCache[channelModelQuotaKey(channelID, model)]
		channelModelQuotaMu.RUnlock()
		return row, ok
	}
	row, ok := lookup()
	if !ok || row.FetchedAt <= 0 || now.Sub(time.Unix(row.FetchedAt, 0)) > ChannelModelQuotaMaxAge {
		// Another node runs the poller; reload before giving up so this node
		// converges within a minute instead of failing open forever.
		refreshQuotaCacheIfStale(now)
		row, ok = lookup()
	}
	if !ok {
		return false, time.Time{}
	}
	if row.Status != ChannelModelQuotaStatusOK {
		return false, time.Time{}
	}
	if row.FetchedAt <= 0 || now.Sub(time.Unix(row.FetchedAt, 0)) > ChannelModelQuotaMaxAge {
		return false, time.Time{}
	}
	if row.RemainingFraction > ChannelModelQuotaExhaustedBelow {
		return false, time.Time{}
	}
	if row.ResetTime <= 0 {
		// Exhausted with no reset clock: do not park forever.
		return false, time.Time{}
	}
	reset := time.Unix(row.ResetTime, 0)
	if !now.Before(reset) {
		// The window already rolled over; the reading is simply stale.
		return false, time.Time{}
	}
	return true, reset
}

// ChannelModelQuotaSummary is the console-facing roll-up of the snapshot table:
// every monitored model with the accounts that can still serve it and when the
// drained ones come back.
type ChannelModelQuotaSummary struct {
	Model             string `json:"model"`
	TotalAccounts     int    `json:"total_accounts"`
	AvailableAccounts int    `json:"available_accounts"`
	EarliestReset     int64  `json:"earliest_reset"`
	// InUse reports whether at least one channel actually routes this model.
	// The board defaults to in-use models and hides the rest of the upstream
	// catalogue behind an explicit toggle.
	InUse    bool                          `json:"in_use"`
	Accounts []ChannelModelQuotaAccountRow `json:"accounts"`
}

// ChannelModelQuotaAccountRow is one account's line in the board.
type ChannelModelQuotaAccountRow struct {
	ChannelId         int     `json:"channel_id"`
	ChannelName       string  `json:"channel_name"`
	RemainingFraction float64 `json:"remaining_fraction"`
	ResetTime         int64   `json:"reset_time"`
	Status            string  `json:"status"`
	FetchedAt         int64   `json:"fetched_at"`
	Exhausted         bool    `json:"exhausted"`
	// RemainingChangePerHour is (previous - current) / hours. Positive means the
	// window is draining; nil means there is no earlier reading to compare with.
	RemainingChangePerHour *float64 `json:"remaining_change_per_hour,omitempty"`
	// ExhaustedAt is the projected moment this account runs dry at the current
	// burn rate. Zero means the rate is unknown or not draining.
	ExhaustedAt int64 `json:"exhausted_at,omitempty"`
}

// ResolveUpstreamModelName follows this channel's model_mapping to the id the
// request is actually forwarded as. It returns name unchanged when the channel
// has no mapping, the mapping has no entry for this model, or the mapping
// cycles.
func (channel *Channel) ResolveUpstreamModelName(name string) string {
	name = strings.TrimSpace(name)
	if channel == nil || name == "" {
		return name
	}
	raw := strings.TrimSpace(channel.GetModelMapping())
	if raw == "" || raw == "{}" {
		return name
	}
	mapping := map[string]string{}
	if err := common.Unmarshal([]byte(raw), &mapping); err != nil || len(mapping) == 0 {
		return name
	}
	upstream, cyclic := followChannelModelMapping(mapping, name)
	if cyclic || strings.TrimSpace(upstream) == "" {
		return name
	}
	return upstream
}

// ModelsServedByType returns the deduplicated set of models actually configured
// on channels of one type.
//
// The upstream catalogue advertises far more models than an operator routes to
// (tx101 reports 31 for 3 configured models). The console defaults to this set
// so the board answers "how is my traffic doing", not "what does Google offer".
func ModelsServedByType(channelType int) (map[string]struct{}, error) {
	var rows []struct {
		Models       string
		ModelMapping *string
	}
	if err := DB.Model(&Channel{}).
		Select("models", "model_mapping").
		Where("type = ? AND status = ?", channelType, 1).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	served := make(map[string]struct{})
	for _, row := range rows {
		channel := &Channel{ModelMapping: row.ModelMapping}
		for _, name := range strings.Split(row.Models, ",") {
			if name = strings.TrimSpace(name); name == "" {
				continue
			}
			served[name] = struct{}{}
			// A channel may route an alias to a different upstream id; the
			// snapshot is keyed by the upstream id, so both must count as
			// "in use" or the board would hide a model that really serves.
			if upstream := channel.ResolveUpstreamModelName(name); upstream != name {
				served[upstream] = struct{}{}
			}
		}
	}
	return served, nil
}

// ChannelNamesByType returns id → name for every channel of one type. The quota
// board needs display names for the accounts it lists.
func ChannelNamesByType(channelType int) (map[int]string, error) {
	var rows []struct {
		Id   int
		Name string
	}
	if err := DB.Model(&Channel{}).Select("id", "name").Where("type = ?", channelType).Find(&rows).Error; err != nil {
		return nil, err
	}
	names := make(map[int]string, len(rows))
	for _, row := range rows {
		names[row.Id] = row.Name
	}
	return names, nil
}

// BuildChannelModelQuotaSummary joins the quota table with channel names and
// groups it per model for the console board.
func BuildChannelModelQuotaSummary(now time.Time, channelNames map[int]string) []ChannelModelQuotaSummary {
	channelModelQuotaMu.RLock()
	rows := make([]ChannelModelQuota, 0, len(channelModelQuotaCache))
	for _, row := range channelModelQuotaCache {
		rows = append(rows, row)
	}
	channelModelQuotaMu.RUnlock()

	byModel := make(map[string][]ChannelModelQuotaAccountRow)
	for _, row := range rows {
		exhausted, reset := ChannelModelQuotaExhausted(row.ChannelId, row.Model, now)
		if !exhausted && row.Status == ChannelModelQuotaStatusOK &&
			row.RemainingFraction <= ChannelModelQuotaExhaustedBelow && row.ResetTime > 0 &&
			now.Unix() < row.ResetTime {
			// Not parked (stale or unknown reset) but still visibly drained.
			exhausted = true
			reset = time.Unix(row.ResetTime, 0)
		}
		accountRow := ChannelModelQuotaAccountRow{
			ChannelId:         row.ChannelId,
			ChannelName:       channelNames[row.ChannelId],
			RemainingFraction: row.RemainingFraction,
			ResetTime:         row.ResetTime,
			Status:            row.Status,
			FetchedAt:         row.FetchedAt,
			Exhausted:         exhausted || reset.After(now),
		}
		if row.PreviousFetchedAt > 0 && row.FetchedAt > row.PreviousFetchedAt {
			hours := float64(row.FetchedAt-row.PreviousFetchedAt) / 3600
			if hours > 0 {
				change := (row.PreviousRemainingFraction - row.RemainingFraction) / hours
				accountRow.RemainingChangePerHour = &change
				// Only project while the window is draining and still has quota;
				// a recovery slope would otherwise produce a nonsense instant.
				if change > 0 && row.RemainingFraction > 0 {
					hoursLeft := row.RemainingFraction / change
					accountRow.ExhaustedAt = now.Add(time.Duration(hoursLeft * float64(time.Hour))).Unix()
				}
			}
		}
		byModel[row.Model] = append(byModel[row.Model], accountRow)
	}

	models := make([]string, 0, len(byModel))
	for model := range byModel {
		models = append(models, model)
	}
	sort.Strings(models)

	summaries := make([]ChannelModelQuotaSummary, 0, len(models))
	for _, modelName := range models {
		accountRows := byModel[modelName]
		// Available accounts first (most quota first), then the drained ones by
		// soonest recovery, so the board answers "when does this come back"
		// without further sorting. Accounts with no reset clock sort last.
		sort.Slice(accountRows, func(i, j int) bool {
			left, right := accountRows[i], accountRows[j]
			if left.Exhausted != right.Exhausted {
				return !left.Exhausted
			}
			if !left.Exhausted {
				if left.RemainingFraction != right.RemainingFraction {
					return left.RemainingFraction > right.RemainingFraction
				}
				return left.ChannelId < right.ChannelId
			}
			leftReset, rightReset := left.ResetTime, right.ResetTime
			if leftReset == 0 {
				leftReset = math.MaxInt64
			}
			if rightReset == 0 {
				rightReset = math.MaxInt64
			}
			if leftReset != rightReset {
				return leftReset < rightReset
			}
			return left.ChannelId < right.ChannelId
		})
		summary := ChannelModelQuotaSummary{
			Model:         modelName,
			TotalAccounts: len(accountRows),
			Accounts:      accountRows,
		}
		for _, accountRow := range accountRows {
			if accountRow.Exhausted {
				if accountRow.ResetTime > 0 && (summary.EarliestReset == 0 || accountRow.ResetTime < summary.EarliestReset) {
					summary.EarliestReset = accountRow.ResetTime
				}
				continue
			}
			summary.AvailableAccounts++
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

// FilterQuotaSummaries keeps the models the operator actually routes and orders
// the result so drained models surface first. It returns the visible slice and
// how many upstream-only models were hidden.
func FilterQuotaSummaries(
	summaries []ChannelModelQuotaSummary,
	served map[string]struct{},
	includeAll bool,
) ([]ChannelModelQuotaSummary, int) {
	visible := make([]ChannelModelQuotaSummary, 0, len(summaries))
	hidden := 0
	for _, summary := range summaries {
		_, inUse := served[summary.Model]
		summary.InUse = inUse
		if !inUse && !includeAll {
			hidden++
			continue
		}
		visible = append(visible, summary)
	}

	sort.SliceStable(visible, func(i, j int) bool {
		left, right := visible[i], visible[j]
		if left.InUse != right.InUse {
			return left.InUse
		}
		leftShort := left.AvailableAccounts < left.TotalAccounts
		rightShort := right.AvailableAccounts < right.TotalAccounts
		if leftShort != rightShort {
			return leftShort
		}
		return left.Model < right.Model
	})
	return visible, hidden
}

// PurgeStaleChannelModelQuotas removes rows for channels that no longer exist.
func PurgeStaleChannelModelQuotas() (int64, error) {
	var validIDs []int
	if err := DB.Model(&Channel{}).Pluck("id", &validIDs).Error; err != nil {
		return 0, err
	}
	query := DB
	if len(validIDs) > 0 {
		query = query.Where("channel_id NOT IN ?", validIDs)
	} else {
		// No channel survives, so every stored snapshot is stale. GORM refuses
		// an unconditional delete, so the full sweep is opted in explicitly.
		query = query.Session(&gorm.Session{AllowGlobalUpdate: true})
	}
	result := query.Delete(&ChannelModelQuota{})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected > 0 {
		_ = LoadChannelModelQuotaCache()
	}
	return result.RowsAffected, nil
}
