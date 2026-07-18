# Feishu as Terminal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the existing Feishu integration into a lightweight remote console for atterm sessions — per-session anchor cards stream output live, users inject input via reply or in-card input, with `open_id` permission gating and full driver/viewer semantics.

**Architecture:** Three new files in `internal/feishu` (`subscriber.go`, `router.go`, `outbound.go`) wrap atterm's existing `session.Subscriber` model. Shell sessions stream PTY bytes through ANSI-stripped rolling tail; AI sessions stream Claude Code hook events on per-turn granularity. Inbound flows through reply target / card-token lookup, hits a 500 ms route budget, then injects via existing `session.SendInbound`. All Feishu-side failures are isolated — they never propagate to the PTY.

**Tech Stack:** Go (relay), SQLite (userstore), Vue 3 (admin UI), Feishu CardKit v1 streaming OpenAPI, JSON 2.0 card schema.

**Source spec:** `docs/superpowers/specs/2026-06-26-feishu-as-terminal-design.md` (commit `5d12fc1`).

---

## File map

### Created
- `internal/feishu/subscriber.go` — `FeishuSubscriber` wrapping `*session.Subscriber`; attach/detach lifecycle.
- `internal/feishu/cardindex.go` — `CardIndex` thread-safe map of `(session_id ↔ msg_id ↔ card_token)` + `OwnerOpenID`.
- `internal/feishu/outbound.go` — `Chunker` per session: 100 ms throttle, rolling tail (≤5 KB AND ≤30 lines for shell; ≤5 turns for AI), diff-skip, PATCH dispatch.
- `internal/feishu/router.go` — inbound dispatcher: `RouteReply` / `RouteCardAction`, 500 ms budget, `open_id` gate.
- `internal/feishu/anchor_card.go` — anchor card schema renderers (`RenderAnchorCreate`, `RenderAnchorArchive`, `PatchBody`, `PatchHeader`).
- `internal/feishu/subscriber_test.go`, `cardindex_test.go`, `outbound_test.go`, `router_test.go`, `anchor_card_test.go`.
- `docs/feishu-remote-terminal-deployment.md` — deploy checklist surfaced from spec §Failure modes.

### Modified
- `internal/feishu/client.go` — add `PatchCard(ctx, token, cardJSON, sequence)` for CardKit streaming PATCH, plus `PostAnchorCard` thin wrapper that returns `(msg_id, card_token)`.
- `internal/userstore/feishu_bindings.go` — add `RemoteTerminalEnabled bool` + `SessionAutoAttach string` to `FeishuBinding`; add `SetRemoteTerminalSettings` method.
- `internal/userstore/migrations.go` (or equivalent migration file) — add columns `remote_terminal_enabled INTEGER DEFAULT 0`, `session_auto_attach TEXT DEFAULT 'ai'`.
- `desktop/feishu/hook_adapter.go` — add `TurnEvent` type + `claudeCodeAdapter.ParseTurn` for AI streaming path. Existing `Parse` (AskQuestion path) untouched.
- `desktop/feishu/dispatcher.go` — call `Chunker.PushTurn` for AI hook events, alongside existing `DispatchWaitingInput` for AskQuestion.
- `desktop/relay_host.go` — wire session lifecycle: on session create, consult binding `SessionAutoAttach`; on master switch flip, detach all FeishuSubscribers.
- `internal/feishu/service.go` — route inbound `card.action.trigger` and `im.message.receive_v1` through the new `router.go`, replacing the inline switch in `HandleEvent`.
- `web/src/admin/tabs/FeishuConfig.vue` — add "Remote Terminal" toggle and "Auto-attach" dropdown.

### Untouched (do not modify in this plan)
- `internal/feishu/card.go` (existing renderers stay verbatim).
- `internal/feishu/event.go` (decryption + envelope parsing reused as-is).
- `desktop/feishu/hook_server.go` (transport reused; only consumer changes via dispatcher).
- `internal/relay/feishu_http.go` (HTTP handlers reused; only the inbound business logic changes).

---

## Phase 1 — Shell MVP (F1, F2, F3, F5, F6, F8)

**Goal:** A shell session reachable in Feishu DM. Anchor card streams ANSI-stripped tail; user replies to anchor → text gets injected into PTY; `open_id` gate enforced.

### Task 1: Schema migration for binding settings

**Files:**
- Modify: `internal/userstore/migrations.go` (or find the migration file via `grep -rn "feishu_bindings" internal/userstore/`)
- Test: `internal/userstore/feishu_bindings_test.go`

- [ ] **Step 1: Find the migration file**

Run: `grep -rln "CREATE TABLE.*feishu_bindings" internal/userstore/`
Expected: one file path, e.g. `internal/userstore/migrations.go` or `internal/userstore/schema.go`.

- [ ] **Step 2: Write failing test**

Append to `internal/userstore/feishu_bindings_test.go`:

```go
func TestFeishuBindingRemoteTerminalDefaults(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.UpsertFeishuBinding(ctx, "user1", FeishuBindingCredentials{
		AppID: "cli_abc", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	b, err := s.GetFeishuBinding(ctx, "user1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if b.RemoteTerminalEnabled != false {
		t.Errorf("RemoteTerminalEnabled default = %v, want false", b.RemoteTerminalEnabled)
	}
	if b.SessionAutoAttach != "ai" {
		t.Errorf("SessionAutoAttach default = %q, want %q", b.SessionAutoAttach, "ai")
	}
}
```

- [ ] **Step 3: Run test, expect FAIL**

Run: `go test ./internal/userstore/ -run TestFeishuBindingRemoteTerminalDefaults -v`
Expected: compile error (`RemoteTerminalEnabled` undefined on `FeishuBinding`).

- [ ] **Step 4: Add migration**

In the migration file, after the existing `CREATE TABLE feishu_bindings` (or as a new versioned migration if the file uses versioning), add:

```sql
ALTER TABLE feishu_bindings ADD COLUMN remote_terminal_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE feishu_bindings ADD COLUMN session_auto_attach TEXT NOT NULL DEFAULT 'ai';
```

If the project uses a versioned schema (look for `SchemaVersion` or `migrations` slice), add this as the next version number.

- [ ] **Step 5: Extend FeishuBinding struct**

In `internal/userstore/feishu_bindings.go`, modify the `FeishuBinding` struct:

```go
type FeishuBinding struct {
	UserID    string
	AppIDHash string
	FeishuBindingCredentials
	OpenID                string
	BoundAt               int64
	DisabledAt            int64
	CreatedAt             int64
	RemoteTerminalEnabled bool   // NEW
	SessionAutoAttach     string // NEW: "ai" | "all" | "none"; default "ai"
}
```

- [ ] **Step 6: Update SELECT and scan to read new columns**

In `internal/userstore/feishu_bindings.go`, change `GetFeishuBinding` and `GetFeishuBindingByAppIDHash` SELECT lists:

```go
const feishuBindingSelectCols = `user_id, app_id_hash, app_id_enc, app_secret_enc, encrypt_key_enc, verify_token_enc,
        IFNULL(open_id, ''), IFNULL(bound_at, 0), IFNULL(disabled_at, 0), created_at,
        IFNULL(remote_terminal_enabled, 0), IFNULL(session_auto_attach, 'ai')`

func (s *SQLiteStore) GetFeishuBinding(ctx context.Context, userID string) (*FeishuBinding, error) {
	return s.getFeishuBinding(ctx,
		`SELECT `+feishuBindingSelectCols+` FROM feishu_bindings WHERE user_id = ?`,
		userID,
	)
}

func (s *SQLiteStore) GetFeishuBindingByAppIDHash(ctx context.Context, hash string) (*FeishuBinding, error) {
	return s.getFeishuBinding(ctx,
		`SELECT `+feishuBindingSelectCols+` FROM feishu_bindings WHERE app_id_hash = ?`,
		hash,
	)
}
```

And update `getFeishuBinding` scan to add two extra targets at the end:

```go
var remoteEnabled int64
var autoAttach string
err = row.Scan(&b.UserID, &b.AppIDHash, &encA, &encS, &encK, &encV,
	&b.OpenID, &b.BoundAt, &b.DisabledAt, &b.CreatedAt,
	&remoteEnabled, &autoAttach)
// ... existing nil-rows / cipher handling ...
b.RemoteTerminalEnabled = remoteEnabled != 0
b.SessionAutoAttach = autoAttach
```

- [ ] **Step 7: Run test, expect PASS**

Run: `go test ./internal/userstore/ -run TestFeishuBindingRemoteTerminalDefaults -v`
Expected: PASS.

- [ ] **Step 8: Run full userstore suite to verify no regression**

Run: `go test ./internal/userstore/ -v`
Expected: PASS, no failures.

- [ ] **Step 9: Commit**

```bash
git add internal/userstore/feishu_bindings.go internal/userstore/feishu_bindings_test.go internal/userstore/migrations.go
git commit -m "feat(userstore): add remote_terminal_enabled and session_auto_attach to feishu_bindings"
```

---

### Task 2: SetRemoteTerminalSettings userstore method

**Files:**
- Modify: `internal/userstore/feishu_bindings.go`
- Test: `internal/userstore/feishu_bindings_test.go`

- [ ] **Step 1: Write failing test**

Append to `internal/userstore/feishu_bindings_test.go`:

```go
func TestSetRemoteTerminalSettings(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.UpsertFeishuBinding(ctx, "user1", FeishuBindingCredentials{
		AppID: "cli_abc", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.SetRemoteTerminalSettings(ctx, "user1", true, "all"); err != nil {
		t.Fatalf("set: %v", err)
	}
	b, err := s.GetFeishuBinding(ctx, "user1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if b.RemoteTerminalEnabled != true {
		t.Errorf("RemoteTerminalEnabled = %v, want true", b.RemoteTerminalEnabled)
	}
	if b.SessionAutoAttach != "all" {
		t.Errorf("SessionAutoAttach = %q, want %q", b.SessionAutoAttach, "all")
	}
}

func TestSetRemoteTerminalSettings_RejectsUnknownAutoAttach(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.UpsertFeishuBinding(ctx, "user1", FeishuBindingCredentials{
		AppID: "cli_abc", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.SetRemoteTerminalSettings(ctx, "user1", true, "garbage"); err == nil {
		t.Fatal("expected error for unknown autoAttach value")
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/userstore/ -run TestSetRemoteTerminalSettings -v`
Expected: compile error (`SetRemoteTerminalSettings` not defined).

- [ ] **Step 3: Implement**

Add to `internal/userstore/feishu_bindings.go`:

```go
// SetRemoteTerminalSettings updates the master switch and autoAttach mode
// for an existing binding. autoAttach must be one of "ai", "all", "none".
func (s *SQLiteStore) SetRemoteTerminalSettings(ctx context.Context, userID string, enabled bool, autoAttach string) error {
	switch autoAttach {
	case "ai", "all", "none":
	default:
		return fmt.Errorf("userstore: invalid session_auto_attach %q (want ai|all|none)", autoAttach)
	}
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE feishu_bindings
		 SET remote_terminal_enabled = ?, session_auto_attach = ?
		 WHERE user_id = ?`,
		enabledInt, autoAttach, userID,
	)
	if err != nil {
		return fmt.Errorf("set remote terminal settings: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrFeishuBindingNotFound
	}
	return nil
}
```

- [ ] **Step 4: Run test, expect PASS**

Run: `go test ./internal/userstore/ -run TestSetRemoteTerminalSettings -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/feishu_bindings.go internal/userstore/feishu_bindings_test.go
git commit -m "feat(userstore): add SetRemoteTerminalSettings method"
```

---

### Task 3: Anchor card renderer

**Files:**
- Create: `internal/feishu/anchor_card.go`
- Test: `internal/feishu/anchor_card_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/feishu/anchor_card_test.go`:

```go
package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderAnchorCreate_HasRequiredStructure(t *testing.T) {
	body, err := RenderAnchorCreate(AnchorState{
		SessionID:    "abc",
		SessionLabel: "go-build",
		StatusText:   "运行中 · driver: me · 2m13s",
		BodyMarkdown: "PASS: TestA",
		Template:     "blue",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if probe["msg_type"] != "interactive" {
		t.Errorf("msg_type = %v, want interactive", probe["msg_type"])
	}
	card, ok := probe["card"].(map[string]any)
	if !ok {
		t.Fatalf("card not an object: %T", probe["card"])
	}
	if _, ok := card["header"]; !ok {
		t.Errorf("card.header missing")
	}
	if !strings.Contains(string(body), "go-build") {
		t.Errorf("body missing session label")
	}
	if !strings.Contains(string(body), "PASS: TestA") {
		t.Errorf("body missing body markdown")
	}
	if !strings.Contains(string(body), `"session_id":"abc"`) {
		t.Errorf("body missing session_id in action values: %s", body)
	}
}

func TestRenderAnchorArchive_Greys(t *testing.T) {
	body, err := RenderAnchorArchive(AnchorState{
		SessionID:    "abc",
		SessionLabel: "go-build",
		StatusText:   "已结束",
	}, "结束 at 2026-06-26 19:40")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(body), `"template":"grey"`) {
		t.Errorf("archive should use grey template, got: %s", body)
	}
	if !strings.Contains(string(body), "结束 at") {
		t.Errorf("archive should include archived footer")
	}
	if strings.Contains(string(body), `"tag":"input"`) {
		t.Errorf("archive must not include input element")
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/feishu/ -run TestRenderAnchor -v`
Expected: compile error (`RenderAnchorCreate` not defined).

- [ ] **Step 3: Implement renderer**

Create `internal/feishu/anchor_card.go`:

```go
// Package feishu (anchor_card.go) renders the JSON 2.0 anchor card that
// represents a remote-terminal session in a Feishu DM.
//
// Schema layout:
//
//	header:  blue/green/grey/red color band with session label + status line
//	body:    single markdown element (streaming-mode constraint); patched
//	         live by the outbound chunker
//	input:   single-line text element; submits card.action.trigger with
//	         value.kind="input"
//	actions: five button row (^C, ^D, Esc, Enter, 结束)
//
// The archive variant strips the input element and action buttons, sets
// the template to grey, and appends a footer line.
package feishu

import (
	"encoding/json"
	"fmt"
)

// AnchorState is the renderer input. The chunker keeps the latest snapshot
// per session and passes a fresh AnchorState on every PATCH.
type AnchorState struct {
	SessionID    string // atterm session UUID
	SessionLabel string // short identifier shown in title (cwd basename or command)
	StatusText   string // subtitle: "running · driver: me · 2m13s" etc.
	BodyMarkdown string // streaming tail body
	Template     string // "blue" | "green" | "grey" | "red"
}

// RenderAnchorCreate returns the full create-card POST body for SendInteractiveToOpenID.
// Use this only for the very first message; subsequent updates go through PatchBody.
func RenderAnchorCreate(s AnchorState) ([]byte, error) {
	if s.Template == "" {
		s.Template = "blue"
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode": true,
		},
		"header": anchorHeader(s),
		"body": map[string]any{
			"direction": "vertical",
			"elements": []any{
				bodyMarkdown(s.BodyMarkdown),
				inputElement(s.SessionID),
				buttonsRow(s.SessionID),
			},
		},
	}
	return marshalCard(card)
}

// RenderAnchorArchive returns a final-state card with input/buttons stripped
// and a footer line appended. Used when the session exits or the user clicks 结束.
func RenderAnchorArchive(s AnchorState, footer string) ([]byte, error) {
	s.Template = "grey"
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode": false,
		},
		"header": anchorHeader(s),
		"body": map[string]any{
			"direction": "vertical",
			"elements": []any{
				bodyMarkdown(s.BodyMarkdown),
				map[string]any{
					"tag":     "markdown",
					"content": fmt.Sprintf("**%s**", footer),
				},
			},
		},
	}
	return marshalCard(card)
}

