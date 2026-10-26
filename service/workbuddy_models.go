package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
)

// GetWorkBuddyChannelModels lists the model ids the channel may call. WorkBuddy
// has no OpenAI-style /v1/models endpoint: the catalogue is the product-config
// call the desktop client makes, and the account's product decides which list
// comes back. It backs the channel drawer's "fetch from upstream" action.
func GetWorkBuddyChannelModels(ctx context.Context, ch *model.Channel) ([]string, error) {
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeWorkBuddy {
		return nil, errors.New("channel type is not WorkBuddy")
	}

	// The catalogue is decided by the product rather than by the account, so
	// the first usable credential answers for the whole channel.
	_, _, credential, err := firstWorkBuddyCredential(ch.Key)
	if err != nil {
		return nil, err
	}

	// A stored channel renews its own access token first; a preview channel
	// (ch.Id == 0) has nothing to write back and uses the credential as given.
	if ch.Id > 0 && credential.NeedsRefresh(time.Now(), wbcore.CredentialRefreshWindow) {
		// The token endpoint is reached from the same egress as the rest of the
		// channel, so the refresh has to honour the channel's proxy too.
		refreshed, err := RefreshWorkBuddyChannelCredential(ctx, ch, ch.GetSetting().Proxy)
		if err != nil {
			return nil, err
		}
		credential = refreshed
	}

	identity := credential.Identity()
	// The builder is a variable so tests can aim the catalogue call at a stub.
	client, err := workbuddyOAuthClient(string(identity.Realm), string(identity.Product), ch.GetSetting().Proxy)
	if err != nil {
		return nil, err
	}

	catalog, err := client.FetchCatalog(ctx, identity)
	if err != nil {
		return nil, err
	}
	return catalog.Curated(), nil
}
