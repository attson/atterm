# Relay Outbound Webhook on Command-Finish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a command finishes on a session, the relay POSTs to each of the session owner's configured webhooks (Feishu custom-bot or generic JSON), alongside the existing Web Push dispatch.

**Architecture:** New `internal/webhook/` package mirrors `internal/webpush/`; per-user webhook rows in `userstore` mirror `apitokens.go`; `/api/me/webhooks` CRUD mirrors `/api/me/tokens`; dispatch is added at the existing command-finish site in `uplink_conn.go`. Browser-side management via a `web/` settings "Webhooks" tab. Independent of the mobile platform stack — branches off `main`.

**Tech Stack:** Go 1.23 (`net/http`, `encoding/json`, `database/sql` SQLite) + Vue 3/Vitest for the web tab.

**Spec:** `docs/superpowers/specs/2026-05-24-relay-webhook-notifications-design.md`
**Branch:** `relay-webhook` (off `main`).

---

## Grounded facts (verified against current code)

- **userstore is SQLite-only.** `internal/userstore/store.go` defines `Store` (interface) + `SQLiteStore`. `store_iface_test.go` is just `var _ Store = (*SQLiteStore)(nil)` — a compile-time assertion. **There is NO separate memory impl to update** (the spec mis-stated this). Adding methods to `Store` requires implementing them on `SQLiteStore`; the compile assertion then covers the contract.
- Migrations: numbered SQL files in `internal/userstore/migrations/` (`0001_init.sql`, `0002_admin_role.sql`), embedded via `//go:embed migrations/*.sql`, applied in sorted order, tracked in `schema_migrations`. New file: `0003_webhooks.sql`.
- `api_tokens` DDL (the row model to mirror) uses `id TEXT PK`, `user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE`, `created_at INTEGER`. IDs come from `defaultIDs.New()`.
- Token CRUD lives on `*SQLiteStore` (`CreateAPIToken`/`ListAPITokens`/`RevokeAPIToken`), errors `ErrTokenNotOwnedOrMissing` for 404-on-delete.
- HTTP handlers live on `*AuthServer` in `internal/relay/auth_http.go`: `a.requireUser(w,r) (p, ok)`, `writeError(w, status, code)`, `writeJSONStatus(w, status, obj)`, `a.Store`. Routes registered in the same file's route block; CSRF via `RequireCSRF(a.Resolver, http.HandlerFunc(...))`. Path param via `r.PathValue("id")`.
- `webpush.Open(dir, vapidSubject)` is constructed in `cmd/atterm-relay/main.go` (~line 130) and assigned `cfg.WebPush = wpSvc`. The dispatch fires at `internal/relay/uplink_conn.go` (~line 368): `s.cfg.WebPush.DispatchCommandFinished(ms.sess.OwnerUserID, webpush.CommandFinished{SessionID, HostID, ExitCode, ElapsedMS, Label})`.
- `internal/relay/server.go` `Config` has `WebPush *webpush.Service` (nil-safe); add `Webhook *webhook.Service` beside it.
- Web settings tabs live in `web/src/settings/tabs/*.vue`; API wrappers in `web/src/shared/api/*.ts` using `apiFetch`; `Notifications.vue` (web-push) + `ApiTokens.vue` are the closest mirrors. Tabs are registered in `web/src/settings/App.vue`.

---

## File Structure

**New (Go):**
- `internal/userstore/migrations/0003_webhooks.sql`
- `internal/userstore/webhooks.go` (+ `webhooks_test.go`)
- `internal/webhook/render.go` (+ `render_test.go`)
- `internal/webhook/transport.go` (+ `transport_test.go`)
- `internal/webhook/dispatch.go`, `internal/webhook/service.go` (+ `dispatch_test.go`)

**Modified (Go):**
- `internal/userstore/store.go` — add 3 methods to the `Store` interface.
- `internal/relay/server.go` — `Config.Webhook *webhook.Service`.
- `internal/relay/auth_http.go` — `/api/me/webhooks` routes + handlers.
- `internal/relay/uplink_conn.go` — parallel webhook dispatch.
- `cmd/atterm-relay/main.go` — construct + wire `webhook.New(store)`.

**New (web):** `web/src/shared/api/webhooks.ts` (+ test), `web/src/settings/tabs/Webhooks.vue` (+ test).
**Modified (web):** `web/src/settings/App.vue` (register tab), `web/src/shared/api/types.ts` (Webhook types).
**Docs:** `AGENTS.md` "何时改哪里" row.

---

### Task 1: userstore webhooks table + CRUD + Store interface

**Files:**
- Create: `internal/userstore/migrations/0003_webhooks.sql`
- Create: `internal/userstore/webhooks.go`
- Create: `internal/userstore/webhooks_test.go`
- Modify: `internal/userstore/store.go`

- [ ] **Step 1: Write the migration**

`internal/userstore/migrations/0003_webhooks.sql`:

```sql
CREATE TABLE webhooks (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    url            TEXT NOT NULL,
    format         TEXT NOT NULL,
    allow_insecure INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL
);
CREATE INDEX idx_webhooks_user ON webhooks(user_id);
```

- [ ] **Step 2: Write the failing test**

`internal/userstore/webhooks_test.go`:

