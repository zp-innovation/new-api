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
	agcore "github.com/QuantumNous/new-api/pkg/antigravity"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	antigravityCredentialRefreshTickInterval = 15 * time.Minute
	// Google access tokens live about 55 minutes, so refresh 15 minutes early.
	antigravityCredentialRefreshWindow    = 15 * time.Minute
	antigravityCredentialRefreshBatchSize = 200
	antigravityCredentialRefreshTimeout   = 15 * time.Second
)

var (
	antigravityCredentialRefreshOnce    sync.Once
	antigravityCredentialRefreshRunning atomic.Bool
)

// StartAntigravityCredentialAutoRefreshTask refreshes Antigravity credentials
// before they expire. Like the Codex task, only the master node runs it.
func StartAntigravityCredentialAutoRefreshTask() {
	antigravityCredentialRefreshOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}

		gopool.Go(func() {
			logger.LogInfo(context.Background(), common.LogText("antigravity credential auto-refresh task started: tick=%s window=%s", antigravityCredentialRefreshTickInterval, antigravityCredentialRefreshWindow))

			ticker := time.NewTicker(antigravityCredentialRefreshTickInterval)
			defer ticker.Stop()

			runAntigravityCredentialAutoRefreshOnce()
			for range ticker.C {
				runAntigravityCredentialAutoRefreshOnce()
			}
		})
	})
}

func runAntigravityCredentialAutoRefreshOnce() {
	if !antigravityCredentialRefreshRunning.CompareAndSwap(false, true) {
		return
	}
	defer antigravityCredentialRefreshRunning.Store(false)

	ctx := context.Background()
	now := time.Now()

	var refreshed int
	var scanned int

	offset := 0
	for {
		var channels []*model.Channel
		err := model.DB.
			Select("id", "name", "key", "status", "channel_info").
			Where("type = ? AND (status = ? OR status = ?)",
				constant.ChannelTypeAntigravity,
				common.ChannelStatusEnabled,
				common.ChannelStatusAutoDisabled,
			).
			Order("id asc").
			Limit(antigravityCredentialRefreshBatchSize).
			Offset(offset).
			Find(&channels).Error
		if err != nil {
			logger.LogError(ctx, common.LogText("antigravity credential auto-refresh: query channels failed: %v", err))
			return
		}
		if len(channels) == 0 {
			break
		}
		offset += antigravityCredentialRefreshBatchSize

		for _, ch := range channels {
			if ch == nil {
				continue
			}
			scanned++
			if ch.ChannelInfo.IsMultiKey {
				continue
			}

			credential, err := agcore.ParseCredential(strings.TrimSpace(ch.Key))
			if err != nil || strings.TrimSpace(credential.RefreshToken) == "" {
				continue
			}
			if !AntigravityCredentialNeedsRefresh(credential, now, antigravityCredentialRefreshWindow) {
				continue
			}

			refreshCtx, cancel := context.WithTimeout(ctx, antigravityCredentialRefreshTimeout)
			updated, _, err := RefreshAntigravityChannelCredential(refreshCtx, ch.Id, AntigravityCredentialRefreshOptions{ResetCaches: false})
			cancel()
			if err != nil {
				logger.LogWarn(ctx, common.LogText("antigravity credential auto-refresh: channel_id=%d name=%s refresh failed: %v", ch.Id, ch.Name, err))
				continue
			}

			refreshed++
			logger.LogInfo(ctx, common.LogText("antigravity credential auto-refresh: channel_id=%d name=%s refreshed, expires_at=%s", ch.Id, ch.Name, updated.ExpiresAt))
		}
	}

	if refreshed > 0 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.LogWarn(ctx, common.LogText("antigravity credential auto-refresh: InitChannelCache panic: %v", r))
				}
			}()
			model.InitChannelCache()
		}()
	}

	if common.DebugEnabled {
		logger.LogDebug(ctx, "antigravity credential auto-refresh: scanned=%d refreshed=%d", scanned, refreshed)
	}
}
