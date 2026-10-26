package opencode

import (
	"context"
	"errors"
	"strings"
)

// ErrUsageUnavailable means the credential's mode has no usage endpoint.
var ErrUsageUnavailable = errors.New("opencode: usage is only available for Go subscriptions")

// UsageWindow is one rolling, weekly, or monthly Go allowance window.
type UsageWindow struct {
	Status   string  `json:"status,omitempty"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resets_at,omitempty"`
}

// UsageData is the sanitized Go usage view.
type UsageData struct {
	Rolling UsageWindow `json:"rolling"`
	Weekly  UsageWindow `json:"weekly"`
	Monthly UsageWindow `json:"monthly"`
}

type usageWindowResponse struct {
	Status        string  `json:"status"`
	Percent       float64 `json:"percent"`
	ResetsAt      string  `json:"resetsAt"`
	ResetsAtSnake string  `json:"resets_at"`
}

func (w usageWindowResponse) toUsageWindow() UsageWindow {
	resetsAt := strings.TrimSpace(w.ResetsAt)
	if resetsAt == "" {
		resetsAt = strings.TrimSpace(w.ResetsAtSnake)
	}
	return UsageWindow{
		Status:   w.Status,
		Percent:  w.Percent,
		ResetsAt: resetsAt,
	}
}

type usagePayloadResponse struct {
	Rolling usageWindowResponse `json:"rolling"`
	Weekly  usageWindowResponse `json:"weekly"`
	Monthly usageWindowResponse `json:"monthly"`
}

// FetchUsage reads the OpenCode Go usage endpoint.
func (c *Client) FetchUsage(ctx context.Context, credential *Credential, baseURL string) (*UsageData, error) {
	if credential == nil {
		return nil, errors.New("opencode: credential is nil")
	}
	if NormalizeMode(credential.Mode) != ModeGo {
		return nil, ErrUsageUnavailable
	}
	if baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/"); baseURL == "" {
		baseURL = credential.BaseURL()
	}
	var response struct {
		Usage   *usagePayloadResponse `json:"usage"`
		Data    *usagePayloadResponse `json:"data"`
		Rolling usageWindowResponse   `json:"rolling"`
		Weekly  usageWindowResponse   `json:"weekly"`
		Monthly usageWindowResponse   `json:"monthly"`
	}
	if err := c.getAbsoluteJSON(ctx, baseURL+UsagePath, credential.AccessToken, &response); err != nil {
		return nil, err
	}
	payload := response.Usage
	if payload == nil {
		payload = response.Data
	}
	if payload != nil {
		return &UsageData{
			Rolling: payload.Rolling.toUsageWindow(),
			Weekly:  payload.Weekly.toUsageWindow(),
			Monthly: payload.Monthly.toUsageWindow(),
		}, nil
	}
	return &UsageData{
		Rolling: response.Rolling.toUsageWindow(),
		Weekly:  response.Weekly.toUsageWindow(),
		Monthly: response.Monthly.toUsageWindow(),
	}, nil
}