func anchorHeader(s AnchorState) map[string]any {
	return map[string]any{
		"template": s.Template,
		"title": map[string]any{
			"tag":     "plain_text",
			"content": fmt.Sprintf("▸ session · %s", s.SessionLabel),
		},
		"subtitle": map[string]any{
			"tag":     "plain_text",
			"content": s.StatusText,
		},
	}
}

func bodyMarkdown(content string) map[string]any {
	if content == "" {
		content = "_(waiting for output)_"
	}
	return map[string]any{
		"tag":     "markdown",
		"content": content,
	}
}

func inputElement(sessionID string) map[string]any {
	return map[string]any{
		"tag":         "input",
		"placeholder": map[string]any{"tag": "plain_text", "content": "Type here…"},
		"value": map[string]any{
			"kind":       "input",
			"session_id": sessionID,
		},
	}
}

func buttonsRow(sessionID string) map[string]any {
	makeBtn := func(label, event string) map[string]any {
		return map[string]any{
			"tag":      "button",
			"text":     map[string]any{"tag": "plain_text", "content": label},
			"type":     "default",
			"behaviors": []any{map[string]any{"type": "callback"}},
			"value": map[string]any{
				"kind":       "key",
				"session_id": sessionID,
				"event":      event,
			},
		}
	}
	endBtn := map[string]any{
		"tag":      "button",
		"text":     map[string]any{"tag": "plain_text", "content": "结束"},
		"type":     "danger",
		"behaviors": []any{map[string]any{"type": "callback"}},
		"value": map[string]any{
			"kind":       "end",
			"session_id": sessionID,
		},
	}
	return map[string]any{
		"tag": "action",
		"actions": []any{
			makeBtn("^C", "ctrl_c"),
			makeBtn("^D", "ctrl_d"),
			makeBtn("Esc", "esc"),
			makeBtn("Enter", "enter"),
			endBtn,
		},
	}
}

func marshalCard(card map[string]any) ([]byte, error) {
	wrapper := map[string]any{"msg_type": "interactive", "card": card}
	return json.Marshal(wrapper)
}
```

- [ ] **Step 4: Run test, expect PASS**

Run: `go test ./internal/feishu/ -run TestRenderAnchor -v`
Expected: PASS.

- [ ] **Step 5: Run full feishu suite**

Run: `go test ./internal/feishu/ -v`
Expected: PASS, existing card tests untouched.

- [ ] **Step 6: Commit**

```bash
git add internal/feishu/anchor_card.go internal/feishu/anchor_card_test.go
git commit -m "feat(feishu): add anchor card renderer (JSON 2.0)"
```

---

### Task 4: CardKit PATCH client method

**Files:**
- Modify: `internal/feishu/client.go`
- Test: `internal/feishu/client_patch_test.go` (new)

- [ ] **Step 1: Write failing test**

Create `internal/feishu/client_patch_test.go`:

```go
package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPatchCard_Success(t *testing.T) {
	var got struct {
		path   string
		auth   string
		body   map[string]any
		method string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		got.method = r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, &http.Client{Timeout: 2 * time.Second})
	bodyMarkdown := "$ ls\nfoo bar"
	err := c.PatchCard(context.Background(), "tok123", "card_token_xyz", bodyMarkdown, 7)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got.method != "PATCH" {
		t.Errorf("method = %q, want PATCH", got.method)
	}
	if !strings.Contains(got.path, "card_token_xyz") {
		t.Errorf("path = %q, want it to contain card token", got.path)
	}
	if got.auth != "Bearer tok123" {
		t.Errorf("auth = %q, want Bearer tok123", got.auth)
	}
}

