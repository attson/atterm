# Feishu Hook + Desktop-Direct Send Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace v0.2.114's relay-side outbound Feishu path with desktop-direct sending; attach precise "AI is asking…" question text captured via claude-code's Notification hook; support both relay-backed and local-only (no relay) onboarding modes.

**Architecture:** A new `desktop/feishu/` package owns all outbound Feishu IM sends (and inbound long-conn events in non-relay mode). A new tiny `cmd/atterm-hook` CLI bridges claude-code's hook system to a localhost HTTP endpoint inside the desktop process. The relay shrinks: its outbound Feishu code is deleted; one new endpoint `/v1/feishu/relay-token/me` hands a fresh `tenant_access_token` to the desktop on demand.

**Tech Stack:** Go 1.22; existing `internal/feishu/{card,client,token,event}` reused in-process (no copying); `github.com/zalando/go-keyring` for local-mode credentials; `github.com/larksuite/oapi-sdk-go/v3` (new dep) for the Feishu long-connection client; standard `net/http` for the desktop's localhost hook receiver.

**Spec:** [`docs/superpowers/specs/2026-06-17-feishu-hook-question-design.md`](../specs/2026-06-17-feishu-hook-question-design.md)

---

## File Structure

**New files:**

| Path | Responsibility |
|---|---|
| `cmd/atterm-hook/main.go` | Standalone CLI bridging claude-code's stdin payload to desktop's localhost endpoint |
| `cmd/atterm-hook/main_test.go` | CLI tests (sub-process based) |
| `desktop/feishu/dispatcher.go` | Coalesces all outbound triggers; dedup table; calls `card` + `client` |
| `desktop/feishu/dispatcher_test.go` | Dispatcher unit tests with fake store/IM/token |
| `desktop/feishu/hook_server.go` | Localhost `net/http.Server` receiving `HookNotifyRequest` |
| `desktop/feishu/hook_server_test.go` | HTTP receiver tests |
| `desktop/feishu/hook_adapter.go` | `HookAdapter` interface + claude-code impl + codex stub |
| `desktop/feishu/hook_adapter_test.go` | Adapter parsing tests |
| `desktop/feishu/binding_store.go` | `BindingStore` + `Credentials` + `BindingView` types |
| `desktop/feishu/binding_store_local.go` | Keychain-backed implementation |
| `desktop/feishu/binding_store_local_test.go` | Local store tests with in-memory keyring fake |
| `desktop/feishu/binding_store_relay.go` | Relay-HTTP-backed implementation |
| `desktop/feishu/binding_store_relay_test.go` | Relay store tests with HTTP stub |
| `desktop/feishu/token.go` | `TokenSource` + `RelayBorrowedTokenSource` + `LocalTenantTokenSource` |
| `desktop/feishu/token_test.go` | Token-source tests |
| `desktop/feishu/longconn.go` | Feishu long-connection event subscriber (local mode) |
| `desktop/feishu/longconn_test.go` | Long-conn tests with SDK fake |
| `desktop/feishu/service.go` | Top-level `Service` that picks mode + assembles pieces |
| `desktop/feishu/service_test.go` | Service mode-selection + lifecycle tests |
| `desktop/feishu/endpoint_file.go` | Read/write hook-endpoint discovery file |
| `desktop/feishu/endpoint_file_test.go` | Endpoint-file tests |
| `scripts/feishu-hook-e2e-checklist.md` | Manual e2e checklist |

**Modified files:**

| Path | Change |
|---|---|
| `internal/feishu/service.go` | DELETE `Send*` outbound surface + `authFailMu`/`authFailures` + `recordSendError`; ADD `RelayToken` method |
| `internal/feishu/service_test.go` | DELETE outbound tests; ADD `TestService_RelayToken_*` |
| `internal/feishu/card.go` | ADD `WaitingInputInput.QuestionText` + render branch |
| `internal/feishu/card_test.go` | ADD `TestRenderWaitingInputCard_WithQuestion` + `TestRenderWaitingInputCard_QuestionTruncation` |
| `internal/relay/feishu_http.go` | ADD `handleRelayToken` for `POST /v1/feishu/relay-token/me` |
| `internal/relay/feishu_http_test.go` | ADD `TestFeishuHTTP_RelayToken_*` |
| `internal/relay/server.go` | Register the new route inside the existing `cfg.Feishu != nil` guard |
| `internal/relay/uplink_conn.go` | DELETE both `s.cfg.Feishu.Send*` dispatch sites + relax the early-return guard |
| `internal/relay/uplink_feishu_test.go` | DELETE entire file |
| `desktop/relay_host.go` | Inject `ATTERM_SESSION_ID` + `ATTERM_HOOK_ENDPOINT` env into spawned PTY processes; subscribe to session task-state callbacks |
| `internal/session/session.go` | ADD `OnTaskStateChange` field + `SetOnTaskStateChange` setter; fire it on state transitions inside the existing locked paths |
| `internal/session/session_test.go` | ADD `TestSession_OnTaskStateChange` covering OSC 133 D + heuristic waiting_input transitions |
| `desktop/app.go` | Construct `desktop/feishu.Service`, wire endpoint file, subscribe to relay-login changes |
| `desktop/frontend/src/components/SettingsFeishu.vue` | Minimal UI: show current mode (relay-backed vs local), bind controls, copy callback URL or display "long-conn active" indicator |
| `desktop/frontend/src/shared/api/feishu.ts` | TypeScript types for the wails bindings (`getFeishuStatus`, `setFeishuCredentials`, etc.) |
| `go.mod` / `go.sum` | Add `github.com/larksuite/oapi-sdk-go/v3` |

**Dependencies (build order):**

```
A1 → A2 → A3 → A4  (relay cleanup; A1 before A2 because Send* deletion frees authFailMu/authFailures referenced nowhere else)

C1 (card QuestionText) — independent, can land anytime before C7

B1 (atterm-hook CLI) — fully independent

C3 (BindingStore iface) → C4 (local) → C5 (relay)
                                     └→ C6 (TokenSource)
C2 (HookAdapter) — independent

C7 (dispatcher) depends on C1, C2, C3, C6
C8 (hook_server) depends on C2 + C7

D1 → D2 → D3 (long-conn series); D2 depends on C3 (binding store)

E1 (Service assembly) depends on everything in C and D
E2 (endpoint file) — independent, can land anytime
E3 (PTY env injection) depends on E2
E4 (session OnTaskStateChange) — independent (internal/session change)
E5 (frontend) — depends on E1 (needs wails bindings)
E6 (final e2e + verify) — last
```

---

## Conventions (read once)

- All Go tests run via `go test ./...`. Per-package: `go test ./desktop/feishu/...`
- Tests use no testify; assert with `if got != want { t.Fatalf(...) }`.
- Conventional Commits everywhere: `feat(scope): ...`, `test(scope): ...`, `refactor(scope): ...`, `fix(scope): ...`, `chore: ...`.
- After each task: `go build ./...` and the affected package's tests before committing.
- Existing keychain helper pattern lives in `desktop/account_key_store.go`. Reuse `github.com/zalando/go-keyring` with a distinct service name `atterm.feishu.binding`.
- Existing session callback pattern lives at `internal/session/session.go:297` (`SetOnAIClassified`). Follow the same shape for `SetOnTaskStateChange`.
- PTY env injection point is `desktop/relay_host.go:381` where `env := terminalEnvForXterm(os.Environ())` is assembled — extend this slice with the two new vars.

---

## Task 1: Delete relay outbound feishu code (Send* methods)

**Files:**
- Modify: `internal/feishu/service.go`
- Modify: `internal/feishu/service_test.go`

- [ ] **Step 1: Identify all symbols to delete**

```bash
grep -n "SendCommandFinished\|SendSessionNotification\|sendCommandFinishedSync\|sendSessionNotificationSync\|recordSendError\|authFailMu\|authFailures\|func (s \*Service) send(" internal/feishu/service.go
```

Expected: lines for `SendCommandFinished`, `SendSessionNotification`, the two `_Sync` siblings, the private `send` helper, `recordSendError`, and the `authFailMu` / `authFailures` struct fields.

- [ ] **Step 2: Edit `internal/feishu/service.go`**

Delete these blocks:

```go
// DELETE: from struct Service { ... }
authFailMu   chan struct{}
authFailures map[string]int

// DELETE: from NewService(...) body
authFailMu:   make(chan struct{}, 1),
authFailures: map[string]int{},

// DELETE: SendCommandFinished + sendCommandFinishedSync
// DELETE: SendSessionNotification + sendSessionNotificationSync
// DELETE: func (s *Service) send(ctx context.Context, userID string, render func() ([]byte, error))
// DELETE: func (s *Service) recordSendError(ctx context.Context, b *Binding, err error)
```

After deletion the `Service` struct keeps only `cfg ServiceConfig`. `NewService` becomes:

```go
func NewService(cfg ServiceConfig) *Service {
    return &Service{cfg: cfg}
}
```

`HandleEvent`, `handleBindMessage`, `sendBindReply`, `MintTokenForCreds`, `InvalidateTokenForAppID` stay.

Also delete unused imports: `"encoding/json"` and `"log"` may still be needed by `sendBindReply` — verify with `go build ./...` and trim only what `goimports` flags.

- [ ] **Step 3: Edit `internal/feishu/service_test.go`**

Delete these test functions:

```go
// DELETE entirely:
//   TestService_SendCommandFinished_NoBinding
//   TestService_SendCommandFinished_HappyPath
//   TestService_SendCommandFinished_SkipsDisabled
```

Also delete now-unused fakes inside the test file: the `fakeIM.authFail` field, the `fakeStore.tokenAuthFail` field (if unused), and any helpers referenced only by the deleted tests.

Keep `TestService_HandleEvent_*` and `TestService_HandleEvent_DecryptFailure`.

- [ ] **Step 4: Run tests + build**

```bash
go build ./...
go test ./internal/feishu/...
```

Expected: PASS. The remaining `HandleEvent` tests still cover their paths.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/service.go internal/feishu/service_test.go
git commit -m "refactor(feishu): delete relay outbound Send* — desktop will own outbound"
```

---

## Task 2: Add `Service.RelayToken` method

**Files:**
- Modify: `internal/feishu/service.go`
- Modify: `internal/feishu/service_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/feishu/service_test.go`:

```go
func TestService_RelayToken_Success(t *testing.T) {
    st := newFakeStore()
    st.addBinding(&Binding{
        UserID: "u1", AppID: "a", AppSecret: "s",
        EncryptKey: "k", VerifyToken: "v",
        AppIDHash: "h", OpenID: "ou_x",
    })
    svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})

    tok, openID, hash, err := svc.RelayToken(context.Background(), "u1")
    if err != nil {
        t.Fatalf("RelayToken: %v", err)
    }
    if tok != "tt" || openID != "ou_x" || hash != "h" {
        t.Fatalf("got tok=%q open=%q hash=%q", tok, openID, hash)
    }
}

func TestService_RelayToken_NoBinding(t *testing.T) {
    svc := newSvc(newFakeStore(), &fakeIM{}, &fakeToken{tok: "tt"})
    _, _, _, err := svc.RelayToken(context.Background(), "missing")
    if !errors.Is(err, ErrBindingNotFound) {
        t.Fatalf("want ErrBindingNotFound, got %v", err)
    }
}

func TestService_RelayToken_Disabled(t *testing.T) {
    st := newFakeStore()
    st.addBinding(&Binding{UserID: "u1", AppIDHash: "h", DisabledAt: 1, OpenID: "ou_x"})
    svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})
    _, _, _, err := svc.RelayToken(context.Background(), "u1")
    if !errors.Is(err, ErrBindingDisabled) {
        t.Fatalf("want ErrBindingDisabled, got %v", err)
    }
}

func TestService_RelayToken_UpstreamFail(t *testing.T) {
    st := newFakeStore()
    st.addBinding(&Binding{UserID: "u1", AppID: "a", AppSecret: "s", AppIDHash: "h", OpenID: "ou_x"})
    bad := errors.New("upstream boom")
    svc := newSvc(st, &fakeIM{}, &fakeToken{err: bad})
    _, _, _, err := svc.RelayToken(context.Background(), "u1")
    if !errors.Is(err, bad) {
        t.Fatalf("want upstream error, got %v", err)
    }
}
```

- [ ] **Step 2: Run to confirm compile failure**

Run: `go test ./internal/feishu/ -run TestService_RelayToken`
Expected: FAIL — `undefined: ErrBindingDisabled`, `undefined: Service.RelayToken`.

- [ ] **Step 3: Add sentinel + method to `service.go`**

Append (above `HandleEvent`):

```go
// ErrBindingDisabled is returned by RelayToken when the stored binding
// has been marked disabled (typically after auth-class failures). The
// caller surfaces this to the desktop so the UI prompts re-config.
var ErrBindingDisabled = errors.New("feishu: binding disabled")

