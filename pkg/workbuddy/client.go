package workbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout bounds one management call.
const DefaultTimeout = 30 * time.Second

// APIError is an upstream business error: either the HTTP call failed or the
// payload carried a non-zero code. HTTPStatus keeps the two apart, because a
// business code can itself be 400 while the transport answered 200.
type APIError struct {
	Code       int
	HTTPStatus int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("workbuddy: upstream error code=%d message=%s", e.Code, e.Message)
}

// envelope is the response shape of every management endpoint
// ({"code":0,"msg":"","data":{...}}).
type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// Client performs the WorkBuddy management calls (login, refresh, catalog,
// quota) for one realm. Relay traffic does not go through it: the relay adaptor
// builds requests straight from the stored credential.
type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	Realm      Realm
	Product    Product
	Timeout    time.Duration
	// BillingBaseURL overrides the realm's billing upstream. The reference
	// gateway always uses the realm's own host; the seam exists so tests and
	// self-hosted exits can point the meter calls elsewhere.
	BillingBaseURL string
	// WebBaseURL overrides the web console host the growth centre and the
	// web-channel conversations live on. Empty resolves by realm.
	WebBaseURL string
}

// NewClient returns a client for one realm. An empty baseURL resolves to the
// realm's chat upstream (wb_accounts.py:173-198) and an empty product to the
// desktop identity.
func NewClient(httpClient *http.Client, baseURL string, realm Realm, product Product) *Client {
	realm = NormalizeRealm(string(realm))
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = realm.Config().ChatUpstream
	}
	return &Client{
		HTTPClient: httpClient,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Realm:      realm,
		Product:    NormalizeProduct(string(product)),
		Timeout:    DefaultTimeout,
	}
}

// managementHeaders returns the header set the login/refresh endpoints accept:
// these are not the chat identity headers and carry no Authorization at all
// (wb_accounts.py:3039-3046, :1218-1231).
func (c *Client) managementHeaders() map[string]string {
	cfg := c.Realm.Config()
	return map[string]string{
		"Content-Type":     "application/json",
		"Accept":           "application/json, text/plain, */*",
		"X-Requested-With": "XMLHttpRequest",
		"User-Agent":       cfg.BillingUA,
		"Origin":           cfg.Origin,
		"Referer":          cfg.Origin + "/",
	}
}

// call performs one JSON call against the client's base URL and decodes the
// envelope's data into out. A non-zero envelope code becomes an *APIError.
func (c *Client) call(ctx context.Context, method, path string, headers map[string]string, body any, out any) error {
	payload, err := c.callRaw(ctx, method, path, headers, body)
	if err != nil {
		return err
	}
	var decoded envelope
	if err := common.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("workbuddy: decode %s %s response: %w", method, path, err)
	}
	if out == nil || len(decoded.Data) == 0 {
		return nil
	}
	return common.Unmarshal(decoded.Data, out)
}

// postStream performs a POST and drains the response without parsing it. The
// chat endpoint answers with SSE rather than with the management envelope, so
// it cannot go through callRawURL.
func (c *Client) postStream(ctx context.Context, url string, headers map[string]string, body any) error {
	encoded, err := common.Marshal(body)
	if err != nil {
		return fmt.Errorf("workbuddy: marshal request body: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("workbuddy: build request: %w", err)
	}
	for key, value := range headers {
		if value != "" {
			request.Header.Set(key, value)
		}
	}
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("workbuddy: POST %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, maxChatErrorBodyBytes+1))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return &chatRejection{StatusCode: response.StatusCode, Body: string(payload)}
	}
	return nil
}

// webBase resolves the web console host (the growth centre and the web-channel
// conversations), which differs from both the chat and the billing upstream.
func (c *Client) webBase() string {
	if c.WebBaseURL != "" {
		return strings.TrimRight(c.WebBaseURL, "/")
	}
	if c.Realm == RealmCN {
		return "https://www.workbuddy.cn"
	}
	return "https://www.workbuddy.ai"
}

// getJSON performs one GET and decodes the envelope's data into out. The growth
// endpoints (tasks, streak, heatmap, travel status, lottery summary) are reads
// while their siblings are POSTs, so both verbs are needed.
func (c *Client) getJSON(ctx context.Context, url string, headers map[string]string, out any) error {
	payload, err := c.callRawURL(ctx, http.MethodGet, url, headers, nil)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	var decoded envelope
	if err := common.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("workbuddy: decode %s response: %w", url, err)
	}
	if len(decoded.Data) == 0 {
		return nil
	}
	return common.Unmarshal(decoded.Data, out)
}

// callRaw performs one JSON call against the client's base URL and returns the
// raw response body.
func (c *Client) callRaw(ctx context.Context, method, path string, headers map[string]string, body any) ([]byte, error) {
	return c.callRawURL(ctx, method, c.BaseURL+path, headers, body)
}

// callRawURL performs one JSON call against an absolute URL and returns the raw
// body. The envelope's code is checked here; the body is handed back because
// some endpoints carry their payload both at the top level and under "data"
// (/v3/config), so a caller cannot rely on envelope.Data alone.
func (c *Client) callRawURL(ctx context.Context, method, url string, headers map[string]string, body any) ([]byte, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		encoded, err := common.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("workbuddy: marshal request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("workbuddy: build request: %w", err)
	}
	for key, value := range headers {
		if value != "" {
			request.Header.Set(key, value)
		}
	}

	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("workbuddy: %s %s: %w", method, url, err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("workbuddy: read %s %s response: %w", method, url, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		// The upstream reports business failures with a non-2xx status and the
		// real code inside the envelope: a duplicate daily sign-in answers HTTP
		// 400 with code 10001, which the caller has to be able to recognise as
		// "already done" rather than as a failure.
		var decoded envelope
		if err := common.Unmarshal(payload, &decoded); err == nil && decoded.Code != 0 {
			message := decoded.Msg
			if strings.TrimSpace(message) == "" {
				message = truncateBody(payload)
			}
			return nil, &APIError{Code: decoded.Code, HTTPStatus: response.StatusCode, Message: message}
		}
		return nil, &APIError{Code: response.StatusCode, HTTPStatus: response.StatusCode, Message: truncateBody(payload)}
	}

	var decoded envelope
	if err := common.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("workbuddy: decode %s %s response: %w", method, url, err)
	}
	if decoded.Code != 0 {
		return nil, &APIError{Code: decoded.Code, HTTPStatus: response.StatusCode, Message: decoded.Msg}
	}
	return payload, nil
}

func truncateBody(payload []byte) string {
	const limit = 512
	text := strings.TrimSpace(string(payload))
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return text
}
