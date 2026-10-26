package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Live smoke test against the real Google / Cloud Code Assist endpoints.
//
// It is skipped unless ANTIGRAVITY_SMOKE is set, so normal test runs never
// touch the network. The flow needs a human for the browser login, so it runs
// in two phases:
//
//	phase 1  ANTIGRAVITY_SMOKE=1 go test ./pkg/antigravity/ -run TestAntigravitySmoke -v
//	         prints the consent URL and stores the PKCE verifier under $TMPDIR
//
//	phase 2  ANTIGRAVITY_SMOKE=1 ANTIGRAVITY_SMOKE_CODE=<code from the address bar> \
//	         go test ./pkg/antigravity/ -run TestAntigravitySmoke -v
//	         exchanges the code and reports the account and quota
const (
	smokeStateFile = "antigravity-smoke-session.json"
	smokeTimeout   = 60 * time.Second
)

type smokeSession struct {
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
	CreatedAt    string `json:"created_at"`
}

func TestAntigravitySmoke(t *testing.T) {
	if os.Getenv("ANTIGRAVITY_SMOKE") == "" {
		t.Skip("set ANTIGRAVITY_SMOKE=1 to run the live Antigravity smoke test")
	}

	rawInput := os.Getenv("ANTIGRAVITY_SMOKE_CODE")
	code := normalizeSmokeCode(rawInput)
	sessionPath := smokeSessionPath(rawInput)

	if code == "" {
		writeSmokeSession(t, sessionPath)
		return
	}
	runSmokeExchange(t, sessionPath, code)
}

// smokeSessionPath keeps one session per state, so generating a newer
// authorization link never invalidates an older one that is still in flight.
func smokeSessionPath(rawInput string) string {
	if parsed, err := url.Parse(strings.TrimSpace(rawInput)); err == nil {
		if state := parsed.Query().Get("state"); state != "" {
			keyed := filepath.Join(os.TempDir(), "antigravity-smoke-"+state+".json")
			if _, err := os.Stat(keyed); err == nil {
				return keyed
			}
		}
	}
	return filepath.Join(os.TempDir(), smokeStateFile)
}

