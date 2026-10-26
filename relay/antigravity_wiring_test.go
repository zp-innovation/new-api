package relay

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/antigravity"
)

// The adaptor must keep satisfying the channel contract; an upstream interface
// change fails this file at compile time, which is the intended early warning.
var _ channel.Adaptor = (*antigravity.Adaptor)(nil)

// TestAntigravityChannelWiring guards every hop a new channel type must
// traverse. If an upstream refactor drops one of them, this test fails instead
// of silently routing Antigravity traffic to the OpenAI fallback adaptor.
func TestAntigravityChannelWiring(t *testing.T) {
	channelType := constant.ChannelTypeAntigravity

	apiType, ok := common.ChannelType2APIType(channelType)
	if !ok {
		t.Fatalf("ChannelType2APIType(%d) is not registered; the channel would fall back to the OpenAI adaptor", channelType)
	}
	if apiType != constant.APITypeAntigravity {
		t.Fatalf("api type = %d, want %d", apiType, constant.APITypeAntigravity)
	}

	adaptor := GetAdaptor(apiType)
	if adaptor == nil {
		t.Fatalf("GetAdaptor(%d) returned nil", apiType)
	}
	if _, ok := adaptor.(*antigravity.Adaptor); !ok {
		t.Fatalf("GetAdaptor(%d) = %T, want *antigravity.Adaptor", apiType, adaptor)
	}

	if name := constant.GetChannelTypeName(channelType); name == "" || name == "Unknown" {
		t.Fatalf("channel type name = %q, want a registered display name", name)
	}
	if baseURL := constant.GetChannelBaseURL(channelType); !strings.HasPrefix(baseURL, "https://") {
		t.Fatalf("channel base url = %q, want an https default", baseURL)
	}

	for _, model := range []string{"claude-sonnet-4-6", "gemini-3-pro-high"} {
		if endpoints := common.GetEndpointTypesByChannelType(channelType, model); len(endpoints) == 0 {
			t.Fatalf("no endpoint types registered for %s", model)
		}
	}

	// ChannelTypeDummy carries no expression, so Go substitutes the previous
	// expression and it aliases the highest registered channel type. The
	// enumeration loops (controller/model.go) use "<= ChannelTypeDummy", so our
	// type must not exceed it.
	if channelType > constant.ChannelTypeDummy {
		t.Fatalf("channel type %d is above ChannelTypeDummy (%d); the model enumeration would skip it", channelType, constant.ChannelTypeDummy)
	}
}

func TestAntigravityAdaptorAdvertisesModels(t *testing.T) {
	adaptor := &antigravity.Adaptor{}
	if got := adaptor.GetChannelName(); got != "antigravity" {
		t.Fatalf("channel name = %q, want antigravity", got)
	}
	models := adaptor.GetModelList()
	if len(models) == 0 {
		t.Fatal("model list is empty")
	}
	for _, want := range []string{"claude-sonnet-4-6", "gemini-3-pro-high"} {
		found := false
		for _, model := range models {
			if model == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("model list is missing %s", want)
		}
	}
}
