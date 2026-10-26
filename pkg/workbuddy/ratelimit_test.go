package workbuddy

import (
	"testing"
	"time"
)

func formatTestTimestamp(unix int64) string {
	return time.Unix(unix, 0).In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
}

func TestParseRateLimitResetUnderstandsBothRealms(t *testing.T) {
	const intl = `{"code":6004,"msg":"usage exceeds limit, your usage will reset at 2026-09-19 18:29:03 UTC+8"}`
	const cn = `{"code":6004,"msg":"额度已达上限，将在 2026-10-09 14:44:59 UTC+8 重置"}`
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "international", message: intl, want: "2026-09-19 18:29:03"},
		{name: "china", message: cn, want: "2026-10-09 14:44:59"},
		{name: "throttle without a clock", message: `{"code":429,"msg":"too many requests"}`, want: ""},
		{name: "empty", message: "", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ParseRateLimitReset(test.message)
			if test.want == "" {
				if ok {
					t.Fatalf("ParseRateLimitReset(%q) = %d, want no reset", test.message, got)
				}
				return
			}
			if !ok {
				t.Fatalf("ParseRateLimitReset(%q) did not find a reset", test.message)
			}
			if formatted := formatTestTimestamp(got); formatted != test.want {
				t.Fatalf("reset = %s, want %s", formatted, test.want)
			}
		})
	}
}

func TestMessageHasRateLimitCode(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{message: `{"code":6004,"msg":"x"}`, want: true},
		{message: `{"code": 6004, "msg": "x"}`, want: true},
		{message: `{"code":429,"msg":"too many requests"}`, want: false},
		{message: `{"code":60040}`, want: false},
	}
	for _, test := range tests {
		if got := MessageHasRateLimitCode(test.message); got != test.want {
			t.Fatalf("MessageHasRateLimitCode(%q) = %v, want %v", test.message, got, test.want)
		}
	}
}

func TestSeededTokenBudget(t *testing.T) {
	tests := []struct {
		model string
		want  int64
		ok    bool
	}{
		{model: "deepseek-v4.1-flash", want: 200_000_000, ok: true},
		{model: "deepseek-v4.1-flash-sg", want: 200_000_000, ok: true},
		{model: "hy4-preview-f", want: 20_000_000, ok: true},
		{model: "gpt-6-astra", want: 0, ok: false},
	}
	for _, test := range tests {
		got, ok := SeededTokenBudget(test.model)
		if ok != test.ok || got != test.want {
			t.Fatalf("SeededTokenBudget(%q) = (%d, %v), want (%d, %v)", test.model, got, ok, test.want, test.ok)
		}
	}
}
