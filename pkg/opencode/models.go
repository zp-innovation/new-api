package opencode

import (
	"context"
	"strings"
)

// DefaultModelIDs is a conservative fallback used before the first successful
// /models fetch. The upstream catalog remains the source of truth.
func DefaultModelIDs() []string {
	return []string{
		"grok-4.7",
		"grok-4.6",
		"gpt-5.6-luna",
		"glm-5.3-flash",
		"glm-5.3",
		"glm-5.2",
		"glm-5.1",
		"kimi-k3",
		"kimi-k2.7-code",
		"kimi-k2.6",
		"longcat-2.0",
		"deepseek-v4-pro",
		"deepseek-v4-flash",
		"mimo-v2.5",
		"mimo-v2.5-pro",
		"minimax-m3",
		"minimax-m2.7",
		"qwen3.8-max",
		"qwen3.8-flash",
		"qwen3.7-max",
		"qwen3.7-plus",
		"qwen3.6-plus",
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-haiku-4-5",
		"hy4-preview",
		"hy3",
		"omen-alpha",
	}
}

// ModelList is the adaptor fallback catalog.
var ModelList = DefaultModelIDs()

// FetchModels reads the upstream OpenAI-style model list.
func (c *Client) FetchModels(ctx context.Context, credential *Credential, baseURL string) ([]string, error) {
	if credential == nil {
		return nil, nil
	}
	if baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/"); baseURL == "" {
		baseURL = credential.BaseURL()
	}
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []string `json:"models"`
	}
	if err := c.getAbsoluteJSON(ctx, baseURL+ModelsPath, credential.AccessToken, &response); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	models := make([]string, 0, len(response.Data)+len(response.Models))
	for _, item := range response.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	for _, id := range response.Models {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	return models, nil
}
