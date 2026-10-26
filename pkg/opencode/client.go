package opencode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const maxResponseBytes = 1 << 20

var (
	// ErrAuthorizationPending means the browser has not finished yet.
	ErrAuthorizationPending = errors.New("opencode: authorization pending")
	// ErrSlowDown asks the caller to increase the polling interval.
	ErrSlowDown = errors.New("opencode: slow down")
	// ErrAuthorizationDenied means the user rejected the login.
	ErrAuthorizationDenied = errors.New("opencode: authorization denied")
	// ErrExpiredToken means the device code expired.
	ErrExpiredToken = errors.New("opencode: device code expired")
	// ErrSignedOut means the refresh token can no longer be used.
	ErrSignedOut = errors.New("opencode: signed out")
)

// DeviceAuthorization is one pending device-code login.
type DeviceAuthorization struct {
	DeviceCode              string
	UserCode                string
	VerificationURIComplete string
	ExpiresIn               int
	Interval                int
}

// TokenResponse is a successful device token exchange or refresh.
type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	OrgID        string
}

// User is the OpenCode Console account.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Organization is one Console organization.
type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Client performs the OpenCode Console management calls.
type Client struct {
	httpClient *http.Client
	server     string
	userAgent  string
}

// NewClientWithError builds a client and rejects an invalid server URL.
func NewClientWithError(httpClient *http.Client, server string) (*Client, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	normalized, err := normalizeServer(server)
	if err != nil {
		return nil, err
	}
	return &Client{
		httpClient: httpClient,
		server:     normalized,
		userAgent:  DefaultUserAgent,
	}, nil
}

// Server returns the normalized Console origin.
func (c *Client) Server() string {
	if c == nil {
		return DefaultServer
	}
	return c.server
}

// SetUserAgent overrides the outbound user agent. An empty value keeps the
// canonical OpenCode identity.
func (c *Client) SetUserAgent(value string) {
	if c == nil || strings.TrimSpace(value) == "" {
		return
	}
	c.userAgent = strings.TrimSpace(value)
}

// StartDeviceAuthorization asks Console for a device code.
func (c *Client) StartDeviceAuthorization(ctx context.Context) (*DeviceAuthorization, error) {
	if c == nil {
		return nil, errors.New("opencode: client is nil")
	}
	request := map[string]any{
		"client_id":          ClientID,
		"supports_org_scope": true,
	}
	var response struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := c.doJSON(ctx, http.MethodPost, DeviceCodePath, "", request, &response); err != nil {
		return nil, err
	}
	if strings.TrimSpace(response.DeviceCode) == "" ||
		strings.TrimSpace(response.UserCode) == "" ||
		strings.TrimSpace(response.VerificationURIComplete) == "" {
		return nil, errors.New("opencode: device authorization response is incomplete")
	}
	verification, err := c.normalizeVerificationURL(response.VerificationURIComplete)
	if err != nil {
		return nil, err
	}
	if response.ExpiresIn <= 0 {
		response.ExpiresIn = LoginTTLSeconds
	}
	if response.Interval <= 0 {
		response.Interval = DefaultPollIntervalSeconds
	}
	return &DeviceAuthorization{
		DeviceCode:              response.DeviceCode,
		UserCode:                response.UserCode,
		VerificationURIComplete: verification,
		ExpiresIn:               response.ExpiresIn,
		Interval:                response.Interval,
	}, nil
}

// PollDeviceToken performs one RFC 8628 token poll.
func (c *Client) PollDeviceToken(ctx context.Context, deviceCode string) (*TokenResponse, error) {
	if strings.TrimSpace(deviceCode) == "" {
		return nil, errors.New("opencode: device_code is required")
	}
	request := map[string]any{
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": deviceCode,
		"client_id":   ClientID,
	}
	return c.exchangeToken(ctx, request)
}

// Refresh exchanges a refresh token for a new access token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, errors.New("opencode: refresh_token is required")
	}
	request := map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     ClientID,
	}
	return c.exchangeToken(ctx, request)
}

