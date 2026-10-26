package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	ccore "github.com/QuantumNous/new-api/pkg/commandcode"
	occore "github.com/QuantumNous/new-api/pkg/opencode"
)

// channelAccountQuotaTimeout bounds one account's quota lookup.
const channelAccountQuotaTimeout = 30 * time.Second

// SyncChannelAccountQuotas polls every channel whose upstream reports a single
// account-level pool instead of per-model limits.
func SyncChannelAccountQuotas(ctx context.Context) (int, error) {
	var channels []*model.Channel
	if err := model.DB.
		Select("id", "name", "key", "type", "status", "setting").
		Where("type IN ? AND status = ?", []int{constant.ChannelTypeWorkBuddy, constant.ChannelTypeOpenCode, constant.ChannelTypeCommandCode}, common.ChannelStatusEnabled).
		Order("id asc").
		Find(&channels).Error; err != nil {
		return 0, err
	}

	// A failed poll must not look like a drained account, so the previous
	// numbers are read once and reused for every channel whose fetch fails.
	existing, err := model.ListChannelAccountQuotas()
	if err != nil {
		return 0, err
	}
	previous := make(map[int]model.ChannelAccountQuota, len(existing))
	for _, quota := range existing {
		previous[quota.ChannelId] = quota
	}

	rows := make([]model.ChannelAccountQuota, 0, len(channels))
	polled := 0
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		row := model.ChannelAccountQuota{ChannelId: ch.Id, FetchedAt: time.Now().Unix()}
		pollCtx, cancel := context.WithTimeout(ctx, channelAccountQuotaTimeout)
		var err error
		switch ch.Type {
		case constant.ChannelTypeWorkBuddy:
			var credits *WorkBuddyChannelCredits
			credits, err = GetWorkBuddyChannelCredits(pollCtx, ch)
			if err == nil && credits != nil && credits.Credits != nil {
				row.Remain = credits.Credits.Remain
				row.Total = credits.Credits.Size
				// WorkBuddy exposes per-package expiry but no single window reset,
				// so ResetTime stays zero until a real reset clock is available.
			}
		case constant.ChannelTypeCommandCode:
			var usage *CommandCodeChannelUsage
			usage, err = GetCommandCodeChannelUsage(pollCtx, ch.Id)
			if err == nil {
				total, remain, reset, ok := commandCodeCredits(usage)
				if ok {
					row.Total = total
					row.Remain = remain
					row.ResetTime = reset
				} else {
					err = errors.New("command code usage returned no credits")
				}
			}
		case constant.ChannelTypeOpenCode:
			var usage *OpenCodeChannelUsage
			usage, err = GetOpenCodeChannelUsage(pollCtx, ch.Id)
			if err == nil && usage != nil {
				if entry, ok := mostConstrainedOpenCodeWindow(usage); ok {
					row.Total = 1
					row.Remain = entry.remain
					row.ResetTime = entry.reset
				} else {
					err = errors.New("opencode usage returned no window")
				}
			}
		}
		cancel()

		if err != nil {
			// Keep the previous numbers visible but flag the poll as stale.
			if old, ok := previous[ch.Id]; ok {
				row.Remain = old.Remain
				row.Total = old.Total
				row.ResetTime = old.ResetTime
			}
			row.Status = model.ChannelAccountQuotaStatusError
			row.Error = err.Error()
			logger.LogWarn(ctx, common.LogText("channel account quota: channel_id=%d type=%d fetch failed: %v", ch.Id, ch.Type, err))
			rows = append(rows, row)
			continue
		}
		row.Status = model.ChannelAccountQuotaStatusOK
		rows = append(rows, row)
		polled++
	}

	if len(rows) > 0 {
		if err := model.UpsertChannelAccountQuotas(rows); err != nil {
			return polled, err
		}
	}
	return polled, nil
}

type channelAccountQuotaSyncHandler struct{}

func (channelAccountQuotaSyncHandler) Type() string {
	return model.SystemTaskTypeChannelAccountQuotaSync
}

func (channelAccountQuotaSyncHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	polled, err := SyncChannelAccountQuotas(ctx)
	if err != nil {
		logger.LogWarn(ctx, common.LogText("channel account quota sync failed: %v", err))
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, err.Error())
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded,
		map[string]any{"polled_accounts": polled, "synced_at": time.Now().Unix()}, "")
}

