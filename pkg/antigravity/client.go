package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Antigravity Cloud Code Assist endpoints. Management calls prefer the
// production host, relay calls prefer the daily host.
const (
	ProdBaseURL  = "https://cloudcode-pa.googleapis.com"
	DailyBaseURL = "https://daily-cloudcode-pa.googleapis.com"

	defaultHTTPTimeout = 30 * time.Second
	tokenSafetyWindow  = 5 * time.Minute
	onboardAttempts    = 5
	onboardInterval    = 2 * time.Second
	maxErrorBodyRunes  = 512
)

// BaseURLs is the management-endpoint preference order.
var BaseURLs = []string{ProdBaseURL, DailyBaseURL}

// ForwardBaseURLs is the relay preference order.
var ForwardBaseURLs = []string{DailyBaseURL, ProdBaseURL}

// Client talks to the Antigravity OAuth and Cloud Code Assist endpoints.
type Client struct {
	httpClient *http.Client
	urls       *URLAvailability

	tokenURL    string
	userInfoURL string
	baseURLs    []string
	forwardURLs []string

	// onboardInterval is the delay between onboardUser polls.
	onboardInterval time.Duration
}

// NewClient creates a client. A nil http.Client selects a default client with
// a 30 second timeout; callers that need proxy support pass their own.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Client{
		httpClient:      httpClient,
		urls:            NewURLAvailability(URLAvailabilityTTL),
		tokenURL:        TokenURL,
		userInfoURL:     UserInfoURL,
		baseURLs:        BaseURLs,
		forwardURLs:     ForwardBaseURLs,
		onboardInterval: onboardInterval,
	}
}

// ForwardURL returns the preferred relay endpoint for an action. The trailing
// "?alt=sse" form is produced by the caller through the action string.
func (c *Client) ForwardURL(action string) string {
	return c.urls.Available(c.forwardURLs)[0] + "/v1internal:" + action
}

// MarkSuccess promotes a base URL after a successful relay call.
func (c *Client) MarkSuccess(baseURL string) {
	c.urls.MarkSuccess(baseURL)
}

// MarkUnavailable demotes a base URL after a failing relay call.
func (c *Client) MarkUnavailable(baseURL string) {
	c.urls.MarkUnavailable(baseURL)
}

// TokenResponse is a Google OAuth token response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// ExpiresAt converts ExpiresIn into an absolute deadline with a safety window
// so a token is never used in its final minutes.
func (t *TokenResponse) ExpiresAt() time.Time {
	return time.Now().Add(time.Duration(t.ExpiresIn)*time.Second - tokenSafetyWindow)
}

// UserInfo is the Google user profile returned by UserInfoURL.
type UserInfo struct {
	Email   string `json:"email"`
	Name    string `json:"name,omitempty"`
	Picture string `json:"picture,omitempty"`
}

// ExchangeCode trades an authorization code for tokens.
func (c *Client) ExchangeCode(ctx context.Context, code, codeVerifier string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", ClientID)
	form.Set("client_secret", ClientSecret())
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", RedirectURI)
	form.Set("grant_type", "authorization_code")
	form.Set("code_verifier", codeVerifier)
	return c.postTokenForm(ctx, form)
}

// RefreshToken trades a refresh token for a new access token.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, errors.New("refresh token is required")
	}
	form := url.Values{}
	form.Set("client_id", ClientID)
	form.Set("client_secret", ClientSecret())
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")
	return c.postTokenForm(ctx, form)
}

func (c *Client) postTokenForm(ctx context.Context, form url.Values) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request failed with HTTP %d: %s", resp.StatusCode, truncateBody(body))
	}

	var token TokenResponse
	if err := unmarshalJSON(body, &token); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if token.AccessToken == "" {
		return nil, errors.New("token response is missing access_token")
	}
	return &token, nil
}

// GetUserInfo reads the authorized Google account profile.
func (c *Client) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read userinfo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo request failed with HTTP %d: %s", resp.StatusCode, truncateBody(body))
	}

	var info UserInfo
	if err := unmarshalJSON(body, &info); err != nil {
		return nil, fmt.Errorf("decode userinfo response: %w", err)
	}
	return &info, nil
}

// IDEMetadata describes the calling IDE for loadCodeAssist.
type IDEMetadata struct {
	IDEType    string `json:"ideType"`
	IDEVersion string `json:"ideVersion"`
	IDEName    string `json:"ideName"`
}

// LoadCodeAssistRequest is the loadCodeAssist payload.
type LoadCodeAssistRequest struct {
	Metadata IDEMetadata `json:"metadata"`
}

// TierInfo describes an account tier. Upstream has returned both bare strings
// and objects for this field over time.
type TierInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// UnmarshalJSON accepts either a bare tier id or a tier object.
func (t *TierInfo) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		return unmarshalJSON(trimmed, &t.ID)
	}
	type tierAlias TierInfo
	var decoded tierAlias
	if err := unmarshalJSON(trimmed, &decoded); err != nil {
		return err
	}
	*t = TierInfo(decoded)
	return nil
}

