package workbuddy

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// The WorkBuddy edge validates more than the OpenAI protocol does, and every
// violation comes back as a protocol-level code instead of an ordinary 4xx:
//
//   - the conversation must open with a system message and the only roles are
//     system / user / assistant / tool (HTTP 400, code 11-128);
//   - tool_choice is declared as a string, so an object fails to unmarshal
//     (code 11101);
//   - a tool call must be answered by its result and the batch must stay
//     contiguous: an unanswered call, a result separated from its batch or a
//     parallel batch replayed as adjacent assistant messages is rejected
//     (code 11133), and the interleaved shape also trips the abuse filter
//     (code 11-128). Because the history is replayed on every later turn, one
//     bad pair retires the whole conversation;
//   - a DeepSeek model runs a reasoning trace only when reasoning_effort rides
//     along, so thinking.type=enabled on its own changes nothing;
//   - max_completion_tokens is not a field it reads at all.
//
// The reference gateway repairs all of this in build_upstream_body
// (wb_proxy.py:8288-8360) before it relays; this file is the same repair.
const (
	// defaultSystemPrompt opens a conversation that arrives without one, and is
	// the exact sentence the reference prepends (wb_proxy.py:237-238).
	defaultSystemPrompt = "You are a helpful assistant."
	// defaultReasoningEffort is filled in when a DeepSeek request asks for a
	// reasoning trace but leaves the level to us (wb_proxy.py:8350-8355).
	defaultReasoningEffort = "high"
	// identityPromptReplacement removes the exact DeepSeek Harness identity
	// sentence rejected by the China WorkBuddy edge without changing the rest
	// of the caller's system prompt.
	identityPromptReplacement = "You are a helpful AI assistant."
	// harnessMarkerReplacement neutralizes the harness name that the same edge
	// keys on outside the identity sentence. A prompt whose opening sentence was
	// already replaced is still rejected (code 11128) while the runtime-context
	// lines or the "Web GUI" paragraph keep the name, so every remaining
	// occurrence in trusted instructions is replaced too.
	harnessMarkerReplacement = "agent platform"
)

var identityPromptPattern = regexp.MustCompile(`(?i)You are\s+(?:an?\s+|the\s+)?(?:[\w.-]+\s+){0,3}?(?:agent|assistant|model|AI|LLM)?[\s,]*(?:powered by|built on|built by|created by|developed by|made by|trained by)`)

// harnessMarkerPattern matches the harness name wherever else it appears in
// trusted instructions.
var harnessMarkerPattern = regexp.MustCompile(`(?i)deepseek\s+harness`)

// normalizeUpstreamRequest rewrites one converted chat request into the shape
// the upstream accepts. It runs last, so every inbound dialect has already been
// reduced to an OpenAI chat request by the delegate.
func normalizeUpstreamRequest(request *dto.GeneralOpenAIRequest, defaultThinking bool, sanitizeCNIdentity bool) {
	// The OpenAI delegate renames a leading system message to developer for the
	// reasoning models (relay/channel/openai/adaptor.go:439), which is a role
	// this upstream does not know.
	for i := range request.Messages {
		if request.Messages[i].Role == "developer" {
			request.Messages[i].Role = "system"
		}
	}
	if sanitizeCNIdentity {
		sanitizeIdentityPrompts(request.Messages)
	}

	thinking := deepseekThinkingRequested(request, defaultThinking)
	if deepseekThinkingOptedOut(request) {
		// An explicit opt-out has to be enforced, not merely left alone: the
		// upstream treats the presence of reasoning_effort as the switch, so
		// reasoning_effort=none still ran a trace (37 reasoning tokens) and
		// thinking.type=disabled beside an effort ran one too (49). Drop the
		// field and say so in thinking, or the opt-out is not one.
		request.ReasoningEffort = ""
		request.THINKING = json.RawMessage(`{"type":"disabled"}`)
	}
	request.Messages = backfillReasoningContent(request.Messages, request.Model, thinking)

	if len(request.Messages) == 0 || request.Messages[0].Role != "system" {
		request.Messages = append([]dto.Message{{Role: "system", Content: defaultSystemPrompt}}, request.Messages...)
	}

	request.Messages = repairToolCallPairing(request.Messages)

	// The delegate moves max_tokens onto max_completion_tokens for the
	// reasoning models, but this upstream ignores that alias: the same request
	// ran to 141 completion tokens with max_completion_tokens=64. Move the value
	// back so the client's ceiling is the one that applies.
	if request.MaxTokens == nil && request.MaxCompletionTokens != nil && *request.MaxCompletionTokens > 0 {
		request.MaxTokens = request.MaxCompletionTokens
	}
	request.MaxCompletionTokens = nil

	request.ToolChoice = normalizeToolChoice(request.ToolChoice)

	if thinking {
		if len(request.THINKING) == 0 {
			request.THINKING = json.RawMessage(`{"type":"enabled"}`)
		}
		if strings.TrimSpace(request.ReasoningEffort) == "" {
			request.ReasoningEffort = defaultReasoningEffort
		}
	}
}

