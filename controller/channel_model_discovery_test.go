package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelKeyForModelDiscovery(t *testing.T) {
	credential := "{\n  \"type\": \"antigravity\",\n  \"access_token\": \"ya29.test\",\n  \"project_id\": \"aicode-consumers\"\n}"

	cases := []struct {
		name        string
		channelType int
		rawKey      string
		want        string
	}{
		{
			name:        "plain channel with several keys keeps the first line",
			channelType: constant.ChannelTypeOpenAI,
			rawKey:      "sk-first\nsk-second\n",
			want:        "sk-first",
		},
		{
			name:        "antigravity keeps its whole multi-line credential",
			channelType: constant.ChannelTypeAntigravity,
			rawKey:      credential,
			want:        credential,
		},
		{
			name:        "antigravity credential is trimmed but not truncated",
			channelType: constant.ChannelTypeAntigravity,
			rawKey:      "  \n" + credential + "\n  ",
			want:        credential,
		},
		{
			name:        "codex keeps its whole multi-line value",
			channelType: constant.ChannelTypeCodex,
			rawKey:      "line-one\nline-two",
			want:        "line-one\nline-two",
		},
		{
			name:        "blank key stays blank",
			channelType: constant.ChannelTypeAntigravity,
			rawKey:      "   ",
			want:        "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := channelKeyForModelDiscovery(tc.channelType, tc.rawKey)
			assert.Equal(t, tc.want, got)
			if tc.channelType == constant.ChannelTypeAntigravity && tc.want != "" {
				require.Contains(t, got, "\n", "the multi-line credential must survive untruncated")
			}
		})
	}
}
