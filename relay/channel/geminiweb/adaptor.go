package geminiweb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/geminiweb/engine"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

const ChannelName = "geminiweb"

// streamEndpoint is the RPC the web client itself calls. It is reported for
// logging only; the engine builds the real request URL per call.
const streamEndpoint = "https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate"

// maxRequestBytes bounds the inbound body a relayed call will buffer.
const maxRequestBytes = 32 << 20

// geminiWebAccountHeartbeatSeconds keeps last_ok_at meaningful without writing
// the account row on every request: a healthy account is only stamped again once
// this much time has passed since its previous success.
const geminiWebAccountHeartbeatSeconds int64 = 600

// Adaptor relays Gemini Web natively: the Google protocol lives in the engine
// package, the account pool lives in the gemini_web_accounts table.
type Adaptor struct{}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func (a *Adaptor) GetModelList() []string {
	return engine.ModelNames()
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return streamEndpoint, nil
}

// SetupRequestHeader is a no-op: the engine owns every upstream header.
func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return a.delegate(info).ConvertOpenAIRequest(c, info, request)
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return a.delegate(info).ConvertClaudeRequest(c, info, request)
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return a.delegate(info).ConvertGeminiRequest(c, info, request)
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return a.delegate(info).ConvertOpenAIResponsesRequest(c, info, request)
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("gemini web channel: rerank is not supported")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("gemini web channel: embeddings are not supported")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("gemini web channel: audio endpoints are not supported")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("gemini web channel: image endpoints are not supported")
}

// DoRequest runs the whole Gemini Web exchange here instead of delegating to a
// generic HTTP client: there is no OpenAI-shaped upstream to call.
func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	payload, err := io.ReadAll(io.LimitReader(requestBody, maxRequestBytes))
	if err != nil {
		return nil, err
	}
	// The inbound dialect decides the body shape: chat and Claude requests are
	// normalised into a chat request, while the Responses protocol keeps its own
	// DTO and is flattened from instructions plus input items.
	var prompt string
	var specs []toolSpec
	var toolChoice any
	if len(payload) > 0 {
		if info.RelayFormat == types.RelayFormatOpenAIResponses {
			var responsesRequest dto.OpenAIResponsesRequest
			if err := common.Unmarshal(payload, &responsesRequest); err != nil {
				return nil, fmt.Errorf("gemini web channel: decode responses request: %w", err)
			}
			prompt = buildResponsesPrompt(responsesRequest.Input, responsesRequest.Instructions)
		} else {
			var request dto.GeneralOpenAIRequest
			if err := common.Unmarshal(payload, &request); err != nil {
				return nil, fmt.Errorf("gemini web channel: decode request: %w", err)
			}
			prompt = buildPrompt(request.Messages)
			specs = toolSpecsFrom(&request)
			toolChoice = request.ToolChoice
		}
	}
	// Tools have no protocol channel on this origin: they ride inside the prompt
	// as a fenced text protocol.
	prompt = augmentPromptWithTools(prompt, specs, toolChoice)
	logger.LogInfo(c, fmt.Sprintf("[geminiweb] tools=%d prompt_bytes=%d", len(specs), len(prompt)))
	if err := requirePrompt(prompt); err != nil {
		return nil, err
	}

	modelName := strings.TrimSpace(info.OriginModelName)
	if modelName == "" {
		modelName = strings.TrimSpace(info.UpstreamModelName)
	}
	if _, _, ok := engine.ResolveModel(modelName); !ok {
		return nil, fmt.Errorf("gemini web channel: model %q is not served by this channel", modelName)
	}

	// The channel proxy is the only way this origin is reachable from some
	// hosts (mainland networks cannot reach Google directly).
	proxyURL := ""
	if info != nil && info.ChannelSetting.Proxy != "" {
		if parsed, _, parseErr := common.ParseProxyURLRuntime(info.ChannelSetting.Proxy); parseErr == nil && parsed != nil {
			proxyURL = parsed.String()
		}
	}
	ctx := c.Request.Context()
	requestId := newCompletionId()
	created := time.Now().Unix()
	clientStreams := common.GetContextKeyBool(c, constant.ContextKeyIsStream)
	return a.relayWithAccountPool(ctx, info.ChannelId, proxyURL, prompt, modelName, requestId, created, clientStreams, specs)
}

const maxGeminiWebAccountTries = 3

