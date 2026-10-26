package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"

	"golang.org/x/sync/singleflight"
)

const workbuddyOAuthHTTPTimeout = 30 * time.Second

// Login statuses the console understands. "pending" means the operator has not
// finished signing in yet and is not an error: the upstream answers the poll
// with the business code 11217 until the browser flow completes.
const (
	WorkBuddyLoginPending = "pending"
	WorkBuddyLoginReady   = "ready"
	WorkBuddyLoginExpired = "expired"
)

// workbuddyLoginSessions holds in-flight device-authorization logins. The
// upstream issues nothing before the operator finishes in the browser, so a
// session is only bookkeeping: the state value, the realm and the product. It
// is addressed by the state, which the panel keeps and never has to display.
var workbuddyLoginSessions = wbcore.NewLoginStore()

// workbuddyOAuthClient builds the protocol client a login or refresh call uses.
// It is a variable so tests can aim the flow at an httptest server.
var workbuddyOAuthClient = newWorkBuddyOAuthClient

// WorkBuddyLoginResult is what the console needs to start a device login: the
// authorization URL to open and the state that identifies the attempt.
type WorkBuddyLoginResult struct {
	State     string `json:"state"`
	AuthURL   string `json:"auth_url"`
	Realm     string `json:"realm"`
	Product   string `json:"product"`
	ExpiresIn int    `json:"expires_in"`
}

// WorkBuddyLoginStatus is one poll's answer. Credential is set only once the
// upstream issued a token.
type WorkBuddyLoginStatus struct {
	Status     string             `json:"status"`
	Credential *wbcore.Credential `json:"credential,omitempty"`
}

// StartWorkBuddyLogin asks the upstream for a device-authorization state and
// returns the URL the operator must open. The platform value the upstream
// expects is the one the reference gateway sends (CLI); the product only
// decides the outbound identity of later relay traffic, never the token.
func StartWorkBuddyLogin(ctx context.Context, realm, product, proxyURL string) (*WorkBuddyLoginResult, error) {
	// Expired sessions are collected here rather than by a background goroutine:
	// the store only grows when an operator starts a login.
	workbuddyLoginSessions.Cleanup()

	client, err := workbuddyOAuthClient(realm, product, proxyURL)
	if err != nil {
		return nil, err
	}
	session, err := client.StartLogin(ctx, "")
	if err != nil {
		return nil, err
	}
	workbuddyLoginSessions.Put(*session)
	return &WorkBuddyLoginResult{
		State:     session.State,
		AuthURL:   session.AuthURL,
		Realm:     string(session.Realm),
		Product:   string(session.Product),
		ExpiresIn: wbcore.LoginTTLSeconds,
	}, nil
}

// PollWorkBuddyLogin performs one poll of an in-flight login. It returns
// "pending" while the browser flow is unfinished, "expired" when the session is
// gone or past its window, and the freshly minted credential once the upstream
// issued a token. A completed login is single use: the state is dropped as soon
// as it yields a credential, so a replay cannot mint a second one.
func PollWorkBuddyLogin(ctx context.Context, state, proxyURL string) (*WorkBuddyLoginStatus, error) {
	session, ok := workbuddyLoginSessions.Get(strings.TrimSpace(state))
	if !ok {
		return &WorkBuddyLoginStatus{Status: WorkBuddyLoginExpired}, nil
	}

	client, err := workbuddyOAuthClient(string(session.Realm), string(session.Product), proxyURL)
	if err != nil {
		return nil, err
	}
	tokens, err := client.PollLogin(ctx, session.State)
	if errors.Is(err, wbcore.ErrLoginPending) {
		return &WorkBuddyLoginStatus{Status: WorkBuddyLoginPending}, nil
	}
	if err != nil {
		return nil, err
	}

	credential := (&wbcore.Credential{
		Type:    wbcore.CredentialType,
		Realm:   string(session.Realm),
		Product: string(session.Product),
	}).WithTokens(tokens, time.Now())
	// A credential without a refresh token dies at the first expiry and cannot
	// be renewed: the upstream rotates refresh tokens, so this is a hard
	// requirement rather than a nicety.
	if err := credential.ValidateForStorage(); err != nil {
		return nil, err
	}
	workbuddyLoginSessions.Delete(session.State)

	// The nickname is decoration: a failed lookup must not cost the operator
	// the credential they just authorized.
	if nickname, err := client.FetchProfile(ctx, session.State, credential.AccessToken); err != nil {
		logger.LogWarn(ctx, common.LogText("workbuddy oauth: fetch profile failed: %v", err))
	} else if strings.TrimSpace(nickname) != "" {
		credential.Nickname = nickname
	}
	return &WorkBuddyLoginStatus{Status: WorkBuddyLoginReady, Credential: credential}, nil
}

