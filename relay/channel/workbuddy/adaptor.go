package workbuddy

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	wbcore "github.com/QuantumNous/new-api/pkg/workbuddy"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// Adaptor relays OpenAI-protocol requests to WorkBuddy.
//
// WorkBuddy speaks the OpenAI chat completions protocol on an edge host chosen
// by the account's realm and imitated client, and it only answers streaming
// requests. Request conversion is delegated to the OpenAI adaptor; this adaptor
// owns the credential, the identity headers, the endpoint choice and the fold
// that turns an always-streaming upstream answer back into the buffered object
// a buffered client asked for.
type Adaptor struct {
	cred *wbcore.Credential
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func (a *Adaptor) GetModelList() []string {
	return wbcore.ModelList
}

// GetRequestURL picks the chat endpoint. The realm and the imitated client
// decide the host (wb_identity.py:51-68), so the credential overrides the
// channel row; an operator who set an explicit API address overrides both,
// which is how a channel is pointed at an alternate exit.
func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	credential, err := a.credentials(info)
	if err != nil {
		return "", err
	}

	baseURL := credential.ChatBaseURL()
	if info.ChannelBaseUrl != "" && info.ChannelBaseUrl != constant.GetChannelBaseURL(info.ChannelType) {
		baseURL = info.ChannelBaseUrl
	}
	if strings.TrimSpace(baseURL) == "" {
		return "", errors.New("workbuddy channel: chat base url is empty")
	}
	return relaycommon.GetFullRequestURL(strings.TrimRight(baseURL, "/"), wbcore.ChatCompletionsPath, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)

	credential, err := a.credentials(info)
	if err != nil {
		return err
	}
	identity := credential.Identity()
	if strings.TrimSpace(identity.AccessToken) == "" {
		return errors.New("workbuddy channel: access_token is required")
	}
	for key, value := range identity.Headers(wbcore.PurposeChat) {
		if strings.TrimSpace(value) == "" {
			continue
		}
		req.Set(key, value)
	}
	// The relay never sends cookies; a stale one would shadow the identity on
	// the upstream edge.
	req.Del("Cookie")
	if info.IsStream {
		// The upstream only answers streams, so the client's Accept header must
		// not talk it out of the event stream.
		req.Set("Accept", "text/event-stream")
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("workbuddy channel: request is nil")
	}
	converted, err := a.delegate(info).ConvertOpenAIRequest(c, info, request)
	if err != nil {
		return nil, err
	}
	return a.finalize(converted, info)
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	converted, err := a.delegate(info).ConvertClaudeRequest(c, info, request)
	if err != nil {
		return nil, err
	}
	return a.finalize(converted, info)
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	converted, err := a.delegate(info).ConvertGeminiRequest(c, info, request)
	if err != nil {
		return nil, err
	}
	return a.finalize(converted, info)
}

// ConvertOpenAIResponsesRequest rewrites a Responses request as the chat body
// the upstream serves. WorkBuddy speaks chat completions only, so the standard
// Responses-to-chat conversion runs first and its result goes through the same
// finalize as every other inbound dialect.
func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	result, err := service.ConvertRequest(c, info, types.RelayFormatOpenAI, &request)
	if err != nil {
		return nil, err
	}
	converted, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("workbuddy channel: expected an OpenAI chat request, got %T", result.Value)
	}
	return a.finalize(converted, info)
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("workbuddy channel: /v1/rerank endpoint not supported")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("workbuddy channel: /v1/embeddings endpoint not supported")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("workbuddy channel: audio endpoints not supported")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("workbuddy channel: image generation endpoint not supported")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	resp, err := channel.DoApiRequest(a, c, info, requestBody)
	if err != nil {
		return nil, err
	}
	// A WorkBuddy quota cap (code 6004) is the only moment the upstream names
	// its window reset clock and the only moment the budget can be learned, so
	// record it here before the generic error path consumes the body.
	a.captureCapEvent(c.Request.Context(), resp, info)
	return resp, nil
}

