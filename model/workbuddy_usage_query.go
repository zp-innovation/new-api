package model

// WorkBuddyUsageFilter narrows the consume-log aggregation. A zero StartUnix
// means "no lower bound"; a zero EndUnix means "now".
type WorkBuddyUsageFilter struct {
	ChannelIds []int
	Models     []string
	StartUnix  int64
	EndUnix    int64
}

// WorkBuddyChannelModelUsage is one channel-model total folded out of the
// consume logs. The account uid is not on the log row, so the caller maps the
// channel back to its credentials.
type WorkBuddyChannelModelUsage struct {
	ChannelId        int    `json:"channel_id"`
	Model            string `json:"model"`
	Requests         int64  `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	LastUsedAt       int64  `json:"last_used_at"`
}

// SumWorkBuddyConsumeLogs folds consume logs into per-(channel, model) totals.
// It reads the log database through the shared LOG_DB handle, so the same code
// serves SQLite, MySQL, PostgreSQL and ClickHouse. Only successful consume
// rows count: an error row carries no usage.
func SumWorkBuddyConsumeLogs(filter WorkBuddyUsageFilter) ([]WorkBuddyChannelModelUsage, error) {
	rows := make([]WorkBuddyChannelModelUsage, 0, len(filter.ChannelIds))
	if len(filter.ChannelIds) == 0 || LOG_DB == nil {
		// The caller is the best-effort cap recorder, so a missing log handle
		// must degrade to "no sample" instead of panicking on the relay path.
		return rows, nil
	}
	query := LOG_DB.Table("logs").
		Select("channel_id, model_name AS model, COUNT(*) AS requests, "+
			"COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, "+
			"COALESCE(SUM(completion_tokens), 0) AS completion_tokens, "+
			"COALESCE(SUM(prompt_tokens) + SUM(completion_tokens), 0) AS total_tokens, "+
			"COALESCE(MAX(created_at), 0) AS last_used_at").
		Where("channel_id IN ?", filter.ChannelIds).
		Where("type = ?", LogTypeConsume)
	if len(filter.Models) > 0 {
		query = query.Where("model_name IN ?", filter.Models)
	}
	if filter.StartUnix > 0 {
		query = query.Where("created_at >= ?", filter.StartUnix)
	}
	if filter.EndUnix > 0 {
		query = query.Where("created_at <= ?", filter.EndUnix)
	}
	err := query.Group("channel_id, model_name").Order("channel_id ASC, model_name ASC").Scan(&rows).Error
	return rows, err
}
