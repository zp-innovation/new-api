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
	agcore "github.com/QuantumNous/new-api/pkg/antigravity"
)

// antigravityRefreshTimeout bounds one upstream token refresh.
const antigravityRefreshTimeout = 15 * time.Second

// AntigravityCredentialRefreshOptions controls a channel credential refresh.
type AntigravityCredentialRefreshOptions struct {
	ResetCaches bool
}

// RefreshAntigravityChannelCredential renews the access token of a stored
// Antigravity channel and writes the merged credential back to the channel key.
// Fields the token endpoint does not return (project_id, email, plan_type) are
// preserved from the stored credential.
func RefreshAntigravityChannelCredential(ctx context.Context, channelID int, opts AntigravityCredentialRefreshOptions) (*agcore.Credential, *model.Channel, error) {
	ch, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, nil, err
	}
	if ch == nil {
		return nil, nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeAntigravity {
		return nil, nil, errors.New("channel type is not Antigravity")
	}

	credential, err := agcore.ParseCredential(strings.TrimSpace(ch.Key))
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(credential.RefreshToken) == "" {
		return nil, nil, errors.New("antigravity channel: refresh_token is required to refresh the credential")
	}

	refreshCtx, cancel := context.WithTimeout(ctx, antigravityRefreshTimeout)
	defer cancel()

	refreshed, err := RefreshAntigravityCredential(refreshCtx, credential.RefreshToken, ch.GetSetting().Proxy)
	if err != nil {
		return nil, nil, err
	}

	credential.AccessToken = refreshed.AccessToken
	credential.RefreshToken = refreshed.RefreshToken
	credential.ExpiresAt = refreshed.ExpiresAt
	credential.LastRefresh = refreshed.LastRefresh
	if strings.TrimSpace(credential.Type) == "" {
		credential.Type = antigravityCredentialType
	}

	encoded, err := common.Marshal(credential)
	if err != nil {
		return nil, nil, err
	}
	// Google rotates the refresh token, so the write-back must not clobber a
	// newer rotation produced by another refresh running against the same row.
	updated, err := model.UpdateChannelKeyIfUnchanged(ch.Id, ch.Key, string(encoded))
	if err != nil {
		return nil, nil, err
	}
	if !updated {
		logger.LogWarn(ctx, common.LogText("antigravity credential refresh: channel_id=%d credential was refreshed by another task, discarding this rotation", ch.Id))
		return nil, nil, model.ErrChannelKeyChanged
	}
	if opts.ResetCaches {
		model.InitChannelCache()
	}
	return credential, ch, nil
}

// AntigravityCredentialNeedsRefresh reports whether the credential expires
// within window. A credential without a parsable expiry is refreshed, because a
// missing deadline cannot be trusted.
func AntigravityCredentialNeedsRefresh(credential *agcore.Credential, now time.Time, window time.Duration) bool {
	if credential == nil {
		return false
	}
	raw := strings.TrimSpace(credential.ExpiresAt)
	if raw == "" {
		return true
	}
	expiresAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return true
	}
	return expiresAt.Sub(now) <= window
}
