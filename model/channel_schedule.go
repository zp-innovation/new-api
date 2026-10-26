package model

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ChannelScheduleOptionPrefix groups the option keys behind the channel time
// schedule. The rules live in one JSON option, the same way the channel
// affinity rules do, so the feature needs no migration and every node picks the
// new value up through the existing option sync.
const ChannelScheduleOptionPrefix = "channel_schedule_setting."

const (
	channelScheduleOptionEnabledKey     = ChannelScheduleOptionPrefix + "enabled"
	channelScheduleOptionTimezoneKey    = ChannelScheduleOptionPrefix + "timezone"
	channelScheduleOptionCheckMinuteKey = ChannelScheduleOptionPrefix + "check_minutes"
	channelScheduleOptionRulesKey       = ChannelScheduleOptionPrefix + "rules"
)

// The rule list is administrator input; these bounds keep one bad save from
// making every scheduled check walk an unbounded amount of work.
const (
	channelScheduleMaxRules              = 100
	channelScheduleMaxActionsPerRule     = 50
	channelScheduleMaxChannelsPerAction  = 500
	minChannelScheduleCheckMinutes       = 1
	maxChannelScheduleCheckMinutes       = 1440
	maxChannelSchedulePriority           = 1_000_000
	maxChannelScheduleWeight             = 1_000_000
	maxChannelScheduleShare              = 100
	channelScheduleShareScale            = 10_000
	defaultChannelScheduleCheckMinutes   = 1
	defaultChannelScheduleTimezone       = "Asia/Shanghai"
	channelScheduleStatusReasonKeyPrefix = "channel schedule rule "
)

// Weight modes of a schedule action.
//
// The default writes the configured number to every channel in scope, so the
// split between two scopes follows how many channels each one holds. The tag
// mode treats the number as the scope tag's share and splits it across the
// tag's current channels instead, so a ratio between two tags survives channels
// being added or removed without anyone editing the rule again.
const (
	ChannelScheduleWeightModeChannel = "channel"
	ChannelScheduleWeightModeTag     = "tag"
)

