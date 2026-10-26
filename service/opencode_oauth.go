package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	occore "github.com/QuantumNous/new-api/pkg/opencode"

	"golang.org/x/sync/singleflight"
)

const (
	// OpenCodeLoginPending means the browser flow is still open.
	OpenCodeLoginPending = "pending"
	// OpenCodeLoginSlowDown asks the panel to increase its poll interval.
	OpenCodeLoginSlowDown = "slow_down"
	// OpenCodeLoginReady carries a freshly minted credential.
	OpenCodeLoginReady = "ready"
	// OpenCodeLoginExpired means the attempt is gone or timed out.
	OpenCodeLoginExpired = "expired"
	// OpenCodeLoginDenied means the user rejected the authorization.
	OpenCodeLoginDenied = "denied"
)

const openCodeOAuthHTTPTimeout = 30 * time.Second

var (
	openCodeLoginSessions = occore.NewSessionStore()
	// openCodeOAuthClient is a variable so tests can point the flow at httptest.
	openCodeOAuthClient = newOpenCodeOAuthClient
	// openCodeChannelRefreshGroup collapses concurrent refreshes of one
	// channel. OpenCode rotates refresh tokens.
	openCodeChannelRefreshGroup singleflight.Group
	openCodeRefreshForce        sync.Map
	errOpenCodeNoRefreshNeeded  = errors.New("opencode: no credential needs refresh")
)

// OpenCodeLoginResult is what the console needs to start a device login.
type OpenCodeLoginResult struct {
	SessionID string `json:"session_id"`
	AuthURL   string `json:"auth_url"`
	UserCode  string `json:"user_code"`
	ExpiresIn int    `json:"expires_in"`
	Interval  int    `json:"interval"`
	Mode      string `json:"mode"`
}

// OpenCodeLoginStatus is one poll result.
type OpenCodeLoginStatus struct {
	Status     string             `json:"status"`
	Interval   int                `json:"interval,omitempty"`
	Credential *occore.Credential `json:"credential,omitempty"`
}

// StartOpenCodeLogin opens a device authorization attempt.
func StartOpenCodeLogin(ctx context.Context, mode, server, proxyURL string) (*OpenCodeLoginResult, error) {
	// Expired sessions are collected here rather than by a background goroutine:
	// the store only grows when an operator starts a login.
	openCodeLoginSessions.Cleanup()

	mode = occore.NormalizeMode(mode)
	client, err := openCodeOAuthClient(mode, server, proxyURL)
	if err != nil {
		return nil, err
	}
	authorization, err := client.StartDeviceAuthorization(ctx)
	if err != nil {
		return nil, err
	}
	sessionID, err := occore.RandomSessionID()
	if err != nil {
		return nil, err
	}
	session := occore.Session{
		DeviceCode: authorization.DeviceCode,
		Server:     client.Server(),
		Mode:       mode,
		Interval:   authorization.Interval,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(time.Duration(authorization.ExpiresIn) * time.Second),
	}
	openCodeLoginSessions.Put(sessionID, session)
	return &OpenCodeLoginResult{
		SessionID: sessionID,
		AuthURL:   authorization.VerificationURIComplete,
		UserCode:  authorization.UserCode,
		ExpiresIn: authorization.ExpiresIn,
		Interval:  authorization.Interval,
		Mode:      mode,
	}, nil
}

// PollOpenCodeLogin performs one poll of an in-flight login.
func PollOpenCodeLogin(ctx context.Context, sessionID, proxyURL string) (*OpenCodeLoginStatus, error) {
	session, ok := openCodeLoginSessions.Get(strings.TrimSpace(sessionID))
	if !ok {
		return &OpenCodeLoginStatus{Status: OpenCodeLoginExpired}, nil
	}
	client, err := openCodeOAuthClient(session.Mode, session.Server, proxyURL)
	if err != nil {
		return nil, err
	}
	token, err := client.PollDeviceToken(ctx, session.DeviceCode)
	switch {
	case errors.Is(err, occore.ErrAuthorizationPending):
		return &OpenCodeLoginStatus{Status: OpenCodeLoginPending, Interval: session.Interval}, nil
	case errors.Is(err, occore.ErrSlowDown):
		session.Interval += occore.SlowDownIncrementSeconds
		openCodeLoginSessions.Put(sessionID, session)
		return &OpenCodeLoginStatus{Status: OpenCodeLoginSlowDown, Interval: session.Interval}, nil
	case errors.Is(err, occore.ErrExpiredToken):
		openCodeLoginSessions.Delete(sessionID)
		return &OpenCodeLoginStatus{Status: OpenCodeLoginExpired}, nil
	case errors.Is(err, occore.ErrAuthorizationDenied):
		openCodeLoginSessions.Delete(sessionID)
		return &OpenCodeLoginStatus{Status: OpenCodeLoginDenied}, nil
	case err != nil:
		return nil, err
	}

	credential, err := client.BuildCredential(ctx, token, session.Mode)
	if err != nil {
		return nil, err
	}
	openCodeLoginSessions.Delete(sessionID)
	return &OpenCodeLoginStatus{Status: OpenCodeLoginReady, Credential: credential}, nil
}

