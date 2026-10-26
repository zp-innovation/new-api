package opencode

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartDeviceAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, DeviceCodePath, request.URL.Path)
		var body map[string]any
		require.NoError(t, decodeBody(request, &body))
		assert.Equal(t, ClientID, body["client_id"])
		assert.Equal(t, true, body["supports_org_scope"])
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"device_code":"device","user_code":"user","verification_uri_complete":"/device?user_code=user","expires_in":60,"interval":2}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	authorization, err := client.StartDeviceAuthorization(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "device", authorization.DeviceCode)
	assert.Equal(t, "user", authorization.UserCode)
	assert.Equal(t, server.URL+"/device?user_code=user", authorization.VerificationURIComplete)
	assert.Equal(t, 60, authorization.ExpiresIn)
	assert.Equal(t, 2, authorization.Interval)
}

func TestPollDeviceTokenHandlesPendingAndSuccess(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		writer.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":600,"org_id":"org"}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.PollDeviceToken(context.Background(), "device")
	require.ErrorIs(t, err, ErrAuthorizationPending)

	token, err := client.PollDeviceToken(context.Background(), "device")
	require.NoError(t, err)
	assert.Equal(t, "access", token.AccessToken)
	assert.Equal(t, "refresh", token.RefreshToken)
	assert.Equal(t, int64(600), token.ExpiresIn)
	assert.Equal(t, "org", token.OrgID)
}

func TestRefreshInvalidGrantIsSignedOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Refresh(context.Background(), "refresh")
	require.ErrorIs(t, err, ErrSignedOut)
}

