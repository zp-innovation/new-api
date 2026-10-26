package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	agcore "github.com/QuantumNous/new-api/pkg/antigravity"
)

const (
	// antigravityQuotaSyncConcurrency bounds how many accounts are polled at
	// once. The upstream catalogue call is cheap, but a large pool should not
	// open hundreds of sockets in one burst.
	antigravityQuotaSyncConcurrency = 4
	// antigravityQuotaSyncTimeout bounds a single account's poll.
	antigravityQuotaSyncTimeout = 30 * time.Second
)

// SyncAntigravityChannelModelQuotas polls every enabled Antigravity account for
// its per-model quota and writes the result to the snapshot table.
//
// fetchAvailableModels is a read-only catalogue call that costs no quota, so
// this is the cheap way to learn when an exhausted account comes back instead
// of waiting for a real request to fail.
func SyncAntigravityChannelModelQuotas(ctx context.Context) (int, error) {
	var channels []*model.Channel
	err := model.DB.
		Select("id", "name", "type", "key", "status", "base_url", "channel_info", "setting").
		Where("type = ? AND status = ?", constant.ChannelTypeAntigravity, common.ChannelStatusEnabled).
		Order("id asc").
		Find(&channels).Error
	if err != nil {
		return 0, err
	}
	if len(channels) == 0 {
		return 0, nil
	}

	sem := make(chan struct{}, antigravityQuotaSyncConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	rows := make([]model.ChannelModelQuota, 0, len(channels)*8)
	polled := 0

	for _, ch := range channels {
		if ch == nil || ch.ChannelInfo.IsMultiKey {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ch *model.Channel) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					logger.LogWarn(ctx, common.LogText("antigravity quota sync: channel_id=%d panic: %v", ch.Id, r))
				}
			}()

			fetched, ok := fetchAntigravityQuotaRows(ctx, ch)
			if !ok {
				return
			}
			mu.Lock()
			rows = append(rows, fetched...)
			polled++
			mu.Unlock()
		}(ch)
	}
	wg.Wait()

	if len(rows) > 0 {
		if err := model.UpsertChannelModelQuotas(rows); err != nil {
			return polled, err
		}
		notifyExhaustedAntigravityModels(ctx)
		// Keep a week of trend data; older samples only exist for the chart.
		if _, err := model.PruneChannelModelQuotaHistory(time.Now().Add(-7 * 24 * time.Hour)); err != nil {
			logger.LogWarn(ctx, common.LogText("antigravity quota history prune failed: %v", err))
		}
	}
	return polled, nil
}

// quotaExhaustedNotified tracks models that are currently fully drained so the
// channel watchers are alerted once per outage instead of on every poll. An entry is
// dropped as soon as the model has a usable account again, which re-arms the
// alert for the next outage.
var quotaExhaustedNotified sync.Map // model -> time.Time

// notifyExhaustedAntigravityModels raises one notification per transition to
// "no account can serve this model". It only looks at models the channels
// actually route; the rest of the upstream catalogue is noise here.
func notifyExhaustedAntigravityModels(ctx context.Context) {
	served, err := model.ModelsServedByType(constant.ChannelTypeAntigravity)
	if err != nil || len(served) == 0 {
		return
	}
	names, err := model.ChannelNamesByType(constant.ChannelTypeAntigravity)
	if err != nil {
		return
	}
	summaries, _ := model.FilterQuotaSummaries(
		model.BuildChannelModelQuotaSummary(time.Now(), names), served, false)

	for _, summary := range takeNewlyExhaustedModels(summaries) {
		reset := "unknown"
		if summary.EarliestReset > 0 {
			reset = time.Unix(summary.EarliestReset, 0).Format("2006-01-02 15:04 MST")
		}
		params := map[string]any{
			"Model":    summary.Model,
			"Accounts": summary.TotalAccounts,
			"Reset":    reset,
		}
		NotifyChannelWatchers("channel_quota_exhausted", func(lang string) (string, string) {
			return i18n.Translate(lang, i18n.MsgChannelNotifyQuotaExhaustedSubject, params),
				i18n.Translate(lang, i18n.MsgChannelNotifyQuotaExhaustedContent, params)
		})
		logger.LogWarn(ctx, common.LogText("antigravity quota: model %s has no available account, earliest reset %s", summary.Model, reset))
	}
}

// takeNewlyExhaustedModels advances the alert state machine and returns only the
// models that just became fully drained. Recovery clears the entry so the next
// outage alerts again.
func takeNewlyExhaustedModels(summaries []model.ChannelModelQuotaSummary) []model.ChannelModelQuotaSummary {
	newly := make([]model.ChannelModelQuotaSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.TotalAccounts == 0 {
			continue
		}
		if summary.AvailableAccounts > 0 {
			quotaExhaustedNotified.Delete(summary.Model)
			continue
		}
		if _, already := quotaExhaustedNotified.Load(summary.Model); already {
			continue
		}
		quotaExhaustedNotified.Store(summary.Model, time.Now())
		newly = append(newly, summary)
	}
	return newly
}

