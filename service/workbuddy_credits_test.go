package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
)

// stubWorkBuddyCreditsClient points the balance query at a test server. The
// production builder resolves the realm's real billing upstream, which no test
// can reach, so both the chat and the billing base URL are overridden.
func stubWorkBuddyCreditsClient(t *testing.T, server *httptest.Server) {
	t.Helper()
	original := workbuddyOAuthClient
	workbuddyOAuthClient = func(realm, product, proxyURL string) (*wbcore.Client, error) {
		client := wbcore.NewClient(server.Client(), server.URL, wbcore.NormalizeRealm(realm), wbcore.NormalizeProduct(product))
		client.BillingBaseURL = server.URL
		return client, nil
	}
	t.Cleanup(func() { workbuddyOAuthClient = original })
}

// workBuddyCreditsServer answers the international fallback meter call with one
// free package.
func workBuddyCreditsServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wbcore.GetResourcePath {
			t.Errorf("path = %s, want %s", r.URL.Path, wbcore.GetResourcePath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"Response":{"Data":{"Accounts":[{"AccountId":"a1","PackageName":"Free pack","PackageCode":"p1","CapacitySizePrecise":100,"CapacityRemainPrecise":25,"CycleEndTime":"2099-01-01 00:00:00"}]}}}}`))
	}))
}

func TestGetWorkBuddyChannelCreditsRejectsUnusableChannels(t *testing.T) {
	if _, err := GetWorkBuddyChannelCredits(context.Background(), nil); err == nil {
		t.Fatal("a nil channel must be rejected")
	}
	wrongType := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "{}"}
	if _, err := GetWorkBuddyChannelCredits(context.Background(), wrongType); err == nil {
		t.Fatal("a non-WorkBuddy channel must be rejected")
	}
	brokenKey := &model.Channel{Type: constant.ChannelTypeWorkBuddy, Key: "not-json"}
	if _, err := GetWorkBuddyChannelCredits(context.Background(), brokenKey); err == nil {
		t.Fatal("a broken credential must be rejected")
	}
}

func TestGetWorkBuddyChannelCreditsReadsTheFallbackBalance(t *testing.T) {
	server := workBuddyCreditsServer(t)
	defer server.Close()
	stubWorkBuddyCreditsClient(t, server)

	channel := &model.Channel{
		Id:   7,
		Type: constant.ChannelTypeWorkBuddy,
		Key:  workBuddyCredential("access-token", "intl", "cli"),
	}
	result, err := GetWorkBuddyChannelCredits(context.Background(), channel)
	if err != nil {
		t.Fatalf("GetWorkBuddyChannelCredits: %v", err)
	}
	credits := result.Credits
	if credits.Remain != 25 || credits.Size != 100 || credits.Used != 75 {
		t.Fatalf("credits = %+v", credits)
	}
	if len(credits.Packages) != 1 || credits.Packages[0].Name != "Free pack" {
		t.Fatalf("packages = %+v", credits.Packages)
	}
	if result.AccountUID != "uid-1" || result.AccountCount != 1 {
		t.Fatalf("account = %+v", result)
	}
}

// A multi-account channel answers for its first usable credential, and the
// caller has to know which account the numbers describe.
func TestGetWorkBuddyChannelCreditsCountsTheAccounts(t *testing.T) {
	server := workBuddyCreditsServer(t)
	defer server.Close()
	stubWorkBuddyCreditsClient(t, server)

	channel := &model.Channel{
		Id:   7,
		Type: constant.ChannelTypeWorkBuddy,
		Key: workBuddyCredential("access-token", "intl", "cli") + "\n" +
			workBuddyCredential("second-token", "cn", "cli"),
	}
	result, err := GetWorkBuddyChannelCredits(context.Background(), channel)
	if err != nil {
		t.Fatalf("GetWorkBuddyChannelCredits: %v", err)
	}
	if result.AccountCount != 2 {
		t.Fatalf("account count = %d, want 2", result.AccountCount)
	}
	if result.AccountUID != "uid-1" {
		t.Fatalf("account uid = %q, want the first line's uid", result.AccountUID)
	}
}