func writeSmokeSession(t *testing.T, sessionPath string) {
	t.Helper()

	state, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier: %v", err)
	}

	payload, err := json.Marshal(smokeSession{
		State:        state,
		CodeVerifier: verifier,
		CreatedAt:    time.Now().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	sessionPath = filepath.Join(os.TempDir(), "antigravity-smoke-"+state+".json")
	if err := os.WriteFile(sessionPath, payload, 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	t.Logf("client_id   : %s", ClientID)
	t.Logf("redirect_uri: %s", RedirectURI)
	t.Logf("state       : %s", state)
	t.Logf("session file: %s", sessionPath)
	t.Logf("")
	authURL := BuildAuthorizationURL(state, GenerateCodeChallenge(verifier))
	if hint := strings.TrimSpace(os.Getenv("ANTIGRAVITY_SMOKE_LOGIN_HINT")); hint != "" {
		// login_hint makes Google pre-select the intended account, which matters
		// when the browser is already signed in with a different one.
		authURL += "&login_hint=" + url.QueryEscape(hint)
		t.Logf("login_hint  : %s", hint)
	}
	t.Logf("OPEN THIS URL AND SIGN IN:")
	t.Logf("%s", authURL)
	t.Logf("")
	t.Logf("Then re-run with ANTIGRAVITY_SMOKE_CODE=<code copied from the address bar>")
}

func runSmokeExchange(t *testing.T, sessionPath, code string) {
	t.Helper()

	raw, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session %s: %v (run phase 1 first)", sessionPath, err)
	}
	var session smokeSession
	if err := json.Unmarshal(raw, &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), smokeTimeout)
	defer cancel()

	client := NewClient(nil)

	token, err := client.ExchangeCode(ctx, code, session.CodeVerifier)
	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}
	t.Logf("STEP 1 exchange-code : ok (access_token=%d chars, refresh_token=%v, expires_in=%d)",
		len(token.AccessToken), token.RefreshToken != "", token.ExpiresIn)

	if info, err := client.GetUserInfo(ctx, token.AccessToken); err != nil {
		t.Errorf("STEP 2 userinfo      : FAILED: %v", err)
	} else {
		t.Logf("STEP 2 userinfo      : ok (email=%s)", info.Email)
	}

	assist, err := client.LoadCodeAssist(ctx, token.AccessToken)
	if err != nil {
		t.Fatalf("STEP 3 loadCodeAssist failed: %v", err)
	}
	t.Logf("STEP 3 loadCodeAssist: ok (tier=%s plan=%s project=%q allowed_tiers=%d)",
		assist.TierID(), TierIDToPlanType(assist.TierID()), assist.ProjectID(), len(assist.AllowedTiers))
	// Dump the whole response: currentTier / allowedTiers / ineligibleTiers are
	// what decide whether a paid tier can be activated via onboardUser.
	if rawAssist, err := json.Marshal(assist); err == nil {
		t.Logf("STEP 3 raw response : %s", string(rawAssist))
	}

	projectID := assist.ProjectID()
	if projectID == "" {
		projectID, err = client.OnboardUser(ctx, token.AccessToken, assist.PreferredTier())
		if err != nil {
			t.Fatalf("STEP 4 onboardUser   failed: %v", err)
		}
		t.Logf("STEP 4 onboardUser   : ok (provisioned project=%s)", projectID)
	} else {
		t.Logf("STEP 4 onboardUser   : skipped (account already has a project)")
	}

	models, err := client.FetchAvailableModels(ctx, token.AccessToken, projectID)
	if err != nil {
		t.Fatalf("STEP 5 fetchAvailableModels failed: %v", err)
	}
	rows := QuotaFromModels(models)
	t.Logf("STEP 5 quota         : ok (%d models)", len(rows))
	for i, row := range rows {
		if i >= 10 {
			t.Logf("        ... %d more", len(rows)-i)
			break
		}
		t.Logf("        %-32s remaining=%.2f reset=%s", row.Model, row.RemainingFraction, row.ResetTime)
	}

	refreshed, err := client.RefreshToken(ctx, token.RefreshToken)
	if err != nil {
		t.Errorf("STEP 6 refresh-token : FAILED: %v", err)
	} else {
		t.Logf("STEP 6 refresh-token : ok (new access_token=%d chars, rotated=%v)",
			len(refreshed.AccessToken), refreshed.RefreshToken != "" && refreshed.RefreshToken != token.RefreshToken)
	}

	credential := Credential{
		Type:         "antigravity",
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt().Format(time.RFC3339),
		LastRefresh:  time.Now().Format(time.RFC3339),
		ProjectID:    projectID,
		PlanType:     TierIDToPlanType(assist.TierID()),
	}
	if parsed, err := ParseCredential(mustJSON(t, credential)); err == nil {
		t.Logf("STEP 7 credential    : ok (round-trips through ParseCredential, project=%s)", parsed.ProjectID)
	} else {
		t.Errorf("STEP 7 credential    : FAILED: %v", err)
	}

	runSmokeRelay(t, ctx, token.AccessToken, projectID)

	// Optional tier activation. This writes to the upstream account, so it only
	// runs when ANTIGRAVITY_SMOKE_ACTIVATE_TIER names a tier the operator has
	// explicitly agreed to activate.
	if tier := strings.TrimSpace(os.Getenv("ANTIGRAVITY_SMOKE_ACTIVATE_TIER")); tier != "" {
		activateSmokeTier(t, ctx, client, token.AccessToken, tier)
	}

	_ = os.Remove(sessionPath)
}

// activateSmokeTier provisions (activates) a tier for the account and then
// re-reads loadCodeAssist to report whether currentTier actually changed.
func activateSmokeTier(t *testing.T, ctx context.Context, client *Client, accessToken, tier string) {
	t.Helper()

	activatedProject, err := client.OnboardUser(ctx, accessToken, tier)
	if err != nil {
		t.Logf("STEP 10 activate %q : FAILED: %v", tier, err)
		return
	}
	t.Logf("STEP 10 activate %q : ok (returned project=%s)", tier, activatedProject)

	after, err := client.LoadCodeAssist(ctx, accessToken)
	if err != nil {
		t.Logf("STEP 11 re-read tier: FAILED: %v", err)
		return
	}
	if raw, err := json.Marshal(after); err == nil {
		t.Logf("STEP 11 re-read tier: currentTier=%s raw=%s", after.TierID(), string(raw))
	}
	if after.TierID() == tier {
		t.Logf("STEP 11 result      : ACTIVATED (currentTier is now %q)", tier)
	} else {
		t.Logf("STEP 11 result      : NOT ACTIVATED (currentTier is still %q)", after.TierID())
	}
}