// AvailableCredit is one entry of the account credit list.
type AvailableCredit struct {
	CreditType    string   `json:"creditType,omitempty"`
	Amount        *float64 `json:"amount,omitempty"`
	MinimumAmount *float64 `json:"minimumAmount,omitempty"`
}

// LoadCodeAssistResponse describes the account returned by loadCodeAssist.
type LoadCodeAssistResponse struct {
	CloudAICompanionProject json.RawMessage   `json:"cloudaicompanionProject,omitempty"`
	CurrentTier             *TierInfo         `json:"currentTier,omitempty"`
	AllowedTiers            []TierInfo        `json:"allowedTiers,omitempty"`
	AvailableCredits        []AvailableCredit `json:"availableCredits,omitempty"`
	IneligibleTiers         json.RawMessage   `json:"ineligibleTiers,omitempty"`
}

// ProjectID returns the project id carried by the response, which upstream may
// encode as a bare string or as an object with an "id" field.
func (r *LoadCodeAssistResponse) ProjectID() string {
	return extractProjectID(r.CloudAICompanionProject)
}

// TierID returns the current account tier id, or "" when the account has none.
func (r *LoadCodeAssistResponse) TierID() string {
	if r.CurrentTier == nil {
		return ""
	}
	return strings.TrimSpace(r.CurrentTier.ID)
}

// PreferredTier returns the tier that should be used for onboarding, preferring
// the current tier and falling back to the first allowed tier.
func (r *LoadCodeAssistResponse) PreferredTier() string {
	if tier := r.TierID(); tier != "" {
		return tier
	}
	for _, tier := range r.AllowedTiers {
		if id := strings.TrimSpace(tier.ID); id != "" {
			return id
		}
	}
	return ""
}

