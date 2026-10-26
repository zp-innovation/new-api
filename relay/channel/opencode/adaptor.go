package opencode

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	occore "github.com/QuantumNous/new-api/pkg/opencode"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	"github.com/QuantumNous/new-api/relay/channel/protocolshim"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const (
	// ChannelName is the adaptor name shown in diagnostics and used as the
	// client-facing error prefix. It stays the channel slug because it is also the
	// model owner name exposed by the model list endpoint.
	ChannelName           = "opencode"
	openCodeSessionHeader = "X-OpenCode-Session"
)

// Adaptor relays OpenCode Console credentials to the Zen or Go gateway.
//
// The upstream exposes Chat Completions, Responses, and Anthropic Messages on
// separate endpoints. Each model is routed to the endpoint the upstream
// documents for it; the existing relayconvert registry handles cross-protocol
// request and response conversion.
type Adaptor struct {
	credential *occore.Credential
	protocol   string
	sessionID  string
	resolveErr error
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	a.ensureResolved(info)
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func (a *Adaptor) GetModelList() []string {
	return occore.ModelList
}

// GetRequestURL selects the endpoint for the model's native protocol.
func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if err := a.ensureResolved(info); err != nil {
		return "", err
	}
	baseURL := a.baseURL(info)
	return protocolshim.BuildURL(baseURL, a.protocol), nil
}

// SetupRequestHeader injects the OAuth access token and the OpenCode session
// identity. Anthropic Messages uses x-api-key because the public proxy reads
// that header for /messages and translates it to a bearer token downstream.
func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	if err := a.ensureResolved(info); err != nil {
		return err
	}
	channel.SetupApiRequestHeader(info, c, req)
	if a.protocol == occore.ProtocolAnthropic {
		req.Del("Authorization")
		req.Set("x-api-key", a.credential.AccessToken)
	} else {
		req.Set("Authorization", "Bearer "+a.credential.AccessToken)
	}
	req.Set("User-Agent", occore.DefaultUserAgent)
	if strings.TrimSpace(a.sessionID) == "" {
		a.sessionID = strings.TrimSpace(c.GetHeader(openCodeSessionHeader))
	}
	if strings.TrimSpace(a.sessionID) == "" {
		a.sessionID = uuid.NewString()
	}
	req.Set(openCodeSessionHeader, a.sessionID)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New(ChannelName + " channel: request is nil")
	}
	if err := a.ensureResolved(info); err != nil {
		return nil, err
	}
	a.captureSessionID(c, request.PromptCacheKey, metadataString(request.Metadata, "session_id", "user_id"))
	if a.protocol == occore.ProtocolChatCompletions {
		return protocolshim.OpenAIDelegate(info).ConvertOpenAIRequest(c, info, request)
	}
	return a.convert(c, info, request, a.protocol)
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request == nil {
		return nil, errors.New(ChannelName + " channel: request is nil")
	}
	if err := a.ensureResolved(info); err != nil {
		return nil, err
	}
	a.captureSessionID(c, metadataString(request.Metadata, "session_id", "user_id"))
	if a.protocol == occore.ProtocolAnthropic {
		return protocolshim.ClaudeDelegate(info).ConvertClaudeRequest(c, info, request)
	}
	return a.convert(c, info, request, a.protocol)
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	if err := a.ensureResolved(info); err != nil {
		return nil, err
	}
	a.captureSessionID(c, rawString(request.PromptCacheKey), metadataString(request.Metadata, "session_id", "user_id"))
	if a.protocol == occore.ProtocolResponses {
		return protocolshim.OpenAIDelegate(info).ConvertOpenAIResponsesRequest(c, info, request)
	}
	return a.convert(c, info, request, a.protocol)
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New(ChannelName + " channel: Gemini generateContent ingress is not supported")
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New(ChannelName + " channel: /v1/rerank endpoint not supported")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New(ChannelName + " channel: /v1/embeddings endpoint not supported")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New(ChannelName + " channel: audio endpoints not supported")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New(ChannelName + " channel: image generation endpoint not supported")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

