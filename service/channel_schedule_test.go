package service

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func channelScheduleTestDialector(t *testing.T, dialect string) gorm.Dialector {
	t.Helper()
	switch dialect {
	case "sqlite":
		return sqlite.Open(":memory:")
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN not configured")
		}
		return mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN not configured")
		}
		return postgres.Open(dsn)
	default:
		t.Fatalf("unknown dialect %s", dialect)
		return nil
	}
}

func seedChannelScheduleChannels(t *testing.T, channels []*model.Channel) {
	t.Helper()
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(channel).Error)
	}
}

func newChannelScheduleTestChannel(id int, name string, tag string, priority int64) *model.Channel {
	weight := uint(0)
	channel := &model.Channel{
		Id:       id,
		Name:     name,
		Tag:      &tag,
		Priority: &priority,
		Weight:   &weight,
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-test",
	}
	return channel
}

// TestChannelScheduleDatabaseMatrix covers the write path the schedule actually
// uses: it resolves a scope to channels, diffs the fields it is asked to change
// and writes only those. SQLite always runs; MySQL and PostgreSQL run when
// TEST_MYSQL_DSN and TEST_POSTGRES_DSN point at loopback instances.
func TestChannelScheduleDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db, err := gorm.Open(channelScheduleTestDialector(t, dialect), &gorm.Config{
				NamingStrategy: schema.NamingStrategy{TablePrefix: "sched_svc_" + dialect + "_"},
			})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)

			previousDB := model.DB
			model.DB = db
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&model.Channel{}, &model.Ability{}))
				model.DB = previousDB
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

			// Each read starts from a fresh value: reusing a struct that already
			// carries a primary key would make GORM add it as a second condition.
			loadChannel := func(id int) *model.Channel {
				var channel model.Channel
				require.NoError(t, db.First(&channel, id).Error)
				return &channel
			}

			priority := int64(100)
			seedChannelScheduleChannels(t, []*model.Channel{
				newChannelScheduleTestChannel(1, "wb-cn-a", "wb-cn", 0),
				newChannelScheduleTestChannel(2, "wb-cn-b", "wb-cn", 0),
				newChannelScheduleTestChannel(3, "wb-intl-a", "wb-intl", 100),
			})

			// A tag scope resolves to exactly the channels carrying that tag.
			targets, err := resolveChannelScheduleTargets(model.ChannelScheduleAction{Mode: "tag", Tag: "wb-cn"})
			require.NoError(t, err)
			require.Len(t, targets, 2)

			// An empty tag must never be treated as a scope: on a real install
			// most channels carry no tag, and matching them all would rewrite
			// unrelated channels.
			targets, err = resolveChannelScheduleTargets(model.ChannelScheduleAction{Mode: "tag", Tag: "  "})
			require.NoError(t, err)
			assert.Empty(t, targets)

			rule := model.ChannelScheduleRule{
				Id:      "r1",
				Name:    "day",
				Enabled: true,
				Start:   "00:00",
				End:     "00:00",
				Actions: []model.ChannelScheduleAction{
					{Mode: "tag", Tag: "wb-cn", Priority: &priority},
				},
			}
			changes, err := applyChannelScheduleRule(rule)
			require.NoError(t, err)
			require.Len(t, changes, 2, "both untagged-priority channels move to 100")
			for _, change := range changes {
				assert.Equal(t, "priority", change.Field)
				assert.Equal(t, "0", change.From)
				assert.Equal(t, "100", change.To)
				assert.Equal(t, "tag: wb-cn (2)", change.Scope)
			}

			// The write reached the channel row and the ability rows that drive
			// channel selection.
			assert.Equal(t, int64(100), loadChannel(1).GetPriority())
			var ability model.Ability
			require.NoError(t, db.Where("channel_id = ?", 1).First(&ability).Error)
			require.NotNil(t, ability.Priority)
			assert.Equal(t, int64(100), *ability.Priority)

			// A second run with the same rule is a no-op: no writes, no history.
			changes, err = applyChannelScheduleRule(rule)
			require.NoError(t, err)
			assert.Empty(t, changes, "a rule that already holds produces no changes")

			// Only the fields named in the action are touched.
			weight := uint(7)
			weightRule := rule
			weightRule.Actions = []model.ChannelScheduleAction{{Mode: "tag", Tag: "wb-cn", Weight: &weight}}
			changes, err = applyChannelScheduleRule(weightRule)
			require.NoError(t, err)
			require.Len(t, changes, 2)
			for _, change := range changes {
				assert.Equal(t, "weight", change.Field)
			}
			afterWeight := loadChannel(1)
			assert.Equal(t, int64(100), afterWeight.GetPriority(), "weight-only action leaves priority alone")
			assert.Equal(t, 7, afterWeight.GetWeight())

			// A per-tag share is split across the channels the tag holds right
			// now, so two tags with different channel counts still reach the
			// same total weight: 2 wb-cn channels at 5000 against 1 wb-intl
			// channel at 10000.
			share := uint(1)
			shareRule := rule
			shareRule.Actions = []model.ChannelScheduleAction{
				{Mode: "tag", Tag: "wb-cn", Weight: &share, WeightMode: model.ChannelScheduleWeightModeTag},
				{Mode: "tag", Tag: "wb-intl", Weight: &share, WeightMode: model.ChannelScheduleWeightModeTag},
			}
			changes, err = applyChannelScheduleRule(shareRule)
			require.NoError(t, err)
			require.Len(t, changes, 3, "every channel in both tags takes its split weight")
			cnWeight := loadChannel(1).GetWeight()
			assert.Equal(t, cnWeight, loadChannel(2).GetWeight(), "channels of one tag carry the same weight")
			assert.Equal(t, 2*cnWeight, loadChannel(3).GetWeight(), "the smaller tag splits the same total across fewer channels")
			assert.Equal(t, 10000, 2*cnWeight, "a share of 1 keeps both tag totals at the scale")

			// The split already holds, so the next run writes nothing.
			changes, err = applyChannelScheduleRule(shareRule)
			require.NoError(t, err)
			assert.Empty(t, changes, "a tag share that already holds produces no changes")

			// A channel scope disables exactly the named channels.
			disabled := false
			statusRule := rule
			statusRule.Actions = []model.ChannelScheduleAction{{Mode: "channel", ChannelIds: []int{3}, Enabled: &disabled}}
			changes, err = applyChannelScheduleRule(statusRule)
			require.NoError(t, err)
			require.Len(t, changes, 1)
			assert.Equal(t, "status", changes[0].Field)
			assert.Equal(t, "enabled", changes[0].From)
			assert.Equal(t, "disabled", changes[0].To)
			afterDisable := loadChannel(3)
			assert.Equal(t, common.ChannelStatusManuallyDisabled, afterDisable.Status)
			assert.Equal(t, model.ChannelScheduleStatusReason("day"), afterDisable.GetOtherInfo()["status_reason"])

			// Re-enabling through the same path restores the previous status.
			enabled := true
			statusRule.Actions = []model.ChannelScheduleAction{{Mode: "channel", ChannelIds: []int{3}, Enabled: &enabled}}
			changes, err = applyChannelScheduleRule(statusRule)
			require.NoError(t, err)
			require.Len(t, changes, 1)
			assert.Equal(t, "enabled", changes[0].To)
			assert.Equal(t, common.ChannelStatusEnabled, loadChannel(3).Status)

			// A channel scope naming a missing channel keeps working for the
			// channels that still exist.
			statusRule.Actions = []model.ChannelScheduleAction{{Mode: "channel", ChannelIds: []int{3, 9999}, Enabled: &disabled}}
			changes, err = applyChannelScheduleRule(statusRule)
			require.NoError(t, err)
			require.Len(t, changes, 1)
			assert.Equal(t, 3, changes[0].ChannelId)
		})
	}
}

