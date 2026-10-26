package model

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A quota rejection is scoped to one (channel, model) pair, not to the whole
// channel: Antigravity reports "Individual quota reached" for a Claude model
// while the same account still serves gemini-3.8-flash. Parking only the pair
// that failed keeps the account in rotation for its other models.
const (
	// channelModelCooldownDefault parks a pair when the upstream did not say
	// when its quota comes back.
	channelModelCooldownDefault = 5 * time.Minute
	// channelModelCooldownMax caps a parsed reset delay so a malformed or
	// hostile upstream message cannot park an account for days.
	channelModelCooldownMax = 6 * time.Hour
	// channelModelCooldownGrace is added to a parsed reset delay so the next
	// attempt does not race the upstream window boundary.
	channelModelCooldownGrace = 30 * time.Second
)

// quotaExhaustedMarkers are the 429 messages that mean "this account has no
// quota left for this model", as opposed to a transient per-second rate limit.
// Matching is case-insensitive against the sanitized upstream error text.
var quotaExhaustedMarkers = []string{
	"individual quota reached",
	"resource has been exhausted",
	"quota exceeded",
	"exceeded your current quota",
	"insufficient_quota",
	"insufficient credits",
}

// quotaResetPattern reads the "Resets in 4h29m56s" suffix Cloud Code Assist
// puts on an account-level quota rejection.
var quotaResetPattern = regexp.MustCompile(`(?i)\bresets? in\s+((?:\d+h)?(?:\d+m)?(?:\d+(?:\.\d+)?s)?)`)

// channelModelCooldowns holds "channelID|model" -> time.Time. It is process
// local: a single gateway node re-learns the state from upstream responses, and
// the entry expires on its own.
var channelModelCooldowns sync.Map

func channelModelCooldownKey(channelID int, model string) string {
	return strconv.Itoa(channelID) + "|" + strings.ToLower(strings.TrimSpace(model))
}