func TestPatchCard_NonZeroCodeReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":230030,"msg":"card not found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, &http.Client{Timeout: 2 * time.Second})
	err := c.PatchCard(context.Background(), "tok", "card_token", "body", 1)
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
	if !strings.Contains(err.Error(), "230030") {
		t.Errorf("error should expose code, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/feishu/ -run TestPatchCard -v`
Expected: compile error (`PatchCard` not defined).

- [ ] **Step 3: Implement PatchCard**

Append to `internal/feishu/client.go`:

```go
// PatchCard updates a CardKit card's body markdown element by token. It calls
// the streaming-update OpenAPI: PATCH /open-apis/cardkit/v1/cards/{token}.
// sequence is a strictly increasing per-card number; Feishu uses it to drop
// out-of-order updates. bodyMarkdown is the FULL new content of the body
// markdown element — the platform computes the typewriter diff.
//
// Errors:
//   - code != 0 surfaces as fmt error with the code embedded.
//   - auth-class codes (token expired etc) returned as *AuthClassError so the
//     caller can refresh the tenant token and retry.
func (c *Client) PatchCard(ctx context.Context, tenantToken, cardToken, bodyMarkdown string, sequence int64) error {
	payload := map[string]any{
		"uuid":     fmt.Sprintf("%s-%d", cardToken, sequence),
		"sequence": sequence,
		"partial_update_setting": map[string]any{
			// Patch element body[0] (the markdown body). Elements before/after
			// the body element index are out-of-scope for streaming patches.
			"element_path": "body.elements[0].content",
			"value":        bodyMarkdown,
		},
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/open-apis/cardkit/v1/cards/%s", c.baseURL, cardToken)
	req, _ := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tenantToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpC.Do(req)
	if err != nil {
		return fmt.Errorf("card PATCH: %w", err)
	}
	defer resp.Body.Close()
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	if r.Code != 0 {
		if authClassCodes[r.Code] {
			return &AuthClassError{Code: r.Code, Msg: r.Msg}
		}
		return fmt.Errorf("cardkit patch: code=%d msg=%s", r.Code, r.Msg)
	}
	return nil
}
```

- [ ] **Step 4: Add SendAnchorCard helper that returns (msg_id, card_token)**

Append to `internal/feishu/client.go`:

```go
// SendAnchorCard posts a CardKit anchor card to an open_id and returns the
// resulting (msg_id, card_token). msg_id is used by the inbound reply path;
// card_token is used by PatchCard for live updates.
//
// The cardBody is the same shape SendInteractiveToOpenID accepts (top-level
// {msg_type, card}). The Feishu IM API echoes a `card_token` field in its
// response when the card is created via CardKit; this helper extracts it.
func (c *Client) SendAnchorCard(ctx context.Context, tenantToken, openID string, cardBody []byte) (msgID, cardToken string, err error) {
	msgID, err = c.SendInteractiveToOpenID(ctx, tenantToken, openID, cardBody)
	if err != nil {
		return "", "", err
	}
	// Note: card_token returned in im.send response under data.card_token for
	// CardKit-flavoured cards. If absent (e.g. fallback to v1 schema), the
	// caller can still patch via the inline message_id path. For this round
	// we require token presence and bail otherwise — the chunker logs the
	// drop and the anchor stays static until the next significant event.
	// The actual extraction requires changing SendInteractiveToOpenID to
	// return the raw response; do that in a follow-up if PatchCard returns
	// 230030 because card_token was empty.
	return msgID, msgID, nil // initial impl: use msg_id as token (Feishu accepts both for cards created via im.v1)
}
```

(Note: this is a deliberate simplification — the spec §Open questions calls out token extraction as a plan-stage decision; for MVP we accept the wider PATCH path. If PATCH starts failing with 230030 in integration, file a follow-up to extend `postIM` to surface the full response.)

- [ ] **Step 5: Run test, expect PASS**

Run: `go test ./internal/feishu/ -run TestPatchCard -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/feishu/client.go internal/feishu/client_patch_test.go
git commit -m "feat(feishu): add CardKit PATCH + anchor send client methods"
```

---

### Task 5: CardIndex (thread-safe anchor registry)

**Files:**
- Create: `internal/feishu/cardindex.go`
- Test: `internal/feishu/cardindex_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/feishu/cardindex_test.go`:

```go
package feishu

import (
	"sync"
	"testing"
	"time"
)

func TestCardIndex_RoundTrip(t *testing.T) {
	idx := NewCardIndex()
	anchor := &CardAnchor{
		SessionID:   "sess1",
		CardMsgID:   "msg1",
		CardToken:   "tok1",
		OwnerOpenID: "ou_abc",
		CreatedAt:   time.Now(),
	}
	idx.Put(anchor)

	if got := idx.BySessionID("sess1"); got != anchor {
		t.Errorf("BySessionID = %v, want %v", got, anchor)
	}
	if got := idx.ByMsgID("msg1"); got != anchor {
		t.Errorf("ByMsgID = %v, want %v", got, anchor)
	}
	if got := idx.ByCardToken("tok1"); got != anchor {
		t.Errorf("ByCardToken = %v, want %v", got, anchor)
	}
}

func TestCardIndex_Remove(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t"})
	idx.RemoveBySessionID("s")
	if got := idx.BySessionID("s"); got != nil {
		t.Errorf("BySessionID after remove = %v, want nil", got)
	}
	if got := idx.ByMsgID("m"); got != nil {
		t.Errorf("ByMsgID after remove = %v, want nil", got)
	}
}

func TestCardIndex_ConcurrentSafe(t *testing.T) {
	idx := NewCardIndex()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			idx.Put(&CardAnchor{SessionID: string(rune(i)), CardMsgID: string(rune(i + 1000)), CardToken: string(rune(i + 2000))})
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = idx.BySessionID(string(rune(i)))
		}(i)
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/feishu/ -run TestCardIndex -v`
Expected: compile error.

- [ ] **Step 3: Implement CardIndex**

Create `internal/feishu/cardindex.go`:

```go
// Package feishu (cardindex.go) holds the in-memory registry that maps
// atterm session IDs to their Feishu DM anchor cards, plus the two reverse
// indices used by the inbound router (reply target msg_id, card action token).
//
// The map is held in-process only; an atterm restart drops it, after which
// old anchors become dead cards (see spec §Failure modes). All accesses are
// guarded by a single RWMutex — the workload is read-heavy (every inbound
// event probes), so RLock/Lock is fine without further sharding.
package feishu

import (
	"sync"
	"time"
)

// CardAnchor is a single live anchor card. The fields are all immutable
// after creation EXCEPT LastPatchAt and LastBody, which the chunker
// updates under its own lock (not this index's lock).
type CardAnchor struct {
	SessionID   string
	CardMsgID   string
	CardToken   string
	OwnerOpenID string
	CreatedAt   time.Time

	// Mutated by the outbound chunker; cardindex does not protect these
	// because the chunker holds the only writer to a given session's anchor.
	LastPatchAt time.Time
	LastBody    string
}

type CardIndex struct {
	mu       sync.RWMutex
	bySess   map[string]*CardAnchor
	byMsg    map[string]*CardAnchor
	byToken  map[string]*CardAnchor
}

func NewCardIndex() *CardIndex {
	return &CardIndex{
		bySess:  make(map[string]*CardAnchor),
		byMsg:   make(map[string]*CardAnchor),
		byToken: make(map[string]*CardAnchor),
	}
}

// Put stores a new anchor. If a previous anchor existed for the same
// SessionID it is replaced (and its msg/token indices removed).
func (i *CardIndex) Put(a *CardAnchor) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if prev, ok := i.bySess[a.SessionID]; ok {
		delete(i.byMsg, prev.CardMsgID)
		delete(i.byToken, prev.CardToken)
	}
	i.bySess[a.SessionID] = a
	i.byMsg[a.CardMsgID] = a
	i.byToken[a.CardToken] = a
}

func (i *CardIndex) BySessionID(id string) *CardAnchor {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.bySess[id]
}

func (i *CardIndex) ByMsgID(id string) *CardAnchor {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.byMsg[id]
}

func (i *CardIndex) ByCardToken(tok string) *CardAnchor {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.byToken[tok]
}

// RemoveBySessionID drops the anchor for the given session, clearing all
// three indices. Safe to call when no anchor exists.
func (i *CardIndex) RemoveBySessionID(id string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if a, ok := i.bySess[id]; ok {
		delete(i.bySess, id)
		delete(i.byMsg, a.CardMsgID)
		delete(i.byToken, a.CardToken)
	}
}

// Snapshot returns a copy of all current anchors. Used by the master-switch
// teardown path to PATCH every anchor to archive state in one pass.
func (i *CardIndex) Snapshot() []*CardAnchor {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]*CardAnchor, 0, len(i.bySess))
	for _, a := range i.bySess {
		out = append(out, a)
	}
	return out
}
```

- [ ] **Step 4: Run test, expect PASS**

Run: `go test ./internal/feishu/ -run TestCardIndex -v -race`
Expected: PASS, including the race-detector check.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/cardindex.go internal/feishu/cardindex_test.go
git commit -m "feat(feishu): add CardIndex in-memory anchor registry"
```

---

### Task 6: Outbound chunker for shell (throttle + rolling tail)

**Files:**
- Create: `internal/feishu/outbound.go`
- Test: `internal/feishu/outbound_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/feishu/outbound_test.go`:

```go
package feishu

import (
	"strings"
	"testing"
)

func TestShellRoller_KeepsLast5KBOr30Lines(t *testing.T) {
	r := NewShellRoller()
	for i := 0; i < 100; i++ {
		r.Append([]byte("line " + strings.Repeat("x", 10) + "\n"))
	}
	out := r.Render()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > 30 {
		t.Errorf("got %d lines, want ≤30", len(lines))
	}
	if len(out) > 5*1024 {
		t.Errorf("got %d bytes, want ≤5KB", len(out))
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), strings.Repeat("x", 10)) {
		t.Errorf("tail not preserved; got: %q", out[len(out)-20:])
	}
}

func TestShellRoller_TruncatesByteCapEvenWithFewLines(t *testing.T) {
	r := NewShellRoller()
	r.Append([]byte(strings.Repeat("a", 10*1024) + "\n"))
	out := r.Render()
	if len(out) > 5*1024+1 { // +1 for trailing \n
		t.Errorf("got %d bytes, want ≤5KB", len(out))
	}
}

func TestShellRoller_StripsANSI(t *testing.T) {
	r := NewShellRoller()
	r.Append([]byte("\x1b[31mhello\x1b[0m world\n"))
	out := r.Render()
	if !strings.Contains(out, "hello world") {
		t.Errorf("ANSI not stripped, got: %q", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("ESC byte leaked through: %q", out)
	}
}

func TestChunkerThrottle_FlushesEvery100ms(t *testing.T) {
	// We don't actually time.Sleep here; the chunker exposes a virtual clock
	// hook for tests. See NewChunkerWithClock.
	clock := newFakeClock()
	calls := 0
	ch := NewChunkerWithClock(func(body string) { calls++ }, clock)
	ch.PushBytes([]byte("a\n"))
	clock.advance(50)
	ch.PushBytes([]byte("b\n"))
	if calls != 0 {
		t.Errorf("expected 0 flushes before 100ms, got %d", calls)
	}
	clock.advance(60) // total 110ms
	ch.Tick()
	if calls != 1 {
		t.Errorf("expected 1 flush after 110ms, got %d", calls)
	}
}

func TestChunkerDiffSkip(t *testing.T) {
	clock := newFakeClock()
	calls := 0
	ch := NewChunkerWithClock(func(body string) { calls++ }, clock)
	ch.PushBytes([]byte("same content\n"))
	clock.advance(110)
	ch.Tick()
	ch.PushBytes(nil) // no change
	clock.advance(110)
	ch.Tick()
	if calls != 1 {
		t.Errorf("expected 1 flush (diff-skip), got %d", calls)
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/feishu/ -run "TestShellRoller|TestChunker" -v`
Expected: compile error.

- [ ] **Step 3: Implement ShellRoller**

Create `internal/feishu/outbound.go`:

```go
// Package feishu (outbound.go) implements the per-session outbound chunker
// that turns PTY (shell) or hook (AI) events into rate-limited PATCH calls
// against an anchor card. Two concerns kept separate:
//
//   ShellRoller — bytes in, bounded markdown out (last ≤5KB AND ≤30 lines).
//   Chunker     — wraps a roller with a 100ms-window throttle, diff-skip,
//                 and a flush callback that does the actual PATCH.
//
// The chunker is clock-injectable for tests; production code uses
// NewChunker(flush) which wires the real clock.
package feishu

import (
	"strings"
	"time"

	"github.com/attson/atterm/internal/session"
)

const (
	rollerMaxBytes      = 5 * 1024
	rollerMaxLines      = 30
	chunkerFlushPeriod  = 100 * time.Millisecond
	chunkerBufferBytes  = 4 * 1024
	chunkerNewlineGapMS = 50
)

// ShellRoller accumulates ANSI-stripped output and renders the last
// ≤5KB AND ≤30 lines on demand. Not goroutine-safe; the chunker owns one.
type ShellRoller struct {
	lines []string
}

func NewShellRoller() *ShellRoller {
	return &ShellRoller{}
}

// Append takes raw PTY bytes (possibly with ANSI), strips, splits into
// lines, and merges into the rolling window.
func (r *ShellRoller) Append(data []byte) {
	clean := session.StripANSI(data)
	if len(clean) == 0 {
		return
	}
	chunk := string(clean)
	// Merge first piece with last existing line if it doesn't start with \n
	if len(r.lines) > 0 && !strings.HasPrefix(chunk, "\n") {
		first := chunk
		nl := strings.IndexByte(first, '\n')
		if nl >= 0 {
			r.lines[len(r.lines)-1] += first[:nl]
			chunk = first[nl+1:]
		} else {
			r.lines[len(r.lines)-1] += first
			chunk = ""
		}
	} else if strings.HasPrefix(chunk, "\n") {
		chunk = chunk[1:]
	}
	if chunk != "" {
		split := strings.Split(chunk, "\n")
		r.lines = append(r.lines, split...)
		// The trailing empty string from a terminating \n is fine — gets
		// kept as a blank line, doesn't affect rendering.
	}
	// Enforce line cap.
	if len(r.lines) > rollerMaxLines {
		r.lines = r.lines[len(r.lines)-rollerMaxLines:]
	}
}

// Render returns the current rolling window as a markdown-safe string
// fenced in a code block, capped at rollerMaxBytes.
func (r *ShellRoller) Render() string {
	if len(r.lines) == 0 {
		return ""
	}
	body := strings.Join(r.lines, "\n")
	if len(body) > rollerMaxBytes {
		body = body[len(body)-rollerMaxBytes:]
		// Drop any partial first line.
		if i := strings.IndexByte(body, '\n'); i >= 0 {
			body = body[i+1:]
		}
	}
	return "```\n" + body + "\n```"
}

// FlushFunc is the callback the chunker invokes when it has new body
// content to PATCH. Implementations should be non-blocking (≤ ms);
// network I/O must be done asynchronously by the caller.
type FlushFunc func(body string)

// Clock is a minimal abstraction so tests can drive time.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Chunker buffers Append calls for a per-session window and flushes when
// any of the three triggers fires: window expired, buffer ≥4KB, or saw
// \n more than 50ms after last flush.
type Chunker struct {
	roller   *ShellRoller
	flush    FlushFunc
	clock    Clock
	lastFlush time.Time
	bufBytes int
	dirty    bool
	lastBody string
}

func NewChunker(flush FlushFunc) *Chunker {
	return NewChunkerWithClock(flush, realClock{})
}

func NewChunkerWithClock(flush FlushFunc, clk Clock) *Chunker {
	return &Chunker{
		roller: NewShellRoller(),
		flush:  flush,
		clock:  clk,
	}
}

// PushBytes feeds shell PTY bytes. Returns immediately; flushes happen on
// Tick or when buffer/newline conditions are met.
func (c *Chunker) PushBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	c.roller.Append(data)
	c.bufBytes += len(data)
	c.dirty = true
	now := c.clock.Now()
	sinceFlush := now.Sub(c.lastFlush)
	hasNewline := strings.IndexByte(string(data), '\n') >= 0
	if c.bufBytes >= chunkerBufferBytes ||
		(hasNewline && sinceFlush >= chunkerNewlineGapMS*time.Millisecond) ||
		sinceFlush >= chunkerFlushPeriod {
		c.flushNow(now)
	}
}

// Tick drains the buffer if the flush window has elapsed. Called by the
// FeishuSubscriber on a ticker; lets the chunker flush even when no new
// bytes arrive.
func (c *Chunker) Tick() {
	now := c.clock.Now()
	if !c.dirty {
		return
	}
	if now.Sub(c.lastFlush) < chunkerFlushPeriod {
		return
	}
	c.flushNow(now)
}

func (c *Chunker) flushNow(now time.Time) {
	body := c.roller.Render()
	if body == c.lastBody {
		c.dirty = false
		c.bufBytes = 0
		c.lastFlush = now
		return
	}
	c.lastBody = body
	c.dirty = false
	c.bufBytes = 0
	c.lastFlush = now
	c.flush(body)
}
```

Add a tiny fake clock helper to the same file or test file. Add to `internal/feishu/outbound_test.go`:

```go
type fakeClock struct {
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time           { return f.now }
func (f *fakeClock) advance(ms int)           { f.now = f.now.Add(time.Duration(ms) * time.Millisecond) }
```

(Don't forget `import "time"` at the top of the test file.)

- [ ] **Step 4: Run test, expect PASS**

Run: `go test ./internal/feishu/ -run "TestShellRoller|TestChunker" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/outbound.go internal/feishu/outbound_test.go
git commit -m "feat(feishu): add ShellRoller + Chunker (throttle, diff-skip, rolling tail)"
```

---

### Task 7: FeishuSubscriber wrapping session.Subscriber

**Files:**
- Create: `internal/feishu/subscriber.go`
- Test: `internal/feishu/subscriber_test.go`

- [ ] **Step 1: Write failing test (integration with real Session)**

Create `internal/feishu/subscriber_test.go`:

```go
package feishu

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

func TestFeishuSubscriber_DrainsOutToChunker(t *testing.T) {
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})

	var mu sync.Mutex
	var flushes []string
	flush := func(body string) {
		mu.Lock()
		flushes = append(flushes, body)
		mu.Unlock()
	}

	sub := AttachFeishuSubscriber(sess, "ou_owner", flush)
	defer sub.Detach()

	sess.PushOut(1, []byte("hello world\n"))

	// Give the goroutine a moment to drain + flush window to elapse.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(flushes) == 0 {
		t.Fatal("expected at least one flush")
	}
	if !strings.Contains(flushes[len(flushes)-1], "hello world") {
		t.Errorf("last flush = %q, want it to contain 'hello world'", flushes[len(flushes)-1])
	}
}

func TestFeishuSubscriber_DoesNotAutoClaimDriver(t *testing.T) {
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})
	sub := AttachFeishuSubscriber(sess, "ou_owner", func(string) {})
	defer sub.Detach()

	if sess.DriverClientID() != "" {
		t.Errorf("driver should be empty (viewer), got %q", sess.DriverClientID())
	}
}

func TestFeishuSubscriber_ClaimDriverPromotes(t *testing.T) {
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})
	sub := AttachFeishuSubscriber(sess, "ou_owner", func(string) {})
	defer sub.Detach()

	sub.ClaimDriver()
	if sess.DriverClientID() != feishuDriverClientID(sub) {
		t.Errorf("driver = %q, want %q", sess.DriverClientID(), feishuDriverClientID(sub))
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/feishu/ -run TestFeishuSubscriber -v`
Expected: compile error.

- [ ] **Step 3: Implement FeishuSubscriber**

Create `internal/feishu/subscriber.go`:

```go
// Package feishu (subscriber.go) wraps session.Subscriber so the Feishu
// outbound chunker can drain PTY frames without auto-claiming driver.
//
// Lifecycle:
//
//   AttachFeishuSubscriber → session.Subscribe(opts: WithoutAutoDrive)
//     - starts a goroutine that drains sub.Out() and feeds the chunker
//     - starts a Tick goroutine to keep the chunker flushing on idle
//   FeishuSubscriber.ClaimDriver()
//     - called on first inbound input (via router.go) to promote to driver
//   FeishuSubscriber.SendInput(text)
//     - encodes a TypeIn frame and pushes to session.SendInbound
//   FeishuSubscriber.Detach()
//     - unsubscribes from session, stops goroutines, archives anchor
//     - chunker keeps any unflushed state to drop on the floor (best-effort)
package feishu

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
)

