package workbuddy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testJWT builds an unsigned token with the two claims this package reads.
func testJWT(t *testing.T, subject string, exp int64) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":%q,"exp":%d}`, subject, exp)))
	return header + "." + payload + ".signature"
}

func newTestClient(t *testing.T, handler http.HandlerFunc, realm Realm) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(server.Client(), server.URL, realm, ProductCLI)
}

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func TestStartLoginSendsTheManagementContract(t *testing.T) {
	var seenMethod, seenPath, seenPlatform, seenBody string
	var seenHeaders http.Header

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		seenPlatform = r.URL.Query().Get("platform")
		seenHeaders = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		seenBody = string(raw)
		writeJSON(t, w, `{"code":0,"msg":"","data":{"state":"st-1","authUrl":"https://www.workbuddy.ai/login?state=st-1"}}`)
	}, RealmIntl)

	session, err := client.StartLogin(context.Background(), "")
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if seenMethod != http.MethodPost || seenPath != AuthStatePath || seenPlatform != DefaultPlatform {
		t.Errorf("request line: %s %s platform=%s", seenMethod, seenPath, seenPlatform)
	}
	if seenBody != "{}" {
		t.Errorf("request body: got %q, want {}", seenBody)
	}
	if got := seenHeaders.Get("Origin"); got != "https://www.workbuddy.ai" {
		t.Errorf("Origin: got %q", got)
	}
	if got := seenHeaders.Get("Referer"); got != "https://www.workbuddy.ai/" {
		t.Errorf("Referer: got %q", got)
	}
	if got := seenHeaders.Get("User-Agent"); got != "WorkBuddy/5.8.4" {
		t.Errorf("User-Agent: got %q", got)
	}
	if got := seenHeaders.Get("X-Requested-With"); got != "XMLHttpRequest" {
		t.Errorf("X-Requested-With: got %q", got)
	}
	if got := seenHeaders.Get("Authorization"); got != "" {
		t.Errorf("the login start call must not carry Authorization, got %q", got)
	}
	if session.State != "st-1" || session.AuthURL != "https://www.workbuddy.ai/login?state=st-1" {
		t.Errorf("session: %+v", session)
	}
	if session.Realm != RealmIntl || session.Platform != DefaultPlatform {
		t.Errorf("session metadata: %+v", session)
	}
}

func TestStartLoginRejectsAnIncompleteResponse(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, `{"code":0,"data":{}}`)
	}, RealmIntl)

	if _, err := client.StartLogin(context.Background(), "CLI"); err == nil {
		t.Fatal("StartLogin accepted a response without state/authUrl")
	}
}

func TestPollLoginReportsPending(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != AuthTokenPath {
			t.Errorf("poll path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("state"); got != "st-1" {
			t.Errorf("state query: %q", got)
		}
		writeJSON(t, w, `{"code":11217,"msg":"11217:login ing..."}`)
	}, RealmIntl)

	if _, err := client.PollLogin(context.Background(), "st-1"); !errors.Is(err, ErrLoginPending) {
		t.Fatalf("PollLogin: got %v, want ErrLoginPending", err)
	}
}

func TestPollLoginBuildsTheTokenResponse(t *testing.T) {
	token := testJWT(t, "uid-9", 2000000000)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, fmt.Sprintf(`{"code":0,"data":{"accessToken":%q,"refreshToken":"rt-1","expiresAt":1900000000}}`, token))
	}, RealmIntl)

	result, err := client.PollLogin(context.Background(), "st-1")
	if err != nil {
		t.Fatalf("PollLogin: %v", err)
	}
	if result.AccessToken != token || result.RefreshToken != "rt-1" {
		t.Errorf("tokens: %+v", result)
	}
	if result.UID != "uid-9" {
		t.Errorf("uid: got %q, want uid-9", result.UID)
	}
	if want := time.Unix(1900000000, 0).UTC(); !result.ExpiresAt.Equal(want) {
		t.Errorf("expiresAt: got %s, want %s", result.ExpiresAt, want)
	}
	if result.Domain != "www.workbuddy.ai" {
		t.Errorf("domain fallback: got %q", result.Domain)
	}
}

