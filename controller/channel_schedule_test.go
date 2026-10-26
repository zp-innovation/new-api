package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelScheduleConfigResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Options map[string]string `json:"options"`
		Status  struct {
			Enabled      bool   `json:"enabled"`
			Timezone     string `json:"timezone"`
			Now          string `json:"now"`
			Consistent   bool   `json:"consistent"`
			Expectations []any  `json:"expectations"`
		} `json:"status"`
	} `json:"data"`
}

// setupChannelScheduleTestDB wires an isolated in-memory database for the
// handlers. It deliberately avoids the shared audit-database helper: that one
// hands out a file under t.TempDir(), whose handle Windows refuses to unlink
// during cleanup, which would make these assertions fail for an unrelated
// reason.
func setupChannelScheduleTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Option{}, &model.Channel{}, &model.Ability{}, &model.SystemTask{}, &model.SystemTaskLock{},
	))

	previousDB, previousMain := model.DB, common.MainDatabaseType()
	previousOptions := common.OptionMap
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousMain)
		common.OptionMap = previousOptions
		require.NoError(t, sqlDB.Close())
	})
}

// resetChannelSchedule puts the schedule back to its defaults through the same
// API the console uses. The snapshot is process state, so a test that saves a
// rule has to put it back for the next one.
func resetChannelSchedule(t *testing.T) {
	t.Helper()
	require.NoError(t, model.UpdateChannelScheduleOptions(map[string]string{
		"channel_schedule_setting.enabled":       "false",
		"channel_schedule_setting.timezone":      "Asia/Shanghai",
		"channel_schedule_setting.check_minutes": "1",
		"channel_schedule_setting.rules":         "[]",
	}))
}

// TestChannelScheduleAPIContract pins the envelope the console consumes: the
// settings live under data.options and the applied-state view under
// data.status. No other layer asserts this shape, so it is checked here.
func TestChannelScheduleAPIContract(t *testing.T) {
	setupChannelScheduleTestDB(t)
	resetChannelSchedule(t)
	t.Cleanup(func() { resetChannelSchedule(t) })

	recorder := modelManagementRequest(t, GetChannelSchedule, http.MethodGet, "/api/option/channel_schedule", nil, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response channelScheduleConfigResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	for _, key := range []string{
		"channel_schedule_setting.enabled",
		"channel_schedule_setting.timezone",
		"channel_schedule_setting.check_minutes",
		"channel_schedule_setting.rules",
	} {
		assert.Contains(t, response.Data.Options, key)
	}
	assert.Equal(t, "Asia/Shanghai", response.Data.Status.Timezone)
	assert.NotEmpty(t, response.Data.Status.Now)
	assert.NotNil(t, response.Data.Status.Expectations, "the status view always carries a list, empty included")

	// A save round-trips through the same envelope.
	rules := `[{"id":"r1","name":"day","enabled":true,"weekdays":[1,2,3,4,5],"start":"07:55","end":"22:00","actions":[{"mode":"tag","tag":"wb-cn","priority":100}]}]`
	recorder = modelManagementRequest(t, UpdateChannelSchedule, http.MethodPatch, "/api/option/channel_schedule",
		map[string]any{"options": map[string]string{
			"channel_schedule_setting.enabled":  "true",
			"channel_schedule_setting.rules":    rules,
			"channel_schedule_setting.timezone": "UTC",
		}}, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	response = channelScheduleConfigResponse{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "true", response.Data.Options["channel_schedule_setting.enabled"])
	assert.Equal(t, rules, response.Data.Options["channel_schedule_setting.rules"])
	assert.True(t, response.Data.Status.Enabled)
	assert.Equal(t, "UTC", response.Data.Status.Timezone)
	require.Len(t, response.Data.Status.Expectations, 1, "one configured action scope is reported")

	// An invalid value is refused with the reason and leaves the saved rules alone.
	recorder = modelManagementRequest(t, UpdateChannelSchedule, http.MethodPatch, "/api/option/channel_schedule",
		map[string]any{"options": map[string]string{"channel_schedule_setting.check_minutes": "0"}}, nil)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var refused struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &refused))
	assert.False(t, refused.Success)
	assert.NotEmpty(t, refused.Message)
	assert.Equal(t, rules, model.CurrentChannelSchedule().Options["channel_schedule_setting.rules"])

	// A body that is not the documented shape is rejected as a bad request.
	recorder = modelManagementRequest(t, UpdateChannelSchedule, http.MethodPatch, "/api/option/channel_schedule", "not-an-object", nil)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

// TestRunChannelScheduleNowReportsTheOutcome covers the manual check: it always
// answers with both the run result and a fresh status view, so the console can
// render the outcome without a second request.
func TestRunChannelScheduleNowReportsTheOutcome(t *testing.T) {
	setupChannelScheduleTestDB(t)
	resetChannelSchedule(t)
	t.Cleanup(func() { resetChannelSchedule(t) })

	// The schedule is disabled by default, so the run reports why it did nothing
	// instead of reporting a failure.
	recorder := modelManagementRequest(t, RunChannelScheduleNow, http.MethodPost, "/api/option/channel_schedule/run", nil, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Result struct {
				Timezone  string `json:"timezone"`
				CheckedAt int64  `json:"checked_at"`
				Skipped   string `json:"skipped"`
				Changes   []any  `json:"changes"`
			} `json:"result"`
			Status struct {
				Enabled bool `json:"enabled"`
			} `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "schedule disabled", response.Data.Result.Skipped)
	assert.NotZero(t, response.Data.Result.CheckedAt)
	assert.NotNil(t, response.Data.Result.Changes)
	assert.Equal(t, "Asia/Shanghai", response.Data.Result.Timezone)
	assert.False(t, response.Data.Status.Enabled)
}