// RelayToken mints a tenant_access_token for the user's bound app and
// returns it along with the bound open_id + app_id_hash. Used by the
// new POST /v1/feishu/relay-token/me handler so the desktop can send
// IM messages directly without the relay seeing the payload.
//
// Errors:
//   ErrBindingNotFound  → caller maps to 404
//   ErrBindingDisabled  → caller maps to 410
//   other               → caller maps to 502 (Feishu unreachable)
func (s *Service) RelayToken(ctx context.Context, userID string) (token, openID, appIDHash string, err error) {
    b, err := s.cfg.Store.GetBindingByUserID(ctx, userID)
    if err != nil {
        return "", "", "", err
    }
    if b.DisabledAt != 0 {
        return "", "", "", ErrBindingDisabled
    }
    tok, err := s.cfg.Token.Get(ctx, b.AppID, b.AppSecret)
    if err != nil {
        return "", "", "", err
    }
    return tok, b.OpenID, b.AppIDHash, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/feishu/ -run TestService_RelayToken -v`
Expected: 4 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/service.go internal/feishu/service_test.go
git commit -m "feat(feishu): Service.RelayToken — mint token + return open_id for desktop borrow"
```

---

## Task 3: Add `POST /v1/feishu/relay-token/me` endpoint

**Files:**
- Modify: `internal/relay/feishu_http.go`
- Modify: `internal/relay/feishu_http_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/feishu_http_test.go`:

```go
func TestFeishuHTTP_RelayToken_Success(t *testing.T) {
    ctx := context.Background()
    st := newTestUserStoreWithCipher(t)
    u, _ := st.CreateOpaqueUser(ctx, "rt@example.com")
    _ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
        AppID: "cli_rt", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
    })
    _ = st.MarkFeishuBindingBound(ctx, u.ID, "ou_user")
    h, _ := newFeishuTestHandler(t, st, 0, "tt-xyz")

    req := httptest.NewRequest("POST", "/v1/feishu/relay-token/me", nil)
    req = req.WithContext(ctxWithUser(req.Context(), u.ID))
    rr := httptest.NewRecorder()
    h.ServeHTTPSession(rr, req)

    if rr.Code != 200 {
        t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
    }
    var resp struct {
        Token     string `json:"tenant_access_token"`
        ExpiresIn int    `json:"expires_in"`
        OpenID    string `json:"open_id"`
        AppIDHash string `json:"app_id_hash"`
    }
    _ = json.Unmarshal(rr.Body.Bytes(), &resp)
    if resp.Token != "tt-xyz" || resp.OpenID != "ou_user" {
        t.Fatalf("resp: %+v", resp)
    }
    if resp.AppIDHash == "" {
        t.Fatalf("expected app_id_hash in response")
    }
}

func TestFeishuHTTP_RelayToken_NoBinding(t *testing.T) {
    ctx := context.Background()
    st := newTestUserStoreWithCipher(t)
    u, _ := st.CreateOpaqueUser(ctx, "nb@example.com")
    h, _ := newFeishuTestHandler(t, st, 0, "tt")

    req := httptest.NewRequest("POST", "/v1/feishu/relay-token/me", nil)
    req = req.WithContext(ctxWithUser(req.Context(), u.ID))
    rr := httptest.NewRecorder()
    h.ServeHTTPSession(rr, req)

    if rr.Code != http.StatusNotFound {
        t.Fatalf("expected 404, got %d", rr.Code)
    }
}

func TestFeishuHTTP_RelayToken_Disabled(t *testing.T) {
    ctx := context.Background()
    st := newTestUserStoreWithCipher(t)
    u, _ := st.CreateOpaqueUser(ctx, "dis@example.com")
    _ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
        AppID: "x", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
    })
    _ = st.MarkFeishuBindingDisabled(ctx, u.ID)
    h, _ := newFeishuTestHandler(t, st, 0, "tt")

    req := httptest.NewRequest("POST", "/v1/feishu/relay-token/me", nil)
    req = req.WithContext(ctxWithUser(req.Context(), u.ID))
    rr := httptest.NewRecorder()
    h.ServeHTTPSession(rr, req)

    if rr.Code != http.StatusGone {
        t.Fatalf("expected 410, got %d", rr.Code)
    }
}

func TestFeishuHTTP_RelayToken_UpstreamFail(t *testing.T) {
    ctx := context.Background()
    st := newTestUserStoreWithCipher(t)
    u, _ := st.CreateOpaqueUser(ctx, "up@example.com")
    _ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
        AppID: "x", AppSecret: "bad", EncryptKey: "k", VerifyToken: "v",
    })
    _ = st.MarkFeishuBindingBound(ctx, u.ID, "ou_user")
    // tokenCode 99991663 = invalid app_secret → auth-class but here we want
    // generic upstream error → use a non-auth code.
    h, _ := newFeishuTestHandler(t, st, 99991000, "")

    req := httptest.NewRequest("POST", "/v1/feishu/relay-token/me", nil)
    req = req.WithContext(ctxWithUser(req.Context(), u.ID))
    rr := httptest.NewRecorder()
    h.ServeHTTPSession(rr, req)

    if rr.Code != http.StatusBadGateway {
        t.Fatalf("expected 502, got %d body=%s", rr.Code, rr.Body.String())
    }
}

func TestFeishuHTTP_RelayToken_Unauthorized(t *testing.T) {
    st := newTestUserStoreWithCipher(t)
    h, _ := newFeishuTestHandler(t, st, 0, "tt")

    req := httptest.NewRequest("POST", "/v1/feishu/relay-token/me", nil)
    rr := httptest.NewRecorder()
    h.ServeHTTPSession(rr, req)

    if rr.Code != http.StatusUnauthorized {
        t.Fatalf("expected 401, got %d", rr.Code)
    }
}
```

- [ ] **Step 2: Run to confirm 404**

Run: `go test ./internal/relay/ -run TestFeishuHTTP_RelayToken`
Expected: tests fail (the route returns `http.NotFound` from `ServeHTTPSession`'s default branch).

- [ ] **Step 3: Add the route + handler to `internal/relay/feishu_http.go`**

Extend the switch in `ServeHTTPSession`:

```go
case r.URL.Path == "/v1/feishu/relay-token/me" && r.Method == http.MethodPost:
    h.handleRelayToken(w, r)
```

Add the handler at the bottom of the file:

```go
// handleRelayToken hands the caller a fresh tenant_access_token plus
// the bound open_id + app_id_hash. Used by the desktop in relay-backed
// mode to send Feishu IM messages directly without round-tripping
// payloads through the relay.
func (h *FeishuHTTPHandler) handleRelayToken(w http.ResponseWriter, r *http.Request) {
    uid, ok := currentUserID(r)
    if !ok {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }
    tok, openID, hash, err := h.svc.RelayToken(r.Context(), uid)
    if errors.Is(err, feishu.ErrBindingNotFound) {
        http.Error(w, "feishu binding not configured", http.StatusNotFound)
        return
    }
    if errors.Is(err, feishu.ErrBindingDisabled) {
        http.Error(w, "feishu binding disabled — re-configure", http.StatusGone)
        return
    }
    if err != nil {
        http.Error(w, "feishu upstream error: "+err.Error(), http.StatusBadGateway)
        return
    }
    writeJSONStatus(w, http.StatusOK, map[string]any{
        "tenant_access_token": tok,
        "open_id":             openID,
        "app_id_hash":         hash,
        // Conservative: clients shouldn't cache > 110 min; Feishu tokens
        // expire after 7200s. We don't know the remaining lifetime here
        // (the cache is opaque), so claim 3000s. Desktop re-mints on miss.
        "expires_in": 3000,
    })
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/relay/ -run TestFeishuHTTP_RelayToken -v`
Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/feishu_http.go internal/relay/feishu_http_test.go
git commit -m "feat(relay): POST /v1/feishu/relay-token/me — mint token for desktop"
```

---

## Task 4: Delete relay uplink_conn feishu dispatch sites

**Files:**
- Modify: `internal/relay/uplink_conn.go`
- Delete: `internal/relay/uplink_feishu_test.go`

- [ ] **Step 1: Locate the dispatch sites**

```bash
grep -n "s.cfg.Feishu" internal/relay/uplink_conn.go
```

Expected: three matches — the early-return guard, the `command_finished` dispatch around the existing `s.cfg.Webhook.DispatchCommandFinished` call, and the `waiting_input` dispatch inside `notifySession`.

- [ ] **Step 2: Delete the three sites**

In `internal/relay/uplink_conn.go`:

A. Change the early-return guard from:

```go
if s.cfg.WebPush == nil && s.cfg.Webhook == nil && s.cfg.Feishu == nil {
    return
}
```

to:

```go
if s.cfg.WebPush == nil && s.cfg.Webhook == nil {
    return
}
```

B. Delete the entire `if s.cfg.Feishu != nil { s.cfg.Feishu.SendCommandFinished(...) }` block (the one immediately after the `s.cfg.Webhook.DispatchCommandFinished` block).

C. Inside `notifySession`, delete the `if s.cfg.Feishu != nil && notificationType == webpush.NotificationWaitingInput { s.cfg.Feishu.SendSessionNotification(...) }` block.

Remove now-unused `"github.com/attson/atterm/internal/feishu"` import if `go build` complains; otherwise leave it (other files in this package may still reference the package via the `cfg.Feishu` field type).

- [ ] **Step 3: Delete the integration test file**

```bash
git rm internal/relay/uplink_feishu_test.go
```

- [ ] **Step 4: Run tests + build**

```bash
go build ./...
go test ./internal/relay/...
```

Expected: PASS. The build will compile because `cfg.Feishu` field is still there (the new `RelayToken` endpoint uses it).

- [ ] **Step 5: Commit**

```bash
git add internal/relay/uplink_conn.go internal/relay/uplink_feishu_test.go
git commit -m "refactor(relay): remove uplink dispatch to feishu — desktop owns outbound now"
```

---

## Task 5: `WaitingInputInput.QuestionText` + truncation render

**Files:**
- Modify: `internal/feishu/card.go`
- Modify: `internal/feishu/card_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/feishu/card_test.go`:

```go
func TestRenderWaitingInputCard_WithQuestion(t *testing.T) {
    sid := uuid.MustParse("00000000-0000-0000-0000-000000000010")
    card := RenderWaitingInputCard(WaitingInputInput{
        SessionID:      sid,
        IdleForSeconds: 12,
        QuestionText:   "Run rm -rf node_modules? (y/N)",
    })
    s := mustJSON(t, card)
    // Question text must appear inside a fenced code block in lark_md
    if !strings.Contains(s, "```") {
        t.Fatalf("expected code fence in card body: %s", s)
    }
    if !strings.Contains(s, "Run rm -rf node_modules? (y/N)") {
        t.Fatalf("expected question text in body: %s", s)
    }
    if !strings.Contains(s, `"orange"`) {
        t.Fatalf("waiting card must still use orange template")
    }
}

func TestRenderWaitingInputCard_QuestionTruncation(t *testing.T) {
    // Build a 2000-char question. Truncation rule: max 1200 chars OR 6 lines.
    long := strings.Repeat("x", 2000)
    card := RenderWaitingInputCard(WaitingInputInput{
        SessionID:    uuid.New(),
        QuestionText: long,
    })
    s := mustJSON(t, card)
    if !strings.Contains(s, "已截断") {
        t.Fatalf("expected truncation marker `已截断` in body: %s", s)
    }
    // Ensure the rendered code-block contents do not contain all 2000 'x'.
    // We can't easily count exactly, but the rendered body must be smaller
    // than the input plus a generous fudge.
    if len(s) >= len(long)+512 {
        t.Fatalf("body length %d suggests truncation did not happen", len(s))
    }
}

func TestRenderWaitingInputCard_QuestionLineTruncation(t *testing.T) {
    // 20 lines, each short. Expect 6 lines + ellipsis.
    lines := make([]string, 20)
    for i := range lines {
        lines[i] = fmt.Sprintf("line %d", i+1)
    }
    long := strings.Join(lines, "\n")
    card := RenderWaitingInputCard(WaitingInputInput{
        SessionID:    uuid.New(),
        QuestionText: long,
    })
    s := mustJSON(t, card)
    if !strings.Contains(s, "已截断") {
        t.Fatalf("expected truncation marker: %s", s)
    }
    if !strings.Contains(s, "line 1") || !strings.Contains(s, "line 6") {
        t.Fatalf("expected lines 1..6 retained: %s", s)
    }
    if strings.Contains(s, "line 7") {
        t.Fatalf("expected line 7 dropped: %s", s)
    }
}

func TestRenderWaitingInputCard_EmptyQuestionStillRenders(t *testing.T) {
    card := RenderWaitingInputCard(WaitingInputInput{
        SessionID:      uuid.New(),
        IdleForSeconds: 30,
        // QuestionText left empty
    })
    s := mustJSON(t, card)
    if strings.Contains(s, "```") || strings.Contains(s, "已截断") {
        t.Fatalf("empty question must NOT render a code block or truncation marker: %s", s)
    }
    if !strings.Contains(s, "已闲置") {
        t.Fatalf("generic waiting copy still expected: %s", s)
    }
}
```

You'll need `"fmt"` in the test imports. Add it if missing.

- [ ] **Step 2: Run to confirm failures**

Run: `go test ./internal/feishu/ -run TestRenderWaitingInputCard`
Expected: the first three new tests FAIL (current `WaitingInputInput` has no `QuestionText` field, so the test compiles only after Step 3 — Go will fail with "unknown field"; that's the expected failure).

- [ ] **Step 3: Update `internal/feishu/card.go`**

Add `QuestionText` to the struct:

```go
type WaitingInputInput struct {
    SessionID      uuid.UUID
    IdleForSeconds int
    QuestionText   string // optional; populated by hook adapters
}
```

Update `RenderWaitingInputCard` to emit a question-text element when present. Replace the function with:

```go
func RenderWaitingInputCard(in WaitingInputInput) Card {
    elements := []any{
        map[string]any{
            "tag": "div",
            "text": map[string]any{
                "tag":     "lark_md",
                "content": fmt.Sprintf("Agent 在等待你回复（已闲置 %ds）", in.IdleForSeconds),
            },
        },
    }
    if q := strings.TrimSpace(in.QuestionText); q != "" {
        body, truncated := truncateQuestion(q)
        content := "```\n" + body + "\n```"
        if truncated {
            content += "\n_（已截断）_"
        }
        elements = append(elements, map[string]any{
            "tag":  "div",
            "text": map[string]any{"tag": "lark_md", "content": content},
        })
    }
    elements = append(elements, actionRow(in.SessionID, "waiting_input"))
    return Card{
        MsgType: "interactive",
        Card: map[string]any{
            "config": map[string]any{"wide_screen_mode": true},
            "header": map[string]any{
                "title":    map[string]any{"tag": "plain_text", "content": "Session 等待输入"},
                "template": "orange",
            },
            "elements": elements,
        },
    }
}

// truncateQuestion enforces the spec's 6-line / 1200-char limit with
// head-preservation. Returns (body, truncated).
func truncateQuestion(q string) (string, bool) {
    const (
        maxLines = 6
        maxChars = 1200
    )
    truncated := false
    lines := strings.Split(q, "\n")
    if len(lines) > maxLines {
        lines = lines[:maxLines]
        truncated = true
    }
    body := strings.Join(lines, "\n")
    if len(body) > maxChars {
        body = body[:maxChars]
        truncated = true
    }
    return body, truncated
}
```

Add `"strings"` to the imports of `card.go` (it likely isn't imported yet — `fmt` already is).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/feishu/ -run TestRenderWaitingInputCard -v`
Expected: 5 PASS (4 new + the existing `TestRenderWaitingInputCard`).

Also run: `go test ./internal/feishu/...`
Expected: full package green.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/card.go internal/feishu/card_test.go
git commit -m "feat(feishu): WaitingInputInput.QuestionText + 6-line/1200-char truncation"
```

---

## Task 6: `cmd/atterm-hook` CLI

**Files:**
- Create: `cmd/atterm-hook/main.go`
- Create: `cmd/atterm-hook/main_test.go`

- [ ] **Step 1: Write the test file**

```go
// cmd/atterm-hook/main_test.go
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// buildHook compiles the CLI into a temp binary once per test run.
func buildHook(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "atterm-hook")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func runHook(t *testing.T, bin string, env []string, stdin string) (stdout, stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err == nil {
		return outBuf.String(), errBuf.String(), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return outBuf.String(), errBuf.String(), ee.ExitCode()
	}
	t.Fatalf("run: %v", err)
	return "", "", -1
}

func TestHook_HappyPath(t *testing.T) {
	bin := buildHook(t)
	var received atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received.Store(string(body))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	env := append(os.Environ(),
		"ATTERM_SESSION_ID=00000000-0000-0000-0000-000000000001",
		"ATTERM_HOOK_ENDPOINT="+srv.URL,
	)
	stdin := `{"matcher":{"type":"idle_prompt"},"prompt_id":"p1"}`
	_, stderr, exit := runHook(t, bin, env, stdin)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	got, _ := received.Load().(string)
	if got == "" {
		t.Fatalf("server received nothing")
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(got), &req); err != nil {
		t.Fatalf("body not JSON: %v\n%s", err, got)
	}
	if req["session_id"] != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("session_id: %v", req["session_id"])
	}
	if req["agent_kind"] != "claude-code" {
		t.Fatalf("agent_kind: %v", req["agent_kind"])
	}
}

func TestHook_MissingSessionID_Silent(t *testing.T) {
	bin := buildHook(t)
	env := []string{"ATTERM_HOOK_ENDPOINT=http://127.0.0.1:1"}
	_, stderr, exit := runHook(t, bin, env, `{}`)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
}

func TestHook_MissingEndpoint_Silent(t *testing.T) {
	bin := buildHook(t)
	env := []string{"ATTERM_SESSION_ID=00000000-0000-0000-0000-000000000001"}
	_, stderr, exit := runHook(t, bin, env, `{}`)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
}

func TestHook_EndpointFromFile(t *testing.T) {
	bin := buildHook(t)
	var hit atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfgDir := filepath.Join(t.TempDir(), "atterm")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "hook-endpoint"), []byte(srv.URL), 0o644); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"HOME=" + filepath.Dir(cfgDir),
		"XDG_CONFIG_HOME=" + filepath.Dir(cfgDir),
		"ATTERM_SESSION_ID=sid",
	}
	_, stderr, exit := runHook(t, bin, env, `{}`)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	if hit.Load() != 1 {
		t.Fatalf("endpoint file not read; expected 1 hit, got %d", hit.Load())
	}
}

func TestHook_PostFailure_Silent(t *testing.T) {
	bin := buildHook(t)
	// Bind to a closed port: spin up + close
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	env := []string{
		"ATTERM_SESSION_ID=sid",
		"ATTERM_HOOK_ENDPOINT=" + srv.URL,
	}
	_, _, exit := runHook(t, bin, env, `{}`)
	if exit != 0 {
		t.Fatalf("expected exit 0 on POST failure, got %d", exit)
	}
}

func TestHook_StdinTooLarge_Drops(t *testing.T) {
	bin := buildHook(t)
	var hit atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	env := []string{
		"ATTERM_SESSION_ID=sid",
		"ATTERM_HOOK_ENDPOINT=" + srv.URL,
	}
	huge := strings.Repeat("x", 65*1024)
	_, stderr, exit := runHook(t, bin, env, huge)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if hit.Load() != 0 {
		t.Fatalf("expected POST suppressed for oversize stdin")
	}
	if !strings.Contains(stderr, "stdin too large") {
		t.Fatalf("expected stderr warning, got %q", stderr)
	}
}
```

- [ ] **Step 2: Run to confirm build failure**

Run: `go build ./cmd/atterm-hook/`
Expected: fail — `cmd/atterm-hook/main.go` doesn't exist yet.

- [ ] **Step 3: Implement `cmd/atterm-hook/main.go`**

```go
// cmd/atterm-hook/main.go
//
// atterm-hook bridges claude-code's Notification hook to a localhost
// HTTP endpoint inside the atterm desktop process. It is deliberately
// trivial: any failure path exits 0 so the hook never wedges
// claude-code itself.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	maxStdin    = 64 * 1024
	httpTimeout = 1 * time.Second
)

type hookNotifyRequest struct {
	SessionID   string          `json:"session_id"`
	AgentKind   string          `json:"agent_kind"`
	HookInput   json.RawMessage `json:"hook_input"`
	HookVersion string          `json:"hook_version,omitempty"`
}

func main() {
	// Read stdin with a hard cap. claude-code's NotificationHookInput
	// is small (~hundreds of bytes); 64 KB is generous.
	limited := io.LimitReader(os.Stdin, maxStdin+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atterm-hook: read stdin: %v\n", err)
		os.Exit(0)
	}
	if len(body) > maxStdin {
		fmt.Fprintf(os.Stderr, "atterm-hook: stdin too large (>%d), dropping\n", maxStdin)
		os.Exit(0)
	}
	if len(body) == 0 {
		os.Exit(0)
	}

	sessionID := os.Getenv("ATTERM_SESSION_ID")
	if sessionID == "" {
		// Running outside an atterm session — nothing to do.
		os.Exit(0)
	}

	endpoint := resolveEndpoint()
	if endpoint == "" {
		os.Exit(0)
	}

	req := hookNotifyRequest{
		SessionID: sessionID,
		AgentKind: "claude-code",
		HookInput: json.RawMessage(body),
	}
	if v := os.Getenv("CLAUDE_CODE_VERSION"); v != "" {
		req.HookVersion = v
	}
	payload, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atterm-hook: encode: %v\n", err)
		os.Exit(0)
	}

	client := &http.Client{Timeout: httpTimeout}
	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		fmt.Fprintf(os.Stderr, "atterm-hook: new request: %v\n", err)
		os.Exit(0)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atterm-hook: post: %v\n", err)
		os.Exit(0)
	}
	defer resp.Body.Close()
	// Any HTTP status is fine — we just don't want to bubble up.
}

// resolveEndpoint returns the desktop's hook endpoint URL. Lookup order:
//
//	1. env ATTERM_HOOK_ENDPOINT
//	2. ~/.config/atterm/hook-endpoint (POSIX)
//	   %APPDATA%/atterm/hook-endpoint (Windows)
//	3. "" — caller should silently exit 0
func resolveEndpoint() string {
	if v := os.Getenv("ATTERM_HOOK_ENDPOINT"); v != "" {
		return v
	}
	dir, err := endpointFileDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "hook-endpoint"))
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(data))
}