// CheckErrorResponse lets the WorkBuddy adaptor classify model-scoped upstream
// codes before the shared handler treats every 5xx as an account failure.
func (a *Adaptor) CheckErrorResponse(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) *types.NewAPIError {
	if err := a.classifyModelScopedResponse(resp, info); err != nil {
		return err
	}
	a.captureEntitlementEvent(resp, info)
	return nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		return nil, types.NewError(errors.New("workbuddy channel: /v1/responses/compact endpoint not supported"), types.ErrorCodeInvalidRequest)
	}
	// The compatible handler promotes info.IsStream as soon as the upstream
	// advertises text/event-stream, but WorkBuddy streams every upstream answer
	// even for a buffered client. The client's original intent survives in the
	// request context, so read it there: only a client that asked for a stream
	// gets the stream handler, everyone else gets the folded object.
	clientStreams := common.GetContextKeyBool(c, constant.ContextKeyIsStream)
	if info.RelayMode == relayconstant.RelayModeResponses {
		// The upstream answer is a chat.completion stream even though the
		// client asked for the Responses protocol, so the chat frames are
		// translated back into Responses events.
		if clientStreams {
			return openai.OaiChatToResponsesStreamHandler(c, info, resp)
		}
		if foldErr := foldChatCompletionsStream(resp); foldErr != nil {
			return nil, types.NewOpenAIError(foldErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		return openai.OaiChatToResponsesHandler(c, info, resp)
	}
	if clientStreams {
		usage, apiErr := openai.OaiStreamHandler(c, info, resp)
		if apiErr == nil {
			a.clearEntitlementAfterSuccess(info)
		}
		return usage, apiErr
	}
	// The upstream streams even for a buffered client, so the body is folded
	// back into one chat.completion object before the buffered handler runs.
	if foldErr := foldChatCompletionsStream(resp); foldErr != nil {
		return nil, types.NewOpenAIError(foldErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	usage, apiErr := openai.OpenaiHandler(c, info, resp)
	if apiErr == nil {
		a.clearEntitlementAfterSuccess(info)
	}
	return usage, apiErr
}

// delegate returns an OpenAI adaptor initialised for this channel, so the
// standard OpenAI request normalisation (channel settings, reasoning suffix,
// token field translation) applies to every inbound dialect.
func (a *Adaptor) delegate(info *relaycommon.RelayInfo) *openai.Adaptor {
	adaptor := &openai.Adaptor{}
	adaptor.Init(info)
	return adaptor
}

// finalize forces the shape the upstream accepts. WorkBuddy answers a buffered
// chat request with HTTP 400 "Non-stream chat request is currently not
// supported", so every relayed call streams upstream and DoResponse folds the
// answer back for buffered clients. normalizeUpstreamRequest applies the rest
// of the upstream's request rules (see convert.go).
func (a *Adaptor) finalize(converted any, info *relaycommon.RelayInfo) (any, error) {
	request, ok := converted.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("workbuddy channel: expected an OpenAI chat request, got %T", converted)
	}
	credential, err := a.credentials(info)
	if err != nil {
		return nil, err
	}
	normalizeUpstreamRequest(request, defaultThinkingEnabled(info), credential.Identity().Realm == wbcore.RealmCN)
	request.Stream = lo.ToPtr(true)
	request.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	return request, nil
}

// defaultThinkingEnabled reads the channel's WorkBuddy thinking default. The
// reference behaviour is a reasoning trace, so an absent setting means on; an
// operator turns it off to make thinking opt-in.
func defaultThinkingEnabled(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return true
	}
	setting := info.ChannelSetting.WorkBuddyDefaultThinking
	return setting == nil || *setting
}

// credentials parses the channel key once per request.
func (a *Adaptor) credentials(info *relaycommon.RelayInfo) (*wbcore.Credential, error) {
	if a.cred != nil {
		return a.cred, nil
	}
	credential, err := wbcore.ParseCredential(info.ApiKey)
	if err != nil {
		return nil, err
	}
	a.cred = credential
	return credential, nil
}
