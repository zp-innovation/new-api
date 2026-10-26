package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
)

// workBuddyCredential builds the compact single-line credential a channel row
// stores.
func workBuddyCredential(accessToken, realm, product string) string {
	return `{"type":"workbuddy","realm":"` + realm + `","product":"` + product + `","uid":"uid-1","access_token":"` + accessToken + `","refresh_token":"refresh-1","expires_at":"2099-01-01T00:00:00Z"}`
}

// workBuddyCatalogServer answers the product-config call with the catalogue the
// desktop client reads.
func workBuddyCatalogServer(t *testing.T, models string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wbcore.ConfigPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wbcore.ConfigPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Errorf("authorization = %q, want the first usable line's token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"agents":{"cli":{"models":` + models + `}}}}`))
	}))
}

func TestGetWorkBuddyChannelModelsRejectsUnusableChannels(t *testing.T) {
	testCases := []struct {
		name    string
		channel *model.Channel
		wantErr string
	}{
		{name: "missing channel", channel: nil, wantErr: "channel not found"},
		{name: "wrong type", channel: &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "{}"}, wantErr: "channel type is not WorkBuddy"},
		{name: "no credential", channel: &model.Channel{Type: constant.ChannelTypeWorkBuddy, Key: "   "}, wantErr: "the channel has no credential"},
		{name: "broken credential", channel: &model.Channel{Type: constant.ChannelTypeWorkBuddy, Key: "not-json"}, wantErr: "invalid credential json"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			models, err := GetWorkBuddyChannelModels(context.Background(), testCase.channel)
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("GetWorkBuddyChannelModels() = %v, %v; want an error containing %q", models, err, testCase.wantErr)
			}
		})
	}
}

// GetWorkBuddyChannelModels backs the drawer's "fetch from upstream" action,
// which cannot use /v1/models: the catalogue comes from the product-config call.
func TestGetWorkBuddyChannelModelsReadsTheCatalogue(t *testing.T) {
	server := workBuddyCatalogServer(t, `["gpt-6-astra","deepseek-v4.1-flash"]`)
	defer server.Close()
	stubWorkBuddyOAuthClient(t, server)

	channel := &model.Channel{
		Type: constant.ChannelTypeWorkBuddy,
		Key:  workBuddyCredential("access-token", "intl", "desktop"),
	}

	models, err := GetWorkBuddyChannelModels(context.Background(), channel)
	if err != nil {
		t.Fatalf("GetWorkBuddyChannelModels: %v", err)
	}
	if strings.Join(models, ",") != "gpt-6-astra,deepseek-v4.1-flash" {
		t.Fatalf("models = %v", models)
	}
}

// A multi-account channel keeps one credential per line; the catalogue is
// decided by the product rather than by the account, so the first usable line
// answers for the whole channel.
func TestGetWorkBuddyChannelModelsUsesTheFirstUsableLine(t *testing.T) {
	server := workBuddyCatalogServer(t, `["gpt-6-astra"]`)
	defer server.Close()
	stubWorkBuddyOAuthClient(t, server)

	channel := &model.Channel{
		Type: constant.ChannelTypeWorkBuddy,
		Key:  "\n" + workBuddyCredential("access-token", "intl", "desktop") + "\n" + workBuddyCredential("second-token", "cn", "cli"),
	}

	models, err := GetWorkBuddyChannelModels(context.Background(), channel)
	if err != nil {
		t.Fatalf("GetWorkBuddyChannelModels: %v", err)
	}
	if strings.Join(models, ",") != "gpt-6-astra" {
		t.Fatalf("models = %v", models)
	}
}

// firstWorkBuddyCredential hands back the line index so a rotated token is
// written where it came from and the other accounts are left alone.
func TestFirstWorkBuddyCredentialSkipsBlankLinesAndKeepsTheIndex(t *testing.T) {
	first := workBuddyCredential("access-a", "intl", "desktop")
	second := workBuddyCredential("access-b", "intl", "cli")

	lines, index, credential, err := firstWorkBuddyCredential("\n" + first + "\n" + second)
	if err != nil {
		t.Fatalf("firstWorkBuddyCredential: %v", err)
	}
	if index != 1 {
		t.Fatalf("index = %d, want 1", index)
	}
	if credential.AccessToken != "access-a" {
		t.Fatalf("access token = %q", credential.AccessToken)
	}
	if len(lines) != 3 || lines[2] != second {
		t.Fatalf("lines = %v; the neighbours must survive a rotation", lines)
	}
}
