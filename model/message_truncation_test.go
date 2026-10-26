package model

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Both helpers feed varchar columns, so a byte cut producing a half rune would
// be rejected (or corrupted) by a utf8mb4 database.
func TestStoredMessageTruncationCountsRunes(t *testing.T) {
	long := strings.Repeat("渠道错误", 100)

	truncated := truncateMessage(long, 5)
	require.True(t, utf8.ValidString(truncated))
	assert.Equal(t, []rune("渠道错误渠"), []rune(truncated))

	sanitized := sanitizeWorkBuddyMessage(long)
	require.True(t, utf8.ValidString(sanitized))
	assert.LessOrEqual(t, len([]rune(sanitized)), workBuddyMessageLimit)
	assert.Equal(t, workBuddyMessageLimit, len([]rune(sanitized)))

	short := "  upstream said no  "
	assert.Equal(t, "upstream said no", truncateMessage(short, 240))
	assert.Equal(t, "upstream said no", sanitizeWorkBuddyMessage(short))
}