func (channelAccountQuotaSyncHandler) Enabled() bool { return true }

// HasPendingWork reports whether any enabled channel of a type that publishes an
// account-level pool exists to poll.
func (channelAccountQuotaSyncHandler) HasPendingWork() bool {
	return scheduledChannelsHaveWork(
		constant.ChannelTypeWorkBuddy,
		constant.ChannelTypeOpenCode,
		constant.ChannelTypeCommandCode,
	)
}

func (channelAccountQuotaSyncHandler) Interval() time.Duration { return 15 * time.Minute }

func (channelAccountQuotaSyncHandler) NewPayload() any { return map[string]any{} }

func init() {
	RegisterSystemTaskHandler(channelAccountQuotaSyncHandler{})
}

// mostConstrainedOpenCodeWindow picks the window with the highest used percent:
// the account runs out as soon as any one of them is exhausted.
func mostConstrainedOpenCodeWindow(usage *OpenCodeChannelUsage) (struct {
	remain float64
	reset  int64
}, bool) {
	out := struct {
		remain float64
		reset  int64
	}{}
	if usage == nil || len(usage.Entries) == 0 {
		return out, false
	}
	worst := -1.0
	for _, entry := range usage.Entries {
		for _, window := range []occore.UsageWindow{entry.Rolling, entry.Weekly, entry.Monthly} {
			if window.Percent > worst {
				worst = window.Percent
				out.remain = (100 - window.Percent) / 100
				if out.remain < 0 {
					out.remain = 0
				}
				out.reset = 0
				if parsed, err := time.Parse(time.RFC3339, window.ResetsAt); err == nil {
					out.reset = parsed.Unix()
				}
			}
		}
	}
	return out, worst >= 0
}

// commandCodeCredits reduces every credential of a channel to one account
// figure.
//
//   - monthly pools are summed across credentials, not maxed, so a multi-account
//     channel reports its real total;
//   - purchased add-on credits are counted on top of the plan cap;
//   - the five-hour / weekly windows cap the result, because a monthly balance
//     means nothing when a short window is already exhausted;
//   - PeriodEnd accepts both RFC3339 and a bare date.
func commandCodeCredits(usage *CommandCodeChannelUsage) (total float64, remain float64, reset int64, ok bool) {
	if usage == nil || len(usage.Entries) == 0 {
		return 0, 0, 0, false
	}

	var totalSum, remainSum float64
	var earliestReset int64
	worstPercent := -1.0
	var worstRemain, worstReset float64

	rememberReset := func(unix int64) {
		if unix > 0 && (earliestReset == 0 || unix < earliestReset) {
			earliestReset = unix
		}
	}
	parseClock := func(raw string) int64 {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return 0
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02"} {
			if parsed, err := time.Parse(layout, raw); err == nil {
				return parsed.Unix()
			}
		}
		return 0
	}

	for _, entry := range usage.Entries {
		cap := entry.MonthlyCap
		purchased := entry.PurchasedCredits
		if cap <= 0 {
			cap = entry.MonthlyCredits + purchased + entry.FreeCredits
			purchased = 0
		}
		capacity := cap + purchased
		if capacity <= 0 {
			continue
		}
		used := entry.MonthlyUsed
		if used > capacity {
			used = capacity
		}
		totalSum += capacity
		remainSum += capacity - used
		rememberReset(parseClock(entry.PeriodEnd))

		for _, window := range []*ccore.WindowUsage{entry.FiveHour, entry.Weekly} {
			if window == nil || window.Cap <= 0 {
				continue
			}
			if window.Percent > worstPercent {
				worstPercent = window.Percent
				worstRemain = (100 - window.Percent) / 100
				if worstRemain < 0 {
					worstRemain = 0
				}
				worstReset = 0
				if parsed, err := time.Parse(time.RFC3339, window.ResetAt); err == nil {
					worstReset = float64(parsed.Unix())
				}
			}
		}
	}

	if totalSum <= 0 {
		return 0, 0, 0, false
	}
	remain = remainSum
	// A tighter rolling or weekly window caps what the account can actually use.
	if worstPercent >= 0 && worstRemain < remainSum/totalSum {
		remain = totalSum * worstRemain
		if worstReset > 0 {
			rememberReset(int64(worstReset))
		}
	}
	return totalSum, remain, earliestReset, true
}
