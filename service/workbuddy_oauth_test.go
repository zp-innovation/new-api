package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
)

// stubWorkBuddyOAuthClient points the login and refresh flows at a test server.
// The production builder resolves the realm's chat upstream, which no test can
// reach.
func stubWorkBuddyOAuthClient(t *testing.T, server *httptest.Server) {
	t.Helper()
	original := workbuddyOAuthClient
	workbuddyOAuthClient = func(realm, product, proxyURL string) (*wbcore.Client, error) {
		return wbcore.NewClient(server.Client(), server.URL, wbcore.NormalizeRealm(realm), wbcore.NormalizeProduct(product)), nil
	}
	t.Cleanup(func() { workbuddyOAuthClient = original })
}

// workBuddyTestJWT builds an unsigned token whose payload carries the subject
// and the expiry the credential reads back.
func workBuddyTestJWT(t *testing.T, subject string, exp int64) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"sub": subject, "exp": exp})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	encode := func(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }
	return encode([]byte("{\"alg\":\"none\"}")) + "." + encode(payload) + ".signature"
}

// newWorkBuddyOAuthServer answers the management endpoints the login flow uses.
func newWorkBuddyOAuthServer(t *testing.T, accessToken, refreshToken string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case wbcore.AuthStatePath:
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"state":"state-1","authUrl":"https://example.test/authorize?state=state-1"}}`))
		case wbcore.AuthTokenPath:
			body, _ := json.Marshal(map[string]any{
				"code": 0,
				"msg":  "",
				"data": map[string]any{
					"accessToken":  accessToken,
					"refreshToken": refreshToken,
					"domain":       "www.workbuddy.ai",
					"expiresAt":    1791655445,
				},
			})
			_, _ = w.Write(body)
		case wbcore.LoginAccountPath:
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"nickname":"Jack"}}`))
		case wbcore.RefreshPath:
			body, _ := json.Marshal(map[string]any{
				"code": 0,
				"msg":  "",
				"data": map[string]any{"accessToken": "access-2", "refreshToken": "refresh-2"},
			})
			_, _ = w.Write(body)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":404,"msg":"not found"}`))
		}
	}))
}

func TestStartWorkBuddyLoginStoresTheSession(t *testing.T) {
	server := newWorkBuddyOAuthServer(t, "access-1", "refresh-1")
	defer server.Close()
	stubWorkBuddyOAuthClient(t, server)

	result, err := StartWorkBuddyLogin(context.Background(), "intl", "cli", "")
	if err != nil {
		t.Fatalf("StartWorkBuddyLogin: %v", err)
	}
	if result.State != "state-1" {
		t.Errorf("state = %q", result.State)
	}
	if !strings.Contains(result.AuthURL, "state=state-1") {
		t.Errorf("auth url = %q", result.AuthURL)
	}
	if result.Realm != "intl" || result.Product != "cli" {
		t.Errorf("realm/product = %q/%q", result.Realm, result.Product)
	}
	if result.ExpiresIn != wbcore.LoginTTLSeconds {
		t.Errorf("expires_in = %d", result.ExpiresIn)
	}

	session, ok := workbuddyLoginSessions.Get("state-1")
	if !ok {
		t.Fatal("the login session was not stored")
	}
	if session.Realm != wbcore.RealmIntl || session.Product != wbcore.ProductCLI {
		t.Fatalf("session realm/product = %q/%q", session.Realm, session.Product)
	}
	workbuddyLoginSessions.Delete("state-1")
}

func TestPollWorkBuddyLoginReportsPending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":11217,"msg":"pending"}`))
	}))
	defer server.Close()
	stubWorkBuddyOAuthClient(t, server)

	workbuddyLoginSessions.Put(wbcore.LoginSession{State: "pending-state", Realm: wbcore.RealmIntl, Product: wbcore.ProductCLI})
	t.Cleanup(func() { workbuddyLoginSessions.Delete("pending-state") })

	status, err := PollWorkBuddyLogin(context.Background(), "pending-state", "")
	if err != nil {
		t.Fatalf("PollWorkBuddyLogin: %v", err)
	}
	if status.Status != WorkBuddyLoginPending {
		t.Fatalf("status = %q, want pending", status.Status)
	}
	if status.Credential != nil {
		t.Fatal("a pending login returned a credential")
	}
	if _, ok := workbuddyLoginSessions.Get("pending-state"); !ok {
		t.Fatal("a pending login dropped its session")
	}
}

