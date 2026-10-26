package workbuddy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// The web channel drives a cloud agent conversation over ACP (Agent Client
// Protocol) on a streamable-HTTP transport: one SSE stream receives the
// notifications while plain POSTs send the JSON-RPC requests. Creating a
// conversation only queues it -- the agent waits for a client to attach to the
// sandbox and request the turn, so a bare create never completes and never
// counts (wb_webagent.py:1-21).
const (
	conversationsPath   = "/console/as/conversations/"
	acpProtocolVersion  = 1
	acpConnectionHeader = "Acp-Connection-Id"
	acpPollInterval     = 3 * time.Second
)

// WebTurnResult reports one web-channel agent conversation.
type WebTurnResult struct {
	OK           bool
	Status       string
	Events       int
	Chunks       int
	ElapsedMS    int
	Error        string
	Conversation string
}

// RunWebConversationTurn creates a web conversation, attaches to its sandbox and
// drives one turn to completion. It is the leg the upstream actually credits for
// the international daily activity (wb_accounts.py:1317-1387).
func (c *Client) RunWebConversationTurn(ctx context.Context, identity Identity, prompt string) *WebTurnResult {
	started := time.Now()
	result := &WebTurnResult{}
	ctx, cancel := context.WithTimeout(ctx, WebTurnTimeout)
	defer cancel()
	defer func() { result.ElapsedMS = int(time.Since(started).Milliseconds()) }()

	headers := c.webHeaders(identity)
	base := c.webBase() + conversationsPath

	var created struct {
		ID string `json:"id"`
	}
	body := map[string]any{
		"prompt":             prompt,
		"model":              dailyChatModel,
		"conversationOrigin": "workbuddy-app",
		"plugins":            []map[string]any{{"name": "weixinpay", "marketplace": "codebuddy-builtin"}},
	}
	if err := c.postJSON(ctx, base, headers, body, &created); err != nil {
		result.Error = "create conversation: " + err.Error()
		return result
	}
	result.Conversation = created.ID
	if created.ID == "" {
		result.Error = "create conversation: upstream returned no id"
		return result
	}

	var session struct {
		Link           string `json:"link"`
		Endpoint       string `json:"endpoint"`
		Token          string `json:"token"`
		SessionID      string `json:"sessionId"`
		SessionIDSnake string `json:"session_id"`
		Cwd            string `json:"cwd"`
	}
	if err := c.getJSON(ctx, base+url.PathEscape(created.ID)+"/session", headers, &session); err != nil {
		result.Error = "query sandbox session: " + err.Error()
		return result
	}
	link := session.Link
	if link == "" {
		link = session.Endpoint
	}
	if link == "" || session.Token == "" {
		result.Error = "sandbox is not ready (no link/token)"
		return result
	}
	sessionID := session.SessionID
	if sessionID == "" {
		sessionID = session.SessionIDSnake
	}
	if sessionID == "" {
		sessionID = created.ID
	}
	cwd := session.Cwd
	if cwd == "" {
		cwd = "/workspace"
	}

	channel := &acpChannel{client: c, link: link, token: session.Token}
	defer channel.close()
	if err := channel.open(ctx); err != nil {
		result.Error = "attach to the sandbox: " + err.Error()
		return result
	}
	if err := channel.request(ctx, "initialize", map[string]any{
		"protocolVersion": acpProtocolVersion,
		// The daily chat needs no filesystem and no terminal.
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
	}, 1); err != nil {
		result.Error = "acp initialize: " + err.Error()
		return result
	}
	if err := channel.request(ctx, "session/load", map[string]any{
		"sessionId": sessionID, "cwd": cwd, "mcpServers": []any{},
	}, 2); err != nil {
		result.Error = "acp session/load: " + err.Error()
		return result
	}
	if err := channel.request(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []map[string]any{{"type": "text", "text": prompt}},
	}, 3); err != nil {
		result.Error = "acp session/prompt: " + err.Error()
		return result
	}

	status := ""
	for {
		status = c.conversationStatus(ctx, base, headers, created.ID)
		if status == "completed" {
			break
		}
		if status == "failed" || status == "error" {
			result.Status = status
			result.Error = "conversation status=" + status
			return result
		}
		select {
		case <-ctx.Done():
			result.Status = status
			result.Error = fmt.Sprintf("conversation did not finish in %s (status=%s)", WebTurnTimeout, statusOrUnknown(status))
			return result
		case <-time.After(acpPollInterval):
		}
	}
	result.Status = status
	result.OK = true
	result.Events = int(channel.events.Load())
	result.Chunks = int(channel.chunks.Load())
	return result
}