// LoadCodeAssist reads the account tier and project binding.
func (c *Client) LoadCodeAssist(ctx context.Context, accessToken string) (*LoadCodeAssistResponse, error) {
	payload := LoadCodeAssistRequest{}
	payload.Metadata.IDEType = "ANTIGRAVITY"
	payload.Metadata.IDEVersion = DefaultVersion()
	payload.Metadata.IDEName = "antigravity"

	var out LoadCodeAssistResponse
	if _, err := c.postInternalJSON(ctx, c.baseURLs, "loadCodeAssist", accessToken, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// OnboardIDEMetadata describes the onboarding client.
type OnboardIDEMetadata struct {
	IDEType    string `json:"ideType"`
	Platform   string `json:"platform"`
	PluginType string `json:"pluginType"`
}

// OnboardUserRequest is the onboardUser payload.
type OnboardUserRequest struct {
	TierID   string             `json:"tierId"`
	Metadata OnboardIDEMetadata `json:"metadata"`
}

// OnboardUserResponse is the onboardUser response.
type OnboardUserResponse struct {
	Done     bool `json:"done"`
	Response struct {
		CloudAICompanionProject json.RawMessage `json:"cloudaicompanionProject,omitempty"`
	} `json:"response"`
}

// ProjectID returns the provisioned project id, or "" while onboarding is still
// running.
func (r *OnboardUserResponse) ProjectID() string {
	return extractProjectID(r.Response.CloudAICompanionProject)
}

// OnboardUser provisions the account and returns its project id. Upstream
// answers with done=false while provisioning is in flight, so the call polls
// until the operation settles.
func (c *Client) OnboardUser(ctx context.Context, accessToken, tierID string) (string, error) {
	tierID = strings.TrimSpace(tierID)
	if tierID == "" {
		return "", errors.New("tier id is required to onboard the account")
	}

	payload := OnboardUserRequest{TierID: tierID}
	payload.Metadata.IDEType = "ANTIGRAVITY"
	payload.Metadata.Platform = "PLATFORM_UNSPECIFIED"
	payload.Metadata.PluginType = "GEMINI"

	for attempt := 0; attempt < onboardAttempts; attempt++ {
		var out OnboardUserResponse
		if _, err := c.postInternalJSON(ctx, c.baseURLs, "onboardUser", accessToken, payload, &out); err != nil {
			return "", err
		}
		if out.Done {
			projectID := out.ProjectID()
			if projectID == "" {
				return "", errors.New("onboardUser finished without a project id")
			}
			return projectID, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(c.onboardInterval):
		}
	}
	return "", fmt.Errorf("onboardUser did not finish within %d attempts", onboardAttempts)
}

// ModelQuotaInfo is the per-model quota snapshot.
type ModelQuotaInfo struct {
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
}

// ModelInfo describes one model offered to the account.
type ModelInfo struct {
	DisplayName      string          `json:"displayName,omitempty"`
	QuotaInfo        *ModelQuotaInfo `json:"quotaInfo,omitempty"`
	SupportsImages   *bool           `json:"supportsImages,omitempty"`
	SupportsThinking *bool           `json:"supportsThinking,omitempty"`
	ThinkingBudget   *int            `json:"thinkingBudget,omitempty"`
	Recommended      *bool           `json:"recommended,omitempty"`
	MaxTokens        *int            `json:"maxTokens,omitempty"`
	MaxOutputTokens  *int            `json:"maxOutputTokens,omitempty"`
}

// DeprecatedModelInfo points a retired model id at its replacement.
type DeprecatedModelInfo struct {
	NewModelID string `json:"newModelId"`
}

// FetchAvailableModelsResponse is the fetchAvailableModels response.
type FetchAvailableModelsResponse struct {
	Models             map[string]ModelInfo           `json:"models"`
	DeprecatedModelIDs map[string]DeprecatedModelInfo `json:"deprecatedModelIds,omitempty"`
}

// FetchAvailableModels reads the models and quota available to the account.
func (c *Client) FetchAvailableModels(ctx context.Context, accessToken, projectID string) (*FetchAvailableModelsResponse, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("project id is required to list models")
	}

	payload := struct {
		Project string `json:"project"`
	}{Project: projectID}

	var out FetchAvailableModelsResponse
	if _, err := c.postInternalJSON(ctx, c.baseURLs, "fetchAvailableModels", accessToken, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FetchAvailableModelsFromURL reads the model catalogue from the exact endpoint
// used for generation. It never falls back to another host, because a catalogue
// from a different endpoint can advertise models that the selected relay host
// cannot serve.
func (c *Client) FetchAvailableModelsFromURL(ctx context.Context, baseURL, accessToken, projectID string) (*FetchAvailableModelsResponse, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("base URL is required to list models")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("project id is required to list models")
	}

	payload := struct {
		Project string `json:"project"`
	}{Project: projectID}

	var out FetchAvailableModelsResponse
	if _, err := c.postInternalJSONAtURL(ctx, baseURL, "fetchAvailableModels", accessToken, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// postInternalJSON posts payload to {base}/v1internal:{action}, retrying the
// next candidate base URL when a host looks unhealthy. It returns the raw
// response body for callers that need untyped fields.
func (c *Client) postInternalJSON(ctx context.Context, baseURLs []string, action, accessToken string, payload, out any) ([]byte, error) {
	return c.postInternalJSONWithFallback(ctx, baseURLs, action, accessToken, payload, out, true)
}

// postInternalJSONAtURL is the fixed-endpoint variant used when callers need
// catalogue data from the same host that will serve generation requests.
func (c *Client) postInternalJSONAtURL(ctx context.Context, baseURL, action, accessToken string, payload, out any) ([]byte, error) {
	return c.postInternalJSONWithFallback(ctx, []string{baseURL}, action, accessToken, payload, out, false)
}

func (c *Client) postInternalJSONWithFallback(ctx context.Context, baseURLs []string, action, accessToken string, payload, out any, allowFallback bool) ([]byte, error) {
	body, err := marshalJSON(payload)
	if err != nil {
		return nil, fmt.Errorf("encode %s request: %w", action, err)
	}

	candidates := baseURLs
	httpClient := c.httpClient
	if allowFallback {
		candidates = c.urls.Available(baseURLs)
	} else {
		// A redirect to another host is still a cross-endpoint fallback. Keep
		// fixed-endpoint catalogue requests on the selected host.
		fixedClient := *c.httpClient
		fixedClient.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		httpClient = &fixedClient
	}
	var lastErr error
	for index, baseURL := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1internal:"+action, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build %s request: %w", action, err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", UserAgent())

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%s request failed: %w", action, err)
			if allowFallback {
				c.urls.MarkUnavailable(baseURL)
			}
			if allowFallback && index < len(candidates)-1 {
				continue
			}
			return nil, lastErr
		}
		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %s response: %w", action, readErr)
		}

		if shouldFallbackToNextURL(resp.StatusCode) {
			lastErr = fmt.Errorf("%s failed with HTTP %d: %s", action, resp.StatusCode, truncateBody(respBody))
			if allowFallback {
				c.urls.MarkUnavailable(baseURL)
			}
			if allowFallback && index < len(candidates)-1 {
				continue
			}
			return nil, lastErr
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s failed with HTTP %d: %s", action, resp.StatusCode, truncateBody(respBody))
		}

		if allowFallback {
			c.urls.MarkSuccess(baseURL)
		}
		if out != nil {
			if err := unmarshalJSON(respBody, out); err != nil {
				return nil, fmt.Errorf("decode %s response: %w", action, err)
			}
		}
		return respBody, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("%s has no available base url", action)
	}
	return nil, lastErr
}

// shouldFallbackToNextURL reports whether an upstream status should demote the
// current base URL and retry the next one.
func shouldFallbackToNextURL(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusNotFound:
		return true
	}
	return statusCode >= 500
}

// extractProjectID reads a project id that upstream encodes either as a bare
// string or as an object with an "id" field.
func extractProjectID(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var id string
		if err := unmarshalJSON(trimmed, &id); err != nil {
			return ""
		}
		return strings.TrimSpace(id)
	}
	var holder struct {
		ID string `json:"id"`
	}
	if err := unmarshalJSON(trimmed, &holder); err != nil {
		return ""
	}
	return strings.TrimSpace(holder.ID)
}

// truncateBody bounds upstream error bodies so they stay readable in logs.
func truncateBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	runes := []rune(text)
	if len(runes) <= maxErrorBodyRunes {
		return text
	}
	return string(runes[:maxErrorBodyRunes]) + "..."
}