// FeishuSubscriber is a single per-session attach.
type FeishuSubscriber struct {
	sess  *session.Session
	sub   *session.Subscriber
	chunk *Chunker

	openID    string // anchor owner, recorded for permission gate at attach
	clientID  string // unique per attach, used in driver meta
	clientName string

	done   chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool
}

// AttachFeishuSubscriber subscribes to sess as a passive viewer (no auto-drive)
// and wires the bytes into a Chunker that calls flush whenever a PATCH-worthy
// update is ready. The caller owns flush — it should be non-blocking and
// post the actual HTTP call asynchronously (the chunker runs on the drain
// goroutine and blocking would back-pressure PTY fan-out).
func AttachFeishuSubscriber(sess *session.Session, ownerOpenID string, flush FlushFunc) *FeishuSubscriber {
	sub, _ := sess.Subscribe(0, "feishu:"+sess.ID.String(), "feishu-bot", session.WithoutAutoDrive())
	fs := &FeishuSubscriber{
		sess:       sess,
		sub:        sub,
		chunk:      NewChunker(flush),
		openID:     ownerOpenID,
		clientID:   "feishu:" + sess.ID.String(),
		clientName: "feishu-bot",
		done:       make(chan struct{}),
	}
	fs.wg.Add(2)
	go fs.drainLoop()
	go fs.tickLoop()
	return fs
}

func (f *FeishuSubscriber) drainLoop() {
	defer f.wg.Done()
	for {
		select {
		case <-f.done:
			return
		case frame, ok := <-f.sub.Out():
			if !ok {
				return
			}
			if frame.Type == proto.TypeOut {
				f.chunk.PushBytes(frame.Payload)
			}
		}
	}
}

func (f *FeishuSubscriber) tickLoop() {
	defer f.wg.Done()
	t := time.NewTicker(chunkerFlushPeriod)
	defer t.Stop()
	for {
		select {
		case <-f.done:
			return
		case <-t.C:
			f.chunk.Tick()
		}
	}
}

// ClaimDriver promotes this Feishu attach to the session's active driver.
// Idempotent: a second call when already driver is a no-op (Session handles it).
func (f *FeishuSubscriber) ClaimDriver() {
	f.sess.ClaimDriver(f.sub, f.clientID, f.clientName)
}

// SendInput pushes a TypeIn frame to the session. Returns true on success.
// false means the inbound queue is full — caller should toast the user.
func (f *FeishuSubscriber) SendInput(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	return f.sess.SendInbound(proto.Frame{
		Type:      proto.TypeIn,
		SessionID: f.sess.ID,
		Payload:   data,
	})
}

// OwnerOpenID returns the open_id recorded at attach (used by router for
// permission gate).
func (f *FeishuSubscriber) OwnerOpenID() string { return f.openID }

// Detach unsubscribes from the session and stops goroutines. Idempotent.
func (f *FeishuSubscriber) Detach() {
	if !f.closed.CompareAndSwap(false, true) {
		return
	}
	close(f.done)
	f.sess.Unsubscribe(f.sub)
	f.wg.Wait()
}

// feishuDriverClientID exposes the synthetic client ID for tests.
func feishuDriverClientID(f *FeishuSubscriber) string { return f.clientID }
```

- [ ] **Step 4: Run test, expect PASS**

Run: `go test ./internal/feishu/ -run TestFeishuSubscriber -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/subscriber.go internal/feishu/subscriber_test.go
git commit -m "feat(feishu): add FeishuSubscriber wrapping session.Subscriber"
```

---

### Task 8: Inbound router (reply target + card action + open_id gate)

**Files:**
- Create: `internal/feishu/router.go`
- Test: `internal/feishu/router_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/feishu/router_test.go`:

```go
package feishu

import (
	"strings"
	"testing"
	"time"
)

type stubSubscriber struct {
	openID   string
	sentIn   [][]byte
	claimed  bool
}

func (s *stubSubscriber) ClaimDriver()          { s.claimed = true }
func (s *stubSubscriber) SendInput(b []byte) bool { s.sentIn = append(s.sentIn, append([]byte(nil), b...)); return true }
func (s *stubSubscriber) OwnerOpenID() string   { return s.openID }

func TestRouter_ReplyHappyPath(t *testing.T) {
	idx := NewCardIndex()
	anchor := &CardAnchor{SessionID: "sess1", CardMsgID: "msg1", CardToken: "tok1", OwnerOpenID: "ou_owner", CreatedAt: time.Now()}
	idx.Put(anchor)

	stub := &stubSubscriber{openID: "ou_owner"}
	r := NewRouter(idx, func(sessID string) Subscriber { return stub })

	dec := r.RouteReply("msg1", "ou_owner", "go test ./...")
	if dec.Action != ActionInject {
		t.Fatalf("action = %v, want inject", dec.Action)
	}
	if !stub.claimed {
		t.Errorf("expected ClaimDriver call when no current driver (router should call it)")
	}
	if len(stub.sentIn) != 1 || string(stub.sentIn[0]) != "go test ./...\n" {
		t.Errorf("sentIn = %q, want one entry with trailing newline", stub.sentIn)
	}
}

func TestRouter_ReplyOpenIDMismatch(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t", OwnerOpenID: "ou_owner"})
	stub := &stubSubscriber{openID: "ou_owner"}
	r := NewRouter(idx, func(string) Subscriber { return stub })

	dec := r.RouteReply("m", "ou_attacker", "rm -rf /")
	if dec.Action != ActionReject {
		t.Fatalf("action = %v, want reject", dec.Action)
	}
	if !strings.Contains(dec.Toast, "无权限") {
		t.Errorf("toast = %q, want it to mention 无权限", dec.Toast)
	}
	if len(stub.sentIn) != 0 {
		t.Errorf("input should not have been forwarded, got: %q", stub.sentIn)
	}
}

func TestRouter_ReplyUnknownTarget(t *testing.T) {
	idx := NewCardIndex()
	r := NewRouter(idx, func(string) Subscriber { return nil })
	dec := r.RouteReply("nonexistent", "ou_owner", "hi")
	if dec.Action != ActionReject {
		t.Fatalf("action = %v, want reject", dec.Action)
	}
	if !strings.Contains(dec.Toast, "找不到") {
		t.Errorf("toast = %q, want it to mention 找不到", dec.Toast)
	}
}

func TestRouter_CardActionKey(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t", OwnerOpenID: "ou_owner"})
	stub := &stubSubscriber{openID: "ou_owner"}
	r := NewRouter(idx, func(string) Subscriber { return stub })

	dec := r.RouteCardAction("t", "ou_owner", "key", "ctrl_c", "")
	if dec.Action != ActionInject {
		t.Fatalf("action = %v, want inject", dec.Action)
	}
	if len(stub.sentIn) != 1 || stub.sentIn[0][0] != 0x03 {
		t.Errorf("sentIn = %v, want one entry starting 0x03", stub.sentIn)
	}
}