func endpointFileDir() (string, error) {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, "atterm"), nil
		}
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "atterm"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "atterm"), nil
}
```

- [ ] **Step 4: Run tests + build**

```bash
go build ./cmd/atterm-hook/
go test ./cmd/atterm-hook/ -v
```

Expected: 6 tests PASS. The tests `go build` the CLI per-test, so the build step exercises the same code path.

- [ ] **Step 5: Commit**

```bash
git add cmd/atterm-hook/main.go cmd/atterm-hook/main_test.go
git commit -m "feat(atterm-hook): standalone CLI bridging claude-code hook to desktop"
```

---

## Task 7: `HookAdapter` interface + claude-code implementation

**Files:**
- Create: `desktop/feishu/hook_adapter.go`
- Create: `desktop/feishu/hook_adapter_test.go`

- [ ] **Step 1: Write the failing test**

```go
// desktop/feishu/hook_adapter_test.go
package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeCodeAdapter_AskUserQuestionEmits(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "matcher": {"type":"idle_prompt", "tool":"AskUserQuestion"},
	  "prompt_id":"p-1",
	  "context": {"tool_input": {"question": "Continue refactor? (y/N)"}}
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatalf("expected emit")
	}
	if !strings.Contains(ev.QuestionText, "Continue refactor?") {
		t.Fatalf("question text missing: %q", ev.QuestionText)
	}
	if ev.DedupKey != "claude-code:p-1" {
		t.Fatalf("dedup key: %q", ev.DedupKey)
	}
}

func TestClaudeCodeAdapter_PermissionPromptEmits(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "matcher": {"type":"permission_prompt"},
	  "prompt_id":"p-2",
	  "context": {
	    "tool_name":"Bash",
	    "tool_input": {"command": "rm -rf node_modules"}
	  }
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatalf("expected emit")
	}
	if !strings.Contains(ev.QuestionText, "Bash") || !strings.Contains(ev.QuestionText, "rm -rf node_modules") {
		t.Fatalf("question text should describe the tool + input: %q", ev.QuestionText)
	}
}

func TestClaudeCodeAdapter_IdleWithoutTool_Skips(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "matcher": {"type":"idle_prompt"},
	  "prompt_id":"p-3"
	}`)
	if _, ok := a.Parse(in, ""); ok {
		t.Fatalf("idle_prompt without AskUserQuestion must skip (false positive guard)")
	}
}

func TestClaudeCodeAdapter_UnknownMatcher_Skips(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "matcher": {"type":"subagent_stop"},
	  "prompt_id":"p-4"
	}`)
	if _, ok := a.Parse(in, ""); ok {
		t.Fatalf("unknown matcher must skip")
	}
}

func TestClaudeCodeAdapter_MalformedJSON_Skips(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{`)
	if _, ok := a.Parse(in, ""); ok {
		t.Fatalf("malformed input must skip")
	}
}

func TestClaudeCodeAdapter_DedupFallback(t *testing.T) {
	a := &claudeCodeAdapter{}
	// No prompt_id → dedup key falls back to hash of question text.
	in := json.RawMessage(`{
	  "matcher": {"type":"idle_prompt", "tool":"AskUserQuestion"},
	  "context": {"tool_input": {"question": "fallback question"}}
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatalf("expected emit")
	}
	if !strings.HasPrefix(ev.DedupKey, "claude-code:hash:") {
		t.Fatalf("expected fallback hash dedup key, got %q", ev.DedupKey)
	}
}

func TestRegistryLookup(t *testing.T) {
	a, ok := LookupHookAdapter("claude-code")
	if !ok || a == nil {
		t.Fatalf("claude-code adapter must be registered")
	}
	if _, ok := LookupHookAdapter("nope"); ok {
		t.Fatalf("unknown agent_kind must return ok=false")
	}
}
```

- [ ] **Step 2: Confirm build failure**

Run: `go test ./desktop/feishu/... -run TestClaudeCodeAdapter`
Expected: fail — package doesn't exist yet.

- [ ] **Step 3: Implement `desktop/feishu/hook_adapter.go`**

```go
// desktop/feishu/hook_adapter.go
package feishu

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// WaitingInputEvent is the normalized payload a HookAdapter emits when
// the underlying agent (claude-code, codex, …) signals it is waiting
// on the user. The dispatcher uses this to render + send a Feishu card.
type WaitingInputEvent struct {
	QuestionText string // truncation happens later in the dispatcher / card render
	DedupKey     string // stable within one prompt; "claude-code:<prompt_id>" or "claude-code:hash:<sha>"
}

// HookAdapter parses an agent-specific hook payload into a normalized
// WaitingInputEvent. Returning (_, false) means "this event isn't a
// real waiting-for-user signal — ignore it".
type HookAdapter interface {
	AgentKind() string
	Parse(hookInput json.RawMessage, hookVersion string) (WaitingInputEvent, bool)
}

var hookAdapters = map[string]HookAdapter{
	"claude-code": &claudeCodeAdapter{},
	// Future: "codex": &codexAdapter{} when openai/codex#19921 lands.
}

// LookupHookAdapter returns the registered adapter for an agent_kind.
func LookupHookAdapter(kind string) (HookAdapter, bool) {
	a, ok := hookAdapters[kind]
	return a, ok
}

// claudeCodeAdapter parses claude-code's NotificationHookInput schema.
type claudeCodeAdapter struct{}

func (*claudeCodeAdapter) AgentKind() string { return "claude-code" }

type ccNotificationHookInput struct {
	Matcher  ccMatcher       `json:"matcher"`
	PromptID string          `json:"prompt_id"`
	Context  json.RawMessage `json:"context"`
}

type ccMatcher struct {
	Type string `json:"type"`
	Tool string `json:"tool"`
}

func (a *claudeCodeAdapter) Parse(raw json.RawMessage, _ string) (WaitingInputEvent, bool) {
	var in ccNotificationHookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return WaitingInputEvent{}, false
	}

	switch in.Matcher.Type {
	case "permission_prompt":
		// Always emit for permission prompts — these are always blocking.
		q := summarizePermissionContext(in.Context)
		return mkEvent(in, q), true
	case "idle_prompt":
		// Only emit when claude-code is actually waiting on an explicit
		// AskUserQuestion tool — the unfiltered idle_prompt matcher has
		// a known false-positive problem (anthropics/claude-code#12048).
		if in.Matcher.Tool != "AskUserQuestion" {
			return WaitingInputEvent{}, false
		}
		q := extractAskUserQuestion(in.Context)
		if q == "" {
			q = "Claude is waiting on a question."
		}
		return mkEvent(in, q), true
	default:
		return WaitingInputEvent{}, false
	}
}

func mkEvent(in ccNotificationHookInput, q string) WaitingInputEvent {
	dedup := "claude-code:" + in.PromptID
	if in.PromptID == "" {
		sum := sha256.Sum256([]byte(q))
		dedup = "claude-code:hash:" + hex.EncodeToString(sum[:8])
	}
	return WaitingInputEvent{
		QuestionText: q,
		DedupKey:     dedup,
	}
}

// summarizePermissionContext renders "<tool> wants to: <args summary>"
// from claude-code's permission context.
func summarizePermissionContext(ctx json.RawMessage) string {
	var p struct {
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(ctx, &p); err != nil {
		return "Claude wants permission for an action."
	}
	tool := p.ToolName
	if tool == "" {
		tool = "Tool"
	}
	argsLine := compactJSONOneLine(p.ToolInput)
	if argsLine == "" {
		return tool + " requested approval."
	}
	return fmt.Sprintf("%s wants to:\n%s", tool, argsLine)
}

// extractAskUserQuestion returns the human-readable question text from
// the AskUserQuestion tool's input (the field name is "question" in
// claude-code's schema; we fall back to the entire compact JSON if not
// present).
func extractAskUserQuestion(ctx json.RawMessage) string {
	var p struct {
		ToolInput struct {
			Question string `json:"question"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(ctx, &p); err == nil && p.ToolInput.Question != "" {
		return p.ToolInput.Question
	}
	var fallback struct {
		ToolInput json.RawMessage `json:"tool_input"`
	}
	_ = json.Unmarshal(ctx, &fallback)
	return compactJSONOneLine(fallback.ToolInput)
}

func compactJSONOneLine(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	out, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(out)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./desktop/feishu/ -run "TestClaudeCodeAdapter|TestRegistryLookup" -v`
Expected: 7 PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/hook_adapter.go desktop/feishu/hook_adapter_test.go
git commit -m "feat(desktop/feishu): HookAdapter + claude-code Notification parser"
```

---

## Task 8: `BindingStore` interface + types

**Files:**
- Create: `desktop/feishu/binding_store.go`
- Create: `desktop/feishu/binding_store_test.go`

- [ ] **Step 1: Write the failing test**

```go
// desktop/feishu/binding_store_test.go
package feishu

import (
	"context"
	"errors"
	"testing"
)

// inMemBindingStore is a trivial implementation used to assert the
// interface contract without dragging in keychain or HTTP code.
type inMemBindingStore struct {
	view *BindingView
}

func (s *inMemBindingStore) Get(ctx context.Context) (*BindingView, error) {
	if s.view == nil {
		return nil, ErrLocalBindingNotFound
	}
	cp := *s.view
	return &cp, nil
}
func (s *inMemBindingStore) SetCredentials(ctx context.Context, c Credentials) error {
	s.view = &BindingView{
		AppID: c.AppID, AppSecret: c.AppSecret,
		EncryptKey: c.EncryptKey, VerifyToken: c.VerifyToken,
		AppIDHash: hashAppID(c.AppID),
	}
	return nil
}
func (s *inMemBindingStore) SetBound(ctx context.Context, openID string) error {
	if s.view == nil {
		return ErrLocalBindingNotFound
	}
	s.view.OpenID = openID
	return nil
}
func (s *inMemBindingStore) SetDisabled(ctx context.Context) error {
	if s.view == nil {
		return ErrLocalBindingNotFound
	}
	s.view.DisabledAt = 1
	return nil
}
func (s *inMemBindingStore) ClearDisabled(ctx context.Context) error {
	if s.view == nil {
		return ErrLocalBindingNotFound
	}
	s.view.DisabledAt = 0
	return nil
}
func (s *inMemBindingStore) Delete(ctx context.Context) error {
	s.view = nil
	return nil
}

func TestBindingStore_Interface(t *testing.T) {
	var _ BindingStore = (*inMemBindingStore)(nil)
	s := &inMemBindingStore{}
	ctx := context.Background()
	if _, err := s.Get(ctx); !errors.Is(err, ErrLocalBindingNotFound) {
		t.Fatalf("empty Get must return ErrLocalBindingNotFound")
	}
	if err := s.SetCredentials(ctx, Credentials{
		AppID: "cli_x", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
	}); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	v, _ := s.Get(ctx)
	if v.AppID != "cli_x" || v.AppIDHash == "" {
		t.Fatalf("view: %+v", v)
	}
	if err := s.SetBound(ctx, "ou_test"); err != nil {
		t.Fatalf("SetBound: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.OpenID != "ou_test" {
		t.Fatalf("OpenID not set")
	}
	if err := s.SetDisabled(ctx); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.DisabledAt == 0 {
		t.Fatalf("DisabledAt not set")
	}
	if err := s.ClearDisabled(ctx); err != nil {
		t.Fatalf("ClearDisabled: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.DisabledAt != 0 {
		t.Fatalf("DisabledAt not cleared")
	}
}

func TestHashAppID(t *testing.T) {
	a := hashAppID("cli_x")
	b := hashAppID("cli_x")
	if a != b || len(a) != 64 {
		t.Fatalf("hash unstable or wrong length: %q vs %q", a, b)
	}
	if a == hashAppID("cli_y") {
		t.Fatalf("hash collision")
	}
}
```

- [ ] **Step 2: Run to confirm fail**

Run: `go test ./desktop/feishu/ -run TestBindingStore`
Expected: fail — types don't exist.

- [ ] **Step 3: Implement `desktop/feishu/binding_store.go`**

```go
// desktop/feishu/binding_store.go
package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// Credentials is the user-supplied tuple required to send + receive
// Feishu IM messages for an app. Identical shape across both storage
// modes.
type Credentials struct {
	AppID       string
	AppSecret   string
	EncryptKey  string
	VerifyToken string
}

// BindingView is the read-side projection seen by the dispatcher.
type BindingView struct {
	AppID, AppSecret, EncryptKey, VerifyToken string
	AppIDHash                                  string // SHA256 hex
	OpenID                                     string // empty until paired
	BoundAt                                    int64
	DisabledAt                                 int64
}

// ErrLocalBindingNotFound is the sentinel both local and relay-backed
// implementations return when Get is called against an empty store.
var ErrLocalBindingNotFound = errors.New("desktop/feishu: binding not found")

// BindingStore is the contract the dispatcher relies on. Two
// implementations: local-keychain (no relay) and relay-HTTP-backed.
type BindingStore interface {
	Get(ctx context.Context) (*BindingView, error)
	SetCredentials(ctx context.Context, c Credentials) error
	SetBound(ctx context.Context, openID string) error
	SetDisabled(ctx context.Context) error
	ClearDisabled(ctx context.Context) error
	Delete(ctx context.Context) error
}

// hashAppID mirrors the relay-side helper. Kept private here because
// the desktop never imports `internal/userstore`; we don't want the
// implementations to drift apart, but copying a 3-line SHA256 is
// cheaper than dragging in a dependency.
func hashAppID(appID string) string {
	sum := sha256.Sum256([]byte(appID))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./desktop/feishu/ -run "TestBindingStore|TestHashAppID" -v`
Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/binding_store.go desktop/feishu/binding_store_test.go
git commit -m "feat(desktop/feishu): BindingStore interface + Credentials/BindingView types"
```

---

## Task 9: `LocalKeychainBindingStore`

**Files:**
- Create: `desktop/feishu/binding_store_local.go`
- Create: `desktop/feishu/binding_store_local_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// desktop/feishu/binding_store_local_test.go
package feishu

import (
	"context"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestLocalKeychainBindingStore_RoundTrip(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)

	s := NewLocalKeychainBindingStore()
	ctx := context.Background()

	if _, err := s.Get(ctx); !errors.Is(err, ErrLocalBindingNotFound) {
		t.Fatalf("empty Get: %v", err)
	}

	if err := s.SetCredentials(ctx, Credentials{
		AppID: "cli_x", AppSecret: "sec",
		EncryptKey: "enc", VerifyToken: "vt",
	}); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}

	v, err := s.Get(ctx)
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if v.AppID != "cli_x" || v.AppSecret != "sec" {
		t.Fatalf("creds round-trip: %+v", v)
	}
	if v.AppIDHash == "" {
		t.Fatalf("AppIDHash should be derived")
	}

	if err := s.SetBound(ctx, "ou_user"); err != nil {
		t.Fatalf("SetBound: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.OpenID != "ou_user" || v.BoundAt == 0 {
		t.Fatalf("bound state: %+v", v)
	}

	if err := s.SetDisabled(ctx); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.DisabledAt == 0 {
		t.Fatalf("DisabledAt not stored")
	}

	if err := s.ClearDisabled(ctx); err != nil {
		t.Fatalf("ClearDisabled: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.DisabledAt != 0 {
		t.Fatalf("DisabledAt not cleared")
	}

	if err := s.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx); !errors.Is(err, ErrLocalBindingNotFound) {
		t.Fatalf("after Delete Get must return ErrLocalBindingNotFound: %v", err)
	}
}

func TestLocalKeychainBindingStore_CredentialUpsertPreservesBoundState(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)

	s := NewLocalKeychainBindingStore()
	ctx := context.Background()
	_ = s.SetCredentials(ctx, Credentials{AppID: "a", AppSecret: "x", EncryptKey: "k", VerifyToken: "v"})
	_ = s.SetBound(ctx, "ou_keep")
	_ = s.SetCredentials(ctx, Credentials{AppID: "a", AppSecret: "x2", EncryptKey: "k2", VerifyToken: "v2"})
	v, _ := s.Get(ctx)
	if v.OpenID != "ou_keep" {
		t.Fatalf("re-Upsert must preserve open_id; got %+v", v)
	}
	if v.AppSecret != "x2" {
		t.Fatalf("re-Upsert must update secrets; got %+v", v)
	}
}
```

- [ ] **Step 2: Confirm failure**

Run: `go test ./desktop/feishu/ -run TestLocalKeychainBindingStore`
Expected: fail — undefined `NewLocalKeychainBindingStore`.

- [ ] **Step 3: Implement `desktop/feishu/binding_store_local.go`**

```go
// desktop/feishu/binding_store_local.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	keychainService = "atterm.feishu.binding"
	keychainAccount = "binding-v1"
)

// LocalKeychainBindingStore persists the user's Feishu binding to the
// OS keychain as a single JSON blob. This is the storage implementation
// used when the desktop is not logged into a relay account.
type LocalKeychainBindingStore struct{}

func NewLocalKeychainBindingStore() *LocalKeychainBindingStore {
	return &LocalKeychainBindingStore{}
}

type localBindingBlob struct {
	AppID       string `json:"app_id"`
	AppSecret   string `json:"app_secret"`
	EncryptKey  string `json:"encrypt_key"`
	VerifyToken string `json:"verify_token"`
	OpenID      string `json:"open_id,omitempty"`
	BoundAt     int64  `json:"bound_at,omitempty"`
	DisabledAt  int64  `json:"disabled_at,omitempty"`
}

func (s *LocalKeychainBindingStore) Get(ctx context.Context) (*BindingView, error) {
	raw, err := keyring.Get(keychainService, keychainAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrLocalBindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("keyring get: %w", err)
	}
	var b localBindingBlob
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return nil, fmt.Errorf("decode blob: %w", err)
	}
	return &BindingView{
		AppID: b.AppID, AppSecret: b.AppSecret,
		EncryptKey: b.EncryptKey, VerifyToken: b.VerifyToken,
		AppIDHash:  hashAppID(b.AppID),
		OpenID:     b.OpenID,
		BoundAt:    b.BoundAt,
		DisabledAt: b.DisabledAt,
	}, nil
}

func (s *LocalKeychainBindingStore) write(b localBindingBlob) error {
	buf, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("encode blob: %w", err)
	}
	return keyring.Set(keychainService, keychainAccount, string(buf))
}

func (s *LocalKeychainBindingStore) SetCredentials(ctx context.Context, c Credentials) error {
	cur, err := s.Get(ctx)
	var blob localBindingBlob
	if err == nil {
		// Preserve open_id / bound_at; clear disabled (re-config implies user wants it back).
		blob = localBindingBlob{
			OpenID:  cur.OpenID,
			BoundAt: cur.BoundAt,
		}
	}
	blob.AppID = c.AppID
	blob.AppSecret = c.AppSecret
	blob.EncryptKey = c.EncryptKey
	blob.VerifyToken = c.VerifyToken
	blob.DisabledAt = 0
	return s.write(blob)
}

func (s *LocalKeychainBindingStore) SetBound(ctx context.Context, openID string) error {
	v, err := s.Get(ctx)
	if err != nil {
		return err
	}
	return s.write(localBindingBlob{
		AppID: v.AppID, AppSecret: v.AppSecret,
		EncryptKey: v.EncryptKey, VerifyToken: v.VerifyToken,
		OpenID:     openID,
		BoundAt:    time.Now().Unix(),
		DisabledAt: v.DisabledAt,
	})
}

func (s *LocalKeychainBindingStore) SetDisabled(ctx context.Context) error {
	v, err := s.Get(ctx)
	if err != nil {
		return err
	}
	return s.write(localBindingBlob{
		AppID: v.AppID, AppSecret: v.AppSecret,
		EncryptKey: v.EncryptKey, VerifyToken: v.VerifyToken,
		OpenID:     v.OpenID,
		BoundAt:    v.BoundAt,
		DisabledAt: time.Now().Unix(),
	})
}

func (s *LocalKeychainBindingStore) ClearDisabled(ctx context.Context) error {
	v, err := s.Get(ctx)
	if err != nil {
		return err
	}
	return s.write(localBindingBlob{
		AppID: v.AppID, AppSecret: v.AppSecret,
		EncryptKey: v.EncryptKey, VerifyToken: v.VerifyToken,
		OpenID:     v.OpenID,
		BoundAt:    v.BoundAt,
		DisabledAt: 0,
	})
}

func (s *LocalKeychainBindingStore) Delete(ctx context.Context) error {
	err := keyring.Delete(keychainService, keychainAccount)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("keyring delete: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./desktop/feishu/ -run TestLocalKeychainBindingStore -v`
Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/binding_store_local.go desktop/feishu/binding_store_local_test.go
git commit -m "feat(desktop/feishu): LocalKeychainBindingStore — keyring-backed local mode"
```

---

## Task 10: `RelayBackedBindingStore`

**Files:**
- Create: `desktop/feishu/binding_store_relay.go`
- Create: `desktop/feishu/binding_store_relay_test.go`

- [ ] **Step 1: Write the failing test**

```go
// desktop/feishu/binding_store_relay_test.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRelayBackedBindingStore_Get(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/feishu/bindings/me" || r.Method != "GET" {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-session-token" {
			t.Errorf("auth: %q", got)
		}
		called++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"configured":   true,
			"bound":        true,
			"open_id":      "ou_relay",
			"disabled_at":  0,
			"callback_url": srv_url_for_test() + "/v1/feishu/events/HASH",
		})
	}))
	defer srv.Close()

	s := NewRelayBackedBindingStore(srv.URL, func() string { return "test-session-token" })
	v, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.OpenID != "ou_relay" {
		t.Fatalf("OpenID: %q", v.OpenID)
	}
	if called != 1 {
		t.Fatalf("called: %d", called)
	}
}

