package codex

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const codexStreamBody = "event: response.created\n" +
	"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_partial\"}}\n\n" +
	"event: response.output_text.delta\n" +
	"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
	"event: response.completed\n" +
	"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-6-luna\",\"output\":[{\"type\":\"message\"}],\"usage\":{\"input_tokens\":3,\"output_tokens\":7,\"total_tokens\":10}}}\n\n" +
	"data: [DONE]\n\n"

// The ChatGPT Codex backend answers a buffered request with HTTP 400
// "Stream must be set to true", so the adaptor always asks upstream to stream.
func TestConvertOpenAIResponsesRequestAlwaysStreamsUpstream(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex},
		RelayMode:   relayconstant.RelayModeResponses,
	}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model:  "gpt-6-luna",
		Input:  json.RawMessage(`"hello"`),
		Stream: lo.ToPtr(false),
	})
	require.NoError(t, err)

	request, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.NotNil(t, request.Stream)
	assert.True(t, *request.Stream)
	assert.JSONEq(t, "false", string(request.Store))
}

func TestFinalResponseFromCodexStreamFoldsCompletedEvent(t *testing.T) {
	final, err := finalResponseFromCodexStream([]byte(codexStreamBody))
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, json.Unmarshal(final, &response))
	assert.Equal(t, "resp_1", response["id"])
	assert.Equal(t, "gpt-6-luna", response["model"])
	usage, ok := response["usage"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(10), usage["total_tokens"])
}

func TestFinalResponseFromCodexStreamReportsStreamError(t *testing.T) {
	body := "event: error\n" +
		"data: {\"type\":\"error\",\"error\":{\"message\":\"usage limit reached\"}}\n\n"

	_, err := finalResponseFromCodexStream([]byte(body))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usage limit reached")
}

func TestFinalResponseFromCodexStreamRejectsIncompleteStream(t *testing.T) {
	body := "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"

	_, err := finalResponseFromCodexStream([]byte(body))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ended without a response")
}

func TestFoldCodexResponsesStreamRewritesTheResponse(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:   io.NopCloser(strings.NewReader(codexStreamBody)),
	}

	require.NoError(t, foldCodexResponsesStream(resp))

	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, int64(len(body)), resp.ContentLength)
	assert.False(t, bytes.Contains(body, []byte("response.completed")))
	assert.Contains(t, string(body), "resp_1")
}

func TestGetRequestURLAlphaSearch(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCodex,
			ChannelBaseUrl: "https://chatgpt.com",
		},
		RelayMode: relayconstant.RelayModeAlphaSearch,
	}

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://chatgpt.com/backend-api/codex/alpha/search", url)
}

// The Codex backend rejects these fields, so the adaptor clears them rather
// than forwarding what the client sent.
func TestConvertOpenAIResponsesRequestDropsPenalties(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex},
		RelayMode:   relayconstant.RelayModeResponses,
	}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model:            "gpt-5-codex",
		Input:            json.RawMessage(`"hello"`),
		MaxOutputTokens:  lo.ToPtr(uint(128)),
		Temperature:      lo.ToPtr(1.0),
		FrequencyPenalty: json.RawMessage(`1.5`),
		PresencePenalty:  json.RawMessage(`1.5`),
	})
	require.NoError(t, err)

	request, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	assert.Nil(t, request.MaxOutputTokens)
	assert.Nil(t, request.Temperature)
	assert.Nil(t, request.FrequencyPenalty)
	assert.Nil(t, request.PresencePenalty)
}
