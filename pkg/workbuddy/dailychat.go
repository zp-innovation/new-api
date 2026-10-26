package workbuddy

import (
	"context"
	"time"
)

// The international realm has no sign-in endpoint: its daily credit comes from
// the daily-activity routine, which the reference gateway drives as two legs
// (wb_accounts.py:38-47, :1389-1450).
const (
	dailyChatModel  = "deepseek-v4.1-flash"
	dailyChatPrompt = "Hi"
	webUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"
	// WebTurnTimeout bounds one web-channel agent conversation. The upstream
	// needs the client to connect to the sandbox and request the turn, so a
	// plain session create never completes on its own.
	WebTurnTimeout = 120 * time.Second
)

// DailyChatResult reports one daily-activity attempt. The two legs are kept
// apart because only the web leg is credited: a caller must not mark the day
// done when the desktop leg succeeded and the web leg failed
// (wb_accounts.py:1428-1444).
type DailyChatResult struct {
	OK           bool   `json:"ok"`
	Message      string `json:"message"`
	WebAttempted bool   `json:"web_attempted"`
	WebOK        bool   `json:"web_ok"`
	WebStatus    string `json:"web_status,omitempty"`
	WebChunks    int    `json:"web_chunks,omitempty"`
	WebEvents    int    `json:"web_events,omitempty"`
	WebElapsedMS int    `json:"web_elapsed_ms,omitempty"`
	Conversation string `json:"conversation,omitempty"`
}

// DailyChat runs the international daily-activity routine. The desktop leg is a
// minimal chat completion that keeps the account warm; the web leg is the agent
// conversation the upstream actually counts. When web is false only the desktop
// leg runs, and the caller treats that as the reference's legacy behaviour
// (wb_accounts.py:1405-1445).
func (c *Client) DailyChat(ctx context.Context, identity Identity, web bool) *DailyChatResult {
	result := &DailyChatResult{}
	body := map[string]any{
		"model": dailyChatModel,
		"messages": []map[string]any{
			// The upstream rejects a conversation whose first message is not a
			// system prompt with code 11-128.
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": dailyChatPrompt},
		},
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
		"max_tokens":     10,
	}
	// A frequency throttle clears on its own; a spent window does not. Retrying
	// only the throttle keeps a transient 429 from losing the day, while a cap
	// fails fast so the caller can record and surface it.
	var lastErr error
	for attempt := 1; attempt <= chatThrottleAttempts; attempt++ {
		lastErr = c.postStream(ctx, c.chatBase()+"/v2/chat/completions", identity.Headers(PurposeChat), body)
		if lastErr == nil {
			break
		}
		rejection, isRejection := asChatRejection(lastErr)
		if !isRejection || !rejection.IsThrottle() {
			break
		}
		if attempt == chatThrottleAttempts {
			break
		}
		select {
		case <-ctx.Done():
			lastErr = ctx.Err()
		case <-time.After(chatThrottleBackoff):
		}
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr != nil {
		// Say which 429 it was. A frequency throttle means "try again shortly",
		// while the quota cap means the model window is spent until the reset
		// clock in the body; without the distinction every 429 looked identical
		// in the run history.
		label := "desktop daily chat failed"
		if rejection, ok := asChatRejection(lastErr); ok {
			if rejection.IsThrottle() {
				label = "desktop daily chat throttled"
			} else {
				label = "desktop daily chat rate-limited"
			}
		}
		result.Message = label + ": " + lastErr.Error()
		return result
	}

	if !web {
		result.OK = true
		result.Message = "daily activity chat completed"
		return result
	}

	result.WebAttempted = true
	turn := c.RunWebConversationTurn(ctx, identity, dailyChatPrompt)
	result.WebOK = turn.OK
	result.WebStatus = turn.Status
	result.WebChunks = turn.Chunks
	result.WebEvents = turn.Events
	result.WebElapsedMS = turn.ElapsedMS
	result.Conversation = turn.Conversation
	if !turn.OK {
		// The day must NOT be marked done: the reference gateway learned that
		// writing lastDailyChat here silently loses the day's credits and stops
		// retrying (wb_accounts.py:1436-1444).
		result.Message = "web daily chat failed: " + turn.Error
		return result
	}
	result.OK = true
	result.Message = "daily activity chat completed (web channel)"
	return result
}

// webHeaders are the browser identity the web console expects: bearer plus
// X-User-Id and no desktop fingerprint (wb_accounts.py:1285-1295).
func (c *Client) webHeaders(identity Identity) map[string]string {
	origin := c.webBase()
	return map[string]string{
		"Authorization": "Bearer " + identity.AccessToken,
		"X-User-Id":     uidOrAnonymous(identity.UID),
		"Content-Type":  "application/json",
		"Accept":        "application/json, text/plain, */*",
		"Origin":        origin,
		"Referer":       origin + "/app",
		"User-Agent":    webUserAgent,
	}
}
