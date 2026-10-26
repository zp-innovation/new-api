# feat(channel): add Google Antigravity (Cloud Code Assist) channel with OAuth login

## Summary

Adds a new channel type **`Google Antigravity` (type 64)** that relays to Google's Cloud Code Assist backend
(`cloudcode-pa.googleapis.com`), plus an admin OAuth login flow, credential auto-refresh and quota reporting.

The channel serves **both Claude and Gemini models**, because Antigravity exposes both families through the same
`v1internal` endpoint.

## Motivation

Cloud Code Assist speaks the Gemini `generateContent` protocol wrapped in its own envelope:

```json
POST /v1internal:streamGenerateContent?alt=sse
{ "project": "...", "model": "...", "request": { /* Gemini generateContent body */ } }

→ data: { "response": { /* Gemini generateContent response */ }, "responseId": "...", "modelVersion": "..." }
```

Today the only way to use it is to hand-craft the envelope outside new-api. This PR makes it a first-class channel.

## Architecture

```
pkg/antigravity/            protocol core (new, no host imports)
  oauth.go                  PKCE, session store, base-URL availability
  client.go                 token exchange/refresh, userinfo, loadCodeAssist,
                            onboardUser, fetchAvailableModels
  envelope.go               v1internal wrap/unwrap (merges responseId + modelVersion)
  unwrap.go                 UnwrapReader: rewrites buffered and SSE bodies
  models.go, quota.go       default model mapping, quota flattening

relay/channel/antigravity/  thin adaptor (new)
  adaptor.go                delegates ALL conversion to gemini.Adaptor and swaps
                            resp.Body for an UnwrapReader before delegating
                            responses to the existing Gemini handlers
```

### Why the adaptor is thin

The envelope is the only thing Antigravity adds on top of Gemini. So instead of porting a protocol converter:

* **requests** — `gemini.Adaptor.ConvertXxxRequest()` produces the Gemini body, and the adaptor only wraps it in
  `{project, model, request}`
* **responses** — `resp.Body` is replaced with `UnwrapReader`, after which the untouched
  `gemini.GeminiChatHandler` / `GeminiChatStreamHandler` / `GeminiResponses*Handler` run

This means billing, usage accounting, grounding, image counting and stream lifecycle are **inherited**, not
reimplemented. Fixes to those paths upstream benefit this channel automatically.

## Diff footprint

The integration is deliberately additive so an upstream merge stays cheap:

| Group | Change |
| --- | --- |
| **Pre-existing Go files** | **23 added lines, 0 removed**, across 8 files (channel type, display name, base URL, API type, channel→API mapping, endpoint types, adaptor factory, stream support map, 5 routes, 1 task start) |
| New files | 33 |
| Frontend | channel type entry, credential validation, typed API wrappers, one panel component, 25 i18n keys |
| Total vs `main` | 52 files, +6631 / −7 |

Every core edit is an appended line at the end of a switch/map/slice — no existing logic is modified.

## Verification

### Automated (66 cases)

```
go build ./relay/... ./common/... ./constant/... ./controller/... ./service/... ./model/... ./pkg/... ./router/...
go vet   ./pkg/antigravity/ ./relay/channel/antigravity/ ./service/ ./controller/ ./router/
go test  ./pkg/antigravity/ ./relay/channel/antigravity/ ./relay/ ./router/ ./constant/ ./common/
node web/scripts/sync-i18n.mjs   # 7 locales, missing=0 extras=0
```

* `pkg/antigravity` (43) — PKCE, session TTL and one-shot consumption, base-URL demotion and recovery, envelope
  round-trip, buffered/SSE unwrapping, quota flattening, plus an architecture test asserting the package imports
  no `new-api/` package
* `relay/channel/antigravity` (12) — credential parsing, header/URL construction, and end-to-end response handling
  through the **real** Gemini handlers for both the OpenAI and Claude output formats, buffered and streaming
* `relay` (2) + `router` (2) + `service` (7) — registry wiring, route permissions, refresh-window logic

### Live verification against the real upstream (3 rounds, 9 steps each)

Ran against real accounts with a two-phase smoke test (`ANTIGRAVITY_SMOKE=1`, skipped by default):

```
1 exchange-code   ok   4 onboardUser   skipped    7 credential   ok
2 userinfo        ok   5 quota         33 models   8 relay buffered ok
3 loadCodeAssist  ok   6 refresh-token ok          9 relay stream   ok
```

Steps 8/9 push a real `generateContent` / `streamGenerateContent` call through the envelope and assert the
unwrapped result, which is the only way to validate the envelope against the live protocol. A frame captured from
that run is pinned as a regression test.

Three findings that only live traffic could surface:

1. **Both test accounts receive the same `project` (`aicode-consumers`)** — it is a shared consumer-tier project,
   not a per-account resource. `project_id` therefore must not be treated as an account identity.
2. **`refresh_token` is not rotated** on refresh — keeping the previous value when the response omits one is
   required, not defensive.
3. **`allowedTiers` is an availability list, not an entitlement list.** An account holding a consumer subscription
   lists `standard-tier` but stays on `free-tier`; `onboardUser` for that account returns HTTP 200 with the
   existing project and does **not** change the tier. The implementation therefore calls `onboardUser` only when
   `loadCodeAssist` returns no project, which is its actual semantic (`onboard` = first-time provisioning).

## Known limitations

* **`onboardUser` is a defensive fallback.** No consumer account reached it in testing (all share
  `aicode-consumers`), and it cannot change tiers. It is covered by unit tests with a stub server only.
* **Tier activation is out of scope** — the upstream API does not expose it, so a consumer subscription cannot be
  upgraded through this channel. `currentTier` is reported as-is in the quota response.
* The quota view shows the current `remainingFraction` per model; there is no history or reset-credit handling.
* Privacy (`setUserSettings`) is not implemented; `Credential.privacy_mode` is reserved but always empty.

## Compliance

* Credentials are stored as the channel key, so the existing channel permission model, masking and audit behaviour
  apply unchanged; the OAuth endpoints sit behind `ChannelSensitiveWrite`.
* The refresh endpoint takes a **channel id**, not a token — credentials never travel to the browser.
* No request fingerprinting, TLS impersonation or detection bypass is included. The UI carries an explicit
  personal-use disclaimer, consistent with the existing Codex channel.

## Checklist

- [x] `gofmt` clean, `go vet` clean, all tests pass
- [x] New UI strings translated into all seven locales (`en`, `zh`, `zh-TW`, `fr`, `ja`, `ru`, `vi`)
- [x] Follows repository conventions (early returns, no single-caller helpers, English source messages through
      `common.ApiSuccessT` / `common.ApiErrorT`, `common.LogText` for server logs)
- [x] No existing logic modified — every core change is an appended line
- [x] Registry wiring, route permissions and package layering covered by guard tests

---

## How to open this PR (maintainer-side steps, not part of the description)

The branch is local only. To publish it:

```bash
# 1. Add your fork as a remote (replace with your fork URL)
git remote add fork https://github.com/<your-account>/new-api.git

# 2. Push the integration branch
git push -u fork feat/antigravity
```

Then open a pull request from `fork:feat/antigravity` into `QuantumNous/new-api:main` and paste everything above
the "How to open this PR" heading into the description.

Suggested PR title:

```
feat(channel): add Google Antigravity (Cloud Code Assist) channel with OAuth login
```