package commandcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ErrUnauthorized reports a rejected API key.
var ErrUnauthorized = errors.New("command_code: unauthorized")

// Client talks to the Command Code management and Provider APIs.
type Client struct {
	httpClient      *http.Client
	apiBaseURL      string
	providerBaseURL string
}

// NewClient builds a client. Empty base URLs fall back to the public hosts.
func NewClient(httpClient *http.Client, apiBaseURL, providerBaseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	apiBaseURL = strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = DefaultAPIBaseURL
	}
	providerBaseURL = strings.TrimRight(strings.TrimSpace(providerBaseURL), "/")
	if providerBaseURL == "" {
		providerBaseURL = DefaultProviderBaseURL
	}
	return &Client{
		httpClient:      httpClient,
		apiBaseURL:      apiBaseURL,
		providerBaseURL: providerBaseURL,
	}
}

// Whoami is the account view returned by /alpha/whoami.
type Whoami struct {
	User *WhoamiUser `json:"user"`
	Org  *WhoamiOrg  `json:"org"`
}

// WhoamiUser is the personal account identity.
type WhoamiUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	UserName string `json:"userName"`
	Email    string `json:"email"`
}

// WhoamiOrg is the organization identity; personal accounts leave it null.
type WhoamiOrg struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Login string `json:"login"`
}

// ValidateAPIKey checks the key and returns the account it belongs to.
func (c *Client) ValidateAPIKey(ctx context.Context, apiKey string) (*Whoami, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("command_code: api_key is required")
	}
	var whoami Whoami
	if err := c.getJSON(ctx, c.apiBaseURL+WhoamiPath, apiKey, nil, &whoami); err != nil {
		return nil, err
	}
	return &whoami, nil
}

// FetchModels reads the public Provider API model catalog.
func (c *Client) FetchModels(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	var response struct {
		Data []ModelInfo `json:"data"`
	}
	if err := c.getJSON(ctx, c.providerBaseURL+ModelsPath, apiKey, nil, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}

// FetchUsage reads the credits, subscription and usage-summary views.
func (c *Client) FetchUsage(ctx context.Context, apiKey string) (*Usage, error) {
	whoami, err := c.ValidateAPIKey(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if whoami.Org != nil && strings.TrimSpace(whoami.Org.ID) != "" {
		query.Set("orgId", strings.TrimSpace(whoami.Org.ID))
	}

	var credits creditsPayload
	if err := c.getJSON(ctx, c.apiBaseURL+CreditsPath, apiKey, query, &credits); err != nil {
		return nil, err
	}
	if credits.Credits == nil {
		return nil, errors.New("command_code: credits missing")
	}

	usage := &Usage{
		UserID:           whoamiUserID(whoami),
		UserName:         whoamiUserName(whoami),
		OrgID:            whoamiOrgID(whoami),
		OrgName:          whoamiOrgName(whoami),
		MonthlyCredits:   credits.Credits.MonthlyCredits.Float64(),
		PurchasedCredits: credits.Credits.PurchasedCredits.Float64(),
		FreeCredits:      credits.Credits.FreeCredits.Float64(),
		FiveHour:         windowUsage(credits.WindowLimits.FiveHour),
		Weekly:           windowUsage(credits.WindowLimits.Weekly),
		FetchedAt:        time.Now().UTC(),
	}

	var subscription subscriptionPayload
	if err := c.getJSON(ctx, c.apiBaseURL+SubscriptionsPath, apiKey, query, &subscription); err == nil {
		usage.PlanID = strings.TrimSpace(subscription.Data.PlanID)
		usage.PlanName = PlanName(usage.PlanID)
		usage.PeriodStart = strings.TrimSpace(subscription.Data.CurrentPeriodStart)
		usage.PeriodEnd = strings.TrimSpace(subscription.Data.CurrentPeriodEnd)
	} else if IsAuthError(err) {
		return nil, err
	}

	if usage.PeriodStart != "" {
		summaryQuery := url.Values{}
		for key, values := range query {
			summaryQuery[key] = values
		}
		summaryQuery.Set("since", usage.PeriodStart)
		var summary usageSummaryPayload
		if err := c.getJSON(ctx, c.apiBaseURL+UsageSummaryPath, apiKey, summaryQuery, &summary); err == nil {
			usage.MonthlyUsed = summary.TotalCost.Float64()
		} else if IsAuthError(err) {
			return nil, err
		}
	}
	usage.MonthlyCap = usage.MonthlyUsed + usage.MonthlyCredits
	return usage, nil
}

func (c *Client) getJSON(ctx context.Context, rawURL, apiKey string, query url.Values, out any) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("command_code: invalid url: %w", err)
	}
	if len(query) > 0 {
		parsed.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return fmt.Errorf("command_code: build request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", DefaultUserAgent)
	if apiKey = strings.TrimSpace(apiKey); apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("command_code: request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("command_code: read response: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: %s", ErrUnauthorized, truncateErrorBody(body))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("command_code: upstream status %d: %s", response.StatusCode, truncateErrorBody(body))
	}
	if len(body) > MaxResponseBytes {
		return fmt.Errorf("command_code: response exceeds the %d-byte limit", MaxResponseBytes)
	}
	if err := common.Unmarshal(body, out); err != nil {
		return fmt.Errorf("command_code: decode response: %w", err)
	}
	return nil
}

// maxErrorBodyRunes caps how much of a rejected response is echoed into the
// error message: an HTML error page would otherwise fill the log.
const maxErrorBodyRunes = 512

// truncateErrorBody trims an upstream error body to a readable length without
// splitting a multibyte character.
func truncateErrorBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	runes := []rune(text)
	if len(runes) <= maxErrorBodyRunes {
		return text
	}
	return string(runes[:maxErrorBodyRunes]) + "..."
}

// IsAuthError reports a rejected or forbidden API key.
func IsAuthError(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}

func whoamiUserID(whoami *Whoami) string {
	if whoami == nil || whoami.User == nil {
		return ""
	}
	return strings.TrimSpace(whoami.User.ID)
}

func whoamiUserName(whoami *Whoami) string {
	if whoami == nil || whoami.User == nil {
		return ""
	}
	if value := strings.TrimSpace(whoami.User.UserName); value != "" {
		return value
	}
	return strings.TrimSpace(whoami.User.Name)
}

func whoamiOrgID(whoami *Whoami) string {
	if whoami == nil || whoami.Org == nil {
		return ""
	}
	return strings.TrimSpace(whoami.Org.ID)
}

func whoamiOrgName(whoami *Whoami) string {
	if whoami == nil || whoami.Org == nil {
		return ""
	}
	if value := strings.TrimSpace(whoami.Org.Name); value != "" {
		return value
	}
	return strings.TrimSpace(whoami.Org.Login)
}
