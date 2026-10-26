package workbuddy

import (
	"strings"
	"testing"
	"time"
)

func sequenceUUID(values ...string) func() string {
	index := 0
	return func() string {
		if index >= len(values) {
			return values[len(values)-1]
		}
		value := values[index]
		index++
		return value
	}
}

func requireHeader(t *testing.T, headers map[string]string, key, want string) {
	t.Helper()
	if got := headers[key]; got != want {
		t.Errorf("header %s: got %q, want %q", key, got, want)
	}
}

func requireAbsent(t *testing.T, headers map[string]string, key string) {
	t.Helper()
	if got, ok := headers[key]; ok {
		t.Errorf("header %s must be absent, got %q", key, got)
	}
}

func TestEndpointMatrix(t *testing.T) {
	cases := []struct {
		realm   Realm
		product Product
		wantURL string
		wantDom string
	}{
		{realm: RealmIntl, product: ProductDesktop, wantURL: "https://www.workbuddy.ai", wantDom: "www.workbuddy.ai"},
		{realm: RealmIntl, product: ProductVSCode, wantURL: "https://www.workbuddy.ai", wantDom: "www.workbuddy.ai"},
		{realm: RealmIntl, product: ProductCLI, wantURL: "https://www.workbuddy.ai", wantDom: "www.workbuddy.ai"},
		{realm: RealmCN, product: ProductDesktop, wantURL: "https://www.workbuddy.cn", wantDom: "www.workbuddy.cn"},
		{realm: RealmCN, product: ProductVSCode, wantURL: "https://www.workbuddy.cn", wantDom: "www.workbuddy.cn"},
		{realm: RealmCN, product: ProductCLI, wantURL: "https://copilot.tencent.com", wantDom: "copilot.tencent.com"},
	}
	for _, tc := range cases {
		gotURL, gotDom := Endpoint(tc.realm, tc.product)
		if gotURL != tc.wantURL || gotDom != tc.wantDom {
			t.Errorf("Endpoint(%s, %s): got (%q, %q), want (%q, %q)", tc.realm, tc.product, gotURL, gotDom, tc.wantURL, tc.wantDom)
		}
	}
}

func TestNormalizeRealmAndProduct(t *testing.T) {
	if got := NormalizeRealm(" CN "); got != RealmCN {
		t.Errorf("NormalizeRealm(\" CN \"): got %q, want %q", got, RealmCN)
	}
	if got := NormalizeRealm("global"); got != RealmIntl {
		t.Errorf("NormalizeRealm(global): got %q, want %q", got, RealmIntl)
	}

	products := map[string]Product{
		"workbuddy": ProductDesktop, "wb": ProductDesktop, "desktop": ProductDesktop,
		"vscode": ProductVSCode, "vsc": ProductVSCode, "ide": ProductVSCode,
		"cli": ProductCLI, "codebuddy": ProductCLI,
		"": ProductDesktop, "unknown": ProductDesktop,
	}
	for input, want := range products {
		if got := NormalizeProduct(input); got != want {
			t.Errorf("NormalizeProduct(%q): got %q, want %q", input, got, want)
		}
	}
}

func TestRealmConfigFallsBackToIntl(t *testing.T) {
	if got := Realm("mars").Config(); got.ChatUpstream != "https://www.workbuddy.ai" {
		t.Errorf("unknown realm resolved to %q", got.ChatUpstream)
	}
	if got := RealmCN.Config(); !got.HasCheckin {
		t.Error("the CN realm must expose the daily check-in")
	}
}

