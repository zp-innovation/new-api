package model

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsQuotaExhaustedError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
		want       bool
	}{
		{
			name:       "antigravity account quota",
			statusCode: http.StatusTooManyRequests,
			message:    "status_code=429, Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 4h29m56s.",
			want:       true,
		},
		{
			name:       "antigravity model rate limit",
			statusCode: http.StatusTooManyRequests,
			message:    "status_code=429, Resource has been exhausted (e.g. check quota).",
			want:       true,
		},
		{
			name:       "openai quota",
			statusCode: http.StatusTooManyRequests,
			message:    "You exceeded your current quota, please check your plan and billing details.",
			want:       true,
		},
		{
			name:       "case insensitive",
			statusCode: http.StatusTooManyRequests,
			message:    "INDIVIDUAL QUOTA REACHED",
			want:       true,
		},
		{
			name:       "plain rate limit is not a quota rejection",
			statusCode: http.StatusTooManyRequests,
			message:    "status_code=429, Rate limit reached for requests",
			want:       false,
		},
		{
			name:       "wrong status code",
			statusCode: http.StatusServiceUnavailable,
			message:    "Individual quota reached",
			want:       false,
		},
		{
			name:       "empty message",
			statusCode: http.StatusTooManyRequests,
			message:    "",
			want:       false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsQuotaExhaustedError(tc.statusCode, tc.message))
		})
	}
}

