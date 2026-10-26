package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/commandcode"
)

// TestCommandCodeChannelWiring pins the registration of channel type 67. The
// channel is wired through the channel/API/endpoint/adaptor tables; a mismatch
// shows up as a channel that saves but never relays.
func TestCommandCodeChannelWiring(t *testing.T) {
	if constant.ChannelTypeCommandCode != 67 {
		t.Fatalf("ChannelTypeCommandCode = %d, want 67", constant.ChannelTypeCommandCode)
	}
	if constant.ChannelTypeCommandCode > constant.ChannelTypeDummy {
		t.Fatalf("channel type %d is above ChannelTypeDummy (%d); the model enumeration would skip it", constant.ChannelTypeCommandCode, constant.ChannelTypeDummy)
	}
	if constant.APITypeDummy != constant.APITypeGeminiWeb+1 {
		t.Fatalf("APITypeDummy = %d, want %d", constant.APITypeDummy, constant.APITypeGeminiWeb+1)
	}
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeCommandCode)
	if !ok || apiType != constant.APITypeCommandCode {
		t.Fatalf("ChannelType2APIType(%d) = (%d, %v), want (%d, true)", constant.ChannelTypeCommandCode, apiType, ok, constant.APITypeCommandCode)
	}

	adaptor := GetAdaptor(constant.APITypeCommandCode)
	if _, ok := adaptor.(*commandcode.Adaptor); !ok {
		t.Fatalf("GetAdaptor(APITypeCommandCode) = %T, want *commandcode.Adaptor", adaptor)
	}

	if got := constant.GetChannelBaseURL(constant.ChannelTypeCommandCode); got == "" {
		t.Fatal("GetChannelBaseURL(ChannelTypeCommandCode) is empty")
	}
	if got := constant.ChannelTypeNames[constant.ChannelTypeCommandCode]; got != "Command Code" {
		t.Fatalf("ChannelTypeNames[%d] = %q, want Command Code", constant.ChannelTypeCommandCode, got)
	}

	endpoints := common.GetEndpointTypesByChannelType(constant.ChannelTypeCommandCode, "gpt-5.6-luna")
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
			t.Fatalf("Command Code must not advertise the Gemini endpoint: %v", endpoints)
		}
		if endpoint == constant.EndpointTypeOpenAIResponseCompact {
			t.Fatalf("Command Code must not advertise the Responses compact endpoint: %v", endpoints)
		}
	}
	for endpoint, found := range wanted {
		if !found {
			t.Fatalf("endpoint %v is missing from %v", endpoint, endpoints)
		}
	}
}
