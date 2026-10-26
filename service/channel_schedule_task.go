package service

import (
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// The ticker runs at a fixed minute and the configured interval is compared
// against the last run, so editing the interval in the console takes effect
// without a restart.
const channelScheduleTickInterval = time.Minute

var channelScheduleLastRunAt atomic.Int64

// StartChannelScheduleTask applies the channel time schedule on the master
// node. Follower nodes keep serving with whatever the master applied, the same
// way the credential refresh tasks behave.
func StartChannelScheduleTask() {
	go func() {
		if !common.IsMasterNode {
			return
		}
		ticker := time.NewTicker(channelScheduleTickInterval)
		defer ticker.Stop()
		for range ticker.C {
			snapshot := model.CurrentChannelSchedule()
			if !snapshot.Enabled {
				continue
			}
			interval := time.Duration(snapshot.CheckMinutes) * time.Minute
			if last := channelScheduleLastRunAt.Load(); last > 0 && time.Since(time.Unix(last, 0)) < interval {
				continue
			}
			channelScheduleLastRunAt.Store(time.Now().Unix())
			if _, err := RunChannelSchedule(false); err != nil {
				common.SysError(common.LogText("channel schedule run failed: %s", err.Error()))
			}
		}
	}()
}
