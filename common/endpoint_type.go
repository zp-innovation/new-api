package common

import "github.com/QuantumNous/new-api/constant"

// GetEndpointTypesByChannelType 获取渠道最优先端点类型（所有的渠道都支持 OpenAI 端点）
func GetEndpointTypesByChannelType(channelType int, modelName string) []constant.EndpointType {
	// The DeepSeek Search and Fetch routing names serve only their endpoint.
	switch modelName {
	case constant.DeepSeekSearchModel:
		return []constant.EndpointType{constant.EndpointTypeDeepSeekToolsSearch}
	case constant.DeepSeekFetchModel:
		return []constant.EndpointType{constant.EndpointTypeDeepSeekToolsFetch}
	}
	var endpointTypes []constant.EndpointType
	switch channelType {
	case constant.ChannelTypeJina:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeJinaRerank}
	//case constant.ChannelTypeMidjourney, constant.ChannelTypeMidjourneyPlus:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeMidjourney}
	//case constant.ChannelTypeSunoAPI:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeSuno}
	//case constant.ChannelTypeKling:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeKling}
	//case constant.ChannelTypeJimeng:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeJimeng}
	case constant.ChannelTypeAws:
		fallthrough
	case constant.ChannelTypeAnthropic:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAI}
	case constant.ChannelTypeVertexAi:
		fallthrough
	case constant.ChannelTypeGemini:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}
	case constant.ChannelTypeOpenRouter: // OpenRouter 只支持 OpenAI 端点
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
	case constant.ChannelTypeXai:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}
	case constant.ChannelTypeVLLM, constant.ChannelTypeSGLang:
		endpointTypes = GetAdvancedCustomPreset(channelType).SupportedEndpointTypesForModel(modelName)
	case constant.ChannelTypeSora:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
	case constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI:
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
			constant.EndpointTypeOpenAIResponseCompact,
			constant.EndpointTypeAnthropic,
			constant.EndpointTypeGemini,
			constant.EndpointTypeOpenAIAlphaSearch,
		}
	case constant.ChannelTypeCodex:
		// The Codex backend only speaks the Responses protocol. Chat
		// Completions and Anthropic Messages are upgraded to Responses by the
		// relay, so advertise those client-facing entry points too.
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
			constant.EndpointTypeOpenAIResponseCompact,
			constant.EndpointTypeAnthropic,
			constant.EndpointTypeOpenAIAlphaSearch,
		}
	case constant.ChannelTypeAntigravity:
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeAnthropic,
			constant.EndpointTypeGemini,
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
		}
	case constant.ChannelTypeWorkBuddy:
		// WorkBuddy speaks the OpenAI chat protocol; the relay converts the
		// Responses, Claude and Gemini request dialects into it.
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
			constant.EndpointTypeAnthropic,
			constant.EndpointTypeGemini,
		}
	case constant.ChannelTypeOpenCode:
		// OpenCode routes each model to Chat, Responses, or Anthropic. Gemini
		// models use a dedicated endpoint and are rejected by the adaptor.
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
			constant.EndpointTypeAnthropic,
		}
	case constant.ChannelTypeCommandCode:
		// Command Code routes each model to Chat Completions, Responses, or
		// Anthropic Messages; the System One decision API is not relayed.
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
			constant.EndpointTypeAnthropic,
		}
	case constant.ChannelTypeGeminiWeb:
		// The gemini-web2api-go sidecar exposes OpenAI-compatible chat and
		// Responses endpoints; the shared handlers convert the other dialects.
		endpointTypes = []constant.EndpointType{
			constant.EndpointTypeOpenAI,
			constant.EndpointTypeOpenAIResponse,
			constant.EndpointTypeAnthropic,
			constant.EndpointTypeGemini,
		}
	default:
		if IsOpenAIResponseOnlyModel(modelName) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIResponse}
		} else {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
		}
	}
	if IsImageGenerationModel(modelName) {
		// add to first
		endpointTypes = append([]constant.EndpointType{constant.EndpointTypeImageGeneration}, endpointTypes...)
	}
	return endpointTypes
}