func TestRelayBackedBindingStore_GetNotConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"configured": false, "bound": false})
	}))
	defer srv.Close()

	s := NewRelayBackedBindingStore(srv.URL, func() string { return "token" })
	if _, err := s.Get(context.Background()); !errors.Is(err, ErrLocalBindingNotFound) {
		t.Fatalf("expected ErrLocalBindingNotFound, got %v", err)
	}
}

func TestRelayBackedBindingStore_SetCredentials(t *testing.T) {
	var got struct {
		AppID, AppSecret, EncryptKey, VerifyToken string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/feishu/bindings/me" && r.Method == "POST" {
			_ = json.NewDecoder(r.Body).Decode(&got)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"app_id_hash":  "h",
				"callback_url": "url",
			})
			return
		}
		t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	s := NewRelayBackedBindingStore(srv.URL, func() string { return "token" })
	err := s.SetCredentials(context.Background(), Credentials{
		AppID: "cli_z", AppSecret: "sec",
		EncryptKey: "ek", VerifyToken: "vt",
	})
	if err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	if got.AppID != "cli_z" || got.AppSecret != "sec" {
		t.Fatalf("upstream got: %+v", got)
	}
}

func TestRelayBackedBindingStore_Delete(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/feishu/bindings/me" && r.Method == "DELETE" {
			hit = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	s := NewRelayBackedBindingStore(srv.URL, func() string { return "token" })
	if err := s.Delete(context.Background()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !hit {
		t.Fatalf("expected DELETE request")
	}
}

func TestRelayBackedBindingStore_SetBoundReturnsErrUnsupported(t *testing.T) {
	s := NewRelayBackedBindingStore("http://example", func() string { return "tok" })
	if err := s.SetBound(context.Background(), "x"); !errors.Is(err, ErrRelayManagedBoundState) {
		t.Fatalf("SetBound on relay-backed store must return ErrRelayManagedBoundState, got %v", err)
	}
}

// srv_url_for_test allows the callback_url string above to compile without
// importing httptest in the format string.
func srv_url_for_test() string { return "http://test" }
```

- [ ] **Step 2: Confirm failure**

Run: `go test ./desktop/feishu/ -run TestRelayBackedBindingStore`
Expected: fail — undefined `NewRelayBackedBindingStore`.

- [ ] **Step 3: Implement `desktop/feishu/binding_store_relay.go`**

```go
// desktop/feishu/binding_store_relay.go
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrRelayManagedBoundState is returned by SetBound / SetDisabled /
// ClearDisabled on the relay-backed store, because those fields are
// owned by the relay (mutated by the inbound /v1/feishu/events
// callback) and the desktop has no direct write path for them.
var ErrRelayManagedBoundState = errors.New("desktop/feishu: bound state managed by relay")

// RelayBackedBindingStore proxies binding operations to the relay's
// /v1/feishu/bindings/me endpoints (introduced in v0.2.114).
type RelayBackedBindingStore struct {
	baseURL  string
	tokenFn  func() string // session bearer token
	client   *http.Client
}

func NewRelayBackedBindingStore(baseURL string, tokenFn func() string) *RelayBackedBindingStore {
	return &RelayBackedBindingStore{
		baseURL: strings.TrimRight(baseURL, "/"),
		tokenFn: tokenFn,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *RelayBackedBindingStore) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader *bytes.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(buf)
	}
	var req *http.Request
	var err error
	if reader != nil {
		req, err = http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, s.baseURL+path, nil)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.tokenFn())
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.client.Do(req)
}

