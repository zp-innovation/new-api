package workbuddy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	return body
}

func newTaskClient(server *httptest.Server, realm Realm) *Client {
	client := NewClient(nil, server.URL, realm, ProductCLI)
	client.BillingBaseURL = server.URL
	client.WebBaseURL = server.URL
	return client
}

// The upstream answers 10001 when the account already signed in today, either
// with HTTP 200 or with HTTP 400 and the code inside the envelope (the live edge
// does the latter: "今天已签到，请明天再来"). Both are a success: the credit was
// granted and a rerun must not look like a failure.
func TestCheckinTreatsAlreadyCheckedInAsSuccess(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{name: "code in a 200 body", status: http.StatusOK},
		{name: "code in a 400 body", status: http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, CheckinPath, r.URL.Path)
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(`{"code":10001,"msg":"今天已签到，请明天再来"}`))
			}))
			defer server.Close()

			result, err := newTaskClient(server, RealmCN).Checkin(context.Background(), Identity{Realm: RealmCN, UID: "uid-1", AccessToken: "token"})
			require.NoError(t, err)
			assert.True(t, result.OK)
			assert.True(t, result.Duplicate)
		})
	}
}

func TestCheckinReportsATransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"msg":"boom"}`))
	}))
	defer server.Close()

	result, err := newTaskClient(server, RealmCN).Checkin(context.Background(), Identity{Realm: RealmCN, UID: "uid-1", AccessToken: "token"})
	require.Error(t, err)
	assert.False(t, result.OK)
}

// The upstream drops an event batch without a userId, silently answering 200.
func TestChatActivityEventCarriesTheUserId(t *testing.T) {
	event := ChatActivityEvent("uid-42", time.UnixMilli(1790000000000))
	assert.Equal(t, "chat_request_send", event["eventCode"])
	assert.Equal(t, "uid-42", event["userId"])
	assert.NotEmpty(t, event["conversationId"])
	assert.NotEqual(t, event["conversationId"], event["requestId"])
	// The report pins its own model pair; it is not the model the daily chat
	// actually sends.
	assert.Equal(t, activityReportModelID, event["requestModelId"])
	assert.Equal(t, activityReportModelName, event["requestModelName"])
	assert.NotEqual(t, dailyChatModel, event["requestModelId"])
}

// The growth callers wrap transport failures with %w, so the *APIError check
// has to unwrap instead of asserting the concrete type (growth.go:276).
func TestAsAPIErrorFindsAWrappedAPIError(t *testing.T) {
	wrapped := fmt.Errorf("claim gift: %w", &APIError{Code: 7, HTTPStatus: http.StatusServiceUnavailable, Message: "busy"})
	var apiErr *APIError
	require.True(t, asAPIError(wrapped, &apiErr))
	require.NotNil(t, apiErr)
	assert.Equal(t, 7, apiErr.Code)
	assert.True(t, isHTTPStatus(wrapped, http.StatusServiceUnavailable))
	assert.False(t, isHTTPStatus(wrapped, http.StatusTooManyRequests))
	assert.False(t, asAPIError(fmt.Errorf("not an api error"), &apiErr))
}

func TestFetchGrowthTasksAppliesTheSpecFallbacks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, GrowthTasks, r.URL.Path)
		_, _ = w.Write([]byte(`{"code":0,"data":{"tasks":[
			{"task_code":"create_canvas","title":"","accept_status":"","task_type":"single","progress":{"current":0}},
			{"task_code":"wb_wechat_oa_subscribe_task","title":"Follow","task_type":"single","progress":{"current":0,"target":1}}
		]}}`))
	}))
	defer server.Close()

	tasks, err := newTaskClient(server, RealmCN).FetchGrowthTasks(context.Background(), Identity{Realm: RealmCN, UID: "uid-1", AccessToken: "token"})
	require.NoError(t, err)
	require.Len(t, tasks, 2)

	canvas := tasks[0]
	assert.Equal(t, "Create a design", canvas.Name)
	assert.Equal(t, 1, canvas.Target)
	assert.Equal(t, float64(300), canvas.RewardCredit)
	assert.Equal(t, "not_accepted", canvas.Status)
	assert.Equal(t, "canvas", canvas.Kind)
	assert.True(t, canvas.NeedsAcceptance())

	subscribe := tasks[1]
	assert.True(t, subscribe.Unforgeable)
	assert.False(t, subscribe.NeedsAcceptance())
}

// Acceptance is chunked and every code has to be accounted for, because events
// reported against an unaccepted task never move its progress.
func TestAcceptGrowthTasksChunksAndReportsFailures(t *testing.T) {
	var batches [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TaskCodes []string `json:"task_codes"`
		}
		require.NoError(t, common.Unmarshal(readBody(t, r), &body))
		batches = append(batches, body.TaskCodes)
		results := make([]map[string]any, 0, len(body.TaskCodes))
		for i, code := range body.TaskCodes {
			status := "accepted"
			if i == 1 {
				status = "rejected"
			}
			results = append(results, map[string]any{"task_code": code, "status": status, "message": ""})
		}
		encoded, err := common.Marshal(map[string]any{"code": 0, "data": map[string]any{"results": results}})
		require.NoError(t, err)
		_, _ = w.Write(encoded)
	}))
	defer server.Close()

	codes := make([]string, 0, 25)
	for i := range 25 {
		codes = append(codes, "task_"+string(rune('a'+i)))
	}
	result, err := newTaskClient(server, RealmCN).AcceptGrowthTasks(context.Background(), Identity{Realm: RealmCN, UID: "uid-1", AccessToken: "token"}, codes)
	require.NoError(t, err)
	require.Len(t, batches, 2)
	assert.Len(t, batches[0], acceptChunkSize)
	assert.Len(t, batches[1], 5)
	assert.Len(t, result.Accepted, 23)
	assert.Len(t, result.Failed, 2)
}

// The chat host rejects a claim with HTTP 400 until the progress lands, and the
// reference retries on the growth-centre web host with the browser identity.
func TestClaimGrowthTaskFallsBackToTheWebHost(t *testing.T) {
	var webCalls int
	origin := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Client-Platform") == "web" {
			webCalls++
			assert.Equal(t, origin, r.Header.Get("Origin"))
			assert.Contains(t, r.Header.Get("Referer"), "growth-center")
			_, _ = w.Write([]byte(`{"code":0,"data":{"credit":100}}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":400,"msg":"task not completed"}`))
	}))
	defer server.Close()
	origin = server.URL

	client := newTaskClient(server, RealmCN)
	client.WebBaseURL = server.URL
	result := client.ClaimGrowthTask(context.Background(), Identity{Realm: RealmCN, UID: "uid-1", AccessToken: "token"}, "skill_1")
	require.True(t, result.OK)
	assert.Equal(t, float64(100), result.Credit)
	assert.Equal(t, 1, webCalls)
}

