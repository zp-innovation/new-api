package engine

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// newUUID returns a random RFC 4122 version 4 identifier.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}

// buildInner assembles the 97-slot payload. Slot values are copied from the
// web client's own requests; the comment on each line is the only documentation
// that exists for most of them.
func buildInner(prompt string, mc ModelConfig) (string, string, error) {
	inner := make([]any, innerSlots)
	inner[0] = []any{prompt, 0, nil, nil, nil, nil, 0}
	inner[1] = []any{"en"}
	inner[2] = []any{"", "", "", nil, nil, nil, nil, nil, nil, ""}
	inner[6] = []any{0}
	inner[7] = 1
	inner[10] = 1
	inner[11] = 0
	// Conversation turn index; every request opens a fresh conversation.
	inner[17] = []any{[]any{0}}
	inner[18] = 0
	inner[27] = 1
	inner[30] = []any{4}
	inner[41] = []any{1}
	inner[53] = 0
	requestUUID := newUUID()
	inner[59] = requestUUID
	inner[61] = []any{}
	inner[68] = 1
	inner[79] = mc.Mode
	think := mc.thinkWire()
	inner[80] = think
	inner[91] = 0
	inner[96] = 0
	if think != thinkingNormal {
		inner[96] = 1
	}

	innerJSON, err := common.Marshal(inner)
	if err != nil {
		return "", "", err
	}
	outerJSON, err := common.Marshal([]any{nil, string(innerJSON)})
	if err != nil {
		return "", "", err
	}
	return string(outerJSON), requestUUID, nil
}

// buildModelHeader assembles x-goog-ext-525001261-jspb. Index 14 mirrors slot
// 79 and index 15 mirrors slot 80; the header is what the server actually
// honours, so both copies must agree.
func buildModelHeader(hexID string, mode, think int, requestUUID string) string {
	if requestUUID == "" {
		return fmt.Sprintf("[1,null,null,null,\"%s\"]", hexID)
	}
	return fmt.Sprintf(
		"[1,null,null,null,\"%s\",null,null,0,[4,5,6,8],null,null,1,null,null,%d,%d,\"%s\"]",
		hexID, mode, think, requestUUID)
}

// buildHeaders returns the request headers for one StreamGenerate call.
func buildHeaders(cookie, sapisid, hexID, modelHeader, sessionHeader string) map[string]string {
	headers := map[string]string{
		"Accept":                    "*/*",
		"Accept-Language":           "en-US,en;q=0.9",
		"Content-Type":              "application/x-www-form-urlencoded;charset=UTF-8",
		"Origin":                    geminiOrigin,
		"Referer":                   geminiAppURL,
		"X-Same-Domain":             "1",
		"X-Goog-AuthUser":           "0",
		"x-goog-ext-73010989-jspb":  "[0]",
		"x-goog-ext-73010990-jspb":  "[0,0,0]",
		"x-goog-ext-525001261-jspb": modelHeader,
		"x-goog-ext-525005358-jspb": sessionHeader,
	}
	if hexID != "" {
		headers["x-goog-ext-525001261-jspb"] = modelHeader
	}
	if cookie != "" {
		headers["Cookie"] = cookie
	}
	if sapisid != "" {
		headers["Authorization"] = SAPISIDHash(sapisid)
	}
	return headers
}

// buildBody encodes the form body, adding the at token when signed in.
func buildBody(outerJSON, at string) string {
	form := url.Values{}
	form.Set("f.req", outerJSON)
	if at != "" {
		form.Set("at", at)
	}
	return form.Encode()
}

// buildEndpoint assembles the StreamGenerate URL for one request.
func buildEndpoint(bl string) string {
	return fmt.Sprintf("%s?bl=%s&hl=en&_reqid=%d&rt=c", streamRPCURL, url.QueryEscape(bl), time.Now().UnixNano()%1000000)
}
