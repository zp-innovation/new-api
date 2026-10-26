package antigravity

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// maxStreamPrebufferBytes bounds how many leading frames are held while
// looking for the first meaningful payload. A stream that only carries
// metadata far past this watermark is treated as meaningful so the guard can
// never grow without limit.
const maxStreamPrebufferBytes = 256 << 10

// maxStreamPrebufferWait bounds how long the leading frames may be held before
// the first meaningful payload arrives. Relay requests run on an HTTP client
// whose Timeout is unlimited unless the operator configures RelayTimeout, so
// without this bound a stalled upstream that never sends a content part would
// block the caller forever. 30s matches the antigravity client's own default
// HTTP timeout and is far longer than a working stream needs for its first
// content part, so only genuinely stalled streams are abandoned.
const maxStreamPrebufferWait = 30 * time.Second

// streamFrameKind classifies one SSE frame of a Gemini stream.
type streamFrameKind int

const (
	streamFrameIgnore streamFrameKind = iota
	streamFrameMeaningful
	streamFrameMalformed
)

// prebufferedScan is the outcome of probing the leading frames of one stream.
// Only the probe goroutine produces it; the body is closed by the caller.
type prebufferedScan struct {
	body       io.ReadCloser
	suppressed bool
}

// prebufferStreamUntilMeaningful holds the leading frames of an upstream SSE
// stream until it can tell whether the model produced anything. It returns the
// stream unchanged (prefixed by the held frames) as soon as a frame carries
// real content, and reports true when the stream ended with
// MALFORMED_FUNCTION_CALL and no content at all. That empty case is safe to
// retry because nothing was handed to the client handler yet.
//
// The probe runs in its own goroutine because a blocked upstream Read cannot be
// interrupted otherwise. ctx and wait bound how long the caller waits for a
// first meaningful frame; when either fires the body is closed and the probe is
// abandoned, so a stalled upstream can never hold the request open. The body is
// closed here, never by the probe, which keeps Close single-owner.
func prebufferStreamUntilMeaningful(ctx context.Context, body io.ReadCloser, wait time.Duration) (io.ReadCloser, bool, error) {
	if body == nil {
		return nil, false, nil
	}

	results := make(chan prebufferedScan, 1)
	go func() {
		results <- scanStreamUntilMeaningful(body)
	}()

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case scan := <-results:
		if scan.suppressed {
			// The empty malformed stream is never handed to a handler, so its
			// body is released here.
			_ = body.Close()
			return nil, true, nil
		}
		return scan.body, false, nil
	case <-ctx.Done():
		// Closing the body unblocks the probe; its buffered result is dropped.
		_ = body.Close()
		return nil, false, ctx.Err()
	case <-timer.C:
		_ = body.Close()
		return nil, false, fmt.Errorf("antigravity: upstream stream produced no content within %s", wait)
	}
}

// scanStreamUntilMeaningful reads leading frames until the stream becomes
// meaningful or ends. It never closes the body: the caller owns the lifecycle so
// an abandoned probe cannot double-close the stream.
func scanStreamUntilMeaningful(body io.ReadCloser) prebufferedScan {
	reader := bufio.NewReader(body)
	var pending bytes.Buffer
	malformed := false

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			pending.Write(line)
			switch classifyStreamFrame(line) {
			case streamFrameMeaningful:
				return prebufferedScan{body: prebufferedStream(reader, body, pending.Bytes())}
			case streamFrameMalformed:
				malformed = true
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && malformed {
				return prebufferedScan{suppressed: true}
			}
			return prebufferedScan{body: prebufferedStream(reader, body, pending.Bytes())}
		}
		if pending.Len() > maxStreamPrebufferBytes {
			return prebufferedScan{body: prebufferedStream(reader, body, pending.Bytes())}
		}
	}
}

// prebufferedStream serves the held prefix before the rest of the stream.
func prebufferedStream(reader io.Reader, body io.ReadCloser, held []byte) io.ReadCloser {
	return &prebufferedStreamBody{reader: io.MultiReader(bytes.NewReader(held), reader), closer: body}
}

// classifyStreamFrame inspects one SSE line. Non-data lines and empty frames
// are ignored; a frame with unusable JSON counts as meaningful so the normal
// handler still reports the parse error.
func classifyStreamFrame(line []byte) streamFrameKind {
	trimmed := bytes.TrimSpace(line)
	payload, ok := bytes.CutPrefix(trimmed, []byte("data:"))
	if !ok {
		return streamFrameIgnore
	}
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return streamFrameIgnore
	}
	if !gjson.ValidBytes(payload) {
		return streamFrameMeaningful
	}

	kind := streamFrameIgnore
	for _, candidate := range gjson.GetBytes(payload, "candidates").Array() {
		if candidate.Get("finishReason").String() == "MALFORMED_FUNCTION_CALL" {
			kind = streamFrameMalformed
		}
		for _, part := range candidate.Get("content.parts").Array() {
			if geminiPartHasContent(part) {
				return streamFrameMeaningful
			}
		}
	}
	return kind
}

// geminiPartHasContent reports whether a Gemini part carries content the user
// would actually receive. thoughtSignature-only parts do not count, matching
// the upstream client's empty-stream check.
func geminiPartHasContent(part gjson.Result) bool {
	if strings.TrimSpace(part.Get("text").String()) != "" {
		return true
	}
	for _, key := range []string{"functionCall", "inlineData", "executableCode", "codeExecutionResult"} {
		if part.Get(key).Exists() {
			return true
		}
	}
	return false
}

// prebufferedStreamBody serves the held prefix before the rest of the upstream
// stream while keeping Close wired to the original body.
type prebufferedStreamBody struct {
	reader io.Reader
	closer io.Closer
}

func (b *prebufferedStreamBody) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *prebufferedStreamBody) Close() error {
	return b.closer.Close()
}
