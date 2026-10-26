package antigravity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(serverURL string) *Client {
	client := NewClient(&http.Client{Timeout: 5 * time.Second})
	client.tokenURL = serverURL + "/token"
	client.userInfoURL = serverURL + "/userinfo"
	client.baseURLs = []string{serverURL}
	client.forwardURLs = []string{serverURL}
	client.onboardInterval = time.Millisecond
	return client
}

func TestExchangeCodePostsPKCEForm(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer server.Close()

	token, err := newTestClient(server.URL).ExchangeCode(context.Background(), "code-1", "verifier-1")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if token.AccessToken != "at-1" || token.RefreshToken != "rt-1" {
		t.Fatalf("token = %+v", token)
	}

	expected := map[string]string{
		"client_id":     ClientID,
		"client_secret": ClientSecret(),
		"code":          "code-1",
		"redirect_uri":  RedirectURI,
		"grant_type":    "authorization_code",
		"code_verifier": "verifier-1",
	}
	for key, want := range expected {
		if got := gotForm.Get(key); got != want {
			t.Errorf("form %s = %q, want %q", key, got, want)
		}
	}

	// Expiry must carry the safety window so a token is never used in its
	// final minutes.
	deadline := token.ExpiresAt()
	if delta := time.Until(deadline); delta < 50*time.Minute || delta > 55*time.Minute {
		t.Fatalf("ExpiresAt is %v away, want about 55 minutes", delta)
	}
}

func TestExchangeCodeSurfacesUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	_, err := newTestClient(server.URL).ExchangeCode(context.Background(), "bad", "verifier")
	if err == nil {
		t.Fatal("ExchangeCode accepted a 400 response")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("error %q does not carry the upstream body", err)
	}
}

func TestExchangeCodeRejectsTokenWithoutAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token_type":"Bearer"}`))
	}))
	defer server.Close()

	if _, err := newTestClient(server.URL).ExchangeCode(context.Background(), "code", "verifier"); err == nil {
		t.Fatal("a token response without access_token was accepted")
	}
}

func TestRefreshTokenRequiresRefreshToken(t *testing.T) {
	if _, err := NewClient(nil).RefreshToken(context.Background(), "  "); err == nil {
		t.Fatal("RefreshToken accepted an empty refresh token")
	}
}

func TestRefreshTokenPostsGrantType(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		_, _ = w.Write([]byte(`{"access_token":"at-2","expires_in":3600}`))
	}))
	defer server.Close()

	token, err := newTestClient(server.URL).RefreshToken(context.Background(), "rt-1")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if token.AccessToken != "at-2" {
		t.Fatalf("access token = %q", token.AccessToken)
	}
	if got := gotForm.Get("grant_type"); got != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", got)
	}
	if got := gotForm.Get("refresh_token"); got != "rt-1" {
		t.Errorf("refresh_token = %q", got)
	}
}

func TestGetUserInfoSendsBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer at-1" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"email":"user@example.com"}`))
	}))
	defer server.Close()

	info, err := newTestClient(server.URL).GetUserInfo(context.Background(), "at-1")
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	if info.Email != "user@example.com" {
		t.Fatalf("email = %q", info.Email)
	}
}

func TestLoadCodeAssistAcceptsStringAndObjectTiers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1internal:loadCodeAssist") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "antigravity/") {
			t.Errorf("User-Agent = %q", got)
		}
		var payload LoadCodeAssistRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Metadata.IDEType != "ANTIGRAVITY" || payload.Metadata.IDEName != "antigravity" {
			t.Errorf("metadata = %+v", payload.Metadata)
		}
		_, _ = w.Write([]byte(`{
			"currentTier": {"id": "g1-pro-tier", "name": "Pro"},
			"allowedTiers": ["free-tier", {"id": "g1-pro-tier"}],
			"cloudaicompanionProject": "proj-1"
		}`))
	}))
	defer server.Close()

	resp, err := newTestClient(server.URL).LoadCodeAssist(context.Background(), "at-1")
	if err != nil {
		t.Fatalf("LoadCodeAssist: %v", err)
	}
	if resp.TierID() != "g1-pro-tier" {
		t.Errorf("TierID = %q", resp.TierID())
	}
	if resp.ProjectID() != "proj-1" {
		t.Errorf("ProjectID = %q", resp.ProjectID())
	}
	if len(resp.AllowedTiers) != 2 || resp.AllowedTiers[0].ID != "free-tier" {
		t.Errorf("AllowedTiers = %+v", resp.AllowedTiers)
	}
	if got := TierIDToPlanType(resp.TierID()); got != "Pro" {
		t.Errorf("plan type = %q, want Pro", got)
	}
}