// RefreshOpenCodeCredential exchanges a refresh token for a new access token.
func RefreshOpenCodeCredential(ctx context.Context, rawCredential, proxyURL string) (*occore.Credential, error) {
	credential, err := occore.ParseCredential(rawCredential)
	if err != nil {
		return nil, err
	}
	if credential.IsAPIKey() {
		return nil, errors.New("opencode: API key credentials do not refresh")
	}
	if strings.TrimSpace(credential.RefreshToken) == "" {
		return nil, errors.New("opencode: refresh_token is required")
	}
	client, err := openCodeOAuthClient(credential.ModeOrDefault(), credential.ServerOrDefault(), proxyURL)
	if err != nil {
		return nil, err
	}
	token, err := client.Refresh(ctx, credential.RefreshToken)
	if err != nil {
		return nil, err
	}
	return credential.WithToken(token, time.Now()), nil
}

// RefreshOpenCodeChannelCredential refreshes every due credential in one
// channel and writes the rotated values back line by line.
func RefreshOpenCodeChannelCredential(ctx context.Context, channelID int, force bool) (*occore.Credential, *model.Channel, error) {
	key := strconv.Itoa(channelID)
	if force {
		openCodeRefreshForce.Store(channelID, true)
		defer openCodeRefreshForce.Delete(channelID)
	}
	refresh := func() (refreshResult, error) {
		value, err, _ := openCodeChannelRefreshGroup.Do(key, func() (any, error) {
			ch, loadErr := model.GetChannelById(channelID, true)
			if loadErr != nil {
				return nil, loadErr
			}
			if ch == nil {
				return nil, errors.New("channel not found")
			}
			if ch.Type != constant.ChannelTypeOpenCode {
				return nil, errors.New("channel type is not OpenCode")
			}
			forced := force
			if _, ok := openCodeRefreshForce.Load(channelID); ok {
				forced = true
			}
			credential, refreshErr := refreshOpenCodeChannelCredential(ctx, ch, forced)
			if refreshErr != nil {
				return nil, refreshErr
			}
			return refreshResult{credential: credential, channel: ch}, nil
		})
		if err != nil {
			return refreshResult{}, err
		}
		result, ok := value.(refreshResult)
		if !ok {
			return refreshResult{}, errors.New("opencode: refresh returned an unexpected result")
		}
		return result, nil
	}
	result, err := refresh()
	if force && errors.Is(err, errOpenCodeNoRefreshNeeded) {
		// A background refresh may have joined the flight first and skipped a
		// credential that then became due. Retry once with force set.
		result, err = refresh()
	}
	if err != nil {
		return nil, nil, err
	}
	return result.credential, result.channel, nil
}

type refreshResult struct {
	credential *occore.Credential
	channel    *model.Channel
}

func refreshOpenCodeChannelCredential(ctx context.Context, ch *model.Channel, force bool) (*occore.Credential, error) {
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	lines := strings.Split(ch.Key, "\n")
	now := time.Now()
	var first *occore.Credential
	var firstRefreshed *occore.Credential
	var firstErr error
	changed := false
	refreshed := 0
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		credential, err := occore.ParseCredential(trimmed)
		if err != nil {
			continue
		}
		if credential.IsAPIKey() {
			if first == nil {
				first = credential
			}
			continue
		}
		if first == nil {
			first = credential
		}
		if !force && !credential.NeedsRefresh(now, occore.CredentialRefreshWindow) {
			continue
		}
		updated, err := RefreshOpenCodeCredential(ctx, trimmed, ch.GetSetting().Proxy)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		encoded, err := updated.Marshal()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		lines[index] = encoded
		if firstRefreshed == nil {
			firstRefreshed = updated
		}
		changed = true
		refreshed++
	}
	if !changed {
		if firstErr != nil {
			return nil, firstErr
		}
		if first == nil {
			return nil, errors.New("opencode: the channel has no credential")
		}
		if !force {
			return nil, errOpenCodeNoRefreshNeeded
		}
		return first, nil
	}
	if ch.Id > 0 {
		// The upstream rotates refresh tokens; a lost update here would replace
		// the newer credential with this older one.
		updated, err := model.UpdateChannelKeyIfUnchanged(ch.Id, ch.Key, strings.Join(lines, "\n"))
		if err != nil {
			return nil, err
		}
		if !updated {
			logger.LogWarn(ctx, common.LogText("opencode credential refresh: channel_id=%d credential was refreshed by another task, discarding this rotation", ch.Id))
			return nil, model.ErrChannelKeyChanged
		}
		model.InitChannelCache()
	}
	logger.LogInfo(ctx, common.LogText("opencode credential refreshed: channel_id=%d keys=%d", ch.Id, refreshed))
	if firstRefreshed != nil {
		return firstRefreshed, nil
	}
	return first, nil
}

// newOpenCodeOAuthClient builds a protocol client that honours the channel proxy.
func newOpenCodeOAuthClient(mode, server, proxyURL string) (*occore.Client, error) {
	baseClient, err := GetHttpClientWithProxy(strings.TrimSpace(proxyURL))
	if err != nil {
		return nil, err
	}
	return occore.NewClientWithError(oauthHTTPClient(baseClient, openCodeOAuthHTTPTimeout), server)
}

// OpenCodeChannelCredential returns the first credential in a channel key.
func OpenCodeChannelCredential(ch *model.Channel) (*occore.Credential, error) {
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	for _, line := range strings.Split(ch.Key, "\n") {
		credential, err := occore.ParseCredential(strings.TrimSpace(line))
		if err == nil {
			return credential, nil
		}
	}
	return nil, errors.New("opencode: the channel has no credential")
}