// fetchAntigravityQuotaRows polls one account and converts the catalogue into
// snapshot rows. It reports false when the account could not be read, in which
// case the previous (and now stale) snapshot keeps its place and is ignored by
// routing once it ages out.
func fetchAntigravityQuotaRows(ctx context.Context, ch *model.Channel) ([]model.ChannelModelQuota, bool) {
	credential, err := agcore.ParseCredential(strings.TrimSpace(ch.Key))
	if err != nil {
		logger.LogWarn(ctx, common.LogText("antigravity quota sync: channel_id=%d parse credential failed: %v", ch.Id, err))
		return nil, false
	}

	// A stored channel renews its own access token first; the background
	// credential task usually keeps it fresh, so this is a safety net.
	if AntigravityCredentialNeedsRefresh(credential, time.Now(), antigravityCredentialRefreshWindow) {
		refreshed, _, err := RefreshAntigravityChannelCredential(ctx, ch.Id, AntigravityCredentialRefreshOptions{})
		if err != nil {
			logger.LogWarn(ctx, common.LogText("antigravity quota sync: channel_id=%d refresh failed: %v", ch.Id, err))
			return nil, false
		}
		credential = refreshed
	}

	client, err := newAntigravityOAuthClient(ch.GetSetting().Proxy)
	if err != nil {
		logger.LogWarn(ctx, common.LogText("antigravity quota sync: channel_id=%d build client failed: %v", ch.Id, err))
		return nil, false
	}

	pollCtx, cancel := context.WithTimeout(ctx, antigravityQuotaSyncTimeout)
	defer cancel()

	response, err := client.FetchAvailableModelsFromURL(
		pollCtx,
		antigravityChannelBaseURL(ch),
		credential.AccessToken,
		credential.ProjectID,
	)
	if err != nil {
		logger.LogWarn(ctx, common.LogText("antigravity quota sync: channel_id=%d fetch failed: %v", ch.Id, err))
		return nil, false
	}
	if response == nil || len(response.Models) == 0 {
		return nil, false
	}

	now := time.Now().Unix()
	return quotaRowsFromCatalogue(ch.Id, response, now), true
}

// quotaRowsFromCatalogue converts one upstream catalogue into snapshot rows.
//
// An exhausted account still reports quotaInfo, but with the remaining fraction
// omitted; that omission is the upstream's way of saying zero, so the row is
// written with 0 rather than skipped. A model with no quotaInfo at all is not
// reported for the account, so it is left out and routing stays fail-open for
// it.
func quotaRowsFromCatalogue(channelID int, response *agcore.FetchAvailableModelsResponse, fetchedAt int64) []model.ChannelModelQuota {
	if response == nil || len(response.Models) == 0 {
		return nil
	}
	rows := make([]model.ChannelModelQuota, 0, len(response.Models))
	for name, info := range response.Models {
		name = strings.TrimSpace(name)
		if name == "" || info.QuotaInfo == nil {
			continue
		}
		row := model.ChannelModelQuota{
			ChannelId: channelID,
			Model:     name,
			Status:    model.ChannelModelQuotaStatusOK,
			FetchedAt: fetchedAt,
		}
		row.RemainingFraction = info.QuotaInfo.RemainingFraction
		if reset, err := time.Parse(time.RFC3339, strings.TrimSpace(info.QuotaInfo.ResetTime)); err == nil {
			row.ResetTime = reset.Unix()
		}
		rows = append(rows, row)
	}
	return rows
}

// antigravityQuotaSyncHandler polls the account pool every 15 minutes. The
// interval matches how coarsely the upstream windows move: a 5-hour Claude
// window and an hourly-ish Gemini window both tolerate a quarter-hour lag, and
// a tighter loop would only add load.
type antigravityQuotaSyncHandler struct{}

func (antigravityQuotaSyncHandler) Type() string { return model.SystemTaskTypeChannelModelQuotaSync }

func (antigravityQuotaSyncHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	polled, err := SyncAntigravityChannelModelQuotas(ctx)
	if err != nil {
		logger.LogWarn(ctx, common.LogText("antigravity quota sync: poll failed: %v", err))
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, err.Error())
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded,
		map[string]any{"polled_accounts": polled, "synced_at": time.Now().Unix()}, "")
}

func (antigravityQuotaSyncHandler) Enabled() bool { return true }

// HasPendingWork reports whether an enabled Antigravity channel would actually
// be polled. The pass skips multi-key channels, so a deployment that only has
// those is idle as far as this sync is concerned.
func (antigravityQuotaSyncHandler) HasPendingWork() bool {
	channels, err := model.ListEnabledChannelsOfTypes([]int{constant.ChannelTypeAntigravity})
	if err != nil {
		return true
	}
	for _, channel := range channels {
		if channel != nil && !channel.ChannelInfo.IsMultiKey {
			return true
		}
	}
	return false
}

func (antigravityQuotaSyncHandler) Interval() time.Duration { return 15 * time.Minute }

func (antigravityQuotaSyncHandler) NewPayload() any { return map[string]any{} }

func init() {
	RegisterSystemTaskHandler(antigravityQuotaSyncHandler{})
}
