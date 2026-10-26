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
	ccore "github.com/QuantumNous/new-api/pkg/commandcode"

	"golang.org/x/sync/singleflight"
)

const commandCodeOAuthHTTPTimeout = 30 * time.Second

// Command Code login statuses the console understands.
const (
	CommandCodeLoginPending = "pending"
	CommandCodeLoginReady   = "ready"
	CommandCodeLoginExpired = "expired"
	CommandCodeLoginDenied  = "denied"
	CommandCodeLoginError   = "error"
)

type commandCodeLoginSession struct {
	State      string
	Callback   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Credential *ccore.Credential
	Error      string
	Denied     bool
}

type commandCodeLoginStore struct {
	mu       sync.Mutex
	sessions map[string]*commandCodeLoginSession
}

func newCommandCodeLoginStore() *commandCodeLoginStore {
	return &commandCodeLoginStore{sessions: make(map[string]*commandCodeLoginSession)}
}

func (s *commandCodeLoginStore) put(session *commandCodeLoginSession) {
	if session == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())
	s.sessions[session.State] = session
}

func (s *commandCodeLoginStore) update(state string, update func(*commandCodeLoginSession)) bool {
	state = strings.TrimSpace(state)
	if state == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())
	session, ok := s.sessions[state]
	if !ok {
		return false
	}
	update(session)
	return true
}

// consumeReady returns a completed or failed session and removes it, leaving a
// still-pending session in place. This makes a completed handoff single-use
// even if two polls race.
func (s *commandCodeLoginStore) consumeReady(state string) (*commandCodeLoginSession, bool) {
	state = strings.TrimSpace(state)
	if state == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())
	session, ok := s.sessions[state]
	if !ok {
		return nil, false
	}
	if session.Credential == nil && session.Error == "" && !session.Denied {
		copy := *session
		return &copy, true
	}
	copy := *session
	delete(s.sessions, state)
	return &copy, true
}

func (s *commandCodeLoginStore) sweepLocked(now time.Time) {
	for state, session := range s.sessions {
		if session == nil || !now.Before(session.ExpiresAt) {
			delete(s.sessions, state)
		}
	}
}

var (
	commandCodeLoginSessions = newCommandCodeLoginStore()
	commandCodeChannelGroup  singleflight.Group
	// commandCodeClientFactory is a variable so tests can point the flow at an
	// httptest server.
	commandCodeClientFactory = newCommandCodeClient
)

// CommandCodeLoginResult is what the console needs to start a browser handoff.
type CommandCodeLoginResult struct {
	SessionID string `json:"session_id"`
	State     string `json:"state"`
	AuthURL   string `json:"auth_url"`
	Callback  string `json:"callback_url"`
	ExpiresIn int    `json:"expires_in"`
}

// CommandCodeLoginStatus is one poll's answer.
type CommandCodeLoginStatus struct {
	Status     string            `json:"status"`
	Credential *ccore.Credential `json:"credential,omitempty"`
}

// StartCommandCodeLogin creates a pending Studio handoff. The callback URL must
// point back at the console; only a loopback console can complete the flow.
func StartCommandCodeLogin(callbackURL, proxyURL string) (*CommandCodeLoginResult, error) {
	callbackURL = strings.TrimSpace(callbackURL)
	if callbackURL == "" {
		return nil, errors.New("command_code: callback_url is required")
	}
	state, err := ccore.GenerateState()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	commandCodeLoginSessions.put(&commandCodeLoginSession{
		State:     state,
		Callback:  callbackURL,
		CreatedAt: now,
		ExpiresAt: now.Add(ccore.OAuthStateTTL),
	})
	return &CommandCodeLoginResult{
		SessionID: state,
		State:     state,
		AuthURL:   ccore.BuildAuthURL(callbackURL, state, ""),
		Callback:  callbackURL,
		ExpiresIn: int(ccore.OAuthStateTTL / time.Second),
	}, nil
}

