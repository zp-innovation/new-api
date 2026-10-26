package antigravity

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	agcore "github.com/QuantumNous/new-api/pkg/antigravity"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// defaultImageTestPrompt is used by the admin "test channel" action for image
// generation models, which need an image-shaped request rather than the
// generic "hi" probe.
const defaultImageTestPrompt = "Draw a simple red circle on a white background."

// Adaptor relays Gemini-protocol requests to Google Antigravity (Cloud Code
// Assist). Request and response conversion is delegated to the Gemini adaptor:
// Antigravity speaks the Gemini generateContent protocol inside its own
// envelope, so only the envelope handling lives here.
type Adaptor struct {
	cred *agcore.Credential
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	action := "generateContent"
	if info.IsStream {
		action = "streamGenerateContent?alt=sse"
	}
	return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, "/v1internal:"+action, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)

	cred, err := a.credentials(info)
	if err != nil {
		return err
	}
	req.Set("Authorization", "Bearer "+cred.AccessToken)
	req.Set("User-Agent", agcore.UserAgent())
	// The v1internal endpoint rejects media types with parameters, so pin the
	// exact JSON type instead of inheriting the client's Content-Type.
	req.Set("Content-Type", "application/json")
	if info.IsStream {
		req.Set("Accept", "text/event-stream")
	}
	return nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return a.convertWithGemini(c, info, func(adaptor *gemini.Adaptor) (any, error) {
		return adaptor.ConvertClaudeRequest(c, info, request)
	})
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("antigravity channel: request is nil")
	}
	return a.convertWithGemini(c, info, func(adaptor *gemini.Adaptor) (any, error) {
		return adaptor.ConvertOpenAIRequest(c, info, request)
	})
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return a.convertWithGemini(c, info, func(adaptor *gemini.Adaptor) (any, error) {
		return adaptor.ConvertOpenAIResponsesRequest(c, info, request)
	})
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return a.convertWithGemini(c, info, func(adaptor *gemini.Adaptor) (any, error) {
		return adaptor.ConvertGeminiRequest(c, info, request)
	})
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("antigravity channel: /v1/rerank endpoint not supported")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("antigravity channel: /v1/embeddings endpoint not supported")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("antigravity channel: audio endpoints not supported")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("antigravity channel: image generation endpoint not supported")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	httpResp, err := a.requestWithCapacityRetry(c, info, requestBody)
	if err != nil {
		return nil, err
	}
	// A failing Cloud Code Assist response can name the GCP project, a
	// service-account email or the consumer number. Rewrite it before the
	// shared error handler forwards any of that to the caller or the console.
	if httpResp != nil &&
		(httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices) {
		httpResp.Body = newSanitizedErrorReader(httpResp.Body, httpResp.StatusCode)
	}
	return httpResp, nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	// Every Antigravity payload is wrapped in {"response": ...}. Unwrapping the
	// body lets the Gemini handlers run unchanged, which keeps billing, usage,
	// grounding and stream lifecycle identical to a native Gemini channel.
	resp.Body = agcore.NewUnwrapReader(resp.Body, info.IsStream)

	// A stream that ends with MALFORMED_FUNCTION_CALL and never produced
	// content is a transient upstream failure. Hold the leading frames until
	// the first meaningful part so the empty case can be retried before any
	// downstream chunk is written.
	if info.IsStream {
		guarded, suppressed, guardErr := prebufferStreamUntilMeaningful(c.Request.Context(), resp.Body, maxStreamPrebufferWait)
		if guardErr != nil {
			// Nothing has been written to the client yet. A canceled caller
			// cannot consume a retry, while a stalled upstream is reported as a
			// gateway timeout so the request can move to another channel.
			if c.Request.Context().Err() != nil {
				return nil, types.NewError(guardErr, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			}
			return nil, types.NewOpenAIError(guardErr, types.ErrorCodeChannelResponseTimeExceeded, http.StatusGatewayTimeout)
		}
		if suppressed {
			return nil, types.NewOpenAIError(
				errors.New("antigravity: upstream stream ended with MALFORMED_FUNCTION_CALL and no content"),
				types.ErrorCodeEmptyResponse,
				http.StatusInternalServerError,
			)
		}
		resp.Body = guarded
	}

	if info.RelayMode == relayconstant.RelayModeResponses {
		if info.IsStream {
			return gemini.GeminiResponsesStreamHandler(c, info, resp)
		}
		return gemini.GeminiResponsesHandler(c, info, resp)
	}
	if info.RelayMode == relayconstant.RelayModeGemini {
		if info.IsStream {
			return gemini.GeminiTextGenerationStreamHandler(c, info, resp)
		}
		return gemini.GeminiTextGenerationHandler(c, info, resp)
	}
	if info.IsStream {
		return gemini.GeminiChatStreamHandler(c, info, resp)
	}
	return gemini.GeminiChatHandler(c, info, resp)
}

