package service

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	// WorkBuddy access tokens live for weeks, so a ten minute tick is plenty and
	// keeps the task cheap. The window is wider than the tick so a token is
	// always refreshed before it lapses: the worst case is one tick of delay
	// against a thirty minute margin.
	workBuddyCredentialRefreshTickInterval = 10 * time.Minute
	workBuddyCredentialRefreshWindow       = 30 * time.Minute
	workBuddyCredentialRefreshBatchSize    = 200
	workBuddyCredentialRefreshTimeout      = 30 * time.Second
)

var (
	workBuddyCredentialRefreshOnce    sync.Once
	workBuddyCredentialRefreshRunning atomic.Bool
)

// StartWorkBuddyCredentialAutoRefreshTask renews WorkBuddy credentials before
// they expire. Without it a lapsed token only surfaces as a 401 on the relay
// path, which also trips the channel's auto-ban, so the channel would stay
// disabled until an operator opened the console panel by hand.
func StartWorkBuddyCredentialAutoRefreshTask() {
	workBuddyCredentialRefreshOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}

		gopool.Go(func() {
			logger.LogInfo(context.Background(), common.LogText(
				"workbuddy credential auto-refresh task started: tick=%s window=%s",
				workBuddyCredentialRefreshTickInterval, workBuddyCredentialRefreshWindow))

			ticker := time.NewTicker(workBuddyCredentialRefreshTickInterval)
			defer ticker.Stop()

			runWorkBuddyCredentialAutoRefreshOnce()
			for range ticker.C {
				runWorkBuddyCredentialAutoRefreshOnce()
			}
		})
	})
}

func runWorkBuddyCredentialAutoRefreshOnce() {
	if !workBuddyCredentialRefreshRunning.CompareAndSwap(false, true) {
		return
	}
	defer workBuddyCredentialRefreshRunning.Store(false)

	ctx := context.Background()
	now := time.Now()
	var refreshed int
	var scanned int

	offset := 0
	for {
		var channels []*model.Channel
		err := model.DB.
			Select("id", "name", "key", "status").
			Where("type = ? AND (status = ? OR status = ?)",
				constant.ChannelTypeWorkBuddy,
				common.ChannelStatusEnabled,
				common.ChannelStatusAutoDisabled,
			).
			Order("id asc").
			Limit(workBuddyCredentialRefreshBatchSize).
			Offset(offset).
			Find(&channels).Error
		if err != nil {
			logger.LogError(ctx, common.LogText("workbuddy credential auto-refresh: query channels failed: %v", err))
			return
		}
		if len(channels) == 0 {
			break
		}
		offset += workBuddyCredentialRefreshBatchSize

		for _, ch := range channels {
			if ch == nil {
				continue
			}
			scanned++
			refreshCtx, cancel := context.WithTimeout(ctx, workBuddyCredentialRefreshTimeout)
			count, err := refreshWorkBuddyChannelLines(refreshCtx, ch, ch.GetSetting().Proxy, now, workBuddyCredentialRefreshWindow)
			cancel()
			if err != nil {
				logger.LogWarn(ctx, common.LogText("workbuddy credential auto-refresh: channel_id=%d name=%s refresh failed: %v", ch.Id, ch.Name, err))
				continue
			}
			if count == 0 {
				continue
			}
			refreshed += count
			logger.LogInfo(ctx, common.LogText("workbuddy credential auto-refresh: channel_id=%d name=%s refreshed %d account(s)", ch.Id, ch.Name, count))
		}
	}

	if refreshed > 0 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.LogWarn(ctx, common.LogText("workbuddy credential auto-refresh: InitChannelCache panic: %v", r))
				}
			}()
			model.InitChannelCache()
		}()
	}

	if common.DebugEnabled {
		logger.LogDebug(ctx, "workbuddy credential auto-refresh: scanned=%d refreshed=%d", scanned, refreshed)
	}
}

// refreshWorkBuddyChannelLines renews every credential line of one channel that
// is due, and writes the key back once. A channel key can hold several accounts
// (one compact credential per line) and the upstream rotates refresh tokens, so
// each rotated value has to replace the line it came from.
func refreshWorkBuddyChannelLines(ctx context.Context, ch *model.Channel, proxyURL string, now time.Time, window time.Duration) (int, error) {
	if ch == nil || strings.TrimSpace(ch.Key) == "" {
		return 0, nil
	}
	lines := strings.Split(ch.Key, "\n")
	refreshed := 0
	changed := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		credential, err := wbcore.ParseCredential(trimmed)
		if err != nil {
			// A half-edited line must not stop the others from being renewed.
			continue
		}
		if strings.TrimSpace(credential.RefreshToken) == "" {
			continue
		}
		if !credential.NeedsRefresh(now, window) {
			continue
		}
		updated, err := RefreshWorkBuddyCredential(ctx, trimmed, proxyURL)
		if err != nil {
			logger.LogWarn(ctx, common.LogText("workbuddy credential auto-refresh: channel_id=%d account=%s refresh failed: %v", ch.Id, credential.UID, err))
			continue
		}
		encoded, err := updated.Marshal()
		if err != nil {
			continue
		}
		lines[index] = encoded
		changed = true
		refreshed++
	}
	if !changed || ch.Id <= 0 {
		return refreshed, nil
	}
	updated, err := model.UpdateChannelKeyIfUnchanged(ch.Id, ch.Key, strings.Join(lines, "\n"))
	if err != nil {
		return refreshed, err
	}
	if !updated {
		// Another refresh (console action, relay path) rotated the key while this
		// batch was in flight. Discard our copy; the next tick retries.
		logger.LogWarn(ctx, common.LogText("workbuddy credential auto-refresh: channel_id=%d credential was refreshed by another task, discarding this batch", ch.Id))
		return 0, model.ErrChannelKeyChanged
	}
	return refreshed, nil
}