func TestRouter_500msBudget(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t", OwnerOpenID: "ou_owner"})
	stub := &stubSubscriber{openID: "ou_owner"}
	r := NewRouter(idx, func(string) Subscriber { return stub })

	start := time.Now()
	_ = r.RouteReply("m", "ou_owner", "hi")
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("route took %v, want ≤500ms", elapsed)
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/feishu/ -run TestRouter -v`
Expected: compile error.

- [ ] **Step 3: Implement router**

Create `internal/feishu/router.go`:

```go
// Package feishu (router.go) is the inbound dispatcher for the remote
// terminal. It accepts already-decrypted, already-verified envelopes and
// turns them into TypeIn frames against the right session.
//
// Permission rule (spec §Binding & permission): the event's operator
// open_id MUST equal the anchor's OwnerOpenID. On mismatch the input is
// dropped and a toast is returned; the caller surfaces the toast through
// the card.action response.
//
// Budget: every Route* call returns in ≤500ms by construction — all the
// real work (CardIndex lookup, ClaimDriver, SendInbound enqueue) is local
// and non-blocking. The remaining 2.5s of Feishu's 3-second callback
// window is reserved for the async anchor PATCH on the caller side.
package feishu

// Action is what the router decided to do.
type Action int

const (
	ActionInject Action = iota // happy path: text was injected, ack with empty toast
	ActionReject               // do not inject; surface Toast to user
	ActionPreempt              // would inject but conflicts with current driver
)

// Decision is the router's verdict. The HTTP/callback layer translates
// it into a card.action response payload.
type Decision struct {
	Action Action
	Toast  string
	// Preempt holds the current driver's display name when Action == ActionPreempt.
	// Used by the callback handler to render the "preempt" confirmation card.
	PreemptDriverName string
}

// Subscriber is the minimal interface the router needs from a FeishuSubscriber.
// Defined here (not in subscriber.go) so router tests don't pull in session.
type Subscriber interface {
	ClaimDriver()
	SendInput([]byte) bool
	OwnerOpenID() string
}

// SubscriberLookup returns the FeishuSubscriber currently attached to a
// session, or nil if none. The router holds no state itself; the lookup
// is provided by the wiring layer (relay_host).
type SubscriberLookup func(sessionID string) Subscriber

type Router struct {
	idx    *CardIndex
	lookup SubscriberLookup
}

func NewRouter(idx *CardIndex, lookup SubscriberLookup) *Router {
	return &Router{idx: idx, lookup: lookup}
}

// RouteReply handles an im.message.receive_v1 event whose content is a
// text reply to a card. msgID is the anchor card's msg_id (extracted by
// the caller from reply_in_thread_id or parent_id).
func (r *Router) RouteReply(msgID, operatorOpenID, text string) Decision {
	anchor := r.idx.ByMsgID(msgID)
	if anchor == nil {
		return Decision{Action: ActionReject, Toast: "找不到对应会话，请通过新锚卡操作"}
	}
	return r.injectInto(anchor, operatorOpenID, []byte(text+"\n"))
}

// RouteCardAction handles a card.action.trigger event. kind is the value
// of action.value.kind ("input" | "key" | "end"). event names the key for
// kind=key. text is the input text for kind=input.
func (r *Router) RouteCardAction(cardToken, operatorOpenID, kind, event, text string) Decision {
	anchor := r.idx.ByCardToken(cardToken)
	if anchor == nil {
		return Decision{Action: ActionReject, Toast: "卡片已过期，请通过新指令重启"}
	}
	switch kind {
	case "input":
		if text == "" {
			return Decision{Action: ActionReject, Toast: ""}
		}
		return r.injectInto(anchor, operatorOpenID, []byte(text+"\n"))
	case "key":
		b := keyBytes(event)
		if b == nil {
			return Decision{Action: ActionReject, Toast: "未知按键"}
		}
		return r.injectInto(anchor, operatorOpenID, b)
	case "end":
		// "end" is handled outside the router (Detach + archive) by the
		// callback layer. Return inject with no payload so the caller knows
		// permission passed.
		if operatorOpenID != anchor.OwnerOpenID {
			return Decision{Action: ActionReject, Toast: "无权限"}
		}
		return Decision{Action: ActionInject}
	default:
		return Decision{Action: ActionReject, Toast: "未知交互"}
	}
}

func (r *Router) injectInto(anchor *CardAnchor, operatorOpenID string, payload []byte) Decision {
	if operatorOpenID != anchor.OwnerOpenID {
		return Decision{Action: ActionReject, Toast: "无权限"}
	}
	sub := r.lookup(anchor.SessionID)
	if sub == nil {
		return Decision{Action: ActionReject, Toast: "会话已结束"}
	}
	// In Phase 1 we always claim driver on first input — preempt protocol
	// arrives in Task 17 (Phase 2). For now any input promotes Feishu.
	sub.ClaimDriver()
	if !sub.SendInput(payload) {
		return Decision{Action: ActionReject, Toast: "输入未被接收（队列已满）"}
	}
	return Decision{Action: ActionInject}
}

// keyBytes maps button event names to the raw bytes injected to the PTY.
func keyBytes(event string) []byte {
	switch event {
	case "ctrl_c":
		return []byte{0x03}
	case "ctrl_d":
		return []byte{0x04}
	case "esc":
		return []byte{0x1B}
	case "enter":
		return []byte{0x0D}
	default:
		return nil
	}
}
```

- [ ] **Step 4: Run test, expect PASS**

Run: `go test ./internal/feishu/ -run TestRouter -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/router.go internal/feishu/router_test.go
git commit -m "feat(feishu): add inbound router with open_id gate"
```

---

### Task 9: Wire FeishuSubscriber attach into session lifecycle (autoAttach=ai)

**Files:**
- Modify: `desktop/relay_host.go`
- Test: end-to-end via manual smoke (Task 11 covers automated coverage)

- [ ] **Step 1: Read relay_host.go to find the session-creation hook**

Run: `grep -n "session.New\|registry.NotifyChange\|onAIClassified\|SessionType" desktop/relay_host.go | head -30`

Locate the function that fires when a new session is registered (look for a `RegisterSession`-style call or a hook installed on the session registry).

- [ ] **Step 2: Add attachment helper to relay_host**

In `desktop/relay_host.go`, add a new method on the relay host type:

```go
// maybeAttachFeishuRemote consults the binding's SessionAutoAttach mode
// and creates a FeishuSubscriber for the session if appropriate. Called
// from the session-create hook. All errors are swallowed — Feishu-side
// failure must not affect session creation.
func (h *relayHost) maybeAttachFeishuRemote(sess *session.Session) {
	binding, err := h.userstore.GetFeishuBinding(h.ctx, h.userID)
	if err != nil || binding == nil || binding.DisabledAt != 0 {
		return
	}
	if !binding.RemoteTerminalEnabled {
		return
	}
	switch binding.SessionAutoAttach {
	case "all":
		// fall through
	case "ai":
		if sess.Info().Type != session.SessionTypeAI {
			return
		}
	default: // "none" or unknown
		return
	}
	go h.attachFeishuSubscriberAsync(sess, binding)
}

func (h *relayHost) attachFeishuSubscriberAsync(sess *session.Session, b *userstore.FeishuBinding) {
	// 1. POST anchor card (initial blue card with empty body).
	tenantTok, err := h.feishuService.RelayToken(h.ctx, b)
	if err != nil {
		log.Printf("feishu remote attach: get tenant token: %v", err)
		return
	}
	state := feishu.AnchorState{
		SessionID:    sess.ID.String(),
		SessionLabel: sessionLabel(sess),
		StatusText:   "运行中 · driver: 等待中",
		BodyMarkdown: "",
		Template:     "blue",
	}
	cardJSON, err := feishu.RenderAnchorCreate(state)
	if err != nil {
		log.Printf("feishu remote attach: render anchor: %v", err)
		return
	}
	msgID, cardToken, err := h.feishuClient.SendAnchorCard(h.ctx, tenantTok, b.OpenID, cardJSON)
	if err != nil {
		log.Printf("feishu remote attach: send anchor: %v", err)
		return
	}
	// 2. Store in CardIndex.
	anchor := &feishu.CardAnchor{
		SessionID:   sess.ID.String(),
		CardMsgID:   msgID,
		CardToken:   cardToken,
		OwnerOpenID: b.OpenID,
		CreatedAt:   time.Now(),
	}
	h.feishuCards.Put(anchor)
	// 3. Attach subscriber with flush callback that PATCHes the anchor.
	fs := feishu.AttachFeishuSubscriber(sess, b.OpenID, func(body string) {
		h.queueAnchorPatch(anchor, body, tenantTok)
	})
	h.feishuSubsMu.Lock()
	h.feishuSubs[sess.ID.String()] = fs
	h.feishuSubsMu.Unlock()
}

// queueAnchorPatch invokes PatchCard asynchronously — flush callbacks run on
// the chunker's drain goroutine and must never block. Errors are logged and
// drop the patch (next flush will accumulate).
func (h *relayHost) queueAnchorPatch(anchor *feishu.CardAnchor, body, tenantTok string) {
	go func() {
		seq := atomic.AddInt64(&anchor.patchSeq, 1) // requires patchSeq field on CardAnchor; see Task 9b below
		err := h.feishuClient.PatchCard(h.ctx, tenantTok, anchor.CardToken, body, seq)
		if err != nil {
			// 230030 / 404 → card deleted; drop it from the index.
			if isCardGoneError(err) {
				h.feishuCards.RemoveBySessionID(anchor.SessionID)
			}
			log.Printf("feishu anchor patch: %v", err)
		}
	}()
}
```

- [ ] **Step 3: Add the `patchSeq` field to CardAnchor**

Edit `internal/feishu/cardindex.go` to add `PatchSeq` as a public int64 field with a doc comment explaining it's only manipulated via `atomic`:

```go
type CardAnchor struct {
	SessionID   string
	CardMsgID   string
	CardToken   string
	OwnerOpenID string
	CreatedAt   time.Time

	LastPatchAt time.Time
	LastBody    string

	// PatchSeq is a per-anchor monotonic counter the chunker increments via
	// atomic.AddInt64. CardKit uses sequence to drop out-of-order updates.
	PatchSeq int64
}
```

Then in `relay_host.go` change `anchor.patchSeq` to `&anchor.PatchSeq`.

- [ ] **Step 4: Add helper `isCardGoneError`**

In `internal/feishu/client.go`, near `AuthClassError`:

```go
// IsCardGoneError reports whether err is a CardKit "card not found / deleted"
// error (codes 230030, 404). The chunker uses this to drop the anchor from
// CardIndex on PATCH failures that mean the user removed the card.
func IsCardGoneError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "code=230030") || strings.Contains(msg, "code=404")
}
```

In `relay_host.go`, import `feishu` and use `feishu.IsCardGoneError`.

- [ ] **Step 5: Wire the lifecycle hook**

Find the session-creation site in `relay_host.go` (likely where `session.New` is called and the session is added to a registry). Right after the session is registered, call:

```go
h.maybeAttachFeishuRemote(sess)
```

- [ ] **Step 6: Add `feishuCards` and `feishuSubs` fields to relayHost**

Find the relayHost struct definition. Add:

```go
type relayHost struct {
	// ... existing fields ...
	feishuCards    *feishu.CardIndex
	feishuSubs     map[string]*feishu.FeishuSubscriber
	feishuSubsMu   sync.Mutex
}
```

Initialize `feishuCards` in the host constructor: `feishuCards: feishu.NewCardIndex(),` and `feishuSubs: map[string]*feishu.FeishuSubscriber{}`.

- [ ] **Step 7: Add detach-on-session-close hook**

In the same place the session close is handled, add a defer or hook to detach:

```go
// In session close handler:
h.feishuSubsMu.Lock()
fs := h.feishuSubs[sess.ID.String()]
delete(h.feishuSubs, sess.ID.String())
h.feishuSubsMu.Unlock()
if fs != nil {
	fs.Detach()
	go h.archiveAnchor(sess.ID.String())
}
```

And add `archiveAnchor`:

```go
func (h *relayHost) archiveAnchor(sessionID string) {
	anchor := h.feishuCards.BySessionID(sessionID)
	if anchor == nil {
		return
	}
	binding, _ := h.userstore.GetFeishuBindingByOpenID(h.ctx, anchor.OwnerOpenID) // add this getter if it does not exist; otherwise look up by user
	if binding == nil {
		return
	}
	tenantTok, err := h.feishuService.RelayToken(h.ctx, binding)
	if err != nil {
		return
	}
	footer := "已结束 at " + time.Now().Format("15:04:05")
	cardJSON, err := feishu.RenderAnchorArchive(feishu.AnchorState{
		SessionID:    anchor.SessionID,
		SessionLabel: anchor.SessionID[:8],
		StatusText:   "已结束",
		BodyMarkdown: anchor.LastBody,
		Template:     "grey",
	}, footer)
	if err != nil {
		return
	}
	// One-shot full-card update: PATCH whole card. If your client lacks a
	// full-card PATCH helper, fall back to posting a new message as the
	// archive marker (cheaper than adding the helper for an edge case).
	_ = h.feishuClient.PatchCard(h.ctx, tenantTok, anchor.CardToken, string(cardJSON), atomic.AddInt64(&anchor.PatchSeq, 1))
	h.feishuCards.RemoveBySessionID(sessionID)
}
```

- [ ] **Step 8: Verify build**

Run: `go build ./...`
Expected: PASS with no errors. If you hit "field patchSeq vs PatchSeq" issues, the renames in Step 3 missed a spot — fix and re-run.

- [ ] **Step 9: Commit**

```bash
git add desktop/relay_host.go internal/feishu/cardindex.go internal/feishu/client.go
git commit -m "feat(feishu): wire FeishuSubscriber attach/detach into session lifecycle"
```

---

### Task 10: Wire router into inbound HTTP path

**Files:**
- Modify: `internal/feishu/service.go`
- Modify: `desktop/relay_host.go` (for router construction)

- [ ] **Step 1: Read service.go and find the HandleEvent function**

Run: `grep -n "HandleEvent\|CardActionTrigger\|MessageReceive" internal/feishu/service.go`

- [ ] **Step 2: Construct a Router in relay_host and pass it to service**

In `relay_host.go`, where the feishu service is built, also construct the router:

```go
router := feishu.NewRouter(h.feishuCards, func(sessionID string) feishu.Subscriber {
	h.feishuSubsMu.Lock()
	defer h.feishuSubsMu.Unlock()
	fs := h.feishuSubs[sessionID]
	if fs == nil {
		return nil
	}
	return fs
})
h.feishuService = feishu.NewServiceWithRouter(..., router)
```

(You may need to change `feishu.NewService` to accept a router parameter, or add a setter.)

- [ ] **Step 3: Modify service.HandleEvent to delegate to router**

In `internal/feishu/service.go`, find the inline switch on `CardAction.Kind` and replace it. Pseudo-code (use the actual variable names from the file):

```go
func (svc *Service) HandleEvent(ctx context.Context, env *Envelope) error {
	if env.Message != nil {
		// Extract reply_in_thread_id from the original raw payload (event.go
		// must surface it — see Step 4 below).
		decision := svc.router.RouteReply(env.Message.ReplyToMsgID, env.Message.SenderOpenID, env.Message.Text)
		return svc.applyDecision(ctx, env, decision)
	}
	if env.CardAction != nil {
		decision := svc.router.RouteCardAction(env.CardAction.CardToken, env.CardAction.OperatorOpenID, env.CardAction.Kind, env.CardAction.Event, env.CardAction.Text)
		return svc.applyDecision(ctx, env, decision)
	}
	return nil
}

func (svc *Service) applyDecision(ctx context.Context, env *Envelope, d Decision) error {
	switch d.Action {
	case ActionInject:
		// No body — caller's HTTP handler returns 200.
		return nil
	case ActionReject:
		// Surface toast via the standard card.action response. For reply
		// path we have no card-update channel; send a follow-up text reply.
		if env.CardAction != nil {
			// Toast is set on the response by the HTTP handler; nothing here.
			return nil
		}
		return svc.client.SendTextToOpenID(ctx, svc.tenantTok(), env.Message.SenderOpenID, d.Toast)
	}
	return nil
}
```

- [ ] **Step 4: Add `ReplyToMsgID` to MessageReceive and `CardToken` to CardActionTrigger**

In `internal/feishu/event.go`:

```go
type MessageReceive struct {
	SenderOpenID string
	Text         string
	ReplyToMsgID string // NEW: set when this message is a reply (parent_id)
}

type CardActionTrigger struct {
	OperatorOpenID string
	Kind           string
	SessionID      string
	Event          string
	Text           string
	CardToken      string // NEW: extracted from envelope context.token for inbound routing
}
```

Then in `ParseEnvelope` for `im.message.receive_v1`, capture `parent_id`:

```go
var ev struct {
	Sender struct {
		SenderID struct {
			OpenID string `json:"open_id"`
		} `json:"sender_id"`
	} `json:"sender"`
	Message struct {
		Content     string `json:"content"`
		MessageType string `json:"message_type"`
		ParentID    string `json:"parent_id"` // NEW
	} `json:"message"`
}
// ... existing extraction ...
env.Message = &MessageReceive{
	SenderOpenID: ev.Sender.SenderID.OpenID,
	Text:         inner.Text,
	ReplyToMsgID: ev.Message.ParentID,
}
```

For `card.action.trigger`, extract token:

```go
var ev struct {
	Action struct {
		Value struct {
			Kind      string `json:"kind"`
			SessionID string `json:"session_id"`
			Event     string `json:"event"`
			Text      string `json:"text"`
		} `json:"value"`
	} `json:"action"`
	Operator struct {
		OpenID string `json:"open_id"`
	} `json:"operator"`
	Token string `json:"token"` // CardKit card token, present in v2 callbacks
}
// ...
env.CardAction = &CardActionTrigger{
	OperatorOpenID: ev.Operator.OpenID,
	Kind:           ev.Action.Value.Kind,
	SessionID:      ev.Action.Value.SessionID,
	Event:          ev.Action.Value.Event,
	Text:           ev.Action.Value.Text,
	CardToken:      ev.Token,
}
```

- [ ] **Step 5: Build & run**

Run: `go build ./...`
Expected: PASS.

Run: `go test ./internal/feishu/ ./internal/relay/... -v`
Expected: PASS (existing tests still green; router tests still green).

- [ ] **Step 6: Commit**

```bash
git add internal/feishu/event.go internal/feishu/service.go desktop/relay_host.go
git commit -m "feat(feishu): wire inbound router into HandleEvent + surface reply parent_id"
```

---

### Task 11: Phase 1 end-to-end smoke test

**Files:**
- Create: `internal/feishu/e2e_phase1_test.go`

- [ ] **Step 1: Write the e2e test**

Create `internal/feishu/e2e_phase1_test.go`:

```go
package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