// convertWithGemini runs the Gemini adaptor's conversion and wraps the result in
// the Antigravity envelope.
func (a *Adaptor) convertWithGemini(c *gin.Context, info *relaycommon.RelayInfo, convert func(*gemini.Adaptor) (any, error)) (any, error) {
	geminiAdaptor := &gemini.Adaptor{}
	converted, err := convert(geminiAdaptor)
	if err != nil {
		return nil, err
	}

	geminiRequest, ok := converted.(*dto.GeminiChatRequest)
	if !ok {
		return nil, fmt.Errorf("antigravity channel: expected Gemini generateContent request, got %T", converted)
	}
	return a.wrap(info, geminiRequest)
}

func (a *Adaptor) wrap(info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	cred, err := a.credentials(info)
	if err != nil {
		return nil, err
	}

	// Model names outside the default table pass through untouched so an
	// operator-configured model mapping keeps working.
	model := strings.TrimSpace(info.UpstreamModelName)
	if variant, ok := agcore.ResolveThinkingVariant(model, a.preferredThinkingLevel(request)); ok {
		model = variant
	} else if mapped, ok := agcore.MapModel(model); ok {
		model = mapped
	}
	if info.IsChannelTest && isImageGenerationModel(model) {
		a.applyImageTestRequest(request)
	}
	return agcore.WrapRequest(cred.ProjectID, model, request)
}

// preferredThinkingLevel mirrors the upstream client's mapping from a Gemini
// thinkingConfig onto the thinking-depth suffix of the model id.
func (a *Adaptor) preferredThinkingLevel(request *dto.GeminiChatRequest) string {
	if request == nil || request.GenerationConfig.ThinkingConfig == nil {
		return "high"
	}
	config := request.GenerationConfig.ThinkingConfig
	switch level := strings.ToLower(strings.TrimSpace(config.ThinkingLevel)); level {
	case "low", "medium", "high":
		return level
	}
	if config.ThinkingBudget == nil {
		return "high"
	}
	switch budget := *config.ThinkingBudget; {
	case budget < 0:
		return "high"
	case budget <= 1024:
		return "low"
	case budget <= 8192:
		return "medium"
	default:
		return "high"
	}
}

// isImageGenerationModel reports whether the upstream id is a Gemini image
// model. Their channel test needs an image-shaped request.
func isImageGenerationModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gemini-") && strings.Contains(model, "-image")
}

// applyImageTestRequest rewrites the generic channel-test probe into the
// request an image model needs: an image prompt, text and image response
// modalities, and a square aspect ratio.
func (a *Adaptor) applyImageTestRequest(request *dto.GeminiChatRequest) {
	if request == nil {
		return
	}
	request.Contents = []dto.GeminiChatContent{
		{
			Role:  "user",
			Parts: []dto.GeminiPart{{Text: defaultImageTestPrompt}},
		},
	}
	request.GenerationConfig.MaxOutputTokens = nil
	request.GenerationConfig.ResponseModalities = []string{"TEXT", "IMAGE"}
	if len(request.GenerationConfig.ImageConfig) == 0 {
		request.GenerationConfig.ImageConfig = json.RawMessage(`{"aspectRatio":"1:1"}`)
	}
}

// credentials parses the channel key once per request.
func (a *Adaptor) credentials(info *relaycommon.RelayInfo) (*agcore.Credential, error) {
	if a.cred != nil {
		return a.cred, nil
	}
	cred, err := agcore.ParseCredential(info.ApiKey)
	if err != nil {
		return nil, err
	}
	a.cred = cred
	return cred, nil
}
