package operation_setting

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// WorkBuddyTaskSetting configures the automatic WorkBuddy task and welfare
// runner. The two realms are configured apart on purpose: the international
// realm only has the daily activity chat (ten to fifteen calls per account),
// while the China realm signs in, reports activity, travels, keeps the streak
// and can run the growth centre (three to seven times that). One shared budget
// or schedule would either waste the international allowance or throttle the
// China routine.
//
// The growth centre and the night-owl task are off by default: they issue the
// most upstream calls per account and are the ones most likely to attract the
// upstream's abuse controls, so an operator opts into them explicitly.
type WorkBuddyTaskSetting struct {
	Enabled bool `json:"enabled"`

	// International realm.
	AutoDailyChat         bool   `json:"auto_daily_chat"`
	DailyChatWeb          bool   `json:"daily_chat_web"`
	ScheduleHoursIntl     string `json:"schedule_hours_intl"`
	DailyRequestLimitIntl int    `json:"daily_request_limit_intl"`

	// China realm.
	AutoCheckin         bool   `json:"auto_checkin"`
	AutoActivityReport  bool   `json:"auto_activity_report"`
	AutoTravel          bool   `json:"auto_travel"`
	AutoStreakBonus     bool   `json:"auto_streak_bonus"`
	AutoGrowthTasks     bool   `json:"auto_growth_tasks"`
	AutoNightCat        bool   `json:"auto_night_cat"`
	ScheduleHoursCN     string `json:"schedule_hours_cn"`
	DailyRequestLimitCN int    `json:"daily_request_limit_cn"`
	GrowthRequestLimit  int    `json:"growth_request_limit"`

	// Shared.
	NightHour         int `json:"night_hour"`
	AccountGapSeconds int `json:"account_gap_seconds"`
	RunRetentionDays  int `json:"run_retention_days"`
}

var workBuddyTaskSetting = WorkBuddyTaskSetting{
	Enabled: true,

	AutoDailyChat:         true,
	DailyChatWeb:          true,
	ScheduleHoursIntl:     "09:00,21:00",
	DailyRequestLimitIntl: 15,

	AutoCheckin:         true,
	AutoActivityReport:  true,
	AutoTravel:          true,
	AutoStreakBonus:     true,
	AutoGrowthTasks:     false,
	AutoNightCat:        false,
	ScheduleHoursCN:     "09:00,21:00",
	DailyRequestLimitCN: 40,
	GrowthRequestLimit:  60,

	NightHour:         1,
	AccountGapSeconds: 1,
	RunRetentionDays:  90,
}

func init() {
	config.GlobalConfig.Register("workbuddy_task_setting", &workBuddyTaskSetting)
}

// GetWorkBuddyTaskSetting returns the current runner configuration.
func GetWorkBuddyTaskSetting() *WorkBuddyTaskSetting {
	return &workBuddyTaskSetting
}

// RequestLimit returns the daily call budget for one account of the given realm.
func (s *WorkBuddyTaskSetting) RequestLimit(cn bool) int {
	if s == nil {
		return 0
	}
	if !cn {
		return max(s.DailyRequestLimitIntl, 0)
	}
	if s.AutoGrowthTasks && s.GrowthRequestLimit > 0 {
		return s.GrowthRequestLimit
	}
	return max(s.DailyRequestLimitCN, 0)
}

// ScheduleHours returns the configured local hours of one realm as a set,
// dropping anything unreadable or out of range.
func (s *WorkBuddyTaskSetting) ScheduleHours(cn bool) string {
	if s == nil {
		return ""
	}
	if cn {
		return s.ScheduleHoursCN
	}
	return s.ScheduleHoursIntl
}

// ScheduledHours parses one realm's schedule into a set of local hours.
func (s *WorkBuddyTaskSetting) ScheduledHours(cn bool) map[int]bool {
	hours := map[int]bool{}
	for part := range strings.SplitSeq(s.ScheduleHours(cn), ",") {
		text := strings.TrimSpace(part)
		if text == "" {
			continue
		}
		hourText, _, found := strings.Cut(text, ":")
		if !found {
			hourText = text
		}
		hour, err := strconv.Atoi(strings.TrimSpace(hourText))
		if err != nil || hour < 0 || hour > 23 {
			continue
		}
		hours[hour] = true
	}
	return hours
}

// RunsAt reports whether the given local hour is scheduled for one realm.
func (s *WorkBuddyTaskSetting) RunsAt(hour int, cn bool) bool {
	return s.ScheduledHours(cn)[hour]
}
