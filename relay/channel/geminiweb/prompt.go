package geminiweb

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// exitCodeRe pulls the exit status out of a tool result wrapper. Agentic
// clients report success as prose, which a small model reads as "no output".
var exitCodeRe = regexp.MustCompile("(?i)exit(?:ed with|\\s*code)?\\s*(?:code\\s*)?(\\d+)")

// formatToolResult renders a tool observation so the model can tell success
// from failure. Without this, a command that succeeds silently (writing a file)
// looks like nothing happened and the model retries it forever.
func formatToolResult(name, raw string) string {
	label := "Tool result"
	if name != "" {
		label = "Tool result for " + name
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return label + ": completed with empty output (normal for write/set commands; the action took effect, do not repeat it)."
	}
	output := trimmed
	if idx := strings.LastIndex(trimmed, "Output:"); idx >= 0 {
		output = strings.TrimSpace(trimmed[idx+len("Output:"):])
	}
	if match := exitCodeRe.FindStringSubmatch(trimmed); match != nil {
		if match[1] == "0" {
			if output == "" {
				return label + ": the harness executed it successfully (exit 0) on the user's machine and the " +
					"action took effect. There is no text output, which is normal for write/set commands. " +
					"Do not repeat it; confirm to the user that it is done."
			}
			return label + ": the harness executed it successfully (exit 0). Output:\n" + output
		}
		return label + ": the harness ran it and it failed (exit " + match[1] + "). Output:\n" + output
	}
	return label + ": " + raw
}

// buildPrompt flattens an OpenAI conversation into the single text prompt the
// web client sends. The web endpoint has no message array, so role markers are
// the only way to keep the model aware of who said what.
func buildPrompt(messages []dto.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		content := contentText(message.Content)
		switch message.Role {
		case "system", "developer":
			// Emitted verbatim and without a bracketed role marker: the web
			// model treats "[System instruction]:" prefixes as quoted text and
			// silently ignores them, while the same words as plain prose are
			// followed. Verified with a "start every answer with BANANA" probe.
			if content != "" {
				parts = append(parts, content)
			}
		case "assistant":
			// A previous turn may have been a tool call with no prose: replay it
			// in the same fenced shape the model is asked to produce, so the
			// conversation reads as a real call/result exchange.
			rendered := content
			if len(message.ToolCalls) > 0 {
				var calls []dto.ToolCallResponse
				if err := common.Unmarshal(message.ToolCalls, &calls); err == nil {
					for _, call := range calls {
						arguments := call.Function.Arguments
						if strings.TrimSpace(arguments) == "" {
							arguments = "{}"
						}
						rendered += "\n" + toolCallFence + "\n{\"name\": \"" + call.Function.Name +
							"\", \"arguments\": " + arguments + "}\n" + codeFence
					}
				}
			}
			if strings.TrimSpace(rendered) != "" {
				parts = append(parts, "Assistant: "+rendered)
			}
		case "tool":
			name := ""
			if message.Name != nil {
				name = *message.Name
			}
			parts = append(parts, formatToolResult(name, content))
		default:
			if content != "" {
				parts = append(parts, content)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// buildResponsesPrompt flattens an OpenAI Responses request body into the same
// single prompt the chat path produces. The Responses API carries the system
// prompt in instructions and the conversation in input, which may be a plain
// string or an array of input items.
func buildResponsesPrompt(input, instructions json.RawMessage) string {
	parts := make([]string, 0, 4)
	if len(instructions) > 0 {
		var text string
		if err := common.Unmarshal(instructions, &text); err == nil && strings.TrimSpace(text) != "" {
			parts = append(parts, "[System instruction]: "+strings.TrimSpace(text))
		}
	}
	if len(input) == 0 {
		return strings.Join(parts, "\n\n")
	}

	var plain string
	if err := common.Unmarshal(input, &plain); err == nil {
		if trimmed := strings.TrimSpace(plain); trimmed != "" {
			parts = append(parts, trimmed)
		}
		return strings.Join(parts, "\n\n")
	}

	var items []map[string]any
	if err := common.Unmarshal(input, &items); err != nil {
		return strings.Join(parts, "\n\n")
	}
	for _, item := range items {
		text := contentText(item["content"])
		if text == "" {
			if typed, ok := item["text"].(string); ok {
				text = strings.TrimSpace(typed)
			}
		}
		if text == "" {
			continue
		}
		if role, _ := item["role"].(string); role == "assistant" {
			parts = append(parts, "[Assistant]: "+text)
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

// contentText reduces a message payload to plain text. Multimodal parts keep
// only their text segments; Gemini Web takes no inline image on this path.
func contentText(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		var builder strings.Builder
		for _, item := range value {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			text, ok := part["text"].(string)
			if !ok {
				continue
			}
			builder.WriteString(text)
		}
		return strings.TrimSpace(builder.String())
	}
	return ""
}