// sanitizeIdentityPrompts applies the China edge's abuse-filter workaround to
// trusted system instructions only. User, assistant, and tool content must
// remain byte-for-byte intact.
func sanitizeIdentityPrompts(messages []dto.Message) {
	for i := range messages {
		if messages[i].Role != "system" && messages[i].Role != "developer" {
			continue
		}
		messages[i].Content = sanitizeIdentityContent(messages[i].Content)
	}
}

func sanitizeIdentityContent(content any) any {
	switch value := content.(type) {
	case string:
		return sanitizeIdentityText(value)
	case []any:
		for i, item := range value {
			switch block := item.(type) {
			case map[string]any:
				sanitizeIdentityTextBlock(block)
			case dto.MediaContent:
				value[i] = sanitizeIdentityMediaContent(block)
			}
		}
		return value
	case []map[string]any:
		for i := range value {
			sanitizeIdentityTextBlock(value[i])
		}
		return value
	case []dto.MediaContent:
		for i := range value {
			value[i] = sanitizeIdentityMediaContent(value[i])
		}
		return value
	}
	return content
}

func sanitizeIdentityTextBlock(block map[string]any) {
	kind, _ := block["type"].(string)
	if kind != "text" && kind != "input_text" {
		return
	}
	text, ok := block["text"].(string)
	if !ok {
		return
	}
	block["text"] = sanitizeIdentityText(text)
}

func sanitizeIdentityMediaContent(content dto.MediaContent) dto.MediaContent {
	if content.Type != "text" && content.Type != "input_text" {
		return content
	}
	content.Text = sanitizeIdentityText(content.Text)
	return content
}

func sanitizeIdentityText(text string) string {
	text = harnessMarkerPattern.ReplaceAllString(text, harnessMarkerReplacement)

	matches := identityPromptPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	var result strings.Builder
	result.Grow(len(text))
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		if start < last {
			continue
		}
		for end < len(text) && text[end] != '\r' && text[end] != '\n' {
			if text[end] == '.' && (end+1 == len(text) || isIdentityPromptSpace(text[end+1])) {
				end++
				break
			}
			end++
		}
		result.WriteString(text[last:start])
		result.WriteString(identityPromptReplacement)
		last = end
	}
	result.WriteString(text[last:])
	return result.String()
}

func isIdentityPromptSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\r', '\n':
		return true
	}
	return false
}

// deepseekThinkingRequested reports whether a DeepSeek request wants a reasoning
// trace. An explicit choice always wins; a request that never mentions thinking
// falls back to the channel's default, which the reference gateway sets to "on"
// (wb_proxy.py:8295-8302).
func deepseekThinkingRequested(request *dto.GeneralOpenAIRequest, defaultThinking bool) bool {
	if !isDeepseekModel(request.Model) || deepseekThinkingOptedOut(request) {
		return false
	}
	if thinkingType(request) == "enabled" || strings.TrimSpace(request.ReasoningEffort) != "" {
		return true
	}
	return defaultThinking
}

// deepseekThinkingOptedOut reports whether the request asked for no reasoning
// trace at all, as opposed to leaving the choice to the gateway.
func deepseekThinkingOptedOut(request *dto.GeneralOpenAIRequest) bool {
	if !isDeepseekModel(request.Model) {
		return false
	}
	if thinkingType(request) == "disabled" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(request.ReasoningEffort), "none")
}

func isDeepseekModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "deepseek")
}

// thinkingType reads thinking.type; a missing or unreadable block reads as
// unset, which the caller treats as "not opted out".
func thinkingType(request *dto.GeneralOpenAIRequest) string {
	if len(request.THINKING) == 0 {
		return ""
	}
	var thinking struct {
		Type string `json:"type"`
	}
	if err := common.Unmarshal(request.THINKING, &thinking); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(thinking.Type))
}

