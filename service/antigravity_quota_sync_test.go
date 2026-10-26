package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/model"
	agcore "github.com/QuantumNous/new-api/pkg/antigravity"
)

// The shape below is copied from a real tx101 response: an exhausted account
// still reports quotaInfo, but only with resetTime.
func TestQuotaRowsFromCatalogueReadsExhaustedAndHealthyModels(t *testing.T) {
	fetchedAt := int64(1792194087)
	response := &agcore.FetchAvailableModelsResponse{
		Models: map[string]agcore.ModelInfo{
			"claude-opus-4-6-thinking": {
				DisplayName: "Claude Opus 4.6 (Thinking)",
				QuotaInfo:   &agcore.ModelQuotaInfo{ResetTime: "2026-10-17T01:47:13Z"},
			},
			"gemini-3.8-flash-high": {
				DisplayName: "Gemini 3.8 Flash (High)",
				QuotaInfo: &agcore.ModelQuotaInfo{
					RemainingFraction: 1,
					ResetTime:         "2026-10-17T05:55:27Z",
				},
			},
			// No quotaInfo at all: the catalogue did not report quota for this
			// model, so it must not produce a row.
			"tab_flash_lite_preview": {DisplayName: "Tab"},
			"   ":                    {QuotaInfo: &agcore.ModelQuotaInfo{RemainingFraction: 1}},
		},
	}

	rows := quotaRowsFromCatalogue(26, response, fetchedAt)
	require.Len(t, rows, 2)

	byModel := map[string]int{}
	for i, row := range rows {
		byModel[row.Model] = i
		assert.Equal(t, 26, row.ChannelId)
		assert.Equal(t, fetchedAt, row.FetchedAt)
		assert.Equal(t, "ok", row.Status)
	}

	claude := rows[byModel["claude-opus-4-6-thinking"]]
	assert.Zero(t, claude.RemainingFraction, "an omitted fraction means exhausted")
	wantReset, err := time.Parse(time.RFC3339, "2026-10-17T01:47:13Z")
	require.NoError(t, err)
	assert.Equal(t, wantReset.Unix(), claude.ResetTime)

	gemini := rows[byModel["gemini-3.8-flash-high"]]
	assert.Equal(t, 1.0, gemini.RemainingFraction)
	wantGeminiReset, err := time.Parse(time.RFC3339, "2026-10-17T05:55:27Z")
	require.NoError(t, err)
	assert.Equal(t, wantGeminiReset.Unix(), gemini.ResetTime)
}

func TestQuotaRowsFromCatalogueLeavesUnparsableResetAtZero(t *testing.T) {
	rows := quotaRowsFromCatalogue(40, &agcore.FetchAvailableModelsResponse{
		Models: map[string]agcore.ModelInfo{
			"claude-opus-4-6-thinking": {
				QuotaInfo: &agcore.ModelQuotaInfo{ResetTime: "not-a-timestamp"},
			},
		},
	}, 1792194087)

	require.Len(t, rows, 1)
	assert.Zero(t, rows[0].ResetTime)
}

func TestQuotaRowsFromCatalogueHandlesEmptyResponses(t *testing.T) {
	assert.Nil(t, quotaRowsFromCatalogue(26, nil, 0))
	assert.Nil(t, quotaRowsFromCatalogue(26, &agcore.FetchAvailableModelsResponse{}, 0))
	assert.Empty(t, quotaRowsFromCatalogue(26, &agcore.FetchAvailableModelsResponse{
		Models: map[string]agcore.ModelInfo{"a": {}},
	}, 0), "a model without quotaInfo yields no row")
}

func TestTakeNewlyExhaustedModelsAlertsOncePerOutage(t *testing.T) {
	const modelName = "claude-opus-4-6-thinking"
	quotaExhaustedNotified.Delete(modelName)
	t.Cleanup(func() { quotaExhaustedNotified.Delete(modelName) })

	drained := []model.ChannelModelQuotaSummary{
		{Model: modelName, TotalAccounts: 10, AvailableAccounts: 0, EarliestReset: 1792200000},
	}
	require.Len(t, takeNewlyExhaustedModels(drained), 1, "the first transition must alert")
	assert.Empty(t, takeNewlyExhaustedModels(drained), "a still-drained model must not alert again")

	healthy := []model.ChannelModelQuotaSummary{
		{Model: modelName, TotalAccounts: 10, AvailableAccounts: 3},
	}
	assert.Empty(t, takeNewlyExhaustedModels(healthy))
	assert.Len(t, takeNewlyExhaustedModels(drained), 1, "recovery must re-arm the alert")
}

func TestTakeNewlyExhaustedModelsIgnoresModelsWithoutAccounts(t *testing.T) {
	quotaExhaustedNotified.Delete("empty-model")
	t.Cleanup(func() { quotaExhaustedNotified.Delete("empty-model") })
	assert.Empty(t, takeNewlyExhaustedModels([]model.ChannelModelQuotaSummary{
		{Model: "empty-model", TotalAccounts: 0, AvailableAccounts: 0},
	}))
}
