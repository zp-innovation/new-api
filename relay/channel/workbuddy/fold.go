package workbuddy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/tidwall/gjson"
)

// maxFoldBodyBytes bounds the body a fold will buffer. A buffered answer is
// small by construction; the cap only exists so a misbehaving upstream cannot
// make the relay allocate without limit.
const maxFoldBodyBytes = 32 << 20

// foldChatCompletionsStream rewrites an always-streaming WorkBuddy answer as
// the single chat.completion object a buffered client expects, so the buffered
// OpenAI handler can treat the folded body like any other JSON reply.
func foldChatCompletionsStream(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return errors.New("workbuddy channel: empty upstream response")
	}

	// One byte over the cap tells a full body from a truncated one: a fold that
	// read the whole allowance cannot prove it saw the end of the answer.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFoldBodyBytes+1))
	if err != nil {
		_ = resp.Body.Close()
		return err
	}
	_ = resp.Body.Close()
	if len(body) > maxFoldBodyBytes {
		return fmt.Errorf("workbuddy channel: the upstream stream exceeded the %d byte fold limit", maxFoldBodyBytes)
	}

	folded, err := foldChatCompletionsChunks(body)
	if err != nil {
		return err
	}

	resp.Body = io.NopCloser(bytes.NewReader(folded))
	resp.ContentLength = int64(len(folded))
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Del("Content-Length")
	return nil
}

// foldChatCompletionsChunks accumulates the deltas of a chat.completion stream
// into one buffered chat.completion payload. The rules mirror the reference
// gateway's aggregate_stream (wb_proxy.py:9223): content and reasoning
// fragments are concatenated, tool-call fragments are merged by index, the last
// usage frame wins, and empty tool-call placeholders are dropped. The fold only
// succeeds for a stream that proved it finished: it must carry the [DONE]
// terminator or a non-empty finish_reason.
func foldChatCompletionsChunks(body []byte) ([]byte, error) {
	var (
		id           string
		model        string
		created      int64
		role         string
		content      strings.Builder
		reasoning    strings.Builder
		toolCalls    []dto.ToolCallResponse
		finishReason string
		usage        *dto.Usage
		sawChunk     bool
		sawDone      bool
	)

	for line := range bytes.SplitSeq(body, []byte{10}) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 {
			continue
		}
		if bytes.Equal(payload, []byte("[DONE]")) {
			// The terminator is the upstream's own proof that the answer is
			// complete; dropping it silently would let a half answer pass as a
			// finished reply.
			sawDone = true
			continue
		}
		if message := streamErrorMessage(payload); message != "" {
			return nil, errors.New("workbuddy channel: " + message)
		}

		var chunk dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal(payload, &chunk); err != nil {
			// Keep-alive comments and unknown frames are not fatal: the
			// upstream interleaves them with the completion frames.
			continue
		}
		sawChunk = true
		if id == "" {
			id = chunk.Id
		}
		if model == "" {
			model = chunk.Model
		}
		if created == 0 {
			created = chunk.Created
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Role != "" {
				role = choice.Delta.Role
			}
			content.WriteString(choice.Delta.GetContentString())
			reasoning.WriteString(choice.Delta.GetReasoningContent())
			for _, call := range choice.Delta.ToolCalls {
				toolCalls = mergeToolCall(toolCalls, call)
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				finishReason = *choice.FinishReason
			}
		}
	}

	if !sawChunk {
		return nil, errors.New("workbuddy channel: the upstream stream carried no completion chunk")
	}
	// A stream that stopped before its terminator is incomplete, not short: an
	// upstream timeout mid-answer must not be folded into a finished reply.
	// This mirrors the incomplete-stream rule in relay/helper/stream_scanner.go.
	if !sawDone && finishReason == "" {
		return nil, errors.New("workbuddy channel: the upstream stream ended before completion")
	}

	if role == "" {
		role = "assistant"
	}
	if finishReason == "" {
		finishReason = "stop"
	}
	if id == "" {
		id = "chatcmpl-workbuddy"
	}

	message := dto.Message{Role: role, Content: content.String()}
	if reasoning.Len() > 0 {
		value := reasoning.String()
		message.ReasoningContent = &value
	}
	if len(toolCalls) > 0 {
		// A buffered tool call carries no stream index.
		for index := range toolCalls {
			toolCalls[index].Index = nil
		}
		encoded, err := common.Marshal(toolCalls)
		if err != nil {
			return nil, fmt.Errorf("workbuddy channel: encode tool calls: %w", err)
		}
		message.ToolCalls = encoded
	}

	response := dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Model:   model,
		Created: created,
		Choices: []dto.OpenAITextResponseChoice{{
			Index:        0,
			Message:      message,
			FinishReason: finishReason,
		}},
	}
	if usage != nil {
		response.Usage = *usage
	}

	payload, err := common.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("workbuddy channel: encode folded response: %w", err)
	}
	return payload, nil
}

// mergeToolCall folds one streamed tool-call delta into the accumulated list.
// The upstream streams a call across frames: the first frame carries the id and
// the name, later frames append argument fragments. A placeholder that carries
// nothing (the upstream sends one when a call is abandoned) is dropped, exactly
// like the reference's aggregate_stream (wb_proxy.py:9223).
func mergeToolCall(calls []dto.ToolCallResponse, delta dto.ToolCallResponse) []dto.ToolCallResponse {
	if strings.TrimSpace(delta.ID) == "" && strings.TrimSpace(delta.Function.Name) == "" && delta.Function.Arguments == "" {
		return calls
	}
	index := 0
	if delta.Index != nil && *delta.Index > 0 {
		index = *delta.Index
	}
	for len(calls) <= index {
		position := len(calls)
		calls = append(calls, dto.ToolCallResponse{Index: &position})
	}
	target := &calls[index]
	if delta.ID != "" {
		target.ID = delta.ID
	}
	if delta.Type != nil {
		target.Type = delta.Type
	}
	if delta.Function.Name != "" {
		target.Function.Name = delta.Function.Name
	}
	target.Function.Arguments += delta.Function.Arguments
	return calls
}

// streamErrorMessage extracts the message of a mid-stream error frame. The
// upstream reports a failure as a regular SSE frame carrying {"error": ...}.
func streamErrorMessage(payload []byte) string {
	for _, path := range []string{"error.message", "message"} {
		value := gjson.GetBytes(payload, path)
		if !value.Exists() || value.Type != gjson.String {
			continue
		}
		if message := strings.TrimSpace(value.String()); message != "" {
			return message
		}
	}
	return ""
}
