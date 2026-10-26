package model

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestWorkBuddyUsageSchemaMatrix proves the two new WorkBuddy tables migrate on
// every supported database, that a second startup does not change the schema,
// and that the uniqueness guarantees hold on each engine. Set TEST_MYSQL_DSN and
// TEST_POSTGRES_DSN to loopback instances to run those engines; SQLite always
// runs. The tables are created under engine-suffixed names so a shared test
// instance stays clean.
func TestWorkBuddyUsageSchemaMatrix(t *testing.T) {
	cases := []struct {
		name string
		env  string
		open func(t *testing.T) *gorm.DB
	}{
		{name: "sqlite", open: func(t *testing.T) *gorm.DB {
			db, err := gorm.Open(sqlite.Open(t.TempDir()+"/workbuddy-usage.db"), &gorm.Config{})
			require.NoError(t, err)
			return db
		}},
		{name: "mysql", env: "TEST_MYSQL_DSN", open: func(t *testing.T) *gorm.DB {
			db, err := gorm.Open(mysql.Open(os.Getenv("TEST_MYSQL_DSN")), &gorm.Config{})
			require.NoError(t, err)
			return db
		}},
		{name: "postgres", env: "TEST_POSTGRES_DSN", open: func(t *testing.T) *gorm.DB {
			db, err := gorm.Open(postgres.Open(os.Getenv("TEST_POSTGRES_DSN")), &gorm.Config{})
			require.NoError(t, err)
			return db
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" && strings.TrimSpace(os.Getenv(tc.env)) == "" {
				t.Skip(tc.env + " is not configured")
			}
			db := tc.open(t)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })

			var version string
			query := "SELECT version()"
			if db.Dialector.Name() == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("%s version: %s", db.Dialector.Name(), version)

			suffix := db.Dialector.Name()
			stateTable := "wb_usage_state_" + suffix
			dailyTable := "wb_usage_daily_" + suffix
			eventTable := "wb_usage_event_" + suffix
			for _, table := range []string{stateTable, dailyTable, eventTable} {
				_ = db.Migrator().DropTable(table)
			}
			t.Cleanup(func() {
				for _, table := range []string{stateTable, dailyTable, eventTable} {
					_ = db.Migrator().DropTable(table)
				}
			})

			// Two startups in a row must both succeed and leave the same shape.
			for range 2 {
				require.NoError(t, db.Table(stateTable).AutoMigrate(&WorkBuddyAccountState{}))
				require.NoError(t, db.Table(dailyTable).AutoMigrate(&WorkBuddyUsageDaily{}))
				require.NoError(t, db.Table(eventTable).AutoMigrate(&WorkBuddyLimitEvent{}))
			}
			for _, table := range []string{stateTable, dailyTable, eventTable} {
				assert.True(t, db.Migrator().HasTable(table), table)
			}

			daily := WorkBuddyUsageDaily{
				ChannelId: 1, AccountUid: "uid-1", Model: "deepseek-v4.1-flash", Day: "2026-10-12",
				Realm: "intl", Requests: 3, PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30,
			}
			require.NoError(t, db.Table(dailyTable).Create(&daily).Error)
			duplicate := daily
			duplicate.Id = 0
			require.Error(t, db.Table(dailyTable).Create(&duplicate).Error, "the (channel, account, model, day) key must reject a duplicate")

			event := WorkBuddyLimitEvent{
				ChannelId: 1, AccountUid: "uid-1", Model: "deepseek-v4.1-flash",
				Realm: "intl", ResetAt: 1_791_000_000, CappedAt: 1_790_900_000,
				SampleTokens: 199_000_000, Source: "relay",
			}
			require.NoError(t, db.Table(eventTable).Create(&event).Error)
			duplicateEvent := event
			duplicateEvent.Id = 0
			require.Error(t, db.Table(eventTable).Create(&duplicateEvent).Error, "the (channel, account, model, reset) key must reject a duplicate")

			state := WorkBuddyAccountState{ChannelId: 1, AccountUid: "uid-1", Realm: "intl"}
			require.NoError(t, db.Table(stateTable).Create(&state).Error)
			duplicateState := state
			duplicateState.Id = 0
			require.Error(t, db.Table(stateTable).Create(&duplicateState).Error, "the (channel, account) key must reject a duplicate")
		})
	}
}