func TestBuildCredentialSelectsOrganization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case UserPath:
			_, _ = writer.Write([]byte(`{"id":"user","email":"user@example.com"}`))
		case OrganizationsPath:
			_, _ = writer.Write([]byte(`[{"id":"z","name":"Zebra"},{"id":"a","name":"Alpha"}]`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	credential, err := client.BuildCredential(context.Background(), &TokenResponse{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresIn:    600,
		OrgID:        "z",
	}, ModeGo)
	require.NoError(t, err)
	assert.Equal(t, "user", credential.AccountID)
	assert.Equal(t, "user@example.com", credential.Email)
	assert.Equal(t, "z", credential.OrgID)
	assert.Equal(t, "Zebra", credential.OrgName)
	assert.Equal(t, ModeGo, credential.Mode)
}

func TestSelectOrganizationRejectsMissingExplicitID(t *testing.T) {
	_, err := SelectOrganization("missing", []Organization{{ID: "a", Name: "Alpha"}})
	require.Error(t, err)
}

func TestImportAuthExport(t *testing.T) {
	raw := `[{"id":"cred_1","integrationID":"opencode","label":"default","active":true,"value":{"type":"oauth","methodID":"device","access":"access","refresh":"refresh","expires":1893456000000,"metadata":{"server":"https://opencode.ai/console","accountID":"user","email":"user@example.com","orgID":"org","orgName":"Org"}}}]`
	credentials, err := ImportAuthExport(raw)
	require.NoError(t, err)
	require.Len(t, credentials, 1)
	assert.Equal(t, "access", credentials[0].AccessToken)
	assert.Equal(t, "refresh", credentials[0].RefreshToken)
	assert.Equal(t, "user", credentials[0].AccountID)
	assert.Equal(t, "org", credentials[0].OrgID)
	assert.Equal(t, "Org", credentials[0].OrgName)
	assert.NotEmpty(t, credentials[0].ExpiresAt)
}

func TestProtocolRules(t *testing.T) {
	assert.Equal(t, ProtocolResponses, ResolveProtocol(ModeGo, "grok-4.7", nil))
	assert.Equal(t, ProtocolAnthropic, ResolveProtocol(ModeGo, "minimax-m3", nil))
	assert.Equal(t, ProtocolAnthropic, ResolveProtocol(ModeZen, "claude-sonnet-4", nil))
	assert.Equal(t, ProtocolChatCompletions, ResolveProtocol(ModeZen, "qwen3.8-max", nil))
	assert.Equal(t, ProtocolChatCompletions, ResolveProtocol(ModeGo, "glm-5.3", nil))
	assert.True(t, IsUnsupportedModel("gemini-3-pro"))
	assert.True(t, IsUnsupportedModel("jev-test"))
	assert.False(t, IsUnsupportedModel("gpt-5.6-luna"))
}

func TestFetchUsageParsesGoWindows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/zen/go/v1/usage", request.URL.Path)
		assert.Equal(t, "Bearer access", request.Header.Get("Authorization"))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"usage":{"rolling":{"status":"ok","percent":12.5,"resetsAt":"2026-10-16T10:00:00Z"},"weekly":{"percent":30},"monthly":{"percent":40}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	usage, err := client.FetchUsage(context.Background(), &Credential{
		Mode:        ModeGo,
		AccessToken: "access",
	}, server.URL+"/zen/go/v1")
	require.NoError(t, err)
	assert.Equal(t, 12.5, usage.Rolling.Percent)
	assert.Equal(t, 30.0, usage.Weekly.Percent)
	assert.Equal(t, 40.0, usage.Monthly.Percent)
	assert.Equal(t, "2026-10-16T10:00:00Z", usage.Rolling.ResetsAt)
}

func TestCredentialMarshalIsCompactAndModeDefaultsToZen(t *testing.T) {
	credential := &Credential{AccessToken: "access", RefreshToken: "refresh"}
	encoded, err := credential.Marshal()
	require.NoError(t, err)
	assert.NotContains(t, encoded, "\n")
	assert.Contains(t, encoded, `"mode":"zen"`)
	assert.Contains(t, encoded, `"type":"opencode"`)
}

// A token that reports a different organization invalidates the cached display
// name: keeping the organization id there would render "org-2" as the name.
func TestWithTokenClearsTheOrganizationNameWhenTheOrganizationChanges(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	credential := &Credential{AccessToken: "old", OrgID: "org-1", OrgName: "Alpha"}

	switched := credential.WithToken(&TokenResponse{AccessToken: "new", ExpiresIn: 600, OrgID: "org-2"}, now)
	require.Equal(t, "org-2", switched.OrgID)
	assert.Empty(t, switched.OrgName)

	same := credential.WithToken(&TokenResponse{AccessToken: "new", ExpiresIn: 600, OrgID: "org-1"}, now)
	assert.Equal(t, "org-1", same.OrgID)
	assert.Equal(t, "Alpha", same.OrgName, "an unchanged organization keeps its display name")

	kept := credential.WithToken(&TokenResponse{AccessToken: "new", ExpiresIn: 600}, now)
	assert.Equal(t, "org-1", kept.OrgID)
	assert.Equal(t, "Alpha", kept.OrgName)
}

func TestParseCredentialAcceptsRawAPIKey(t *testing.T) {
	credential, err := ParseCredential("oc_sk_test-key")
	require.NoError(t, err)
	assert.True(t, credential.IsAPIKey())
	assert.Equal(t, "oc_sk_test-key", credential.AccessToken)
	assert.Empty(t, credential.Mode)
	assert.False(t, credential.NeedsRefresh(time.Now(), time.Minute))
	assert.NoError(t, credential.ValidateForStorage())
}

func TestParseCredentialAcceptsSub2APIAccountJSON(t *testing.T) {
	credential, err := ParseCredential(
		`{"api_key":"oc_sk_sub2","account_mode":"zen","base_url":"https://opencode.ai/zen/v1","protocol_rules":[{"pattern":"gpt-*","protocol":"responses"}]}`,
	)
	require.NoError(t, err)
	assert.True(t, credential.IsAPIKey())
	assert.Equal(t, ModeZen, credential.Mode)
	assert.Equal(t, "oc_sk_sub2", credential.AccessToken)
	require.Len(t, credential.ProtocolRules, 1)
	assert.Equal(t, ProtocolResponses, credential.ProtocolRules[0].Protocol)
}

func TestParseCredentialInfersGoFromSub2APIBaseURL(t *testing.T) {
	credential, err := ParseCredential(
		`{"api_key":"oc_sk_sub2","base_url":"https://opencode.ai/zen/go/v1"}`,
	)
	require.NoError(t, err)
	assert.Equal(t, ModeGo, credential.Mode)
}

func TestMarshalPreservesAPIKeyAuthType(t *testing.T) {
	credential := &Credential{
		AuthType:    AuthTypeAPIKey,
		AccessToken: "oc_sk_test",
	}
	encoded, err := credential.Marshal()
	require.NoError(t, err)
	assert.Contains(t, encoded, `"auth_type":"api_key"`)
	assert.NotContains(t, encoded, `"refresh_token"`)
}

func TestNormalizeServerRejectsPlainHTTPForRemoteHosts(t *testing.T) {
	_, err := normalizeServer("http://example.com/console")
	require.Error(t, err)
}

func TestNormalizeServerDropsQueryAndFragment(t *testing.T) {
	server, err := normalizeServer("https://opencode.ai/console///?ignored=true#ignored")
	require.NoError(t, err)
	assert.Equal(t, "https://opencode.ai/console", server)
}

func newTestClient(t *testing.T, server string) *Client {
	t.Helper()
	client, err := NewClientWithError(nil, server)
	require.NoError(t, err)
	return client
}

func decodeBody(request *http.Request, target any) error {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return err
	}
	return common.Unmarshal(body, target)
}
