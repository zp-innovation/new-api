package antigravity

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// Cloud Code Assist answers 503 MODEL_CAPACITY_EXHAUSTED while a model's
// shared capacity pool is temporarily full; the same request usually succeeds
// after a short wait. The budget stays small because the first rejection also
// opens the fail-fast window below, so replaying the request dozens of times
// only adds upstream load without improving the outcome.
var (
	capacityRetryAttempts = 4
	capacityRetryInterval = time.Second
	capacityCooldown      = 10 * time.Second
)

// capacityPeekBytes bounds how much of a response body is buffered to decide
// whether it is a capacity rejection. The body is restored afterwards.
const capacityPeekBytes = 8 << 10

// modelCapacityUntil deduplicates retries across concurrent requests: once a
// model is known to be exhausted, later requests for it fail fast instead of
// each waiting out the full retry budget.
var modelCapacityUntil sync.Map // model name -> time.Time

// isModelCapacityExhausted reports whether a failing response carries the
// explicit MODEL_CAPACITY_EXHAUSTED reason.
func isModelCapacityExhausted(statusCode int, body []byte) bool {
	if statusCode != http.StatusServiceUnavailable && statusCode != http.StatusTooManyRequests {
		return false
	}
	return bytes.Contains(bytes.ToUpper(body), []byte("MODEL_CAPACITY_EXHAUSTED"))
}

// modelCapacityCooldownActive reports whether another request already gave up
// on this model recently.
func modelCapacityCooldownActive(model string, now time.Time) bool {
	if strings.TrimSpace(model) == "" {
		return false
	}
	until, ok := modelCapacityUntil.Load(model)
	if !ok {
		return false
	}
	deadline, ok := until.(time.Time)
	if !ok || now.After(deadline) {
		modelCapacityUntil.Delete(model)
		return false
	}
	return true
}

// markModelCapacityCooldown starts the fail-fast window for a model whose
// capacity pool just rejected a request.
func markModelCapacityCooldown(model string, now time.Time) {
	if strings.TrimSpace(model) == "" || capacityCooldown <= 0 {
		return
	}
	modelCapacityUntil.Store(model, now.Add(capacityCooldown))
}

// replayableBody wraps a restored response body so Close still reaches the
// upstream connection while reads replay the peeked prefix first.
type replayableBody struct {
	io.Reader
	closer io.Closer
}

func (b *replayableBody) Close() error {
	return b.closer.Close()
}

// replayRequestReader presents a fresh storage cursor to net/http while still
// exposing the Size/NewReader metadata the transport uses to set
// Content-Length and GetBody on a retried request.
type replayRequestReader struct {
	common.ReplayableBody
	reader io.ReadCloser
}

func (r replayRequestReader) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

// peekResponseBody reads up to limit bytes and restores the response body so
// the caller can still hand it to the normal error path.
func peekResponseBody(resp *http.Response, limit int64) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	peeked, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if len(peeked) > 0 {
		resp.Body = &replayableBody{
			Reader: io.MultiReader(bytes.NewReader(peeked), resp.Body),
			closer: resp.Body,
		}
	}
	return peeked, err
}

// requestWithCapacityRetry performs the upstream call and retries the same
// request while the model reports MODEL_CAPACITY_EXHAUSTED. Retrying needs a
// replayable body, so callers that stream from a one-shot reader keep the
// single-attempt behavior.
func (a *Adaptor) requestWithCapacityRetry(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	resp, err := channel.DoApiRequest(a, c, info, requestBody)
	if err != nil {
		return nil, err
	}
	httpResp := resp
	if httpResp == nil {
		return resp, nil
	}

	replay, replayable := requestBody.(common.ReplayableBody)
	model := strings.TrimSpace(info.UpstreamModelName)
	// Another request already gave up on this model: fail fast instead of
	// replaying the retry budget for every caller.
	failFast := modelCapacityCooldownActive(model, time.Now())

	for attempt := 0; attempt < capacityRetryAttempts; attempt++ {
		peeked, peekErr := peekResponseBody(httpResp, capacityPeekBytes)
		if !isModelCapacityExhausted(httpResp.StatusCode, peeked) {
			return resp, peekErr
		}
		// Open the fail-fast window on the first rejection rather than after this
		// request's retries are exhausted: a full capacity pool rejects every
		// concurrent caller, so each of them replaying a full budget only makes
		// the exhaustion last longer. This request keeps its own retries.
		markModelCapacityCooldown(model, time.Now())
		if failFast || !replayable {
			return resp, nil
		}

		_ = httpResp.Body.Close()
		if !sleepWithContext(c.Request.Context(), capacityRetryInterval) {
			return nil, c.Request.Context().Err()
		}

		reader, readerErr := replay.NewReader()
		if readerErr != nil {
			return nil, readerErr
		}
		resp, err = channel.DoApiRequest(a, c, info, replayRequestReader{ReplayableBody: replay, reader: reader})
		if err != nil {
			return nil, err
		}
		httpResp = resp
		if httpResp == nil {
			return resp, nil
		}
	}
	return resp, nil
}

// sleepWithContext waits for d unless the request is cancelled first. It
// reports false when the context ended.
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