func TestChatHeadersDesktopRealmIntl(t *testing.T) {
	identity := Identity{
		Realm:       RealmIntl,
		Product:     ProductDesktop,
		UID:         "uid-1",
		AccessToken: "tok",
		NewUUID:     sequenceUUID("uuid-1"),
	}
	headers := identity.Headers(PurposeChat)

	want := map[string]string{
		"Content-Type":        "application/json",
		"Accept":              "application/json, text/plain, */*",
		"X-Requested-With":    "XMLHttpRequest",
		"Origin":              "https://www.workbuddy.ai",
		"Referer":             "https://www.workbuddy.ai/",
		"X-CodeBuddy-Request": "1",
		"Accept-Language":     "en-US",
		"X-Machine-ID":        MachineID("uid-1"),
		"X-Session-ID":        SessionID("uid-1"),
		"Authorization":       "Bearer tok",
		"X-User-Id":           "uid-1",
		"X-Domain":            "www.workbuddy.ai",
		"X-Agent-Intent":      "craft",
		"X-Agent-Purpose":     "conversation",
		"X-Agent-Type":        "main",
		"X-IDE-Name":          "WorkBuddy",
		"X-IDE-Type":          "WorkBuddy",
		"X-IDE-Version":       DesktopIDEIntlVersion,
		"X-Private-Data":      "true",
		"X-Product":           "SaaS",
		"User-Agent":          "WorkBuddy/5.8.4 workbuddy-ai/5.8.4 CLI/2.161.0",
		"X-Conversation-ID":   "uuid-1",
		"X-Request-ID":        "uuid1",
	}
	for key, value := range want {
		requireHeader(t, headers, key, value)
	}
	requireHeader(t, headers, "X-Trace-ID", "uuid1")
	requireHeader(t, headers, "X-B3-TraceId", "uuid1")
	traceParts := strings.Split(headers["traceparent"], "-")
	if len(traceParts) != 4 || traceParts[1] != headers["X-B3-TraceId"] || traceParts[2] != headers["X-B3-SpanId"] {
		t.Errorf("traceparent = %q is not aligned with the B3 headers", headers["traceparent"])
	}
	requireAbsent(t, headers, "X-No-Enterprise-Id")
	requireAbsent(t, headers, "X-Enterprise-Id")
}

func TestChatHeadersVSCodeAndCLI(t *testing.T) {
	vscode := Identity{
		Realm:       RealmCN,
		Product:     ProductVSCode,
		UID:         "uid-1",
		AccessToken: "tok",
		NewUUID:     sequenceUUID("uuid-1", "uuid-2", "uuid-3"),
	}.Headers(PurposeChat)

	requireHeader(t, vscode, "Origin", "https://www.codebuddy.cn")
	requireHeader(t, vscode, "Accept-Language", "zh-CN")
	requireHeader(t, vscode, "X-Domain", "www.workbuddy.cn")
	requireHeader(t, vscode, "X-Agent-Intent", "craft")
	requireHeader(t, vscode, "X-IDE-Type", "VSCode")
	requireHeader(t, vscode, "X-IDE-Version", VSCodeIDEVersion)
	requireHeader(t, vscode, "X-Product-Version", VSCodeProductVersion)
	requireHeader(t, vscode, "X-Env-ID", "production")
	requireHeader(t, vscode, "X-Product", "SaaS")
	requireHeader(t, vscode, "User-Agent", "VSCode/1.119.0 WorkBuddy/4.12.39997873")
	requireHeader(t, vscode, "X-Request-ID", "uuid3")
	requireHeader(t, vscode, "X-Conversation-ID", "uuid-1")
	requireHeader(t, vscode, "X-Conversation-Request-ID", "uuid3")
	requireHeader(t, vscode, "X-Conversation-Message-ID", "uuid2")
	requireAbsent(t, vscode, "X-Agent-Purpose")

	cli := Identity{
		Realm:       RealmCN,
		Product:     ProductCLI,
		UID:         "uid-1",
		AccessToken: "tok",
		NewUUID:     sequenceUUID("uuid-1", "uuid-2", "uuid-3"),
	}.Headers(PurposeChat)

	requireHeader(t, cli, "X-IDE-Type", "CLI")
	requireHeader(t, cli, "X-IDE-Version", CLIIDEVersion)
	requireHeader(t, cli, "X-Product", "SaaS")
	requireHeader(t, cli, "User-Agent", "CLI/2.168.2 CodeBuddy/2.168.2")
	requireHeader(t, cli, "X-Conversation-Message-ID", "uuid2")
	requireHeader(t, cli, "X-Agent-Runtime", "other")
	requireHeader(t, cli, "X-Root-Request-ID", "uuid3")
	requireHeader(t, cli, "X-Private-Data", "false")

	cnDesktop := Identity{Realm: RealmCN, Product: ProductDesktop, UID: "uid-1", AccessToken: "tok", NewUUID: sequenceUUID("uuid-1")}.Headers(PurposeChat)
	requireHeader(t, cnDesktop, "X-IDE-Version", DesktopIDECNVersion)
	requireHeader(t, cnDesktop, "User-Agent", "WorkBuddy/5.8.3 WorkBuddy/5.8.3 CLI/2.161.0")
}

