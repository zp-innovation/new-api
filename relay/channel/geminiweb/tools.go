package geminiweb

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// Gemini Web has no protocol-level tool channel. Following the reference
// sidecar, tools are described inside the prompt and the model answers with a
// fenced tool_call block that we parse back into OpenAI tool calls. The protocol
// is text on both ends, so the wording matters as much as the schema.
const toolCallFence = "\x60\x60\x60tool_call"

// codeFence closes a fenced block. It must not repeat the descriptor.
const codeFence = "\x60\x60\x60"

var toolCallFenceRe = regexp.MustCompile("(?s)\x60\x60\x60tool_call\\s*\\n(.*?)\x60\x60\x60")

// toolSpec is the trimmed tool description sent to the model.
type toolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// toolSpecsFrom reduces the request's tools to the fields worth prompting with.
func toolSpecsFrom(request *dto.GeneralOpenAIRequest) []toolSpec {
	if request == nil || len(request.Tools) == 0 {
		return nil
	}
	specs := make([]toolSpec, 0, len(request.Tools))
	for _, tool := range request.Tools {
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			continue
		}
		specs = append(specs, toolSpec{
			Name:        name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}
	return specs
}

// toolChoiceMode reports whether tools are disabled and which function the
// client pinned with tool_choice.
func toolChoiceMode(toolChoice any) (mode string, forced string) {
	switch value := toolChoice.(type) {
	case string:
		switch value {
		case "none", "required", "auto":
			return value, ""
		}
	case map[string]any:
		function, ok := value["function"].(map[string]any)
		if !ok {
			return "", ""
		}
		name, _ := function["name"].(string)
		if name != "" {
			return "required", name
		}
	}
	return "", ""
}

// augmentPromptWithTools prepends the tool protocol description to the prompt.
// The reminder is repeated at the end on purpose: agentic clients ship very long
// developer prompts that bury the opening instruction, and a model that forgets
// the protocol answers in prose instead of calling a tool.
func augmentPromptWithTools(prompt string, specs []toolSpec, toolChoice any) string {
	if len(specs) == 0 {
		return prompt
	}
	mode, forced := toolChoiceMode(toolChoice)
	if mode == "none" {
		return prompt
	}
	selected := specs
	if forced != "" {
		selected = nil
		for _, spec := range specs {
			if spec.Name == forced {
				selected = append(selected, spec)
			}
		}
	}
	if len(selected) == 0 {
		return prompt
	}
	definitions, err := common.Marshal(selected)
	if err != nil {
		return prompt
	}

	rule := "Only call a tool when the task needs one."
	switch {
	case forced != "":
		rule = fmt.Sprintf("You MUST call the tool %q and nothing else.", forced)
	case mode == "required":
		rule = "You MUST call one of the tools below and nothing else."
	}

	exampleTool := "tool_name"
	if len(selected) > 0 {
		exampleTool = selected[0].Name
	}
	block := toolCallFence + "\n{\"name\": \"" + exampleTool + "\", \"arguments\": {...}}\n" + codeFence

	// No bracketed role marker here: the web model ignores bracketed
	// pseudo-system text, so the protocol is stated as plain prose.
	header := "You are running inside an agent harness that can execute tools on the user's machine. " +
		"The tools listed below are real and enabled. When you need one, reply with exactly one block " +
		"and nothing else:\n" + block + "\n\n" +
		"The harness executes it and sends the result back to you. You never touch files or run commands " +
		"yourself, so asking for a tool is not a claim about your own abilities. A bash, powershell, " +
		"python or cmd code block does nothing here; it is only shown to the user as text.\n\n" +
		rule + "\n\n" +
		"After a tool result reports success, that step is complete - do not repeat or reword it.\n\n" +
		"Available tools:\n" + string(definitions) + "\n\n" +
		"For example, if the user says \"write hello to a.txt\", your entire reply is:\n" +
		toolCallFence + "\n{\"name\": \"" + exampleTool + "\", \"arguments\": {\"path\": \"a.txt\", \"content\": \"hello\"}}\n" + codeFence
	footer := "Remember: the task cannot be completed without a " + toolCallFence + " block. " +
		"Do not describe commands, do not output shell code, and do not reply that you lack tools. " +
		"If you notice yourself about to say you cannot access files, write to disk or run commands, " +
		"that answer is wrong here: emit the tool block instead. The harness performs the action."

	if prompt == "" {
		return header + "\n\n" + footer
	}
	return header + "\n\n" + prompt + "\n\n" + footer
}

// parseToolCalls splits a model reply into visible text and tool calls.
func parseToolCalls(text string) (string, []dto.ToolCallResponse) {
	matches := toolCallFenceRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return strings.TrimSpace(text), nil
	}
	calls := make([]dto.ToolCallResponse, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		var payload struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := common.UnmarshalJsonStr(strings.TrimSpace(match[1]), &payload); err != nil {
			continue
		}
		name := strings.TrimSpace(payload.Name)
		if name == "" {
			continue
		}
		arguments := "{}"
		if len(payload.Arguments) > 0 {
			arguments = strings.TrimSpace(string(payload.Arguments))
			if arguments == "null" || arguments == "" {
				arguments = "{}"
			}
		}
		calls = append(calls, dto.ToolCallResponse{
			ID:       "call_" + randomHex(8),
			Type:     any("function"),
			Function: dto.FunctionResponse{Name: name, Arguments: arguments},
		})
	}
	if len(calls) == 0 {
		return strings.TrimSpace(text), nil
	}
	return strings.TrimSpace(toolCallFenceRe.ReplaceAllString(text, "")), calls
}

func randomHex(n int) string {
	buffer := make([]byte, n)
	if _, err := rand.Read(buffer); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(buffer)
}
