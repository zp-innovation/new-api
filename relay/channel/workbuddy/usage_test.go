package workbuddy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A quota cap body must be sniffed without changing what the generic error path
// reads: the handler downstream still has to see the exact upstream bytes.
func TestCaptureCapEventPreservesTheBody(t *testing.T) {
	credential := channelKey("intl", "cli")
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ApiKey: credential, ChannelId: 7},
		OriginModelName: "deepseek-v4.1-flash",
	}
	body := `{"code":6004,"msg":"your usage will reset at 2026-10-09 14:44:59 UTC+8"}`
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	adaptor := &Adaptor{}
	adaptor.captureCapEvent(context.Background(), resp, info)

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(got))
}

// A body larger than the sniff bound must be restored in full, not truncated.
func TestCaptureCapEventRestoresAnOversizedBody(t *testing.T) {
	credential := channelKey("intl", "cli")
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ApiKey: credential, ChannelId: 7},
		OriginModelName: "deepseek-v4.1-flash",
	}
	prefix := `{"code":6004,"msg":"your usage will reset at 2026-10-09 14:44:59 UTC+8","detail":"`
	body := prefix + strings.Repeat("x", maxCapBodyBytes) + `"}`
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	adaptor := &Adaptor{}
	adaptor.captureCapEvent(context.Background(), resp, info)

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.True(t, bytes.Equal([]byte(body), got), "the full body must survive the sniff")
}

// A plain frequency throttle must not be recorded as a spent window.
func TestCaptureCapEventIgnoresAThrottle(t *testing.T) {
	credential := channelKey("intl", "cli")
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ApiKey: credential, ChannelId: 7},
		OriginModelName: "deepseek-v4.1-flash",
	}
	body := `{"code":429,"msg":"usage exceeds frequency limit"}`
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	adaptor := &Adaptor{}
	adaptor.captureCapEvent(context.Background(), resp, info)

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(got))
}