func TestQuotaResetDelay(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    time.Duration
		wantOK  bool
	}{
		{name: "hours minutes seconds", message: "Resets in 4h29m56s.", want: 4*time.Hour + 29*time.Minute + 56*time.Second, wantOK: true},
		{name: "minutes only", message: "resets in 12m", want: 12 * time.Minute, wantOK: true},
		{name: "seconds only", message: "Resets in 45s", want: 45 * time.Second, wantOK: true},
		{name: "no reset text", message: "Individual quota reached.", wantOK: false},
		{name: "unparsable", message: "Resets in soon", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := QuotaResetDelay(tc.message)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestMarkChannelModelCooldownFromError(t *testing.T) {
	const model = "cooldown-unit-test-model"
	const channelID = 990001
	t.Cleanup(func() { ClearChannelModelCooldown(channelID, model) })

	message := "status_code=429, Individual quota reached. Resets in 4h29m56s."
	until, parked := MarkChannelModelCooldownFromError(channelID, model, http.StatusTooManyRequests, message)
	require.True(t, parked)
	require.True(t, ChannelModelCoolingDown(channelID, model))
	stored, ok := ChannelModelCooldownUntil(channelID, model)
	require.True(t, ok)
	assert.WithinDuration(t, until, stored, time.Second)

	// The deadline follows the upstream reset plus a small grace window.
	wantMin := time.Now().Add(4*time.Hour + 29*time.Minute + 56*time.Second + channelModelCooldownGrace)
	assert.WithinDuration(t, wantMin, until, 10*time.Second)

	// The parking lot is per (channel, model): other models on the same account
	// keep working.
	assert.False(t, ChannelModelCoolingDown(channelID, "gemini-3.8-flash-high"))
	assert.False(t, ChannelModelCoolingDown(channelID+1, model))

	ClearChannelModelCooldown(channelID, model)
	assert.False(t, ChannelModelCoolingDown(channelID, model))
}

func TestMarkChannelModelCooldownIgnoresNonQuotaAndBoundsReset(t *testing.T) {
	const model = "cooldown-bounds-model"
	const channelID = 990002
	t.Cleanup(func() { ClearChannelModelCooldown(channelID, model) })

	_, parked := MarkChannelModelCooldownFromError(channelID, model, http.StatusTooManyRequests, "Rate limit reached")
	assert.False(t, parked)
	assert.False(t, ChannelModelCoolingDown(channelID, model))

	_, parked = MarkChannelModelCooldownFromError(channelID, model, http.StatusBadRequest, "Individual quota reached")
	assert.False(t, parked)

	until, parked := MarkChannelModelCooldownFromError(channelID, model, http.StatusTooManyRequests, "quota exceeded. Resets in 99h")
	require.True(t, parked)
	assert.WithinDuration(t, time.Now().Add(channelModelCooldownMax), until, 10*time.Second)

	ClearChannelModelCooldown(channelID, model)
	until, parked = MarkChannelModelCooldownFromError(channelID, model, http.StatusTooManyRequests, "Individual quota reached")
	require.True(t, parked)
	assert.WithinDuration(t, time.Now().Add(channelModelCooldownDefault), until, 10*time.Second)
}

func TestChannelModelCooldownExpires(t *testing.T) {
	const model = "cooldown-expiry-model"
	const channelID = 990003
	t.Cleanup(func() { ClearChannelModelCooldown(channelID, model) })

	channelModelCooldowns.Store(channelModelCooldownKey(channelID, model), time.Now().Add(-time.Second))
	assert.False(t, ChannelModelCoolingDown(channelID, model))
	_, ok := channelModelCooldowns.Load(channelModelCooldownKey(channelID, model))
	assert.False(t, ok, "an expired entry is dropped on read")
}

func TestDropCoolingDownCandidates(t *testing.T) {
	const model = "cooldown-selection-model"
	ids := []int{990011, 990012, 990013}
	t.Cleanup(func() {
		for _, id := range ids {
			ClearChannelModelCooldown(id, model)
		}
	})

	got, until := dropCoolingDownChannels(ids, model)
	assert.Equal(t, ids, got, "no cooldown keeps every candidate")
	assert.True(t, until.IsZero())

	channelModelCooldowns.Store(channelModelCooldownKey(990011, model), time.Now().Add(time.Hour))
	got, until = dropCoolingDownChannels(ids, model)
	assert.Equal(t, []int{990012, 990013}, got)
	assert.True(t, until.IsZero())

	abilities := []Ability{{ChannelId: 990011}, {ChannelId: 990012}, {ChannelId: 990013}}
	gotAbilities, _ := dropCoolingDownAbilities(abilities, model)
	assert.Equal(t, []Ability{{ChannelId: 990012}, {ChannelId: 990013}}, gotAbilities)

	// When every candidate is parked the caller gets an empty slice plus the
	// earliest recovery, so it can answer with Retry-After instead of routing
	// into a known-drained account.
	deadline := time.Now().Add(time.Hour).Truncate(time.Second)
	for _, id := range ids {
		channelModelCooldowns.Store(channelModelCooldownKey(id, model), deadline)
	}
	got, until = dropCoolingDownChannels(ids, model)
	assert.Empty(t, got)
	assert.WithinDuration(t, deadline, until, time.Second)

	gotAbilities, until = dropCoolingDownAbilities(abilities, model)
	assert.Empty(t, gotAbilities)
	assert.WithinDuration(t, deadline, until, time.Second)
}
func TestDropCoolingDownCandidatesIsModelScoped(t *testing.T) {
	const model = "cooldown-scope-model"
	const otherModel = "gemini-3.8-flash-high"
	const parkedChannelID = 990021
	const healthyChannelID = 990022
	t.Cleanup(func() { ClearChannelModelCooldown(parkedChannelID, model) })

	channelModelCooldowns.Store(channelModelCooldownKey(parkedChannelID, model), time.Now().Add(time.Hour))

	ids := []int{parkedChannelID, healthyChannelID}
	got, _ := dropCoolingDownChannels(ids, model)
	assert.Equal(t, []int{healthyChannelID}, got, "the parked pair is skipped")
	got, _ = dropCoolingDownChannels(ids, otherModel)
	assert.Equal(t, ids, got, "another model on the same account still works")
}

func TestModelQuotaExhaustedErrorRetryAfter(t *testing.T) {
	now := time.Now()
	err := &ModelQuotaExhaustedError{Model: "claude-opus-4-6-thinking", EarliestReset: now.Add(90 * time.Minute)}
	assert.Equal(t, 5400, err.RetryAfterSeconds(now))
	assert.Contains(t, err.Error(), "claude-opus-4-6-thinking")

	// A reset already in the past must not produce a zero or negative wait.
	past := &ModelQuotaExhaustedError{Model: "m", EarliestReset: now.Add(-time.Minute)}
	assert.Equal(t, 1, past.RetryAfterSeconds(now))

	empty := &ModelQuotaExhaustedError{Model: "m"}
	assert.Zero(t, empty.RetryAfterSeconds(now))
}

// The 429 parking lot is keyed by the model id the request is forwarded as, so a
// channel alias must be resolved on the availability check and on the recovery
// lookup alike. While only the check resolved it, the candidate was dropped but
// the deadline stayed invisible, and the caller degraded to a bare 503.
func TestDropCoolingDownCandidatesResolvesModelAlias(t *testing.T) {
	const channelID = 990031
	const origin = "claude-opus-4-6"
	const upstream = "claude-opus-4-6-thinking"
	resetChannelModelQuotaCache(t)

	mapping := `{"claude-opus-4-6":"claude-opus-4-6-thinking"}`
	channelSyncLock.Lock()
	previous := channelsIDM
	channelsIDM = map[int]*Channel{channelID: {Id: channelID, ModelMapping: &mapping}}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		ClearChannelModelCooldown(channelID, upstream)
		channelSyncLock.Lock()
		channelsIDM = previous
		channelSyncLock.Unlock()
	})

	deadline := time.Now().Add(90 * time.Minute).Truncate(time.Second)
	channelModelCooldowns.Store(channelModelCooldownKey(channelID, upstream), deadline)

	got, until := dropCoolingDownChannels([]int{channelID}, origin)
	assert.Empty(t, got, "the alias inherits the forwarded model's 429 cooldown")
	assert.WithinDuration(t, deadline, until, time.Second, "the recovery deadline must survive the alias lookup")

	gotAbilities, abilitiesUntil := dropCoolingDownAbilities([]Ability{{ChannelId: channelID}}, origin)
	assert.Empty(t, gotAbilities)
	assert.WithinDuration(t, deadline, abilitiesUntil, time.Second)

	aliasUntil, ok := ChannelModelUnavailableUntil(channelID, origin)
	require.True(t, ok)
	assert.WithinDuration(t, deadline, aliasUntil, time.Second)

	kept, unrelatedUntil := dropCoolingDownChannels([]int{channelID}, "gemini-3.8-flash-high")
	assert.Equal(t, []int{channelID}, kept, "resolving the alias must not park other models")
	assert.True(t, unrelatedUntil.IsZero())

	ClearChannelModelCooldown(channelID, upstream)
	kept, until = dropCoolingDownChannels([]int{channelID}, origin)
	assert.Equal(t, []int{channelID}, kept)
	assert.True(t, until.IsZero())
}
