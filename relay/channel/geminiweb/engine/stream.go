package engine

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// readBody streams the response, invoking onLine for every line so the caller
// can forward deltas before the upstream finishes.
func readBody(r io.Reader, onLine func(string)) ([]byte, error) {
	var buf bytes.Buffer
	sc := bufio.NewScanner(io.TeeReader(r, &buf))
	// A single frame can exceed 400KB; the default 64KB limit is too small.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		if onLine != nil {
			onLine(sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		return buf.Bytes(), err
	}
	return buf.Bytes(), nil
}

// doRequest performs one StreamGenerate call.
func doRequest(ctx context.Context, endpoint, body string, headers map[string]string, proxyURL string,
	timeout time.Duration, onLine func(string)) (int, []byte, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	applyBrowserHeaders(req)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := Client(proxyURL, timeout).Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	raw, readErr := readBody(resp.Body, onLine)
	if readErr != nil {
		return resp.StatusCode, raw, resp.Header.Values("Set-Cookie"), readErr
	}
	return resp.StatusCode, raw, resp.Header.Values("Set-Cookie"), nil
}

// innerOfLine decodes one wrb.fr frame and returns its inner payload array.
func innerOfLine(line string) []any {
	if !strings.Contains(line, "\"wrb.fr\"") || len(line) < 200 {
		return nil
	}
	var arr []any
	if err := common.UnmarshalJsonStr(line, &arr); err != nil || len(arr) == 0 {
		return nil
	}
	first, ok := arr[0].([]any)
	if !ok || len(first) < 3 {
		return nil
	}
	innerStr, ok := first[2].(string)
	if !ok || len(innerStr) < 50 {
		return nil
	}
	var inner []any
	if err := common.UnmarshalJsonStr(innerStr, &inner); err != nil || len(inner) <= 4 {
		return nil
	}
	return inner
}

// textsInLine returns the candidate reply texts carried by one frame. Every
// frame repeats the full text so far rather than a delta.
func textsInLine(line string) []string {
	inner := innerOfLine(line)
	if inner == nil {
		return nil
	}
	parts, ok := inner[4].([]any)
	if !ok {
		return nil
	}
	var texts []string
	for _, p := range parts {
		pl, ok := p.([]any)
		if !ok || len(pl) < 2 {
			continue
		}
		tl, ok := pl[1].([]any)
		if !ok {
			continue
		}
		for _, t := range tl {
			if s, ok := t.(string); ok && s != "" {
				texts = append(texts, s)
			}
		}
	}
	return texts
}

// reasoningInLine returns the thinking chain of one frame, which sits two
// levels deeper than the reply text.
func reasoningInLine(line string) string {
	inner := innerOfLine(line)
	if inner == nil {
		return ""
	}
	parts, ok := inner[4].([]any)
	if !ok || len(parts) == 0 {
		return ""
	}
	p0, ok := parts[0].([]any)
	if !ok || len(p0) <= 37 {
		return ""
	}
	lvl1, ok := p0[37].([]any)
	if !ok || len(lvl1) == 0 {
		return ""
	}
	lvl2, ok := lvl1[0].([]any)
	if !ok || len(lvl2) == 0 {
		return ""
	}
	s, _ := lvl2[0].(string)
	return s
}

// hasContentFrame reports whether the response carried any content at all.
func hasContentFrame(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		if len(textsInLine(line)) > 0 || reasoningInLine(line) != "" {
			return true
		}
	}
	return false
}

// codeArtifactRe matches fenced code blocks the web client executes and hides.
var codeArtifactRe = regexp.MustCompile("(?s)\x60\x60\x60(?:python|javascript|text)\\?code_(?:reference|stdout)&code_event_index=\\d+\\n.*?\x60\x60\x60\\n?")

// upstreamModelRe matches the display name the server reports for the model it
// actually used, carried at index 42 of a response frame.
var upstreamModelRe = regexp.MustCompile("\\\"[0-9a-f]{16}\\\",null,null,\\\"([^\"\\\\]{1,40})\\\"")

// elicitationsMarker starts the web UI's follow-up suggestion block. The origin
// appends it to the rendered answer; it is interface furniture, not content, and
// nothing legitimate follows it.
const elicitationsMarker = "<ElicitationsGroup"

// sanitizeText strips the artifacts the web client adds around the answer: the
// executed-code fences it hides, and the trailing Elicitations block. It never
// trims: the streaming tracker diffs consecutive frames, and trimming would drop
// the space between two words that land in different frames.
func sanitizeText(text string) string {
	cleaned := codeArtifactRe.ReplaceAllString(text, "")
	if idx := strings.Index(cleaned, elicitationsMarker); idx >= 0 {
		// Everything from the marker on belongs to the UI block.
		return cleaned[:idx]
	}
	// Hold back a partial marker at the tail so it never reaches the client as
	// literal text while the next frame is still arriving.
	for n := min(len(cleaned), len(elicitationsMarker)-1); n > 0; n-- {
		if strings.HasPrefix(elicitationsMarker, cleaned[len(cleaned)-n:]) {
			return cleaned[:len(cleaned)-n]
		}
	}
	return cleaned
}

// cleanText is the finished-text form: artifacts stripped and edges trimmed.
func cleanText(text string) string {
	return strings.TrimSpace(sanitizeText(text))
}

// ExtractUpstreamModel returns the display name the server reports for the
// model it actually used, which may differ from the requested one.
func ExtractUpstreamModel(raw string) string {
	matches := upstreamModelRe.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

// ExtractResponseText returns the final reply text of a complete response.
func ExtractResponseText(raw string) string {
	var texts []string
	for _, line := range strings.Split(raw, "\n") {
		texts = append(texts, textsInLine(line)...)
	}
	for i := len(texts) - 1; i >= 0; i-- {
		if strings.TrimSpace(texts[i]) != "" {
			cleaned := cleanText(texts[i])
			if cleaned == "" {
				cleaned = strings.TrimSpace(texts[i])
			}
			return cleaned
		}
	}
	return ""
}

// ExtractReasoning returns the longest thinking chain in the response.
func ExtractReasoning(raw string) string {
	best := ""
	for _, line := range strings.Split(raw, "\n") {
		if r := reasoningInLine(line); len(r) > len(best) {
			best = r
		}
	}
	return cleanText(best)
}

// deltaTracker converts repeated cumulative frames into incremental deltas.
type deltaTracker struct{ emitted string }

func (d *deltaTracker) push(full string) string {
	cleaned := sanitizeText(full)
	if len(cleaned) <= len(d.emitted) || !strings.HasPrefix(cleaned, d.emitted) {
		return ""
	}
	delta := cleaned[len(d.emitted):]
	d.emitted = cleaned
	return delta
}