func TestPollLoginNormalizesMillisecondExpiryAndFallsBackToTheToken(t *testing.T) {
	token := testJWT(t, "uid-9", 2000000000)

	milliseconds := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, fmt.Sprintf(`{"code":0,"data":{"accessToken":%q,"expiresAt":1900000000000}}`, token))
	}, RealmIntl)
	result, err := milliseconds.PollLogin(context.Background(), "st-1")
	if err != nil {
		t.Fatalf("PollLogin: %v", err)
	}
	if want := time.Unix(1900000000, 0).UTC(); !result.ExpiresAt.Equal(want) {
		t.Errorf("millisecond expiry: got %s, want %s", result.ExpiresAt, want)
	}

	noExpiry := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, fmt.Sprintf(`{"code":0,"data":{"accessToken":%q,"domain":"copilot.tencent.com"}}`, token))
	}, RealmCN)
	result, err = noExpiry.PollLogin(context.Background(), "st-1")
	if err != nil {
		t.Fatalf("PollLogin: %v", err)
	}
	if want := time.Unix(2000000000, 0).UTC(); !result.ExpiresAt.Equal(want) {
		t.Errorf("exp claim fallback: got %s, want %s", result.ExpiresAt, want)
	}
	if result.Domain != "copilot.tencent.com" {
		t.Errorf("domain: got %q", result.Domain)
	}
}

func TestPollLoginSurfacesBusinessErrors(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, `{"code":40001,"msg":"nope"}`)
	}, RealmIntl)

	_, err := client.PollLogin(context.Background(), "st-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("PollLogin: got %v, want *APIError", err)
	}
	if apiErr.Code != 40001 || apiErr.Message != "nope" {
		t.Errorf("APIError: %+v", apiErr)
	}
}

func TestPollLoginTreatsAMissingTokenAsPending(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, `{"code":0,"data":{}}`)
	}, RealmIntl)

	if _, err := client.PollLogin(context.Background(), "st-1"); !errors.Is(err, ErrLoginPending) {
		t.Fatalf("PollLogin: got %v, want ErrLoginPending", err)
	}
}

func TestRefreshSendsTheRotationContract(t *testing.T) {
	token := testJWT(t, "uid-9", 2000000000)
	var seen http.Header
	var seenBody string
	var seenPath string

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		seenPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		seenBody = string(raw)
		writeJSON(t, w, fmt.Sprintf(`{"code":0,"data":{"accessToken":%q,"refreshToken":"rt-2"}}`, token))
	}, RealmIntl)

	result, err := client.Refresh(context.Background(), RefreshRequest{UID: "uid-9", RefreshToken: "rt-1"})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if seenPath != RefreshPath || seenBody != "{}" {
		t.Errorf("request: path=%s body=%q", seenPath, seenBody)
	}
	if got := seen.Get("X-Refresh-Token"); got != "rt-1" {
		t.Errorf("X-Refresh-Token: got %q", got)
	}
	if got := seen.Get("X-Auth-Refresh-Source"); got != "plugin" {
		t.Errorf("X-Auth-Refresh-Source: got %q, want plugin", got)
	}
	if got := seen.Get("X-User-Id"); got != "uid-9" {
		t.Errorf("X-User-Id: got %q", got)
	}
	if got := seen.Get("X-Domain"); got != "www.workbuddy.ai" {
		t.Errorf("X-Domain: got %q", got)
	}
	if got := seen.Get("Accept-Language"); got != "en-US" {
		t.Errorf("Accept-Language: got %q", got)
	}
	if result.RefreshToken != "rt-2" {
		t.Errorf("rotated refresh token: got %q", result.RefreshToken)
	}
	if want := time.Unix(2000000000, 0).UTC(); !result.ExpiresAt.Equal(want) {
		t.Errorf("expiresAt from the exp claim: got %s, want %s", result.ExpiresAt, want)
	}
}

func TestRefreshUsesTheChineseRefreshSource(t *testing.T) {
	token := testJWT(t, "uid-9", 2000000000)
	var seen http.Header

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		writeJSON(t, w, fmt.Sprintf(`{"code":0,"data":{"accessToken":%q}}`, token))
	}, RealmCN)

	if _, err := client.Refresh(context.Background(), RefreshRequest{UID: "uid-9", RefreshToken: "rt-1"}); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := seen.Get("X-Auth-Refresh-Source"); got != "workbuddy" {
		t.Errorf("X-Auth-Refresh-Source: got %q, want workbuddy", got)
	}
	if got := seen.Get("Accept-Language"); got != "zh-CN" {
		t.Errorf("Accept-Language: got %q, want zh-CN", got)
	}
	if got := seen.Get("User-Agent"); got != "WorkBuddy/5.8.3" {
		t.Errorf("User-Agent: got %q, want the CN billing UA", got)
	}
}