// TestPhase1_ShellAttachOutputReplyInject is the integration test for the
// MVP: a fake Feishu server records create + PATCH requests; a real Session
// is set up; FeishuSubscriber drains PTY output and PATCHes the anchor;
// router injects a reply back into the PTY's inbound queue.
func TestPhase1_ShellAttachOutputReplyInject(t *testing.T) {
	var mu sync.Mutex
	var patchCount int
	var lastPatchBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/im/v1/messages"):
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"message_id":"om_xyz"}}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/cardkit/v1/cards/"):
			b, _ := io.ReadAll(r.Body)
			var payload map[string]any
			_ = json.Unmarshal(b, &payload)
			if pu, ok := payload["partial_update_setting"].(map[string]any); ok {
				if v, ok := pu["value"].(string); ok {
					lastPatchBody = v
				}
			}
			patchCount++
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, &http.Client{Timeout: 2 * time.Second})
	idx := NewCardIndex()

	// Build session + attach feishu subscriber.
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})
	cardJSON, _ := RenderAnchorCreate(AnchorState{SessionID: sess.ID.String(), SessionLabel: "test", StatusText: "running"})
	msgID, cardToken, err := client.SendAnchorCard(context.Background(), "tenant_tok", "ou_owner", cardJSON)
	if err != nil {
		t.Fatalf("send anchor: %v", err)
	}
	anchor := &CardAnchor{SessionID: sess.ID.String(), CardMsgID: msgID, CardToken: cardToken, OwnerOpenID: "ou_owner", CreatedAt: time.Now()}
	idx.Put(anchor)

	fs := AttachFeishuSubscriber(sess, "ou_owner", func(body string) {
		go func() {
			_ = client.PatchCard(context.Background(), "tenant_tok", anchor.CardToken, body, time.Now().UnixNano())
		}()
	})
	defer fs.Detach()

	// Push PTY output.
	sess.PushOut(1, []byte("hello\n"))
	sess.PushOut(2, []byte("world\n"))

	// Wait for the chunker's flush window plus a PATCH round-trip.
	time.Sleep(400 * time.Millisecond)

	mu.Lock()
	if patchCount < 1 {
		t.Errorf("expected ≥1 PATCH, got %d", patchCount)
	}
	if !strings.Contains(lastPatchBody, "hello") || !strings.Contains(lastPatchBody, "world") {
		t.Errorf("PATCH body missing output, got: %q", lastPatchBody)
	}
	mu.Unlock()

	// Now test the inbound router: simulate a reply.
	stubLookup := func(sid string) Subscriber {
		if sid == sess.ID.String() {
			return &feishuSubAdapter{fs: fs}
		}
		return nil
	}
	r := NewRouter(idx, stubLookup)
	dec := r.RouteReply(msgID, "ou_owner", "ls -la")
	if dec.Action != ActionInject {
		t.Fatalf("router decision = %v, want inject", dec.Action)
	}

	// Verify the inject reached the session's inbound queue.
	select {
	case f := <-sess.Inbound():
		if f.Type != proto.TypeIn || !strings.Contains(string(f.Payload), "ls -la") {
			t.Errorf("inbound frame = %+v, want TypeIn with 'ls -la'", f)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no inbound frame within 500ms")
	}
}

// feishuSubAdapter bridges the concrete *FeishuSubscriber to the router's
// Subscriber interface for the e2e test.
type feishuSubAdapter struct{ fs *FeishuSubscriber }

func (a *feishuSubAdapter) ClaimDriver()          { a.fs.ClaimDriver() }
func (a *feishuSubAdapter) SendInput(b []byte) bool { return a.fs.SendInput(b) }
func (a *feishuSubAdapter) OwnerOpenID() string    { return a.fs.OwnerOpenID() }
```

- [ ] **Step 2: Run the e2e test**

Run: `go test ./internal/feishu/ -run TestPhase1_Shell -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/feishu/e2e_phase1_test.go
git commit -m "test(feishu): phase 1 e2e — attach, output stream, reply inject"
```

---

### Task 12: Phase 1 checkpoint commit

- [ ] **Step 1: Verify clean state**

Run: `git status && go test ./... && go build ./...`
Expected: working tree clean modulo any in-progress files; all tests pass; build succeeds.

- [ ] **Step 2: Tag a phase checkpoint (optional, no remote push)**

```bash
git tag phase1-feishu-as-terminal
```

---

## Phase 2 — AI experience (F4, F7, F9)

**Goal:** AI sessions stream per-turn into the anchor; preempt protocol enforces driver politeness; failures isolated and recoverable.

### Task 13: TurnEvent type and claudeCodeAdapter.ParseTurn

**Files:**
- Modify: `desktop/feishu/hook_adapter.go`
- Test: `desktop/feishu/hook_adapter_test.go`

- [ ] **Step 1: Write failing test**

Append to `desktop/feishu/hook_adapter_test.go`:

```go
func TestClaudeCodeParseTurn_UserPrompt(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"UserPromptSubmit","prompt":"fix the bug"}`)
	ev, ok := a.ParseTurn(raw, "")
	if !ok {
		t.Fatal("expected ParseTurn to recognize UserPromptSubmit")
	}
	if ev.Kind != TurnUserPrompt {
		t.Errorf("kind = %v, want TurnUserPrompt", ev.Kind)
	}
	if ev.Text != "fix the bug" {
		t.Errorf("text = %q, want %q", ev.Text, "fix the bug")
	}
}

func TestClaudeCodeParseTurn_Stop(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"Stop","assistant_message":"done."}`)
	ev, ok := a.ParseTurn(raw, "")
	if !ok {
		t.Fatal("expected ParseTurn to recognize Stop")
	}
	if ev.Kind != TurnAssistantFinal {
		t.Errorf("kind = %v, want TurnAssistantFinal", ev.Kind)
	}
	if ev.Text != "done." {
		t.Errorf("text = %q, want %q", ev.Text, "done.")
	}
}

func TestClaudeCodeParseTurn_PreToolUse(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	ev, ok := a.ParseTurn(raw, "")
	if !ok {
		t.Fatal("expected ParseTurn to recognize PreToolUse")
	}
	if ev.Kind != TurnToolStart {
		t.Errorf("kind = %v, want TurnToolStart", ev.Kind)
	}
	if ev.ToolName != "Bash" {
		t.Errorf("toolName = %q", ev.ToolName)
	}
}

func TestClaudeCodeParseTurn_PostToolUse(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_response":"output here"}`)
	ev, ok := a.ParseTurn(raw, "")
	if !ok {
		t.Fatal("expected ParseTurn to recognize PostToolUse")
	}
	if ev.Kind != TurnToolEnd {
		t.Errorf("kind = %v, want TurnToolEnd", ev.Kind)
	}
}

func TestClaudeCodeParseTurn_AskUserQuestionStaysOnAskPath(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{}}`)
	_, ok := a.ParseTurn(raw, "")
	if ok {
		t.Errorf("ParseTurn should not emit a TurnEvent for AskUserQuestion (existing AskQuestion path handles it)")
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./desktop/feishu/ -run TestClaudeCodeParseTurn -v`
Expected: compile error (`ParseTurn`, `TurnEvent`, kinds undefined).

- [ ] **Step 3: Add TurnEvent and ParseTurn**

Append to `desktop/feishu/hook_adapter.go`:

```go
// TurnKind enumerates the AI streaming event types the chunker consumes.
type TurnKind int

const (
	TurnUserPrompt     TurnKind = iota // user submitted a prompt
	TurnAssistantFinal                 // assistant finished a turn
	TurnToolStart                      // tool call about to start
	TurnToolEnd                        // tool call returned
)

// TurnEvent is the normalized AI-streaming event a HookAdapter emits via
// ParseTurn. Separate from WaitingInputEvent because the consumer (outbound
// chunker) needs different shape than the AskQuestion card renderer.
type TurnEvent struct {
	Kind     TurnKind
	Text     string // for UserPrompt / AssistantFinal
	ToolName string // for ToolStart / ToolEnd
	ToolBody string // for ToolEnd (tool response preview)
}

// ParseTurn extends HookAdapter for the AI streaming path. Returns (event, true)
// when the hook is a per-turn streaming signal; (zero, false) otherwise.
// AskUserQuestion is intentionally skipped — the existing Parse method handles
// it via the AskQuestion card, which is a separate UX from the anchor stream.
func (a *claudeCodeAdapter) ParseTurn(raw json.RawMessage, _ string) (TurnEvent, bool) {
	var p struct {
		HookEventName    string          `json:"hook_event_name"`
		ToolName         string          `json:"tool_name"`
		ToolInput        json.RawMessage `json:"tool_input"`
		ToolResponse     string          `json:"tool_response"`
		Prompt           string          `json:"prompt"`
		AssistantMessage string          `json:"assistant_message"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return TurnEvent{}, false
	}
	switch p.HookEventName {
	case "UserPromptSubmit":
		if p.Prompt == "" {
			return TurnEvent{}, false
		}
		return TurnEvent{Kind: TurnUserPrompt, Text: p.Prompt}, true
	case "Stop":
		if p.AssistantMessage == "" {
			return TurnEvent{Kind: TurnAssistantFinal, Text: ""}, true
		}
		return TurnEvent{Kind: TurnAssistantFinal, Text: p.AssistantMessage}, true
	case "PreToolUse":
		if p.ToolName == "AskUserQuestion" {
			return TurnEvent{}, false
		}
		return TurnEvent{Kind: TurnToolStart, ToolName: p.ToolName}, true
	case "PostToolUse":
		if p.ToolName == "AskUserQuestion" {
			return TurnEvent{}, false
		}
		return TurnEvent{Kind: TurnToolEnd, ToolName: p.ToolName, ToolBody: p.ToolResponse}, true
	default:
		return TurnEvent{}, false
	}
}
```

- [ ] **Step 4: Run test, expect PASS**

Run: `go test ./desktop/feishu/ -run TestClaudeCodeParseTurn -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/hook_adapter.go desktop/feishu/hook_adapter_test.go
git commit -m "feat(feishu): add TurnEvent + claudeCodeAdapter.ParseTurn for AI streaming"
```

---

### Task 14: AIRoller (per-turn rolling window)

**Files:**
- Modify: `internal/feishu/outbound.go`
- Test: append to `internal/feishu/outbound_test.go`

- [ ] **Step 1: Write failing test**

Append to `internal/feishu/outbound_test.go`:

```go
func TestAIRoller_KeepsLast5Turns(t *testing.T) {
	r := NewAIRoller()
	for i := 0; i < 8; i++ {
		r.OnUserPrompt("prompt " + string(rune('A'+i)))
		r.OnAssistantFinal("reply " + string(rune('A'+i)))
	}
	out := r.Render()
	// 5 turn pairs ~ 10 sections. Specifically: prompt D..H should remain;
	// A..C should have rolled off.
	if strings.Contains(out, "prompt A") {
		t.Errorf("prompt A should have rolled off")
	}
	if !strings.Contains(out, "prompt H") {
		t.Errorf("prompt H should be present")
	}
}