func TestPollWorkBuddyLoginReturnsACompactCredential(t *testing.T) {
	accessToken := workBuddyTestJWT(t, "uid-1", 4102444800)
	server := newWorkBuddyOAuthServer(t, accessToken, "refresh-1")
	defer server.Close()
	stubWorkBuddyOAuthClient(t, server)

	workbuddyLoginSessions.Put(wbcore.LoginSession{State: "ready-state", Realm: wbcore.RealmCN, Product: wbcore.ProductVSCode})

	status, err := PollWorkBuddyLogin(context.Background(), "ready-state", "")
	if err != nil {
		t.Fatalf("PollWorkBuddyLogin: %v", err)
	}
	if status.Status != WorkBuddyLoginReady {
		t.Fatalf("status = %q, want ready", status.Status)
	}
	if status.Credential == nil {
		t.Fatal("a completed login returned no credential")
	}

	encoded, err := status.Credential.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.ContainsAny(encoded, "\n\r") {
		t.Fatalf("the credential is not single line: %q", encoded)
	}
	parsed, err := wbcore.ParseCredential(encoded)
	if err != nil {
		t.Fatalf("ParseCredential: %v", err)
	}
	if parsed.UID != "uid-1" || parsed.RefreshToken != "refresh-1" {
		t.Errorf("credential = %+v", parsed)
	}
	if parsed.Realm != "cn" || parsed.Product != "vscode" {
		t.Errorf("credential realm/product = %q/%q", parsed.Realm, parsed.Product)
	}
	if parsed.Nickname != "Jack" {
		t.Errorf("nickname = %q", parsed.Nickname)
	}
	if parsed.ExpiresAt == "" {
		t.Error("credential has no expiry")
	}

	// A completed login is single use: the state must not mint a second
	// credential.
	if _, ok := workbuddyLoginSessions.Get("ready-state"); ok {
		t.Fatal("the login session survived a completed poll")
	}
	replay, err := PollWorkBuddyLogin(context.Background(), "ready-state", "")
	if err != nil {
		t.Fatalf("replay PollWorkBuddyLogin: %v", err)
	}
	if replay.Status != WorkBuddyLoginExpired {
		t.Fatalf("replay status = %q, want expired", replay.Status)
	}
}

func TestPollWorkBuddyLoginExpiresAnUnknownState(t *testing.T) {
	status, err := PollWorkBuddyLogin(context.Background(), "never-started", "")
	if err != nil {
		t.Fatalf("PollWorkBuddyLogin: %v", err)
	}
	if status.Status != WorkBuddyLoginExpired {
		t.Fatalf("status = %q, want expired", status.Status)
	}
}

func TestRefreshWorkBuddyCredentialRotatesTheToken(t *testing.T) {
	server := newWorkBuddyOAuthServer(t, "access-1", "refresh-1")
	defer server.Close()
	stubWorkBuddyOAuthClient(t, server)

	raw := `{"type":"workbuddy","realm":"intl","product":"cli","uid":"uid-1","domain":"www.workbuddy.ai","access_token":"access-1","refresh_token":"refresh-1"}`
	credential, err := RefreshWorkBuddyCredential(context.Background(), raw, "")
	if err != nil {
		t.Fatalf("RefreshWorkBuddyCredential: %v", err)
	}
	if credential.AccessToken != "access-2" {
		t.Errorf("access token = %q", credential.AccessToken)
	}
	// The upstream rotates the refresh token: keeping the old one would leave a
	// value the upstream has already consumed.
	if credential.RefreshToken != "refresh-2" {
		t.Errorf("refresh token = %q", credential.RefreshToken)
	}
	if credential.Realm != "intl" || credential.UID != "uid-1" {
		t.Errorf("credential = %+v", credential)
	}
}

func TestRefreshWorkBuddyCredentialRejectsAMissingToken(t *testing.T) {
	raw := `{"type":"workbuddy","uid":"uid-1","access_token":"access-1"}`
	_, err := RefreshWorkBuddyCredential(context.Background(), raw, "")
	if err == nil {
		t.Fatal("RefreshWorkBuddyCredential accepted a credential without a refresh token")
	}
	if !strings.Contains(err.Error(), "refresh token") {
		t.Fatalf("error = %q, want it to mention the refresh token", err)
	}
}