func TestRefreshKeepsTheOldTokenWhenTheResponseOmitsIt(t *testing.T) {
	token := testJWT(t, "uid-9", 2000000000)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, fmt.Sprintf(`{"code":0,"data":{"accessToken":%q}}`, token))
	}, RealmIntl)

	result, err := client.Refresh(context.Background(), RefreshRequest{UID: "uid-9", RefreshToken: "rt-1"})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.RefreshToken != "rt-1" {
		t.Errorf("an absent refresh token must keep the previous value, got %q", result.RefreshToken)
	}
}

func TestRefreshUnwrapsTheDoubleNestedData(t *testing.T) {
	token := testJWT(t, "uid-9", 2000000000)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, fmt.Sprintf(`{"code":0,"data":{"data":{"accessToken":%q,"refreshToken":"rt-3"}}}`, token))
	}, RealmIntl)

	result, err := client.Refresh(context.Background(), RefreshRequest{UID: "uid-9", RefreshToken: "rt-1"})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.AccessToken != token || result.RefreshToken != "rt-3" {
		t.Errorf("double nested data: %+v", result)
	}
}

func TestRefreshRequiresARefreshToken(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the client must not call the upstream without a refresh token")
	}, RealmIntl)

	if _, err := client.Refresh(context.Background(), RefreshRequest{UID: "uid-9"}); err == nil {
		t.Fatal("Refresh accepted an empty refresh token")
	}
}

func TestLoginStoreExpiresSessions(t *testing.T) {
	store := NewLoginStore()
	created := time.Unix(1000, 0)
	store.SetClock(func() time.Time { return created })
	store.Put(LoginSession{State: "st-1", AuthURL: "https://www.workbuddy.ai/login", CreatedAt: created})

	if _, ok := store.Get("st-1"); !ok {
		t.Fatal("a fresh session must be readable")
	}
	if _, ok := store.Get("nope"); ok {
		t.Fatal("an unknown state must not resolve")
	}

	store.SetClock(func() time.Time { return created.Add(LoginTTLSeconds*time.Second + time.Second) })
	if _, ok := store.Get("st-1"); ok {
		t.Fatal("an expired session must not resolve")
	}
	if store.Delete("st-1") {
		t.Fatal("an expired session must already be gone")
	}

	store.SetClock(func() time.Time { return created })
	store.Put(LoginSession{State: "st-2", CreatedAt: created})
	if !store.Delete("st-2") {
		t.Fatal("Delete must report a present session")
	}
}

func TestLoginStoreCleanupDropsAbandonedSessions(t *testing.T) {
	store := NewLoginStore()
	created := time.Unix(2000, 0)
	store.SetClock(func() time.Time { return created })
	store.Put(LoginSession{State: "abandoned"})

	require.Equal(t, 0, store.Cleanup(), "live sessions must survive Cleanup")

	store.SetClock(func() time.Time { return created.Add(LoginTTLSeconds*time.Second + time.Second) })
	store.Put(LoginSession{State: "fresh"})
	require.Equal(t, 1, store.Cleanup(), "Cleanup must drop the abandoned state")

	_, ok := store.Get("fresh")
	assert.True(t, ok)
}

func TestFetchProfileReturnsTheNickname(t *testing.T) {
	var seenAuth string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != LoginAccountPath {
			t.Errorf("profile path: %s", r.URL.Path)
		}
		seenAuth = r.Header.Get("Authorization")
		writeJSON(t, w, `{"code":0,"data":{"nickname":"jack"}}`)
	}, RealmIntl)

	nickname, err := client.FetchProfile(context.Background(), "st-1", "tok")
	if err != nil {
		t.Fatalf("FetchProfile: %v", err)
	}
	if nickname != "jack" {
		t.Errorf("nickname: got %q", nickname)
	}
	if seenAuth != "Bearer tok" {
		t.Errorf("Authorization: got %q", seenAuth)
	}
}

func TestClientRejectsAnHTTPErrorStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "upstream exploded")
	}, RealmIntl)

	_, err := client.PollLogin(context.Background(), "st-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("PollLogin: got %v, want *APIError", err)
	}
	if apiErr.Code != http.StatusBadGateway {
		t.Errorf("APIError code: %d", apiErr.Code)
	}
	if apiErr.Message != "upstream exploded" {
		t.Errorf("APIError message: %q", apiErr.Message)
	}
}

func TestEnvelopeDecoding(t *testing.T) {
	var decoded struct {
		Value string `json:"value"`
	}
	raw := json.RawMessage(`{"value":"ok"}`)
	if err := common.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Value != "ok" {
		t.Errorf("value: %q", decoded.Value)
	}
}
