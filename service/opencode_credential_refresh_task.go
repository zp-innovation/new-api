package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	openCodeCredentialRefreshTickInterval = 5 * time.Minute
	openCodeCredentialRefreshWindow       = 5 * time.Minute
	openCodeCredentialRefreshBatchSize    = 200
	openCodeCredentialRefreshTimeout      = 15 * time.Second
)

var (
	openCodeCredentialRefreshOnce    sync.Once
	openCodeCredentialRefreshRunning atomic.Bool
)

// StartOpenCodeCredentialAutoRefreshTask refreshes OpenCode credentials before
// they expire. Only the master node runs it.
func StartOpenCodeCredentialAutoRefreshTask() {
	openCodeCredentialRefreshOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), common.LogText(
				"opencode credential auto-refresh task started: tick=%s window=%s",
				openCodeCredentialRefreshTickInterval,
				openCodeCredentialRefreshWindow,
			))
			ticker := time.NewTicker(openCodeCredentialRefreshTickInterval)
			defer ticker.Stop()
			runOpenCodeCredentialAutoRefreshOnce()
			for range ticker.C {
				runOpenCodeCredentialAutoRefreshOnce()
			}
		})
	})
}

func runOpenCodeCredentialAutoRefreshOnce() {
	if !openCodeCredentialRefreshRunning.CompareAndSwap(false, true) {
		return
	}
	defer openCodeCredentialRefreshRunning.Store(false)

	ctx := context.Background()
	var refreshed int
	var scanned int
	offset := 0
	for {
		var channels []*model.Channel
		err := model.DB.
			Select("id", "name", "key", "status", "channel_info").
			Where("type = ? AND (status = ? OR status = ?)",
				constant.ChannelTypeOpenCode,
				common.ChannelStatusEnabled,
				common.ChannelStatusAutoDisabled,
			).
			Order("id asc").
			Limit(openCodeCredentialRefreshBatchSize).
			Offset(offset).
			Find(&channels).Error
		if err != nil {
			logger.LogError(ctx, common.LogText("opencode credential auto-refresh: query channels failed: %v", err))
			return
		}
		if len(channels) == 0 {
			break
		}
		offset += openCodeCredentialRefreshBatchSize
		for _, ch := range channels {
			if ch == nil {
				continue
			}
			scanned++
			refreshCtx, cancel := context.WithTimeout(ctx, openCodeCredentialRefreshTimeout)
			_, _, err := RefreshOpenCodeChannelCredential(refreshCtx, ch.Id, false)
			cancel()
			if errors.Is(err, errOpenCodeNoRefreshNeeded) {
				continue
			}
			if err != nil {
				logger.LogWarn(ctx, common.LogText(
					"opencode credential auto-refresh: channel_id=%d name=%s refresh failed: %v",
					ch.Id, ch.Name, err,
				))
				continue
			}
			refreshed++
		}
	}
	if refreshed > 0 {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.LogWarn(ctx, common.LogText("opencode credential auto-refresh: InitChannelCache panic: %v", recovered))
				}
			}()
			model.InitChannelCache()
		}()
	}
	if common.DebugEnabled {
		logger.LogDebug(ctx, "opencode credential auto-refresh: scanned=%d refreshed=%d", scanned, refreshed)
	}
}