// backfillReasoningContent makes every assistant message of a DeepSeek
// conversation carry a reasoning trace. While thinking is on the upstream
// requires the field on every assistant turn, and it validates the length, so
// an empty placeholder is not enough on its own: reasoning mirrors a non-empty
// value alongside reasoning_content (wb_proxy.py:7857-7885).
func backfillReasoningContent(messages []dto.Message, model string, thinking bool) []dto.Message {
	if !isDeepseekModel(model) {
		return messages
	}
	if !thinking && !hasReasoningTrace(messages) {
		return messages
	}
	for i := range messages {
		if messages[i].Role != "assistant" {
			continue
		}
		content := ""
		if messages[i].ReasoningContent != nil {
			content = *messages[i].ReasoningContent
		} else if messages[i].Reasoning != nil {
			content = *messages[i].Reasoning
		}
		reasoningContent := content
		messages[i].ReasoningContent = &reasoningContent
		if messages[i].Reasoning == nil || *messages[i].Reasoning == "" {
			placeholder := content
			if placeholder == "" {
				placeholder = " "
			}
			messages[i].Reasoning = &placeholder
		}
	}
	return messages
}

// hasReasoningTrace reports whether any message already carries a reasoning
// trace. A history that has one needs the backfill even when this request does
// not ask for thinking, or the earlier turns stop matching (wb_proxy.py:7864).
func hasReasoningTrace(messages []dto.Message) bool {
	for i := range messages {
		if messages[i].ReasoningContent != nil {
			return true
		}
		if messages[i].Reasoning != nil && *messages[i].Reasoning != "" {
			return true
		}
	}
	return false
}

// repairToolCallPairing restores the tool-call shape the upstream accepts.
// Order matters: merging the split declarations first lets the repack see one
// complete batch.
func repairToolCallPairing(messages []dto.Message) []dto.Message {
	return cleanupOrphanToolCalls(repackToolResults(mergeAdjacentToolCalls(messages)))
}

// mergeAdjacentToolCalls folds a parallel batch that a client replayed as
// several adjacent assistant messages back into one (wb_proxy.py:7616-7671).
func mergeAdjacentToolCalls(messages []dto.Message) []dto.Message {
	out := make([]dto.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == "assistant" && len(out) > 0 {
			previous := &out[len(out)-1]
			if previous.Role == "assistant" && len(previous.ToolCalls) > 0 {
				// Form 1: this message declares the rest of the batch.
				if len(message.ToolCalls) > 0 && isEmptyContent(message.Content) {
					previous.ToolCalls = joinToolCalls(previous.ToolCalls, message.ToolCalls)
					previous.ReasoningContent = joinReasoning(previous.ReasoningContent, message.ReasoningContent)
					continue
				}
				// Form 2: the same batch replayed in the other order, with the
				// answer text on its own message.
				if len(message.ToolCalls) == 0 && isEmptyContent(previous.Content) {
					if text, ok := message.Content.(string); ok && text != "" {
						previous.Content = text
						previous.ReasoningContent = joinReasoning(previous.ReasoningContent, message.ReasoningContent)
						continue
					}
				}
			}
		}
		out = append(out, message)
	}
	return out
}

// repackToolResults keeps a tool-call batch and its results adjacent: an
// interleaved message lands between two results, the pairing reads as broken
// and the upstream rejects the request. Only the order changes
// (wb_proxy.py:7673-7740).
func repackToolResults(messages []dto.Message) []dto.Message {
	out := make([]dto.Message, 0, len(messages))
	for i := 0; i < len(messages); {
		message := messages[i]
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			out = append(out, message)
			i++
			continue
		}
		wanted := toolCallIDs(message.ToolCalls)
		out = append(out, message)
		i++
		results := make([]dto.Message, 0, len(wanted))
		between := make([]dto.Message, 0, 1)
		for i < len(messages) {
			next := messages[i]
			if next.Role == "tool" {
				if !wanted[next.ToolCallId] {
					break
				}
				results = append(results, next)
				i++
				continue
			}
			if len(results) == 0 {
				break
			}
			// A following batch belongs to the outer loop, or its own results
			// never get repacked.
			if next.Role == "assistant" && len(next.ToolCalls) > 0 {
				break
			}
			between = append(between, next)
			i++
		}
		out = append(out, results...)
		out = append(out, between...)
	}
	return out
}