// CompleteCommandCodeLogin stores the API key Studio handed back to the
// callback. The credential is not consumed until the console polls, so a
// dropped response can be retried by the browser.
func CompleteCommandCodeLogin(state, apiKey, userID, userName, keyName string) error {
	credential, err := ccore.ParseCredential(apiKey)
	if err != nil {
		return err
	}
	if value := strings.TrimSpace(userID); value != "" {
		credential.UserID = value
	}
	if value := strings.TrimSpace(userName); value != "" {
		credential.UserName = value
	}
	if value := strings.TrimSpace(keyName); value != "" {
		credential.KeyName = value
	}
	if err := credential.ValidateForStorage(); err != nil {
		return err
	}
	if !commandCodeLoginSessions.update(state, func(session *commandCodeLoginSession) {
		if session.Credential == nil {
			session.Credential = credential
		}
	}) {
		return errors.New("command_code: login session not found")
	}
	return nil
}

// FailCommandCodeLogin records a denial or error from the browser handoff.
func FailCommandCodeLogin(state, errorCode, description string) error {
	errorCode = strings.TrimSpace(errorCode)
	description = strings.TrimSpace(description)
	if errorCode == "" {
		errorCode = "error"
	}
	if !commandCodeLoginSessions.update(state, func(session *commandCodeLoginSession) {
		session.Error = description
		session.Denied = errorCode == "access_denied"
	}) {
		return errors.New("command_code: login session not found")
	}
	return nil
}

// PollCommandCodeLogin validates the handed-back key and returns the enriched
// credential once. A completed login is single use.
func PollCommandCodeLogin(ctx context.Context, sessionID, proxyURL string) (*CommandCodeLoginStatus, error) {
	session, ok := commandCodeLoginSessions.consumeReady(sessionID)
	if !ok {
		return &CommandCodeLoginStatus{Status: CommandCodeLoginExpired}, nil
	}
	if session.Error != "" || session.Denied {
		if session.Denied {
			return &CommandCodeLoginStatus{Status: CommandCodeLoginDenied}, nil
		}
		return &CommandCodeLoginStatus{Status: CommandCodeLoginError}, nil
	}
	if session.Credential == nil {
		return &CommandCodeLoginStatus{Status: CommandCodeLoginPending}, nil
	}
	credential, err := ValidateCommandCodeCredential(ctx, session.Credential, proxyURL)
	if err != nil {
		return nil, err
	}
	return &CommandCodeLoginStatus{Status: CommandCodeLoginReady, Credential: credential}, nil
}