// IsQuotaExhaustedError reports whether an upstream failure means the account
// ran out of quota for the requested model rather than hitting a burst limit.
func IsQuotaExhaustedError(statusCode int, message string) bool {
	if statusCode != http.StatusTooManyRequests {
		return false
	}
	lower := strings.ToLower(message)
	for _, marker := range quotaExhaustedMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// QuotaResetDelay reads the reset delay out of a quota message such as
// "Resets in 4h29m56s". It reports false when the message carries no usable
// delay.
func QuotaResetDelay(message string) (time.Duration, bool) {
	match := quotaResetPattern.FindStringSubmatch(message)
	if len(match) < 2 || strings.TrimSpace(match[1]) == "" {
		return 0, false
	}
	delay, err := time.ParseDuration(strings.ToLower(strings.TrimSpace(match[1])))
	if err != nil || delay <= 0 {
		return 0, false
	}
	return delay, true
}

// MarkChannelModelCooldownFromError parks one (channel, model) pair after an
// upstream quota rejection. It returns the deadline and whether a cooldown was
// recorded, so callers can log the decision.
func MarkChannelModelCooldownFromError(channelID int, model string, statusCode int, message string) (time.Time, bool) {
	model = strings.TrimSpace(model)
	if channelID <= 0 || model == "" {
		return time.Time{}, false
	}
	if !IsQuotaExhaustedError(statusCode, message) {
		return time.Time{}, false
	}

	delay := channelModelCooldownDefault
	if reset, ok := QuotaResetDelay(message); ok {
		delay = reset + channelModelCooldownGrace
	}
	if delay > channelModelCooldownMax {
		delay = channelModelCooldownMax
	}

	until := time.Now().Add(delay)
	channelModelCooldowns.Store(channelModelCooldownKey(channelID, model), until)
	return until, true
}

// ChannelModelCoolingDown reports whether the pair is currently parked. An
// expired entry is dropped on read.
func ChannelModelCoolingDown(channelID int, model string) bool {
	key := channelModelCooldownKey(channelID, model)
	value, ok := channelModelCooldowns.Load(key)
	if !ok {
		return false
	}
	until, ok := value.(time.Time)
	if !ok || !time.Now().Before(until) {
		channelModelCooldowns.Delete(key)
		return false
	}
	return true
}

// ChannelModelCooldownUntil exposes the current deadline for logs and tests.
func ChannelModelCooldownUntil(channelID int, model string) (time.Time, bool) {
	value, ok := channelModelCooldowns.Load(channelModelCooldownKey(channelID, model))
	if !ok {
		return time.Time{}, false
	}
	until, ok := value.(time.Time)
	if !ok || !time.Now().Before(until) {
		return time.Time{}, false
	}
	return until, true
}

// ClearChannelModelCooldown removes a pair from the parking lot.
func ClearChannelModelCooldown(channelID int, model string) {
	channelModelCooldowns.Delete(channelModelCooldownKey(channelID, model))
}

// forgetChannelModelCooldowns clears every parked pair of a deleted channel, so
// a recycled channel id does not start out unavailable.
func forgetChannelModelCooldowns(channelID int) {
	prefix := strconv.Itoa(channelID) + "|"
	channelModelCooldowns.Range(func(key, _ any) bool {
		name, ok := key.(string)
		if ok && strings.HasPrefix(name, prefix) {
			ClearChannelModelCooldown(channelID, strings.TrimPrefix(name, prefix))
		}
		return true
	})
}

// channelModelCooldownNames lists every name a (channel, model) pair may be
// parked under: the id the request is forwarded as after following the channel's
// model_mapping, plus the name the client asked for. The 429 lot and the quota
// snapshot both key off the forwarded id, so an alias has to be resolved before
// anything is looked up. Without a cached channel (memory cache off) the origin
// name is the only name available, which is today's behaviour.
//
// The availability check and the recovery lookup share this list on purpose:
// while only one of them resolved the alias, a pair parked under the forwarded
// id was skipped but its deadline stayed invisible, so the caller lost the
// earliest reset and degraded a 429 with Retry-After into a bare 503.
func channelModelCooldownNames(channelID int, model string) []string {
	model = strings.TrimSpace(model)
	upstream := model
	channelSyncLock.RLock()
	if ch, ok := channelsIDM[channelID]; ok && ch != nil {
		if resolved := strings.TrimSpace(ch.ResolveUpstreamModelName(model)); resolved != "" {
			upstream = resolved
		}
	}
	channelSyncLock.RUnlock()

	if upstream == model {
		return []string{model}
	}
	return []string{upstream, model}
}

// channelModelUnavailable reports whether a (channel, model) pair cannot serve
// traffic right now. Two independent sources feed it:
//
//   - a 429 quota rejection seen at request time (this file's in-memory lot),
//   - the periodic upstream quota snapshot (channel_model_quota.go), which
//     knows about drained accounts before any request fails.
//
// Both are scoped to the pair, so an account drained on Claude keeps serving
// Gemini.
func channelModelUnavailable(channelID int, model string, now time.Time) bool {
	for _, name := range channelModelCooldownNames(channelID, model) {
		if ChannelModelCoolingDown(channelID, name) {
			return true
		}
		if exhausted, _ := ChannelModelQuotaExhausted(channelID, name, now); exhausted {
			return true
		}
	}
	return false
}

// ChannelModelUnavailableUntil reports when this (channel, model) pair becomes
// usable again, taking the later of the observed 429 cooldown and the upstream
// quota reset. It returns false when neither source knows of a blocking window.
//
// The pair is looked up under the same names channelModelUnavailable checks, so a
// pair parked behind the channel's model_mapping alias still reports its reset.
func ChannelModelUnavailableUntil(channelID int, model string) (time.Time, bool) {
	now := time.Now()
	var until time.Time
	for _, name := range channelModelCooldownNames(channelID, model) {
		if deadline, ok := ChannelModelCooldownUntil(channelID, name); ok && deadline.After(until) {
			until = deadline
		}
		if exhausted, reset := ChannelModelQuotaExhausted(channelID, name, now); exhausted && reset.After(until) {
			until = reset
		}
	}
	if until.IsZero() {
		return time.Time{}, false
	}
	return until, true
}

// ModelQuotaExhaustedError reports that every candidate account for one model is
// parked. It carries the earliest recovery so the caller can answer with a
// Retry-After instead of a bare 429 that gives the client no idea when to retry.
type ModelQuotaExhaustedError struct {
	Model         string
	EarliestReset time.Time
}

func (e *ModelQuotaExhaustedError) Error() string {
	if e == nil {
		return ""
	}
	msg := "all accounts have no quota for model " + e.Model
	if !e.EarliestReset.IsZero() {
		msg += ", earliest reset at " + e.EarliestReset.Format(time.RFC3339)
	}
	return msg
}

// RetryAfterSeconds is the delay a client should wait before retrying, floored
// at one second so a boundary race does not produce "retry immediately".
func (e *ModelQuotaExhaustedError) RetryAfterSeconds(now time.Time) int {
	if e == nil || e.EarliestReset.IsZero() {
		return 0
	}
	seconds := int(e.EarliestReset.Sub(now).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

// dropCoolingDownChannels keeps the channels that can serve this model. When
// every candidate is parked it returns an empty slice plus the earliest moment
// one of them recovers, so the caller can fail fast with a Retry-After instead
// of routing traffic into an account that is known to be drained.
func dropCoolingDownChannels(ids []int, model string) ([]int, time.Time) {
	if len(ids) == 0 || strings.TrimSpace(model) == "" {
		return ids, time.Time{}
	}
	now := time.Now()
	kept := make([]int, 0, len(ids))
	var earliest time.Time
	for _, id := range ids {
		if !channelModelUnavailable(id, model, now) {
			kept = append(kept, id)
			continue
		}
		if until, ok := ChannelModelUnavailableUntil(id, model); ok {
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
		}
	}
	if len(kept) == 0 {
		return nil, earliest
	}
	return kept, time.Time{}
}

// dropCoolingDownAbilities is dropCoolingDownChannels for the database-backed
// selection path, which works on abilities rather than channel ids.
func dropCoolingDownAbilities(abilities []Ability, model string) ([]Ability, time.Time) {
	if len(abilities) == 0 || strings.TrimSpace(model) == "" {
		return abilities, time.Time{}
	}
	now := time.Now()
	kept := make([]Ability, 0, len(abilities))
	var earliest time.Time
	for _, ability := range abilities {
		if !channelModelUnavailable(ability.ChannelId, model, now) {
			kept = append(kept, ability)
			continue
		}
		if until, ok := ChannelModelUnavailableUntil(ability.ChannelId, model); ok {
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
		}
	}
	if len(kept) == 0 {
		return nil, earliest
	}
	return kept, time.Time{}
}