```go
package userstore

import (
	"context"
	"testing"
)

func TestWebhookCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t) // existing test helper used by apitokens_test.go
	uid := createTestUser(t, s, "wh@example.com")

	wh, err := s.CreateWebhook(ctx, uid, "https://open.feishu.cn/x", "feishu", "phone", false)
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if wh.ID == "" || wh.UserID != uid || wh.URL != "https://open.feishu.cn/x" || wh.Format != "feishu" || wh.Name != "phone" {
		t.Fatalf("unexpected webhook row: %+v", wh)
	}

	list, err := s.ListWebhooks(ctx, uid)
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(list) != 1 || list[0].ID != wh.ID {
		t.Fatalf("expected 1 webhook, got %+v", list)
	}

	// delete scoped to owner
	if err := s.DeleteWebhook(ctx, wh.ID, "other-user"); err != ErrWebhookNotOwnedOrMissing {
		t.Fatalf("cross-user delete should fail with ErrWebhookNotOwnedOrMissing, got %v", err)
	}
	if err := s.DeleteWebhook(ctx, wh.ID, uid); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	list, _ = s.ListWebhooks(ctx, uid)
	if len(list) != 0 {
		t.Fatalf("expected 0 webhooks after delete, got %d", len(list))
	}
}

func TestWebhookCascadeOnUserDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	uid := createTestUser(t, s, "wh2@example.com")
	if _, err := s.CreateWebhook(ctx, uid, "https://r/x", "generic", "n", false); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if err := s.DeleteUser(ctx, uid); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	list, err := s.ListWebhooks(ctx, uid)
	if err != nil {
		t.Fatalf("ListWebhooks after user delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected cascade-delete to remove webhooks, got %d", len(list))
	}
}
```

NOTE: confirm the exact names of the existing test helpers (`newTestStore`, `createTestUser`, `DeleteUser`) by reading `internal/userstore/apitokens_test.go` and `users_delete.go` first; adapt the test to whatever those files actually use (e.g. the helper may be `openTestStore(t)` and user creation may be `s.CreateUser(...)`). Match the existing convention exactly.

- [ ] **Step 3: Run to verify it fails**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/userstore/ -run TestWebhook 2>&1 | tail -15
```
Expected: FAIL — `CreateWebhook`/`ListWebhooks`/`DeleteWebhook`/`Webhook`/`ErrWebhookNotOwnedOrMissing` undefined.

- [ ] **Step 4: Implement `internal/userstore/webhooks.go`**

```go
package userstore

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrWebhookNotOwnedOrMissing mirrors ErrTokenNotOwnedOrMissing: returned by
// DeleteWebhook when the id is unknown or owned by another user. Callers map
// it to HTTP 404 to avoid an existence oracle.
var ErrWebhookNotOwnedOrMissing = errors.New("userstore: webhook not found or not owned by the requesting user")

// Webhook is a per-user outbound webhook configuration. The URL is stored and
// returned to its owner verbatim (Feishu URLs embed a bot token).
type Webhook struct {
	ID            string
	UserID        string
	Name          string
	URL           string
	Format        string // "feishu" | "generic"
	AllowInsecure bool
	CreatedAt     time.Time
}

