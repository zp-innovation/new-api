package model

import (
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"gorm.io/gorm/clause"
)

// WorkBuddyModelEntitlement records an upstream entitlement refusal for one
// WorkBuddy account/model pair. It is deliberately account-scoped: two
// credentials in the same channel can have different plans.
type WorkBuddyModelEntitlement struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId  int    `json:"channel_id" gorm:"not null;uniqueIndex:idx_wb_model_entitlement"`
	AccountUid string `json:"account_uid" gorm:"type:varchar(128);not null;uniqueIndex:idx_wb_model_entitlement"`
	Realm      string `json:"realm" gorm:"type:varchar(16);not null"`
	Model      string `json:"model" gorm:"type:varchar(191);not null;uniqueIndex:idx_wb_model_entitlement"`
	Code       int    `json:"code" gorm:"not null"`
	UserType   string `json:"user_type" gorm:"type:varchar(64);not null"`
	CheckedAt  int64  `json:"checked_at" gorm:"bigint;not null;index:idx_wb_model_entitlement_checked"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt  int64  `json:"updated_at" gorm:"bigint"`
}

func (WorkBuddyModelEntitlement) TableName() string {
	return "workbuddy_model_entitlements"
}

const WorkBuddyEntitlementTTL = 24 * time.Hour

type workBuddyEntitlementKey struct {
	ChannelId  int
	AccountUid string
	Model      string
}

var workBuddyEntitlementMarks sync.Map

// RefreshWorkBuddyModelEntitlements loads the fresh marks into the hot-path
// cache. Channel cache refreshes call it so a restart or multi-node database
// update becomes visible without a request-time query.
func RefreshWorkBuddyModelEntitlements(now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-WorkBuddyEntitlementTTL).Unix()
	var rows []WorkBuddyModelEntitlement
	if err := DB.Where("checked_at >= ?", cutoff).Find(&rows).Error; err != nil {
		return err
	}
	workBuddyEntitlementMarks.Range(func(key, _ any) bool {
		workBuddyEntitlementMarks.Delete(key)
		return true
	})
	for _, row := range rows {
		workBuddyEntitlementMarks.Store(workBuddyEntitlementKey{
			ChannelId: row.ChannelId, AccountUid: row.AccountUid, Model: row.Model,
		}, row.CheckedAt)
	}
	return nil
}

// MarkWorkBuddyModelBlocked stores a 11158 refusal. The mark expires after a
// day because a plan or account may change; it is not a permanent allow-list.
func MarkWorkBuddyModelBlocked(channelId int, uid, realm, modelName string, code int, userType string, now time.Time) error {
	uid = strings.TrimSpace(uid)
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || uid == "" || modelName == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	entry := &WorkBuddyModelEntitlement{
		ChannelId:  channelId,
		AccountUid: uid,
		Realm:      strings.TrimSpace(realm),
		Model:      modelName,
		Code:       code,
		UserType:   strings.TrimSpace(userType),
		CheckedAt:  now.Unix(),
		CreatedAt:  now.Unix(),
		UpdatedAt:  now.Unix(),
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}, {Name: "account_uid"}, {Name: "model"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"realm", "code", "user_type", "checked_at", "updated_at",
		}),
	}).Create(entry).Error; err != nil {
		return err
	}
	workBuddyEntitlementMarks.Store(workBuddyEntitlementKey{
		ChannelId: channelId, AccountUid: uid, Model: modelName,
	}, now.Unix())
	return nil
}

// ClearWorkBuddyModelBlocked removes the mark after a successful request. One
// success is enough to disprove the current negative entitlement.
func ClearWorkBuddyModelBlocked(channelId int, uid, modelName string) error {
	uid = strings.TrimSpace(uid)
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || uid == "" || modelName == "" {
		return nil
	}
	if err := DB.Where("channel_id = ? AND account_uid = ? AND model = ?", channelId, uid, modelName).
		Delete(&WorkBuddyModelEntitlement{}).Error; err != nil {
		return err
	}
	workBuddyEntitlementMarks.Delete(workBuddyEntitlementKey{
		ChannelId: channelId, AccountUid: uid, Model: modelName,
	})
	return nil
}

// WorkBuddyChannelHasUsableCredential reports whether at least one credential
// line in a channel lacks a fresh entitlement mark for this model. Unparseable
// keys stay eligible so the normal credential error remains visible.
func WorkBuddyChannelHasUsableCredential(channel *Channel, modelName string, now time.Time) bool {
	if channel == nil || channel.Type != constant.ChannelTypeWorkBuddy || strings.TrimSpace(modelName) == "" {
		return true
	}
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-WorkBuddyEntitlementTTL).Unix()
	for line := range strings.SplitSeq(channel.Key, "\n") {
		raw := strings.TrimSpace(line)
		if raw == "" {
			continue
		}
		var credential struct {
			UID string `json:"uid"`
		}
		if err := common.Unmarshal([]byte(raw), &credential); err != nil || strings.TrimSpace(credential.UID) == "" {
			return true
		}
		if checkedAt, ok := workBuddyEntitlementMarks.Load(workBuddyEntitlementKey{
			ChannelId: channel.Id, AccountUid: strings.TrimSpace(credential.UID), Model: modelName,
		}); !ok {
			return true
		} else if checked, ok := checkedAt.(int64); !ok || checked < cutoff {
			workBuddyEntitlementMarks.Delete(workBuddyEntitlementKey{
				ChannelId: channel.Id, AccountUid: strings.TrimSpace(credential.UID), Model: modelName,
			})
			return true
		}
	}
	return false
}

// WorkBuddyAccountsBlockedForModel returns the UIDs whose 11158 mark is still
// fresh. Entries are pruned on read so the table cannot silently accumulate
// stale plan evidence.
func WorkBuddyAccountsBlockedForModel(channelId int, modelName string, now time.Time) (map[string]bool, error) {
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || modelName == "" {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-WorkBuddyEntitlementTTL).Unix()
	// This lookup runs on every WorkBuddy request and the common answer is "no
	// blocked account", so the result set is allocated only when it is needed.
	// Callers use it as a lookup set; a nil map answers every lookup with false.
	var blocked map[string]bool
	workBuddyEntitlementMarks.Range(func(key, value any) bool {
		entry, ok := key.(workBuddyEntitlementKey)
		if !ok || entry.ChannelId != channelId || entry.Model != modelName {
			return true
		}
		checked, ok := value.(int64)
		if !ok || checked < cutoff {
			workBuddyEntitlementMarks.Delete(key)
			return true
		}
		if blocked == nil {
			blocked = make(map[string]bool)
		}
		blocked[entry.AccountUid] = true
		return true
	})
	return blocked, nil
}
