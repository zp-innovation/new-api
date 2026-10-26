package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	occore "github.com/QuantumNous/new-api/pkg/opencode"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A failed poll has to flag the row as stale without discarding the numbers the
// last successful poll recorded. Writing a fresh row would zero remain and
// total, which reads as a drained account.
func TestSyncChannelAccountQuotasKeepsPreviousNumbersOnFetchFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if requests.Add(1) > 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"rolling":{"status":"ok","percent":42,"resetsAt":"2030-01-01T00:00:00Z"}}`))
	}))
	defer server.Close()

	original := openCodeOAuthClient
	openCodeOAuthClient = func(mode, rawServer, proxyURL string) (*occore.Client, error) {
		return occore.NewClientWithError(nil, server.URL)
	}
	t.Cleanup(func() { openCodeOAuthClient = original })

	baseURL := server.URL
	channel := &model.Channel{
		Id:      11,
		Name:    "opencode-go",
		Type:    constant.ChannelTypeOpenCode,
		Status:  common.ChannelStatusEnabled,
		Key:     `{"type":"opencode","mode":"go","access_token":"go-access","refresh_token":"go-refresh","account_id":"user-go"}`,
		BaseURL: &baseURL,
	}
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelAccountQuota{}))
	require.NoError(t, db.Create(channel).Error)
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	polled, err := SyncChannelAccountQuotas(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, polled)

	rows, err := model.ListChannelAccountQuotas()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	before := rows[0]
	require.Equal(t, model.ChannelAccountQuotaStatusOK, before.Status)
	require.Positive(t, before.Remain)
	require.Positive(t, before.Total)

	polled, err = SyncChannelAccountQuotas(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, polled)

	rows, err = model.ListChannelAccountQuotas()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	after := rows[0]
	assert.Equal(t, model.ChannelAccountQuotaStatusError, after.Status)
	assert.NotEmpty(t, after.Error)
	assert.InDelta(t, before.Remain, after.Remain, 1e-6)
	assert.InDelta(t, before.Total, after.Total, 1e-6)
	assert.Equal(t, before.ResetTime, after.ResetTime)
}
