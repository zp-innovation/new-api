package antigravity

import (
	"slices"

	agcore "github.com/QuantumNous/new-api/pkg/antigravity"
)

// ChannelName is the adaptor name reported by the channel.
const ChannelName = "antigravity"

// ModelList is the model names the channel advertises. It is derived from the
// protocol package's default mapping so the two cannot drift apart.
var ModelList = slices.Clone(agcore.ModelList)
