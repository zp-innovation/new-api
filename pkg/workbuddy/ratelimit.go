package workbuddy

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// RateLimitCode is the upstream envelope code that marks a spent quota window.
// A plain 429 ("usage exceeds frequency limit") is a frequency throttle and
// carries no reset clock, so it must not be mistaken for a spent window.
const RateLimitCode = 6004

// The reset wall clock rides inside the rejection text. The international edge
// answers in English and the China edge in Chinese, both against UTC+8:
//
//	... your usage will reset at 2026-09-19 18:29:03 UTC+8 ...
//	... 将在 2026-10-09 14:44:59 UTC+8 重置 ...
var rateLimitResetPattern = regexp.MustCompile(
	`(?i)(?:usage will reset at|将在)\s*(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2})`)

// ParseRateLimitReset extracts the reset wall clock from an upstream rejection
// message. The upstream states it in UTC+8 regardless of the caller's zone, so
// the value is interpreted there and returned as a Unix timestamp.
func ParseRateLimitReset(message string) (int64, bool) {
	match := rateLimitResetPattern.FindStringSubmatch(message)
	if len(match) < 2 {
		return 0, false
	}
	text := strings.ReplaceAll(match[1], "T", " ")
	zone := time.FixedZone("UTC+8", 8*60*60)
	at, err := time.ParseInLocation("2006-01-02 15:04:05", text, zone)
	if err != nil {
		return 0, false
	}
	return at.Unix(), true
}

// MessageHasRateLimitCode reports whether an upstream body carries the 6004
// envelope code. The code is read as a JSON number, so a value like 60040 cannot
// be mistaken for a cap. Bodies that are not JSON fall back to a bounded
// substring check, since the upstream has always wrapped the code so far.
func MessageHasRateLimitCode(message string) bool {
	if !strings.Contains(message, "6004") {
		return false
	}
	var decoded struct {
		Code json.Number `json:"code"`
	}
	if err := common.Unmarshal([]byte(message), &decoded); err == nil && decoded.Code != "" {
		value, err := decoded.Code.Int64()
		return err == nil && value == RateLimitCode
	}
	return strings.Contains(message, `"code":6004`) ||
		strings.Contains(message, `"code": 6004`)
}
