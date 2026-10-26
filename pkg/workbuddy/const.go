package workbuddy

// Upstream paths, copied verbatim from the reference gateway
// (wb_accounts.py:200-210). Note that three billing paths carry no /v2 prefix.
const (
	AuthStatePath            = "/v2/plugin/auth/state"
	AuthTokenPath            = "/v2/plugin/auth/token"
	LoginAccountPath         = "/v2/plugin/login/account"
	RefreshPath              = "/v2/plugin/auth/token/refresh"
	CheckinPath              = "/v2/billing/meter/daily-checkin"
	GetResourcePath          = "/v2/billing/meter/get-user-resource"
	ResourceSummaryPath      = "/billing/meter/get-user-resource-summary"
	ResourceFreePackagesPath = "/billing/meter/get-user-resource-free-packages"
	ResourcePaidPackagesPath = "/billing/meter/get-user-resource-paid-packages"
	CheckinStatusPath        = "/v2/billing/meter/checkin-activity-status"
	EnterpriseUsagePath      = "/billing/meter/get-enterprise-user-usage"
	ChatCompletionsPath      = "/v2/chat/completions"
	ConfigPath               = "/v3/config"
	PersonalModelsPath       = "/v2/enterprises/personal/models"
)

// Login protocol constants (wb_accounts.py:212-213).
const (
	// LoginPending is the code the poll endpoint returns while the user has
	// not finished signing in yet.
	LoginPending = 11217
	// LoginTTLSeconds bounds one authorization session.
	LoginTTLSeconds = 600
)
