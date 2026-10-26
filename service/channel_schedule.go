package service

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// channelScheduleLeaseSeconds is how long a claimed run owns the task before
// another node may steal it, and channelScheduleLeaseRenewInterval is how often
// the owner pushes that deadline out again. Renewing matters when applying the
// rule outlives one lease: without it the owner loses the lock mid-run and its
// FinishSystemTask is rejected.
const channelScheduleLeaseSeconds int64 = 300

const channelScheduleLeaseRenewInterval = 90 * time.Second

// ChannelScheduleChange is one field transition the schedule applied to one
// channel. The console reads the same shape back from the system task history.
type ChannelScheduleChange struct {
	ChannelId   int    `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Scope       string `json:"scope"`
	Field       string `json:"field"`
	From        string `json:"from"`
	To          string `json:"to"`
}

// ChannelScheduleRunResult is the payload stored on the system task record.
type ChannelScheduleRunResult struct {
	RuleId    string                  `json:"rule_id,omitempty"`
	RuleName  string                  `json:"rule_name,omitempty"`
	Timezone  string                  `json:"timezone"`
	CheckedAt int64                   `json:"checked_at"`
	Changes   []ChannelScheduleChange `json:"changes"`
	Skipped   string                  `json:"skipped,omitempty"`
}

// ChannelScheduleFieldExpectation is one field of one scope, as the schedule
// wants it versus what the database currently holds.
type ChannelScheduleFieldExpectation struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Matches  bool   `json:"matches"`
}

// ChannelScheduleExpectation is one action scope resolved to concrete channels.
// Participates is false for scopes that only other rules cover, so the console
// can label them as not part of the schedule right now.
type ChannelScheduleExpectation struct {
	Scope        string                            `json:"scope"`
	Mode         string                            `json:"mode"`
	Tag          string                            `json:"tag,omitempty"`
	ChannelIds   []int                             `json:"channel_ids"`
	Participates bool                              `json:"participates"`
	Consistent   bool                              `json:"consistent"`
	Fields       []ChannelScheduleFieldExpectation `json:"fields"`
}

// ChannelScheduleStatus is the read model behind the applied-state view.
type ChannelScheduleStatus struct {
	Enabled         bool                         `json:"enabled"`
	Timezone        string                       `json:"timezone"`
	Now             string                       `json:"now"`
	MatchedRuleId   string                       `json:"matched_rule_id,omitempty"`
	MatchedRuleName string                       `json:"matched_rule_name,omitempty"`
	Consistent      bool                         `json:"consistent"`
	LastRunAt       int64                        `json:"last_run_at"`
	Expectations    []ChannelScheduleExpectation `json:"expectations"`
}

// channelScheduleRunMutex keeps two runs in one process from racing on the same
// channels; the system task lock covers the other nodes.
var channelScheduleRunMutex sync.Mutex

func channelScheduleScopeLabel(action model.ChannelScheduleAction, tagCount int) string {
	if action.Mode == "tag" {
		if tagCount >= 0 {
			return fmt.Sprintf("tag: %s (%d)", action.Tag, tagCount)
		}
		return "tag: " + action.Tag
	}
	ids := make([]string, 0, len(action.ChannelIds))
	for _, id := range action.ChannelIds {
		ids = append(ids, fmt.Sprintf("#%d", id))
	}
	return strings.Join(ids, ", ")
}

// channelScheduleStatusTarget maps the schedule's enabled flag onto the channel
// status value the rest of the gateway uses.
func channelScheduleStatusTarget(enabled bool) int {
	if enabled {
		return common.ChannelStatusEnabled
	}
	return common.ChannelStatusManuallyDisabled
}

// resolveChannelScheduleTargets turns an action scope into concrete channels.
// A tag is only ever used to select rows here: the tag column holds a single
// value per channel, and an empty tag would otherwise match every untagged
// channel, so an empty tag resolves to nothing.
func resolveChannelScheduleTargets(action model.ChannelScheduleAction) ([]*model.Channel, error) {
	switch action.Mode {
	case "tag":
		if strings.TrimSpace(action.Tag) == "" {
			return nil, nil
		}
		channels, err := model.GetChannelsByTag(action.Tag, true, true)
		if err != nil {
			return nil, err
		}
		return channels, nil
	case "channel":
		channels := make([]*model.Channel, 0, len(action.ChannelIds))
		for _, id := range action.ChannelIds {
			channel, err := model.GetChannelById(id, true)
			if err != nil {
				// A rule that names a deleted channel keeps working for the
				// channels that still exist.
				continue
			}
			channels = append(channels, channel)
		}
		return channels, nil
	default:
		return nil, nil
	}
}

// channelScheduleActionWeight resolves the weight one action wants on each
// channel of its scope. A per-tag share is split across the channels the tag
// currently holds, so the ratio between two tags holds while accounts come and
// go; every other mode writes the configured number as-is.
func channelScheduleActionWeight(action model.ChannelScheduleAction, channelCount int) (uint, bool) {
	if action.Weight == nil {
		return 0, false
	}
	if action.WeightMode == model.ChannelScheduleWeightModeTag {
		return model.ChannelScheduleTagShareWeight(*action.Weight, channelCount), true
	}
	return *action.Weight, true
}

// expectedChannelScheduleFields returns the field expectations for one channel
// under one action, in a stable order. The weight is passed in already resolved
// because a per-tag share depends on how many channels the scope holds.
func expectedChannelScheduleFields(channel *model.Channel, action model.ChannelScheduleAction, weight uint, hasWeight bool) []ChannelScheduleFieldExpectation {
	fields := make([]ChannelScheduleFieldExpectation, 0, 3)
	if action.Priority != nil {
		expected := fmt.Sprintf("%d", *action.Priority)
		actual := fmt.Sprintf("%d", channel.GetPriority())
		fields = append(fields, ChannelScheduleFieldExpectation{Field: "priority", Expected: expected, Actual: actual, Matches: expected == actual})
	}
	if hasWeight {
		expected := fmt.Sprintf("%d", weight)
		actual := fmt.Sprintf("%d", channel.GetWeight())
		fields = append(fields, ChannelScheduleFieldExpectation{Field: "weight", Expected: expected, Actual: actual, Matches: expected == actual})
	}
	if action.Enabled != nil {
		target := channelScheduleStatusTarget(*action.Enabled)
		expected := channelScheduleStatusLabels[target]
		actual := channelScheduleStatusLabels[channel.Status]
		if actual == "" {
			actual = fmt.Sprintf("status %d", channel.Status)
		}
		matches := channel.Status == target
		if !matches && target == common.ChannelStatusEnabled && channel.Status == common.ChannelStatusAutoDisabled {
			// An "enable" action leaves an auto-disabled channel alone (see
			// applyChannelScheduleAction), so it is not drift.
			expected = actual
			matches = true
		}
		fields = append(fields, ChannelScheduleFieldExpectation{Field: "status", Expected: expected, Actual: actual, Matches: matches})
	}
	return fields
}

var channelScheduleStatusLabels = map[int]string{
	common.ChannelStatusEnabled:          "enabled",
	common.ChannelStatusManuallyDisabled: "disabled",
	common.ChannelStatusAutoDisabled:     "auto-disabled",
}

// ChannelScheduleStatusReport renders the applied-state view: the rule covering
// now, plus every configured scope with its expected and actual values. Scopes
// that only other rules cover are reported but marked as not participating.
func ChannelScheduleStatusReport() (*ChannelScheduleStatus, error) {
	snapshot := model.CurrentChannelSchedule()
	location, err := time.LoadLocation(snapshot.Timezone)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone: %s", snapshot.Timezone)
	}
	now := time.Now().In(location)
	report := &ChannelScheduleStatus{
		Enabled:      snapshot.Enabled,
		Timezone:     snapshot.Timezone,
		Now:          now.Format("2006-01-02 15:04:05"),
		Consistent:   true,
		Expectations: make([]ChannelScheduleExpectation, 0, len(snapshot.Rules)),
	}
	if latest, err := model.GetLatestSystemTask(model.SystemTaskTypeChannelSchedule); err == nil && latest != nil {
		report.LastRunAt = latest.UpdatedAt
	}

	matched := model.MatchChannelScheduleRule(snapshot.Rules, now)
	if snapshot.Enabled && matched != nil {
		report.MatchedRuleId = matched.Id
		report.MatchedRuleName = matched.Name
	}
	for ruleIndex := range snapshot.Rules {
		rule := snapshot.Rules[ruleIndex]
		isMatchedRule := matched != nil && matched.Id == rule.Id && rule.Enabled
		for actionIndex := range rule.Actions {
			action := rule.Actions[actionIndex]
			channels, err := resolveChannelScheduleTargets(action)
			if err != nil {
				return nil, err
			}
			expectation := ChannelScheduleExpectation{
				Mode:         action.Mode,
				Tag:          action.Tag,
				Participates: isMatchedRule,
				Consistent:   true,
				ChannelIds:   make([]int, 0, len(channels)),
				Fields:       []ChannelScheduleFieldExpectation{},
			}
			if action.Mode == "channel" {
				expectation.ChannelIds = slices.Clone(action.ChannelIds)
			}
			for _, channel := range channels {
				if action.Mode == "tag" {
					expectation.ChannelIds = append(expectation.ChannelIds, channel.Id)
				}
			}
			expectation.Scope = channelScheduleScopeLabel(action, len(channels))
			if expectation.Participates {
				expectation.Fields = channelScheduleExpectedFieldsForScope(channels, action)
				for _, field := range expectation.Fields {
					if !field.Matches {
						expectation.Consistent = false
						report.Consistent = false
					}
				}
			}
			report.Expectations = append(report.Expectations, expectation)
		}
	}
	// Nothing participates when no rule covers the current time, so the report
	// stays consistent by construction: fields are only compared for the scope
	// the matched rule owns.
	return report, nil
}

// channelScheduleExpectedFieldsForScope folds the per-channel expectations of
// one scope into a single row. Channels that already agree are summarized as
// "already matching", so a drifted field is never hidden behind a consistent
// sibling channel.
func channelScheduleExpectedFieldsForScope(channels []*model.Channel, action model.ChannelScheduleAction) []ChannelScheduleFieldExpectation {
	if len(channels) == 0 {
		return []ChannelScheduleFieldExpectation{}
	}
	weight, hasWeight := channelScheduleActionWeight(action, len(channels))
	perChannel := make([][]ChannelScheduleFieldExpectation, 0, len(channels))
	for _, channel := range channels {
		perChannel = append(perChannel, expectedChannelScheduleFields(channel, action, weight, hasWeight))
	}
	rows := make([]ChannelScheduleFieldExpectation, 0, len(perChannel[0]))
	for fieldIndex := range perChannel[0] {
		row := ChannelScheduleFieldExpectation{Field: perChannel[0][fieldIndex].Field}
		matching := 0
		expectedValues := make([]string, 0, len(perChannel))
		actualValues := make([]string, 0, len(perChannel))
		for _, fields := range perChannel {
			if fieldIndex >= len(fields) {
				continue
			}
			// expectedChannelScheduleFields owns the match decision: a field can
			// be satisfied without its strings being equal, which is exactly how
			// an auto-disabled channel counts as intentionally left alone.
			if fields[fieldIndex].Matches {
				matching++
				continue
			}
			expectedValues = appendUniqueString(expectedValues, fields[fieldIndex].Expected)
			actualValues = appendUniqueString(actualValues, fields[fieldIndex].Actual)
		}
		row.Matches = len(expectedValues) == 0
		row.Expected = summarizeChannelScheduleValues(expectedValues, matching, len(channels))
		row.Actual = summarizeChannelScheduleValues(actualValues, matching, len(channels))
		rows = append(rows, row)
	}
	return rows
}

func appendUniqueString(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func summarizeChannelScheduleValues(values []string, matching int, total int) string {
	if len(values) == 0 {
		return fmt.Sprintf("matching (%d)", matching)
	}
	joined := strings.Join(values, ", ")
	if matching == 0 {
		return joined
	}
	return fmt.Sprintf("%s | matching (%d)", joined, matching)
}

// channelScheduleHistoryTask returns the system task this run should record
// under, or nil when another run is currently in flight.
//
// Only one task of a type may hold the active key at a time, so an existing
// row is reused instead of inserting a second one. A run that failed to claim
// leaves the task pending, and the next run picks that same row up and finishes
// it — which is what keeps a lost race from blocking every later run on the
// unique index.
func channelScheduleHistoryTask(rule *model.ChannelScheduleRule, timezone string, manual bool) (*model.SystemTask, error) {
	active, err := model.GetActiveSystemTask(model.SystemTaskTypeChannelSchedule)
	if err != nil {
		return nil, err
	}
	if active == nil {
		return model.CreateSystemTask(model.SystemTaskTypeChannelSchedule, map[string]any{
			"rule_id":   rule.Id,
			"rule_name": rule.Name,
			"timezone":  timezone,
			"manual":    manual,
		}, map[string]any{"changes": 0})
	}
	if active.Status != model.SystemTaskStatusPending {
		return nil, nil
	}
	return active, nil
}

// RunChannelSchedule applies the rule covering now. A manual run uses the same
// path as the scheduled one, so "check now" can never produce a state the
// timer would not.
func RunChannelSchedule(manual bool) (*ChannelScheduleRunResult, error) {
	channelScheduleRunMutex.Lock()
	defer channelScheduleRunMutex.Unlock()

	snapshot := model.CurrentChannelSchedule()
	location, err := time.LoadLocation(snapshot.Timezone)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone: %s", snapshot.Timezone)
	}
	now := time.Now().In(location)
	result := &ChannelScheduleRunResult{
		Timezone:  snapshot.Timezone,
		CheckedAt: time.Now().Unix(),
		Changes:   []ChannelScheduleChange{},
	}
	if !snapshot.Enabled {
		result.Skipped = "schedule disabled"
		return result, nil
	}
	rule := model.MatchChannelScheduleRule(snapshot.Rules, now)
	if rule == nil {
		result.Skipped = "no rule matches the current time"
		return result, nil
	}
	result.RuleId = rule.Id
	result.RuleName = rule.Name

	task, err := channelScheduleHistoryTask(rule, snapshot.Timezone, manual)
	if err != nil {
		return nil, err
	}
	if task == nil {
		result.Skipped = "a previous run is still active"
		return result, nil
	}
	// The runner id has to be unique per attempt. A timestamp with second
	// granularity let two nodes start in the same second and then finish each
	// other's task, because FinishSystemTask matches on the runner id.
	suffix, err := common.GenerateRandomCharsKey(8)
	if err != nil {
		return nil, err
	}
	runnerID := fmt.Sprintf("channel-schedule-%d-%s", time.Now().UnixNano(), suffix)
	_, claimed, err := model.ClaimSystemTask(task.ID, model.SystemTaskTypeChannelSchedule, runnerID, common.GetTimestamp()+channelScheduleLeaseSeconds)
	if err != nil {
		return nil, err
	}
	if !claimed {
		// The row stays pending and keeps the per-type active key, so the next
		// run reuses it instead of creating a second task and hitting the unique
		// index. That is what stops a run that lost the race from wedging the
		// schedule until someone cleans the table by hand.
		result.Skipped = "another run holds the lock"
		return result, nil
	}

	stopLeaseRenewal := startChannelScheduleLeaseRenewal(task.TaskID, runnerID)
	changes, applyErr := applyChannelScheduleRule(*rule)
	stopLeaseRenewal()
	result.Changes = changes
	if applyErr != nil {
		finishErr := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, result, applyErr.Error())
		if finishErr != nil {
			common.SysError(common.LogText("failed to finish channel schedule task: %s", finishErr.Error()))
		}
		return result, applyErr
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, ""); err != nil {
		common.SysError(common.LogText("failed to finish channel schedule task: %s", err.Error()))
	}
	return result, nil
}

// startChannelScheduleLeaseRenewal pushes the task lease out until the returned
// stop function runs. The caller must stop it before finishing the task, so a
// renewal cannot race with the release that FinishSystemTask performs.
func startChannelScheduleLeaseRenewal(taskID string, runnerID string) func() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(channelScheduleLeaseRenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				lockUntil := common.GetTimestamp() + channelScheduleLeaseSeconds
				if err := model.RenewSystemTaskLock(taskID, runnerID, lockUntil); err != nil {
					common.SysError(common.LogText("channel schedule could not renew the task lease: %s", err.Error()))
					return
				}
			}
		}
	})
	return func() {
		close(done)
		wg.Wait()
	}
}

// applyChannelScheduleRule diffs every action scope against the database and
// writes only what differs, so a rule that already holds produces no writes and
// no history noise.
func applyChannelScheduleRule(rule model.ChannelScheduleRule) ([]ChannelScheduleChange, error) {
	changes := make([]ChannelScheduleChange, 0)
	for actionIndex := range rule.Actions {
		action := rule.Actions[actionIndex]
		channels, err := resolveChannelScheduleTargets(action)
		if err != nil {
			return changes, err
		}
		scope := channelScheduleScopeLabel(action, len(channels))
		weight, hasWeight := channelScheduleActionWeight(action, len(channels))
		for _, channel := range channels {
			channelChanges, err := applyChannelScheduleAction(channel, action, weight, hasWeight, scope, rule.Name)
			if err != nil {
				return changes, err
			}
			changes = append(changes, channelChanges...)
		}
	}
	return changes, nil
}

func applyChannelScheduleAction(channel *model.Channel, action model.ChannelScheduleAction, weight uint, hasWeight bool, scope string, ruleName string) ([]ChannelScheduleChange, error) {
	changes := make([]ChannelScheduleChange, 0, 3)
	name := channel.Name

	priorityChanged := action.Priority != nil && channel.GetPriority() != *action.Priority
	weightChanged := hasWeight && channel.GetWeight() != int(weight)
	if priorityChanged || weightChanged {
		// Capture the old values before the row is mutated; the change record is
		// only appended once the write below has succeeded.
		fromPriority := channel.GetPriority()
		fromWeight := channel.GetWeight()
		if action.Priority != nil {
			channel.Priority = action.Priority
		}
		if hasWeight {
			channel.Weight = &weight
		}
		// Update persists the changed columns, rebuilds this channel's abilities
		// from the new values, and reloads the row.
		if err := channel.Update(); err != nil {
			return changes, err
		}
		if priorityChanged {
			changes = append(changes, ChannelScheduleChange{ChannelId: channel.Id, ChannelName: name, Scope: scope, Field: "priority", From: fmt.Sprintf("%d", fromPriority), To: fmt.Sprintf("%d", *action.Priority)})
		}
		if weightChanged {
			changes = append(changes, ChannelScheduleChange{ChannelId: channel.Id, ChannelName: name, Scope: scope, Field: "weight", From: fmt.Sprintf("%d", fromWeight), To: fmt.Sprintf("%d", weight)})
		}
		model.CacheUpdateChannel(channel)
	}

	if action.Enabled != nil {
		target := channelScheduleStatusTarget(*action.Enabled)
		// A channel the gateway auto-disabled was taken out of rotation by
		// upstream failures. Re-enabling it every tick would put a dead
		// credential straight back into service, so an "enable" action only
		// covers channels that are still enabled or were disabled by hand.
		if target == common.ChannelStatusEnabled && channel.Status == common.ChannelStatusAutoDisabled {
			return changes, nil
		}
		if channel.Status != target {
			from := channelScheduleStatusLabels[channel.Status]
			if from == "" {
				from = fmt.Sprintf("status %d", channel.Status)
			}
			reason := model.ChannelScheduleStatusReason(ruleName)
			if !model.UpdateChannelStatus(channel.Id, "", target, reason) {
				common.SysLog(common.LogText("channel schedule could not set status: channel_id=%d, status=%d", channel.Id, target))
				return changes, nil
			}
			// The row is only reported as changed after the update went through.
			changes = append(changes, ChannelScheduleChange{ChannelId: channel.Id, ChannelName: name, Scope: scope, Field: "status", From: from, To: channelScheduleStatusLabels[target]})
		}
	}
	return changes, nil
}
