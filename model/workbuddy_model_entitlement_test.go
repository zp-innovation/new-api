package model

import (
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestWorkBuddyModelEntitlementDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var dialector gorm.Dialector
			switch dialect {
			case "sqlite":
				dialector = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				dialector = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				dialector = postgres.Open(dsn)
			}
			db, err := gorm.Open(dialector, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "workbuddy_entitlement_test_"}})
			require.NoError(t, err)
			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			require.NoError(t, db.AutoMigrate(&WorkBuddyModelEntitlement{}))
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&WorkBuddyModelEntitlement{}))
				DB = previousDB
				common.SetMainDatabaseType(previousType)
			})

			now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
			require.NoError(t, MarkWorkBuddyModelBlocked(48, "uid-1", "cn", "gpt-6-luna", 11158, "personal_free_user", now))
			require.NoError(t, MarkWorkBuddyModelBlocked(48, "uid-2", "cn", "gpt-6-luna", 11158, "personal_free_user", now))
			require.NoError(t, RefreshWorkBuddyModelEntitlements(now))

			blocked, err := WorkBuddyAccountsBlockedForModel(48, "gpt-6-luna", now)
			require.NoError(t, err)
			assert.Equal(t, map[string]bool{"uid-1": true, "uid-2": true}, blocked)

			require.NoError(t, ClearWorkBuddyModelBlocked(48, "uid-1", "gpt-6-luna"))
			require.NoError(t, RefreshWorkBuddyModelEntitlements(now))
			blocked, err = WorkBuddyAccountsBlockedForModel(48, "gpt-6-luna", now)
			require.NoError(t, err)
			assert.Equal(t, map[string]bool{"uid-2": true}, blocked)

			require.NoError(t, RefreshWorkBuddyModelEntitlements(now.Add(WorkBuddyEntitlementTTL+time.Minute)))
			blocked, err = WorkBuddyAccountsBlockedForModel(48, "gpt-6-luna", now.Add(WorkBuddyEntitlementTTL+time.Minute))
			require.NoError(t, err)
			assert.Empty(t, blocked)
		})
	}
}

func setupWorkBuddyEntitlementTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&WorkBuddyModelEntitlement{}))
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		workBuddyEntitlementMarks.Range(func(key, _ any) bool {
			workBuddyEntitlementMarks.Delete(key)
			return true
		})
		DB = previousDB
	})
}

func TestWorkBuddyChannelHasUsableCredential(t *testing.T) {
	setupWorkBuddyEntitlementTestDB(t)
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	channel := &Channel{Id: 48, Type: constant.ChannelTypeWorkBuddy, Key: `{"uid":"uid-1"}` + "\n" + `{"uid":"uid-2"}`}
	require.NoError(t, RefreshWorkBuddyModelEntitlements(now))
	require.NoError(t, MarkWorkBuddyModelBlocked(48, "uid-1", "cn", "gpt-6-luna", 11158, "personal_free_user", now))
	assert.True(t, WorkBuddyChannelHasUsableCredential(channel, "gpt-6-luna", now), "uid-2 is still eligible")

	require.NoError(t, MarkWorkBuddyModelBlocked(48, "uid-2", "cn", "gpt-6-luna", 11158, "personal_free_user", now))
	assert.False(t, WorkBuddyChannelHasUsableCredential(channel, "gpt-6-luna", now), "all accounts are blocked")
	assert.True(t, WorkBuddyChannelHasUsableCredential(channel, "deepseek-v4.1-flash", now), "another model is unaffected")
}

func TestGetNextEnabledKeyExcludingSkipsBlockedAccount(t *testing.T) {
	channel := &Channel{
		Id:   48,
		Type: constant.ChannelTypeWorkBuddy,
		Key:  `{"uid":"uid-1"}` + "\n" + `{"uid":"uid-2"}`,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeyMode: constant.MultiKeyModeRandom,
		},
	}
	key, index, apiErr := channel.GetNextEnabledKeyExcluding(map[string]bool{"uid-1": true})
	require.Nil(t, apiErr)
	require.Equal(t, 1, index)
	require.Contains(t, key, `"uid":"uid-2"`)

	_, _, apiErr = channel.GetNextEnabledKeyExcluding(map[string]bool{"uid-1": true, "uid-2": true})
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeChannelNoAvailableKey, apiErr.GetErrorCode())
}