func statusOrUnknown(status string) string {
	if status == "" {
		return "unknown"
	}
	return status
}

// conversationStatus reads one conversation's status; a failed read is not fatal
// because the poll loop retries.
func (c *Client) conversationStatus(ctx context.Context, base string, headers map[string]string, id string) string {
	var data struct {
		Status string `json:"status"`
	}
	if err := c.getJSON(ctx, base+url.PathEscape(id), headers, &data); err != nil {
		return ""
	}
	return data.Status
}

// acpChannel is the ACP streamable-HTTP transport: one SSE stream in, POSTs out.
type acpChannel struct {
	client       *Client
	link         string
	token        string
	connectionID string
	stream       io.ReadCloser
	cancelStream context.CancelFunc
	events       atomic.Int64
	chunks       atomic.Int64
}

// httpClient returns the transport client without its management timeout: the
// ACP stream stays open for the whole turn, so the request context is the real
// bound and a 30 second client timeout would cut the stream off mid-turn.
func (a *acpChannel) httpClient() *http.Client {
	base := a.client.HTTPClient
	if base == nil {
		base = http.DefaultClient
	}
	clone := *base
	clone.Timeout = 0
	return &clone
}

// open attaches the SSE stream and captures the connection id the POSTs have to
// carry (wb_webagent.py:106-140).
func (a *acpChannel) open(ctx context.Context) error {
	streamCtx, cancel := context.WithCancel(ctx)
	request, err := http.NewRequestWithContext(streamCtx, http.MethodGet, a.link, nil)
	if err != nil {
		cancel()
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Authorization", "Bearer "+a.token)
	request.Header.Set("User-Agent", webUserAgent)
	response, err := a.httpClient().Do(request)
	if err != nil {
		cancel()
		return err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		_ = response.Body.Close()
		cancel()
		return fmt.Errorf("stream answered HTTP %d", response.StatusCode)
	}
	a.connectionID = response.Header.Get(acpConnectionHeader)
	if a.connectionID == "" {
		_ = response.Body.Close()
		cancel()
		return errors.New("stream carried no " + acpConnectionHeader)
	}
	a.stream = response.Body
	a.cancelStream = cancel
	go a.readStream(response.Body)
	return nil
}

// readStream counts the session updates the server pushes back.
func (a *acpChannel) readStream(body io.Reader) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var message struct {
			Method string `json:"method"`
			Params struct {
				Update struct {
					SessionUpdate string `json:"sessionUpdate"`
				} `json:"update"`
			} `json:"params"`
		}
		if err := common.Unmarshal([]byte(payload), &message); err != nil {
			continue
		}
		if message.Method != "session/update" {
			continue
		}
		a.events.Add(1)
		if message.Params.Update.SessionUpdate == "agent_message_chunk" {
			a.chunks.Add(1)
		}
	}
}

// request posts one JSON-RPC request to the sandbox. The transport answers 200
// or 202 for accepted requests (wb_webagent.py:180-205).
func (a *acpChannel) request(ctx context.Context, method string, params any, id int) error {
	payload, err := common.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.link, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set(acpConnectionHeader, a.connectionID)
	request.Header.Set("Authorization", "Bearer "+a.token)
	request.Header.Set("User-Agent", webUserAgent)
	response, err := a.httpClient().Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("%s answered HTTP %d", method, response.StatusCode)
	}
	return nil
}

func (a *acpChannel) close() {
	if a.cancelStream != nil {
		a.cancelStream()
	}
	if a.stream != nil {
		_ = a.stream.Close()
	}
}
