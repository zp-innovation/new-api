package model

import (
	"errors"
	"maps"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func mustLoadScheduleLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	require.NoError(t, err)
	return location
}

func channelScheduleRulesOption(t *testing.T, rulesJSON string) map[string]string {
	t.Helper()
	return map[string]string{channelScheduleOptionRulesKey: rulesJSON}
}

func TestChannelScheduleBuildRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]string
	}{
		{
			name:    "unknown timezone",
			options: map[string]string{channelScheduleOptionTimezoneKey: "Mars/Olympus"},
		},
		{
			name:    "check interval below the minimum",
			options: map[string]string{channelScheduleOptionCheckMinuteKey: "0"},
		},
		{
			name:    "check interval above the maximum",
			options: map[string]string{channelScheduleOptionCheckMinuteKey: "1441"},
		},
		{
			name:    "enabled flag is not a boolean",
			options: map[string]string{channelScheduleOptionEnabledKey: "yes"},
		},
		{
			name:    "rules are not a JSON array",
			options: channelScheduleRulesOption(t, `{"id":"r1"}`),
		},
		{
			name: "rule id is missing",
			options: channelScheduleRulesOption(t,
				`[{"name":"day","start":"08:00","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn","priority":1}]}]`),
		},
		{
			name: "duplicated rule id",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn","priority":1}]},
				  {"id":"r1","name":"b","start":"09:00","end":"10:00","actions":[{"mode":"tag","tag":"wb-intl","priority":1}]}]`),
		},
		{
			name: "rule name is missing",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","start":"08:00","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn","priority":1}]}]`),
		},
		{
			name: "start time is not a clock value",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"8","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn","priority":1}]}]`),
		},
		{
			name: "hour out of range",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"24:00","end":"23:00","actions":[{"mode":"tag","tag":"wb-cn","priority":1}]}]`),
		},
		{
			name: "rule has no action",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[]}]`),
		},
		{
			name: "action mode is unknown",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"group","priority":1}]}]`),
		},
		{
			name: "tag action without a tag",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"tag","priority":1}]}]`),
		},
		{
			name: "channel action without channels",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"channel","channel_ids":[],"priority":1}]}]`),
		},
		{
			name: "channel id is not positive",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"channel","channel_ids":[0],"priority":1}]}]`),
		},
		{
			name: "action changes nothing",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn"}]}]`),
		},
		{
			name: "priority above the bound",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn","priority":1000001}]}]`),
		},
		{
			name: "per-tag weight on a channel scope",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"channel","channel_ids":[1],"weight":1,"weight_mode":"tag"}]}]`),
		},
		{
			name: "per-tag weight without a value",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn","priority":1,"weight_mode":"tag"}]}]`),
		},
		{
			name: "tag share of zero",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn","weight":0,"weight_mode":"tag"}]}]`),
		},
		{
			name: "tag share above the bound",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn","weight":101,"weight_mode":"tag"}]}]`),
		},
		{
			name: "unknown weight mode",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn","weight":1,"weight_mode":"even"}]}]`),
		},
		{
			name: "per-tag and per-account weights in one rule",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","start":"08:00","end":"09:00","actions":[
					{"mode":"tag","tag":"wb-cn","weight":1,"weight_mode":"tag"},
					{"mode":"tag","tag":"wb-intl","weight":1}]}]`),
		},
		{
			name: "weekday out of range",
			options: channelScheduleRulesOption(t,
				`[{"id":"r1","name":"a","weekdays":[7],"start":"08:00","end":"09:00","actions":[{"mode":"tag","tag":"wb-cn","priority":1}]}]`),
		},
		{
			name:    "option key outside the schedule",
			options: map[string]string{"RetryTimes": "2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildChannelSchedule(tc.options)
			assert.Error(t, err)
		})
	}
}

func TestChannelScheduleBuildAcceptsAValidRule(t *testing.T) {
	priority := int64(100)
	weight := uint(10)
	enabled := false
	assert.NotNil(t, priority)
	assert.NotNil(t, weight)
	assert.NotNil(t, enabled)

	snapshot, err := BuildChannelSchedule(map[string]string{
		channelScheduleOptionEnabledKey:     "true",
		channelScheduleOptionTimezoneKey:    "UTC",
		channelScheduleOptionCheckMinuteKey: "5",
		channelScheduleOptionRulesKey: `[{"id":"r1","name":"day","enabled":true,"weekdays":[1,2,3,4,5],
			"start":"07:55","end":"22:00",
			"actions":[{"mode":"tag","tag":"wb-cn","priority":100},
			           {"mode":"channel","channel_ids":[31,15,31],"weight":10,"enabled":false}]}]`,
	})
	require.NoError(t, err)
	assert.True(t, snapshot.Enabled)
	assert.Equal(t, "UTC", snapshot.Timezone)
	assert.Equal(t, 5, snapshot.CheckMinutes)
	require.Len(t, snapshot.Rules, 1)
	require.Len(t, snapshot.Rules[0].Actions, 2)
	// Duplicated channel ids collapse and the list is sorted, so the same rule
	// saved twice produces the same payload.
	assert.Equal(t, []int{15, 31}, snapshot.Rules[0].Actions[1].ChannelIds)
	// A tag action never carries an explicit channel list.
	assert.Empty(t, snapshot.Rules[0].Actions[0].ChannelIds)
}

// TestChannelScheduleTagShareKeepsTagTotalsEqual pins the reason the share mode
// exists: two tags have to end up with the same total weight no matter how many
// channels each one holds, otherwise the split drifts every time an account is
// added or removed.
func TestChannelScheduleTagShareKeepsTagTotalsEqual(t *testing.T) {
	tests := []struct {
		name       string
		leftCount  int
		rightCount int
	}{
		{name: "equal counts", leftCount: 14, rightCount: 14},
		{name: "one side larger", leftCount: 14, rightCount: 7},
		{name: "one side much larger", leftCount: 30, rightCount: 4},
		{name: "one side is a single channel", leftCount: 1, rightCount: 12},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			left := ChannelScheduleTagShareWeight(1, tc.leftCount)
			right := ChannelScheduleTagShareWeight(1, tc.rightCount)
			require.NotZero(t, left)
			require.NotZero(t, right)

			leftTotal := int(left) * tc.leftCount
			rightTotal := int(right) * tc.rightCount
			// Rounding to whole weights leaves a gap smaller than one weight per
			// channel, which is well under a percent of the tag total.
			assert.InDelta(t, leftTotal, rightTotal, float64(max(tc.leftCount, tc.rightCount)))
		})
	}

	t.Run("a larger share gets proportionally more", func(t *testing.T) {
		single := int(ChannelScheduleTagShareWeight(1, 10)) * 10
		double := int(ChannelScheduleTagShareWeight(2, 10)) * 10
		assert.InDelta(t, 2*single, double, float64(single)*0.01)
	})

	t.Run("an empty scope has no weight to split", func(t *testing.T) {
		assert.Zero(t, ChannelScheduleTagShareWeight(1, 0))
	})
}

func TestChannelScheduleDefaultsApplyWhenNothingIsSaved(t *testing.T) {
	snapshot, err := BuildChannelSchedule(nil)
	require.NoError(t, err)
	assert.False(t, snapshot.Enabled)
	assert.Equal(t, defaultChannelScheduleTimezone, snapshot.Timezone)
	assert.Equal(t, defaultChannelScheduleCheckMinutes, snapshot.CheckMinutes)
	assert.Empty(t, snapshot.Rules)
	// Every default key is a schedule option, otherwise reloading them would fail.
	for key := range snapshot.Options {
		assert.True(t, IsChannelScheduleOption(key), key)
	}
}

func TestChannelScheduleRuleWindowMatching(t *testing.T) {
	location := mustLoadScheduleLocation(t, "Asia/Shanghai")
	at := func(hour, minute int, weekday time.Weekday) time.Time {
		// 2026-10-12 is a Monday, so adding whole days reaches the wanted weekday.
		base := time.Date(2026, 10, 12, hour, minute, 0, 0, location)
		return base.AddDate(0, 0, int(weekday)-int(time.Monday))
	}
	rule := ChannelScheduleRule{
		Id:       "r1",
		Name:     "day",
		Enabled:  true,
		Weekdays: []int{1, 2, 3, 4, 5},
		Start:    "07:55",
		End:      "22:00",
		Actions:  []ChannelScheduleAction{{Mode: "tag", Tag: "wb-cn", Priority: new(int64)}},
	}

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{name: "inside the window on a weekday", now: at(8, 0, time.Monday), want: true},
		{name: "at the opening minute", now: at(7, 55, time.Monday), want: true},
		{name: "one minute before opening", now: at(7, 54, time.Monday), want: false},
		{name: "at the closing minute the window is already over", now: at(22, 0, time.Monday), want: false},
		{name: "last minute inside the window", now: at(21, 59, time.Monday), want: true},
		{name: "weekend is excluded", now: at(8, 0, time.Saturday), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ChannelScheduleRuleMatches(rule, tc.now))
		})
	}

	overnight := rule
	overnight.Start = "22:00"
	overnight.End = "07:55"
	overnight.Weekdays = nil
	assert.True(t, ChannelScheduleRuleMatches(overnight, at(23, 30, time.Saturday)), "an empty weekday list means every day")
	assert.True(t, ChannelScheduleRuleMatches(overnight, at(7, 54, time.Monday)), "a wrapping window covers the early hours")
	assert.False(t, ChannelScheduleRuleMatches(overnight, at(7, 55, time.Monday)), "the wrapping window ends at the closing minute")
	assert.False(t, ChannelScheduleRuleMatches(overnight, at(12, 0, time.Monday)), "midday is outside a wrapping window")

	fullDay := overnight
	fullDay.Start = "00:00"
	fullDay.End = "00:00"
	assert.True(t, ChannelScheduleRuleMatches(fullDay, at(12, 0, time.Monday)), "an empty window covers the whole day")

	disabled := rule
	disabled.Enabled = false
	assert.False(t, ChannelScheduleRuleMatches(disabled, at(8, 0, time.Monday)))
}

func TestMatchChannelScheduleRuleTakesTheFirstHit(t *testing.T) {
	location := mustLoadScheduleLocation(t, "UTC")
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, location)
	rules := []ChannelScheduleRule{
		{Id: "off", Name: "off", Enabled: false, Start: "00:00", End: "23:59"},
		{Id: "first", Name: "first", Enabled: true, Start: "08:00", End: "10:00"},
		{Id: "second", Name: "second", Enabled: true, Start: "09:00", End: "11:00"},
	}
	matched := MatchChannelScheduleRule(rules, now)
	require.NotNil(t, matched)
	assert.Equal(t, "first", matched.Id)

	assert.Nil(t, MatchChannelScheduleRule(rules, time.Date(2026, 10, 12, 12, 0, 0, 0, location)))
}

func TestChannelScheduleStatusReasonRecordsTheRule(t *testing.T) {
	assert.Equal(t, "channel schedule rule 白天 · 国内优先", ChannelScheduleStatusReason("白天 · 国内优先"))
}

// TestChannelScheduleDatabaseMatrix proves the schedule persists and reloads on
// every supported database. The rules live in the shared options table, so this
// also covers that a schedule save keeps working next to the other option
// families. Set TEST_MYSQL_DSN and TEST_POSTGRES_DSN to loopback instances to
// run those engines; SQLite always runs. Tables are created with an
// engine-scoped prefix so a shared instance stays clean.
func TestChannelScheduleDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "schedule_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)

			previousDB, previousType := DB, common.MainDatabaseType()
			previousSchedule, previousPolicy := CurrentChannelSchedule(), CurrentRequestPolicy()
			common.OptionMapRWMutex.Lock()
			previousOptions := maps.Clone(common.OptionMap)
			common.OptionMap = map[string]string{}
			common.OptionMapRWMutex.Unlock()
			DB = db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			initCol()
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&Option{}))
				channelScheduleSnapshot.Store(previousSchedule)
				requestPolicySnapshot.Store(previousPolicy)
				common.OptionMapRWMutex.Lock()
				common.OptionMap = previousOptions
				common.OptionMapRWMutex.Unlock()
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				initCol()
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&Option{}))

			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			// Nothing saved yet: the code defaults are in force and no row exists.
			channelScheduleSnapshot.Store(nil)
			assert.Equal(t, defaultChannelScheduleTimezone, CurrentChannelSchedule().Timezone)
			var optionCount int64
			require.NoError(t, db.Model(&Option{}).Count(&optionCount).Error)
			assert.Zero(t, optionCount)

			rules := `[{"id":"r1","name":"day","enabled":true,"weekdays":[1,2,3,4,5],"start":"07:55","end":"22:00",
				"actions":[{"mode":"tag","tag":"wb-cn","priority":100,"future_field":{"keep":true}}]}]`
			require.NoError(t, UpdateChannelScheduleOptions(map[string]string{
				channelScheduleOptionEnabledKey:     "true",
				channelScheduleOptionTimezoneKey:    "UTC",
				channelScheduleOptionCheckMinuteKey: "5",
				channelScheduleOptionRulesKey:       rules,
			}))
			snapshot := CurrentChannelSchedule()
			assert.True(t, snapshot.Enabled)
			assert.Equal(t, "UTC", snapshot.Timezone)
			assert.Equal(t, 5, snapshot.CheckMinutes)
			require.Len(t, snapshot.Rules, 1)
			assert.Equal(t, "r1", snapshot.Rules[0].Id)
			// Unknown fields inside a rule are preserved verbatim, so a future
			// release can add one without this version dropping it on save.
			assert.Contains(t, snapshot.Options[channelScheduleOptionRulesKey], "future_field")

			// Reloading twice must be idempotent and keep the stored values.
			loadOptionsFromDatabase()
			loadOptionsFromDatabase()
			reloaded := CurrentChannelSchedule()
			assert.True(t, reloaded.Enabled)
			assert.Equal(t, "UTC", reloaded.Timezone)
			assert.Equal(t, 5, reloaded.CheckMinutes)
			require.Len(t, reloaded.Rules, 1)
			assert.Equal(t, rules, reloaded.Options[channelScheduleOptionRulesKey])

			var option Option
			require.NoError(t, db.Where(map[string]any{"key": channelScheduleOptionCheckMinuteKey}).First(&option).Error)
			assert.Equal(t, "5", option.Value)

			// A rejected save leaves both the snapshot and the stored row alone.
			// The snapshot is captured here because a reload replaces the pointer.
			stable := CurrentChannelSchedule()
			assert.Error(t, UpdateChannelScheduleOptions(map[string]string{channelScheduleOptionCheckMinuteKey: "0"}))
			assert.Same(t, stable, CurrentChannelSchedule())
			assert.Error(t, UpdateChannelScheduleOptions(map[string]string{channelScheduleOptionTimezoneKey: "Mars/Olympus"}))
			assert.Same(t, stable, CurrentChannelSchedule())
			assert.Error(t, UpdateChannelScheduleOptions(map[string]string{"RetryTimes": "2"}), "a key outside the schedule is rejected")
			assert.Same(t, stable, CurrentChannelSchedule())

			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("schedule_fail", func(tx *gorm.DB) {
				if tx.Statement.Table == "schedule_test_options" {
					tx.AddError(errors.New("save rejected"))
				}
			}))
			assert.Error(t, UpdateChannelScheduleOptions(map[string]string{channelScheduleOptionCheckMinuteKey: "15"}))
			assert.Same(t, stable, CurrentChannelSchedule())
			require.NoError(t, db.Callback().Update().Remove("schedule_fail"))
			option = Option{}
			require.NoError(t, db.Where(map[string]any{"key": channelScheduleOptionCheckMinuteKey}).First(&option).Error)
			assert.Equal(t, "5", option.Value, "a failed save keeps the persisted interval")
			loadOptionsFromDatabase()
			assert.Equal(t, 5, CurrentChannelSchedule().CheckMinutes)
		})
	}
}
