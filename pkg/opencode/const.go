package opencode

import "time"

const (
	// CredentialType is the marker stored in a channel key.
	CredentialType = "opencode"

	// ModeZen is the pay-as-you-go OpenCode Zen gateway.
	ModeZen = "zen"
	// ModeGo is the OpenCode Go subscription gateway.
	ModeGo = "go"

	// DefaultServer is the Console origin used by the official client. Device
	// authorization and account discovery live under this origin; inference
	// lives under /zen.
	DefaultServer = "https://opencode.ai/console"

	// ClientID is the public device-flow client used by the official CLI.
	ClientID = "opencode-cli"

	DeviceCodePath    = "/auth/device/code"
	DeviceTokenPath   = "/auth/device/token"
	UserPath          = "/api/user"
	OrganizationsPath = "/api/orgs"

	// DefaultUserAgent is the canonical OpenCode identity. The official
	// inference edge is protected by Cloudflare and rejects generic library
	// user agents.
	DefaultUserAgent = "opencode/1.0.0"

	// LoginTTLSeconds bounds one pending device authorization.
	LoginTTLSeconds = 600
	// DefaultPollIntervalSeconds is used when the upstream omits interval.
	DefaultPollIntervalSeconds = 5
	// SlowDownIncrementSeconds is the RFC 8628 slow_down step.
	SlowDownIncrementSeconds = 5

	// CredentialRefreshWindow is how long before expiry a credential is
	// considered due for refresh.
	CredentialRefreshWindow = 5 * time.Minute
	// DefaultHTTPTimeout bounds one management call.
	DefaultHTTPTimeout = 30 * time.Second
)

const (
	// ProtocolChatCompletions is the OpenAI Chat Completions protocol.
	ProtocolChatCompletions = "chat_completions"
	// ProtocolAnthropic is the Anthropic Messages protocol.
	ProtocolAnthropic = "anthropic"
	// ProtocolResponses is the OpenAI Responses protocol.
	ProtocolResponses = "responses"
)

const (
	// BaseURLZen is the OpenCode Zen inference base.
	BaseURLZen = "https://opencode.ai/zen/v1"
	// BaseURLZenAnthropic is the OpenCode Zen Anthropic base.
	BaseURLZenAnthropic = "https://opencode.ai/zen"
	// BaseURLGo is the OpenCode Go inference base.
	BaseURLGo = "https://opencode.ai/zen/go/v1"
	// BaseURLGoAnthropic is the OpenCode Go Anthropic base.
	BaseURLGoAnthropic = "https://opencode.ai/zen/go"

	ModelsPath = "/models"
	UsagePath  = "/usage"
)

// Auth export is the JSON emitted by `opencode auth export`.
const AuthExportIntegrationID = "opencode"