func TestLoadCodeAssistFallsBackToNextBaseURL(t *testing.T) {
	var firstHits, secondHits int32

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&firstHits, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondHits, 1)
		_, _ = w.Write([]byte(`{"cloudaicompanionProject":"proj-2"}`))
	}))
	defer second.Close()

	client := newTestClient(first.URL)
	client.baseURLs = []string{first.URL, second.URL}

	resp, err := client.LoadCodeAssist(context.Background(), "at-1")
	if err != nil {
		t.Fatalf("LoadCodeAssist: %v", err)
	}
	if resp.ProjectID() != "proj-2" {
		t.Fatalf("ProjectID = %q, want proj-2", resp.ProjectID())
	}
	if atomic.LoadInt32(&firstHits) != 1 || atomic.LoadInt32(&secondHits) != 1 {
		t.Fatalf("hits = (%d, %d), want one each", firstHits, secondHits)
	}
}

func TestLoadCodeAssistDoesNotFallbackOnClientError(t *testing.T) {
	var secondHits int32

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondHits, 1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer second.Close()

	client := newTestClient(first.URL)
	client.baseURLs = []string{first.URL, second.URL}

	if _, err := client.LoadCodeAssist(context.Background(), "at-1"); err == nil {
		t.Fatal("403 was treated as success")
	}
	if atomic.LoadInt32(&secondHits) != 0 {
		t.Fatal("403 triggered a base URL fallback; only transport faults and 429/408/404/5xx should")
	}
}

func TestOnboardUserPollsUntilDone(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			_, _ = w.Write([]byte(`{"done":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"done":true,"response":{"cloudaicompanionProject":{"id":"proj-3"}}}`))
	}))
	defer server.Close()

	projectID, err := newTestClient(server.URL).OnboardUser(context.Background(), "at-1", "free-tier")
	if err != nil {
		t.Fatalf("OnboardUser: %v", err)
	}
	if projectID != "proj-3" {
		t.Fatalf("project id = %q, want proj-3", projectID)
	}
	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Fatalf("onboardUser calls = %d, want at least 2", got)
	}
}

func TestOnboardUserRequiresTier(t *testing.T) {
	if _, err := NewClient(nil).OnboardUser(context.Background(), "at-1", "  "); err == nil {
		t.Fatal("OnboardUser accepted an empty tier id")
	}
}

func TestOnboardUserReportsMissingProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"done":true,"response":{}}`))
	}))
	defer server.Close()

	if _, err := newTestClient(server.URL).OnboardUser(context.Background(), "at-1", "free-tier"); err == nil {
		t.Fatal("OnboardUser accepted a completed response without a project id")
	}
}

func TestFetchAvailableModelsBuildsQuotaRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Project string `json:"project"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Project != "proj-1" {
			t.Errorf("project = %q, want proj-1", payload.Project)
		}
		_, _ = w.Write([]byte(`{
			"models": {
				"gemini-3-pro-high": {"displayName":"Gemini 3 Pro High","recommended":true,
					"quotaInfo":{"remainingFraction":0.75,"resetTime":"2026-10-10T00:00:00Z"}},
				"claude-sonnet-4-6": {"displayName":"Claude Sonnet 4.6","supportsThinking":true}
			},
			"deprecatedModelIds": {"gemini-3-pro-preview":{"newModelId":"gemini-3-pro-high"}}
		}`))
	}))
	defer server.Close()

	resp, err := newTestClient(server.URL).FetchAvailableModels(context.Background(), "at-1", "proj-1")
	if err != nil {
		t.Fatalf("FetchAvailableModels: %v", err)
	}
	if len(resp.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(resp.Models))
	}

	rows := QuotaFromModels(resp)
	if len(rows) != 2 {
		t.Fatalf("quota rows = %d, want 2", len(rows))
	}
	// Rows are sorted by model name: claude-* before gemini-*.
	if rows[0].Model != "claude-sonnet-4-6" || rows[1].Model != "gemini-3-pro-high" {
		t.Fatalf("rows are not sorted: %+v", rows)
	}
	if rows[1].RemainingFraction != 0.75 || !rows[1].Recommended {
		t.Fatalf("gemini row = %+v", rows[1])
	}
	if !rows[0].SupportsThinking {
		t.Fatalf("claude row lost supportsThinking: %+v", rows[0])
	}
}

func TestFetchAvailableModelsRequiresProject(t *testing.T) {
	if _, err := NewClient(nil).FetchAvailableModels(context.Background(), "at-1", ""); err == nil {
		t.Fatal("FetchAvailableModels accepted an empty project id")
	}
}

