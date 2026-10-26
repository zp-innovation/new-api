package workbuddy

import (
	"strings"
	"testing"
	"time"
)

func credentialJSON(t *testing.T, body string) string {
	t.Helper()
	return body
}

func TestParseCredentialAcceptsWorkBuddyJSON(t *testing.T) {
	raw := `{
	  "type": "workbuddy",
	  "realm": "cn",
	  "product": "cli",
	  "uid": "uid-1",
	  "nickname": "tester",
	  "access_token": "access-token",
	  "refresh_token": "refresh-token",
	  "expires_at": "2030-01-01T00:00:00Z"
	}`

	credential, err := ParseCredential(raw)
	if err != nil {
		t.Fatalf("ParseCredential() error = %v", err)
	}
	if credential.UID != "uid-1" {
		t.Fatalf("UID = %q, want uid-1", credential.UID)
	}
	identity := credential.Identity()
	if identity.Realm != RealmCN {
		t.Fatalf("Realm = %q, want %q", identity.Realm, RealmCN)
	}
	if identity.Product != ProductCLI {
		t.Fatalf("Product = %q, want %q", identity.Product, ProductCLI)
	}
	if got := credential.ChatBaseURL(); got != "https://copilot.tencent.com" {
		t.Fatalf("ChatBaseURL() = %q, want the CN chat host", got)
	}
	if err := credential.ValidateForStorage(); err != nil {
		t.Fatalf("ValidateForStorage() error = %v", err)
	}
}

func TestParseCredentialDefaultsToIntlAndCLI(t *testing.T) {
	credential, err := ParseCredential(`{"access_token":"token","uid":"uid-1"}`)
	if err != nil {
		t.Fatalf("ParseCredential() error = %v", err)
	}
	identity := credential.Identity()
	if identity.Realm != RealmIntl {
		t.Fatalf("Realm = %q, want %q", identity.Realm, RealmIntl)
	}
	if identity.Product != ProductCLI {
		t.Fatalf("Product = %q, want %q", identity.Product, ProductCLI)
	}
	if got := credential.ChatBaseURL(); got != "https://www.workbuddy.ai" {
		t.Fatalf("ChatBaseURL() = %q, want the intl chat host", got)
	}
	headers := identity.Headers(PurposeChat)
	if ua := headers["User-Agent"]; !strings.Contains(ua, "CLI/") {
		t.Fatalf("User-Agent = %q, want the CLI identity", ua)
	}
}

func TestParseCredentialRejections(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		message string
	}{
		{name: "empty", raw: "   ", message: "credential is empty"},
		{name: "invalid json", raw: "{", message: "invalid credential json"},
		{name: "foreign type", raw: `{"type":"antigravity","access_token":"t","uid":"u"}`, message: "is not"},
		{name: "missing access token", raw: `{"uid":"u"}`, message: "access_token is required"},
		{name: "missing uid", raw: `{"access_token":"t"}`, message: "uid is required"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseCredential(testCase.raw)
			if err == nil {
				t.Fatalf("ParseCredential() error = nil, want %q", testCase.message)
			}
			if !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), testCase.message)
			}
		})
	}
}

func TestMarshalStaysSingleLineAndRoundTrips(t *testing.T) {
	tasksEnabled := false
	credential := &Credential{
		UID:          "uid-1",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Realm:        string(RealmCN),
		TasksEnabled: &tasksEnabled,
	}
	encoded, err := credential.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.ContainsAny(encoded, "\n\r") {
		t.Fatalf("Marshal() produced a multi-line credential: %q", encoded)
	}
	if !strings.Contains(encoded, `"type":"workbuddy"`) {
		t.Fatalf("Marshal() = %q, want the type marker", encoded)
	}
	parsed, err := ParseCredential(encoded)
	if err != nil {
		t.Fatalf("ParseCredential(Marshal()) error = %v", err)
	}
	if parsed.RefreshToken != "refresh-token" {
		t.Fatalf("RefreshToken = %q, want refresh-token", parsed.RefreshToken)
	}
	if parsed.TasksEnabled == nil || *parsed.TasksEnabled {
		t.Fatalf("TasksEnabled = %v, want false", parsed.TasksEnabled)
	}
}

func TestValidateForStorageRequiresRefreshToken(t *testing.T) {
	credential := &Credential{UID: "uid-1", AccessToken: "token"}
	err := credential.ValidateForStorage()
	if err == nil || !strings.Contains(err.Error(), "refresh_token is required") {
		t.Fatalf("ValidateForStorage() error = %v, want a refresh_token requirement", err)
	}
}

func TestNeedsRefreshUsesTheStoredDeadline(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		expires  string
		token    string
		expected bool
	}{
		{name: "expires soon", expires: now.Add(30 * time.Second).Format(time.RFC3339), token: "t", expected: true},
		{name: "expires later", expires: now.Add(time.Hour).Format(time.RFC3339), token: "t", expected: false},
		{name: "missing deadline", expires: "", token: "opaque-token", expected: true},
		{name: "missing token", expires: now.Add(time.Hour).Format(time.RFC3339), token: "", expected: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			credential := &Credential{UID: "uid-1", AccessToken: testCase.token, ExpiresAt: testCase.expires}
			if got := credential.NeedsRefresh(now, CredentialRefreshWindow); got != testCase.expected {
				t.Fatalf("NeedsRefresh() = %v, want %v", got, testCase.expected)
			}
		})
	}
}

func TestWithTokensKeepsTheRotatedRefreshToken(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	original := &Credential{
		Type:         CredentialType,
		UID:          "uid-1",
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
	}

	updated := original.WithTokens(&TokenResponse{AccessToken: "new-access"}, now)
	if updated.RefreshToken != "old-refresh" {
		t.Fatalf("RefreshToken = %q, want the previous value kept", updated.RefreshToken)
	}
	if updated.AccessToken != "new-access" {
		t.Fatalf("AccessToken = %q, want new-access", updated.AccessToken)
	}
	if updated.LastRefresh != now.Format(time.RFC3339) {
		t.Fatalf("LastRefresh = %q, want %q", updated.LastRefresh, now.Format(time.RFC3339))
	}
	if original.AccessToken != "old-access" {
		t.Fatalf("WithTokens() mutated the receiver: AccessToken = %q", original.AccessToken)
	}

	rotated := original.WithTokens(&TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh"}, now)
	if rotated.RefreshToken != "new-refresh" {
		t.Fatalf("RefreshToken = %q, want the rotated value", rotated.RefreshToken)
	}

	withExpiry := original.WithTokens(&TokenResponse{
		AccessToken: "new-access",
		ExpiresAt:   now.Add(2 * time.Hour),
	}, now)
	want := now.Add(2 * time.Hour).Format(time.RFC3339)
	if withExpiry.ExpiresAt != want {
		t.Fatalf("ExpiresAt = %q, want %q", withExpiry.ExpiresAt, want)
	}
}
