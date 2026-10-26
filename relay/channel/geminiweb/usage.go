package geminiweb

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/tokenkit"
)

// estimateUsage derives the billable token counts. Gemini Web reports no usage
// of its own, so both sides are counted locally from the exact text that was
// sent and received.
func estimateUsage(model, prompt, completion string) *dto.Usage {
	promptTokens := tokenkit.Count(model, prompt)
	completionTokens := tokenkit.Count(model, completion)
	return &dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
}
