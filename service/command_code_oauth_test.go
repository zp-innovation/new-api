package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	ccore "github.com/QuantumNous/new-api/pkg/commandcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func overrideCommandCodeClient(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	original := commandCodeClientFactory
	commandCodeClientFactory = func(providerBaseURL, apiBaseURL, proxyURL string) (*ccore.Client, error) {
		return ccore.NewClient(server.Client(), server.URL, server.URL), nil
	}
	t.Cleanup(func() {
		commandCodeClientFactory = original
		server.Close()
	})
	return server
}

func TestValidateCommandCodeCredentialFillsAccountMetadata(t *testing.T) {
	overrideCommandCodeClient(t, func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, ccore.WhoamiPath, request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"user":{"id":"u1","userName":"Alice"},"org":{"id":"org-1","name":"Example Org"}}`))
	})

	credential, err := ccore.ParseCredential("user_test")
	require.NoError(t, err)
	validated, err := ValidateCommandCodeCredential(context.Background(), credential, "")
	require.NoError(t, err)
	assert.Equal(t, "u1", validated.UserID)
	assert.Equal(t, "Alice", validated.UserName)
	assert.Equal(t, "org-1", validated.OrgID)
	assert.Equal(t, "Example Org", validated.OrgName)
	assert.NotEmpty(t, validated.AuthenticatedAt)
}

func TestCommandCodeLoginFlowCompletesOnce(t *testing.T) {
	overrideCommandCodeClient(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"user":{"id":"u1","userName":"Alice"}}`))
	})

	started, err := StartCommandCodeLogin("http://127.0.0.1:3000/callback", "")
	require.NoError(t, err)
	require.NotEmpty(t, started.State)
	assert.Contains(t, started.AuthURL, "callback=")
	assert.Contains(t, started.AuthURL, "mode=redirect")

	require.NoError(t, CompleteCommandCodeLogin(started.State, "user_test", "u1", "Alice", "cli"))
	status, err := PollCommandCodeLogin(context.Background(), started.State, "")
	require.NoError(t, err)
	assert.Equal(t, CommandCodeLoginReady, status.Status)
	require.NotNil(t, status.Credential)
	assert.Equal(t, "user_test", status.Credential.APIKey)

	replay, err := PollCommandCodeLogin(context.Background(), started.State, "")
	require.NoError(t, err)
	assert.Equal(t, CommandCodeLoginExpired, replay.Status)
}

func TestGetCommandCodeChannelModelsFallsBackToStaticList(t *testing.T) {
	overrideCommandCodeClient(t, func(writer http.ResponseWriter, request *http.Request) {
		http.NotFound(writer, request)
	})
	channel := &model.Channel{
		Type: constant.ChannelTypeCommandCode,
		Key:  "user_test",
	}
	models, err := GetCommandCodeChannelModelsForChannel(context.Background(), channel)
	require.NoError(t, err)
	assert.NotEmpty(t, models)
}

func TestGetCommandCodeChannelModelsReadsTheCatalog(t *testing.T) {
	overrideCommandCodeClient(t, func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, ccore.ModelsPath, request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":[{"id":"claude-sonnet-4-6","supported_endpoints":["/messages"]},{"id":"gpt-5.6-luna","supported_endpoints":["/chat/completions","/responses"]}]}`))
	})
	channel := &model.Channel{
		Type: constant.ChannelTypeCommandCode,
		Key:  "user_test",
	}
	models, err := GetCommandCodeChannelModelsForChannel(context.Background(), channel)
	require.NoError(t, err)
	assert.Equal(t, []string{"claude-sonnet-4-6", "gpt-5.6-luna"}, models)

	set, ok := ccore.ResolveProtocolSet("claude-sonnet-4-6", nil)
	require.True(t, ok)
	assert.Equal(t, []string{ccore.ProtocolAnthropic}, set)
}

func TestCommandCodeChannelUsesOfficialHost(t *testing.T) {
	custom := "https://relay.example.com/v1"
	assert.False(t, commandCodeChannelUsesOfficialHost(&model.Channel{BaseURL: &custom}))
	assert.True(t, commandCodeChannelUsesOfficialHost(&model.Channel{}))
}
