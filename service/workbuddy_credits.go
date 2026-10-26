package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
)

// WorkBuddyChannelCredits is one account's credit state plus the identity of
// the account it belongs to. A WorkBuddy channel may hold several credentials
// (one per line) and the panel queries the first usable one, so the account has
// to travel with the numbers instead of being silently implied.
type WorkBuddyChannelCredits struct {
	Credits      *wbcore.Credits
	AccountUID   string
	AccountName  string
	AccountCount int
}

// GetWorkBuddyChannelCredits reads the credits, usage and daily check-in state
// of a WorkBuddy channel's first usable account. It backs the balance-query
// entry point, which subscription channels use instead of a monetary balance.
func GetWorkBuddyChannelCredits(ctx context.Context, ch *model.Channel) (*WorkBuddyChannelCredits, error) {
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeWorkBuddy {
		return nil, errors.New("channel type is not WorkBuddy")
	}

	lines, _, credential, err := firstWorkBuddyCredential(ch.Key)
	if err != nil {
		return nil, err
	}

	// A stored channel renews its own access token first; a preview channel
	// (ch.Id == 0) has nothing to write back and uses the credential as given.
	if ch.Id > 0 && credential.NeedsRefresh(time.Now(), wbcore.CredentialRefreshWindow) {
		refreshed, err := RefreshWorkBuddyChannelCredential(ctx, ch, ch.GetSetting().Proxy)
		if err != nil {
			return nil, err
		}
		credential = refreshed
	}

	client, err := workbuddyOAuthClient(credential.Realm, credential.Product, ch.GetSetting().Proxy)
	if err != nil {
		return nil, err
	}
	credits, err := client.FetchCredits(ctx, credential.Identity(), time.Now())
	if err != nil {
		return nil, err
	}
	cacheWorkBuddyCredits(ch, credential, credits)
	return &WorkBuddyChannelCredits{
		Credits:      credits,
		AccountUID:   strings.TrimSpace(credential.UID),
		AccountName:  strings.TrimSpace(credential.Nickname),
		AccountCount: countWorkBuddyCredentials(lines),
	}, nil
}

// cacheWorkBuddyCredits stores the freshly read balance and streak on the account
// row, so the console's account table shows the numbers without asking the
// upstream again.
func cacheWorkBuddyCredits(ch *model.Channel, credential *wbcore.Credential, credits *wbcore.Credits) {
	if ch == nil || credential == nil || credits == nil || ch.Id <= 0 {
		return
	}
	uid := strings.TrimSpace(credential.UID)
	if uid == "" {
		return
	}
	state, err := model.GetWorkBuddyAccountState(ch.Id, uid)
	if err != nil {
		return
	}
	if state == nil {
		state = &model.WorkBuddyAccountState{ChannelId: ch.Id, AccountUid: uid}
	}
	identity := credential.Identity()
	state.Realm = string(identity.Realm)
	state.Product = string(identity.Product)
	state.Nickname = strings.TrimSpace(credential.Nickname)
	state.RemainCredit = credits.Remain
	if credits.Checkin != nil {
		state.StreakDays = credits.Checkin.StreakDays
		state.DailyCredit = credits.Checkin.DailyCredit
		state.TodayCredit = credits.Checkin.TodayCredit
	}
	_ = model.SaveWorkBuddyAccountState(state)
}

// countWorkBuddyCredentials counts the non-empty credential lines of a channel
// key, which is how the import and the OAuth panel store several accounts.
func countWorkBuddyCredentials(lines []string) int {
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