// relayWithAccountPool keeps account selection, egress pinning and per-account
// health writes in one place. A bad cookie is skipped within this request
// instead of making the caller retry the whole channel.
func (a *Adaptor) relayWithAccountPool(
	ctx context.Context,
	channelID int,
	channelProxy string,
	prompt, modelName, requestID string,
	created int64,
	clientStreams bool,
	specs []toolSpec,
) (any, error) {
	tried := make(map[int]bool)
	for len(tried) < maxGeminiWebAccountTries {
		account, err := model.PickGeminiWebAccountExcept(channelID, tried)
		if err != nil {
			return nil, fmt.Errorf("gemini web channel: load account: %w", err)
		}
		if account == nil {
			if len(tried) == 0 && !hasLoginOnlyModel(modelName) {
				return a.relayAnonymous(ctx, channelProxy, prompt, modelName, requestID, created, clientStreams, specs)
			}
			return nil, errors.New("gemini web channel: all enabled accounts are unavailable; re-import a valid cookie or use an anonymous model")
		}
		tried[account.Id] = true
		proxyURL := strings.TrimSpace(account.Proxy)
		if proxyURL == "" {
			proxyURL = channelProxy
			// The binding only pins this fallback for later requests: the current
			// request sends proxyURL either way, so a failed write is logged
			// instead of failing a request that can proceed.
			if err := model.BindGeminiWebAccountProxy(account.Id, proxyURL); err != nil {
				common.SysError(common.LogText("gemini web account proxy binding failed: channel=%d account=%d error=%v", channelID, account.Id, err))
			}
		}
		credential := engine.Credential{
			AccountID: account.Id,
			Cookie:    account.Cookie,
			Proxy:     proxyURL,
			Timeout:   120 * time.Second,
		}
		result, err := a.relaySelectedAccount(ctx, account, credential, prompt, modelName, requestID, created, clientStreams, specs)
		if err == nil {
			return result, nil
		}
		status := authStatusOf(err)
		markAccountResult(account, status, err.Error())
		if status != http.StatusUnauthorized {
			return nil, err
		}
	}
	return nil, errors.New("gemini web channel: no account established a signed-in session")
}

func (a *Adaptor) relayAnonymous(
	ctx context.Context,
	proxyURL, prompt, modelName, requestID string,
	created int64,
	clientStreams bool,
	specs []toolSpec,
) (any, error) {
	credential := engine.Credential{Proxy: proxyURL, Timeout: 120 * time.Second}
	return a.relaySelectedAccount(ctx, nil, credential, prompt, modelName, requestID, created, clientStreams, specs)
}

func (a *Adaptor) relaySelectedAccount(
	ctx context.Context,
	account *model.GeminiWebAccount,
	credential engine.Credential,
	prompt, modelName, requestID string,
	created int64,
	clientStreams bool,
	specs []toolSpec,
) (any, error) {
	if len(specs) > 0 {
		return a.toolCallResponse(ctx, account, credential, prompt, modelName, requestID, created, clientStreams)
	}
	if clientStreams {
		return a.streamResponse(ctx, account, credential, prompt, modelName, requestID, created)
	}
	started := time.Now()
	result, err := engine.Generate(ctx, prompt, modelName, credential, nil, nil)
	logger.LogInfo(ctx, fmt.Sprintf("[geminiweb] engine=%s ttfb=%s prompt_bytes=%d account=%d",
		time.Since(started), resultTTFB(result), len(prompt), credential.AccountID))
	if err != nil {
		return nil, err
	}
	persistAccountResultCookie(credential.AccountID, result)
	markAccountResult(account, http.StatusOK, "")
	return bufferedResponse(result, modelName, prompt, requestID)
}

// streamResponse opens the SSE body before any upstream byte arrives, so the
// client sees the response start immediately while the model is still thinking.
func (a *Adaptor) streamResponse(ctx context.Context, account *model.GeminiWebAccount, credential engine.Credential, prompt, modelName, requestId string, created int64) (any, error) {
	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		if err := streamPrelude(writer, requestId, modelName, created); err != nil {
			return
		}
		emitted := false
		result, err := engine.Generate(ctx, prompt, modelName, credential,
			func(delta string) {
				emitted = true
				_ = streamDelta(writer, requestId, modelName, created, delta, "")
			},
			func(delta string) {
				emitted = true
				_ = streamDelta(writer, requestId, modelName, created, "", delta)
			})
		if err != nil {
			markAccountResult(account, authStatusOf(err), err.Error())
			if !emitted {
				streamFailure(writer, err.Error())
			}
			return
		}
		markAccountResult(account, http.StatusOK, "")
		// Every response refreshes SIDCC family cookies; dropping them makes the
		// session look stale to the origin.
		persistAccountResultCookie(credential.AccountID, result)
		_ = streamFinish(writer, requestId, modelName, created, estimateUsage(modelName, prompt, result.Text))
	}()

	header := http.Header{}
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     header,
		Body:       reader,
	}, nil
}

