package commandcode

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchUsageReadsWhoamiCreditsAndWindows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		assert.Equal(t, DefaultUserAgent, request.Header.Get("User-Agent"))
		switch request.URL.Path {
		case WhoamiPath:
			_, _ = writer.Write([]byte(`{"user":{"id":"u1","userName":"Alice"},"org":{"id":"org-1","name":"Example Org"}}`))
		case CreditsPath:
			assert.Equal(t, "org-1", request.URL.Query().Get("orgId"))
			_, _ = writer.Write([]byte(`{"credits":{"monthlyCredits":12.5,"purchasedCredits":3,"freeCredits":1},"windowLimits":{"limited":true,"fiveHour":{"used":2,"cap":10,"resetAt":1893456000000},"weekly":{"used":4,"cap":20,"resetAt":1893456000000}}}`))
		case SubscriptionsPath:
			_, _ = writer.Write([]byte(`{"data":{"planId":"individual-pro","currentPeriodStart":"2026-10-01T00:00:00Z","currentPeriodEnd":"2026-11-01T00:00:00Z"}}`))
		case UsageSummaryPath:
			assert.Equal(t, "2026-10-01T00:00:00Z", request.URL.Query().Get("since"))
			_, _ = writer.Write([]byte(`{"totalCost":"7.5","totalMonthlyCredits":12.5}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL)
	usage, err := client.FetchUsage(context.Background(), "user_test")
	require.NoError(t, err)
	assert.Equal(t, "u1", usage.UserID)
	assert.Equal(t, "Alice", usage.UserName)
	assert.Equal(t, "org-1", usage.OrgID)
	assert.Equal(t, "Example Org", usage.OrgName)
	assert.Equal(t, "Pro", usage.PlanName)
	assert.InDelta(t, 12.5, usage.MonthlyCredits, 0.001)
	assert.InDelta(t, 7.5, usage.MonthlyUsed, 0.001)
	assert.InDelta(t, 20, usage.MonthlyCap, 0.001)
	require.NotNil(t, usage.FiveHour)
	assert.InDelta(t, 20, usage.FiveHour.Percent, 0.001)
}

func TestGetJSONRejectsAnOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(bytes.Repeat([]byte(" "), MaxResponseBytes+1))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL)
	var out map[string]any
	err := client.getJSON(context.Background(), server.URL+WhoamiPath, "user_test", nil, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("response exceeds the %d-byte limit", MaxResponseBytes))
}

func TestGetJSONTruncatesAnUpstreamErrorBodyByRunes(t *testing.T) {
	const repeated = 2000
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(strings.Repeat("中", repeated)))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL)
	_, err := client.ValidateAPIKey(context.Background(), "user_bad")
	require.Error(t, err)
	assert.True(t, IsAuthError(err))
	assert.Equal(t, ErrUnauthorized.Error()+": "+strings.Repeat("中", maxErrorBodyRunes)+"...", err.Error())
	assert.True(t, utf8.ValidString(err.Error()))
}

func TestValidateAPIKeyRejectsUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL)
	_, err := client.ValidateAPIKey(context.Background(), "user_bad")
	require.Error(t, err)
	assert.True(t, IsAuthError(err))
}
