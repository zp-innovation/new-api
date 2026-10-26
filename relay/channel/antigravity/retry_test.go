package antigravity

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const capacityErrorBody = `{"error":{"code":503,"status":"UNAVAILABLE",` +
	`"message":"The model is currently experiencing high demand",` +
	`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"MODEL_CAPACITY_EXHAUSTED"}]}}`

func TestIsModelCapacityExhausted(t *testing.T) {
	assert.True(t, isModelCapacityExhausted(http.StatusServiceUnavailable, []byte(capacityErrorBody)))
	assert.True(t, isModelCapacityExhausted(http.StatusTooManyRequests, []byte("model_capacity_exhausted")))
	assert.False(t, isModelCapacityExhausted(http.StatusInternalServerError, []byte(capacityErrorBody)))
	assert.False(t, isModelCapacityExhausted(http.StatusServiceUnavailable, []byte(`{"error":{"message":"overloaded"}}`)))
}

func TestModelCapacityCooldown(t *testing.T) {
	model := "test-model-capacity-cooldown"
	modelCapacityUntil.Delete(model)
	t.Cleanup(func() { modelCapacityUntil.Delete(model) })

	now := time.Now()
	assert.False(t, modelCapacityCooldownActive(model, now))

	markModelCapacityCooldown(model, now)
	assert.True(t, modelCapacityCooldownActive(model, now))
	assert.False(t, modelCapacityCooldownActive(model, now.Add(capacityCooldown+time.Second)))
}

func TestRequestWithCapacityRetryRepostsUntilSuccess(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, capacityErrorBody)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}}`)
	}))
	defer server.Close()

	restoreRetrySettings(t)
	model := "gemini-4-argon-retry-success"
	modelCapacityUntil.Delete(model)
	t.Cleanup(func() { modelCapacityUntil.Delete(model) })

	adaptor := &Adaptor{}
	info := newCapacityTestInfo(server.URL, model)
	body, closer, err := relaycommon.NewOutboundJSONBody([]byte(`{"project":"proj-1","model":"gemini-4-argon"}`))
	require.NoError(t, err)
	defer closer.Close()

	resp, err := adaptor.requestWithCapacityRetry(newTestContext(t), info, body)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.GreaterOrEqual(t, attempts.Load(), int32(3))
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func TestRequestWithCapacityRetryHonorsCooldown(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, capacityErrorBody)
	}))
	defer server.Close()

	restoreRetrySettings(t)
	model := "gemini-4-argon-retry-cooldown"
	modelCapacityUntil.Delete(model)
	t.Cleanup(func() { modelCapacityUntil.Delete(model) })
	markModelCapacityCooldown(model, time.Now())

	adaptor := &Adaptor{}
	info := newCapacityTestInfo(server.URL, model)
	body, closer, err := relaycommon.NewOutboundJSONBody([]byte(`{"project":"proj-1"}`))
	require.NoError(t, err)
	defer closer.Close()

	resp, err := adaptor.requestWithCapacityRetry(newTestContext(t), info, body)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, int32(1), attempts.Load())
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func TestRequestWithCapacityRetryFailsFastAfterARejection(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, capacityErrorBody)
	}))
	defer server.Close()

	restoreRetrySettings(t)
	model := "gemini-4-argon-retry-fail-fast"
	modelCapacityUntil.Delete(model)
	t.Cleanup(func() { modelCapacityUntil.Delete(model) })

	adaptor := &Adaptor{}
	info := newCapacityTestInfo(server.URL, model)

	first, closer, err := relaycommon.NewOutboundJSONBody([]byte(`{"project":"proj-1"}`))
	require.NoError(t, err)
	defer closer.Close()
	resp, err := adaptor.requestWithCapacityRetry(newTestContext(t), info, first)
	require.NoError(t, err)
	require.NotNil(t, resp)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	afterFirst := attempts.Load()
	require.GreaterOrEqual(t, afterFirst, int32(2))

	second, secondCloser, err := relaycommon.NewOutboundJSONBody([]byte(`{"project":"proj-1"}`))
	require.NoError(t, err)
	defer secondCloser.Close()
	resp, err = adaptor.requestWithCapacityRetry(newTestContext(t), info, second)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, afterFirst+1, attempts.Load(), "a later request must fail fast instead of replaying the budget")
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func TestRequestWithCapacityRetryNeedsReplayableBody(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, capacityErrorBody)
	}))
	defer server.Close()

	restoreRetrySettings(t)
	model := "gemini-4-argon-retry-one-shot"
	modelCapacityUntil.Delete(model)
	t.Cleanup(func() { modelCapacityUntil.Delete(model) })

	adaptor := &Adaptor{}
	info := newCapacityTestInfo(server.URL, model)

	resp, err := adaptor.requestWithCapacityRetry(newTestContext(t), info, strings.NewReader(`{"project":"proj-1"}`))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, int32(1), attempts.Load())
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func newCapacityTestInfo(baseURL, model string) *relaycommon.RelayInfo {
	info := newRelayInfo(validKey, model)
	info.ChannelBaseUrl = baseURL
	info.ChannelType = constant.ChannelTypeAntigravity
	return info
}

// restoreRetrySettings shortens the retry loop for tests and restores the
// production values afterwards.
func restoreRetrySettings(t *testing.T) {
	t.Helper()
	oldAttempts, oldInterval := capacityRetryAttempts, capacityRetryInterval
	capacityRetryAttempts, capacityRetryInterval = 5, time.Millisecond
	t.Cleanup(func() {
		capacityRetryAttempts, capacityRetryInterval = oldAttempts, oldInterval
	})
}
