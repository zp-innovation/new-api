package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

func TestAntigravityChannelBaseURL(t *testing.T) {
	custom := "https://daily.example.test/"
	customChannel := &model.Channel{
		Type:    constant.ChannelTypeAntigravity,
		BaseURL: &custom,
	}
	assert.Equal(t, "https://daily.example.test", antigravityChannelBaseURL(customChannel))

	defaultChannel := &model.Channel{Type: constant.ChannelTypeAntigravity}
	assert.Equal(t, constant.GetChannelBaseURL(constant.ChannelTypeAntigravity), antigravityChannelBaseURL(defaultChannel))
	assert.Equal(t, constant.GetChannelBaseURL(constant.ChannelTypeAntigravity), antigravityChannelBaseURL(&model.Channel{}), "a narrow channel projection without type still uses the Antigravity default")
	assert.Empty(t, antigravityChannelBaseURL(nil))
}

func TestGetAntigravityChannelModelsUsesChannelBaseURL(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer at-selected" {
			t.Errorf("authorization = %q, want selected credential", got)
		}
		_, _ = w.Write([]byte(`{"models":{"gemini-4-argon-high":{"displayName":"Gemini 4 Argon High"}}}`))
	}))
	defer server.Close()

	baseURL := server.URL
	channel := &model.Channel{
		Type:    constant.ChannelTypeAntigravity,
		BaseURL: &baseURL,
		Key:     `{"access_token":"at-selected","project_id":"proj-selected"}`,
	}
	models, err := GetAntigravityChannelModels(context.Background(), channel)
	require.NoError(t, err)
	assert.Equal(t, []string{"gemini-4-argon-high"}, models)
	assert.Equal(t, "/v1internal:fetchAvailableModels", gotPath)
}

// GetAntigravityChannelModels backs the channel drawer's "fetch from upstream"
// action, so a malformed channel must fail before any network call is made.
func TestGetAntigravityChannelModelsRejectsUnusableChannels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		channel *model.Channel
		wantErr string
	}{
		{name: "missing channel", channel: nil, wantErr: "channel not found"},
		{
			name:    "other channel type",
			channel: &model.Channel{Type: constant.ChannelTypeOpenAI},
			wantErr: "channel type is not Antigravity",
		},
		{
			name:    "key is not a credential",
			channel: &model.Channel{Type: constant.ChannelTypeAntigravity, Key: "not-json"},
			wantErr: "invalid credential json",
		},
		{
			name:    "credential without a project",
			channel: &model.Channel{Type: constant.ChannelTypeAntigravity, Key: `{"access_token":"token"}`},
			wantErr: "project_id is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			models, err := GetAntigravityChannelModels(context.Background(), tc.channel)
			if err == nil {
				t.Fatalf("GetAntigravityChannelModels() = %v, want error containing %q", models, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
			if models != nil {
				t.Fatalf("models = %v, want nil on error", models)
			}
		})
	}
}
