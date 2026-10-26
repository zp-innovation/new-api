package service

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	occore "github.com/QuantumNous/new-api/pkg/opencode"
)

// OpenCodeUsageEntry is one account's sanitized Go usage.
type OpenCodeUsageEntry struct {
	AccountID string             `json:"account_id,omitempty"`
	Email     string             `json:"email,omitempty"`
	OrgID     string             `json:"org_id,omitempty"`
	OrgName   string             `json:"org_name,omitempty"`
	Rolling   occore.UsageWindow `json:"rolling"`
	Weekly    occore.UsageWindow `json:"weekly"`
	Monthly   occore.UsageWindow `json:"monthly"`
}

// OpenCodeChannelUsage is the per-account Go usage view.
type OpenCodeChannelUsage struct {
	Mode    string               `json:"mode"`
	Entries []OpenCodeUsageEntry `json:"entries"`
	// SkippedModes names the credential modes that were not read because they
	// have no usage endpoint. A channel may mix modes, so they are reported here
	// instead of failing the whole view.
	SkippedModes []string `json:"skipped_modes,omitempty"`
}

// GetOpenCodeChannelUsage reads the Go usage endpoint for every credential in a
// channel. Zen and unknown credential modes have no equivalent endpoint, so a
// channel that mixes modes still reports the Go accounts it could read.
func GetOpenCodeChannelUsage(ctx context.Context, channelID int) (*OpenCodeChannelUsage, error) {
	ch, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeOpenCode {
		return nil, errors.New("channel type is not OpenCode")
	}
	result := &OpenCodeChannelUsage{}
	var firstErr error
	credentialCount := 0
	for _, line := range strings.Split(ch.Key, "\n") {
		credential, err := occore.ParseCredential(strings.TrimSpace(line))
		if err != nil {
			continue
		}
		credentialCount++
		mode := openCodeCredentialMode(ch, credential)
		if mode != occore.ModeGo {
			// Only Go subscriptions expose usage. Skip this credential and keep
			// reading the rest, so one Zen credential cannot hide the Go ones.
			label := mode
			if raw := strings.TrimSpace(credential.Mode); raw != "" {
				label = raw
			}
			if !slices.Contains(result.SkippedModes, label) {
				result.SkippedModes = append(result.SkippedModes, label)
			}
			continue
		}
		client, err := openCodeOAuthClient(mode, credential.ServerOrDefault(), ch.GetSetting().Proxy)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		usage, err := client.FetchUsage(ctx, credential, openCodeChannelBaseURL(ch, credential, occore.ProtocolChatCompletions))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		result.Mode = mode
		result.Entries = append(result.Entries, OpenCodeUsageEntry{
			AccountID: credential.AccountID,
			Email:     credential.Email,
			OrgID:     credential.OrgID,
			OrgName:   credential.OrgName,
			Rolling:   usage.Rolling,
			Weekly:    usage.Weekly,
			Monthly:   usage.Monthly,
		})
	}
	if len(result.Entries) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		if credentialCount == 0 {
			return nil, errors.New("channel has no OpenCode credential")
		}
		// Every credential uses a mode without a usage endpoint.
		return nil, occore.ErrUsageUnavailable
	}
	if len(result.SkippedModes) > 0 {
		logger.LogInfo(ctx, common.LogText("opencode usage: channel_id=%d skipped credential modes=%v", channelID, result.SkippedModes))
	}
	return result, nil
}

// GetOpenCodeChannelModelsForChannel reads the upstream model catalog from an
// already-loaded channel.
func GetOpenCodeChannelModelsForChannel(ctx context.Context, ch *model.Channel) ([]string, error) {
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeOpenCode {
		return nil, errors.New("channel type is not OpenCode")
	}
	credential, err := OpenCodeChannelCredential(ch)
	if err != nil {
		return append([]string(nil), occore.ModelList...), nil
	}
	client, err := openCodeOAuthClient(credential.ModeOrDefault(), credential.ServerOrDefault(), ch.GetSetting().Proxy)
	if err != nil {
		return append([]string(nil), occore.ModelList...), nil
	}
	models, err := client.FetchModels(ctx, credential, openCodeChannelBaseURL(ch, credential, occore.ProtocolChatCompletions))
	if err != nil || len(models) == 0 {
		return append([]string(nil), occore.ModelList...), nil
	}
	return models, nil
}

func openCodeChannelBaseURL(ch *model.Channel, credential *occore.Credential, protocol string) string {
	if ch != nil && ch.BaseURL != nil {
		custom := strings.TrimSpace(*ch.BaseURL)
		defaultBase := constant.GetChannelBaseURL(constant.ChannelTypeOpenCode)
		if custom != "" && custom != defaultBase {
			base := strings.TrimRight(custom, "/")
			if protocol == occore.ProtocolAnthropic {
				return strings.TrimSuffix(base, "/v1")
			}
			if !strings.HasSuffix(base, "/v1") {
				base += "/v1"
			}
			return base
		}
	}
	return occore.ProtocolBaseURL(openCodeCredentialMode(ch, credential), protocol)
}

func openCodeCredentialMode(ch *model.Channel, credential *occore.Credential) string {
	if credential != nil && strings.TrimSpace(credential.Mode) != "" {
		return occore.NormalizeMode(credential.Mode)
	}
	if ch != nil && strings.Contains(strings.ToLower(ch.GetBaseURL()), "/zen/go") {
		return occore.ModeGo
	}
	return occore.ModeZen
}
