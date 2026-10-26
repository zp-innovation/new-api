package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	occore "github.com/QuantumNous/new-api/pkg/opencode"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRefreshOpenCodeCredentialKeepsMetadataAndRotatesTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/auth/device/token", request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":600,"org_id":"org-1"}`))
	}))
	defer server.Close()

	original := openCodeOAuthClient
	openCodeOAuthClient = func(mode, rawServer, proxyURL string) (*occore.Client, error) {
		return occore.NewClientWithError(nil, server.URL)
	}
	t.Cleanup(func() { openCodeOAuthClient = original })

	raw := `{"type":"opencode","mode":"go","server":"` + server.URL + `","access_token":"old-access","refresh_token":"old-refresh","account_id":"user-1","email":"user@example.com","org_id":"org-1","org_name":"Example Org"}`
	credential, err := RefreshOpenCodeCredential(context.Background(), raw, "")
	require.NoError(t, err)
	assert.Equal(t, "new-access", credential.AccessToken)
	assert.Equal(t, "new-refresh", credential.RefreshToken)
	assert.Equal(t, "user-1", credential.AccountID)
	assert.Equal(t, "user@example.com", credential.Email)
	assert.Equal(t, "org-1", credential.OrgID)
	assert.Equal(t, "Example Org", credential.OrgName)
	assert.NotEmpty(t, credential.ExpiresAt)
}

func TestGetOpenCodeChannelModelsFallsBackToStaticList(t *testing.T) {
	channel := &model.Channel{
		Type: constant.ChannelTypeOpenCode,
		Key:  `{"type":"opencode","mode":"zen","server":"http://example.com","access_token":"access","refresh_token":"refresh"}`,
	}
	models, err := GetOpenCodeChannelModelsForChannel(context.Background(), channel)
	require.NoError(t, err)
	assert.NotEmpty(t, models)
}

func TestGetOpenCodeChannelUsageRejectsZen(t *testing.T) {
	channel := &model.Channel{
		Id:   1,
		Type: constant.ChannelTypeOpenCode,
		Key:  `{"type":"opencode","mode":"zen","access_token":"access","refresh_token":"refresh"}`,
	}
	// The service reads from the database by channel id, so point the database
	// at a temporary SQLite file with this one row.
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	require.NoError(t, db.Create(channel).Error)
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	_, err = GetOpenCodeChannelUsage(context.Background(), channel.Id)
	require.ErrorIs(t, err, occore.ErrUsageUnavailable)
}

func TestRefreshOpenCodeCredentialRejectsAPIKey(t *testing.T) {
	_, err := RefreshOpenCodeCredential(context.Background(), "oc_sk_test", "")
	require.ErrorContains(t, err, "API key credentials do not refresh")
}

// A channel can hold credentials in different modes. A Zen credential must not
// hide the Go accounts that follow or precede it: the view reports the Go
// accounts it could read and lists the skipped modes.
func TestGetOpenCodeChannelUsageReadsGoCredentialFromMixedModeChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/usage", request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"rolling":{"status":"ok","percent":42,"resetsAt":"2030-01-01T00:00:00Z"},"weekly":{"percent":10},"monthly":{"percent":5}}`))
	}))
	defer server.Close()

	original := openCodeOAuthClient
	openCodeOAuthClient = func(mode, rawServer, proxyURL string) (*occore.Client, error) {
		require.Equal(t, occore.ModeGo, mode)
		return occore.NewClientWithError(nil, server.URL)
	}
	t.Cleanup(func() { openCodeOAuthClient = original })

	goLine := `{"type":"opencode","mode":"go","access_token":"go-access","refresh_token":"go-refresh","account_id":"user-go"}`
	zenLine := `{"type":"opencode","mode":"zen","access_token":"zen-access","refresh_token":"zen-refresh"}`
	baseURL := server.URL
	channel := &model.Channel{
		Id:      2,
		Type:    constant.ChannelTypeOpenCode,
		Key:     goLine + "\n" + zenLine,
		BaseURL: &baseURL,
	}
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	require.NoError(t, db.Create(channel).Error)
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	usage, err := GetOpenCodeChannelUsage(context.Background(), channel.Id)
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, occore.ModeGo, usage.Mode)
	require.Len(t, usage.Entries, 1)
	assert.Equal(t, "user-go", usage.Entries[0].AccountID)
	assert.InDelta(t, 42, usage.Entries[0].Rolling.Percent, 0.001)
	assert.Equal(t, []string{"zen"}, usage.SkippedModes)
}

// The OAuth wrappers share cached proxy clients, so the timeout has to be set on
// a copy: mutating the shared client would leak one channel's timeout into the
// next request.
func TestOAuthHTTPClientCopiesTheBaseClient(t *testing.T) {
	base := &http.Client{Timeout: time.Second}
	client := oauthHTTPClient(base, 5*time.Second)
	require.NotSame(t, base, client)
	assert.Equal(t, 5*time.Second, client.Timeout)
	assert.Equal(t, time.Second, base.Timeout)

	fallback := oauthHTTPClient(nil, 5*time.Second)
	require.NotNil(t, fallback)
	assert.Equal(t, 5*time.Second, fallback.Timeout)
}
