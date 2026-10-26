package workbuddy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// maxCapBodyBytes bounds the rejection body the cap sniffer will buffer. The
// upstream's quota answer is a short JSON envelope, so this is only a guard
// against a misbehaving edge making the relay allocate without limit.
const maxCapBodyBytes = 64 << 10

// captureCapEvent inspects a rejected upstream answer and records a quota cap
// when the body carries the upstream's 6004 envelope code. The body is read and
// restored so the downstream handler still parses exactly what upstream sent,
// and a bookkeeping failure can never change the reply the client receives.
func (a *Adaptor) captureCapEvent(ctx context.Context, resp *http.Response, info *relaycommon.RelayInfo) {
	if resp == nil || resp.Body == nil || info == nil {
		return
	}
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusBadRequest {
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCapBodyBytes+1))
	if err != nil {
		// The body is unusable, but the caller must still get a body to close.
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return
	}
	// Restore exactly what upstream sent. When the read hit the sniff bound the
	// tail is still unread on the original body, so the prefix is replayed and
	// the rest streams through; the error handler and the client then see the
	// whole reply byte for byte.
	if len(body) > maxCapBodyBytes {
		resp.Body = struct {
			io.Reader
			io.Closer
		}{
			Reader: io.MultiReader(bytes.NewReader(body), resp.Body),
			Closer: resp.Body,
		}
	} else {
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	// The cap code is always near the start of the envelope, so the sniffed
	// prefix is enough to classify it.
	if !wbcore.MessageHasRateLimitCode(string(body)) {
		return
	}
	uid, realm := a.accountIdentity(info)
	if uid == "" {
		return
	}
	modelName := strings.TrimSpace(info.OriginModelName)
	if modelName == "" {
		modelName = strings.TrimSpace(info.UpstreamModelName)
	}
	if modelName == "" {
		return
	}
	service.CaptureWorkBuddyCapEvent(ctx, info.ChannelId, uid, realm, modelName, string(body), time.Now())
}

// accountIdentity resolves the credential the relay actually used. The channel
// key the distributor selected is the one in the context, so a multi-key channel
// reports the right account rather than the first line of the stored key.
func (a *Adaptor) accountIdentity(info *relaycommon.RelayInfo) (string, string) {
	credential, err := a.credentials(info)
	if err != nil || credential == nil {
		return "", ""
	}
	return strings.TrimSpace(credential.UID), string(credential.Identity().Realm)
}
