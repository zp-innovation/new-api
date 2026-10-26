package workbuddy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// A frequency throttle and a spent quota window are both 429 but mean opposite
// things, and the daily-activity probe must tell them apart.
func TestChatRejectionDistinguishesThrottleFromCap(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		throttle bool
	}{
		{
			name:     "bare frequency limit",
			status:   http.StatusTooManyRequests,
			body:     `{"code":14003,"msg":"usage exceeds frequency limit"}`,
			throttle: true,
		},
		{
			name:     "too many requests",
			status:   http.StatusTooManyRequests,
			body:     `{"code":14003,"msg":"too many requests"}`,
			throttle: true,
		},
		{
			name:     "quota cap with reset clock",
			status:   http.StatusTooManyRequests,
			body:     `{"code":6004,"msg":"usage will reset at 2026-10-13 23:00:00 UTC+8"}`,
			throttle: false,
		},
		{
			name:     "non-429",
			status:   http.StatusBadRequest,
			body:     `{"code":11128,"msg":"first message is not system prompt"}`,
			throttle: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rejection := &chatRejection{StatusCode: test.status, Body: test.body}
			assert.Equal(t, test.throttle, rejection.IsThrottle())
		})
	}
}

// The daily-activity desktop leg retries a transient throttle, so a burst that
// clears in a second does not lose the day.
func TestDailyChatRetriesAFrequencyThrottle(t *testing.T) {
	// The production pause honours the upstream's "about a minute", which is far
	// too long for a unit test; the retry logic itself is what matters here.
	original := chatThrottleBackoff
	chatThrottleBackoff = 10 * time.Millisecond
	t.Cleanup(func() { chatThrottleBackoff = original })

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":14003,"msg":"usage exceeds frequency limit"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL, RealmIntl, ProductCLI)
	result := client.DailyChat(context.Background(), Identity{Realm: RealmIntl, UID: "uid-1"}, false)

	assert.True(t, result.OK, result.Message)
	assert.EqualValues(t, 3, calls.Load(), "the throttle must be retried until it clears")
}

// A spent window is not retried: the reset clock will not move for hours, and
// burning attempts on it only delays the report.
func TestDailyChatDoesNotRetryAQuotaCap(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":6004,"msg":"usage will reset at 2026-10-13 23:00:00 UTC+8"}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL, RealmIntl, ProductCLI)
	result := client.DailyChat(context.Background(), Identity{Realm: RealmIntl, UID: "uid-1"}, false)

	assert.False(t, result.OK)
	assert.EqualValues(t, 1, calls.Load(), "a quota cap must fail fast")
	assert.Contains(t, result.Message, "rate-limited")
	assert.Contains(t, result.Message, "2026-10-13 23:00:00")
}

// The rejection keeps the upstream body and status, which is what made the
// original failure unreadable.
func TestChatRejectionCarriesTheUpstreamBody(t *testing.T) {
	rejection := &chatRejection{StatusCode: http.StatusTooManyRequests, Body: `{"code":14003,"msg":"too many requests"}`}
	message := rejection.Error()
	assert.Contains(t, message, "429")
	assert.Contains(t, message, "too many requests")

	empty := &chatRejection{StatusCode: http.StatusBadGateway}
	assert.Contains(t, empty.Error(), "502")
}

// The code is read from either the number or the string form.
func TestDecodeChatErrorCode(t *testing.T) {
	tests := []struct {
		body string
		want int
		ok   bool
	}{
		{body: `{"code":14003}`, want: 14003, ok: true},
		{body: `{"code":"14003"}`, want: 14003, ok: true},
		{body: `{"msg":"no code"}`, ok: false},
		{body: `not json`, ok: false},
	}
	for _, test := range tests {
		got, ok := decodeChatErrorCode(test.body)
		assert.Equal(t, test.ok, ok, test.body)
		assert.Equal(t, test.want, got, test.body)
	}
}

func TestModelScopedChatErrorCodes(t *testing.T) {
	for _, code := range []int{11102, 11129, 11134, 11150, 11158} {
		assert.True(t, IsModelScopedChatError(code), "code %d must not penalize the account", code)
	}
	for _, code := range []int{0, 11101, 11128, 14003, 6004} {
		assert.False(t, IsModelScopedChatError(code), "code %d is not model scoped", code)
	}
	assert.True(t, IsModelEntitlementChatError(11158))
	assert.False(t, IsModelEntitlementChatError(11134))

	code, ok := ChatErrorCode(`{"code":"11158","msg":"model forbidden"}`)
	assert.True(t, ok)
	assert.Equal(t, 11158, code)
}

// A truncated upstream error has to stay valid UTF-8: the panel renders it, and
// slicing the body by bytes would cut a multibyte character in half.
func TestTruncateBodyDoesNotSplitMultibyteCharacters(t *testing.T) {
	assert.Equal(t, "boom", truncateBody([]byte("  boom\n")))

	truncated := truncateBody([]byte(strings.Repeat("中", 1000)))
	assert.True(t, utf8.ValidString(truncated))
	assert.Equal(t, strings.Repeat("中", 512)+"...", truncated)
}