func (s *SQLiteStore) CreateWebhook(ctx context.Context, userID, url, format, name string, allowInsecure bool) (*Webhook, error) {
	id := defaultIDs.New()
	now := time.Now()
	insecure := 0
	if allowInsecure {
		insecure = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhooks(id, user_id, name, url, format, allow_insecure, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		id, userID, name, url, format, insecure, now.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert webhook: %w", err)
	}
	return &Webhook{ID: id, UserID: userID, Name: name, URL: url, Format: format, AllowInsecure: allowInsecure, CreatedAt: now}, nil
}

func (s *SQLiteStore) ListWebhooks(ctx context.Context, userID string) ([]Webhook, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, url, format, allow_insecure, created_at
		 FROM webhooks WHERE user_id = ? ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		var (
			id, uid, name, url, format string
			insecure                   int
			createdAt                  int64
		)
		if err := rows.Scan(&id, &uid, &name, &url, &format, &insecure, &createdAt); err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}
		out = append(out, Webhook{
			ID: id, UserID: uid, Name: name, URL: url, Format: format,
			AllowInsecure: insecure != 0, CreatedAt: time.Unix(createdAt, 0),
		})
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteWebhook(ctx context.Context, webhookID, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM webhooks WHERE id = ? AND user_id = ?`,
		webhookID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return ErrWebhookNotOwnedOrMissing
	}
	return nil
}
```

(Imports: `context`, `errors`, `fmt`, `time` only — `database/sql` is NOT needed here since no `sql.NullX` scans are used.)

- [ ] **Step 5: Add the three methods to the `Store` interface**

In `internal/userstore/store.go`, find the `type Store interface {` block (it lists `CreateAPIToken`, `ListAPITokens`, etc.) and add:

```go
	CreateWebhook(ctx context.Context, userID, url, format, name string, allowInsecure bool) (*Webhook, error)
	ListWebhooks(ctx context.Context, userID string) ([]Webhook, error)
	DeleteWebhook(ctx context.Context, webhookID, userID string) error
```

(`store_iface_test.go`'s compile assertion `var _ Store = (*SQLiteStore)(nil)` now requires these — already satisfied by Step 4.)

- [ ] **Step 6: Run to verify it passes**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/userstore/ -run TestWebhook 2>&1 | tail -10
go build ./... 2>&1 | tail -5
```
Expected: webhook tests PASS; build succeeds (interface satisfied).

- [ ] **Step 7: Commit**

```bash
git add internal/userstore/migrations/0003_webhooks.sql internal/userstore/webhooks.go internal/userstore/webhooks_test.go internal/userstore/store.go
git commit -m "feat(userstore): per-user webhooks table + CRUD"
```

---

### Task 2: webhook render (Feishu + generic)

**Files:**
- Create: `internal/webhook/render.go`
- Create: `internal/webhook/render_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/webhook/render_test.go
package webhook

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func sampleEvent() CommandFinished {
	return CommandFinished{
		SessionID: uuid.MustParse("00000000-0000-0000-0000-0000000000a1"),
		HostID:    uuid.MustParse("00000000-0000-0000-0000-0000000000b2"),
		ExitCode:  0,
		ElapsedMS: 2300,
		Label:     "npm test",
	}
}

func TestRenderFeishu(t *testing.T) {
	body := renderFeishu(sampleEvent())
	var parsed struct {
		MsgType string `json:"msg_type"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("feishu body not valid json: %v", err)
	}
	if parsed.MsgType != "text" {
		t.Fatalf("msg_type = %q, want text", parsed.MsgType)
	}
	if !strings.Contains(parsed.Content.Text, "npm test") || !strings.Contains(parsed.Content.Text, "exit 0") {
		t.Fatalf("feishu text missing label/exit: %q", parsed.Content.Text)
	}
	if !strings.Contains(parsed.Content.Text, "2.3s") {
		t.Fatalf("feishu text missing formatted elapsed: %q", parsed.Content.Text)
	}
}

func TestRenderGeneric(t *testing.T) {
	body := renderGeneric(sampleEvent())
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("generic body not valid json: %v", err)
	}
	if parsed["label"] != "npm test" || parsed["exit_code"].(float64) != 0 || parsed["elapsed_ms"].(float64) != 2300 {
		t.Fatalf("generic payload wrong: %+v", parsed)
	}
	if parsed["session_id"] == "" || parsed["host_id"] == "" {
		t.Fatalf("generic payload missing ids: %+v", parsed)
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := map[int]string{500: "0.5s", 2300: "2.3s", 64000: "1m04s", 3600000: "60m00s"}
	for ms, want := range cases {
		if got := formatElapsed(ms); got != want {
			t.Errorf("formatElapsed(%d) = %q, want %q", ms, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/webhook/ -run 'TestRender|TestFormatElapsed' 2>&1 | tail -10
```
Expected: FAIL — package/functions undefined.

- [ ] **Step 3: Implement `internal/webhook/render.go`**

```go
package webhook

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// CommandFinished is the event shape, identical to webpush.CommandFinished so
// the dispatch site can construct both from the same decoded uplink payload.
type CommandFinished struct {
	SessionID uuid.UUID
	HostID    uuid.UUID
	ExitCode  int
	ElapsedMS int
	Label     string
}

// formatElapsed renders milliseconds as "2.3s" (<60s) or "1m04s" (>=60s),
// matching the desktop frontend's command-finish formatting.
func formatElapsed(ms int) string {
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	totalSec := ms / 1000
	return fmt.Sprintf("%dm%02ds", totalSec/60, totalSec%60)
}

func humanText(ev CommandFinished) string {
	label := ev.Label
	if label == "" {
		label = "command"
	}
	return fmt.Sprintf("%s finished (exit %d, %s)", label, ev.ExitCode, formatElapsed(ev.ElapsedMS))
}

func renderFeishu(ev CommandFinished) []byte {
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": humanText(ev)},
	}
	b, _ := json.Marshal(payload)
	return b
}

func renderGeneric(ev CommandFinished) []byte {
	payload := map[string]any{
		"event":      "command_finished",
		"session_id": ev.SessionID.String(),
		"host_id":    ev.HostID.String(),
		"exit_code":  ev.ExitCode,
		"elapsed_ms": ev.ElapsedMS,
		"label":      ev.Label,
		"text":       humanText(ev),
	}
	b, _ := json.Marshal(payload)
	return b
}

// renderForFormat selects the renderer; unknown formats fall back to generic.
func renderForFormat(format string, ev CommandFinished) []byte {
	if format == "feishu" {
		return renderFeishu(ev)
	}
	return renderGeneric(ev)
}
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/webhook/ -run 'TestRender|TestFormatElapsed' 2>&1 | tail -8
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/render.go internal/webhook/render_test.go
git commit -m "feat(webhook): Feishu + generic render with formatElapsed"
```

---

### Task 3: webhook transport (POST with timeout)

**Files:**
- Create: `internal/webhook/transport.go`
- Create: `internal/webhook/transport_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/webhook/transport_test.go
package webhook

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPostSendsJSONBody(t *testing.T) {
	var gotBody []byte
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = buf
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newClient(5 * time.Second)
	if err := post(c, srv.URL, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("post: %v", err)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if string(gotBody) != `{"a":1}` {
		t.Errorf("body = %q", gotBody)
	}
}

func TestPostNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	if err := post(newClient(5*time.Second), srv.URL, []byte(`{}`)); err == nil {
		t.Fatal("expected error on 500")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/webhook/ -run TestPost 2>&1 | tail -8
```
Expected: FAIL — `newClient`/`post` undefined.

- [ ] **Step 3: Implement `internal/webhook/transport.go`**

```go
package webhook

import (
	"bytes"
	"fmt"
	"net/http"
	"time"
)

func newClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// post sends body as application/json to url. Returns an error on transport
// failure or non-2xx status. The response body is intentionally discarded
// (never surfaced to the user — avoids a blind-SSRF data oracle).
func post(c *http.Client, url string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/webhook/ -run TestPost 2>&1 | tail -8
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/transport.go internal/webhook/transport_test.go
git commit -m "feat(webhook): http transport with timeout + non-2xx error"
```

---

### Task 4: webhook Service + DispatchCommandFinished

**Files:**
- Create: `internal/webhook/service.go`
- Create: `internal/webhook/dispatch.go`
- Create: `internal/webhook/dispatch_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/webhook/dispatch_test.go
package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeStore struct {
	hooks []Webhook
	err   error
}

func (f *fakeStore) ListWebhooks(_ context.Context, _ string) ([]Webhook, error) {
	return f.hooks, f.err
}

func TestDispatchFansOutToAllWebhooks(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	mk := func(tag string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock(); hits[tag]++; mu.Unlock()
			w.WriteHeader(200)
		}))
	}
	s1 := mk("a"); defer s1.Close()
	s2 := mk("b"); defer s2.Close()

	store := &fakeStore{hooks: []Webhook{
		{ID: "1", URL: s1.URL, Format: "feishu"},
		{ID: "2", URL: s2.URL, Format: "generic"},
	}}
	svc := New(store)
	svc.DispatchCommandFinished("u1", CommandFinished{SessionID: uuid.New(), HostID: uuid.New(), Label: "x"})

	// dispatch is async; wait briefly for both POSTs
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock(); done := hits["a"] == 1 && hits["b"] == 1; mu.Unlock()
		if done { return }
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected both webhooks hit once, got %v", hits)
}

func TestDispatchReturnsImmediately(t *testing.T) {
	store := &fakeStore{hooks: []Webhook{{ID: "1", URL: "http://127.0.0.1:1/slow", Format: "generic"}}}
	svc := New(store)
	start := time.Now()
	svc.DispatchCommandFinished("u1", CommandFinished{SessionID: uuid.New()})
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("DispatchCommandFinished should return immediately (fan-out is async)")
	}
}

func TestDispatchStoreErrorIsNoOp(t *testing.T) {
	store := &fakeStore{err: context.DeadlineExceeded}
	svc := New(store)
	// must not panic
	svc.DispatchCommandFinished("u1", CommandFinished{SessionID: uuid.New()})
}
```

NOTE: `Webhook` is referenced from this package (`webhook.Webhook`), but the row type lives in `userstore`. To avoid an import cycle (relay imports both; webhook should NOT import userstore), define the webhook-package's own minimal `Webhook` value type in `service.go` (fields `ID, URL, Format string`) and have the relay map `userstore.Webhook` → `webhook.Webhook` at the dispatch boundary. The `WebhookStore` interface returns `[]Webhook` (the webhook-package type). Adjust the fakeStore + the relay wiring accordingly.

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/webhook/ -run TestDispatch 2>&1 | tail -10
```
Expected: FAIL — `New`/`Service`/`WebhookStore`/`Webhook` undefined.

- [ ] **Step 3: Implement `internal/webhook/service.go` + `dispatch.go`**

`service.go`:

```go
package webhook

import (
	"context"
	"net/http"
	"time"
)

// Webhook is the minimal config the dispatcher needs. The relay maps
// userstore.Webhook into this type at the dispatch boundary so this package
// never imports userstore (avoids an import cycle).
type Webhook struct {
	ID     string
	URL    string
	Format string
}

// WebhookStore is the slice of persistence the Service depends on.
type WebhookStore interface {
	ListWebhooks(ctx context.Context, userID string) ([]Webhook, error)
}

type Service struct {
	store  WebhookStore
	client *http.Client
}

// New constructs a Service with an 8s per-POST timeout.
func New(store WebhookStore) *Service {
	return &Service{store: store, client: newClient(8 * time.Second)}
}
```

`dispatch.go`:

```go
package webhook

import (
	"context"
	"log"
)

// DispatchCommandFinished fans the event out to all of the owner's webhooks.
// Returns immediately; each POST runs in its own goroutine. Store errors and
// non-2xx POSTs are logged and otherwise ignored (best-effort, like web-push).
func (s *Service) DispatchCommandFinished(ownerUserID string, ev CommandFinished) {
	hooks, err := s.store.ListWebhooks(context.Background(), ownerUserID)
	if err != nil {
		log.Printf("webhook: list for user=%s failed: %v", ownerUserID, err)
		return
	}
	for _, h := range hooks {
		h := h
		body := renderForFormat(h.Format, ev)
		go func() {
			if err := post(s.client, h.URL, body); err != nil {
				log.Printf("webhook: POST id=%s failed: %v", h.ID, err)
			}
		}()
	}
}
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/webhook/ 2>&1 | tail -10
go vet ./internal/webhook/ 2>&1 | tail -5
```
Expected: all webhook tests PASS; vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/service.go internal/webhook/dispatch.go internal/webhook/dispatch_test.go
git commit -m "feat(webhook): Service + async DispatchCommandFinished fan-out"
```

---

### Task 5: /api/me/webhooks HTTP handlers

**Files:**
- Modify: `internal/relay/auth_http.go`
- Create: `internal/relay/webhooks_http_test.go`

- [ ] **Step 1: Read the existing token routes + handlers**

```bash
sed -n '65,75p;330,440p' internal/relay/auth_http.go
```
Confirm: route registration block, `requireUser`, `writeError`, `writeJSONStatus`, `RequireCSRF`, `r.PathValue`.

- [ ] **Step 2: Write the failing test**

`internal/relay/webhooks_http_test.go` — mirror the token HTTP test (`me_http_test.go`) helpers (`newAuthTestServer`, cookie + CSRF helpers). Read `me_http_test.go` first to reuse its exact harness, then:

```go
package relay

import (
	"net/http"
	"strings"
	"testing"
)

// TestCreateAndListWebhook: POST /api/me/webhooks (CSRF) → 201; GET lists it.
func TestCreateAndListWebhook(t *testing.T) {
	h, cookie, csrf := newAuthedUser(t) // adapt to me_http_test.go's helper names

	w := postJSONWithCSRF(h, "/api/me/webhooks",
		map[string]any{"url": "https://open.feishu.cn/x", "format": "feishu", "name": "phone", "allow_insecure": false},
		cookie, csrf)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status %d body %s", w.Code, w.Body.String())
	}

	list := getWithCookie(h, "/api/me/webhooks", cookie)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "open.feishu.cn") {
		t.Fatalf("list: status %d body %s", list.Code, list.Body.String())
	}
}

func TestCreateWebhookRejectsBadFormat(t *testing.T) {
	h, cookie, csrf := newAuthedUser(t)
	w := postJSONWithCSRF(h, "/api/me/webhooks",
		map[string]any{"url": "https://r/x", "format": "slack", "name": "n"}, cookie, csrf)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad format, got %d", w.Code)
	}
}

func TestCreateWebhookRejectsInsecureWithoutFlag(t *testing.T) {
	h, cookie, csrf := newAuthedUser(t)
	w := postJSONWithCSRF(h, "/api/me/webhooks",
		map[string]any{"url": "http://r.example.com/x", "format": "generic", "name": "n", "allow_insecure": false}, cookie, csrf)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for http without allow_insecure, got %d", w.Code)
	}
}

func TestDeleteWebhook(t *testing.T) {
	h, cookie, csrf := newAuthedUser(t)
	c := postJSONWithCSRF(h, "/api/me/webhooks",
		map[string]any{"url": "https://r/x", "format": "generic", "name": "n"}, cookie, csrf)
	id := extractJSONField(t, c.Body.Bytes(), "id")
	d := deleteWithCSRF(h, "/api/me/webhooks/"+id, cookie, csrf)
	if d.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d", d.Code)
	}
}
```

NOTE: the helper names above (`newAuthedUser`, `postJSONWithCSRF`, `getWithCookie`, `deleteWithCSRF`, `extractJSONField`) are placeholders for whatever `me_http_test.go` actually provides — READ that file and use its real helpers. Do not invent helpers.

- [ ] **Step 3: Run to verify it fails**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/relay/ -run TestWebhook -run 'Webhook' 2>&1 | tail -12
```
Expected: FAIL — routes 404 (handlers not registered).

- [ ] **Step 4: Add routes + handlers in `auth_http.go`**

In the route registration block (next to the `/api/me/tokens` routes), add:

```go
	mux.Handle("GET /api/me/webhooks", http.HandlerFunc(a.handleListWebhooks))
	mux.Handle("POST /api/me/webhooks", RequireCSRF(a.Resolver, http.HandlerFunc(a.handleCreateWebhook)))
	mux.Handle("DELETE /api/me/webhooks/{id}", RequireCSRF(a.Resolver, http.HandlerFunc(a.handleDeleteWebhook)))
```

Add the handlers (mirror `handleListTokens`/`handleCreateToken`/`handleRevokeToken`):

```go
// handleListWebhooks implements GET /api/me/webhooks.
func (a *AuthServer) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	hooks, err := a.Store.ListWebhooks(r.Context(), p.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	type row struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		Format    string `json:"format"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]row, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, row{ID: h.ID, Name: h.Name, URL: h.URL, Format: h.Format, CreatedAt: h.CreatedAt.UTC().Format(time.RFC3339)})
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// handleCreateWebhook implements POST /api/me/webhooks (CSRF-gated).
func (a *AuthServer) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var body struct {
		URL           string `json:"url"`
		Format        string `json:"format"`
		Name          string `json:"name"`
		AllowInsecure bool   `json:"allow_insecure"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if body.Format != "feishu" && body.Format != "generic" {
		writeError(w, http.StatusBadRequest, "invalid_format")
		return
	}
	u, err := url.Parse(strings.TrimSpace(body.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		writeError(w, http.StatusBadRequest, "invalid_url")
		return
	}
	if u.Scheme == "http" && !body.AllowInsecure {
		writeError(w, http.StatusBadRequest, "insecure_url")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "name_required")
		return
	}
	wh, err := a.Store.CreateWebhook(r.Context(), p.UserID, body.URL, body.Format, body.Name, body.AllowInsecure)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{
		"id": wh.ID, "url": wh.URL, "format": wh.Format, "name": wh.Name,
		"created_at": wh.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// handleDeleteWebhook implements DELETE /api/me/webhooks/{id} (CSRF-gated).
func (a *AuthServer) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := a.Store.DeleteWebhook(r.Context(), id, p.UserID); err != nil {
		if errors.Is(err, userstore.ErrWebhookNotOwnedOrMissing) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

Ensure `net/url` is imported in `auth_http.go` (add `"net/url"` to the import block if absent).

- [ ] **Step 5: Run to verify it passes**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/relay/ -run 'Webhook' 2>&1 | tail -10
```
Expected: PASS — create/list/delete + validation.

- [ ] **Step 6: Commit**

```bash
git add internal/relay/auth_http.go internal/relay/webhooks_http_test.go
git commit -m "feat(relay): /api/me/webhooks CRUD (CSRF, https-or-insecure-flag)"
```

---

### Task 6: dispatch wiring + Config.Webhook

**Files:**
- Modify: `internal/relay/server.go`
- Modify: `internal/relay/uplink_conn.go`
- Create/extend: `internal/relay/uplink_webhook_test.go` (or extend the existing uplink test)

- [ ] **Step 1: Add `Webhook` to `Config`**

In `internal/relay/server.go`, beside `WebPush *webpush.Service`, add:

```go
	// Webhook, when non-nil, fires per-user outbound webhooks on command-finish.
	Webhook *webhook.Service
```

Add the import `"github.com/attson/atterm/internal/webhook"` (match the module path used by the existing `webpush` import — check the import block).

- [ ] **Step 2: Wire the dispatch**

In `internal/relay/uplink_conn.go`, right after the existing `s.cfg.WebPush.DispatchCommandFinished(...)` call, add:

```go
	if s.cfg.Webhook != nil {
		s.cfg.Webhook.DispatchCommandFinished(ms.sess.OwnerUserID, webhook.CommandFinished{
			SessionID: f.SessionID,
			HostID:    hostID,
			ExitCode:  payload.ExitCode,
			ElapsedMS: payload.ElapsedMS,
			Label:     payload.Label,
		})
	}
```

Add the `webhook` import to `uplink_conn.go`.

The relay must satisfy `webhook.WebhookStore` — but `userstore.Store.ListWebhooks` returns `[]userstore.Webhook`, while `webhook.New` wants something returning `[]webhook.Webhook`. Add a tiny adapter where the service is constructed (Task 7) that maps the types. (Keep the mapping in `cmd/atterm-relay/main.go` so neither `internal/webhook` nor `internal/relay` imports the other's row type.)

- [ ] **Step 3: Write/extend the dispatch test**

Add a test that constructs a `Server` with a `Webhook` service backed by a fake store + an `httptest` sink, drives a `command_event` frame through the uplink handler (reuse the existing uplink test's harness — read the current uplink test file first), and asserts the webhook endpoint received a POST. If the existing uplink test harness is heavy, a lighter alternative: a focused test asserting that given a non-nil `cfg.Webhook`, the command-event handler calls `DispatchCommandFinished` (e.g. via a spy `webhook.Service` constructed over a fake store + httptest sink). Match the existing test style in `internal/relay/uplink_conn_test.go`.

- [ ] **Step 4: Run to verify**

```bash
cd /Users/attson/code/github.com.attson/atterm
go build ./... 2>&1 | tail -5
go test ./internal/relay/ 2>&1 | tail -10
```
Expected: build ok; relay tests green (existing + new dispatch test).

- [ ] **Step 5: Commit**

```bash
git add internal/relay/server.go internal/relay/uplink_conn.go internal/relay/uplink_webhook_test.go
git commit -m "feat(relay): fire per-user webhooks on command-finish (beside web-push)"
```

---

### Task 7: wire webhook.Service in main.go

**Files:**
- Modify: `cmd/atterm-relay/main.go`

- [ ] **Step 1: Construct + wire the service with a store adapter**

After the `cfg.WebPush = wpSvc` block in `cmd/atterm-relay/main.go`, add:

```go
	cfg.Webhook = webhook.New(webhookStoreAdapter{store})
```

And define the adapter (in `main.go` or a small `webhook_adapter.go` in `package main`):

```go
// webhookStoreAdapter maps userstore.Webhook rows into the minimal
// webhook.Webhook type the dispatcher needs, so internal/webhook never
// imports userstore (no import cycle).
type webhookStoreAdapter struct{ s *userstore.SQLiteStore }

func (a webhookStoreAdapter) ListWebhooks(ctx context.Context, userID string) ([]webhook.Webhook, error) {
	rows, err := a.s.ListWebhooks(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]webhook.Webhook, 0, len(rows))
	for _, r := range rows {
		out = append(out, webhook.Webhook{ID: r.ID, URL: r.URL, Format: r.Format})
	}
	return out, nil
}
```

Add imports: `"context"`, `"github.com/attson/atterm/internal/webhook"`, and ensure `userstore` is imported (it is — `store` is built from it). Confirm `store` is a `*userstore.SQLiteStore`; if it's the `Store` interface type, change the adapter field to `userstore.Store` (which has `ListWebhooks` after Task 1).

- [ ] **Step 2: Build + smoke**

```bash
cd /Users/attson/code/github.com.attson/atterm
go build ./... 2>&1 | tail -5
go vet ./... 2>&1 | tail -8
```
Expected: build + vet clean.

- [ ] **Step 3: Commit**

```bash
git add cmd/atterm-relay/main.go
git commit -m "feat(relay): construct + wire webhook.Service in main"
```

---

### Task 8: web settings Webhooks tab

**Files:**
- Create: `web/src/shared/api/webhooks.ts`
- Modify: `web/src/shared/api/types.ts`
- Create: `web/src/settings/tabs/Webhooks.vue`
- Create: `web/tests/unit/settings/tabs/Webhooks.test.ts`
- Modify: `web/src/settings/App.vue`

- [ ] **Step 1: Add types + api wrapper**

In `web/src/shared/api/types.ts` add:

```ts
export interface WebhookRow {
  id: string
  name: string
  url: string
  format: 'feishu' | 'generic'
  created_at: string
}
```

Create `web/src/shared/api/webhooks.ts`:

```ts
import { apiFetch } from './client'
import type { WebhookRow } from './types'

export async function listWebhooks(): Promise<WebhookRow[]> {
  const { data } = await apiFetch<WebhookRow[]>('/api/me/webhooks')
  return data
}

export async function createWebhook(input: {
  url: string
  format: 'feishu' | 'generic'
  name: string
  allow_insecure: boolean
}): Promise<WebhookRow> {
  const { data } = await apiFetch<WebhookRow>('/api/me/webhooks', {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return data
}

export async function deleteWebhook(id: string): Promise<void> {
  await apiFetch(`/api/me/webhooks/${encodeURIComponent(id)}`, { method: 'DELETE' })
}
```

- [ ] **Step 2: Write the failing component test**

`web/tests/unit/settings/tabs/Webhooks.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/webhooks', () => ({
  listWebhooks: vi.fn().mockResolvedValue([
    { id: '1', name: 'phone', url: 'https://open.feishu.cn/x', format: 'feishu', created_at: '2026-05-24T00:00:00Z' },
  ]),
  createWebhook: vi.fn().mockResolvedValue({ id: '2', name: 'box', url: 'https://r/y', format: 'generic', created_at: '2026-05-24T00:00:00Z' }),
  deleteWebhook: vi.fn().mockResolvedValue(undefined),
}))

import Webhooks from '@/settings/tabs/Webhooks.vue'
import { listWebhooks, createWebhook, deleteWebhook } from '@shared/api/webhooks'

describe('Webhooks tab', () => {
  beforeEach(() => vi.clearAllMocks())

  it('lists existing webhooks on mount', async () => {
    const w = mount(Webhooks)
    await flushPromises()
    expect(listWebhooks).toHaveBeenCalled()
    expect(w.text()).toContain('phone')
    expect(w.text()).toContain('open.feishu.cn')
  })

  it('creates a webhook from the form', async () => {
    const w = mount(Webhooks)
    await flushPromises()
    await w.find('[data-testid="wh-name"]').setValue('box')
    await w.find('[data-testid="wh-url"]').setValue('https://r/y')
    await w.find('[data-testid="wh-add"]').trigger('click')
    await flushPromises()
    expect(createWebhook).toHaveBeenCalledWith(expect.objectContaining({ name: 'box', url: 'https://r/y' }))
  })

  it('deletes a webhook', async () => {
    const w = mount(Webhooks)
    await flushPromises()
    await w.find('[data-testid="wh-del-1"]').trigger('click')
    await flushPromises()
    expect(deleteWebhook).toHaveBeenCalledWith('1')
  })
})
```

- [ ] **Step 3: Run to verify it fails**

```bash
cd web && npx vitest run tests/unit/settings/tabs/Webhooks.test.ts
```
Expected: FAIL — `@/settings/tabs/Webhooks.vue` missing.

- [ ] **Step 4: Implement `Webhooks.vue`**

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { listWebhooks, createWebhook, deleteWebhook } from '@shared/api/webhooks'
import type { WebhookRow } from '@shared/api/types'

const rows = ref<WebhookRow[]>([])
const name = ref('')
const url = ref('')
const format = ref<'feishu' | 'generic'>('feishu')
const allowInsecure = ref(false)
const error = ref('')
const busy = ref(false)

async function refresh() {
  try { rows.value = await listWebhooks() } catch (e) { error.value = String(e) }
}
onMounted(refresh)

async function add() {
  error.value = ''
  if (!name.value.trim() || !url.value.trim()) { error.value = 'name and url are required'; return }
  busy.value = true
  try {
    await createWebhook({ name: name.value.trim(), url: url.value.trim(), format: format.value, allow_insecure: allowInsecure.value })
    name.value = ''; url.value = ''
    await refresh()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function remove(id: string) {
  await deleteWebhook(id)
  await refresh()
}
</script>

<template>
  <section class="webhooks">
    <h3>Webhooks</h3>
    <p class="hint">Fire a webhook (Feishu or generic JSON) when a command finishes. URLs must be https unless you allow insecure.</p>
    <ul class="list">
      <li v-for="r in rows" :key="r.id">
        <span class="nm">{{ r.name }}</span>
        <span class="fmt">{{ r.format }}</span>
        <span class="url">{{ r.url }}</span>
        <button :data-testid="`wh-del-${r.id}`" @click="remove(r.id)">delete</button>
      </li>
    </ul>
    <div class="form">
      <input data-testid="wh-name" v-model="name" placeholder="name" />
      <select data-testid="wh-format" v-model="format">
        <option value="feishu">Feishu</option>
        <option value="generic">Generic JSON</option>
      </select>
      <input data-testid="wh-url" v-model="url" placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/…" />
      <label><input data-testid="wh-insecure" type="checkbox" v-model="allowInsecure" /> allow insecure http</label>
      <button data-testid="wh-add" :disabled="busy" @click="add">Add webhook</button>
      <p v-if="error" class="error">{{ error }}</p>
    </div>
  </section>
</template>

<style scoped>
.webhooks { padding: 1rem; color: var(--fg); }
.hint { color: var(--fg-dim); font-size: 0.85rem; }
.list { list-style: none; padding: 0; }
.list li { display: flex; gap: 0.75rem; align-items: center; padding: 0.4rem 0; border-bottom: 1px solid var(--border); font-size: 0.85rem; }
.list .url { color: var(--fg-dim); flex: 1; overflow: hidden; text-overflow: ellipsis; font-family: ui-monospace, monospace; }
.form { display: flex; flex-direction: column; gap: 0.5rem; margin-top: 1rem; max-width: 520px; }
.form input[type="text"], .form input:not([type]), .form select { height: 34px; border-radius: 7px; border: 1px solid var(--border); background: var(--panel, #11182b); color: var(--fg); padding: 0 10px; }
.error { color: #f87171; font-size: 0.8rem; }
</style>
```

- [ ] **Step 5: Register the tab in `web/src/settings/App.vue`**

Read `web/src/settings/App.vue`, then add a "Webhooks" entry to its tab list + render `<Webhooks v-show="activeTab === 'webhooks'" />` (mirror how `Notifications`/`ApiTokens` tabs are wired). Import `Webhooks from './tabs/Webhooks.vue'`.

- [ ] **Step 6: Run to verify it passes**

```bash
cd web && npx vitest run tests/unit/settings/tabs/Webhooks.test.ts && npx vue-tsc --noEmit
```
Expected: 3 cases PASS; tsc clean.

- [ ] **Step 7: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/shared/api/webhooks.ts web/src/shared/api/types.ts web/src/settings/tabs/Webhooks.vue web/tests/unit/settings/tabs/Webhooks.test.ts web/src/settings/App.vue
git commit -m "feat(web): settings Webhooks tab (per-user CRUD)"
```

---

### Task 9: docs + final verification

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Add the routing-table row**

In `AGENTS.md` `## 何时改哪里` table, append:

```markdown
| 改 relay 出站 webhook（命令结束通知） | `internal/webhook/`（render/transport/dispatch/service）+ `internal/userstore/webhooks.go`（+ migration `0003_webhooks.sql` + Store 接口）+ `internal/relay/auth_http.go`（`/api/me/webhooks`）+ `internal/relay/uplink_conn.go`（命令结束分发点，紧挨 WebPush）+ `cmd/atterm-relay/main.go`（构造 service + store adapter）+ `web/src/settings/tabs/Webhooks.vue` |
```

- [ ] **Step 2: Commit docs**

```bash
git add AGENTS.md
git commit -m "docs: AGENTS.md routing for relay webhook subsystem"
```

- [ ] **Step 3: Final verification gate (no commit)**

```bash
cd /Users/attson/code/github.com.attson/atterm
go build ./... 2>&1 | tail -5
go vet ./... 2>&1 | tail -8
go test ./internal/webhook/ ./internal/userstore/ ./internal/relay/ 2>&1 | tail -20
cd web && npm test 2>&1 | tail -6 && npx vue-tsc --noEmit && echo "web ok"
```
Expected: build + vet clean; Go webhook/userstore/relay tests green; web suite green + tsc clean.

- [ ] **Step 4: Manual end-to-end smoke (owner runs)**

```bash
# terminal 1: a mock webhook sink
python3 -c "import http.server,sys; \
  h=type('H',(http.server.BaseHTTPRequestHandler,),{'do_POST':lambda s:(print(s.rfile.read(int(s.headers['content-length']))),s.send_response(200),s.end_headers())}); \
  http.server.HTTPServer(('127.0.0.1',9999),h).serve_forever()"
# terminal 2: relay (dev) + create a webhook via the web UI pointing at http://127.0.0.1:9999 (allow insecure),
# then run a command in a shell-integration-enabled session and watch terminal 1 print the POST body.
```

This is a verification gate; no commit.

---

## Self-Review Notes

- **Spec coverage:** package render/transport/dispatch/service (Tasks 2-4), userstore CRUD + migration + Store iface (Task 1), `/api/me/webhooks` (Task 5), dispatch wiring + Config.Webhook (Task 6), main wiring + store adapter (Task 7), web tab + api wrapper (Task 8), AGENTS.md (Task 9). All spec sections mapped.
- **Import-cycle guard:** `internal/webhook` defines its own minimal `Webhook` type and a `WebhookStore` interface; the `userstore.Webhook → webhook.Webhook` mapping lives in `cmd/atterm-relay` (the composition root), so neither internal package imports the other. Tasks 4, 6, 7 are consistent on this.
- **Spec correction:** userstore has no memory impl (SQLite-only + a compile-time iface assertion); Task 1 reflects that — no second impl to update.
- **Red-line 4:** no `internal/proto` change; `command_event` is consumed, not altered.
- **Red-line 9:** create-handler enforces https unless `allow_insecure`; response body never surfaced (transport discards it); CSRF on mutating routes; ownership-scoped delete.
- **Helper-name caveat:** Tasks 1 and 5 explicitly say to read `apitokens_test.go` / `me_http_test.go` first and reuse their real test helpers rather than the placeholder names shown — flagged inline so the implementer doesn't invent helpers.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-24-relay-webhook-notifications.md`. Two execution options:

1. **Subagent-Driven (recommended)** — fresh subagent per task, two-stage review.
2. **Inline Execution** — checkpoints in this session.

Which approach?