func TestFetchAvailableModelsFromURLUsesSelectedEndpoint(t *testing.T) {
	var gotPath, gotAuthorization string
	var gotProject string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		var payload struct {
			Project string `json:"project"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		gotProject = payload.Project
		_, _ = w.Write([]byte(`{"models":{"gemini-4-argon-high":{"supportsThinking":true}}}`))
	}))
	defer server.Close()

	resp, err := newTestClient(server.URL).FetchAvailableModelsFromURL(context.Background(), server.URL+"/", "at-selected", "proj-selected")
	if err != nil {
		t.Fatalf("FetchAvailableModelsFromURL: %v", err)
	}
	if _, ok := resp.Models["gemini-4-argon-high"]; !ok {
		t.Fatalf("models = %+v, want selected endpoint catalogue", resp.Models)
	}
	if gotPath != "/v1internal:fetchAvailableModels" {
		t.Fatalf("path = %q, want fetchAvailableModels", gotPath)
	}
	if gotAuthorization != "Bearer at-selected" {
		t.Fatalf("authorization = %q, want selected token", gotAuthorization)
	}
	if gotProject != "proj-selected" {
		t.Fatalf("project = %q, want selected project", gotProject)
	}
}

func TestFetchAvailableModelsFromURLDoesNotFallbackOrFollowRedirects(t *testing.T) {
	t.Run("HTTP failure", func(t *testing.T) {
		var fallbackHits int32
		selected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"selected unavailable"}`))
		}))
		defer selected.Close()
		fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&fallbackHits, 1)
			_, _ = w.Write([]byte(`{"models":{"wrong-endpoint":{}}}`))
		}))
		defer fallback.Close()

		client := newTestClient(selected.URL)
		client.baseURLs = []string{selected.URL, fallback.URL}
		if _, err := client.FetchAvailableModelsFromURL(context.Background(), selected.URL, "at-1", "proj-1"); err == nil {
			t.Fatal("selected endpoint failure was hidden by a fallback")
		}
		if got := atomic.LoadInt32(&fallbackHits); got != 0 {
			t.Fatalf("fallback endpoint received %d requests, want 0", got)
		}
	})

	t.Run("redirect", func(t *testing.T) {
		var fallbackHits int32
		fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&fallbackHits, 1)
			_, _ = w.Write([]byte(`{"models":{"wrong-endpoint":{}}}`))
		}))
		defer fallback.Close()
		selected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, fallback.URL+"/v1internal:fetchAvailableModels", http.StatusTemporaryRedirect)
		}))
		defer selected.Close()

		client := newTestClient(selected.URL)
		client.baseURLs = []string{selected.URL, fallback.URL}
		if _, err := client.FetchAvailableModelsFromURL(context.Background(), selected.URL, "at-1", "proj-1"); err == nil {
			t.Fatal("redirect to another endpoint was followed")
		}
		if got := atomic.LoadInt32(&fallbackHits); got != 0 {
			t.Fatalf("redirect target received %d requests, want 0", got)
		}
	})
}

func TestFetchAvailableModelsFromURLValidatesEndpointAndProject(t *testing.T) {
	client := NewClient(nil)
	if _, err := client.FetchAvailableModelsFromURL(context.Background(), "", "at-1", "proj-1"); err == nil {
		t.Fatal("empty base URL was accepted")
	}
	if _, err := client.FetchAvailableModelsFromURL(context.Background(), "https://example.test", "at-1", ""); err == nil {
		t.Fatal("empty project id was accepted")
	}
}

func TestForwardURLPrefersDailyHost(t *testing.T) {
	client := NewClient(nil)
	got := client.ForwardURL("streamGenerateContent?alt=sse")
	if !strings.HasPrefix(got, DailyBaseURL) {
		t.Fatalf("ForwardURL = %q, want the daily host first", got)
	}
	if !strings.HasSuffix(got, "/v1internal:streamGenerateContent?alt=sse") {
		t.Fatalf("ForwardURL = %q, want the v1internal action path", got)
	}
}

// A channel key written by an older build may carry a field the gateway has
// since retired; ParseCredential must keep accepting such keys.
func TestParseCredentialAcceptsARetiredField(t *testing.T) {
	credential, err := ParseCredential(`{"access_token":"at-1","project_id":"proj-1","privacy_mode":"opt-out"}`)
	require.NoError(t, err)
	assert.Equal(t, "at-1", credential.AccessToken)
	assert.Equal(t, "proj-1", credential.ProjectID)
}

func TestForwardURLSkipsDemotedHost(t *testing.T) {
	client := NewClient(nil)
	client.MarkUnavailable(DailyBaseURL)

	got := client.ForwardURL("generateContent")
	if !strings.HasPrefix(got, ProdBaseURL) {
		t.Fatalf("ForwardURL = %q, want the production host after demotion", got)
	}
}

func TestQuotaFromModelsHandlesNilResponse(t *testing.T) {
	if rows := QuotaFromModels(nil); rows != nil {
		t.Fatalf("QuotaFromModels(nil) = %v, want nil", rows)
	}
}
