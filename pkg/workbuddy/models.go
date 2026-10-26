package workbuddy

// ModelList is the seed catalogue a WorkBuddy channel advertises: the model
// identifiers the upstream serves, extracted from the reference gateway's
// static tables (wb_catalog.py STATIC_INTL_MODELS + STATIC_CN_MODELS).
//
// The five routing aliases the reference exposes (default-model, fast-model,
// balanced-model, primary-model, deep-model) are deliberately absent: they are
// gateway-side switches, not upstream model identifiers. Entries are
// deduplicated across the two realms and keep the reference's order.
//
// The list is only a seed for the channel form. What an account may actually
// call is decided upstream, and the account's own catalogue is read from
// /v3/config at request time.
var ModelList = []string{
	"deepseek-v4.1-flash",
	"gpt-6-astra",
	"hy4-preview-f",
	"hy4-preview",
	"hy3",
	"gpt-5.6-sol",
	"gpt-5.6-terra",
	"gpt-5.6-luna",
	"gpt-5.5",
	"gpt-5.4",
	"grok-4.7",
	"gpt-5.3-codex",
	"gemini-3.5-flash",
	"glm-5.3",
	"glm-5.3-flash",
	"glm-5.2",
	"kimi-k3",
	"kimi-k2.8-preview",
	"kimi-k2.6",
	"hy3-x",
	"hy4-preview-dev",
	"hy4-preview-x",
	"minimax-m2.5",
	"glm-5v-turbo",
	"glm-5.1",
	"glm-5.0-turbo",
	"glm-4.6v",
	"kimi-k3-1",
	"kimi-k2.7",
	"kimi-k2.5",
	"kimi-k2-thinking",
	"minimax-m3",
	"minimax-m2.7",
	"glm-4.6",
	"deepseek-v4-flash",
	"deepseek-v4-pro",
	"deepseek-v3-2-volc",
	"deepseek-v3-1-volc",
	"deepseek-v3-1-lkeap",
	"deepseek-v3-1",
	"deepseek-v3-0324-lkeap",
	"deepseek-r1-0528-lkeap",
	"kimi-k2-instruct-taiji",
	"default-1.1",
	"deepseek-r1-0528",
	"deepseek-v3-0324",
	"default-1.2",
	"hunyuan-2.0-instruct",
	"hunyuan-chat",
}