func (s *RelayBackedBindingStore) Get(ctx context.Context) (*BindingView, error) {
	resp, err := s.do(ctx, "GET", "/v1/feishu/bindings/me", nil)
	if err != nil {
		return nil, fmt.Errorf("relay get binding: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("relay get binding: status %d", resp.StatusCode)
	}
	var r struct {
		Configured  bool   `json:"configured"`
		Bound       bool   `json:"bound"`
		OpenID      string `json:"open_id"`
		DisabledAt  int64  `json:"disabled_at"`
		CallbackURL string `json:"callback_url"`
		AppIDHash   string `json:"app_id_hash"` // may not be present; some fields stripped client-side
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode binding: %w", err)
	}
	if !r.Configured {
		return nil, ErrLocalBindingNotFound
	}
	// Derive app_id_hash from the callback URL tail if not provided
	// (the relay's GET intentionally hides the app credentials).
	hash := r.AppIDHash
	if hash == "" && r.CallbackURL != "" {
		if i := strings.LastIndex(r.CallbackURL, "/"); i >= 0 {
			hash = r.CallbackURL[i+1:]
		}
	}
	return &BindingView{
		// Note: AppID/AppSecret/EncryptKey/VerifyToken are NOT returned by
		// the relay GET. The dispatcher should obtain a fresh token via
		// the RelayBorrowedTokenSource instead of reaching for these.
		AppIDHash:  hash,
		OpenID:     r.OpenID,
		DisabledAt: r.DisabledAt,
	}, nil
}

func (s *RelayBackedBindingStore) SetCredentials(ctx context.Context, c Credentials) error {
	resp, err := s.do(ctx, "POST", "/v1/feishu/bindings/me", map[string]string{
		"app_id":       c.AppID,
		"app_secret":   c.AppSecret,
		"encrypt_key":  c.EncryptKey,
		"verify_token": c.VerifyToken,
	})
	if err != nil {
		return fmt.Errorf("relay set credentials: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := readSnippet(resp.Body)
		return fmt.Errorf("relay set credentials: status %d body=%s", resp.StatusCode, body)
	}
	return nil
}

func (s *RelayBackedBindingStore) SetBound(ctx context.Context, openID string) error {
	return ErrRelayManagedBoundState
}
func (s *RelayBackedBindingStore) SetDisabled(ctx context.Context) error {
	return ErrRelayManagedBoundState
}
func (s *RelayBackedBindingStore) ClearDisabled(ctx context.Context) error {
	return ErrRelayManagedBoundState
}

func (s *RelayBackedBindingStore) Delete(ctx context.Context) error {
	resp, err := s.do(ctx, "DELETE", "/v1/feishu/bindings/me", nil)
	if err != nil {
		return fmt.Errorf("relay delete binding: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("relay delete binding: status %d", resp.StatusCode)
	}
	return nil
}

func readSnippet(r interface{ Read([]byte) (int, error) }) (string, error) {
	buf := make([]byte, 256)
	n, err := r.Read(buf)
	return string(buf[:n]), err
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./desktop/feishu/ -run TestRelayBackedBindingStore -v`
Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/binding_store_relay.go desktop/feishu/binding_store_relay_test.go
git commit -m "feat(desktop/feishu): RelayBackedBindingStore — proxies /v1/feishu/bindings/me"
```

---

## Task 11: `TokenSource` + two impls

**Files:**
- Create: `desktop/feishu/token.go`
- Create: `desktop/feishu/token_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// desktop/feishu/token_test.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRelayBorrowedTokenSource_GetAndCache(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/feishu/relay-token/me" && r.Method == "POST" {
			calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tenant_access_token": "tt-1",
				"open_id":             "ou_x",
				"app_id_hash":         "h",
				"expires_in":          3000,
			})
			return
		}
		t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	ts := NewRelayBorrowedTokenSource(srv.URL, func() string { return "tok" })
	tok, openID, _, err := ts.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if tok != "tt-1" || openID != "ou_x" {
		t.Fatalf("token: %q open: %q", tok, openID)
	}
	// Second call within cache window must reuse.
	_, _, _, _ = ts.Get(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls.Load())
	}
	// Invalidate forces a refresh.
	ts.Invalidate()
	_, _, _, _ = ts.Get(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("expected 2 upstream calls after Invalidate, got %d", calls.Load())
	}
}

func TestRelayBorrowedTokenSource_NotConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "feishu binding not configured", http.StatusNotFound)
	}))
	defer srv.Close()
	ts := NewRelayBorrowedTokenSource(srv.URL, func() string { return "tok" })
	_, _, _, err := ts.Get(context.Background())
	if !errors.Is(err, ErrTokenNotConfigured) {
		t.Fatalf("want ErrTokenNotConfigured, got %v", err)
	}
}

func TestRelayBorrowedTokenSource_Disabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "disabled", http.StatusGone)
	}))
	defer srv.Close()
	ts := NewRelayBorrowedTokenSource(srv.URL, func() string { return "tok" })
	_, _, _, err := ts.Get(context.Background())
	if !errors.Is(err, ErrTokenDisabled) {
		t.Fatalf("want ErrTokenDisabled, got %v", err)
	}
}

func TestLocalTenantTokenSource_DelegatesToFeishuClient(t *testing.T) {
	// Feishu tenant_access_token/internal stub
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "msg": "ok",
			"tenant_access_token": "local-tt",
			"expire":              7200,
		})
	}))
	defer upstream.Close()

	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{
		AppID: "cli_x", AppSecret: "sec",
		EncryptKey: "ek", VerifyToken: "vt",
	})
	_ = store.SetBound(context.Background(), "ou_local")

	ts := NewLocalTenantTokenSource(store, upstream.URL, nil, func() time.Time { return time.Unix(1000, 0) })
	tok, openID, hash, err := ts.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if tok != "local-tt" {
		t.Fatalf("token: %q", tok)
	}
	if openID != "ou_local" {
		t.Fatalf("open: %q", openID)
	}
	if hash == "" {
		t.Fatalf("hash empty")
	}
}
```

- [ ] **Step 2: Confirm failure**

Run: `go test ./desktop/feishu/ -run "TestRelayBorrowedTokenSource|TestLocalTenantTokenSource"`
Expected: fail — undefined symbols.

- [ ] **Step 3: Implement `desktop/feishu/token.go`**

```go
// desktop/feishu/token.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/feishu"
)

// TokenSource returns a (tenant_access_token, open_id, app_id_hash)
// tuple ready to use with the Feishu IM client.
type TokenSource interface {
	Get(ctx context.Context) (token, openID, appIDHash string, err error)
	Invalidate()
}

// Sentinels surfaced to the dispatcher.
var (
	ErrTokenNotConfigured = errors.New("desktop/feishu: feishu not configured")
	ErrTokenDisabled      = errors.New("desktop/feishu: feishu binding disabled")
)

// RelayBorrowedTokenSource calls POST /v1/feishu/relay-token/me on the
// relay (added in Task 3) and caches the result.
type RelayBorrowedTokenSource struct {
	baseURL string
	tokenFn func() string
	client  *http.Client

	mu    sync.Mutex
	cache cachedRelayToken
}

type cachedRelayToken struct {
	tok, openID, hash string
	expiresAt         time.Time
}

func NewRelayBorrowedTokenSource(baseURL string, tokenFn func() string) *RelayBorrowedTokenSource {
	return &RelayBorrowedTokenSource{
		baseURL: strings.TrimRight(baseURL, "/"),
		tokenFn: tokenFn,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (r *RelayBorrowedTokenSource) Invalidate() {
	r.mu.Lock()
	r.cache = cachedRelayToken{}
	r.mu.Unlock()
}

func (r *RelayBorrowedTokenSource) Get(ctx context.Context) (string, string, string, error) {
	r.mu.Lock()
	if r.cache.tok != "" && time.Now().Before(r.cache.expiresAt.Add(-5*time.Minute)) {
		c := r.cache
		r.mu.Unlock()
		return c.tok, c.openID, c.hash, nil
	}
	r.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, "POST", r.baseURL+"/v1/feishu/relay-token/me", nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+r.tokenFn())
	resp, err := r.client.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("borrow token: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		// fall through
	case http.StatusNotFound:
		return "", "", "", ErrTokenNotConfigured
	case http.StatusGone:
		return "", "", "", ErrTokenDisabled
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", "", "", fmt.Errorf("borrow token: status %d body=%s", resp.StatusCode, body)
	}
	var rr struct {
		Token     string `json:"tenant_access_token"`
		OpenID    string `json:"open_id"`
		Hash      string `json:"app_id_hash"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return "", "", "", fmt.Errorf("decode token: %w", err)
	}
	r.mu.Lock()
	r.cache = cachedRelayToken{
		tok: rr.Token, openID: rr.OpenID, hash: rr.Hash,
		expiresAt: time.Now().Add(time.Duration(rr.ExpiresIn) * time.Second),
	}
	r.mu.Unlock()
	return rr.Token, rr.OpenID, rr.Hash, nil
}

// LocalTenantTokenSource mints tokens directly against Feishu using
// the credentials stored in a local BindingStore.
type LocalTenantTokenSource struct {
	store BindingStore
	cache *feishu.TenantTokenCache
}

func NewLocalTenantTokenSource(store BindingStore, baseURL string, httpC *http.Client, now func() time.Time) *LocalTenantTokenSource {
	return &LocalTenantTokenSource{
		store: store,
		cache: feishu.NewTenantTokenCache(baseURL, httpC, now),
	}
}

func (l *LocalTenantTokenSource) Invalidate() {
	// Tokens are keyed on app_id; we don't have a "drop all" API, but
	// Get below re-reads the store so a credentials change naturally
	// re-keys.  No-op for now.
}

func (l *LocalTenantTokenSource) Get(ctx context.Context) (string, string, string, error) {
	v, err := l.store.Get(ctx)
	if err != nil {
		if errors.Is(err, ErrLocalBindingNotFound) {
			return "", "", "", ErrTokenNotConfigured
		}
		return "", "", "", err
	}
	if v.DisabledAt != 0 {
		return "", "", "", ErrTokenDisabled
	}
	tok, err := l.cache.Get(ctx, v.AppID, v.AppSecret)
	if err != nil {
		return "", "", "", fmt.Errorf("mint local token: %w", err)
	}
	return tok, v.OpenID, v.AppIDHash, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./desktop/feishu/ -run "TestRelayBorrowedTokenSource|TestLocalTenantTokenSource" -v`
Expected: 4 PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/token.go desktop/feishu/token_test.go
git commit -m "feat(desktop/feishu): TokenSource + relay-borrowed + local-tenant impls"
```

---

## Task 12: `Dispatcher` with dedup window

**Files:**
- Create: `desktop/feishu/dispatcher.go`
- Create: `desktop/feishu/dispatcher_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// desktop/feishu/dispatcher_test.go
package feishu

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

type capturingIM struct {
	mu       sync.Mutex
	bodies   []string
	openIDs  []string
	tokens   []string
	err      error
	authFail bool
}

func (c *capturingIM) SendInteractiveToOpenID(ctx context.Context, token, openID string, body []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authFail {
		return &authError{}
	}
	if c.err != nil {
		return c.err
	}
	c.tokens = append(c.tokens, token)
	c.openIDs = append(c.openIDs, openID)
	c.bodies = append(c.bodies, string(body))
	return nil
}
func (c *capturingIM) SendTextToOpenID(ctx context.Context, token, openID, text string) error {
	return nil
}

type authError struct{}

func (*authError) Error() string                { return "auth-class fake" }
func (*authError) IsFeishuAuthClassError() bool { return true }

type stubTokenSource struct {
	tok, openID, hash string
	err               error
	invalidated       atomic.Int32
}

func (s *stubTokenSource) Get(ctx context.Context) (string, string, string, error) {
	if s.err != nil {
		return "", "", "", s.err
	}
	return s.tok, s.openID, s.hash, nil
}
func (s *stubTokenSource) Invalidate() { s.invalidated.Add(1) }

func TestDispatcher_CommandFinishedHappy(t *testing.T) {
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = store.SetBound(context.Background(), "ou_x")
	im := &capturingIM{}
	ts := &stubTokenSource{tok: "tt", openID: "ou_x", hash: "h"}
	d := NewDispatcher(DispatcherConfig{Store: store, Token: ts, IM: im})

	d.DispatchCommandFinished(context.Background(), CommandFinishedEvent{
		SessionID: uuid.New(),
		ExitCode:  0,
		Label:     "go test",
		ElapsedMS: 2500,
	})
	if len(im.bodies) != 1 {
		t.Fatalf("expected 1 send, got %d", len(im.bodies))
	}
	if im.openIDs[0] != "ou_x" || im.tokens[0] != "tt" {
		t.Fatalf("send args: %+v %+v", im.openIDs, im.tokens)
	}
}

func TestDispatcher_NoBindingDrops(t *testing.T) {
	store := &inMemBindingStore{}
	d := NewDispatcher(DispatcherConfig{
		Store: store,
		Token: &stubTokenSource{err: ErrTokenNotConfigured},
		IM:    &capturingIM{},
	})
	im := d.cfg.IM.(*capturingIM)
	d.DispatchCommandFinished(context.Background(), CommandFinishedEvent{SessionID: uuid.New()})
	if len(im.bodies) != 0 {
		t.Fatalf("expected drop")
	}
}

func TestDispatcher_DedupWindowSuppressesSecondWaiting(t *testing.T) {
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = store.SetBound(context.Background(), "ou_x")
	im := &capturingIM{}
	d := NewDispatcher(DispatcherConfig{
		Store: store,
		Token: &stubTokenSource{tok: "tt", openID: "ou_x", hash: "h"},
		IM:    im,
	})
	sid := uuid.New()
	d.DispatchWaitingInput(context.Background(), WaitingInputDispatchEvent{
		SessionID: sid, Source: WaitingSourceHook, QuestionText: "Q1", DedupKey: "k1",
	})
	d.DispatchWaitingInput(context.Background(), WaitingInputDispatchEvent{
		SessionID: sid, Source: WaitingSourceHeuristic, QuestionText: "", DedupKey: "",
	})
	if len(im.bodies) != 1 {
		t.Fatalf("expected 1 send, got %d", len(im.bodies))
	}
}

func TestDispatcher_AuthFailDisables(t *testing.T) {
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = store.SetBound(context.Background(), "ou_x")
	im := &capturingIM{authFail: true}
	ts := &stubTokenSource{tok: "tt", openID: "ou_x", hash: "h"}
	d := NewDispatcher(DispatcherConfig{Store: store, Token: ts, IM: im})
	for i := 0; i < 3; i++ {
		d.DispatchCommandFinished(context.Background(), CommandFinishedEvent{
			SessionID: uuid.New(),
		})
	}
	v, _ := store.Get(context.Background())
	if v.DisabledAt == 0 {
		t.Fatalf("expected store.DisabledAt set after 3 auth-class failures")
	}
	if ts.invalidated.Load() == 0 {
		t.Fatalf("expected token source to be invalidated")
	}
}

func TestDispatcher_DedupExpires(t *testing.T) {
	now := newAtomicTime(1_000_000)
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = store.SetBound(context.Background(), "ou_x")
	im := &capturingIM{}
	d := NewDispatcher(DispatcherConfig{
		Store: store,
		Token: &stubTokenSource{tok: "tt", openID: "ou_x", hash: "h"},
		IM:    im,
		Now:   now.read,
	})
	sid := uuid.New()
	d.DispatchWaitingInput(context.Background(), WaitingInputDispatchEvent{
		SessionID: sid, Source: WaitingSourceHook, DedupKey: "k1",
	})
	now.advance(31) // > 30s window
	d.DispatchWaitingInput(context.Background(), WaitingInputDispatchEvent{
		SessionID: sid, Source: WaitingSourceHeuristic,
	})
	if len(im.bodies) != 2 {
		t.Fatalf("expected 2 sends after window expiry; got %d", len(im.bodies))
	}
}

// atomicTime is a tiny test clock used by the dedup window test.
type atomicTime struct {
	v atomic.Int64
}

func newAtomicTime(seconds int64) *atomicTime { a := &atomicTime{}; a.v.Store(seconds); return a }
func (a *atomicTime) read() (sec int64)        { return a.v.Load() }
func (a *atomicTime) advance(s int64)          { a.v.Add(s) }
```

Helper note: the test file declares an `atomicTime` clock type but the
public dispatcher accepts a `Now func() int64` (unix seconds). Adjust
the literal helper above to match the constructor convention you pick.

- [ ] **Step 2: Confirm failure**

Run: `go test ./desktop/feishu/ -run TestDispatcher`
Expected: fail — undefined.

- [ ] **Step 3: Implement `desktop/feishu/dispatcher.go`**

```go
// desktop/feishu/dispatcher.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	internalfeishu "github.com/attson/atterm/internal/feishu"
)

// IMClient is the subset of internal/feishu.Client the dispatcher uses.
type IMClient interface {
	SendInteractiveToOpenID(ctx context.Context, token, openID string, body []byte) error
	SendTextToOpenID(ctx context.Context, token, openID, text string) error
}

// CommandFinishedEvent feeds the dispatcher from the heuristic OSC 133 D path.
type CommandFinishedEvent struct {
	SessionID  uuid.UUID
	ExitCode   int
	ElapsedMS  int
	Label      string
	SealedBody []byte
}

// WaitingInputDispatchEvent feeds the dispatcher from both hook (precise)
// and heuristic paths. Source is used for dedup priority.
type WaitingInputDispatchEvent struct {
	SessionID      uuid.UUID
	IdleForSeconds int
	Source         WaitingSource
	QuestionText   string // empty for heuristic
	DedupKey       string // empty when Source is heuristic
}

type WaitingSource int

const (
	WaitingSourceHeuristic WaitingSource = iota
	WaitingSourceHook
)

// DispatcherConfig holds the wired-in dependencies.
type DispatcherConfig struct {
	Store BindingStore
	Token TokenSource
	IM    IMClient
	// Now returns Unix seconds. Default = time.Now().Unix.
	Now func() int64
}

// Dispatcher merges the heuristic + hook trigger streams into Feishu IM
// sends. Safe for concurrent use.
type Dispatcher struct {
	cfg DispatcherConfig

	muD          sync.Mutex
	lastDispatch map[string]int64 // dedup-key OR session_id → unix seconds
	failures     map[string]int   // counter per session_id
}

const (
	dedupWindowSeconds = 30
	maxAuthFails       = 3
)

func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	if cfg.Now == nil {
		cfg.Now = func() int64 { return time.Now().Unix() }
	}
	return &Dispatcher{
		cfg:          cfg,
		lastDispatch: map[string]int64{},
		failures:     map[string]int{},
	}
}

func (d *Dispatcher) DispatchCommandFinished(ctx context.Context, ev CommandFinishedEvent) {
	d.dispatch(ctx, ev.SessionID, "cmd:"+ev.SessionID.String(), func() ([]byte, error) {
		card := internalfeishu.RenderCommandFinishedCard(internalfeishu.CommandFinishedInput{
			SessionID:  ev.SessionID,
			ExitCode:   ev.ExitCode,
			ElapsedMS:  ev.ElapsedMS,
			Label:      ev.Label,
			SealedBody: ev.SealedBody,
		})
		return json.Marshal(card)
	})
}

func (d *Dispatcher) DispatchWaitingInput(ctx context.Context, ev WaitingInputDispatchEvent) {
	key := ev.DedupKey
	if key == "" {
		key = "waiting:" + ev.SessionID.String()
	}
	d.dispatch(ctx, ev.SessionID, key, func() ([]byte, error) {
		card := internalfeishu.RenderWaitingInputCard(internalfeishu.WaitingInputInput{
			SessionID:      ev.SessionID,
			IdleForSeconds: ev.IdleForSeconds,
			QuestionText:   ev.QuestionText,
		})
		return json.Marshal(card)
	})
}

func (d *Dispatcher) dispatch(ctx context.Context, sid uuid.UUID, dedupKey string, render func() ([]byte, error)) {
	now := d.cfg.Now()

	// Dedup window check.
	d.muD.Lock()
	if last, ok := d.lastDispatch[dedupKey]; ok && now-last < dedupWindowSeconds {
		d.muD.Unlock()
		return
	}
	d.muD.Unlock()

	tok, openID, _, err := d.cfg.Token.Get(ctx)
	if err != nil {
		if errors.Is(err, ErrTokenNotConfigured) {
			return // silent
		}
		if errors.Is(err, ErrTokenDisabled) {
			return
		}
		log.Printf("feishu: dispatch token: %v", err)
		return
	}
	if openID == "" {
		return
	}

	body, err := render()
	if err != nil {
		log.Printf("feishu: render card: %v", err)
		return
	}

	if err := d.cfg.IM.SendInteractiveToOpenID(ctx, tok, openID, body); err != nil {
		d.recordSendError(ctx, sid, err)
		return
	}

	// Reset the failure counter on success.
	d.muD.Lock()
	delete(d.failures, sid.String())
	d.lastDispatch[dedupKey] = now
	d.muD.Unlock()
}

type feishuAuthClass interface {
	IsFeishuAuthClassError() bool
}

func (d *Dispatcher) recordSendError(ctx context.Context, sid uuid.UUID, err error) {
	var ac feishuAuthClass
	if errors.As(err, &ac) && ac.IsFeishuAuthClassError() {
		d.muD.Lock()
		d.failures[sid.String()]++
		count := d.failures[sid.String()]
		d.muD.Unlock()
		if count >= maxAuthFails {
			if setErr := d.cfg.Store.SetDisabled(ctx); setErr != nil && !errors.Is(setErr, ErrRelayManagedBoundState) {
				log.Printf("feishu: SetDisabled: %v", setErr)
			}
			d.cfg.Token.Invalidate()
		}
		return
	}
	log.Printf("feishu: send to %s: %v", sid, err)
}
```

The above implementation relies on the IM client's auth-class errors
implementing `IsFeishuAuthClassError() bool`. Task 18 (Service
assembly) wires the real `internal/feishu.Client` and the existing
`AuthClassError` already has the right method (after a tiny adapter —
see Task 18).

- [ ] **Step 4: Run tests**

Run: `go test ./desktop/feishu/ -run TestDispatcher -v`
Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/dispatcher.go desktop/feishu/dispatcher_test.go
git commit -m "feat(desktop/feishu): Dispatcher — 30s dedup window + auth-class disable counter"
```

---

## Task 13: Localhost `hook_server`

**Files:**
- Create: `desktop/feishu/hook_server.go`
- Create: `desktop/feishu/hook_server_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// desktop/feishu/hook_server_test.go
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

type recordingDispatcher struct {
	mu      sync.Mutex
	waiting []WaitingInputDispatchEvent
}

func (r *recordingDispatcher) DispatchWaitingInput(ctx context.Context, ev WaitingInputDispatchEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waiting = append(r.waiting, ev)
}

type sessionsFake struct {
	known map[string]bool
}

func (f *sessionsFake) Exists(sid uuid.UUID) bool { return f.known[sid.String()] }

func TestHookServer_HappyPath(t *testing.T) {
	disp := &recordingDispatcher{}
	sid := uuid.MustParse("00000000-0000-0000-0000-000000000099")
	sessions := &sessionsFake{known: map[string]bool{sid.String(): true}}
	h := NewHookServer(disp, sessions)

	body := mustJSONBytes(t, hookNotifyRequest{
		SessionID: sid.String(),
		AgentKind: "claude-code",
		HookInput: json.RawMessage(`{"matcher":{"type":"idle_prompt","tool":"AskUserQuestion"},"prompt_id":"p","context":{"tool_input":{"question":"go?"}}}`),
	})
	req := httptest.NewRequest("POST", "/atterm-hook/notify", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	disp.mu.Lock()
	defer disp.mu.Unlock()
	if len(disp.waiting) != 1 {
		t.Fatalf("expected 1 dispatch, got %d", len(disp.waiting))
	}
	if disp.waiting[0].QuestionText == "" {
		t.Fatalf("expected question text")
	}
}

func TestHookServer_UnknownSession(t *testing.T) {
	disp := &recordingDispatcher{}
	sessions := &sessionsFake{known: map[string]bool{}}
	h := NewHookServer(disp, sessions)

	body := mustJSONBytes(t, hookNotifyRequest{
		SessionID: uuid.New().String(),
		AgentKind: "claude-code",
		HookInput: json.RawMessage(`{}`),
	})
	req := httptest.NewRequest("POST", "/atterm-hook/notify", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestHookServer_RejectsRemoteAddr(t *testing.T) {
	h := NewHookServer(&recordingDispatcher{}, &sessionsFake{})
	req := httptest.NewRequest("POST", "/atterm-hook/notify", strings.NewReader("{}"))
	req.RemoteAddr = "10.0.0.1:5555"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestHookServer_OversizeBody(t *testing.T) {
	h := NewHookServer(&recordingDispatcher{}, &sessionsFake{})
	big := bytes.Repeat([]byte("x"), 130*1024)
	req := httptest.NewRequest("POST", "/atterm-hook/notify", bytes.NewReader(big))
	req.RemoteAddr = "127.0.0.1:5555"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestHookServer_BindLocalhost(t *testing.T) {
	h := NewHookServer(&recordingDispatcher{}, &sessionsFake{})
	addr, srv, err := h.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("expected loopback bind, got %q", host)
	}
	_ = atomic.AddInt32(new(int32), 1) // touch atomic import to keep gofmt happy
}

func mustJSONBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
```

- [ ] **Step 2: Confirm failure**

Run: `go test ./desktop/feishu/ -run TestHookServer`
Expected: fail — undefined.

- [ ] **Step 3: Implement `desktop/feishu/hook_server.go`**

```go
// desktop/feishu/hook_server.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const maxHookBody = 128 * 1024 // a bit more headroom than the CLI's 64 KB cap

// SessionLookup is the subset of session bookkeeping the hook server
// needs.
type SessionLookup interface {
	Exists(sid uuid.UUID) bool
}

// WaitingDispatcher is the subset of *Dispatcher the hook server uses.
type WaitingDispatcher interface {
	DispatchWaitingInput(ctx context.Context, ev WaitingInputDispatchEvent)
}

// HookServer terminates POSTs from the atterm-hook CLI and forwards
// the normalized event to the dispatcher.
type HookServer struct {
	disp     WaitingDispatcher
	sessions SessionLookup
}

type hookNotifyRequest struct {
	SessionID   string          `json:"session_id"`
	AgentKind   string          `json:"agent_kind"`
	HookInput   json.RawMessage `json:"hook_input"`
	HookVersion string          `json:"hook_version,omitempty"`
}

func NewHookServer(disp WaitingDispatcher, sessions SessionLookup) *HookServer {
	return &HookServer{disp: disp, sessions: sessions}
}

// Start binds a localhost listener on 127.0.0.1:0 and returns the
// chosen address + the *http.Server so the caller can Shutdown.
func (h *HookServer) Start() (addr string, srv *http.Server, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/atterm-hook/notify", h)
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(ln) }()
	return ln.Addr().String(), server, nil
}

func (h *HookServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Defense in depth: reject if remote isn't loopback even though the
	// listener binds to 127.0.0.1.
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != "127.0.0.1" && host != "::1" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxHookBody)+1))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if len(body) > maxHookBody {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}

	var req hookNotifyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}

	sid, err := uuid.Parse(strings.TrimSpace(req.SessionID))
	if err != nil || sid == uuid.Nil {
		http.Error(w, "bad session id", http.StatusBadRequest)
		return
	}
	if h.sessions != nil && !h.sessions.Exists(sid) {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}

	adapter, ok := LookupHookAdapter(req.AgentKind)
	if !ok {
		// Unknown agent — drop politely.
		w.WriteHeader(http.StatusOK)
		return
	}
	ev, emit := adapter.Parse(req.HookInput, req.HookVersion)
	if !emit {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.disp.DispatchWaitingInput(r.Context(), WaitingInputDispatchEvent{
		SessionID:    sid,
		Source:       WaitingSourceHook,
		QuestionText: ev.QuestionText,
		DedupKey:     ev.DedupKey,
	})

	// Best-effort context-cancel safety: avoid blocking on dispatch.
	_ = errors.New // keep import set stable in case build trims
	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./desktop/feishu/ -run TestHookServer -v`
Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/hook_server.go desktop/feishu/hook_server_test.go
git commit -m "feat(desktop/feishu): localhost HookServer — bind 127.0.0.1, 128 KB cap"
```

---

## Task 14: Hook endpoint discovery file

**Files:**
- Create: `desktop/feishu/endpoint_file.go`
- Create: `desktop/feishu/endpoint_file_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// desktop/feishu/endpoint_file_test.go
package feishu

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEndpointFile_WriteAndRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", t.TempDir())
	} else {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("HOME", t.TempDir())
	}
	url := "http://127.0.0.1:12345/atterm-hook/notify"
	if err := WriteEndpointFile(url); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadEndpointFile()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != url {
		t.Fatalf("got %q want %q", got, url)
	}
	if err := DeleteEndpointFile(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(endpointFilePathForTest(t)); !os.IsNotExist(err) {
		t.Fatalf("expected gone")
	}
}

func TestEndpointFile_ReadMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", t.TempDir())
	} else {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("HOME", t.TempDir())
	}
	if _, err := ReadEndpointFile(); err == nil {
		t.Fatalf("expected error on missing file")
	}
}

// endpointFilePathForTest is exposed for the test only — see
// implementation comments below.
func endpointFilePathForTest(t *testing.T) string {
	t.Helper()
	p, err := endpointFilePath()
	if err != nil {
		t.Fatal(err)
	}
	_ = filepath.Clean(p)
	return p
}
```

- [ ] **Step 2: Confirm failure**

Run: `go test ./desktop/feishu/ -run TestEndpointFile`
Expected: fail — undefined.

- [ ] **Step 3: Implement `desktop/feishu/endpoint_file.go`**

```go
// desktop/feishu/endpoint_file.go
//
// Hook-endpoint discovery file. Lives at
//   ~/.config/atterm/hook-endpoint           (POSIX)
//   %APPDATA%\atterm\hook-endpoint           (Windows)
// and contains the URL the atterm-hook CLI POSTs to when its env var
// is not set.
package feishu

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func endpointFilePath() (string, error) {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, "atterm", "hook-endpoint"), nil
		}
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "atterm", "hook-endpoint"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "atterm", "hook-endpoint"), nil
}

func WriteEndpointFile(url string) error {
	path, err := endpointFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(url), 0o600)
}

func ReadEndpointFile() (string, error) {
	path, err := endpointFilePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func DeleteEndpointFile() error {
	path, err := endpointFilePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./desktop/feishu/ -run TestEndpointFile -v`
Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/endpoint_file.go desktop/feishu/endpoint_file_test.go
git commit -m "feat(desktop/feishu): endpoint-file discovery (write/read/delete)"
```

---

## Task 15: Feishu long-conn SDK dep + skeleton

**Files:**
- Modify: `go.mod` / `go.sum`
- Create: `desktop/feishu/longconn.go`
- Create: `desktop/feishu/longconn_test.go`

- [ ] **Step 1: Add SDK dependency**

```bash
go get github.com/larksuite/oapi-sdk-go/v3@latest
go mod tidy
```

Verify only `larksuite/oapi-sdk-go` and its transitive deps were added (no surprise upgrades to unrelated packages).

- [ ] **Step 2: Write the failing test (skeleton driver)**

```go
// desktop/feishu/longconn_test.go
package feishu

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLongConn_NewAndClose(t *testing.T) {
	lc := NewLongConn(LongConnConfig{
		AppID:     "cli_x",
		AppSecret: "s",
		// Empty backoff makes Close fast even on a failed Start.
		Backoff: BackoffConfig{Initial: 10 * time.Millisecond, Max: 50 * time.Millisecond},
	})
	if err := lc.Close(context.Background()); err != nil {
		t.Fatalf("Close on never-started: %v", err)
	}
}

func TestLongConn_StartCallsSDKFactory(t *testing.T) {
	var called int
	factory := func(cfg LongConnConfig) (longConnRuntime, error) {
		called++
		return &fakeRuntime{}, nil
	}
	lc := newLongConnWithFactory(LongConnConfig{AppID: "cli_x", AppSecret: "s"}, factory)
	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if called != 1 {
		t.Fatalf("factory called %d times", called)
	}
	_ = lc.Close(context.Background())
}

func TestLongConn_StartFailure(t *testing.T) {
	bad := errors.New("boom")
	factory := func(cfg LongConnConfig) (longConnRuntime, error) {
		return nil, bad
	}
	lc := newLongConnWithFactory(LongConnConfig{AppID: "cli_x", AppSecret: "s"}, factory)
	err := lc.Start(context.Background())
	if !errors.Is(err, bad) {
		t.Fatalf("want bad error, got %v", err)
	}
}

// fakeRuntime is the SDK boundary stub used by the longconn tests.
type fakeRuntime struct {
	closed bool
}

func (r *fakeRuntime) Run(ctx context.Context) error    { <-ctx.Done(); return nil }
func (r *fakeRuntime) Close(ctx context.Context) error  { r.closed = true; return nil }
```

- [ ] **Step 3: Implement `desktop/feishu/longconn.go` skeleton**

```go
// desktop/feishu/longconn.go
//
// Feishu long-connection subscriber. In local mode (no relay) this is
// the desktop's only way to receive Feishu inbound events: bind-message
// from "/bind XXX" private messages, and card.action.trigger from the
// "确认" button on outbound cards.
//
// This task ships the skeleton + dependency-injection boundary so the
// tests don't pull in the larksuite SDK directly. Tasks 16 and 17 fill
// in event routing and reconnect logic.
package feishu

import (
	"context"
	"errors"
	"sync"
	"time"
)

type BackoffConfig struct {
	Initial time.Duration
	Max     time.Duration
}

type LongConnConfig struct {
	AppID     string
	AppSecret string
	Backoff   BackoffConfig

	// OnBindMessage is invoked when the user sends "/bind XXXX" to the
	// bot in private chat. Local mode uses this to flip the binding
	// state. nil → events dropped.
	OnBindMessage func(ctx context.Context, senderOpenID, text string)

	// OnCardAction is invoked on card.action.trigger events. nil → events dropped.
	OnCardAction func(ctx context.Context, sessionID, kind, event, operatorOpenID string)
}

// longConnRuntime is the boundary the SDK adapter satisfies. Production
// uses an internal larkRuntime wrapping github.com/larksuite/oapi-sdk-go/v3.
type longConnRuntime interface {
	Run(ctx context.Context) error
	Close(ctx context.Context) error
}

type runtimeFactory func(cfg LongConnConfig) (longConnRuntime, error)

// LongConn manages a single long-connection client across reconnects.
type LongConn struct {
	cfg     LongConnConfig
	factory runtimeFactory

	mu      sync.Mutex
	cancel  context.CancelFunc
	rt      longConnRuntime
	started bool
}

// NewLongConn returns a LongConn using the production SDK-backed
// factory. Tasks 16/17 implement the factory body.
func NewLongConn(cfg LongConnConfig) *LongConn {
	return newLongConnWithFactory(cfg, newLarkRuntime)
}

func newLongConnWithFactory(cfg LongConnConfig, factory runtimeFactory) *LongConn {
	if cfg.Backoff.Initial == 0 {
		cfg.Backoff.Initial = time.Second
	}
	if cfg.Backoff.Max == 0 {
		cfg.Backoff.Max = 5 * time.Minute
	}
	return &LongConn{cfg: cfg, factory: factory}
}

// Start opens the long connection and begins delivering events. Returns
// the constructor error if the SDK rejects the credentials up-front;
// runtime errors after that are handled by the internal reconnect loop
// (Task 17).
func (l *LongConn) Start(ctx context.Context) error {
	l.mu.Lock()
	if l.started {
		l.mu.Unlock()
		return errors.New("longconn: already started")
	}
	rt, err := l.factory(l.cfg)
	if err != nil {
		l.mu.Unlock()
		return err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.rt = rt
	l.started = true
	l.mu.Unlock()
	go func() { _ = rt.Run(runCtx) }()
	return nil
}

func (l *LongConn) Close(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	if l.rt != nil {
		err := l.rt.Close(ctx)
		l.rt = nil
		l.started = false
		return err
	}
	return nil
}

// newLarkRuntime is the production factory. Task 16 fills the body.
func newLarkRuntime(cfg LongConnConfig) (longConnRuntime, error) {
	return nil, errors.New("longconn: lark runtime not yet implemented (Task 16)")
}
```

- [ ] **Step 4: Run tests**

```bash
go build ./...
go test ./desktop/feishu/ -run TestLongConn -v
```

Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/longconn.go desktop/feishu/longconn_test.go go.mod go.sum
git commit -m "feat(desktop/feishu): LongConn skeleton + SDK dependency (oapi-sdk-go/v3)"
```

---

## Task 16: Lark SDK runtime — event routing

**Files:**
- Modify: `desktop/feishu/longconn.go`
- Modify: `desktop/feishu/longconn_test.go`

- [ ] **Step 1: Append event-routing tests**

```go
func TestLongConn_OnBindMessage_RoutesText(t *testing.T) {
	gotSender := ""
	gotText := ""
	cfg := LongConnConfig{
		AppID: "x", AppSecret: "y",
		OnBindMessage: func(ctx context.Context, senderOpenID, text string) {
			gotSender = senderOpenID
			gotText = text
		},
	}
	// Use the production newLarkRuntime via a synthetic event injector.
	r := newTestableRuntime(cfg)
	r.injectIMMessage("ou_sender", "/bind ABC123")
	if gotSender != "ou_sender" || gotText != "/bind ABC123" {
		t.Fatalf("not routed: sender=%q text=%q", gotSender, gotText)
	}
}

func TestLongConn_OnCardAction_RoutesAck(t *testing.T) {
	called := 0
	gotSID := ""
	cfg := LongConnConfig{
		AppID: "x", AppSecret: "y",
		OnCardAction: func(ctx context.Context, sessionID, kind, event, operatorOpenID string) {
			called++
			gotSID = sessionID
		},
	}
	r := newTestableRuntime(cfg)
	r.injectCardAction("ou_op", "sid-99", "ack", "command_finished")
	if called != 1 || gotSID != "sid-99" {
		t.Fatalf("expected one card-action callback with sid-99, got %d / %q", called, gotSID)
	}
}
```

- [ ] **Step 2: Implement the SDK-backed runtime**

Replace the `newLarkRuntime` stub in `desktop/feishu/longconn.go` with a real
larksuite SDK wrapper. The minimal SDK surface needed:

- `lark.NewClient(appID, appSecret, lark.WithLogLevel(...))`
- `larkws.NewClient(appID, appSecret, larkws.WithEventHandler(...))` (the
  long-connection client lives in the SDK's `larkws` subpackage; verify
  the import path against your installed version)
- `dispatcher.NewEventDispatcher("", "")` to bind handlers per event type
- `OnP2ImMessageReceiveV1` for IM bind messages
- `OnP2CardActionTrigger` for card action callbacks

```go
// inside desktop/feishu/longconn.go (replace the stub newLarkRuntime)

import (
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	dispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
)

type larkRuntime struct {
	cli     *larkws.Client
	handler *dispatcher.EventDispatcher
}

func newLarkRuntime(cfg LongConnConfig) (longConnRuntime, error) {
	disp := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, ev *larkim.P2MessageReceiveV1) error {
			if cfg.OnBindMessage == nil {
				return nil
			}
			text := extractTextFromP2Message(ev)
			senderID := derefSenderOpenID(ev)
			if text == "" || senderID == "" {
				return nil
			}
			cfg.OnBindMessage(ctx, senderID, text)
			return nil
		}).
		OnP2CardActionTrigger(func(ctx context.Context, ev *lark.CardActionTriggerEvent) (*lark.CardActionTriggerResponse, error) {
			if cfg.OnCardAction == nil {
				return nil, nil
			}
			kind, sid, event, op := parseCardActionEvent(ev)
			cfg.OnCardAction(ctx, sid, kind, event, op)
			return nil, nil // SDK will use the ack-update card the caller writes via Service.
		})

	wsClient := larkws.NewClient(cfg.AppID, cfg.AppSecret,
		larkws.WithEventHandler(disp),
		larkws.WithAutoReconnect(true),
	)
	return &larkRuntime{cli: wsClient, handler: disp}, nil
}

func (r *larkRuntime) Run(ctx context.Context) error { return r.cli.Start(ctx) }
func (r *larkRuntime) Close(_ context.Context) error { /* SDK doesn't expose Close cleanly; Run returns when ctx is done */ return nil }

// Helpers — extract text + sender open_id from the SDK's message event.
func extractTextFromP2Message(ev *larkim.P2MessageReceiveV1) string {
	if ev == nil || ev.Event == nil || ev.Event.Message == nil || ev.Event.Message.Content == nil {
		return ""
	}
	// Content is a JSON string like {"text":"..."}
	type t struct{ Text string `json:"text"` }
	var inner t
	if err := json.Unmarshal([]byte(*ev.Event.Message.Content), &inner); err != nil {
		return ""
	}
	return inner.Text
}

func derefSenderOpenID(ev *larkim.P2MessageReceiveV1) string {
	if ev == nil || ev.Event == nil || ev.Event.Sender == nil || ev.Event.Sender.SenderId == nil || ev.Event.Sender.SenderId.OpenId == nil {
		return ""
	}
	return *ev.Event.Sender.SenderId.OpenId
}

func parseCardActionEvent(ev *lark.CardActionTriggerEvent) (kind, sid, event, op string) {
	// ev.Event.Action.Value is the "value" field we set on the button
	// in card.go. Type assertion through map[string]any.
	if ev == nil || ev.Event == nil {
		return "", "", "", ""
	}
	if v, ok := ev.Event.Action.Value.(map[string]any); ok {
		kind, _ = v["kind"].(string)
		sid, _ = v["session_id"].(string)
		event, _ = v["event"].(string)
	}
	if ev.Event.Operator != nil {
		op = ev.Event.Operator.OpenID
	}
	return
}
```

NOTE: the exact field names and import paths above are from a recent
version of `oapi-sdk-go/v3`. Validate against the version pinned by
Task 15 and adjust accordingly. The shape (event dispatcher with two
handlers; WS client with auto-reconnect) is stable.

Also add a `testableRuntime` helper used by the new tests to inject
synthetic events without spinning up the SDK:

```go
type testableRuntime struct{ cfg LongConnConfig }

func newTestableRuntime(cfg LongConnConfig) *testableRuntime { return &testableRuntime{cfg: cfg} }

func (r *testableRuntime) injectIMMessage(senderOpenID, text string) {
	if r.cfg.OnBindMessage != nil {
		r.cfg.OnBindMessage(context.Background(), senderOpenID, text)
	}
}
func (r *testableRuntime) injectCardAction(operatorOpenID, sessionID, kind, event string) {
	if r.cfg.OnCardAction != nil {
		r.cfg.OnCardAction(context.Background(), sessionID, kind, event, operatorOpenID)
	}
}
```

- [ ] **Step 3: Run tests + build**

```bash
go build ./...
go test ./desktop/feishu/ -v
```

Expected: all new + existing tests PASS. Build may surface SDK version
mismatches — adjust import paths to match the pinned version.

- [ ] **Step 4: Commit**

```bash
git add desktop/feishu/longconn.go desktop/feishu/longconn_test.go
git commit -m "feat(desktop/feishu): wire larksuite SDK — IM message + card action routing"
```

---

## Task 17: Long-conn reconnect + auth-class disable

**Files:**
- Modify: `desktop/feishu/longconn.go`
- Modify: `desktop/feishu/longconn_test.go`

- [ ] **Step 1: Append the reconnect tests**

```go
func TestLongConn_ReconnectBackoff(t *testing.T) {
	// Three failures then success.
	attempts := 0
	factory := func(cfg LongConnConfig) (longConnRuntime, error) {
		attempts++
		if attempts < 4 {
			return &flakyRuntime{returns: errors.New("conn drop")}, nil
		}
		return &fakeRuntime{}, nil
	}
	cfg := LongConnConfig{
		AppID: "x", AppSecret: "y",
		Backoff: BackoffConfig{Initial: 10 * time.Millisecond, Max: 50 * time.Millisecond},
	}
	lc := newLongConnWithFactory(cfg, factory)
	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Give the reconnect loop time to climb the backoff.
	time.Sleep(300 * time.Millisecond)
	if attempts < 4 {
		t.Fatalf("expected ≥4 attempts, got %d", attempts)
	}
	_ = lc.Close(context.Background())
}

func TestLongConn_AuthClassDisablesAndStops(t *testing.T) {
	var disabled int32
	cfg := LongConnConfig{
		AppID: "x", AppSecret: "y",
		Backoff: BackoffConfig{Initial: 10 * time.Millisecond, Max: 20 * time.Millisecond},
		OnAuthClassFailure: func(ctx context.Context, _ error) {
			atomic.StoreInt32(&disabled, 1)
		},
	}
	factory := func(cfg LongConnConfig) (longConnRuntime, error) {
		return &flakyRuntime{returns: &authError{}}, nil
	}
	lc := newLongConnWithFactory(cfg, factory)
	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Wait a bit; the loop should stop after the first auth-class fail.
	time.Sleep(80 * time.Millisecond)
	if atomic.LoadInt32(&disabled) != 1 {
		t.Fatalf("expected OnAuthClassFailure to fire")
	}
	_ = lc.Close(context.Background())
}

type flakyRuntime struct{ returns error }

func (r *flakyRuntime) Run(ctx context.Context) error   { return r.returns }
func (r *flakyRuntime) Close(_ context.Context) error   { return nil }
```

Add `"sync/atomic"` to the test file imports.

- [ ] **Step 2: Add reconnect loop + auth-class hook**

Edit `desktop/feishu/longconn.go`:

```go
// Append to LongConnConfig:
OnAuthClassFailure func(ctx context.Context, err error)

// Replace Start with a wrapped loop. Refactor the existing inline
// `go func() { _ = rt.Run(runCtx) }()` into a runLoop helper:

func (l *LongConn) Start(ctx context.Context) error {
	l.mu.Lock()
	if l.started {
		l.mu.Unlock()
		return errors.New("longconn: already started")
	}
	runCtx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.started = true
	l.mu.Unlock()
	go l.runLoop(runCtx)
	return nil
}

func (l *LongConn) runLoop(ctx context.Context) {
	backoff := l.cfg.Backoff.Initial
	for {
		if ctx.Err() != nil {
			return
		}
		rt, err := l.factory(l.cfg)
		if err != nil {
			if isAuthClass(err) {
				if l.cfg.OnAuthClassFailure != nil {
					l.cfg.OnAuthClassFailure(ctx, err)
				}
				return
			}
			// transient: retry with backoff
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = nextBackoff(backoff, l.cfg.Backoff.Max)
			continue
		}
		l.mu.Lock()
		l.rt = rt
		l.mu.Unlock()
		err = rt.Run(ctx)
		_ = rt.Close(context.Background())
		if ctx.Err() != nil {
			return
		}
		if isAuthClass(err) {
			if l.cfg.OnAuthClassFailure != nil {
				l.cfg.OnAuthClassFailure(ctx, err)
			}
			return
		}
		// connection drop → backoff + retry
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = nextBackoff(backoff, l.cfg.Backoff.Max)
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	return next
}

func isAuthClass(err error) bool {
	var ac interface{ IsFeishuAuthClassError() bool }
	return errors.As(err, &ac) && ac.IsFeishuAuthClassError()
}
```

`Start` should NOT return an error from the SDK factory anymore — the
production reconnect path silently retries. The earlier test
`TestLongConn_StartFailure` no longer applies; delete it and replace
with the new `TestLongConn_AuthClassDisablesAndStops` (already in
Step 1).

- [ ] **Step 3: Run tests**

```bash
go test ./desktop/feishu/ -v
```

Expected: long-conn tests + previously written tests all PASS.

- [ ] **Step 4: Commit**

```bash
git add desktop/feishu/longconn.go desktop/feishu/longconn_test.go
git commit -m "feat(desktop/feishu): long-conn reconnect with backoff + auth-class halt"
```

---

## Task 18: `Service` top-level assembly + mode selection

**Files:**
- Create: `desktop/feishu/service.go`
- Create: `desktop/feishu/service_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// desktop/feishu/service_test.go
package feishu

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

func TestService_RelayMode_SelectsRelayStore(t *testing.T) {
	svc, _ := NewService(ServiceConfig{
		Mode:        ModeRelay,
		RelayURL:    "http://example",
		RelayToken:  func() string { return "tok" },
	})
	if _, ok := svc.store.(*RelayBackedBindingStore); !ok {
		t.Fatalf("relay mode should pick relay store, got %T", svc.store)
	}
}

func TestService_LocalMode_SelectsLocalStore(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{
		Mode:        ModeLocal,
		FeishuBase:  "https://open.feishu.cn",
	})
	if _, ok := svc.store.(*LocalKeychainBindingStore); !ok {
		t.Fatalf("local mode should pick local store, got %T", svc.store)
	}
}

func TestService_DispatchExposesDispatcher(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{Mode: ModeLocal, FeishuBase: "https://open.feishu.cn"})
	if svc.Dispatcher() == nil {
		t.Fatalf("Service.Dispatcher() must return non-nil")
	}
}

func TestService_HookServerExposed(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{Mode: ModeLocal, FeishuBase: "https://open.feishu.cn"})
	addr, _, err := svc.HookServer().Start()
	if err != nil {
		t.Fatalf("hook server start: %v", err)
	}
	if addr == "" {
		t.Fatalf("expected bound addr")
	}
}

// stubSessionLookup is the minimal SessionLookup used by the assembly tests.
type stubSessionLookup struct{}

func (stubSessionLookup) Exists(uuid.UUID) bool { return true }

// Silence unused import warnings on stubSessionLookup if Go cleans imports.
var _ SessionLookup = stubSessionLookup{}

func TestService_HandleCardAction_RoutesToInternalEvent(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{Mode: ModeLocal, FeishuBase: "https://open.feishu.cn"})
	ack := svc.RenderAck("command_finished", "session-123")
	if ack.Card == nil {
		t.Fatalf("RenderAck must return a card payload")
	}
}

// Compile-time guard: Service must satisfy SessionLookup (the dispatcher
// needs Exists for the hook server). If you wire a real session registry
// later, swap this for that registry.
func TestService_ExistsSatisfiesLookup(t *testing.T) {
	var _ SessionLookup = (*Service)(nil)
}

// Smoke: in relay mode without a RelayToken func, NewService rejects.
func TestService_RelayMode_RequiresTokenFn(t *testing.T) {
	_, err := NewService(ServiceConfig{Mode: ModeRelay, RelayURL: "http://x"})
	if err == nil {
		t.Fatalf("expected error when RelayToken is nil in relay mode")
	}
}

func _ctx() context.Context { return context.Background() }
```

- [ ] **Step 2: Confirm failure**

Run: `go test ./desktop/feishu/ -run TestService`
Expected: fail — undefined `NewService`, `Service`, etc.

- [ ] **Step 3: Implement `desktop/feishu/service.go`**

```go
// desktop/feishu/service.go
package feishu

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	internalfeishu "github.com/attson/atterm/internal/feishu"
)

type Mode int

const (
	ModeRelay Mode = iota
	ModeLocal
)

// ServiceConfig assembles all the moving parts at startup.
type ServiceConfig struct {
	Mode Mode

	// Relay mode:
	RelayURL   string
	RelayToken func() string

	// Local mode:
	FeishuBase string // typically "https://open.feishu.cn"
	HTTPClient *http.Client

	// Optional: override the clock used by the dispatcher for tests.
	Now func() int64

	// Sessions is the session registry the hook server uses for
	// existence checks. May be nil for stand-alone tests; production
	// passes the desktop's session list wrapper.
	Sessions SessionLookup
}

// Service is the top-level façade that desktop/app.go constructs once
// at startup. It owns the dispatcher, hook server, long-conn (local
// mode only), and the IM client.
type Service struct {
	cfg ServiceConfig

	store      BindingStore
	tokenSrc   TokenSource
	imClient   IMClient
	dispatcher *Dispatcher
	hookSrv    *HookServer
	longConn   *LongConn // nil in relay mode
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Mode == ModeRelay && cfg.RelayToken == nil {
		return nil, errors.New("desktop/feishu: relay mode requires RelayToken func")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.FeishuBase == "" {
		cfg.FeishuBase = "https://open.feishu.cn"
	}

	var store BindingStore
	var ts TokenSource
	switch cfg.Mode {
	case ModeRelay:
		store = NewRelayBackedBindingStore(cfg.RelayURL, cfg.RelayToken)
		ts = NewRelayBorrowedTokenSource(cfg.RelayURL, cfg.RelayToken)
	case ModeLocal:
		ls := NewLocalKeychainBindingStore()
		store = ls
		ts = NewLocalTenantTokenSource(ls, cfg.FeishuBase, cfg.HTTPClient, func() time.Time { return time.Now() })
	default:
		return nil, errors.New("desktop/feishu: invalid mode")
	}

	im := &authClassAdaptingClient{inner: internalfeishu.NewClient(cfg.FeishuBase, cfg.HTTPClient)}

	d := NewDispatcher(DispatcherConfig{
		Store: store,
		Token: ts,
		IM:    im,
		Now:   cfg.Now,
	})

	sessions := cfg.Sessions
	if sessions == nil {
		sessions = noOpSessionLookup{}
	}
	hookSrv := NewHookServer(d, sessions)

	var lc *LongConn
	if cfg.Mode == ModeLocal {
		// Long-conn is set up lazily by the caller after the user has saved
		// credentials. Start with a nil LongConn; the wails settings panel
		// calls EnsureLongConn after credentials are persisted.
	}

	return &Service{
		cfg: cfg, store: store, tokenSrc: ts,
		imClient: im, dispatcher: d, hookSrv: hookSrv, longConn: lc,
	}, nil
}

func (s *Service) Store() BindingStore           { return s.store }
func (s *Service) Dispatcher() *Dispatcher        { return s.dispatcher }
func (s *Service) HookServer() *HookServer        { return s.hookSrv }
func (s *Service) Token() TokenSource             { return s.tokenSrc }

// Exists allows Service to satisfy SessionLookup for embedded use.
// Production callers pass an external implementation via ServiceConfig.
func (s *Service) Exists(uuid.UUID) bool { return true }

// EnsureLongConn starts the long-connection if in local mode and not
// already running. No-op in relay mode.
func (s *Service) EnsureLongConn(ctx context.Context) error {
	if s.cfg.Mode != ModeLocal {
		return nil
	}
	v, err := s.store.Get(ctx)
	if err != nil {
		return err
	}
	if v.AppID == "" || v.AppSecret == "" {
		return errors.New("desktop/feishu: credentials missing")
	}
	if s.longConn != nil {
		return nil
	}
	lc := NewLongConn(LongConnConfig{
		AppID:     v.AppID,
		AppSecret: v.AppSecret,
		Backoff:   BackoffConfig{Initial: time.Second, Max: 5 * time.Minute},
		OnBindMessage: func(ctx context.Context, senderOpenID, text string) {
			s.handleBindMessage(ctx, senderOpenID, text)
		},
		OnCardAction: func(ctx context.Context, sessionID, kind, event, operatorOpenID string) {
			s.handleCardAction(ctx, sessionID, kind, event)
		},
		OnAuthClassFailure: func(ctx context.Context, _ error) {
			_ = s.store.SetDisabled(ctx)
		},
	})
	if err := lc.Start(ctx); err != nil {
		return err
	}
	s.longConn = lc
	return nil
}

// RenderAck returns the ack-update card payload that the long-conn's
// card-action handler sends back to Feishu. Pulled out so the test
// can exercise it without the SDK.
func (s *Service) RenderAck(event, sessionID string) internalfeishu.AckResponse {
	return internalfeishu.RenderAckUpdateCard(internalfeishu.AckUpdateInput{
		Event: event, SessionID: sessionID,
	})
}

// handleBindMessage is a tiny in-memory short-code matcher used in
// local mode (relay mode goes through the relay's bindings/me/begin-pair
// path instead).
func (s *Service) handleBindMessage(ctx context.Context, senderOpenID, text string) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "/bind ") {
		return
	}
	code := strings.TrimSpace(strings.TrimPrefix(t, "/bind "))
	if !s.consumePending(code) {
		// reply not handled in this task — the user resubmits.
		return
	}
	_ = s.store.SetBound(ctx, senderOpenID)
}

func (s *Service) handleCardAction(ctx context.Context, sessionID, kind, event string) {
	// Stub for now; long-conn already echoes the updated card back via
	// the SDK ack channel. The dispatch + render lives there.
	_ = sessionID
	_ = kind
	_ = event
}

// In-memory short-code table for local mode.
var (
	pendingMu      sync.Mutex
	pendingCodes   = map[string]int64{}
)

// IssuePending generates a 6-char short-code valid for 15 minutes.
func (s *Service) IssuePending() string {
	code := internalfeishuPairCode()
	pendingMu.Lock()
	pendingCodes[code] = time.Now().Add(15 * time.Minute).Unix()
	pendingMu.Unlock()
	return code
}

func (s *Service) consumePending(code string) bool {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	exp, ok := pendingCodes[code]
	if !ok || exp < time.Now().Unix() {
		delete(pendingCodes, code)
		return false
	}
	delete(pendingCodes, code)
	return true
}

// internalfeishuPairCode shells out to internal/userstore via a 6-char
// alphabet (excludes confusable chars). We don't import userstore from
// desktop, so duplicate the alphabet locally — keep in sync.
func internalfeishuPairCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 6)
	rb := make([]byte, 6)
	if _, err := cryptoRandRead(rb); err != nil {
		panic(err)
	}
	for i, b := range rb {
		buf[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(buf)
}

// noOpSessionLookup keeps standalone tests + simple wiring happy.
type noOpSessionLookup struct{}

func (noOpSessionLookup) Exists(uuid.UUID) bool { return true }

// authClassAdaptingClient promotes internal/feishu.AuthClassError to
// satisfy the desktop dispatcher's IsFeishuAuthClassError contract.
type authClassAdaptingClient struct {
	inner *internalfeishu.Client
}

func (c *authClassAdaptingClient) SendInteractiveToOpenID(ctx context.Context, tok, open string, body []byte) error {
	return c.adapt(c.inner.SendInteractiveToOpenID(ctx, tok, open, body))
}
func (c *authClassAdaptingClient) SendTextToOpenID(ctx context.Context, tok, open, text string) error {
	return c.adapt(c.inner.SendTextToOpenID(ctx, tok, open, text))
}
func (c *authClassAdaptingClient) adapt(err error) error {
	if err == nil {
		return nil
	}
	if internalfeishu.IsAuthClassError(err) {
		return &authClassErr{inner: err}
	}
	return err
}

type authClassErr struct{ inner error }

func (e *authClassErr) Error() string                  { return e.inner.Error() }
func (e *authClassErr) Unwrap() error                  { return e.inner }
func (e *authClassErr) IsFeishuAuthClassError() bool   { return true }
```

You'll need a small `cryptoRandRead` import line — just call
`crypto/rand`'s `Read` directly:

```go
import (
	cryptorand "crypto/rand"
	"sync"
)

func cryptoRandRead(b []byte) (int, error) { return cryptorand.Read(b) }
```

Also delete the now-duplicate `_ctx` helper in the test if `goimports`
doesn't drop it.

- [ ] **Step 4: Run tests**

Run: `go test ./desktop/feishu/... -v`
Expected: all pass. Adjust import paths if the larkws import added in
Task 16 breaks tests without a real Feishu account (tests should use
`newTestableRuntime`, not the SDK runtime).

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/service.go desktop/feishu/service_test.go
git commit -m "feat(desktop/feishu): Service — mode selection + assembly + ack render"
```

---

## Task 19: `Session.OnTaskStateChange` callback

**Files:**
- Modify: `internal/session/session.go`
- Modify: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/session/session_test.go`:

```go
func TestSession_OnTaskStateChange_FiresOnTransitions(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	var transitions []struct{ Prev, Next string }
	var mu sync.Mutex
	s.SetOnTaskStateChange(func(_ uuid.UUID, prev, next string, _ TaskMeta) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, struct{ Prev, Next string }{prev, next})
	})

	// Simulate running → completed via OSC 133 D
	prompt := "$ "
	s.PushOut([]byte(prompt + "\x1b]133;A\x07command\x1b]133;C\x07output\x1b]133;D;0\x07"))

	mu.Lock()
	defer mu.Unlock()
	// Expect at least one running → completed transition.
	found := false
	for _, tr := range transitions {
		if tr.Next == proto.TaskStateCompleted {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a completed transition; saw %+v", transitions)
	}
}

func TestSession_OnTaskStateChange_FiresOnWaiting(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	fired := make(chan struct{}, 1)
	s.SetOnTaskStateChange(func(_ uuid.UUID, prev, next string, _ TaskMeta) {
		if next == proto.TaskStateWaitingInput {
			select {
			case fired <- struct{}{}:
			default:
			}
		}
	})
	// Feed bytes that look like a "do you want? (y/N)" prompt.
	s.PushOut([]byte("Continue? (y/N) "))
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatalf("expected WaitingInput transition")
	}
}
```

You'll need `"sync"` and `"time"` and the `proto` and `uuid` packages
already imported by this file.

- [ ] **Step 2: Confirm failure**

Run: `go test ./internal/session/ -run TestSession_OnTaskStateChange`
Expected: fail — undefined `SetOnTaskStateChange` and `TaskMeta`.

- [ ] **Step 3: Add field + setter + fire sites in `internal/session/session.go`**

Add to the Session struct (next to the existing `onAIClassified` field
around line 145):

```go
// onTaskStateChange fires once per state transition observed within
// the session. The callback runs while the session mutex is held —
// implementers must NOT call back into Session methods that take s.mu.
// The desktop dispatcher uses this to translate transitions into
// Feishu IM sends.
onTaskStateChange func(sid uuid.UUID, prev, next string, meta TaskMeta)
```

Define `TaskMeta` at package-level:

```go
// TaskMeta carries the fields the OnTaskStateChange caller needs that
// are NOT already in proto.SessionInfo. Right now this is just the
// SealedBody from the most recent CommandEvent (used for E2EE
// command_finished cards). Add fields here as later use cases need them.
type TaskMeta struct {
	ExitCode   int
	ElapsedMS  int
	Label      string
	SealedBody []byte
}
```

Add a setter modeled on `SetOnAIClassified`:

```go
// SetOnTaskStateChange registers a callback that fires on every
// task-state transition observed inside the session. The callback
// receives (session_id, prev_state, next_state, extracted_meta).
func (s *Session) SetOnTaskStateChange(fn func(sid uuid.UUID, prev, next string, meta TaskMeta)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTaskStateChange = fn
}
```

Inside each existing block that mutates `s.meta.TaskState`, add a
helper call right after the assignment. Look for sites like:

```go
s.meta.TaskState = proto.TaskStateWaitingInput
```

and turn them into:

```go
prev := s.meta.TaskState
s.meta.TaskState = proto.TaskStateWaitingInput
s.fireTaskStateLocked(prev, proto.TaskStateWaitingInput, TaskMeta{})
```

Add the helper at the bottom of the file:

```go
// fireTaskStateLocked invokes onTaskStateChange while s.mu is already
// held. Skips the callback if it isn't registered or if the state did
// not actually change.
func (s *Session) fireTaskStateLocked(prev, next string, meta TaskMeta) {
	if prev == next || s.onTaskStateChange == nil {
		return
	}
	s.onTaskStateChange(s.ID, prev, next, meta)
}
```

Cover at minimum: the running→completed transition in
`applyOSC133Locked` (capture `ExitCode` from `parseOSC133Exit`), the
running→failed transition (same), and the running→waiting_input
transition both in the silence-driven `fireWaitingLocked` path and in
the `looksLikeWaitingInput` fast-path on line ~781.

For command_finished, also populate `ElapsedMS` (= now − command start)
and `Label` (= the previously stored CommandLine for that run, which
session.go already tracks for OSC 133).

- [ ] **Step 4: Run tests**

```bash
go test ./internal/session/ -v
```

Expected: PASS — both new + all existing tests stay green.

- [ ] **Step 5: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "feat(session): OnTaskStateChange callback + TaskMeta for desktop dispatch"
```

---

## Task 20: PTY env injection + relay_host wiring

**Files:**
- Modify: `desktop/relay_host.go`

- [ ] **Step 1: Locate the env-assembly point**

```bash
grep -n "terminalEnvForXterm\|cfg.Env" desktop/relay_host.go
```

Expected: a single call to `terminalEnvForXterm(os.Environ())` around
line 381 producing the `env` slice passed into the PTY config.

- [ ] **Step 2: Add helpers + extend env**

Add a small helper at the top of `desktop/relay_host.go` (or in a
new sibling file if this file is getting unwieldy):

```go
// appendFeishuHookEnv adds ATTERM_SESSION_ID + ATTERM_HOOK_ENDPOINT to
// a process env slice. The hook endpoint comes from the desktop's
// in-process FeishuService (set up in Task 21); empty endpoint = skip.
func appendFeishuHookEnv(env []string, sessionID, hookEndpoint string) []string {
	if sessionID != "" {
		env = append(env, "ATTERM_SESSION_ID="+sessionID)
	}
	if hookEndpoint != "" {
		env = append(env, "ATTERM_HOOK_ENDPOINT="+hookEndpoint)
	}
	return env
}
```

Find the env construction around line 381:

```go
env := terminalEnvForXterm(os.Environ())
```

Extend it to thread the two vars through. The caller (the session
spawn site) needs access to the session UUID and the hook endpoint. If
the spawn function doesn't have those, plumb them in via the function
signature — typically `RelayHost.spawnSession` (search for it) carries
a session struct that already has the UUID.

For the hook endpoint URL, add a field on `RelayHost` (or whatever
struct owns the spawn) like:

```go
// FeishuHookEndpoint is set by app.go at startup once the HookServer
// has bound a port. Empty when feishu is disabled or not yet started.
FeishuHookEndpoint string
```

Then replace the env line with:

```go
env := terminalEnvForXterm(os.Environ())
env = appendFeishuHookEnv(env, sess.ID.String(), p.host.FeishuHookEndpoint)
```

(`sess.ID` and `p.host` are placeholders — adapt to the actual variable
names in the function.)

- [ ] **Step 3: Subscribe to session task-state changes**

In the same function that spawns the PTY and creates the Session
(grep for `session.New(`), immediately after the session is created
register the dispatch callback:

```go
sess.SetOnTaskStateChange(func(sid uuid.UUID, prev, next string, meta session.TaskMeta) {
	switch next {
	case proto.TaskStateCompleted, proto.TaskStateFailed:
		p.host.FeishuDispatcher.DispatchCommandFinished(context.Background(),
			feishu.CommandFinishedEvent{
				SessionID:  sid,
				ExitCode:   meta.ExitCode,
				ElapsedMS:  meta.ElapsedMS,
				Label:      meta.Label,
				SealedBody: meta.SealedBody,
			})
	case proto.TaskStateWaitingInput:
		p.host.FeishuDispatcher.DispatchWaitingInput(context.Background(),
			feishu.WaitingInputDispatchEvent{
				SessionID: sid,
				Source:    feishu.WaitingSourceHeuristic,
			})
	}
})
```

Add an import for `github.com/attson/atterm/desktop/feishu`. Add a
field on the host:

```go
// FeishuDispatcher is set by app.go at startup. nil → no-op.
FeishuDispatcher *feishu.Dispatcher
```

Guard the callback against nil:

```go
if p.host.FeishuDispatcher == nil {
	return
}
```

- [ ] **Step 4: Build + run**

```bash
go build ./...
go test ./...
```

Expected: build OK; nothing should break tests since the host fields
default to nil/empty.

- [ ] **Step 5: Commit**

```bash
git add desktop/relay_host.go
git commit -m "feat(desktop): inject ATTERM_SESSION_ID + hook endpoint into PTY env; wire OnTaskStateChange"
```

---

## Task 21: `app.go` assembly + minimal frontend wiring

**Files:**
- Modify: `desktop/app.go`
- Modify (or create): `desktop/frontend/src/shared/api/feishu.ts`
- Modify (or create): `desktop/frontend/src/components/SettingsFeishu.vue`

The frontend changes here are intentionally minimal — just enough for
the user to enter credentials in local mode and start the pair flow.
Full polish lives in a follow-up PR.

- [ ] **Step 1: Construct the Service in `desktop/app.go`**

Find `App.startup` (or the equivalent — search for `OnStartup`). After
the relay client / OPAQUE session is established (so the relay token
function is available), add:

```go
// feishuMode picks the correct mode based on relay-login state.
mode := feishu.ModeLocal
if a.relay != nil && a.relay.IsLoggedIn() {
	mode = feishu.ModeRelay
}

svc, err := feishu.NewService(feishu.ServiceConfig{
	Mode:       mode,
	RelayURL:   a.cfg.RelayBaseURL,
	RelayToken: func() string { return a.relay.SessionToken() },
	FeishuBase: "https://open.feishu.cn",
	Sessions:   a.host, // implements Exists(uuid.UUID) — see Task 22
})
if err != nil {
	log.Printf("feishu disabled: %v", err)
} else {
	addr, _, err := svc.HookServer().Start()
	if err != nil {
		log.Printf("feishu hook server: %v", err)
	} else {
		hookEndpoint := "http://" + addr + "/atterm-hook/notify"
		_ = feishu.WriteEndpointFile(hookEndpoint)
		a.host.FeishuHookEndpoint = hookEndpoint
		a.host.FeishuDispatcher = svc.Dispatcher()
		// Local mode: kick off long-conn lazily once credentials exist
		_ = svc.EnsureLongConn(ctx)
	}
	a.feishuService = svc
}
```

On shutdown (find `OnShutdown` or similar):

```go
if a.feishuService != nil {
	_ = feishu.DeleteEndpointFile()
}
```

- [ ] **Step 2: Add the wails bindings**

Wails auto-generates bindings from struct methods. Add (on the same
struct that hosts `OnStartup`):

```go
// GetFeishuStatus surfaces the current mode + bound state to the UI.
func (a *App) GetFeishuStatus(ctx context.Context) (FeishuStatusResp, error) {
	if a.feishuService == nil {
		return FeishuStatusResp{Enabled: false}, nil
	}
	v, err := a.feishuService.Store().Get(ctx)
	if errors.Is(err, feishu.ErrLocalBindingNotFound) {
		return FeishuStatusResp{
			Enabled: true,
			Mode:    modeName(a.feishuService),
			Bound:   false,
		}, nil
	}
	if err != nil {
		return FeishuStatusResp{}, err
	}
	return FeishuStatusResp{
		Enabled: true,
		Mode:    modeName(a.feishuService),
		Bound:   v.OpenID != "",
		OpenID:  v.OpenID,
		Disabled: v.DisabledAt != 0,
	}, nil
}

func (a *App) SetFeishuCredentials(ctx context.Context, c feishu.Credentials) error {
	if a.feishuService == nil {
		return errors.New("feishu disabled")
	}
	if err := a.feishuService.Store().SetCredentials(ctx, c); err != nil {
		return err
	}
	// Local mode only: try to (re)start the long-connection.
	return a.feishuService.EnsureLongConn(ctx)
}

func (a *App) BeginFeishuPair(ctx context.Context) (string, error) {
	if a.feishuService == nil {
		return "", errors.New("feishu disabled")
	}
	return a.feishuService.IssuePending(), nil
}

func (a *App) DeleteFeishuBinding(ctx context.Context) error {
	if a.feishuService == nil {
		return errors.New("feishu disabled")
	}
	return a.feishuService.Store().Delete(ctx)
}

type FeishuStatusResp struct {
	Enabled  bool   `json:"enabled"`
	Mode     string `json:"mode"`
	Bound    bool   `json:"bound"`
	OpenID   string `json:"open_id"`
	Disabled bool   `json:"disabled"`
}

func modeName(s *feishu.Service) string {
	// Mode is private on Service; expose a tiny helper there or just
	// remember the mode in App. For now, hard-code based on relay state.
	return "local" // adjusted by SetCredentials reload path
}
```

- [ ] **Step 3: Frontend TypeScript types**

Create or extend `desktop/frontend/src/shared/api/feishu.ts`:

```ts
// desktop/frontend/src/shared/api/feishu.ts
import { go } from "../../wailsjs/go/main/App";

export interface FeishuCredentials {
  app_id: string;
  app_secret: string;
  encrypt_key: string;
  verify_token: string;
}

export interface FeishuStatus {
  enabled: boolean;
  mode: "local" | "relay";
  bound: boolean;
  open_id: string;
  disabled: boolean;
}

export async function getFeishuStatus(): Promise<FeishuStatus> {
  return go.GetFeishuStatus();
}

export async function setFeishuCredentials(c: FeishuCredentials): Promise<void> {
  return go.SetFeishuCredentials(c);
}

export async function beginFeishuPair(): Promise<string> {
  return go.BeginFeishuPair();
}

export async function deleteFeishuBinding(): Promise<void> {
  return go.DeleteFeishuBinding();
}
```

(The exact wails JS import path varies by project layout — look at how
other API files like `desktop/frontend/src/shared/api/webhooks.ts`
import.)

- [ ] **Step 4: Frontend UI**

Create or modify `desktop/frontend/src/components/SettingsFeishu.vue`
with the following minimal shape (no styling — match existing tabs):

```vue
<template>
  <section>
    <h3>{{ t("settings.feishu.title") }}</h3>
    <p v-if="!status.enabled">{{ t("settings.feishu.disabled") }}</p>
    <template v-else>
      <p>{{ t("settings.feishu.mode") }}: {{ status.mode }}</p>
      <p v-if="status.bound">{{ t("settings.feishu.bound", { open_id: status.open_id }) }}</p>
      <button v-if="status.bound" @click="onDelete">{{ t("settings.feishu.delete") }}</button>
      <fieldset v-else>
        <label>App ID <input v-model="creds.app_id" /></label>
        <label>App Secret <input v-model="creds.app_secret" type="password" /></label>
        <label>Encrypt Key <input v-model="creds.encrypt_key" type="password" /></label>
        <label>Verify Token <input v-model="creds.verify_token" type="password" /></label>
        <button @click="onSave">{{ t("settings.feishu.save") }}</button>
        <button @click="onPair" :disabled="!status.bound && !saved">{{ t("settings.feishu.begin_pair") }}</button>
        <p v-if="pairCode">{{ t("settings.feishu.pair_hint", { code: pairCode }) }}</p>
      </fieldset>
    </template>
  </section>
</template>

<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import {
  getFeishuStatus, setFeishuCredentials, beginFeishuPair, deleteFeishuBinding,
  FeishuStatus, FeishuCredentials,
} from "../shared/api/feishu";

const { t } = useI18n();
const status = ref<FeishuStatus>({ enabled: false, mode: "local", bound: false, open_id: "", disabled: false });
const creds = ref<FeishuCredentials>({ app_id: "", app_secret: "", encrypt_key: "", verify_token: "" });
const pairCode = ref("");
const saved = ref(false);

async function refresh() {
  status.value = await getFeishuStatus();
}
onMounted(refresh);

async function onSave() {
  await setFeishuCredentials(creds.value);
  saved.value = true;
  await refresh();
}
async function onPair() {
  pairCode.value = await beginFeishuPair();
}
async function onDelete() {
  await deleteFeishuBinding();
  pairCode.value = "";
  saved.value = false;
  await refresh();
}
</script>
```

Add the i18n keys in `desktop/frontend/src/i18n/messages/en.ts` and
`zh-CN.ts`:

```ts
// en.ts
settings: {
  // ... existing ...
  feishu: {
    title: "Feishu Integration",
    disabled: "Feishu integration is not active.",
    mode: "Mode",
    bound: "Bound to open_id: {open_id}",
    save: "Save Credentials",
    begin_pair: "Start Pair",
    pair_hint: "Send \"/bind {code}\" to the bot in private chat within 15 minutes.",
    delete: "Delete Binding",
  },
},
```

```ts
// zh-CN.ts
settings: {
  feishu: {
    title: "飞书集成",
    disabled: "飞书集成尚未启用。",
    mode: "模式",
    bound: "已绑定到 open_id：{open_id}",
    save: "保存凭据",
    begin_pair: "开始绑定",
    pair_hint: "请在 15 分钟内向机器人发送私聊：/bind {code}",
    delete: "解绑",
  },
},
```

Register the new tab in `desktop/frontend/src/components/Settings.vue`
(or whichever component holds the settings tabs) alongside the
existing Webhooks/Relay/General tabs.

- [ ] **Step 5: Build + test**

```bash
go build ./...
go test ./desktop/...
cd desktop/frontend && npm run typecheck && cd -
```

Expected: Go and TS both clean. (Vue runtime warnings about the i18n
keys can be silenced once the keys land in both message files.)

- [ ] **Step 6: Commit**

```bash
git add desktop/app.go desktop/frontend/src/shared/api/feishu.ts \
        desktop/frontend/src/components/SettingsFeishu.vue \
        desktop/frontend/src/components/Settings.vue \
        desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "feat(desktop): assemble feishu.Service in app.go + minimal settings UI"
```

---

## Task 22: E2E checklist + final verification

**Files:**
- Create: `scripts/feishu-hook-e2e-checklist.md`
- Modify: `README.md`

- [ ] **Step 1: Create the manual checklist**

```bash
mkdir -p scripts
```

Write `scripts/feishu-hook-e2e-checklist.md`:

```markdown
# Feishu Hook + Desktop-Direct E2E Checklist

Run before each PR that touches `desktop/feishu/`, `cmd/atterm-hook/`,
or `internal/feishu/`. Not part of CI — requires a real Feishu app.

## Prereqs (one-time)

1. Build atterm desktop + the `atterm-hook` CLI:
   ```bash
   go build -o /tmp/atterm-hook ./cmd/atterm-hook
   sudo install /tmp/atterm-hook /usr/local/bin/atterm-hook
   ```
2. Create a self-built Feishu app at https://open.feishu.cn/app, note
   app_id/app_secret/encrypt_key/verify_token, enable "事件订阅加密",
   subscribe `im.message.receive_v1` + `card.action.trigger`.
3. Add Notification hooks to `~/.claude/settings.json`:
   ```json
   { "hooks": {
       "Notification": [
         { "matcher": {"type":"idle_prompt","tool":"AskUserQuestion"},
           "command": "atterm-hook" },
         { "matcher": {"type":"permission_prompt"},
           "command": "atterm-hook" }
       ] } }
   ```

## Relay-backed mode

- [ ] Log in to a relay account in atterm desktop.
- [ ] Settings → Feishu → save credentials → confirm "configured".
- [ ] Click "Start Pair" → receive a short code.
- [ ] In Feishu IM, private-message the bot: `/bind <code>` → bot replies
      "✅ 已绑定 到 atterm" (via relay HTTPS callback path).
- [ ] In atterm, start a session and run `claude` inside.
- [ ] Ask claude something that triggers `AskUserQuestion` → wait for
      a Feishu card with title "Session 等待输入" + the question text
      in a fenced code block.
- [ ] Tap "确认" → card updates to "已确认".
- [ ] Tap "跳回打开 session" → `atterm://session/<id>` (still no-op
      until URL scheme handler ships).
- [ ] Run a non-AI command that takes > 5s and finishes → confirm a
      separate `command_finished` card.

## Local-only mode

- [ ] Log out of relay; restart atterm.
- [ ] Settings → Feishu shows "local" mode.
- [ ] Save credentials (same app) → long-connection should attach
      within seconds (check `atterm` logs for `longconn: connected`).
- [ ] Start pair → `/bind <code>` over IM → bot replies → settings tab
      flips to "bound".
- [ ] Run claude inside an atterm session → AskUserQuestion → card lands.
- [ ] Tap "确认" → card updates (handled by long-conn ack path).

## Edge cases

- [ ] Misconfigure verify_token in local mode → long-conn should reject
      handshake → settings tab shows "disabled" banner.
- [ ] Send 3 IM messages while authcode is wrong (force IM 99991663) →
      `feishu_bindings.disabled_at` flag flips → cards stop sending.
- [ ] Delete binding → confirm long-connection cleanly closes.

## Cleanup

- [ ] Delete binding via UI → confirm endpoint file `~/.config/atterm/hook-endpoint`
      removed after atterm shutdown.
```

- [ ] **Step 2: Update README**

Add a section to `README.md`'s feishu coverage (or create one if
v0.2.114 didn't): document the new `atterm-hook` CLI, the env vars
that get auto-injected (`ATTERM_SESSION_ID`, `ATTERM_HOOK_ENDPOINT`),
and how to set up the claude-code Notification hook.

Example block (mirror the existing README style):

```markdown
### Feishu — claude-code question text

When `claude-code` is running inside an atterm session, configure its
Notification hooks (see `~/.claude/settings.json`) to fire the
`atterm-hook` CLI. The CLI POSTs the prompt context to atterm's
desktop process, which sends a Feishu IM card carrying the actual
question text. The `ATTERM_SESSION_ID` and `ATTERM_HOOK_ENDPOINT`
environment variables are injected into every PTY atterm spawns, so
no extra wiring is needed beyond the hook config.

For the manual end-to-end checklist, see
[`scripts/feishu-hook-e2e-checklist.md`](scripts/feishu-hook-e2e-checklist.md).
```

- [ ] **Step 3: Final verification**

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .
```

Expected: build green, all tests pass, no vet warnings, no unformatted
files. Run the manual e2e checklist before merging.

- [ ] **Step 4: Commit**

```bash
git add scripts/feishu-hook-e2e-checklist.md README.md
git commit -m "docs(feishu): e2e checklist + README for hook-based question text"
```

---

## Post-implementation notes

- The codex precise hook adapter is intentionally a stub (`hook_adapter.go`).
  Once `openai/codex#19921` lands a `waiting_for_input` event, register
  a `codexAdapter` alongside `claudeCodeAdapter` and add a parse test
  per agent_kind.
- `atterm-hook` is small and dependency-free; ship it from the Release
  workflow as a separate artifact alongside the desktop installers
  (homebrew formula, MSI, tarball).
- The keychain blob stays unencrypted-at-rest because the OS keychain
  IS the encryption layer. If a future user runs without a keyring
  available (some Linux server distros), surface a fallback to AEAD
  via `ATTERM_FEISHU_ENCRYPT_KEY` reused from the relay side — but
  that's out of scope for this PR.
- The dispatcher's 30-second dedup window is a compromise. If real
  usage shows false negatives (legitimate second prompts being
  silenced), surface as a setting.