func (c *Client) exchangeToken(ctx context.Context, request map[string]any) (*TokenResponse, error) {
	var response struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int64  `json:"expires_in"`
		OrgID            string `json:"org_id"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	status, payload, err := c.doRequest(ctx, http.MethodPost, DeviceTokenPath, "", request)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(payload)) > 0 {
		if decodeErr := common.Unmarshal(payload, &response); decodeErr != nil {
			return nil, fmt.Errorf("opencode: decode token response: %w", decodeErr)
		}
	}
	if status < 200 || status >= 300 {
		switch strings.TrimSpace(response.Error) {
		case "authorization_pending":
			return nil, ErrAuthorizationPending
		case "slow_down":
			return nil, ErrSlowDown
		case "access_denied":
			return nil, ErrAuthorizationDenied
		case "expired_token":
			return nil, ErrExpiredToken
		case "invalid_grant":
			return nil, ErrSignedOut
		default:
			return nil, fmt.Errorf("opencode: token exchange failed: HTTP %d", status)
		}
	}
	if response.AccessToken == "" {
		switch strings.TrimSpace(response.Error) {
		case "authorization_pending":
			return nil, ErrAuthorizationPending
		case "slow_down":
			return nil, ErrSlowDown
		case "access_denied":
			return nil, ErrAuthorizationDenied
		case "expired_token":
			return nil, ErrExpiredToken
		case "invalid_grant":
			return nil, ErrSignedOut
		case "":
			return nil, errors.New("opencode: token response carried no access token")
		default:
			return nil, fmt.Errorf("opencode: token exchange failed: %s", firstNonEmpty(response.ErrorDescription, response.Error))
		}
	}
	return &TokenResponse{
		AccessToken:  response.AccessToken,
		RefreshToken: response.RefreshToken,
		ExpiresIn:    response.ExpiresIn,
		OrgID:        response.OrgID,
	}, nil
}

// FetchUser returns the account behind an access token.
func (c *Client) FetchUser(ctx context.Context, accessToken string) (*User, error) {
	var user User
	if err := c.doJSON(ctx, http.MethodGet, UserPath, accessToken, nil, &user); err != nil {
		return nil, err
	}
	if strings.TrimSpace(user.ID) == "" {
		return nil, errors.New("opencode: user response carried no id")
	}
	return &user, nil
}

// FetchOrganizations returns every organization visible to the token.
func (c *Client) FetchOrganizations(ctx context.Context, accessToken string) ([]Organization, error) {
	var organizations []Organization
	if err := c.doJSON(ctx, http.MethodGet, OrganizationsPath, accessToken, nil, &organizations); err != nil {
		return nil, err
	}
	return organizations, nil
}

// SelectOrganization returns the organization named by orgID, or the first one
// when orgID is empty. An explicit orgID that is absent is an error.
func SelectOrganization(orgID string, organizations []Organization) (*Organization, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID != "" {
		for index := range organizations {
			if strings.TrimSpace(organizations[index].ID) == orgID {
				return &organizations[index], nil
			}
		}
		return nil, fmt.Errorf("opencode: organization %q not found", orgID)
	}
	if len(organizations) == 0 {
		return nil, nil
	}
	sorted := append([]Organization(nil), organizations...)
	sort.Slice(sorted, func(i, j int) bool {
		leftName := strings.TrimSpace(sorted[i].Name)
		rightName := strings.TrimSpace(sorted[j].Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return strings.TrimSpace(sorted[i].ID) < strings.TrimSpace(sorted[j].ID)
	})
	return &sorted[0], nil
}

// BuildCredential completes a successful device login with account metadata.
func (c *Client) BuildCredential(ctx context.Context, token *TokenResponse, mode string) (*Credential, error) {
	if token == nil {
		return nil, errors.New("opencode: token is nil")
	}
	user, err := c.FetchUser(ctx, token.AccessToken)
	if err != nil {
		return nil, err
	}
	organizations, err := c.FetchOrganizations(ctx, token.AccessToken)
	if err != nil {
		return nil, err
	}
	organization, err := SelectOrganization(token.OrgID, organizations)
	if err != nil {
		return nil, err
	}
	credential := &Credential{
		Type:      CredentialType,
		Mode:      NormalizeMode(mode),
		Server:    c.server,
		AccountID: strings.TrimSpace(user.ID),
		Email:     strings.TrimSpace(user.Email),
	}
	credential = credential.WithToken(token, time.Now())
	if organization != nil {
		credential.OrgID = strings.TrimSpace(organization.ID)
		credential.OrgName = strings.TrimSpace(organization.Name)
	}
	if err := credential.ValidateForStorage(); err != nil {
		return nil, err
	}
	return credential, nil
}

func (c *Client) doJSON(ctx context.Context, method, path, accessToken string, body any, out any) error {
	status, payload, err := c.doRequest(ctx, method, path, accessToken, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("opencode: upstream HTTP %d", status)
	}
	if out == nil || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := common.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("opencode: decode response: %w", err)
	}
	return nil
}

func (c *Client) doRequest(ctx context.Context, method, path, accessToken string, body any) (int, []byte, error) {
	return c.doAbsoluteRequest(ctx, method, c.server+path, accessToken, body)
}

func (c *Client) doAbsoluteRequest(ctx context.Context, method, rawURL, accessToken string, body any) (int, []byte, error) {
	if c == nil {
		return 0, nil, errors.New("opencode: client is nil")
	}
	var reader io.Reader
	if body != nil {
		payload, err := common.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("opencode: encode request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken = strings.TrimSpace(accessToken); accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("opencode: request failed: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("opencode: read response: %w", err)
	}
	if len(payload) > maxResponseBytes {
		return response.StatusCode, nil, errors.New("opencode: response exceeds 1 MiB")
	}
	return response.StatusCode, payload, nil
}

func (c *Client) getAbsoluteJSON(ctx context.Context, rawURL, accessToken string, out any) error {
	status, payload, err := c.doAbsoluteRequest(ctx, http.MethodGet, rawURL, accessToken, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("opencode: upstream HTTP %d", status)
	}
	if out == nil || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := common.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("opencode: decode response: %w", err)
	}
	return nil
}

func (c *Client) normalizeVerificationURL(raw string) (string, error) {
	base, err := url.Parse(c.server + "/")
	if err != nil {
		return "", fmt.Errorf("opencode: invalid server URL: %w", err)
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("opencode: invalid device verification URL")
	}
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", errors.New("opencode: device verification URL must use HTTP(S)")
	}
	if resolved.User != nil || resolved.Hostname() == "" {
		return "", errors.New("opencode: invalid device verification URL")
	}
	if !sameHost(base, resolved) {
		return "", errors.New("opencode: device verification URL points at another host")
	}
	return resolved.String(), nil
}

func normalizeServer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultServer, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return "", errors.New("opencode: invalid server URL")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return "", errors.New("opencode: server URL must use HTTPS")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sameHost(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Hostname(), right.Hostname()) && effectivePort(left) == effectivePort(right)
}

func effectivePort(value *url.URL) string {
	if value == nil {
		return ""
	}
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
