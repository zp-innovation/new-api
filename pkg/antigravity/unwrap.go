package antigravity

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// UnwrapReader adapts an Antigravity response body into the plain Gemini body
// the host handlers parse. For streaming responses it rewrites each SSE data
// frame; for buffered responses it rewrites the whole body on first read.
type UnwrapReader struct {
	body      io.ReadCloser
	streaming bool
	reader    *bufio.Reader
	pending   []byte
	loaded    bool
	drained   bool
}

// NewUnwrapReader wraps body. Set streaming when the upstream response is SSE.
func NewUnwrapReader(body io.ReadCloser, streaming bool) *UnwrapReader {
	reader := &UnwrapReader{body: body, streaming: streaming}
	if streaming {
		reader.reader = bufio.NewReader(body)
	}
	return reader
}

// Read implements io.Reader.
func (r *UnwrapReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if err := r.fill(); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// Close implements io.Closer.
func (r *UnwrapReader) Close() error {
	return r.body.Close()
}

func (r *UnwrapReader) fill() error {
	if r.streaming {
		return r.fillFromStream()
	}
	return r.fillFromBody()
}

func (r *UnwrapReader) fillFromBody() error {
	if r.loaded {
		return io.EOF
	}
	r.loaded = true

	body, err := io.ReadAll(r.body)
	if err != nil {
		return err
	}
	unwrapped, err := UnwrapResponse(body)
	if err != nil {
		return err
	}
	r.pending = unwrapped
	return nil
}

func (r *UnwrapReader) fillFromStream() error {
	for {
		if r.drained {
			return io.EOF
		}

		line, err := r.reader.ReadString('\n')
		if err == nil {
			r.pending = unwrapStreamLine(line)
			if len(r.pending) > 0 {
				return nil
			}
			continue
		}

		r.drained = true
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				return io.EOF
			}
			return err
		}
		// Emit the trailing partial line; the next read reports io.EOF.
		r.pending = unwrapStreamLine(line)
		return nil
	}
}

// unwrapStreamLine rewrites one SSE data frame, leaving every other line
// (comments, blank separators, event names) untouched.
func unwrapStreamLine(line string) []byte {
	payload, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "data:")
	if !ok {
		return []byte(line)
	}
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "[DONE]" {
		return []byte(line)
	}

	unwrapped, err := UnwrapResponse([]byte(payload))
	if err != nil {
		return []byte(line)
	}
	return []byte("data: " + string(unwrapped) + "\n")
}