func TestResolveChannelScheduleTargetsIgnoresUnknownMode(t *testing.T) {
	targets, err := resolveChannelScheduleTargets(model.ChannelScheduleAction{Mode: "group", Tag: "wb-cn"})
	require.NoError(t, err)
	assert.Empty(t, targets)
}

// TestChannelScheduleHistoryTaskKeepsOneActiveSlot pins the invariant that made
// a lost race dangerous: only one task of a type may hold the active key, so a
// run must reuse a pending row instead of inserting a second one. Without this,
// a run that failed to claim would leave the row pending forever and every
// later run would fail on the unique index.
func TestChannelScheduleHistoryTaskKeepsOneActiveSlot(t *testing.T) {
	// No table prefix here: FinishSystemTask checks the lock through a raw
	// EXISTS subquery against the literal system_task_locks table, so a prefixed
	// schema would not be found. An in-memory SQLite database is isolated anyway.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		require.NoError(t, db.Migrator().DropTable(&model.SystemTask{}, &model.SystemTaskLock{}))
		model.DB = previousDB
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	rule := &model.ChannelScheduleRule{Id: "r1", Name: "day"}
	first, err := channelScheduleHistoryTask(rule, "UTC", false)
	require.NoError(t, err)
	require.NotNil(t, first)

	reused, err := channelScheduleHistoryTask(rule, "UTC", false)
	require.NoError(t, err)
	require.NotNil(t, reused, "a pending task is reused, not duplicated")
	assert.Equal(t, first.ID, reused.ID)

	_, claimed, err := model.ClaimSystemTask(first.ID, model.SystemTaskTypeChannelSchedule, "runner-a", common.GetTimestamp()+300)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.FinishSystemTask(first.TaskID, "runner-a", model.SystemTaskStatusSucceeded, map[string]any{"changes": 0}, ""))

	// Finishing released the active key, so the next run records a new row.
	second, err := channelScheduleHistoryTask(rule, "UTC", true)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.NotEqual(t, first.ID, second.ID)

	// A run already in flight blocks the new one instead of stacking up.
	_, claimed, err = model.ClaimSystemTask(second.ID, model.SystemTaskTypeChannelSchedule, "runner-b", common.GetTimestamp()+300)
	require.NoError(t, err)
	require.True(t, claimed)
	blocked, err := channelScheduleHistoryTask(rule, "UTC", false)
	require.NoError(t, err)
	assert.Nil(t, blocked)
}
