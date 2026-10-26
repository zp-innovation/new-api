package antigravity

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

// maxErrorBodyBytes bounds how much of a failing upstream response is read
// before it is rewritten for the client. The Cloud Code Assist error envelope
// is a short JSON object, so this only guards against a misbehaving edge.
const maxErrorBodyBytes = 64 << 10

// maxErrorTextRunes bounds the upstream message kept in the client-facing
// error. Anything longer is truncated before the sanitizing pass.
const maxErrorTextRunes = 500

// maxErrorBodyDrainBytes bounds what Close discards from the upstream body. A
// larger remainder is abandoned: the connection only returns to the pool when
// the whole body was consumed.
const maxErrorBodyDrainBytes = 1 << 20

var (
	projectRefPattern = regexp.MustCompile(`(?i)\bprojects/[a-z0-9][a-z0-9._:-]*`)
	emailPattern      = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	consumerPattern   = regexp.MustCompile(`(?i)\b(consumer|project(?:[ _-]?(?:id|number))?)(\s*[:=]?\s*['"]?)[0-9]{6,}`)
	bearerPattern     = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-]+`)
)

// upstreamErrorEnvelope carries the error object of a Cloud Code Assist
// failure. Every field stays raw: the rewrite re-encodes the classification
// fields (type, param, details, ...) the upstream reported instead of
// projecting the envelope onto three known keys.
type upstreamErrorEnvelope struct {
	Error map[string]json.RawMessage `json:"error"`
}

// newSanitizedErrorReader consumes a failing upstream response body and
// returns a reader over the Gemini-style error object rewritten for the client.
// The upstream's own classification fields survive; its message is scrubbed of
// pool identity and truncated. Closing the returned reader drains and closes
// the upstream body, so an only partly read error response does not cost the
// connection.
func newSanitizedErrorReader(body io.ReadCloser, statusCode int) io.ReadCloser {
	if body == nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxErrorBodyBytes))
	if err != nil && len(raw) == 0 {
		raw = nil
	}
	return &sanitizedErrorBody{
		Reader:   bytes.NewReader(buildClientErrorBody(statusCode, raw)),
		upstream: body,
	}
}

// sanitizedErrorBody serves the rewritten error and owns the upstream body it
// was built from. The remainder of that body is discarded, never forwarded: it
// is the unsanitized upstream text, which is exactly what the rewrite exists to
// keep away from the caller.
type sanitizedErrorBody struct {
	io.Reader
	upstream io.ReadCloser
}

func (b *sanitizedErrorBody) Close() error {
	_, _ = io.CopyN(io.Discard, b.upstream, maxErrorBodyDrainBytes)
	return b.upstream.Close()
}

// buildClientErrorBody renders the error object forwarded to the client. It is
// a pure function so the scrubbing rules can be tested without an HTTP round
// trip.
//
// Only the message text is rewritten. The upstream's classification fields
// (type, param, details, ...) are re-encoded as they were reported, because the
// shared error handler and the client both use them to tell a retryable
// capacity failure from a permanent one. Their strings still go through the
// scrubber: those fields are exactly where the GCP project, the service-account
// email and the consumer number appear.
func buildClientErrorBody(statusCode int, body []byte) []byte {
	errorObject := map[string]any{}
	code := any(statusCode)
	status := ""
	message := ""

	var envelope upstreamErrorEnvelope
	if len(body) > 0 && common.Unmarshal(body, &envelope) == nil {
		for key, raw := range envelope.Error {
			switch key {
			case "code":
				var number int
				if err := common.Unmarshal(raw, &number); err == nil && number != 0 {
					code = number
					continue
				}
				// Cloud Code Assist reports a numeric code, but a non-numeric one
				// is an upstream classification token worth forwarding as-is.
				var token any
				if err := common.Unmarshal(raw, &token); err == nil {
					code = sanitizeErrorValue(token)
				}
			case "status":
				var text string
				if err := common.Unmarshal(raw, &text); err == nil {
					status = strings.TrimSpace(text)
				}
			case "message":
				var text string
				if err := common.Unmarshal(raw, &text); err == nil {
					message = strings.TrimSpace(text)
				}
			default:
				var value any
				if err := common.Unmarshal(raw, &value); err == nil {
					errorObject[key] = sanitizeErrorValue(value)
				}
			}
		}
	}
	if message == "" {
		// A body that failed to parse as JSON may be a JSON fragment; only
		// non-JSON text is worth forwarding as a message.
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) > 0 && trimmed[0] != '{' && trimmed[0] != '[' {
			message = string(trimmed)
		}
	}
	if message == "" {
		message = http.StatusText(statusCode)
	}
	if message == "" {
		message = "Upstream request failed"
	}
	if status == "" {
		status = geminiStatusFromHTTP(statusCode)
	}

	errorObject["code"] = code
	errorObject["message"] = truncateErrorText(sanitizeErrorText(message))
	errorObject["status"] = status

	encoded, err := common.Marshal(map[string]any{"error": errorObject})
	if err != nil {
		return []byte(`{"error":{"code":500,"message":"Upstream request failed","status":"INTERNAL"}}`)
	}
	return encoded
}

// sanitizeErrorValue scrubs every string inside a preserved error field so that
// forwarding the upstream's structure cannot forward the pool identity with it.
func sanitizeErrorValue(value any) any {
	switch typed := value.(type) {
	case string:
		return sanitizeErrorText(typed)
	case []any:
		for index := range typed {
			typed[index] = sanitizeErrorValue(typed[index])
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			typed[key] = sanitizeErrorValue(item)
		}
		return typed
	default:
		return value
	}
}

// sanitizeErrorText strips GCP project references, service-account emails,
// consumer/project numbers and bearer tokens from an upstream message.
func sanitizeErrorText(message string) string {
	if message == "" {
		return message
	}
	message = bearerPattern.ReplaceAllString(message, "Bearer ***")
	message = projectRefPattern.ReplaceAllString(message, "projects/***")
	message = emailPattern.ReplaceAllString(message, "***")
	message = consumerPattern.ReplaceAllString(message, "$1$2***")
	return message
}

// truncateErrorText keeps a short single-line excerpt of a non-JSON error
// body so the message stays useful without carrying an unbounded payload.
func truncateErrorText(raw string) string {
	raw = strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(raw) <= maxErrorTextRunes {
		return raw
	}
	runes := []rune(raw)
	return string(runes[:maxErrorTextRunes]) + "..."
}

// geminiStatusFromHTTP maps an HTTP status onto the Google RPC status name the
// Gemini error shape uses when the upstream did not provide one.
func geminiStatusFromHTTP(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusServiceUnavailable:
		return "UNAVAILABLE"
	case http.StatusGatewayTimeout:
		return "DEADLINE_EXCEEDED"
	default:
		if statusCode >= 500 {
			return "INTERNAL"
		}
		return "UNKNOWN"
	}
}
