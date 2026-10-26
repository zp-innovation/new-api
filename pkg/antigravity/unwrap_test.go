package antigravity

import (
	"io"
	"strings"
	"testing"
)

func TestUnwrapReaderBuffered(t *testing.T) {
	body := io.NopCloser(strings.NewReader(`{"response":{"candidates":[{"finishReason":"STOP"}]},"modelVersion":"v1"}`))
	reader := NewUnwrapReader(body, false)

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if strings.Contains(string(got), `"response":{`) {
		t.Fatalf("buffered body was not unwrapped: %s", got)
	}
	if !strings.Contains(string(got), `"candidates"`) || !strings.Contains(string(got), `"modelVersion":"v1"`) {
		t.Fatalf("unwrapped body lost content: %s", got)
	}
}

func TestUnwrapReaderStream(t *testing.T) {
	input := strings.Join([]string{
		": keepalive",
		"",
		`data: {"response":{"candidates":[{"finishReason":"STOP"}]},"modelVersion":"v1"}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	reader := NewUnwrapReader(io.NopCloser(strings.NewReader(input)), true)

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	text := string(got)

	if !strings.Contains(text, ": keepalive") {
		t.Errorf("comment line was dropped: %q", text)
	}
	if !strings.Contains(text, "data: [DONE]") {
		t.Errorf("terminator was dropped: %q", text)
	}
	if strings.Contains(text, `"response":{`) {
		t.Errorf("data frame was not unwrapped: %q", text)
	}
	if !strings.Contains(text, `"modelVersion":"v1"`) {
		t.Errorf("modelVersion was lost: %q", text)
	}
	if !strings.HasSuffix(text, "\n") {
		t.Errorf("stream output lost its trailing newline: %q", text)
	}
}

func TestUnwrapReaderStreamHandlesTrailingLineWithoutNewline(t *testing.T) {
	input := `data: {"response":{"candidates":[]},"modelVersion":"v1"}`
	reader := NewUnwrapReader(io.NopCloser(strings.NewReader(input)), true)

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !strings.Contains(string(got), `"candidates"`) {
		t.Fatalf("trailing frame was lost: %q", got)
	}
}

func TestUnwrapReaderStreamKeepsUndecodableFrame(t *testing.T) {
	input := "data: not-json\n"
	reader := NewUnwrapReader(io.NopCloser(strings.NewReader(input)), true)

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != input {
		t.Fatalf("undecodable frame was rewritten: %q", got)
	}
}

func TestUnwrapReaderCloseDelegates(t *testing.T) {
	body := &closeRecorder{Reader: strings.NewReader("{}")}
	reader := NewUnwrapReader(body, false)

	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !body.closed {
		t.Fatal("Close did not reach the underlying body")
	}
}

type closeRecorder struct {
	*strings.Reader
	closed bool
}

func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}

// TestUnwrapReaderRealAntigravityFrames replays frames captured from a live
// gemini-2.5-flash streamGenerateContent call. It pins the two things the live
// smoke run proved: the envelope is removed, and responseId / modelVersion are
// merged into the inner payload.
func TestUnwrapReaderRealAntigravityFrames(t *testing.T) {
	// Verbatim upstream frames (response body of /v1internal:streamGenerateContent?alt=sse).
	input := strings.Join([]string{
		`data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"thought":true,"text":""}]}}],"modelVersion":"gemini-2.5-flash","responseId":"MzvKaqeoMLGBmtkPr5rqqQU","usageMetadata":{"promptTokenCount":9,"totalTokenCount":9}}}`,
		``,
		`data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"antig"}]}}],"modelVersion":"gemini-2.5-flash","responseId":"MzvKaqeoMLGBmtkPr5rqqQU","usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":2,"totalTokenCount":11}}}`,
		``,
		"data: [DONE]",
		``,
	}, "\n")

	reader := NewUnwrapReader(io.NopCloser(strings.NewReader(input)), true)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	text := string(got)

	if strings.Contains(text, `"response":`) {
		t.Fatalf("envelope was not removed: %q", text)
	}
	if !strings.Contains(text, `"modelVersion":"gemini-2.5-flash"`) {
		t.Errorf("modelVersion was not merged into the frame: %q", text)
	}
	if !strings.Contains(text, `"responseId":"MzvKaqeoMLGBmtkPr5rqqQU"`) {
		t.Errorf("responseId was not merged into the frame: %q", text)
	}
	if !strings.Contains(text, `"usageMetadata"`) {
		t.Errorf("usageMetadata was lost: %q", text)
	}
	if !strings.Contains(text, `"text":"antig"`) {
		t.Errorf("delta text was lost: %q", text)
	}
}
