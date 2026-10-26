package antigravity

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrebufferStreamSuppressesEmptyMalformedStream(t *testing.T) {
	input := strings.Join([]string{
		`data: {"usageMetadata":{"promptTokenCount":3,"totalTokenCount":3}}`,
		"",
		`data: {"candidates":[{"finishReason":"MALFORMED_FUNCTION_CALL"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	body := &trackedReadCloser{Reader: strings.NewReader(input)}

	guarded, suppressed, err := prebufferStreamUntilMeaningful(context.Background(), body, maxStreamPrebufferWait)
	require.NoError(t, err)
	assert.True(t, suppressed)
	assert.Nil(t, guarded)
	assert.True(t, body.closed)
}

func TestPrebufferStreamPassesThroughOnceContentArrives(t *testing.T) {
	input := strings.Join([]string{
		`data: {"usageMetadata":{"promptTokenCount":3}}`,
		"",
		`data: {"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`,
		"",
		`data: {"candidates":[{"finishReason":"MALFORMED_FUNCTION_CALL"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	guarded, suppressed, err := prebufferStreamUntilMeaningful(context.Background(), io.NopCloser(strings.NewReader(input)), maxStreamPrebufferWait)
	require.NoError(t, err)
	require.False(t, suppressed)
	require.NotNil(t, guarded)

	got, readErr := io.ReadAll(guarded)
	require.NoError(t, readErr)
	assert.Equal(t, input, string(got))
}

func TestPrebufferStreamPassesEmptyNonMalformedStream(t *testing.T) {
	input := strings.Join([]string{
		`data: {"usageMetadata":{"promptTokenCount":3}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	guarded, suppressed, err := prebufferStreamUntilMeaningful(context.Background(), io.NopCloser(strings.NewReader(input)), maxStreamPrebufferWait)
	require.NoError(t, err)
	require.False(t, suppressed)

	got, readErr := io.ReadAll(guarded)
	require.NoError(t, readErr)
	assert.Equal(t, input, string(got))
}

func TestPrebufferStreamTreatsInvalidJSONAsContent(t *testing.T) {
	input := "data: {not-json}\n\n"

	guarded, suppressed, err := prebufferStreamUntilMeaningful(context.Background(), io.NopCloser(strings.NewReader(input)), maxStreamPrebufferWait)
	require.NoError(t, err)
	require.False(t, suppressed)

	got, readErr := io.ReadAll(guarded)
	require.NoError(t, readErr)
	assert.Equal(t, input, string(got))
}

// A caller that went away must not keep the upstream stream buffered: the guard
// aborts immediately and releases the body.
func TestPrebufferStreamStopsWhenTheCallerDisconnects(t *testing.T) {
	body := newBlockingReadCloser()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	guarded, suppressed, err := prebufferStreamUntilMeaningful(ctx, body, time.Minute)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, guarded)
	assert.False(t, suppressed)
	assert.True(t, body.isClosed())
}

// An upstream that never produces its first meaningful frame must not block the
// relay indefinitely; the injected wait keeps the test fast and deterministic.
func TestPrebufferStreamStopsWaitingForAStalledUpstream(t *testing.T) {
	body := newBlockingReadCloser()
	start := time.Now()

	guarded, suppressed, err := prebufferStreamUntilMeaningful(context.Background(), body, 20*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "produced no content within")
	assert.Nil(t, guarded)
	assert.False(t, suppressed)
	assert.True(t, body.isClosed())
	assert.Less(t, elapsed, time.Second)
}

// blockingReadCloser stalls like an upstream that never sends its first frame
// and releases a pending read when closed, matching how an HTTP response body
// unblocks on Close.
type blockingReadCloser struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{closed: make(chan struct{})}
}

func (b *blockingReadCloser) Read([]byte) (int, error) {
	<-b.closed
	return 0, errors.New("stream closed")
}

func (b *blockingReadCloser) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func (b *blockingReadCloser) isClosed() bool {
	select {
	case <-b.closed:
		return true
	default:
		return false
	}
}

func TestClassifyStreamFrame(t *testing.T) {
	tests := []struct {
		name string
		line string
		want streamFrameKind
	}{
		{name: "comment", line: ": keepalive\n", want: streamFrameIgnore},
		{name: "done", line: "data: [DONE]\n", want: streamFrameIgnore},
		{name: "usage only", line: `data: {"usageMetadata":{"promptTokenCount":1}}` + "\n", want: streamFrameIgnore},
		{name: "text", line: `data: {"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}` + "\n", want: streamFrameMeaningful},
		{name: "function call", line: `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup"}}]}}]}` + "\n", want: streamFrameMeaningful},
		{name: "inline image", line: `data: {"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGk="}}]}}]}` + "\n", want: streamFrameMeaningful},
		{name: "signature only", line: `data: {"candidates":[{"content":{"parts":[{"thoughtSignature":"abc"}]}}]}` + "\n", want: streamFrameIgnore},
		{name: "malformed", line: `data: {"candidates":[{"finishReason":"MALFORMED_FUNCTION_CALL"}]}` + "\n", want: streamFrameMalformed},
		{name: "content beats malformed", line: `data: {"candidates":[{"finishReason":"MALFORMED_FUNCTION_CALL","content":{"parts":[{"text":"partial"}]}}]}` + "\n", want: streamFrameMeaningful},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyStreamFrame([]byte(tc.line)))
		})
	}
}