// Expert and team events are deduplicated upstream by (eventCode, id), so a
// batch has to walk the pool instead of repeating one id.
func TestBuildGrowthEventUsesDistinctExpertIDs(t *testing.T) {
	now := time.UnixMilli(1790000000000)
	first := BuildGrowthEvent("uid-1", "expert", 0, ExpertIDFor("expert", 0), now)
	second := BuildGrowthEvent("uid-1", "expert", 1, ExpertIDFor("expert", 1), now)
	assert.Equal(t, "expert_actual_use", first["eventCode"])
	assert.NotEqual(t, first["id"], second["id"])
	assert.Equal(t, "uid-1", first["userId"])

	team := BuildGrowthEvent("uid-1", "team", 0, ExpertIDFor("team", 0), now)
	assert.Equal(t, "team", team["expertType"])
}

func TestInNightWindow(t *testing.T) {
	at := func(hour int) time.Time {
		return time.Date(2026, 10, 12, hour, 0, 0, 0, time.Local)
	}
	assert.True(t, InNightWindow(at(23)))
	assert.True(t, InNightWindow(at(1)))
	assert.True(t, InNightWindow(at(7)))
	assert.False(t, InNightWindow(at(8)))
	assert.False(t, InNightWindow(at(22)))
}

// The daily credit comes from the web leg alone: a desktop leg that succeeded
// while the web leg failed must not be reported as a completed day, or the day
// is silently lost and never retried.
func TestDailyChatRequiresTheWebLeg(t *testing.T) {
	var webAttempted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/chat/completions":
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case strings.HasPrefix(r.URL.Path, conversationsPath):
			webAttempted = true
			_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newTaskClient(server, RealmIntl)
	result := client.DailyChat(context.Background(), Identity{Realm: RealmIntl, UID: "uid-1", AccessToken: "token"}, true)
	assert.True(t, webAttempted)
	assert.False(t, result.OK, "a failed web leg must not count as a completed day")
	assert.True(t, result.WebAttempted)
	assert.False(t, result.WebOK)
}

// Without the web leg the reference keeps its legacy meaning: the desktop chat
// alone counts.
func TestDailyChatWithoutTheWebLeg(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/chat/completions", r.URL.Path)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	result := newTaskClient(server, RealmIntl).DailyChat(context.Background(), Identity{Realm: RealmIntl, UID: "uid-1", AccessToken: "token"}, false)
	assert.True(t, result.OK)
	assert.False(t, result.WebAttempted)
}
