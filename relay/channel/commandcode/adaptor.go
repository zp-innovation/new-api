package commandcode

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	ccore "github.com/QuantumNous/new-api/pkg/commandcode"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	"github.com/QuantumNous/new-api/relay/channel/protocolshim"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	// ChannelName is the adaptor name shown in diagnostics and in the errors
	// returned to API clients. It mirrors the channel type display name so both
	// surfaces agree.
	ChannelName = "Command Code"
)

// Adaptor relays Command Code Provider API credentials.
//
// The upstream exposes Chat Completions, Responses and Anthropic Messages on
// one base URL. Each model's supported_endpoints catalog entry decides which
// upstream protocol serves it; the existing relayconvert registry handles
// cross-protocol request and response conversion.
type Adaptor struct {
	credential *ccore.Credential
	protocol   string
	resolveErr error
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	a.ensureResolved(info)
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func (a *Adaptor) GetModelList() []string {
	if models := ccore.CurrentModelList(); len(models) > 0 {
		return models
	}
	return ccore.DefaultModelIDs()
}

// GetRequestURL selects the endpoint for the model's native protocol.
func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if err := a.ensureResolved(info); err != nil {
		return "", err
	}
	return protocolshim.BuildURL(a.baseURL(info), a.protocol), nil
}

// SetupRequestHeader injects the Command Code API key as a bearer token. The
// Anthropic Messages endpoint uses the same bearer token and additionally
// needs the Anthropic protocol headers.
func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	if err := a.ensureResolved(info); err != nil {
		return err
	}
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+a.credential.APIKey)
	req.Set("User-Agent", ccore.DefaultUserAgent)
	if a.protocol == ccore.ProtocolAnthropic {
		anthropicVersion := c.Request.Header.Get("anthropic-version")
		if anthropicVersion == "" {
			anthropicVersion = "2023-06-01"
		}
		req.Set("anthropic-version", anthropicVersion)
		claude.CommonClaudeHeadersOperation(c, req, info)
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New(ChannelName + " channel: request is nil")
	}
	if err := a.ensureResolved(info); err != nil {
		return nil, err
	}
	if a.protocol == ccore.ProtocolChatCompletions {
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
	if a.protocol == ccore.ProtocolAnthropic {
		return protocolshim.ClaudeDelegate(info).ConvertClaudeRequest(c, info, request)
	}
	return a.convert(c, info, request, a.protocol)
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	if err := a.ensureResolved(info); err != nil {
		return nil, err
	}
	if a.protocol == ccore.ProtocolResponses {
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

// DoResponse delegates to the handler matching the upstream protocol.
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if err := a.ensureResolved(info); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest)
	}
	switch a.protocol {
	case ccore.ProtocolAnthropic:
		return protocolshim.ClaudeDelegate(info).DoResponse(c, resp, info)
	case ccore.ProtocolResponses:
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
	credential, err := ccore.ParseCredential(info.ApiKey)
	if err != nil {
		a.resolveErr = err
		return err
	}
	if ccore.IsUnsupportedModel(info.UpstreamModelName) {
		a.resolveErr = fmt.Errorf(ChannelName+" channel: model %q requires the System One endpoint", info.UpstreamModelName)
		return a.resolveErr
	}
	set, ok := ccore.ResolveProtocolSet(info.UpstreamModelName, credential.ProtocolRules)
	if !ok {
		set = []string{ccore.ProtocolChatCompletions}
	}
	a.credential = credential
	a.protocol = ccore.PickProtocol(set, inboundProtocol(info))
	return nil
}

func inboundProtocol(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ccore.ProtocolChatCompletions
	}
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		return ccore.ProtocolAnthropic
	case types.RelayFormatOpenAIResponses:
		return ccore.ProtocolResponses
	default:
		return ccore.ProtocolChatCompletions
	}
}

func (a *Adaptor) baseURL(info *relaycommon.RelayInfo) string {
	custom := strings.TrimSpace(info.ChannelBaseUrl)
	if custom != "" && custom != constant.GetChannelBaseURL(info.ChannelType) {
		return custom
	}
	return ccore.DefaultProviderBaseURL
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