// ChannelScheduleAction is one target plus the fields it rewrites. A nil field
// is left untouched, which is what the console shows as "not modified".
//
// Target selection is either a tag or an explicit channel list. The tag is only
// ever used to resolve which channels are in scope: channels carry a single tag
// column, and an empty tag would otherwise match every untagged channel.
type ChannelScheduleAction struct {
	Mode       string `json:"mode"`
	Tag        string `json:"tag,omitempty"`
	ChannelIds []int  `json:"channel_ids,omitempty"`
	Priority   *int64 `json:"priority,omitempty"`
	Weight     *uint  `json:"weight,omitempty"`
	// WeightMode decides what Weight means: ChannelScheduleWeightModeChannel
	// (also the empty value, so rules saved before this field existed keep
	// working) writes the number to each channel as-is, while
	// ChannelScheduleWeightModeTag splits it across the tag's channels.
	WeightMode string `json:"weight_mode,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

// ChannelScheduleRule is one time window with the changes applied inside it.
// The window is [Start, End) in the configured timezone; an End earlier than
// Start wraps past midnight. An empty Weekdays list means every day.
type ChannelScheduleRule struct {
	Id       string                  `json:"id"`
	Name     string                  `json:"name"`
	Enabled  bool                    `json:"enabled"`
	Weekdays []int                   `json:"weekdays,omitempty"`
	Start    string                  `json:"start"`
	End      string                  `json:"end"`
	Actions  []ChannelScheduleAction `json:"actions"`
}

// ChannelScheduleSnapshot is the validated read model behind the schedule API.
// It is rebuilt from the option map on every option write, so the runtime
// globals updated by the same write never disagree with it.
type ChannelScheduleSnapshot struct {
	Enabled      bool
	Timezone     string
	CheckMinutes int
	Rules        []ChannelScheduleRule
	Options      map[string]string
}

var channelScheduleSnapshot atomic.Pointer[ChannelScheduleSnapshot]
var channelScheduleOptionMutex sync.Mutex

func channelScheduleDefaultOptions() map[string]string {
	return map[string]string{
		channelScheduleOptionEnabledKey:     "false",
		channelScheduleOptionTimezoneKey:    defaultChannelScheduleTimezone,
		channelScheduleOptionCheckMinuteKey: strconv.Itoa(defaultChannelScheduleCheckMinutes),
		channelScheduleOptionRulesKey:       "[]",
	}
}

// ChannelScheduleStatusReason marks a channel status change as coming from a
// schedule rule, so the stored reason explains why the channel was switched.
func ChannelScheduleStatusReason(ruleName string) string {
	return channelScheduleStatusReasonKeyPrefix + ruleName
}

// IsChannelScheduleOption reports whether a key belongs to the schedule. Writes
// to any other key are rejected, so a typo cannot silently land in the options
// table and be ignored forever.
func IsChannelScheduleOption(key string) bool {
	_, ok := strings.CutPrefix(key, ChannelScheduleOptionPrefix)
	return ok
}

func CurrentChannelSchedule() *ChannelScheduleSnapshot {
	if snapshot := channelScheduleSnapshot.Load(); snapshot != nil {
		return snapshot
	}
	// Startup and test fixtures may not have loaded options yet.
	snapshot, err := BuildChannelSchedule(nil)
	if err != nil {
		return &ChannelScheduleSnapshot{Options: map[string]string{}}
	}
	return snapshot
}

// BuildChannelSchedule validates a whole option set and returns its read model.
// The caller passes either the persisted options or nil for the defaults.
func BuildChannelSchedule(options map[string]string) (*ChannelScheduleSnapshot, error) {
	raw := channelScheduleDefaultOptions()
	maps.Copy(raw, options)
	for key := range raw {
		if !IsChannelScheduleOption(key) {
			return nil, fmt.Errorf("not a channel schedule option: %s", key)
		}
	}

	snapshot := &ChannelScheduleSnapshot{Options: raw}
	enabled, err := strconv.ParseBool(raw[channelScheduleOptionEnabledKey])
	if err != nil {
		return nil, fmt.Errorf("invalid enabled flag")
	}
	snapshot.Enabled = enabled

	snapshot.Timezone = strings.TrimSpace(raw[channelScheduleOptionTimezoneKey])
	if snapshot.Timezone == "" {
		snapshot.Timezone = defaultChannelScheduleTimezone
	}
	if _, err := time.LoadLocation(snapshot.Timezone); err != nil {
		return nil, fmt.Errorf("unknown timezone: %s", snapshot.Timezone)
	}

	snapshot.CheckMinutes, err = strconv.Atoi(raw[channelScheduleOptionCheckMinuteKey])
	if err != nil || snapshot.CheckMinutes < minChannelScheduleCheckMinutes || snapshot.CheckMinutes > maxChannelScheduleCheckMinutes {
		return nil, fmt.Errorf("check interval must be between %d and %d minutes", minChannelScheduleCheckMinutes, maxChannelScheduleCheckMinutes)
	}

	rulesJSON := strings.TrimSpace(raw[channelScheduleOptionRulesKey])
	if rulesJSON == "" {
		rulesJSON = "[]"
	}
	var rules []ChannelScheduleRule
	if err := common.UnmarshalJsonStr(rulesJSON, &rules); err != nil {
		return nil, fmt.Errorf("invalid rules: %w", err)
	}
	if len(rules) > channelScheduleMaxRules {
		return nil, fmt.Errorf("at most %d rules are supported", channelScheduleMaxRules)
	}
	seenIds := make(map[string]struct{}, len(rules))
	for i := range rules {
		rule := &rules[i]
		if err := validateChannelScheduleRule(rule); err != nil {
			return nil, err
		}
		if _, duplicated := seenIds[rule.Id]; duplicated {
			return nil, fmt.Errorf("duplicated rule id: %s", rule.Id)
		}
		seenIds[rule.Id] = struct{}{}
	}
	snapshot.Rules = rules
	return snapshot, nil
}

func validateChannelScheduleRule(rule *ChannelScheduleRule) error {
	rule.Id = strings.TrimSpace(rule.Id)
	if rule.Id == "" {
		return fmt.Errorf("rule id is required")
	}
	if len(rule.Id) > 64 {
		return fmt.Errorf("rule id is too long: %s", rule.Id)
	}
	rule.Name = strings.TrimSpace(rule.Name)
	if rule.Name == "" {
		return fmt.Errorf("rule %s needs a name", rule.Id)
	}
	if len(rule.Name) > 128 {
		return fmt.Errorf("rule %s has a name that is too long", rule.Id)
	}
	if _, _, err := parseChannelScheduleClock(rule.Start); err != nil {
		return fmt.Errorf("rule %s: %w", rule.Name, err)
	}
	if _, _, err := parseChannelScheduleClock(rule.End); err != nil {
		return fmt.Errorf("rule %s: %w", rule.Name, err)
	}
	seenDays := make(map[int]struct{}, len(rule.Weekdays))
	for _, day := range rule.Weekdays {
		if day < 0 || day > 6 {
			return fmt.Errorf("rule %s: weekday must be between 0 and 6", rule.Name)
		}
		if _, duplicated := seenDays[day]; duplicated {
			return fmt.Errorf("rule %s: duplicated weekday", rule.Name)
		}
		seenDays[day] = struct{}{}
	}
	if len(rule.Actions) == 0 {
		return fmt.Errorf("rule %s needs at least one action", rule.Name)
	}
	if len(rule.Actions) > channelScheduleMaxActionsPerRule {
		return fmt.Errorf("rule %s has too many actions", rule.Name)
	}
	for i := range rule.Actions {
		if err := validateChannelScheduleAction(rule.Name, &rule.Actions[i]); err != nil {
			return err
		}
	}
	// A per-tag share is split on a much larger scale than a per-channel value,
	// so the two cannot be compared inside one rule. Rejecting the mix keeps a
	// rule from silently starving whichever scope was written the small number.
	perTagWeights := 0
	perChannelWeights := 0
	for i := range rule.Actions {
		if rule.Actions[i].Weight == nil {
			continue
		}
		if rule.Actions[i].WeightMode == ChannelScheduleWeightModeTag {
			perTagWeights++
			continue
		}
		perChannelWeights++
	}
	if perTagWeights > 0 && perChannelWeights > 0 {
		return fmt.Errorf("rule %s cannot mix per-tag and per-account weights", rule.Name)
	}
	return nil
}

func validateChannelScheduleAction(ruleName string, action *ChannelScheduleAction) error {
	switch action.Mode {
	case "tag":
		action.Tag = strings.TrimSpace(action.Tag)
		if action.Tag == "" {
			return fmt.Errorf("rule %s: a tag action needs a tag", ruleName)
		}
		if len(action.Tag) > 64 {
			return fmt.Errorf("rule %s: the tag is too long", ruleName)
		}
		action.ChannelIds = nil
	case "channel":
		if len(action.ChannelIds) == 0 {
			return fmt.Errorf("rule %s: a channel action needs at least one channel", ruleName)
		}
		if len(action.ChannelIds) > channelScheduleMaxChannelsPerAction {
			return fmt.Errorf("rule %s: too many channels in one action", ruleName)
		}
		unique := make(map[int]struct{}, len(action.ChannelIds))
		ids := make([]int, 0, len(action.ChannelIds))
		for _, id := range action.ChannelIds {
			if id <= 0 {
				return fmt.Errorf("rule %s: channel id must be positive", ruleName)
			}
			if _, duplicated := unique[id]; duplicated {
				continue
			}
			unique[id] = struct{}{}
			ids = append(ids, id)
		}
		slices.Sort(ids)
		action.ChannelIds = ids
		action.Tag = ""
	default:
		return fmt.Errorf("rule %s: unknown action mode %q", ruleName, action.Mode)
	}

	switch action.WeightMode {
	case "", ChannelScheduleWeightModeChannel:
		action.WeightMode = ChannelScheduleWeightModeChannel
	case ChannelScheduleWeightModeTag:
		if action.Mode != "tag" {
			return fmt.Errorf("rule %s: a per-tag weight needs a tag scope", ruleName)
		}
		if action.Weight == nil {
			return fmt.Errorf("rule %s: a per-tag weight needs a share", ruleName)
		}
	default:
		return fmt.Errorf("rule %s: unknown weight mode %q", ruleName, action.WeightMode)
	}

	if action.Priority == nil && action.Weight == nil && action.Enabled == nil {
		return fmt.Errorf("rule %s: an action must change priority, weight or the enabled state", ruleName)
	}
	if action.Priority != nil && (*action.Priority < 0 || *action.Priority > maxChannelSchedulePriority) {
		return fmt.Errorf("rule %s: priority must be between 0 and %d", ruleName, maxChannelSchedulePriority)
	}
	if action.Weight != nil {
		if action.WeightMode == ChannelScheduleWeightModeTag {
			if *action.Weight < 1 || *action.Weight > maxChannelScheduleShare {
				return fmt.Errorf("rule %s: a tag share must be between 1 and %d", ruleName, maxChannelScheduleShare)
			}
		} else if *action.Weight > maxChannelScheduleWeight {
			return fmt.Errorf("rule %s: weight must be between 0 and %d", ruleName, maxChannelScheduleWeight)
		}
	}
	return nil
}

// ChannelScheduleTagShareWeight resolves a per-tag share into the weight each of
// the tag's channels has to carry. The share is scaled before it is divided, so
// two tags configured with the same share end up with the same total weight even
// when they hold different numbers of channels; without the scale a share of 1
// over 14 channels would round to zero.
func ChannelScheduleTagShareWeight(share uint, channelCount int) uint {
	if channelCount <= 0 {
		return 0
	}
	weight := (uint64(share)*channelScheduleShareScale + uint64(channelCount)/2) / uint64(channelCount)
	if weight < 1 {
		return 1
	}
	if weight > maxChannelScheduleWeight {
		return maxChannelScheduleWeight
	}
	return uint(weight)
}

// parseChannelScheduleClock reads an "HH:MM" wall clock value.
func parseChannelScheduleClock(value string) (hour int, minute int, err error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("time %q must use HH:MM", value)
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("time %q has an invalid hour", value)
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("time %q has an invalid minute", value)
	}
	return hour, minute, nil
}

// ChannelScheduleRuleMatches reports whether the rule covers the given instant.
// Rules are evaluated in list order and the first match wins, so this answers
// only the "does this window contain now" question.
func ChannelScheduleRuleMatches(rule ChannelScheduleRule, now time.Time) bool {
	if !rule.Enabled {
		return false
	}
	if len(rule.Weekdays) > 0 && !slices.Contains(rule.Weekdays, int(now.Weekday())) {
		return false
	}
	startHour, startMinute, err := parseChannelScheduleClock(rule.Start)
	if err != nil {
		return false
	}
	endHour, endMinute, err := parseChannelScheduleClock(rule.End)
	if err != nil {
		return false
	}
	current := now.Hour()*60 + now.Minute()
	start := startHour*60 + startMinute
	end := endHour*60 + endMinute
	switch {
	case start == end:
		// An empty window would otherwise read as "never"; treat the whole day
		// as covered so the rule behaves like the 00:00-24:00 entry the console
		// writes for full-day rules.
		return true
	case start < end:
		return current >= start && current < end
	default:
		// The window wraps past midnight: 22:00-07:55 covers 22:00-23:59 and
		// 00:00-07:54.
		return current >= start || current < end
	}
}

// MatchChannelScheduleRule returns the first enabled rule covering the instant,
// or nil when none does.
func MatchChannelScheduleRule(rules []ChannelScheduleRule, now time.Time) *ChannelScheduleRule {
	for i := range rules {
		if ChannelScheduleRuleMatches(rules[i], now) {
			return &rules[i]
		}
	}
	return nil
}

func refreshChannelScheduleSnapshot() error {
	common.OptionMapRWMutex.RLock()
	options := make(map[string]string)
	for key, value := range common.OptionMap {
		if IsChannelScheduleOption(key) {
			options[key] = value
		}
	}
	common.OptionMapRWMutex.RUnlock()
	snapshot, err := BuildChannelSchedule(options)
	if err == nil {
		channelScheduleSnapshot.Store(snapshot)
	}
	return err
}

// UpdateChannelScheduleOptions validates the entire resulting schedule before
// any write, then persists it in one transaction and swaps the snapshot. A
// rejected save leaves both the database and the runtime snapshot untouched.
func UpdateChannelScheduleOptions(values map[string]string) error {
	channelScheduleOptionMutex.Lock()
	defer channelScheduleOptionMutex.Unlock()
	options := maps.Clone(CurrentChannelSchedule().Options)
	for key, value := range values {
		if !IsChannelScheduleOption(key) {
			return fmt.Errorf("not a channel schedule option: %s", key)
		}
		options[key] = value
	}
	snapshot, err := BuildChannelSchedule(options)
	if err != nil {
		return err
	}
	if err := saveChannelScheduleOptions(values); err != nil {
		return err
	}
	channelScheduleSnapshot.Store(snapshot)
	return nil
}

// saveChannelScheduleOptions persists only the keys the administrator changed,
// so untouched defaults stay implicit and keep following the code.
func saveChannelScheduleOptions(changed map[string]string) error {
	if len(changed) == 0 {
		return nil
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		for key, value := range changed {
			option := Option{Key: key}
			if err := tx.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
				return err
			}
			if err := tx.Model(&option).Update("value", value).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for key, value := range changed {
		if err := updateOptionMap(key, value); err != nil {
			return err
		}
	}
	return nil
}
