package workbuddy

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// The CN realm's daily routine endpoints. The sign-in itself lives on the
// billing host while the status query and the growth events live on the chat
// host, which is why the two calls do not share a base URL
// (wb_accounts.py:1657-1666, :1474-1477).
const (
	ReportPath   = "/v2/report"
	EnergyPath   = "/v2/activity/growth/energy"
	StreakPath   = "/activity/growth/streak"
	HeatmapPath  = "/activity/growth/heatmap"
	GrowthTasks  = "/v2/activity/growth/tasks"
	GrowthAccept = "/v2/activity/growth/tasks/accept"
	TravelStatus = "/activity/growth/buddy/travel/status"
	TravelConfig = "/activity/growth/buddy/travel/config"
	TravelDepart = "/activity/growth/buddy/travel/depart"
	TravelClaim  = "/activity/growth/buddy/travel/claim"
	MakeupUse    = "/activity/growth/makeup-cards/use"
	StreakRedeem = "/activity/growth/redeem"
	LotteryQuery = "/activity/growth/lottery/summary"
	LotteryDraw  = "/activity/growth/lottery/draw"
	ClaimGift    = "/billing/meter/claim-gift"
	ClaimComp    = "/billing/meter/claim-compensation"
)

// alreadyCheckedIn is the upstream's "you signed in already today" code. It is
// a success for us: the credit is granted and re-running must not look like a
// failure (wb_accounts.py:1485).
const alreadyCheckedIn = 10001

// CheckinResult is one daily sign-in attempt.
type CheckinResult struct {
	OK        bool   `json:"ok"`
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Duplicate bool   `json:"duplicate"`
}

// Checkin performs the CN realm's daily sign-in. The endpoint is on the billing
// host and takes an empty body (wb_accounts.py:1474-1490).
func (c *Client) Checkin(ctx context.Context, identity Identity) (*CheckinResult, error) {
	_, err := c.callRawURL(ctx, "POST", c.billingBase()+CheckinPath, billingHeaders(identity), map[string]any{})
	if err == nil {
		return &CheckinResult{OK: true, Code: 0, Message: "checked in"}, nil
	}
	var apiErr *APIError
	if ok := asAPIError(err, &apiErr); ok && apiErr.Code == alreadyCheckedIn {
		return &CheckinResult{OK: true, Code: alreadyCheckedIn, Duplicate: true, Message: apiErr.Message}, nil
	}
	return &CheckinResult{OK: false, Message: err.Error()}, err
}

// GrowthEvent is one event of the /v2/report payload. The fields differ per
// event code, so it stays a map instead of a struct with a dozen optional
// members.
type GrowthEvent map[string]any

// ReportEvents posts an event batch to the chat host's /v2/report. The upstream
// answers 200 even when it silently drops the batch (a missing userId does
// that), so the envelope code is the only signal worth checking
// (wb_tasks.py:390-408).
func (c *Client) ReportEvents(ctx context.Context, identity Identity, events []GrowthEvent) error {
	if len(events) == 0 {
		return nil
	}
	_, err := c.callRawURL(ctx, "POST", c.chatBase()+ReportPath, billingHeaders(identity), events)
	return err
}

// The CN activity report mirrors the desktop client's chat_request_send
// payload, which pins the model it claims to have chatted with instead of
// reporting the account's current model. The pair must stay fixed: the event is
// growth-streak bookkeeping, not a real completion. It is deliberately not
// dailyChatModel (dailychat.go:12), which is the model the international
// daily-activity routine actually sends (wb_tasks.py:373-386,
// wb_accounts.py:1494-1496).
const (
	activityReportModelID   = "deepseek-v4-flash"
	activityReportModelName = "DeepSeek V4 Flash"
)

// ChatActivityEvent builds the "chat_request_send" event the CN daily activity
// report sends. The upstream keys the growth streak on it and requires the
// userId, without which the event is discarded silently
// (wb_tasks.py:373-386, wb_accounts.py:1494-1496).
func ChatActivityEvent(uid string, now time.Time) GrowthEvent {
	millis := now.UnixMilli()
	conversation := "wb-daily-" + strconv.FormatInt(millis, 10)
	request := conversation + "-req"
	return GrowthEvent{
		"eventCode":        "chat_request_send",
		"timestamp":        millis,
		"reportDelay":      0,
		"mode":             "craft",
		"conversationId":   conversation,
		"requestId":        request,
		"inputLength":      12,
		"requestModelId":   activityReportModelID,
		"requestModelName": activityReportModelName,
		"isPlan":           false,
		"agentName":        "default",
		"agentType":        "conversation",
		"userId":           uid,
	}
}

// StreakState is the CN growth streak summary the panel renders.
type StreakState struct {
	Days        int
	MakeupCards int
	Tiers       []string
	TierStatus  map[string]string
}

// FetchStreak reads the growth streak, its makeup cards and the tier
// redemption state (wb_tasks.py:141-188).
func (c *Client) FetchStreak(ctx context.Context, identity Identity) (*StreakState, error) {
	var data struct {
		Streak struct {
			Days int `json:"days"`
		} `json:"streak"`
		MakeupCards struct {
			Balance int `json:"balance"`
		} `json:"makeup_cards"`
		Redemption struct {
			Tiers   []map[string]any `json:"tiers"`
			Tier7D  string           `json:"tier_7d_status"`
			Tier14D string           `json:"tier_14d_status"`
			Tier28D string           `json:"tier_28d_status"`
		} `json:"redemption_status"`
	}
	if err := c.getJSON(ctx, c.chatBase()+StreakPath, billingHeaders(identity), &data); err != nil {
		return nil, err
	}
	state := &StreakState{
		Days:        data.Streak.Days,
		MakeupCards: data.MakeupCards.Balance,
		TierStatus: map[string]string{
			"7d":  data.Redemption.Tier7D,
			"14d": data.Redemption.Tier14D,
			"28d": data.Redemption.Tier28D,
		},
	}
	for _, tier := range data.Redemption.Tiers {
		if name, ok := tier["tier"].(string); ok && name != "" {
			state.Tiers = append(state.Tiers, name)
		}
	}
	return state, nil
}

// asAPIError reports whether err is an *APIError and stores it in target. It
// must unwrap: the callers add context with %w, and a bare assertion misses the
// wrapped error (growth.go:276).
func asAPIError(err error, target **APIError) bool {
	return errors.As(err, target)
}
