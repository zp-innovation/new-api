package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/workbuddy"
)

// TestWorkBuddyChannelWiring pins the registration of channel type 65. The
// channel is wired through five tables that must agree; a mismatch shows up as
// a channel that saves but never relays.
func TestWorkBuddyChannelWiring(t *testing.T) {
	if constant.ChannelTypeWorkBuddy != 65 {
		t.Fatalf("ChannelTypeWorkBuddy = %d, want 65", constant.ChannelTypeWorkBuddy)
	}
	// ChannelTypeDummy carries no expression, so Go substitutes the previous
	// expression: it is an upper-bound sentinel, not a distinct type. The model
	// enumeration loops (controller/model.go) use "<= ChannelTypeDummy", so the
	// new type must not sit above it.
	if constant.ChannelTypeWorkBuddy > constant.ChannelTypeDummy {
		t.Fatalf("channel type %d is above ChannelTypeDummy (%d); the model enumeration would skip it", constant.ChannelTypeWorkBuddy, constant.ChannelTypeDummy)
	}
	if constant.APITypeWorkBuddy >= constant.APITypeDummy {
		t.Fatalf("APITypeWorkBuddy = %d must stay below APITypeDummy = %d", constant.APITypeWorkBuddy, constant.APITypeDummy)
	}
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeWorkBuddy)
	if !ok || apiType != constant.APITypeWorkBuddy {
		t.Fatalf("ChannelType2APIType(%d) = (%d, %v), want (%d, true)", constant.ChannelTypeWorkBuddy, apiType, ok, constant.APITypeWorkBuddy)
	}

	adaptor := GetAdaptor(constant.APITypeWorkBuddy)
	if _, ok := adaptor.(*workbuddy.Adaptor); !ok {
		t.Fatalf("GetAdaptor(APITypeWorkBuddy) = %T, want *workbuddy.Adaptor", adaptor)
	}

	if got := constant.GetChannelBaseURL(constant.ChannelTypeWorkBuddy); got == "" {
		t.Fatal("GetChannelBaseURL(ChannelTypeWorkBuddy) is empty")
	}
	if got := constant.ChannelTypeNames[constant.ChannelTypeWorkBuddy]; got != "WorkBuddy" {
		t.Fatalf("ChannelTypeNames[%d] = %q, want WorkBuddy", constant.ChannelTypeWorkBuddy, got)
	}

	endpoints := common.GetEndpointTypesByChannelType(constant.ChannelTypeWorkBuddy, "deepseek-v4.1-flash")
	wanted := map[constant.EndpointType]bool{
		constant.EndpointTypeOpenAI:         false,
		constant.EndpointTypeOpenAIResponse: false,
		constant.EndpointTypeAnthropic:      false,
		constant.EndpointTypeGemini:         false,
	}
	for _, endpoint := range endpoints {
		if _, ok := wanted[endpoint]; ok {
			wanted[endpoint] = true
		}
		if endpoint == constant.EndpointTypeOpenAIResponseCompact {
			t.Fatalf("WorkBuddy must not advertise the Responses compact endpoint: %v", endpoints)
		}
	}
	for endpoint, found := range wanted {
		if !found {
			t.Fatalf("endpoint %v is missing from %v", endpoint, endpoints)
		}
	}
}