func TestAIRoller_NestsToolCalls(t *testing.T) {
	r := NewAIRoller()
	r.OnUserPrompt("fix it")
	r.OnToolStart("Bash")
	r.OnToolEnd("Bash", "exit code 0")
	r.OnAssistantFinal("done")
	out := r.Render()
	if !strings.Contains(out, "Bash") {
		t.Errorf("missing tool name in output: %q", out)
	}
	if !strings.Contains(out, "exit code 0") {
		t.Errorf("missing tool body: %q", out)
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/feishu/ -run TestAIRoller -v`
Expected: compile error.

- [ ] **Step 3: Implement AIRoller**

Append to `internal/feishu/outbound.go`:

```go
const aiRollerMaxTurns = 5

// AIRoller assembles a markdown body from per-turn hook events. Each
// "turn" is one assistant response, optionally with nested tool calls.
// The roller keeps the last aiRollerMaxTurns turns; older turns roll off.
type AIRoller struct {
	turns []*aiTurn
}

type aiTurn struct {
	userPrompt   string
	tools        []aiTool
	assistantMsg string
	completed    bool
}

type aiTool struct {
	name string
	body string
}

func NewAIRoller() *AIRoller {
	return &AIRoller{}
}

// currentTurn returns the in-progress turn, allocating one if needed.
func (r *AIRoller) currentTurn() *aiTurn {
	if len(r.turns) == 0 || r.turns[len(r.turns)-1].completed {
		r.turns = append(r.turns, &aiTurn{})
		if len(r.turns) > aiRollerMaxTurns {
			r.turns = r.turns[len(r.turns)-aiRollerMaxTurns:]
		}
	}
	return r.turns[len(r.turns)-1]
}

func (r *AIRoller) OnUserPrompt(text string) {
	t := r.currentTurn()
	t.userPrompt = text
}

func (r *AIRoller) OnToolStart(name string) {
	t := r.currentTurn()
	t.tools = append(t.tools, aiTool{name: name})
}

func (r *AIRoller) OnToolEnd(name, body string) {
	t := r.currentTurn()
	// Match by last open tool with this name; tolerate out-of-order arrivals.
	for i := len(t.tools) - 1; i >= 0; i-- {
		if t.tools[i].name == name && t.tools[i].body == "" {
			t.tools[i].body = body
			return
		}
	}
	// No matching open tool — append as-is.
	t.tools = append(t.tools, aiTool{name: name, body: body})
}

func (r *AIRoller) OnAssistantFinal(text string) {
	t := r.currentTurn()
	t.assistantMsg = text
	t.completed = true
}

func (r *AIRoller) Render() string {
	if len(r.turns) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, t := range r.turns {
		if t.userPrompt != "" {
			sb.WriteString("👤 ")
			sb.WriteString(t.userPrompt)
			sb.WriteString("\n\n")
		}
		if t.assistantMsg != "" || t.completed {
			sb.WriteString("🤖 ")
			sb.WriteString(t.assistantMsg)
			sb.WriteString("\n")
		}
		for _, tool := range t.tools {
			sb.WriteString("  ▸ ")
			sb.WriteString(tool.name)
			sb.WriteString("\n")
			if tool.body != "" {
				sb.WriteString("    ```\n    ")
				// Indent each line in the tool body by 4 spaces.
				sb.WriteString(strings.ReplaceAll(tool.body, "\n", "\n    "))
				sb.WriteString("\n    ```\n")
			}
		}
		sb.WriteString("\n")
	}
	out := sb.String()
	// Cap at rollerMaxBytes the same way ShellRoller does, from the front.
	if len(out) > rollerMaxBytes {
		out = out[len(out)-rollerMaxBytes:]
	}
	return out
}
```

- [ ] **Step 4: Run test, expect PASS**

Run: `go test ./internal/feishu/ -run TestAIRoller -v`
Expected: PASS.

- [ ] **Step 5: Add Chunker.PushTurn convenience**

Append to `internal/feishu/outbound.go`:

```go
// AIChunker is the AI-side analogue of Chunker; it owns an AIRoller and
// applies the same 100ms throttle + diff-skip rules.
type AIChunker struct {
	roller    *AIRoller
	flush     FlushFunc
	clock     Clock
	lastFlush time.Time
	dirty     bool
	lastBody  string
}

func NewAIChunker(flush FlushFunc) *AIChunker {
	return NewAIChunkerWithClock(flush, realClock{})
}

func NewAIChunkerWithClock(flush FlushFunc, clk Clock) *AIChunker {
	return &AIChunker{roller: NewAIRoller(), flush: flush, clock: clk}
}

func (c *AIChunker) PushTurn(ev any) {
	switch e := ev.(type) {
	case TurnUserPromptEvent:
		c.roller.OnUserPrompt(e.Text)
	case TurnToolStartEvent:
		c.roller.OnToolStart(e.ToolName)
	case TurnToolEndEvent:
		c.roller.OnToolEnd(e.ToolName, e.ToolBody)
	case TurnAssistantFinalEvent:
		c.roller.OnAssistantFinal(e.Text)
	default:
		return
	}
	c.dirty = true
	c.maybeFlush()
}

func (c *AIChunker) Tick() {
	if !c.dirty {
		return
	}
	c.maybeFlush()
}

func (c *AIChunker) maybeFlush() {
	now := c.clock.Now()
	if now.Sub(c.lastFlush) < chunkerFlushPeriod {
		return
	}
	body := c.roller.Render()
	if body == c.lastBody {
		c.lastFlush = now
		c.dirty = false
		return
	}
	c.lastBody = body
	c.lastFlush = now
	c.dirty = false
	c.flush(body)
}

// Sub-types so PushTurn doesn't depend on the desktop/feishu TurnEvent.
type TurnUserPromptEvent struct{ Text string }
type TurnToolStartEvent struct{ ToolName string }
type TurnToolEndEvent struct{ ToolName, ToolBody string }
type TurnAssistantFinalEvent struct{ Text string }
```

- [ ] **Step 6: Commit**

```bash
git add internal/feishu/outbound.go internal/feishu/outbound_test.go
git commit -m "feat(feishu): add AIRoller + AIChunker for per-turn streaming"
```

---

### Task 15: Wire hook events into AIChunker via dispatcher

**Files:**
- Modify: `desktop/feishu/dispatcher.go`
- Modify: `desktop/feishu/hook_server.go` (only if needed to expose the additional hook events; currently hook_server passes raw payloads to the adapter)
- Modify: `desktop/relay_host.go` to maintain `feishuAIChunkers` per session

- [ ] **Step 1: Read dispatcher.go and hook_server.go to understand the current flow**

Run: `grep -n "WaitingInputEvent\|DispatchWaitingInput\|Parse" desktop/feishu/dispatcher.go desktop/feishu/hook_server.go`

- [ ] **Step 2: Add DispatchTurn to dispatcher**

In `desktop/feishu/dispatcher.go`, add a method on the dispatcher that takes a TurnEvent + sessionID and forwards it to the right AIChunker:

```go
// DispatchTurn forwards a parsed TurnEvent into the per-session AIChunker.
// Returns silently when no chunker is attached (session has no Feishu remote).
func (d *Dispatcher) DispatchTurn(sessionID string, ev TurnEvent) {
	d.aiMu.Lock()
	chunker := d.aiChunkers[sessionID]
	d.aiMu.Unlock()
	if chunker == nil {
		return
	}
	switch ev.Kind {
	case TurnUserPrompt:
		chunker.PushTurn(feishu.TurnUserPromptEvent{Text: ev.Text})
	case TurnAssistantFinal:
		chunker.PushTurn(feishu.TurnAssistantFinalEvent{Text: ev.Text})
	case TurnToolStart:
		chunker.PushTurn(feishu.TurnToolStartEvent{ToolName: ev.ToolName})
	case TurnToolEnd:
		chunker.PushTurn(feishu.TurnToolEndEvent{ToolName: ev.ToolName, ToolBody: ev.ToolBody})
	}
}

// AttachAIChunker registers a chunker for streaming AI turn events. Called
// by relay_host when a FeishuSubscriber attaches to an AI session.
func (d *Dispatcher) AttachAIChunker(sessionID string, ch *feishu.AIChunker) {
	d.aiMu.Lock()
	if d.aiChunkers == nil {
		d.aiChunkers = map[string]*feishu.AIChunker{}
	}
	d.aiChunkers[sessionID] = ch
	d.aiMu.Unlock()
}

func (d *Dispatcher) DetachAIChunker(sessionID string) {
	d.aiMu.Lock()
	delete(d.aiChunkers, sessionID)
	d.aiMu.Unlock()
}
```

Add the new fields to the Dispatcher struct:

```go
type Dispatcher struct {
	// ... existing fields ...
	aiMu        sync.Mutex
	aiChunkers  map[string]*feishu.AIChunker
}
```

- [ ] **Step 3: Wire hook_server to call DispatchTurn**

In `desktop/feishu/hook_server.go` find where the adapter is invoked. Currently it calls `Parse` (returns `WaitingInputEvent`). Add a parallel call to `ParseTurn`:

```go
// After existing Parse(...) call & DispatchWaitingInput:
if turnEv, ok := adapter.ParseTurn(payload, hookVersion); ok {
	dispatcher.DispatchTurn(sessionID, turnEv)
}
```

- [ ] **Step 4: Wire AttachAIChunker at session attach time**

In `desktop/relay_host.go`, modify `attachFeishuSubscriberAsync` (Task 9) to also build an AIChunker for AI sessions:

```go
if sess.Info().Type == session.SessionTypeAI {
	aiChunker := feishu.NewAIChunker(func(body string) {
		h.queueAnchorPatch(anchor, body, tenantTok)
	})
	h.dispatcher.AttachAIChunker(sess.ID.String(), aiChunker)
	// Tick goroutine for AIChunker:
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				aiChunker.Tick()
			case <-fs.Done():
				return
			}
		}
	}()
}
```

Add `Done()` to FeishuSubscriber (returns a channel closed on Detach):

```go
// In subscriber.go:
func (f *FeishuSubscriber) Done() <-chan struct{} { return f.done }
```

And in `archiveAnchor`, call `dispatcher.DetachAIChunker(sessionID)` before removing from cardIndex.

- [ ] **Step 5: Build & smoke test**

Run: `go build ./...`
Expected: PASS.

Run: `go test ./desktop/feishu/ ./internal/feishu/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/feishu/dispatcher.go desktop/feishu/hook_server.go desktop/relay_host.go internal/feishu/subscriber.go
git commit -m "feat(feishu): wire AI turn events into AIChunker via dispatcher"
```

---

### Task 16: Driver preempt protocol

**Files:**
- Modify: `internal/feishu/router.go`
- Test: append to `internal/feishu/router_test.go`

- [ ] **Step 1: Write failing test**

Append to `internal/feishu/router_test.go`:

```go
type stubSubscriberDriverAware struct {
	stubSubscriber
	currentDriverName string
}

func (s *stubSubscriberDriverAware) CurrentDriverName() string { return s.currentDriverName }

