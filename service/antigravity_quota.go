package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	agcore "github.com/QuantumNous/new-api/pkg/antigravity"
)

// antigravityQuotaTimeout bounds the upstream model/quota lookup.
const antigravityQuotaTimeout = 30 * time.Second

// antigravityChannelBaseURL resolves the host that serves this channel's relay
// traffic. Catalogue reads must use the same host; a custom channel base URL
// takes precedence over the built-in default.
func antigravityChannelBaseURL(ch *model.Channel) string {
	if ch == nil {
		return ""
	}
	baseURL := strings.TrimSpace(ch.GetBaseURL())
	if baseURL == "" {
		// The quota sync loads a narrow channel projection. Fall back to the
		// Antigravity default even when that projection omitted Type.
		baseURL = constant.GetChannelBaseURL(constant.ChannelTypeAntigravity)
	}
	return strings.TrimRight(baseURL, "/")
}

// AntigravityChannelQuota is the quota snapshot reported for one channel.
type AntigravityChannelQuota struct {
	ChannelID   int                 `json:"channel_id"`
	ChannelName string              `json:"channel_name"`
	Email       string              `json:"email,omitempty"`
	PlanType    string              `json:"plan_type,omitempty"`
	ProjectID   string              `json:"project_id,omitempty"`
	Models      []agcore.ModelQuota `json:"models"`
}

// GetAntigravityChannelQuota reads the models and remaining quota available to
// a stored Antigravity channel.
func GetAntigravityChannelQuota(ctx context.Context, channelID int) (*AntigravityChannelQuota, error) {
	ch, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeAntigravity {
		return nil, errors.New("channel type is not Antigravity")
	}

	credential, err := agcore.ParseCredential(strings.TrimSpace(ch.Key))
	if err != nil {
		return nil, err
	}

	client, err := newAntigravityOAuthClient(ch.GetSetting().Proxy)
	if err != nil {
		return nil, err
	}

	quotaCtx, cancel := context.WithTimeout(ctx, antigravityQuotaTimeout)
	defer cancel()

	response, err := client.FetchAvailableModelsFromURL(
		quotaCtx,
		antigravityChannelBaseURL(ch),
		credential.AccessToken,
		credential.ProjectID,
	)
	if err != nil {
		return nil, err
	}

	return &AntigravityChannelQuota{
		ChannelID:   ch.Id,
		ChannelName: ch.Name,
		Email:       credential.Email,
		PlanType:    credential.PlanType,
		ProjectID:   credential.ProjectID,
		Models:      agcore.QuotaFromModels(response),
	}, nil
}

// GetAntigravityChannelModels lists the model ids the channel account may call.
// It backs the channel drawer's "fetch from upstream" action, which cannot use
// an OpenAI-style /v1/models endpoint because Cloud Code Assist has none.
func GetAntigravityChannelModels(ctx context.Context, ch *model.Channel) ([]string, error) {
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeAntigravity {
		return nil, errors.New("channel type is not Antigravity")
	}

	credential, err := agcore.ParseCredential(strings.TrimSpace(ch.Key))
	if err != nil {
		return nil, err
	}

	// A stored channel renews its own access token first; a preview channel
	// (ch.Id == 0) has nothing to write back and uses the credential as given.
	if ch.Id > 0 && AntigravityCredentialNeedsRefresh(credential, time.Now(), antigravityCredentialRefreshWindow) {
		refreshed, _, err := RefreshAntigravityChannelCredential(ctx, ch.Id, AntigravityCredentialRefreshOptions{})
		if err != nil {
			return nil, err
		}
		credential = refreshed
	}

	client, err := newAntigravityOAuthClient(ch.GetSetting().Proxy)
	if err != nil {
		return nil, err
	}

	listCtx, cancel := context.WithTimeout(ctx, antigravityQuotaTimeout)
	defer cancel()

	response, err := client.FetchAvailableModelsFromURL(
		listCtx,
		antigravityChannelBaseURL(ch),
		credential.AccessToken,
		credential.ProjectID,
	)
	if err != nil {
		return nil, err
	}
	return agcore.ModelNames(response), nil
}
