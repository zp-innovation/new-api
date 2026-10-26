package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/opencode"
)

// TestOpenCodeChannelWiring pins the registration of channel type 66. The
// channel is wired through the channel/API/endpoint/adaptor tables; a mismatch
// shows up as a channel that saves but never relays.
func TestOpenCodeChannelWiring(t *testing.T) {
	if constant.ChannelTypeOpenCode != 66 {
		t.Fatalf("ChannelTypeOpenCode = %d, want 66", constant.ChannelTypeOpenCode)
	}
	if constant.ChannelTypeOpenCode > constant.ChannelTypeDummy {
		t.Fatalf("channel type %d is above ChannelTypeDummy (%d); the model enumeration would skip it", constant.ChannelTypeOpenCode, constant.ChannelTypeDummy)
	}
	if constant.APITypeDummy != constant.APITypeGeminiWeb+1 {
		t.Fatalf("APITypeDummy = %d, want %d", constant.APITypeDummy, constant.APITypeGeminiWeb+1)
	}
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeOpenCode)
	if !ok || apiType != constant.APITypeOpenCode {
		t.Fatalf("ChannelType2APIType(%d) = (%d, %v), want (%d, true)", constant.ChannelTypeOpenCode, apiType, ok, constant.APITypeOpenCode)
	}

	adaptor := GetAdaptor(constant.APITypeOpenCode)
	if _, ok := adaptor.(*opencode.Adaptor); !ok {
		t.Fatalf("GetAdaptor(APITypeOpenCode) = %T, want *opencode.Adaptor", adaptor)
	}

	if got := constant.GetChannelBaseURL(constant.ChannelTypeOpenCode); got == "" {
		t.Fatal("GetChannelBaseURL(ChannelTypeOpenCode) is empty")
	}
	if got := constant.ChannelTypeNames[constant.ChannelTypeOpenCode]; got != "OpenCode" {
		t.Fatalf("ChannelTypeNames[%d] = %q, want OpenCode", constant.ChannelTypeOpenCode, got)
	}

	endpoints := common.GetEndpointTypesByChannelType(constant.ChannelTypeOpenCode, "gpt-5.6-luna")
	wanted := map[constant.EndpointType]bool{
		constant.EndpointTypeOpenAI:         false,
		constant.EndpointTypeOpenAIResponse: false,
		constant.EndpointTypeAnthropic:      false,
	}
	for _, endpoint := range endpoints {
		if _, ok := wanted[endpoint]; ok {
			wanted[endpoint] = true
		}
		if endpoint == constant.EndpointTypeGemini {
			t.Fatalf("OpenCode must not advertise the Gemini endpoint: %v", endpoints)
		}
		if endpoint == constant.EndpointTypeOpenAIResponseCompact {
			t.Fatalf("OpenCode must not advertise the Responses compact endpoint: %v", endpoints)
		}
	}
	for endpoint, found := range wanted {
		if !found {
			t.Fatalf("endpoint %v is missing from %v", endpoint, endpoints)
		}
	}
}