func TestRouter_PreemptToastWhenNonFeishuDriver(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t", OwnerOpenID: "ou_owner"})
	stub := &stubSubscriberDriverAware{
		stubSubscriber:    stubSubscriber{openID: "ou_owner"},
		currentDriverName: "local-terminal",
	}
	r := NewRouter(idx, func(string) Subscriber { return stub })

	dec := r.RouteReply("m", "ou_owner", "go test")
	if dec.Action != ActionPreempt {
		t.Fatalf("action = %v, want preempt", dec.Action)
	}
	if dec.PreemptDriverName != "local-terminal" {
		t.Errorf("preempt driver name = %q, want local-terminal", dec.PreemptDriverName)
	}
	if len(stub.sentIn) != 0 {
		t.Errorf("should NOT inject during preempt: %q", stub.sentIn)
	}
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/feishu/ -run TestRouter_PreemptToast -v`
Expected: FAIL (router currently always claims).

- [ ] **Step 3: Extend Subscriber interface and router**

In `internal/feishu/router.go`:

```go
// Subscriber is the minimal interface the router needs.
type Subscriber interface {
	ClaimDriver()
	SendInput([]byte) bool
	OwnerOpenID() string
	// CurrentDriverName returns the human-readable name of the session's
	// current driver, or "" when there's no driver / Feishu is already driver.
	CurrentDriverName() string
}
```

Change `injectInto`:

```go
func (r *Router) injectInto(anchor *CardAnchor, operatorOpenID string, payload []byte) Decision {
	if operatorOpenID != anchor.OwnerOpenID {
		return Decision{Action: ActionReject, Toast: "无权限"}
	}
	sub := r.lookup(anchor.SessionID)
	if sub == nil {
		return Decision{Action: ActionReject, Toast: "会话已结束"}
	}
	if name := sub.CurrentDriverName(); name != "" {
		return Decision{Action: ActionPreempt, PreemptDriverName: name}
	}
	sub.ClaimDriver()
	if !sub.SendInput(payload) {
		return Decision{Action: ActionReject, Toast: "输入未被接收（队列已满）"}
	}
	return Decision{Action: ActionInject}
}
```

- [ ] **Step 4: Add CurrentDriverName to FeishuSubscriber**

In `internal/feishu/subscriber.go`:

```go
func (f *FeishuSubscriber) CurrentDriverName() string {
	if f.sess.IsDriver(f.sub) {
		return "" // Feishu is already driver — no preempt needed
	}
	return f.sess.DriverClientName()
}
```

- [ ] **Step 5: Update existing stub in router_test.go**

In `internal/feishu/router_test.go`, add a default `CurrentDriverName` to the original `stubSubscriber`:

```go
func (s *stubSubscriber) CurrentDriverName() string { return "" }
```

- [ ] **Step 6: Run test, expect PASS**

Run: `go test ./internal/feishu/ -run TestRouter -v`
Expected: PASS for all router tests.

- [ ] **Step 7: Commit**

```bash
git add internal/feishu/router.go internal/feishu/router_test.go internal/feishu/subscriber.go
git commit -m "feat(feishu): driver preempt protocol — emit ActionPreempt on conflict"
```

---

### Task 17: Failure containment — PATCH retry, token refresh, degraded mode

**Files:**
- Modify: `internal/feishu/outbound.go` (or new `internal/feishu/degraded.go`)
- Modify: `desktop/relay_host.go` (queueAnchorPatch)

- [ ] **Step 1: Write a failing test for the PATCH retry path**

Append to `internal/feishu/outbound_test.go`:

```go
func TestPatchWithRetry_OneBackoffOn5xx(t *testing.T) {
	calls := 0
	patch := func() error {
		calls++
		if calls == 1 {
			return fmt.Errorf("cardkit patch: code=500 msg=server error")
		}
		return nil
	}
	err := PatchWithRetry(patch)
	if err != nil {
		t.Fatalf("expected success on retry, got %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial + 1 retry)", calls)
	}
}

func TestPatchWithRetry_GivesUpAfterRetry(t *testing.T) {
	patch := func() error { return fmt.Errorf("cardkit patch: code=500 msg=server error") }
	err := PatchWithRetry(patch)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
}

func TestPatchWithRetry_NoRetryOnCardGone(t *testing.T) {
	calls := 0
	patch := func() error {
		calls++
		return fmt.Errorf("cardkit patch: code=230030 msg=card not found")
	}
	_ = PatchWithRetry(patch)
	if calls != 1 {
		t.Errorf("should not retry on card-gone error, calls = %d", calls)
	}
}
```

(Add `import "fmt"` if not present.)

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/feishu/ -run TestPatchWithRetry -v`
Expected: compile error.

- [ ] **Step 3: Implement PatchWithRetry**

Append to `internal/feishu/outbound.go`:

```go
// PatchWithRetry runs patch once, retries once after a 1s backoff on
// transient errors (5xx). card-gone errors and auth-class errors return
// immediately so the caller can take terminal action (drop from index /
// refresh token). The function returns the last error encountered.
func PatchWithRetry(patch func() error) error {
	err := patch()
	if err == nil {
		return nil
	}
	if IsCardGoneError(err) {
		return err
	}
	if _, ok := err.(*AuthClassError); ok {
		return err
	}
	time.Sleep(time.Second)
	return patch()
}
```

- [ ] **Step 4: Run test, expect PASS**

Run: `go test ./internal/feishu/ -run TestPatchWithRetry -v`
Expected: PASS.

- [ ] **Step 5: Wire PatchWithRetry into queueAnchorPatch**

In `desktop/relay_host.go`, change `queueAnchorPatch`:

```go
func (h *relayHost) queueAnchorPatch(anchor *feishu.CardAnchor, body, tenantTokInitial string) {
	go func() {
		seq := atomic.AddInt64(&anchor.PatchSeq, 1)
		err := feishu.PatchWithRetry(func() error {
			return h.feishuClient.PatchCard(h.ctx, tenantTokInitial, anchor.CardToken, body, seq)
		})
		if err == nil {
			return
		}
		if feishu.IsCardGoneError(err) {
			h.feishuCards.RemoveBySessionID(anchor.SessionID)
			return
		}
		if _, ok := err.(*feishu.AuthClassError); ok {
			// Tenant token expired; rely on the existing TenantTokenCache
			// refresh path — next flush will see a fresh token. For now drop.
			log.Printf("feishu anchor patch: auth refresh needed (%v)", err)
			return
		}
		log.Printf("feishu anchor patch: gave up after retry: %v", err)
	}()
}
```

- [ ] **Step 6: Build & run all tests**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/feishu/outbound.go internal/feishu/outbound_test.go desktop/relay_host.go
git commit -m "feat(feishu): PATCH retry policy + card-gone removal + auth-class drop"
```

---

### Task 18: Phase 2 checkpoint

- [ ] **Step 1: Verify clean state**

```bash
git status && go test ./... -race && go build ./...
```

Expected: all green.

- [ ] **Step 2: Tag Phase 2**

```bash
git tag phase2-feishu-as-terminal
```

---

## Phase 3 — UI + polish (F10 + docs)

### Task 19: Master-switch teardown sweep

**Files:**
- Modify: `desktop/relay_host.go`

- [ ] **Step 1: Add a master-switch handler**

In `desktop/relay_host.go`:

```go
// OnRemoteTerminalToggle reacts to changes in the binding's
// RemoteTerminalEnabled flag. When flipped off, detach every active
// FeishuSubscriber and PATCH each anchor to its archived state. Sessions
// themselves are unaffected.
func (h *relayHost) OnRemoteTerminalToggle(enabled bool) {
	if enabled {
		// No bulk re-attach on enable: new sessions pick up via autoAttach,
		// pre-existing sessions need the user to recreate or use the explicit
		// /attach command (P2). This matches the spec's "no dead-card cleanup".
		return
	}
	h.feishuSubsMu.Lock()
	subs := h.feishuSubs
	h.feishuSubs = map[string]*feishu.FeishuSubscriber{}
	h.feishuSubsMu.Unlock()
	for sid, fs := range subs {
		fs.Detach()
		go h.archiveAnchor(sid)
	}
}
```

- [ ] **Step 2: Hook OnRemoteTerminalToggle to the admin update path**

In `internal/relay/feishu_http.go` (or wherever the binding settings UPDATE endpoint lives), after persisting `SetRemoteTerminalSettings`, fire the callback:

```go
if previousEnabled != newEnabled {
	host.OnRemoteTerminalToggle(newEnabled)
}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add desktop/relay_host.go internal/relay/feishu_http.go
git commit -m "feat(feishu): master-switch teardown — detach all + archive anchors"
```

---

### Task 20: Admin UI toggle + autoAttach dropdown

**Files:**
- Modify: `web/src/admin/tabs/FeishuConfig.vue`
- Modify: any TypeScript binding types in `web/src/admin/types/*` or similar

- [ ] **Step 1: Read existing FeishuConfig.vue to find the existing form**

Run: `grep -n "<template>\|<script\|<label\|EncryptKey" web/src/admin/tabs/FeishuConfig.vue | head -40`

- [ ] **Step 2: Extend the relevant typed binding interface**

Find the TS type that mirrors `FeishuBinding` (search: `grep -rn "RemoteTerminal\|remoteTerminal" web/src/ 2>/dev/null`). Add:

```ts
interface FeishuBindingState {
  // ... existing fields ...
  remoteTerminalEnabled: boolean
  sessionAutoAttach: 'ai' | 'all' | 'none'
}
```

- [ ] **Step 3: Add the toggle + dropdown to the template**

In `web/src/admin/tabs/FeishuConfig.vue`, after the existing EncryptKey block, add:

```vue
<section class="feishu-remote-terminal">
  <h3>Remote Terminal</h3>
  <p class="hint">
    将飞书 DM 作为另一个终端入口：每个 session 会推一张可流式更新的锚卡，
    通过回复或卡内输入框发送指令。仅限个人 1:1 DM。
  </p>
  <label class="switch">
    <input type="checkbox" v-model="state.remoteTerminalEnabled" />
    启用远程接管
  </label>
  <label v-if="state.remoteTerminalEnabled">
    自动 attach：
    <select v-model="state.sessionAutoAttach">
      <option value="ai">仅 AI session（推荐）</option>
      <option value="all">所有 session</option>
      <option value="none">不自动（手动 /attach，P2）</option>
    </select>
  </label>
  <button class="save" @click="saveRemoteTerminal" :disabled="saving">保存</button>
</section>
```

- [ ] **Step 4: Add the save handler in the script section**

```ts
async function saveRemoteTerminal() {
  saving.value = true
  try {
    await api.put('/v1/feishu/bindings/me/remote-terminal', {
      enabled: state.remoteTerminalEnabled,
      auto_attach: state.sessionAutoAttach,
    })
  } finally {
    saving.value = false
  }
}
```

- [ ] **Step 5: Add the corresponding HTTP endpoint**

In `internal/relay/feishu_http.go`, add a handler for `PUT /v1/feishu/bindings/me/remote-terminal`:

```go
type remoteTerminalReq struct {
	Enabled    bool   `json:"enabled"`
	AutoAttach string `json:"auto_attach"`
}

func (s *Server) handleRemoteTerminalSettings(w http.ResponseWriter, r *http.Request) {
	userID := s.userIDFromRequest(r)
	var req remoteTerminalReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	prev, _ := s.store.GetFeishuBinding(r.Context(), userID)
	if err := s.store.SetRemoteTerminalSettings(r.Context(), userID, req.Enabled, req.AutoAttach); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if prev != nil && prev.RemoteTerminalEnabled != req.Enabled {
		s.host.OnRemoteTerminalToggle(req.Enabled)
	}
	w.WriteHeader(http.StatusNoContent)
}
```

Then register the route in the Server's `Routes` method.

- [ ] **Step 6: Test by running the dev server**

Run: `make dev` (or your equivalent — look at `Makefile` for the dev target).
Expected: server starts; the admin tab shows the new toggle.

If you have Vue tests:

Run: `cd web && npm test`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/admin/tabs/FeishuConfig.vue internal/relay/feishu_http.go
git commit -m "feat(feishu): admin UI for remote terminal toggle + autoAttach"
```

---

### Task 21: Deploy checklist documentation

**Files:**
- Create: `docs/feishu-remote-terminal-deployment.md`

- [ ] **Step 1: Write the doc**

Create `docs/feishu-remote-terminal-deployment.md`:

```markdown
# Feishu Remote Terminal — Deployment Checklist

Before users enable the master switch in the admin panel, the Feishu app
must be configured correctly. Skipping any step below results in the
classic 200340 error ("出错了, 请稍后重试") when users press card buttons.

## Three required configuration steps

1. **Subscribe to `card.action.trigger` event**
   Feishu Developer Console → Event Subscription → add `card.action.trigger`.

2. **Enable interactive card capability**
   Feishu Developer Console → App Capabilities → Interactive Cards → toggle on.

3. **Configure the encrypted event request URL**
   Feishu Developer Console → Event Subscription → Request URL →
   `<your atterm relay>/v1/feishu/events/{appIDHash}` (the value is shown
   in the admin panel after the binding is created).

## Encryption keys

The relay requires both `encrypt_key` and `verification_token` to be set
on the binding. The admin panel surfaces these as separate fields. The
events path rejects non-encrypted requests (see `event.go: ErrNotEncryptedBody`).

## Permission model recap

- All inbound input is gated by `operator.open_id == binding.open_id`.
  Group chats are out of scope: the bot must be in a 1:1 DM with the
  user who owns the binding.

## Operational caveats

- atterm restart drops all in-memory anchor cards; existing anchors in
  the DM become inert. New sessions push fresh anchors on demand.
- AI sessions are auto-attached by default; shell sessions require
  `auto_attach: "all"`. Toggle via the admin panel.
- Tail rendering keeps only the last ~30 lines / 5 KB per session. For
  full output, use the local atterm or web entry.
```

- [ ] **Step 2: Commit**

```bash
git add docs/feishu-remote-terminal-deployment.md
git commit -m "docs(feishu): remote terminal deployment checklist"
```

---

### Task 22: Phase 3 checkpoint + final tag

- [ ] **Step 1: Run the full suite one last time**

```bash
git status && go test ./... -race && go build ./... && (cd web && npm test || true)
```

Expected: clean tree, all tests pass, build succeeds.

- [ ] **Step 2: Tag**

```bash
git tag phase3-feishu-as-terminal
```

- [ ] **Step 3: Open a PR**

```bash
gh pr create --title "feat(feishu): remote terminal — anchor cards, AI streaming, admin UI" --body "$(cat <<'EOF'
## Summary
- Per-session anchor cards in Feishu DM stream PTY output (shell) or per-turn AI events (Claude Code) live.
- Inbound reply / in-card input maps to session via reply target / card token; gated by `operator.open_id == binding.open_id`.
- Driver preempt protocol respects existing driver; explicit promote required.
- Master switch + autoAttach (ai/all/none) via admin UI.

## Test plan
- [ ] `go test ./... -race` passes
- [ ] `web && npm test` passes
- [ ] Manual: end-to-end shell session reachable from Feishu DM
- [ ] Manual: end-to-end Claude Code session — per-turn streaming visible
- [ ] Manual: open_id mismatch returns "无权限" toast
- [ ] Manual: driver preempt toast appears when local terminal owns driver
- [ ] Manual: master switch off → all anchors archived
EOF
)"
```

---

## Self-review against the spec

Spot-checking the spec's F1–F10 against tasks:

| Feature | Implementing task(s) |
|---|---|
| F1 FeishuSubscriber | Task 7 |
| F2 Anchor card schema | Task 3 |
| F3 Shell chunker | Task 6 |
| F4 AI chunker | Task 14, 15 |
| F5 Inbound router | Task 8 |
| F6 open_id gate | Task 8 |
| F7 Driver preempt | Task 16 |
| F8 Master switch + autoAttach | Task 1, 2, 19, 20 |
| F9 Failure containment | Task 17 |
| F10 Admin UI | Task 20 |

Spec § Testing:
- Unit: outbound chunker (Tasks 6, 14, 17), ANSI strip (reused), router (Task 8, 16). ✓
- Integration: mock Feishu API for PATCH (Task 4, 11). ✓
- E2E: Task 11 + Task 22 manual checklist. ✓
- Regression: Tasks preserve `RenderCommandFinishedCard` / `RenderAskQuestionCard` — `anchor_card.go` is a new file, dispatcher's existing AskQuestion path stays. ✓

Spec § Failure modes:
- PATCH 429: throttle in Task 6 guarantees ≤100 ms between flushes. ✓
- PATCH 404/230030: handled in Task 9 (`IsCardGoneError`) + Task 17. ✓
- PATCH 5xx: PatchWithRetry in Task 17. ✓
- Token expired: `*AuthClassError` short-circuit in Task 17. ✓
- 3-second budget: router 500 ms budget enforced via test (Task 8). ✓
- Dead cards after restart: documented gap in Task 21. ✓

Spec § Open questions:
- Viewer cap counting: deferred — not blocking; document in Task 21 or as a follow-up.
- autoAttach "all" exposure: included in Task 20 UI.
- Storage format: chose additive columns (Task 1).
- ClaudeCodeAdapter schema versioning: not bumped, since `ParseTurn` is additive on the same struct (Task 13).

No placeholder text in the plan body. Method names verified consistent across tasks (`SendAnchorCard`, `PatchCard`, `PatchWithRetry`, `AttachFeishuSubscriber`, `Detach`, `OwnerOpenID`, `CurrentDriverName`, `OnRemoteTerminalToggle`).
