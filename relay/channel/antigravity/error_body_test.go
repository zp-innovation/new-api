package antigravity

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildClientErrorBodyScrubsPoolIdentity(t *testing.T) {
	upstream := []byte(`{"error":{"code":403,` +
		`"message":"Permission denied on resource project projects/123456789 for consumer: projects/123456789; ` +
		`caller pool-sa@my-gcp-proj.iam.gserviceaccount.com; Bearer ya29.abcdef",` +
		`"status":"PERMISSION_DENIED",` +
		`"type":"PERMISSION_DENIED","param":"projects/123456789",` +
		`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"CONSUMER_INVALID",` +
		`"metadata":{"consumer":"projects/123456789"}}]}}`)

	out := string(buildClientErrorBody(http.StatusForbidden, upstream))
	for _, forbidden := range []string{"123456789", "pool-sa@", "gserviceaccount.com", "ya29.abcdef"} {
		assert.NotContains(t, out, forbidden)
	}

	var parsed struct {
		Error struct {
			Code    int              `json:"code"`
			Message string           `json:"message"`
			Status  string           `json:"status"`
			Type    string           `json:"type"`
			Param   string           `json:"param"`
			Details []map[string]any `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal([]byte(out), &parsed))
	assert.Equal(t, http.StatusForbidden, parsed.Error.Code)
	assert.Equal(t, "PERMISSION_DENIED", parsed.Error.Status)
	assert.Contains(t, parsed.Error.Message, "Permission denied")
	assert.Contains(t, parsed.Error.Message, "projects/***")
	// The upstream classification is the client's and the retry layer's only way
	// to tell failures apart, so it survives the scrubbing.
	assert.Equal(t, "PERMISSION_DENIED", parsed.Error.Type)
	assert.Equal(t, "projects/***", parsed.Error.Param)
	require.Len(t, parsed.Error.Details, 1)
	assert.Equal(t, "type.googleapis.com/google.rpc.ErrorInfo", parsed.Error.Details[0]["@type"])
	assert.Equal(t, "CONSUMER_INVALID", parsed.Error.Details[0]["reason"])
	assert.Equal(t, map[string]any{"consumer": "projects/***"}, parsed.Error.Details[0]["metadata"])
}

func TestBuildClientErrorBodyHandlesNonJSON(t *testing.T) {
	out := string(buildClientErrorBody(http.StatusTooManyRequests, []byte("quota exceeded for consumer 987654321 sa@x.iam.gserviceaccount.com")))
	assert.NotContains(t, out, "987654321")
	assert.NotContains(t, out, "gserviceaccount")
	assert.Contains(t, out, `"status":"RESOURCE_EXHAUSTED"`)
	assert.Contains(t, out, `"code":429`)
}

func TestBuildClientErrorBodyFallsBackToStatusText(t *testing.T) {
	out := string(buildClientErrorBody(http.StatusBadGateway, nil))
	assert.Contains(t, out, `"status":"INTERNAL"`)
	assert.Contains(t, out, `"message":"Bad Gateway"`)
}

func TestNewSanitizedErrorReaderDrainsTheUpstreamBody(t *testing.T) {
	source := strings.NewReader(`{"error":{"code":403,"message":"project projects/1 leaked"}}` +
		strings.Repeat(" ", maxErrorBodyBytes))
	body := &trackedReadCloser{Reader: source}
	reader := newSanitizedErrorReader(body, http.StatusForbidden)
	require.NotNil(t, reader)
	assert.False(t, body.closed, "the upstream body stays readable until the rewritten error is closed")

	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "projects/1")
	assert.Contains(t, string(got), "projects/***")
	require.NotZero(t, source.Len(), "the body is longer than the rewrite reads")

	require.NoError(t, reader.Close())
	assert.True(t, body.closed)
	assert.Zero(t, source.Len(), "Close must drain the unread upstream bytes")
}

type trackedReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackedReadCloser) Close() error {
	r.closed = true
	return nil
}
