package commandcode

import "time"

const (
	// CredentialType is the marker stored in a Command Code channel key.
	CredentialType = "command_code"
	// AuthTypeAPIKey is the only credential kind Command Code issues. The
	// browser handoff and the Studio both end at a long-lived API key, so there
	// is no refresh token to rotate.
	AuthTypeAPIKey = "api_key"

	// DefaultAPIBaseURL is the Command Code management API origin.
	DefaultAPIBaseURL = "https://api.commandcode.ai"
	// DefaultProviderBaseURL is the Provider API base. Chat Completions,
	// Responses, Anthropic Messages and the public model catalog all hang off
	// it.
	DefaultProviderBaseURL = DefaultAPIBaseURL + "/provider/v1"
	// DefaultUserAgent identifies new-api to the management API. The upstream
	// rejects requests that carry a generic library user agent with HTTP 403.
	DefaultUserAgent = "new-api/command-code"

	// OAuthAuthorizeURL is the Studio handoff page the official CLI opens.
	OAuthAuthorizeURL = "https://commandcode.ai/studio/auth/cli"
	// OAuthModeRedirect asks Studio to redirect back to the callback instead of
	// showing a value the operator has to copy.
	OAuthModeRedirect = "redirect"
	// OAuthStateTTL bounds one pending browser login.
	OAuthStateTTL = 5 * time.Minute

	// Protocol names are shared with the relay protocol resolver.
	ProtocolChatCompletions = "chat_completions"
	ProtocolResponses       = "responses"
	ProtocolAnthropic       = "anthropic"

	// Provider API paths.
	ChatCompletionsPath = "/chat/completions"
	ResponsesPath       = "/responses"
	MessagesPath        = "/messages"
	ModelsPath          = "/models"

	// Management paths under DefaultAPIBaseURL.
	WhoamiPath        = "/alpha/whoami"
	CreditsPath       = "/alpha/billing/credits"
	SubscriptionsPath = "/alpha/billing/subscriptions"
	UsageSummaryPath  = "/alpha/usage/summary"

	// DefaultHTTPTimeout bounds one management call.
	DefaultHTTPTimeout = 30 * time.Second
	// MaxResponseBytes caps one management response.
	MaxResponseBytes = 4 << 20
)
