package model

import "time"

// GeminiWebChannelUsage is this gateway's own record of what a Gemini Web
// channel has served. Gemini Web exposes no credit or quota endpoint: the /app
// page carries no balance, plan or remaining-request field, so the only usage
// figures that exist are the ones this gateway already recorded.
type GeminiWebChannelUsage struct {
	Requests         int64 `json:"requests"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	Quota            int64 `json:"quota"`
}

// GetGeminiWebChannelUsage aggregates the consume logs of one channel since the
// given timestamp.
func GetGeminiWebChannelUsage(channelId int, since int64) (GeminiWebChannelUsage, error) {
	var usage GeminiWebChannelUsage
	err := LOG_DB.Table("logs").
		Select("count(*) as requests, "+
			"coalesce(sum(prompt_tokens), 0) as prompt_tokens, "+
			"coalesce(sum(completion_tokens), 0) as completion_tokens, "+
			"coalesce(sum(quota), 0) as quota").
		Where("channel_id = ? AND type = ? AND created_at >= ?", channelId, LogTypeConsume, since).
		Scan(&usage).Error
	return usage, err
}

// GeminiWebTodayStart returns the local midnight timestamp used as the usage
// window start.
func GeminiWebTodayStart(now time.Time) int64 {
	local := now.Local()
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	return midnight.Unix()
}