// DoResponse renders the single answer as whatever dialect the client spoke.
// Chat and Claude inbound requests are handled by the OpenAI handlers, which
// convert internally; the Responses protocol needs its own chat-to-responses
// translation.
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	clientStreams := common.GetContextKeyBool(c, constant.ContextKeyIsStream)
	if info.RelayFormat == types.RelayFormatOpenAIResponses {
		if clientStreams {
			return openai.OaiChatToResponsesStreamHandler(c, info, resp)
		}
		return openai.OaiChatToResponsesHandler(c, info, resp)
	}
	if clientStreams {
		return openai.OaiStreamHandler(c, info, resp)
	}
	return openai.OpenaiHandler(c, info, resp)
}

// delegate returns an OpenAI adaptor so every inbound dialect shares the
// project's request normalisation.
func (a *Adaptor) delegate(info *relaycommon.RelayInfo) *openai.Adaptor {
	adaptor := &openai.Adaptor{}
	adaptor.Init(info)
	return adaptor
}

// resultTTFB reports the upstream time-to-first-byte observed by the engine.
func resultTTFB(result *engine.Result) string {
	if result == nil {
		return "-"
	}
	return result.TTFB.Round(time.Millisecond).String()
}

// newCompletionId returns an OpenAI-shaped completion identifier.
func newCompletionId() string {
	var buffer [12]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return fmt.Sprintf("chatcmpl-geminiweb-%d", time.Now().UnixNano())
	}
	return "chatcmpl-geminiweb-" + hex.EncodeToString(buffer[:])
}

// authStatusOf classifies an engine failure. Only a rejected session counts
// against the account; transport problems do not.
func authStatusOf(err error) int {
	if err == nil {
		return 0
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "snlm0e") || strings.Contains(message, "cookie expired") {
		return http.StatusUnauthorized
	}
	return 0
}

// accountResultAlreadyRecorded reports whether recording this outcome would
// write nothing. A success only clears fail_count, last_error and refreshes
// last_ok_at, so an account that carries no failure state and was stamped within
// the heartbeat window keeps the same state. Every other outcome changes the
// failure state the picker and the console read, so it is always stored.
func accountResultAlreadyRecorded(account *model.GeminiWebAccount, status int, now int64) bool {
	if status != http.StatusOK || account == nil {
		return false
	}
	if account.FailCount != 0 || account.LastError != "" {
		return false
	}
	return account.LastOkAt > 0 && now-account.LastOkAt < geminiWebAccountHeartbeatSeconds
}

// markAccountResult records one upstream outcome. A success on an already clean
// account is skipped so a healthy relay does not write the account row on every
// request; last_ok_at is refreshed by the heartbeat instead, so the timestamp
// stays meaningful without a per-request write. The request already has its own
// result to return, so a lost write is logged, not propagated.
func markAccountResult(account *model.GeminiWebAccount, status int, message string) {
	if account == nil || account.Id <= 0 {
		return
	}
	if accountResultAlreadyRecorded(account, status, common.GetTimestamp()) {
		return
	}
	if err := model.MarkGeminiWebAccountResult(account.Id, status, message); err != nil {
		common.SysError(common.LogText(
			"gemini web account result write failed: account=%d status=%d error=%v", account.Id, status, err))
	}
}

// persistAccountResultCookie stores tickets rotated either before the request
// (session self-heal) or by Set-Cookie on the upstream response. The CAS guard
// prevents a late response from overwriting a newer ticket produced by another
// request or by the background rotation task.
func persistAccountResultCookie(accountID int, result *engine.Result) {
	if accountID <= 0 || result == nil {
		return
	}
	cookie := strings.TrimSpace(result.Cookie)
	if cookie == "" {
		return
	}
	merged := engine.MergeSetCookie(cookie, result.SetCookies)
	if merged == cookie {
		return
	}
	_, _ = model.UpdateGeminiWebAccountCookieIfUnchanged(accountID, cookie, merged)
}

func hasLoginOnlyModel(modelName string) bool {
	_, mc, ok := engine.ResolveModel(modelName)
	return ok && mc.NeedsLogin()
}