// DoResponse delegates to the handler matching the upstream protocol. Those
// handlers already know how to convert back into info.RelayFormat.
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if err := a.ensureResolved(info); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest)
	}
	switch a.protocol {
	case occore.ProtocolAnthropic:
		return protocolshim.ClaudeDelegate(info).DoResponse(c, resp, info)
	case occore.ProtocolResponses:
		if info.RelayFormat == types.RelayFormatOpenAIResponses {
			if info.IsStream {
				return openai.OaiResponsesStreamHandler(c, info, resp)
			}
			return openai.OaiResponsesHandler(c, info, resp)
		}
		if info.IsStream {
			return openai.OaiResponsesToChatStreamHandler(c, info, resp)
		}
		return openai.OaiResponsesToChatHandler(c, info, resp)
	default:
		if info.RelayFormat == types.RelayFormatOpenAIResponses {
			if info.IsStream {
				return openai.OaiChatToResponsesStreamHandler(c, info, resp)
			}
			return openai.OaiChatToResponsesHandler(c, info, resp)
		}
		return protocolshim.OpenAIDelegate(info).DoResponse(c, resp, info)
	}
}

func (a *Adaptor) ensureResolved(info *relaycommon.RelayInfo) error {
	if a.resolveErr != nil {
		return a.resolveErr
	}
	if a.credential != nil {
		return nil
	}
	if info == nil {
		a.resolveErr = errors.New(ChannelName + " channel: missing relay info")
		return a.resolveErr
	}
	credential, err := occore.ParseCredential(info.ApiKey)
	if err != nil {
		a.resolveErr = err
		return err
	}
	if occore.IsUnsupportedModel(info.UpstreamModelName) {
		a.resolveErr = fmt.Errorf(ChannelName+" channel: model %q requires a dedicated endpoint", info.UpstreamModelName)
		return a.resolveErr
	}
	if strings.TrimSpace(credential.Mode) == "" {
		credential.Mode = modeFromChannelBaseURL(info.ChannelBaseUrl)
	}
	a.credential = credential
	a.protocol = occore.ResolveProtocol(credential.ModeOrDefault(), info.UpstreamModelName, credential.ProtocolRules)
	return nil
}

func modeFromChannelBaseURL(baseURL string) string {
	if strings.Contains(strings.ToLower(baseURL), "/zen/go") {
		return occore.ModeGo
	}
	return occore.ModeZen
}

func (a *Adaptor) baseURL(info *relaycommon.RelayInfo) string {
	custom := strings.TrimSpace(info.ChannelBaseUrl)
	if custom != "" && custom != constant.GetChannelBaseURL(info.ChannelType) {
		return custom
	}
	return occore.ProtocolBaseURL(a.credential.ModeOrDefault(), a.protocol)
}

func (a *Adaptor) convert(c *gin.Context, info *relaycommon.RelayInfo, request any, protocol string) (any, error) {
	target := protocolshim.RelayFormatForProtocol(protocol)
	result, err := service.ConvertRequest(c, info, target, request)
	if err != nil {
		return nil, err
	}
	info.FinalRequestRelayFormat = target
	info.AppendRequestConversion(target)
	return result.Value, nil
}

func (a *Adaptor) captureSessionID(c *gin.Context, candidates ...string) {
	if c != nil {
		if sessionID := strings.TrimSpace(c.GetHeader(openCodeSessionHeader)); sessionID != "" {
			a.sessionID = sessionID
			return
		}
	}
	for _, candidate := range candidates {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			a.sessionID = candidate
			return
		}
	}
}

// metadataString extracts the session id carried by metadata-shaped JSON. Only
// scalar strings are eligible: an object or array value under a candidate key is
// skipped instead of being sent upstream as its JSON text. Bare string metadata
// keeps the historical contract.
func metadataString(raw []byte, keys ...string) string {
	if len(raw) == 0 {
		return ""
	}
	for _, key := range keys {
		result := gjson.GetBytes(raw, key)
		if result.Type != gjson.String {
			continue
		}
		if value := strings.TrimSpace(result.Str); value != "" {
			return value
		}
	}
	return rawString(raw)
}

// rawString reads a session id from an unstructured field such as
// prompt_cache_key. Structured JSON is rejected so a request body object cannot
// leak into the session header; scalar JSON strings keep their contract.
func rawString(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	parsed := gjson.ParseBytes(raw)
	if parsed.IsObject() || parsed.IsArray() {
		return ""
	}
	var value string
	if err := common.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(raw))
}