func TestBillingHeaders(t *testing.T) {
	now := func() time.Time { return time.Unix(0, 123456789) }

	cn := Identity{Realm: RealmCN, Product: ProductCLI, UID: "uid-1", AccessToken: "tok", Now: now}.Headers(PurposeBilling)
	requireHeader(t, cn, "User-Agent", "WorkBuddy/5.8.3")
	requireHeader(t, cn, "X-Request-ID", "f2845c7ef2a8a8d654f1c55eed4d5730-456789")
	requireHeader(t, cn, "X-Product", "SaaS")
	requireHeader(t, cn, "X-Domain", "copilot.tencent.com")
	requireHeader(t, cn, "Authorization", "Bearer tok")
	requireHeader(t, cn, "X-Machine-ID", MachineID("uid-1"))
	requireAbsent(t, cn, "X-IDE-Type")
	requireAbsent(t, cn, "X-Agent-Intent")
	requireAbsent(t, cn, "X-Conversation-ID")
	requireAbsent(t, cn, "X-No-Enterprise-Id")

	intl := Identity{Realm: RealmIntl, UID: "uid-1", AccessToken: "tok", Now: now}.Headers(PurposeBilling)
	requireHeader(t, intl, "User-Agent", "WorkBuddy/5.8.4")
	requireAbsent(t, intl, "X-Product")
	requireHeader(t, intl, "X-Domain", "www.workbuddy.ai")
	requireHeader(t, intl, "X-Request-ID", "f2845c7ef2a8a8d654f1c55eed4d5730-456789")
}

func TestIdentityOverridesEnterpriseAndDeviceIdentifiers(t *testing.T) {
	identity := Identity{
		Realm:        RealmIntl,
		Product:      ProductCLI,
		UID:          "uid-1",
		AccessToken:  "tok",
		Domain:       "override.example.com",
		EnterpriseID: "ent-1",
		MachineID:    "machine-override",
		SessionID:    "session-override",
		NewUUID:      sequenceUUID("uuid-1"),
	}
	headers := identity.Headers(PurposeChat)

	requireHeader(t, headers, "X-Machine-ID", "machine-override")
	requireHeader(t, headers, "X-Session-ID", "session-override")
	requireHeader(t, headers, "X-Domain", "override.example.com")
	requireHeader(t, headers, "X-Enterprise-Id", "ent-1")
	requireHeader(t, headers, "X-Tenant-Id", "ent-1")
	requireAbsent(t, headers, "X-No-Enterprise-Id")
	// The reference gateway can supply a department and a device token, but no
	// production writer of this fork ever did, so those headers stay absent.
	requireAbsent(t, headers, "X-Department-Info")
	requireAbsent(t, headers, "X-Device-Token")

	anonymous := Identity{Realm: RealmIntl, AccessToken: "tok", NewUUID: sequenceUUID("uuid-1")}.Headers(PurposeChat)
	requireHeader(t, anonymous, "X-User-Id", "anonymous")
	requireHeader(t, anonymous, "X-Machine-ID", MachineID("anonymous"))
	requireAbsent(t, anonymous, "X-Enterprise-Id")
	requireAbsent(t, anonymous, "X-Tenant-Id")
}

func TestNewUUIDIsVersion4(t *testing.T) {
	seen := map[string]bool{}
	for index := 0; index < 32; index++ {
		value := newUUID()
		if len(value) != 36 || strings.Count(value, "-") != 4 {
			t.Fatalf("newUUID produced %q", value)
		}
		if value[14] != '4' {
			t.Fatalf("newUUID version nibble: %q", value)
		}
		if !strings.ContainsRune("89ab", rune(value[19])) {
			t.Fatalf("newUUID variant nibble: %q", value)
		}
		if seen[value] {
			t.Fatalf("newUUID repeated %q", value)
		}
		seen[value] = true
	}
}