// ValidateCommandCodeCredential checks the API key against /alpha/whoami and
// fills in the account metadata the console displays.
func ValidateCommandCodeCredential(ctx context.Context, credential *ccore.Credential, proxyURL string) (*ccore.Credential, error) {
	if credential == nil {
		return nil, errors.New("command_code: credential is nil")
	}
	client, err := commandCodeClientFactory("", "", proxyURL)
	if err != nil {
		return nil, err
	}
	whoami, err := client.ValidateAPIKey(ctx, credential.APIKey)
	if err != nil {
		return nil, err
	}
	validated := *credential
	if whoami != nil && whoami.User != nil {
		if validated.UserID == "" {
			validated.UserID = strings.TrimSpace(whoami.User.ID)
		}
		if validated.UserName == "" {
			validated.UserName = strings.TrimSpace(whoami.User.UserName)
			if validated.UserName == "" {
				validated.UserName = strings.TrimSpace(whoami.User.Name)
			}
		}
	}
	if whoami != nil && whoami.Org != nil {
		validated.OrgID = strings.TrimSpace(whoami.Org.ID)
		validated.OrgName = strings.TrimSpace(whoami.Org.Name)
		if validated.OrgName == "" {
			validated.OrgName = strings.TrimSpace(whoami.Org.Login)
		}
	}
	if validated.AuthenticatedAt == "" {
		validated.AuthenticatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return &validated, nil
}

// CommandCodeChannelCredential returns the first credential in a channel key.
func CommandCodeChannelCredential(ch *model.Channel) (*ccore.Credential, error) {
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	for _, line := range strings.Split(ch.Key, "\n") {
		credential, err := ccore.ParseCredential(strings.TrimSpace(line))
		if err == nil {
			return credential, nil
		}
	}
	return nil, errors.New("command_code: the channel has no credential")
}

// CommandCodeChannelUsageEntry is one account's sanitized usage view.
type CommandCodeChannelUsageEntry struct {
	UserID           string             `json:"user_id,omitempty"`
	UserName         string             `json:"user_name,omitempty"`
	OrgID            string             `json:"org_id,omitempty"`
	OrgName          string             `json:"org_name,omitempty"`
	PlanID           string             `json:"plan_id,omitempty"`
	PlanName         string             `json:"plan_name,omitempty"`
	MonthlyCredits   float64            `json:"monthly_credits"`
	PurchasedCredits float64            `json:"purchased_credits"`
	FreeCredits      float64            `json:"free_credits"`
	MonthlyUsed      float64            `json:"monthly_used"`
	MonthlyCap       float64            `json:"monthly_cap"`
	FiveHour         *ccore.WindowUsage `json:"five_hour,omitempty"`
	Weekly           *ccore.WindowUsage `json:"weekly,omitempty"`
	PeriodStart      string             `json:"period_start,omitempty"`
	PeriodEnd        string             `json:"period_end,omitempty"`
}

// CommandCodeChannelUsage is the per-account usage view.
type CommandCodeChannelUsage struct {
	Entries []CommandCodeChannelUsageEntry `json:"entries"`
}

// GetCommandCodeChannelUsage reads the credits view for every credential in a
// channel. The management API lives on the official host; a custom relay base
// has no equivalent endpoint.
func GetCommandCodeChannelUsage(ctx context.Context, channelID int) (*CommandCodeChannelUsage, error) {
	ch, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeCommandCode {
		return nil, errors.New("channel type is not Command Code")
	}
	if !commandCodeChannelUsesOfficialHost(ch) {
		return nil, errors.New("command_code: usage is only available for the official host")
	}
	result := &CommandCodeChannelUsage{}
	var firstErr error
	for _, line := range strings.Split(ch.Key, "\n") {
		credential, err := ccore.ParseCredential(strings.TrimSpace(line))
		if err != nil {
			continue
		}
		client, err := commandCodeClientFactory("", "", ch.GetSetting().Proxy)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		usage, err := client.FetchUsage(ctx, credential.APIKey)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		result.Entries = append(result.Entries, CommandCodeChannelUsageEntry{
			UserID:           usage.UserID,
			UserName:         usage.UserName,
			OrgID:            usage.OrgID,
			OrgName:          usage.OrgName,
			PlanID:           usage.PlanID,
			PlanName:         usage.PlanName,
			MonthlyCredits:   usage.MonthlyCredits,
			PurchasedCredits: usage.PurchasedCredits,
			FreeCredits:      usage.FreeCredits,
			MonthlyUsed:      usage.MonthlyUsed,
			MonthlyCap:       usage.MonthlyCap,
			FiveHour:         usage.FiveHour,
			Weekly:           usage.Weekly,
			PeriodStart:      usage.PeriodStart,
			PeriodEnd:        usage.PeriodEnd,
		})
	}
	if len(result.Entries) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return result, nil
}

// GetCommandCodeChannelModelsForChannel reads the upstream catalog from an
// already-loaded channel.
func GetCommandCodeChannelModelsForChannel(ctx context.Context, ch *model.Channel) ([]string, error) {
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeCommandCode {
		return nil, errors.New("channel type is not Command Code")
	}
	credential, err := CommandCodeChannelCredential(ch)
	if err != nil {
		return append([]string(nil), ccore.DefaultModelIDs()...), nil
	}
	client, err := commandCodeClientFactory(commandCodeProviderBaseURL(ch), "", ch.GetSetting().Proxy)
	if err != nil {
		return append([]string(nil), ccore.DefaultModelIDs()...), nil
	}
	models, err := client.FetchModels(ctx, credential.APIKey)
	if err != nil || len(models) == 0 {
		return append([]string(nil), ccore.DefaultModelIDs()...), nil
	}
	ccore.UpdateCatalog(models)
	ids := make([]string, 0, len(models))
	for _, item := range models {
		if id := strings.TrimSpace(item.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return append([]string(nil), ccore.DefaultModelIDs()...), nil
	}
	return ids, nil
}

// ValidateCommandCodeChannelCredential re-checks the stored API key and writes
// the refreshed account metadata back to the line it came from.
func ValidateCommandCodeChannelCredential(ctx context.Context, ch *model.Channel, proxyURL string) (*ccore.Credential, error) {
	if ch == nil {
		return nil, errors.New("channel not found")
	}
	if ch.Type != constant.ChannelTypeCommandCode {
		return nil, errors.New("channel type is not Command Code")
	}
	if ch.Id <= 0 {
		credential, err := CommandCodeChannelCredential(ch)
		if err != nil {
			return nil, err
		}
		return ValidateCommandCodeCredential(ctx, credential, proxyURL)
	}
	value, err, _ := commandCodeChannelGroup.Do(strconv.Itoa(ch.Id), func() (any, error) {
		fresh := ch
		if loaded, loadErr := model.GetChannelById(ch.Id, true); loadErr == nil && loaded != nil {
			fresh = loaded
		}
		return validateCommandCodeChannelCredential(ctx, fresh, proxyURL)
	})
	if err != nil {
		return nil, err
	}
	credential, ok := value.(*ccore.Credential)
	if !ok || credential == nil {
		return nil, errors.New("command_code: credential validation returned no credential")
	}
	return credential, nil
}

func validateCommandCodeChannelCredential(ctx context.Context, ch *model.Channel, proxyURL string) (*ccore.Credential, error) {
	lines := strings.Split(ch.Key, "\n")
	index := -1
	var credential *ccore.Credential
	for i, line := range lines {
		parsed, err := ccore.ParseCredential(strings.TrimSpace(line))
		if err != nil {
			continue
		}
		index = i
		credential = parsed
		break
	}
	if credential == nil {
		return nil, errors.New("command_code: the channel has no credential")
	}
	validated, err := ValidateCommandCodeCredential(ctx, credential, proxyURL)
	if err != nil {
		return nil, err
	}
	encoded, err := validated.Marshal()
	if err != nil {
		return nil, err
	}
	lines[index] = encoded
	updated, err := model.UpdateChannelKeyIfUnchanged(ch.Id, ch.Key, strings.Join(lines, "\n"))
	if err != nil {
		return nil, err
	}
	if !updated {
		logger.LogWarn(ctx, common.LogText("command_code credential validation: channel_id=%d credential was refreshed by another task, discarding this write", ch.Id))
		return nil, model.ErrChannelKeyChanged
	}
	model.InitChannelCache()
	return validated, nil
}

func commandCodeProviderBaseURL(ch *model.Channel) string {
	if ch != nil && ch.BaseURL != nil {
		custom := strings.TrimSpace(*ch.BaseURL)
		defaultBase := constant.GetChannelBaseURL(constant.ChannelTypeCommandCode)
		if custom != "" && custom != defaultBase {
			return custom
		}
	}
	return ccore.DefaultProviderBaseURL
}

func commandCodeChannelUsesOfficialHost(ch *model.Channel) bool {
	baseURL := strings.ToLower(commandCodeProviderBaseURL(ch))
	return strings.Contains(baseURL, "api.commandcode.ai")
}

// newCommandCodeClient builds a protocol client that honours the channel proxy.
func newCommandCodeClient(providerBaseURL, apiBaseURL, proxyURL string) (*ccore.Client, error) {
	baseClient, err := GetHttpClientWithProxy(strings.TrimSpace(proxyURL))
	if err != nil {
		return nil, err
	}
	return ccore.NewClient(oauthHTTPClient(baseClient, commandCodeOAuthHTTPTimeout), apiBaseURL, providerBaseURL), nil
}