// runSmokeRelay exercises the real relay path: a Gemini generateContent payload
// wrapped in the Antigravity envelope, sent to the daily relay host, then
// unwrapped exactly like the channel adaptor does. Both the buffered and the
// streaming form are covered.
func runSmokeRelay(t *testing.T, ctx context.Context, accessToken, projectID string) {
	t.Helper()

	inner := map[string]any{
		"contents": []any{
			map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": "Reply with exactly: antigravity-ok"}},
			},
		},
		"generationConfig": map[string]any{"maxOutputTokens": 64},
	}
	envelope, err := WrapRequest(projectID, "gemini-2.5-flash", inner)
	if err != nil {
		t.Fatalf("STEP 8 wrap request failed: %v", err)
	}

	raw, err := postV1Internal(ctx, accessToken, "generateContent", envelope)
	if err != nil {
		t.Errorf("STEP 8 relay (buffered) FAILED: %v", err)
	} else if unwrapped, err := UnwrapResponse(raw); err != nil {
		t.Errorf("STEP 8 unwrap (buffered) FAILED: %v (raw=%s)", err, truncateBody(raw))
	} else {
		text := string(unwrapped)
		if !strings.Contains(text, "antigravity-ok") {
			t.Errorf("STEP 8 relay (buffered) returned no expected text: %s", truncateBody(unwrapped))
		} else if !strings.Contains(text, "usageMetadata") {
			t.Errorf("STEP 8 relay (buffered) returned no usageMetadata: %s", truncateBody(unwrapped))
		} else {
			t.Logf("STEP 8 relay (buffered): ok (text + usageMetadata present)")
		}
	}

	streamBody, err := postV1InternalStream(ctx, accessToken, envelope)
	if err != nil {
		t.Errorf("STEP 9 relay (stream) FAILED: %v", err)
		return
	}
	// Streaming delivers the answer as deltas, so the assertion has to assemble
	// the frames before comparing instead of searching the raw SSE.
	if assembled := streamedDeltaText(streamBody); !strings.Contains(assembled, "antigravity-ok") {
		t.Errorf("STEP 9 relay (stream) assembled %q, want it to contain antigravity-ok: %s",
			assembled, truncateBody([]byte(streamBody)))
		return
	}
	if !strings.Contains(streamBody, "usageMetadata") {
		t.Errorf("STEP 9 relay (stream) returned no usageMetadata: %s", truncateBody([]byte(streamBody)))
		return
	}
	if strings.Contains(streamBody, `"response":{`) {
		t.Errorf("STEP 9 relay (stream) leaked the raw envelope to the client: %s", truncateBody([]byte(streamBody)))
		return
	}
	t.Logf("STEP 9 relay (stream)  : ok (unwrapped SSE deltas, no raw envelope)")
}

func postV1Internal(ctx context.Context, accessToken, action string, payload any) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		ForwardBaseURLs[0]+"/v1internal:"+action, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBody(body))
	}
	return body, nil
}

// postV1InternalStream sends a streaming relay call and pipes the response
// through UnwrapReader, mirroring what the channel adaptor hands to the Gemini
// stream handler.
func postV1InternalStream(ctx context.Context, accessToken string, payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		ForwardBaseURLs[0]+"/v1internal:streamGenerateContent?alt=sse", bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", UserAgent())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBody(body))
	}

	unwrapped, err := io.ReadAll(NewUnwrapReader(resp.Body, true))
	if err != nil {
		return "", err
	}
	return string(unwrapped), nil
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}

// normalizeSmokeCode accepts the whole redirect URL, a bare query string, a
// percent-encoded code, or a plain code, so the operator can paste whatever the
// address bar shows.
func normalizeSmokeCode(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if parsed, err := url.Parse(raw); err == nil {
		if code := parsed.Query().Get("code"); code != "" {
			return code
		}
	}
	if decoded, err := url.QueryUnescape(raw); err == nil {
		return decoded
	}
	return raw
}

// streamedDeltaText concatenates the text parts of every unwrapped SSE frame.
func streamedDeltaText(body string) string {
	var builder strings.Builder
	for _, line := range strings.Split(body, "\n") {
		payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data:")
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var frame struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			continue
		}
		for _, candidate := range frame.Candidates {
			for _, part := range candidate.Content.Parts {
				builder.WriteString(part.Text)
			}
		}
	}
	return builder.String()
}
