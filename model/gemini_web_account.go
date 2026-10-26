package model

import (
	"errors"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"gorm.io/gorm"
)

// Gemini Web account states.
const (
	GeminiWebAccountEnabled  = "enabled"
	GeminiWebAccountDisabled = "disabled"

	// GeminiWebMaxAuthFailures is the number of consecutive 401/403 replies
	// after which an account is removed from selection. One transient XSRF
	// response must not disable a credential; three consecutive failures means
	// the session is no longer usable and should be re-imported.
	GeminiWebMaxAuthFailures = 3
)

var geminiWebAccountPickMu sync.Mutex

// GeminiWebAccount is one signed-in Google account used by the Gemini Web
// channel. The cookie is a full Cookie header value and is never returned to
// the console.
type GeminiWebAccount struct {
	Id         int    `json:"id" gorm:"primaryKey"`
	ChannelId  int    `json:"channel_id" gorm:"index"`
	Label      string `json:"label" gorm:"type:varchar(128)"`
	Cookie     string `json:"-" gorm:"type:text;not null"`
	Status     string `json:"status" gorm:"type:varchar(32);index"`
	Note       string `json:"note" gorm:"type:varchar(255)"`
	Proxy      string `json:"-" gorm:"type:text"`
	FailCount  int    `json:"fail_count"`
	LastUsedAt int64  `json:"last_used_at"`
	LastOkAt   int64  `json:"last_ok_at"`
	LastError  string `json:"last_error" gorm:"type:varchar(255)"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// AddGeminiWebAccount inserts one account for a channel.
func AddGeminiWebAccount(channelId int, label, cookie, note, proxy string) (int, error) {
	cookie = strings.TrimSpace(cookie)
	if channelId <= 0 {
		return 0, errors.New("channel id is required")
	}
	if cookie == "" {
		return 0, errors.New("cookie is required")
	}
	now := common.GetTimestamp()
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Gemini Web account"
	}
	account := &GeminiWebAccount{
		ChannelId: channelId,
		Label:     label,
		Cookie:    cookie,
		Status:    GeminiWebAccountEnabled,
		Note:      strings.TrimSpace(note),
		Proxy:     strings.TrimSpace(proxy),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := DB.Create(account).Error; err != nil {
		return 0, err
	}
	return account.Id, nil
}

// InsertGeminiWebChannelWithAccount creates a channel and its initial signed-in
// account atomically. Keeping both rows in one transaction prevents a channel
// from being published without the credential that makes paid models usable.
func InsertGeminiWebChannelWithAccount(channel *Channel, account *GeminiWebAccount) error {
	if channel == nil || account == nil {
		return errors.New("channel and account are required")
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()
	if err := tx.Create(channel).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := channel.AddAbilities(tx); err != nil {
		tx.Rollback()
		return err
	}
	account.ChannelId = channel.Id
	account.Cookie = strings.TrimSpace(account.Cookie)
	account.Label = strings.TrimSpace(account.Label)
	if account.Label == "" {
		account.Label = "Gemini Web account"
	}
	account.Status = GeminiWebAccountEnabled
	now := common.GetTimestamp()
	account.CreatedAt = now
	account.UpdatedAt = now
	if err := tx.Create(account).Error; err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit().Error
}

// ListGeminiWebAccounts returns every account of one channel, newest first.
func ListGeminiWebAccounts(channelId int) ([]*GeminiWebAccount, error) {
	var accounts []*GeminiWebAccount
	if err := DB.Where("channel_id = ?", channelId).Order("id desc").Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

// CountGeminiWebAccounts returns the total and enabled counts of one channel.
func CountGeminiWebAccounts(channelId int) (total int64, enabled int64, err error) {
	if err = DB.Model(&GeminiWebAccount{}).Where("channel_id = ?", channelId).Count(&total).Error; err != nil {
		return 0, 0, err
	}
	if err = DB.Model(&GeminiWebAccount{}).
		Where("channel_id = ? AND status = ?", channelId, GeminiWebAccountEnabled).
		Count(&enabled).Error; err != nil {
		return 0, 0, err
	}
	return total, enabled, nil
}

// ListEnabledGeminiWebAccounts returns enabled accounts whose Gemini Web
// channel still exists and is enabled. Deleted channels leave account rows
// behind, and rotating those credentials keeps a session alive that nothing
// can serve while also competing with the instance that still owns it.
func ListEnabledGeminiWebAccounts() ([]*GeminiWebAccount, error) {
	var accounts []*GeminiWebAccount
	if err := DB.Model(&GeminiWebAccount{}).
		Select("gemini_web_accounts.*").
		Joins("JOIN channels ON channels.id = gemini_web_accounts.channel_id").
		Where("gemini_web_accounts.status = ? AND channels.status = ? AND channels.type = ?",
			GeminiWebAccountEnabled, common.ChannelStatusEnabled, constant.ChannelTypeGeminiWeb).
		Order("gemini_web_accounts.id asc").
		Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

// DeleteGeminiWebAccount removes one account.
func DeleteGeminiWebAccount(id int) error {
	return DB.Delete(&GeminiWebAccount{}, id).Error
}

// SetGeminiWebAccountStatus enables or disables one account.
func SetGeminiWebAccountStatus(id int, status string) error {
	if status != GeminiWebAccountEnabled && status != GeminiWebAccountDisabled {
		return errors.New("invalid account status")
	}
	return DB.Model(&GeminiWebAccount{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": status, "updated_at": common.GetTimestamp()}).Error
}

// UpdateGeminiWebAccountCookieIfUnchanged stores a refreshed cookie only when
// the stored credential is still the one this refresh started from. Google
// rotates one browser session at a time; writing an older result over a newer
// rotation silently invalidates the account.
func UpdateGeminiWebAccountCookieIfUnchanged(id int, oldCookie, cookie string) (bool, error) {
	if strings.TrimSpace(cookie) == "" {
		return false, errors.New("cookie is required")
	}
	result := DB.Model(&GeminiWebAccount{}).
		Where("id = ? AND cookie = ?", id, oldCookie).
		Updates(map[string]any{"cookie": cookie, "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// UpdateGeminiWebAccountCookie stores a refreshed cookie unconditionally.
// Callers on a request path should prefer the CAS variant above.
func UpdateGeminiWebAccountCookie(id int, cookie string) error {
	return DB.Model(&GeminiWebAccount{}).
		Where("id = ?", id).
		Updates(map[string]any{"cookie": cookie, "updated_at": common.GetTimestamp()}).Error
}

// BindGeminiWebAccountProxy pins the egress used by an account. Cookie rotation
// and relay traffic must leave through the same IP so Google does not see one
// session active from two networks.
func BindGeminiWebAccountProxy(id int, proxy string) error {
	return DB.Model(&GeminiWebAccount{}).
		Where("id = ?", id).
		Updates(map[string]any{"proxy": strings.TrimSpace(proxy), "updated_at": common.GetTimestamp()}).Error
}

// PickGeminiWebAccount returns the enabled account that has been idle longest,
// so consecutive requests spread across the pool. A channel without accounts
// returns (nil, nil): the caller then relays anonymously.
func PickGeminiWebAccount(channelId int) (*GeminiWebAccount, error) {
	return PickGeminiWebAccountExcept(channelId, nil)
}

// PickGeminiWebAccountExcept skips accounts already tried by this request.
// Healthy accounts are preferred, then the least recently used one.
func PickGeminiWebAccountExcept(channelId int, skip map[int]bool) (*GeminiWebAccount, error) {
	geminiWebAccountPickMu.Lock()
	defer geminiWebAccountPickMu.Unlock()

	var accounts []*GeminiWebAccount
	if err := DB.Where("channel_id = ? AND status = ?", channelId, GeminiWebAccountEnabled).
		Order("fail_count asc, last_used_at asc, id asc").
		Find(&accounts).Error; err != nil {
		return nil, err
	}
	for _, account := range accounts {
		if skip[account.Id] {
			continue
		}
		if err := DB.Model(&GeminiWebAccount{}).
			Where("id = ?", account.Id).
			Update("last_used_at", common.GetTimestamp()).Error; err != nil {
			// Losing the timestamp only skews the pick order, so keep serving
			// the account; the failure still has to be visible.
			common.SysError(common.LogText("failed to stamp gemini web account usage: account_id=%d, error=%v", account.Id, err))
		}
		return account, nil
	}
	return nil, nil
}

// MarkGeminiWebAccountResult records the outcome of one upstream call. Only
// authentication failures count against the account; transport hiccups do not.
// Every write is logged when it fails, and the combined error is returned so a
// caller that can retry or report it does not depend on the log line alone. In
// particular a failed disable would leave a dead credential enabled forever.
func MarkGeminiWebAccountResult(id int, status int, errMessage string) error {
	if id <= 0 {
		return nil
	}
	now := common.GetTimestamp()
	if status == 200 {
		err := DB.Model(&GeminiWebAccount{}).Where("id = ?", id).Updates(map[string]any{
			"fail_count": 0,
			"last_ok_at": now,
			"last_error": "",
			"updated_at": now,
		}).Error
		if err != nil {
			common.SysError(common.LogText("failed to record gemini web account success: account_id=%d, error=%v", id, err))
		}
		return err
	}
	if status != 401 && status != 403 {
		return nil
	}
	var errs []error
	if err := DB.Model(&GeminiWebAccount{}).
		Where("id = ? AND status = ?", id, GeminiWebAccountEnabled).
		Updates(map[string]any{
			"fail_count": gorm.Expr("fail_count + 1"),
			"last_error": truncateMessage(errMessage, 240),
			"updated_at": now,
		}).Error; err != nil {
		common.SysError(common.LogText("failed to record gemini web account auth failure: account_id=%d, error=%v", id, err))
		errs = append(errs, err)
	}
	if err := DB.Model(&GeminiWebAccount{}).
		Where("id = ? AND status = ? AND fail_count >= ?", id, GeminiWebAccountEnabled, GeminiWebMaxAuthFailures).
		Updates(map[string]any{"status": GeminiWebAccountDisabled, "updated_at": now}).Error; err != nil {
		common.SysError(common.LogText("failed to disable gemini web account after repeated auth failures: account_id=%d, error=%v", id, err))
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func truncateMessage(message string, limit int) string {
	message = strings.TrimSpace(message)
	// Limits are counted in runes: a byte slice can end mid-character and the
	// truncated text would be rejected by a utf8mb4 column.
	runes := []rune(message)
	if len(runes) <= limit {
		return message
	}
	return string(runes[:limit])
}