// RefreshWorkBuddyCredential exchanges the refresh token of a stored credential
// for a fresh access token. The refresh token is rotated upstream, so the
// caller must persist the returned credential: keeping the old one would leave
// a token the upstream has already consumed.
func RefreshWorkBuddyCredential(ctx context.Context, rawCredential, proxyURL string) (*wbcore.Credential, error) {
	credential, err := wbcore.ParseCredential(rawCredential)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(credential.RefreshToken) == "" {
		return nil, errors.New("workbuddy: refresh token is required")
	}

	client, err := workbuddyOAuthClient(credential.Realm, credential.Product, proxyURL)
	if err != nil {
		return nil, err
	}
	tokens, err := client.Refresh(ctx, wbcore.RefreshRequest{
		UID:          credential.UID,
		RefreshToken: credential.RefreshToken,
		Domain:       credential.Domain,
		EnterpriseID: credential.EnterpriseID,
	})
	if err != nil {
		return nil, err
	}
	return credential.WithTokens(tokens, time.Now()), nil
}

// newWorkBuddyOAuthClient builds a protocol client that honours the channel
// proxy. The management endpoints live on the realm's chat upstream, which is
// what an empty base URL resolves to.
func newWorkBuddyOAuthClient(realm, product, proxyURL string) (*wbcore.Client, error) {
	normalizedRealm := wbcore.NormalizeRealm(realm)
	normalizedProduct := wbcore.NormalizeProduct(product)

	baseClient, err := GetHttpClientWithProxy(strings.TrimSpace(proxyURL))
	if err != nil {
		return nil, err
	}
	if baseClient == nil {
		return wbcore.NewClient(nil, "", normalizedRealm, normalizedProduct), nil
	}
	return wbcore.NewClient(oauthHTTPClient(baseClient, workbuddyOAuthHTTPTimeout), "", normalizedRealm, normalizedProduct), nil
}

// firstWorkBuddyCredential reads the first line of a channel key that parses as
// a WorkBuddy credential. A multi-account channel keeps one compact credential
// per line, so the line index travels back with the credential: a rotated token
// has to be written to the line it came from, leaving its neighbours alone.
func firstWorkBuddyCredential(rawKey string) ([]string, int, *wbcore.Credential, error) {
	lines := strings.Split(rawKey, "\n")
	var parseErr error
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		credential, err := wbcore.ParseCredential(trimmed)
		if err != nil {
			if parseErr == nil {
				parseErr = err
			}
			continue
		}
		lines[index] = trimmed
		return lines, index, credential, nil
	}
	if parseErr != nil {
		return nil, -1, nil, parseErr
	}
	return nil, -1, nil, errors.New("workbuddy: the channel has no credential")
}

// workBuddyChannelRefreshGroup collapses concurrent refreshes of one channel.
// The upstream rotates refresh tokens, so two concurrent calls that read the
// same stored token would make one of them spend an already-consumed value.
var workBuddyChannelRefreshGroup singleflight.Group

// RefreshWorkBuddyChannelCredential renews the access token of a stored channel
// and writes the rotated credential back to the line it came from. The upstream
// rotates refresh tokens, so the answer must replace the stored value or the
// account is lost at the next expiry.
func RefreshWorkBuddyChannelCredential(ctx context.Context, ch *model.Channel, proxyURL string) (*wbcore.Credential, error) {
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeWorkBuddy {
		return nil, errors.New("channel type is not WorkBuddy")
	}
	if ch.Id <= 0 {
		// A preview channel has no row to write back; the caller uses the value.
		return refreshWorkBuddyChannelCredential(ctx, ch, proxyURL)
	}

	value, err, _ := workBuddyChannelRefreshGroup.Do(strconv.Itoa(ch.Id), func() (any, error) {
		// Re-read the row inside the single flight so every waiter refreshes
		// from the persisted key instead of a possibly stale cache copy.
		fresh := ch
		if loaded, loadErr := model.GetChannelById(ch.Id, true); loadErr == nil && loaded != nil {
			fresh = loaded
		}
		return refreshWorkBuddyChannelCredential(ctx, fresh, proxyURL)
	})
	if err != nil {
		return nil, err
	}
	credential, ok := value.(*wbcore.Credential)
	if !ok || credential == nil {
		return nil, errors.New("workbuddy: credential refresh returned no credential")
	}
	return credential, nil
}

func refreshWorkBuddyChannelCredential(ctx context.Context, ch *model.Channel, proxyURL string) (*wbcore.Credential, error) {
	lines, index, _, err := firstWorkBuddyCredential(ch.Key)
	if err != nil {
		return nil, err
	}

	refreshed, err := RefreshWorkBuddyCredential(ctx, lines[index], proxyURL)
	if err != nil {
		return nil, err
	}
	if ch.Id <= 0 {
		return refreshed, nil
	}

	encoded, err := refreshed.Marshal()
	if err != nil {
		return nil, err
	}
	lines[index] = encoded
	// WorkBuddy rotates refresh tokens on every refresh, so a blind write can
	// destroy the rotation another refresh already persisted.
	updated, err := model.UpdateChannelKeyIfUnchanged(ch.Id, ch.Key, strings.Join(lines, "\n"))
	if err != nil {
		return nil, err
	}
	if !updated {
		logger.LogWarn(ctx, common.LogText("workbuddy credential refresh: channel_id=%d credential was refreshed by another task, discarding this rotation", ch.Id))
		return nil, model.ErrChannelKeyChanged
	}
	// The relay resolves channels from the in-memory cache, so the rotated
	// credential has to be republished; otherwise requests keep the consumed
	// refresh token until the periodic channel sync runs.
	model.InitChannelCache()
	return refreshed, nil
}