// cleanupOrphanToolCalls drops calls that never got a result and results that
// have no call. A failed tool call leaves an assistant entry the client can
// never answer, and the upstream rejects every later turn that replays it
// (wb_proxy.py:7742-7810).
func cleanupOrphanToolCalls(messages []dto.Message) []dto.Message {
	callIDs := map[string]bool{}
	resultIDs := map[string]bool{}
	for _, message := range messages {
		switch message.Role {
		case "tool":
			if message.ToolCallId != "" {
				resultIDs[message.ToolCallId] = true
			}
		case "assistant":
			for id := range toolCallIDs(message.ToolCalls) {
				callIDs[id] = true
			}
		}
	}
	if len(callIDs) == 0 && len(resultIDs) == 0 {
		return messages
	}
	paired := map[string]bool{}
	for id := range callIDs {
		if resultIDs[id] {
			paired[id] = true
		}
	}
	out := make([]dto.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == "tool" {
			if paired[message.ToolCallId] {
				out = append(out, message)
			}
			continue
		}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			message.ToolCalls = keepToolCalls(message.ToolCalls, paired)
		}
		out = append(out, message)
	}
	return out
}

// toolCallIDs collects the ids a tool_calls array declares.
func toolCallIDs(raw json.RawMessage) map[string]bool {
	ids := map[string]bool{}
	for _, call := range decodeToolCalls(raw) {
		var identified struct {
			ID string `json:"id"`
		}
		if err := common.Unmarshal(call, &identified); err != nil {
			continue
		}
		if identified.ID != "" {
			ids[identified.ID] = true
		}
	}
	return ids
}

// keepToolCalls returns the calls whose ids are paired with a result, or nil
// when none of them are, so the field is dropped rather than sent empty.
func keepToolCalls(raw json.RawMessage, paired map[string]bool) json.RawMessage {
	kept := make([]json.RawMessage, 0, len(paired))
	for _, call := range decodeToolCalls(raw) {
		var identified struct {
			ID string `json:"id"`
		}
		if err := common.Unmarshal(call, &identified); err != nil {
			continue
		}
		if identified.ID != "" && paired[identified.ID] {
			kept = append(kept, call)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	encoded, err := common.Marshal(kept)
	if err != nil {
		return raw
	}
	return encoded
}

// joinToolCalls concatenates two tool_calls arrays, keeping both intact.
func joinToolCalls(first, second json.RawMessage) json.RawMessage {
	left := decodeToolCalls(first)
	right := decodeToolCalls(second)
	if len(right) == 0 {
		return first
	}
	if len(left) == 0 {
		return second
	}
	merged, err := common.Marshal(append(left, right...))
	if err != nil {
		return first
	}
	return merged
}

// decodeToolCalls reads a tool_calls array as opaque members: the repair only
// needs the ids, and everything else must reach the upstream untouched.
func decodeToolCalls(raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var calls []json.RawMessage
	if err := common.Unmarshal(raw, &calls); err != nil {
		return nil
	}
	return calls
}

// joinReasoning folds a second reasoning trace into the first, newline-joined.
func joinReasoning(first, second *string) *string {
	if second == nil || *second == "" {
		return first
	}
	if first == nil || *first == "" {
		return second
	}
	joined := *first + "\n" + *second
	return &joined
}

// isEmptyContent reports whether an assistant message carries no content at
// all. Clients disagree on which empty shape they emit, and a missed one was
// the original reason a split batch stopped merging.
func isEmptyContent(content any) bool {
	switch value := content.(type) {
	case nil:
		return true
	case string:
		return value == ""
	case []any:
		return len(value) == 0
	}
	return false
}

// normalizeToolChoice reduces tool_choice to the string form the upstream
// declares: an object fails to unmarshal with code 11101. The declarations in
// tools are deliberately kept even for "none" -- dropping them makes an agent
// emit its call as unparseable text instead (wb_proxy.py:7899-7938).
func normalizeToolChoice(choice any) any {
	switch value := choice.(type) {
	case nil:
		return nil
	case string:
		if strings.EqualFold(strings.TrimSpace(value), "none") {
			return "none"
		}
		return value
	case map[string]any:
		kind, _ := value["type"].(string)
		switch strings.ToLower(strings.TrimSpace(kind)) {
		case "none":
			return "none"
		case "auto":
			return "auto"
		case "required":
			return "required"
		case "function":
			if name := chosenFunctionName(value); name != "" {
				return name
			}
			return "auto"
		}
		return nil
	}
	return nil
}

// chosenFunctionName reads the function name out of the object tool_choice
// shape, which names it either under function or at the top level.
func chosenFunctionName(choice map[string]any) string {
	if function, ok := choice["function"].(map[string]any); ok {
		if name, ok := function["name"].(string); ok && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	if name, ok := choice["name"].(string); ok {
		return strings.TrimSpace(name)
	}
	return ""
}
