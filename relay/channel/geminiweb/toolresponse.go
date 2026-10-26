package geminiweb

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/geminiweb/engine"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// Tools are the one case where the answer cannot be forwarded as it streams: the
// model emits a fenced tool_call block, and a client must never see that fence
// as text. When tools are in play the reply is generated in full, split into
// text and calls, and only then rendered - for streaming clients as a short SSE
// burst, for buffered clients as one object.
func (a *Adaptor) toolCallResponse(ctx context.Context, account *model.GeminiWebAccount, credential engine.Credential,
	prompt, modelName, requestId string, created int64, clientStreams bool) (any, error) {
	result, err := engine.Generate(ctx, prompt, modelName, credential, nil, nil)
	if err != nil {
		// The account pool records this failure once it decides whether to retry
		// with another account; recording it here as well would count one failed
		// call twice and disable a working account too early.
		return nil, err
	}
	persistAccountResultCookie(credential.AccountID, result)
	markAccountResult(account, http.StatusOK, "")

	content, calls := parseToolCalls(result.Text)
	usage := estimateUsage(modelName, prompt, content)

	if clientStreams {
		return toolStreamResponse(requestId, modelName, created, content, calls, usage), nil
	}
	return toolBufferedResponse(requestId, modelName, content, calls, usage)
}

// toolStreamResponse renders the finished answer as an SSE body in one go.
func toolStreamResponse(requestId, modelName string, created int64, content string,
	calls []dto.ToolCallResponse, usage *dto.Usage) *http.Response {
	var buffer bytes.Buffer
	if err := streamPrelude(&buffer, requestId, modelName, created); err != nil {
		return toolErrorResponse(err)
	}
	if len(calls) > 0 {
		if err := streamToolCalls(&buffer, requestId, modelName, created, calls); err != nil {
			return toolErrorResponse(err)
		}
		if err := streamFinishReason(&buffer, requestId, modelName, created, "tool_calls", usage); err != nil {
			return toolErrorResponse(err)
		}
	} else {
		if content != "" {
			if err := streamDelta(&buffer, requestId, modelName, created, content, ""); err != nil {
				return toolErrorResponse(err)
			}
		}
		if err := streamFinishReason(&buffer, requestId, modelName, created, "stop", usage); err != nil {
			return toolErrorResponse(err)
		}
	}
	header := http.Header{}
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	payload := buffer.Bytes()
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(payload)),
		ContentLength: int64(len(payload)),
	}
}

func toolErrorResponse(err error) *http.Response {
	payload, _ := common.Marshal(map[string]any{
		"error": map[string]any{"message": err.Error(), "type": "upstream_error"},
	})
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(payload)),
	}
}

// toolBufferedResponse renders a chat.completion carrying either tool calls or
// plain content.
func toolBufferedResponse(requestId, modelName, content string, calls []dto.ToolCallResponse,
	usage *dto.Usage) (*http.Response, error) {
	message := dto.Message{Role: "assistant", Content: content}
	finishReason := "stop"
	if len(calls) > 0 {
		finishReason = "tool_calls"
		encoded, err := common.Marshal(calls)
		if err != nil {
			return nil, err
		}
		message.ToolCalls = encoded
		if strings.TrimSpace(content) == "" {
			message.Content = nil
		}
	}
	response := dto.OpenAITextResponse{
		Id:      requestId,
		Object:  completionObject,
		Model:   modelName,
		Created: time.Now().Unix(),
		Choices: []dto.OpenAITextResponseChoice{{
			Index:        0,
			Message:      message,
			FinishReason: finishReason,
		}},
		Usage: *usage,
	}
	payload, err := common.Marshal(response)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(payload)),
		ContentLength: int64(len(payload)),
	}, nil
}
