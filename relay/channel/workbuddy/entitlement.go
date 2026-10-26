package workbuddy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// captureEntitlementEvent reads a non-streaming refusal only far enough to
// classify its upstream business code, then restores the exact body. A 11158
// means the current account may not call this model; it is persisted per
// account so routing can skip the pair without disabling the whole channel.
func (a *Adaptor) captureEntitlementEvent(resp *http.Response, info *relaycommon.RelayInfo) {
	if resp == nil || resp.Body == nil || info == nil || resp.StatusCode < http.StatusBadRequest {
		return
	}
	body, err := readAndRestoreResponseBody(resp, maxCapBodyBytes)
	if err != nil {
		return
	}

	code, ok := wbcore.ChatErrorCode(string(body))
	if !ok || !wbcore.IsModelEntitlementChatError(code) {
		return
	}
	credential, err := a.credentials(info)
	if err != nil || credential == nil {
		return
	}
	userType := entitlementUserType(string(body))
	for _, modelName := range entitlementModelNames(info) {
		if err := model.MarkWorkBuddyModelBlocked(info.ChannelId, credential.UID, string(credential.Identity().Realm), modelName, code, userType, time.Now()); err != nil {
			common.SysError(common.LogText("workbuddy entitlement mark failed: channel=%d uid=%s model=%s error=%v", info.ChannelId, credential.UID, modelName, err))
		}
	}
}

// classifyModelScopedResponse peeks at an error envelope, stores a 11158 mark,
// and returns a non-retryable model-scoped error. The body is restored before
// returning so the normal error handler remains the fallback for other codes.
func (a *Adaptor) classifyModelScopedResponse(resp *http.Response, info *relaycommon.RelayInfo) *types.NewAPIError {
	if resp == nil || resp.Body == nil || resp.StatusCode < 400 || resp.StatusCode >= 600 {
		return nil
	}
	body, err := readAndRestoreResponseBody(resp, maxCapBodyBytes)
	if err != nil {
		return nil
	}
	code, ok := wbcore.ChatErrorCode(string(body))
	if !ok || !wbcore.IsModelScopedChatError(code) {
		return nil
	}
	if wbcore.IsModelEntitlementChatError(code) {
		a.captureEntitlementEvent(resp, info)
	}
	// The caller returns this error without reading the body, so release the
	// upstream response here instead of leaving the connection to the garbage
	// collector.
	_ = resp.Body.Close()
	return ModelScopedRefusalError(body, resp.StatusCode)
}

func entitlementUserType(body string) string {
	var payload struct {
		UserType  string `json:"userType"`
		UserType2 string `json:"user_type"`
		Message   string `json:"message"`
		Msg       string `json:"msg"`
	}
	if err := common.Unmarshal([]byte(body), &payload); err != nil {
		return ""
	}
	if value := strings.TrimSpace(payload.UserType); value != "" {
		return value
	}
	if value := strings.TrimSpace(payload.UserType2); value != "" {
		return value
	}
	message := strings.TrimSpace(payload.Message)
	if message == "" {
		message = payload.Msg
	}
	const marker = "user type ["
	if start := strings.Index(message, marker); start >= 0 {
		start += len(marker)
		if end := strings.Index(message[start:], "]"); end >= 0 {
			return strings.TrimSpace(message[start : start+end])
		}
	}
	return ""
}

// readAndRestoreResponseBody buffers at most limit bytes of an error body for
// classification and puts the body back for whoever reads it next. When the
// envelope is longer than the bound the unread tail stays attached to the
// original reader: re-encoding only the prefix would hand the shared error
// handler a truncated JSON document and lose the upstream's error reason.
func readAndRestoreResponseBody(resp *http.Response, limit int64) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, io.EOF
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return nil, err
	}
	if int64(len(body)) > limit {
		// The tail stays attached to the live reader: re-encoding only the
		// prefix would hand the next reader a truncated JSON document and lose
		// the upstream's error reason.
		resp.Body = struct {
			io.Reader
			io.Closer
		}{Reader: io.MultiReader(bytes.NewReader(body), resp.Body), Closer: resp.Body}
		return body, nil
	}
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// clearEntitlementAfterSuccess lets a renewed plan become usable immediately
// instead of waiting for the 24 hour mark to expire.
func (a *Adaptor) clearEntitlementAfterSuccess(info *relaycommon.RelayInfo) {
	credential, err := a.credentials(info)
	if err != nil || credential == nil {
		return
	}
	for _, modelName := range entitlementModelNames(info) {
		if err := model.ClearWorkBuddyModelBlocked(info.ChannelId, credential.UID, modelName); err != nil {
			common.SysError(common.LogText("workbuddy entitlement clear failed: channel=%d uid=%s model=%s error=%v", info.ChannelId, credential.UID, modelName, err))
		}
	}
}

func entitlementModelNames(info *relaycommon.RelayInfo) []string {
	if info == nil {
		return nil
	}
	names := make([]string, 0, 2)
	for _, value := range []string{info.OriginModelName, info.UpstreamModelName} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		found := false
		for _, existing := range names {
			if existing == value {
				found = true
				break
			}
		}
		if !found {
			names = append(names, value)
		}
	}
	return names
}

// maxRefusalMessageRunes bounds the upstream refusal text forwarded to the
// client. The raw refusal body can be a multi-kilobyte HTML page or a JSON
// envelope that names the account, so only a short single-line excerpt is kept.
const maxRefusalMessageRunes = 300

// ModelScopedRefusalError builds a shared relay error for a model/request
// refusal. Such errors must not charge the account or disable the channel.
func ModelScopedRefusalError(body []byte, statusCode int) *types.NewAPIError {
	var decoded dto.GeneralErrorResponse
	_ = common.Unmarshal(body, &decoded)
	// Only a message the upstream explicitly declared is forwarded. The raw body
	// itself never is: it can be an HTML page or a JSON document that names the
	// account or exposes internal fields, and the shared error handler would
	// surface it verbatim to the caller and the console.
	message := strings.Join(strings.Fields(decoded.ToMessage()), " ")
	message = common.MaskSensitiveInfo(message)
	if runes := []rune(message); len(runes) > maxRefusalMessageRunes {
		message = string(runes[:maxRefusalMessageRunes]) + "..."
	}
	if message == "" {
		message = fmt.Sprintf("upstream refused the model request (status %d)", statusCode)
	}
	return types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, statusCode,
		types.ErrOptionWithSkipRetry(), types.ErrOptionWithModelScoped())
}
