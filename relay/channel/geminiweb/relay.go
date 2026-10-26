package geminiweb

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/geminiweb/engine"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const (
	chunkObject      = "chat.completion.chunk"
	completionObject = "chat.completion"
)

// streamPrelude emits the opening frame that announces the assistant role.
func streamPrelude(w io.Writer, id, model string, created int64) error {
	role := "assistant"
	chunk := dto.ChatCompletionsStreamResponse{
		Id:      id,
		Object:  chunkObject,
		Created: created,
		Model:   model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: role},
		}},
	}
	return writeSSE(w, chunk)
}

// streamDelta emits one content or reasoning fragment.
func streamDelta(w io.Writer, id, model string, created int64, content, reasoning string) error {
	delta := dto.ChatCompletionsStreamResponseChoiceDelta{}
	if reasoning != "" {
		delta.SetReasoningContent(reasoning)
	}
	if content != "" {
		delta.SetContentString(content)
	}
	chunk := dto.ChatCompletionsStreamResponse{
		Id:      id,
		Object:  chunkObject,
		Created: created,
		Model:   model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, Delta: delta}},
	}
	return writeSSE(w, chunk)
}

// streamFinish closes the stream: the finish frame, the usage frame the billing
// path reads, and the DONE sentinel.
func streamFinish(w io.Writer, id, model string, created int64, usage *dto.Usage) error {
	reason := "stop"
	chunk := dto.ChatCompletionsStreamResponse{
		Id:      id,
		Object:  chunkObject,
		Created: created,
		Model:   model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index:        0,
			Delta:        dto.ChatCompletionsStreamResponseChoiceDelta{},
			FinishReason: &reason,
		}},
	}
	if err := writeSSE(w, chunk); err != nil {
		return err
	}
	if usage != nil {
		usageChunk := dto.ChatCompletionsStreamResponse{
			Id:      id,
			Object:  chunkObject,
			Created: created,
			Model:   model,
			Choices: []dto.ChatCompletionsStreamResponseChoice{},
			Usage:   usage,
		}
		if err := writeSSE(w, usageChunk); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "data: [DONE]\n\n")
	return err
}

// streamFailure reports an upstream failure inside an already-open stream. The
// stream cannot be rewound, so the client gets an error frame instead of a
// truncated answer.
func streamFailure(w io.Writer, message string) {
	payload, err := common.Marshal(map[string]any{
		"error": map[string]any{"message": message, "type": "upstream_error"},
	})
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// streamToolCalls emits the tool calls of a finished answer as one delta.
func streamToolCalls(w io.Writer, id, model string, created int64, calls []dto.ToolCallResponse) error {
	for index := range calls {
		position := index
		calls[index].Index = &position
	}
	chunk := dto.ChatCompletionsStreamResponse{
		Id:      id,
		Object:  chunkObject,
		Created: created,
		Model:   model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: calls},
		}},
	}
	return writeSSE(w, chunk)
}

// streamFinishReason closes a stream with an explicit finish reason.
func streamFinishReason(w io.Writer, id, model string, created int64, reason string, usage *dto.Usage) error {
	chunk := dto.ChatCompletionsStreamResponse{
		Id:      id,
		Object:  chunkObject,
		Created: created,
		Model:   model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index:        0,
			Delta:        dto.ChatCompletionsStreamResponseChoiceDelta{},
			FinishReason: &reason,
		}},
	}
	if err := writeSSE(w, chunk); err != nil {
		return err
	}
	if usage != nil {
		usageChunk := dto.ChatCompletionsStreamResponse{
			Id:      id,
			Object:  chunkObject,
			Created: created,
			Model:   model,
			Choices: []dto.ChatCompletionsStreamResponseChoice{},
			Usage:   usage,
		}
		if err := writeSSE(w, usageChunk); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "data: [DONE]\n\n")
	return err
}

func writeSSE(w io.Writer, chunk dto.ChatCompletionsStreamResponse) error {
	payload, err := common.Marshal(chunk)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

// bufferedResponse turns a completed call into the single chat.completion body
// a buffered client expects.
func bufferedResponse(result *engine.Result, model, prompt, requestId string) (*http.Response, error) {
	usage := estimateUsage(model, prompt, result.Text)
	message := dto.Message{Role: "assistant", Content: result.Text}
	if result.Reasoning != "" {
		reasoning := result.Reasoning
		message.ReasoningContent = &reasoning
	}
	response := dto.OpenAITextResponse{
		Id:      requestId,
		Object:  completionObject,
		Model:   model,
		Created: time.Now().Unix(),
		Choices: []dto.OpenAITextResponseChoice{{
			Index:        0,
			Message:      message,
			FinishReason: "stop",
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

var errNoPrompt = errors.New("gemini web channel: the request carries no text content")

// requirePrompt guards the endpoint against requests it cannot serve.
func requirePrompt(prompt string) error {
	if prompt == "" {
		return errNoPrompt
	}
	return nil
}
