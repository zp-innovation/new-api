package workbuddy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// The chat endpoint has two distinct 429s and they mean opposite things:
//
//   - a frequency throttle (intl code 14003, or the bare "usage exceeds frequency
//     limit" body) is transient and clears on its own within seconds;
//   - a quota cap (code 6004) is the model window being spent, and only that one
//     carries a reset wall clock.
//
// Folding both into "chat request rejected" hid which one happened, so the run
// history could not tell a two-second throttle from a spent window. The probe now
// keeps the status and the upstream body, classifies the 429, and retries only
// the transient shape.
const (
	// chatThrottleAttempts bounds the retry of a transient frequency throttle.
	// Three attempts with the pause below is about the minute the upstream asks
	// for; retrying faster than that just spends attempts inside the same
	// throttle window.
	chatThrottleAttempts = 3
	// maxChatErrorBodyBytes bounds the rejection body kept for diagnostics and
	// for the cap parser, which reads the reset clock out of the text.
	maxChatErrorBodyBytes = 4 << 10
	// modelScopedError codes describe the model or request shape, not the
	// credential. Treating them as account failures would disable a healthy
	// account because one model is unavailable to its plan.
	modelScopedErrorUnavailable    = 11102
	modelScopedErrorInvalidParams  = 11129
	modelScopedErrorProviderDown   = 11134
	modelScopedErrorUnsupportedEff = 11150
	modelScopedErrorEntitlement    = 11158
)

// chatThrottleBackoff is the pause between throttle attempts. The upstream's own
// guidance for code 14003 is "wait about a minute before retrying"; a variable
// rather than a constant so a test can exercise the retry without sleeping.
var chatThrottleBackoff = 30 * time.Second

// chatRejection is a non-2xx answer from the chat endpoint, carrying enough of
// the upstream body to classify the 429.
type chatRejection struct {
	StatusCode int
	Body       string
}

func (e *chatRejection) Error() string {
	if e == nil {
		return "chat request rejected"
	}
	summary := e.summary()
	if summary == "" {
		return fmt.Sprintf("chat request rejected (status %d)", e.StatusCode)
	}
	return fmt.Sprintf("chat request rejected (status %d): %s", e.StatusCode, summary)
}

// summary renders the useful part of the upstream body: the envelope code and
// message plus the actionable hints, without the request ids and display-blob
// noise that made the original failure unreadable in the run history.
func (e *chatRejection) summary() string {
	if e == nil {
		return ""
	}
	var decoded struct {
		Code        json.RawMessage `json:"code"`
		Msg         string          `json:"msg"`
		RetryPolicy struct {
			Retry bool `json:"retry"`
		} `json:"retry_policy"`
		DisplayTips struct {
			En string `json:"en"`
			Zh string `json:"zh"`
		} `json:"displayTips"`
	}
	if err := common.Unmarshal([]byte(strings.TrimSpace(e.Body)), &decoded); err != nil {
		return strings.TrimSpace(e.Body)
	}
	parts := make([]string, 0, 4)
	if code, ok := decodeChatErrorCode(e.Body); ok {
		parts = append(parts, fmt.Sprintf("code=%d", code))
	}
	if msg := strings.TrimSpace(decoded.Msg); msg != "" {
		parts = append(parts, msg)
	}
	if hint := strings.TrimSpace(decoded.DisplayTips.Zh); hint != "" {
		parts = append(parts, hint)
	}
	if decoded.RetryPolicy.Retry {
		parts = append(parts, "retryable")
	}
	if len(parts) == 0 {
		return strings.TrimSpace(e.Body)
	}
	return strings.Join(parts, "; ")
}

// IsThrottle reports whether the rejection is a transient frequency limit rather
// than a spent quota window. Only a body that carries the cap code is excluded,
// so an unrecognised 429 is treated as retryable: retrying a throttle is cheap,
// while treating a cap as retryable only wastes the few attempts this allows.
func (e *chatRejection) IsThrottle() bool {
	if e == nil || e.StatusCode != http.StatusTooManyRequests {
		return false
	}
	return !MessageHasRateLimitCode(e.Body)
}

// asChatRejection unwraps a chat rejection from an error chain.
func asChatRejection(err error) (*chatRejection, bool) {
	var rejection *chatRejection
	if errors.As(err, &rejection) {
		return rejection, true
	}
	return nil, false
}

// decodeChatErrorCode reads the envelope code out of an upstream body, tolerating
// both the number and the string form.
func decodeChatErrorCode(body string) (int, bool) {
	var decoded struct {
		Code json.RawMessage `json:"code"`
	}
	if err := common.Unmarshal([]byte(body), &decoded); err != nil || len(decoded.Code) == 0 {
		return 0, false
	}
	text := strings.Trim(strings.TrimSpace(string(decoded.Code)), `"`)
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return value, true
}

// ChatErrorCode returns the upstream business code carried by a response body.
// Relay error handling uses it before generic status-code health accounting.
func ChatErrorCode(body string) (int, bool) {
	return decodeChatErrorCode(body)
}

// IsModelScopedChatError reports whether the upstream rejected the model or a
// model-specific request parameter rather than the account credentials.
func IsModelScopedChatError(code int) bool {
	switch code {
	case modelScopedErrorUnavailable,
		modelScopedErrorInvalidParams,
		modelScopedErrorProviderDown,
		modelScopedErrorUnsupportedEff,
		modelScopedErrorEntitlement:
		return true
	}
	return false
}

func IsModelEntitlementChatError(code int) bool {
	return code == modelScopedErrorEntitlement
}
