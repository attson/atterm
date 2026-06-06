# Session Attention/Seen — Backend (relay) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the relay-side machinery for a per-user seen/unread inbox: a per-session `attention_at` signal, per-user `seen_at` storage, a `unread` flag computed per authenticated user in the session list, auto-mark-seen on attach, an explicit mark-seen endpoint, and notification suppression while a session is being watched.

**Architecture:** `attention_at` lives on `proto.SessionInfo`/`MetaPayload` and is bumped in `internal/session` on attention-worthy state transitions (waiting_input always; completed/failed only for non-shell sessions). `seen_at` is persisted per (user, session) in the SQLite `userstore`. The relay computes `unread = attention_at > 0 && attention_at > seen_at && subscriberCount == 0` only when building the per-user `TypeListResp` (the authoritative channel). The same `subscriberCount == 0` term gates Web Push dispatch. META carries `attention_at` only (an attached client is by definition watching ⇒ read).

**Tech Stack:** Go, `modernc.org/sqlite`, `nhooyr.io/websocket` (existing), standard `net/http` ServeMux with method patterns.

**Spec:** `docs/superpowers/specs/2026-06-06-session-attention-model-design.md`. Deviation: META does not carry `unread` (only `attention_at`); unread is authoritative via `TypeListResp`. Update the spec §3/§5 to match when this lands.

---

## File Structure

- `internal/proto/frame.go` — add `AttentionAt`/`Unread` to `SessionInfo`, `AttentionAt` to `MetaPayload`.
- `internal/session/session.go` — `SubscriberCount()` accessor; bump `AttentionAt` on transitions; `isAttentionType` helper; carry `AttentionAt` into `encodeMetaPayload`.
- `internal/userstore/migrations/0005_session_seen.sql` — new table.
- `internal/userstore/store.go` — `Store` interface + `SQLiteStore` methods `SetSeen`/`SeenAt`/`PruneSeenSession`.
- `internal/relay/client_sessions_conn.go` — fill `Unread` in the per-owner list.
- `internal/relay/client_conn.go` — auto-mark-seen on ATTACH.
- `internal/relay/server.go` — `removeSession` wrapper (prune) + register `POST /api/sessions/seen`.
- `internal/relay/{agent_conn,adopt,uplink_conn}.go` — route removals through `removeSession`.
- `internal/relay/sessions_seen_http.go` (new) — the endpoint handler.
- `internal/relay/uplink_conn.go` — suppress dispatch when `SubscriberCount() > 0`.

---

## Task 1: proto fields

**Files:**
- Modify: `internal/proto/frame.go` (`SessionInfo` struct ~198-228; `MetaPayload` struct ~88-117)
- Test: `internal/proto/frame_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/proto/frame_test.go`:

```go
func TestSessionInfoAttentionJSON(t *testing.T) {
	in := SessionInfo{ID: "s1", AttentionAt: 1700000000, Unread: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out SessionInfo
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.AttentionAt != 1700000000 || !out.Unread {
		t.Fatalf("round-trip lost fields: %+v", out)
	}
	// Zero values must be omitted.
	z, _ := json.Marshal(SessionInfo{ID: "s2"})
	if strings.Contains(string(z), "attention_at") || strings.Contains(string(z), "unread") {
		t.Fatalf("zero values not omitted: %s", z)
	}
}
```

Ensure `encoding/json` and `strings` are imported in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proto/ -run TestSessionInfoAttentionJSON -v`
Expected: FAIL — `in.AttentionAt`/`in.Unread` undefined (compile error).

- [ ] **Step 3: Add the fields**

In `internal/proto/frame.go`, in `SessionInfo`, after the `Summary *SessionSummary` field:

```go
	// AttentionAt is the unix time (seconds) the session last entered an
	// attention-worthy state (waiting_input, or a non-shell completed/failed).
	// Zero means nothing is pending. See spec §4.
	AttentionAt int64 `json:"attention_at,omitempty"`
	// Unread is computed per authenticated user by the relay when building the
	// session list: attention_at > seen_at AND no client is attached. Always
	// zero in session-local copies; the relay sets it. See spec §2.
	Unread bool `json:"unread,omitempty"`
```

In `MetaPayload`, after the `Summary *SessionSummary` field:

```go
	// AttentionAt mirrors SessionInfo.AttentionAt so attached clients learn
	// the latest attention timestamp in real time. Unread is intentionally
	// NOT carried in META: an attached client is watching ⇒ read.
	AttentionAt int64 `json:"attention_at,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/proto/ -run TestSessionInfoAttentionJSON -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/proto/frame.go internal/proto/frame_test.go
git commit -m "proto: add attention_at/unread to SessionInfo, attention_at to MetaPayload"
```

---

## Task 2: `Session.SubscriberCount()` accessor

**Files:**
- Modify: `internal/session/session.go` (near `SetSubscriberCountHook`, ~144)
- Test: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestSubscriberCount(t *testing.T) {
	s := New(uuid.New(), proto.OpenPayload{Cols: 80, Rows: 24})
	if got := s.SubscriberCount(); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
	sub, _ := s.Subscribe(0, "c1", "dev1")
	if got := s.SubscriberCount(); got != 1 {
		t.Fatalf("want 1, got %d", got)
	}
	s.Unsubscribe(sub)
	if got := s.SubscriberCount(); got != 0 {
		t.Fatalf("want 0 after unsubscribe, got %d", got)
	}
}
```

> NOTE: match the real `New(...)` / `Subscribe(...)` / `Unsubscribe(...)` signatures used elsewhere in `session_test.go`. If the constructor differs, copy the construction idiom from an existing test in this file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestSubscriberCount -v`
Expected: FAIL — `s.SubscriberCount` undefined.

- [ ] **Step 3: Add the accessor**

In `internal/session/session.go`, after `SetSubscriberCountHook`:

```go
// SubscriberCount returns the number of currently-attached subscribers.
func (s *Session) SubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subs)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/ -run TestSubscriberCount -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "session: add SubscriberCount() accessor"
```

---

## Task 3: bump `attention_at` on attention-worthy transitions

**Files:**
- Modify: `internal/session/session.go` (`updateTerminalState` waiting_input branch ~571; `applyOSC133Locked` case `'D'` ~660-685; `encodeMetaPayload` ~494)
- Test: `internal/session/attention_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/session/attention_test.go`:

```go
package session

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/attson/atterm/internal/proto"
)

func osc(s string) []byte { return []byte("\x1b]133;" + s + "\x07") }

func TestAttentionAt_WaitingInputAlwaysBumps(t *testing.T) {
	s := New(uuid.New(), proto.OpenPayload{Cols: 80, Rows: 24})
	s.updateTerminalState([]byte("Continue? [y/N] "))
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("expected waiting_input, got %q", s.Info().TaskState)
	}
	if s.Info().AttentionAt == 0 {
		t.Fatalf("waiting_input must bump attention_at")
	}
}

func TestAttentionAt_NonShellCompletionBumps(t *testing.T) {
	s := New(uuid.New(), proto.OpenPayload{Cols: 80, Rows: 24})
	s.updateTerminalState(osc("C;claude")) // classifies type=ai, state=running
	if s.Info().Type != SessionTypeAI {
		t.Fatalf("expected ai, got %q", s.Info().Type)
	}
	s.updateTerminalState(osc("D;0")) // completed
	if s.Info().AttentionAt == 0 {
		t.Fatalf("non-shell completion must bump attention_at")
	}
}

func TestAttentionAt_ShellCompletionDoesNotBump(t *testing.T) {
	s := New(uuid.New(), proto.OpenPayload{Cols: 80, Rows: 24})
	s.updateTerminalState(osc("C;ls")) // shell, type stays ""
	s.updateTerminalState(osc("D;0"))  // completed shell
	if s.Info().AttentionAt != 0 {
		t.Fatalf("shell completion must NOT bump attention_at, got %d", s.Info().AttentionAt)
	}
	_ = time.Now
}
```

> NOTE: confirm the OSC 133 wire format the parser expects by reading `consumeOSC133Locked`/`parseOSC133Exit` — adjust the `osc()` helper if the terminator or prefix differs. Confirm `New(...)` signature against existing tests.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestAttentionAt -v`
Expected: FAIL — `AttentionAt` stays 0 (no bump logic yet).

- [ ] **Step 3: Add the helper + bump logic**

In `internal/session/session.go`, add near `ClassifyCommand` usage (top-level func):

```go
// isAttentionType reports whether a session whose workload Type is t should
// generate an inbox entry when it finishes. Empty Type means shell.
func isAttentionType(t string) bool {
	return t != "" && t != SessionTypeShell
}
```

In `updateTerminalState`, the existing waiting_input branch:

```go
	} else if s.meta.TaskState != proto.TaskStateRunning && looksLikeWaitingInput(data) && s.meta.TaskState != proto.TaskStateWaitingInput {
		s.meta.TaskState = proto.TaskStateWaitingInput
		s.meta.AttentionAt = now.Unix() // ADD
		changed = true
	}
```

In `applyOSC133Locked`, case `'D'`, after the `CommandExitCode` block and before/after the `Summary` assignment:

```go
		s.meta.Summary = computeSummary(s.scroll, now, exitCode != 0)
		if isAttentionType(s.meta.Type) { // ADD
			s.meta.AttentionAt = now.Unix()
		}
		changed = true
```

In `encodeMetaPayload`, add `AttentionAt: meta.AttentionAt,` to the `proto.MetaPayload{...}` literal.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/ -run TestAttentionAt -v`
Expected: PASS

- [ ] **Step 5: Run the whole session package to catch regressions**

Run: `go test ./internal/session/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/session/session.go internal/session/attention_test.go
git commit -m "session: bump attention_at on waiting_input and non-shell completion"
```

---

## Task 4: userstore `session_seen` table + methods

**Files:**
- Create: `internal/userstore/migrations/0005_session_seen.sql`
- Modify: `internal/userstore/store.go` (`Store` interface ~143; `SQLiteStore` methods, append near other methods)
- Test: `internal/userstore/store_test.go` (or a new `seen_test.go` in package `userstore`)

- [ ] **Step 1: Write the failing test**

Create `internal/userstore/seen_test.go`:

```go
package userstore

import (
	"context"
	"testing"
)

func TestSessionSeen(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if err := s.SetSeen(ctx, "userA", []string{"sess1", "sess2"}, 100); err != nil {
		t.Fatalf("SetSeen: %v", err)
	}
	// Upsert: newer timestamp overwrites.
	if err := s.SetSeen(ctx, "userA", []string{"sess1"}, 200); err != nil {
		t.Fatalf("SetSeen upsert: %v", err)
	}
	got, err := s.SeenAt(ctx, "userA")
	if err != nil {
		t.Fatalf("SeenAt: %v", err)
	}
	if got["sess1"] != 200 || got["sess2"] != 100 {
		t.Fatalf("unexpected seen map: %+v", got)
	}
	// Isolation: another user sees nothing.
	other, _ := s.SeenAt(ctx, "userB")
	if len(other) != 0 {
		t.Fatalf("cross-user leak: %+v", other)
	}
	// Prune by session id removes across users.
	_ = s.SetSeen(ctx, "userB", []string{"sess1"}, 50)
	if err := s.PruneSeenSession(ctx, "sess1"); err != nil {
		t.Fatalf("PruneSeenSession: %v", err)
	}
	a, _ := s.SeenAt(ctx, "userA")
	if _, ok := a["sess1"]; ok {
		t.Fatalf("sess1 not pruned for userA")
	}
	if a["sess2"] != 100 {
		t.Fatalf("prune removed too much: %+v", a)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/userstore/ -run TestSessionSeen -v`
Expected: FAIL — `SetSeen` undefined.

- [ ] **Step 3: Add the migration**

Create `internal/userstore/migrations/0005_session_seen.sql`:

```sql
CREATE TABLE session_seen (
    user_id    TEXT    NOT NULL,
    session_id TEXT    NOT NULL,
    seen_at    INTEGER NOT NULL,
    PRIMARY KEY (user_id, session_id)
);
```

- [ ] **Step 4: Add interface methods + implementation**

In `internal/userstore/store.go`, add to the `Store` interface (before `Close() error`):

```go
	// Session seen/unread inbox
	SetSeen(ctx context.Context, userID string, sessionIDs []string, at int64) error
	SeenAt(ctx context.Context, userID string) (map[string]int64, error)
	PruneSeenSession(ctx context.Context, sessionID string) error
```

Append the implementations (anywhere among the other `func (s *SQLiteStore)` methods):

```go
func (s *SQLiteStore) SetSeen(ctx context.Context, userID string, sessionIDs []string, at int64) error {
	if userID == "" || len(sessionIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SetSeen: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO session_seen(user_id, session_id, seen_at)
		 VALUES(?,?,?)
		 ON CONFLICT(user_id, session_id) DO UPDATE SET seen_at=excluded.seen_at`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare SetSeen: %w", err)
	}
	defer stmt.Close()
	for _, sid := range sessionIDs {
		if sid == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, userID, sid, at); err != nil {
			tx.Rollback()
			return fmt.Errorf("exec SetSeen: %w", err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) SeenAt(ctx context.Context, userID string) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, seen_at FROM session_seen WHERE user_id=?`, userID)
	if err != nil {
		return nil, fmt.Errorf("query SeenAt: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var sid string
		var at int64
		if err := rows.Scan(&sid, &at); err != nil {
			return nil, fmt.Errorf("scan SeenAt: %w", err)
		}
		out[sid] = at
	}
	return out, rows.Err()
}

func (s *SQLiteStore) PruneSeenSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM session_seen WHERE session_id=?`, sessionID)
	return err
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/userstore/ -run TestSessionSeen -v`
Expected: PASS

- [ ] **Step 6: Run the userstore package**

Run: `go test ./internal/userstore/`
Expected: PASS (migration applies cleanly alongside existing 0001-0004).

- [ ] **Step 7: Commit**

```bash
git add internal/userstore/migrations/0005_session_seen.sql internal/userstore/store.go internal/userstore/seen_test.go
git commit -m "userstore: session_seen table + SetSeen/SeenAt/PruneSeenSession"
```

---

## Task 5: compute `Unread` in the per-owner session list

**Files:**
- Modify: `internal/relay/client_sessions_conn.go` (`sessionInfoListForOwner` ~87; `writeSessionList` ~52)
- Test: `internal/relay/client_sessions_unread_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/relay/client_sessions_unread_test.go`. Use the existing in-memory store + the list helper pattern. The key assertions: an attention-bearing session with no subscriber and no seen row is `Unread`; once `SetSeen` is at/after `attention_at`, it is not.

```go
package relay

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
)

func TestUnreadComputation(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	srv := &Server{registry: session.NewRegistry(), cfg: Config{Store: store}}

	sess := session.New(uuid.New(), proto.OpenPayload{Cols: 80, Rows: 24})
	sess.OwnerUserID = "userA"
	// Simulate a non-shell completion that set attention_at.
	sess.SetMetaForTest(proto.SessionInfo{Type: session.SessionTypeAI, TaskState: proto.TaskStateCompleted, AttentionAt: 1000})
	srv.registry.Add(sess)

	// No seen row, no subscriber -> unread.
	seen, _ := store.SeenAt(ctx, "userA")
	infos := srv.sessionInfoListForOwner("userA", seen)
	if len(infos) != 1 || !infos[0].Unread {
		t.Fatalf("expected unread, got %+v", infos)
	}

	// Mark seen at >= attention_at -> read.
	_ = store.SetSeen(ctx, "userA", []string{sess.ID.String()}, 1000)
	seen, _ = store.SeenAt(ctx, "userA")
	infos = srv.sessionInfoListForOwner("userA", seen)
	if infos[0].Unread {
		t.Fatalf("expected read after SetSeen, got unread")
	}
}
```

> NOTE: this test needs a way to set session meta directly. If a test helper like `SetMetaForTest` does not exist, add a small exported test helper to `internal/session` (guarded comment "test-only"), or drive the meta through `updateTerminalState(osc(...))` as in Task 3. Prefer driving via `updateTerminalState` if you want zero production-surface test hooks. Confirm `Server` struct field names (`registry`, `cfg`) and `Registry.Add` signature against the codebase before finalizing.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestUnreadComputation -v`
Expected: FAIL — `sessionInfoListForOwner` takes one argument (no `seen`).

- [ ] **Step 3: Thread seen map + compute unread**

In `internal/relay/client_sessions_conn.go`, change `sessionInfoListForOwner`:

```go
func (s *Server) sessionInfoListForOwner(ownerUserID string, seen map[string]int64) []proto.SessionInfo {
	sessions := s.registry.List()
	infos := make([]proto.SessionInfo, 0)
	for _, ss := range sessions {
		if ss.OwnerUserID != ownerUserID {
			continue
		}
		info := ss.Info()
		if info.AttentionAt > 0 && info.AttentionAt > seen[info.ID] && ss.SubscriberCount() == 0 {
			info.Unread = true
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos
}
```

In `writeSessionList`, where it calls the owner list:

```go
	if ownerUserID != "" {
		var seen map[string]int64
		if s.cfg.Store != nil {
			seen, _ = s.cfg.Store.SeenAt(ctx, ownerUserID)
		}
		infos = s.sessionInfoListForOwner(ownerUserID, seen)
	} else {
		infos = s.sessionInfoList()
	}
```

(A nil `seen` map reads as 0 for any key — correct: never-seen ⇒ unread.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestUnreadComputation -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/relay/client_sessions_conn.go internal/relay/client_sessions_unread_test.go
git commit -m "relay: compute per-user Unread in the session list"
```

---

## Task 6: auto-mark-seen on ATTACH

**Files:**
- Modify: `internal/relay/client_conn.go` (TypeAttach case, just after `sess.Subscribe(...)` ~157)
- Test: `internal/relay/client_conn_test.go` (extend existing attach test, or add `TestAttachMarksSeen`)

- [ ] **Step 1: Write the failing test**

Add to `internal/relay/client_conn_test.go` (reuse `newClientTestStore` which creates users A/B + cookies):

```go
func TestAttachMarksSeen(t *testing.T) {
	// Build a server with registry + store, add an owned session with
	// attention_at set, then drive a client ATTACH and assert a seen row
	// appears for the owner at >= attention_at.
	//
	// Follow the existing attach test in this file for wiring the websocket
	// client and server (newClientTestStore + the test server harness).
	// After a successful attach + brief wait:
	//   seen, _ := store.SeenAt(ctx, userAID)
	//   if seen[sessionID] == 0 { t.Fatalf("attach did not mark seen") }
}
```

> NOTE: Flesh this out against the concrete attach harness already present in `client_conn_test.go` (it has helpers to spin up the relay and connect a client). The assertion is: a `session_seen` row exists for the attaching user + session after attach.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestAttachMarksSeen -v`
Expected: FAIL — no seen row written on attach.

- [ ] **Step 3: Write seen on attach**

In `internal/relay/client_conn.go`, immediately after `sess, sub` are established in the `case proto.TypeAttach` block (after `sub, _ = sess.Subscribe(...)`):

```go
			if ownerUserID != "" && s.cfg.Store != nil {
				// Attaching == viewing == read. Best-effort; a failed write
				// just leaves the item unread, which is safe.
				_ = s.cfg.Store.SetSeen(context.Background(), ownerUserID,
					[]string{sess.ID.String()}, time.Now().Unix())
			}
```

Ensure `context` and `time` are imported in `client_conn.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestAttachMarksSeen -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/relay/client_conn.go internal/relay/client_conn_test.go
git commit -m "relay: auto-mark-seen on client ATTACH"
```

---

## Task 7: prune seen on session removal

**Files:**
- Modify: `internal/relay/server.go` (add `removeSession`)
- Modify: `internal/relay/agent_conn.go:64`, `internal/relay/adopt.go:58`, `internal/relay/uplink_conn.go:343,357,466`
- Test: `internal/relay/remove_session_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package relay

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
)

func TestRemoveSessionPrunesSeen(t *testing.T) {
	ctx := context.Background()
	store, _ := userstore.Open(ctx, ":memory:")
	defer store.Close()
	srv := &Server{registry: session.NewRegistry(), cfg: Config{Store: store}}

	sess := session.New(uuid.New(), proto.OpenPayload{Cols: 80, Rows: 24})
	sess.OwnerUserID = "userA"
	srv.registry.Add(sess)
	_ = store.SetSeen(ctx, "userA", []string{sess.ID.String()}, 123)

	srv.removeSession(sess.ID)

	seen, _ := store.SeenAt(ctx, "userA")
	if _, ok := seen[sess.ID.String()]; ok {
		t.Fatalf("removeSession did not prune seen row")
	}
	if _, ok := srv.registry.Get(sess.ID); ok {
		t.Fatalf("removeSession did not remove from registry")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestRemoveSessionPrunesSeen -v`
Expected: FAIL — `srv.removeSession` undefined.

- [ ] **Step 3: Add `removeSession` and route call sites through it**

In `internal/relay/server.go`:

```go
// removeSession removes a session from the registry and prunes any per-user
// seen rows for it. All session teardown paths funnel through here so the
// session_seen table does not accumulate rows for dead sessions.
func (s *Server) removeSession(id uuid.UUID) {
	s.registry.Remove(id)
	if s.cfg.Store != nil {
		_ = s.cfg.Store.PruneSeenSession(context.Background(), id.String())
	}
}
```

Ensure `context` and `github.com/google/uuid` are imported in `server.go`.

Replace each removal call site:
- `internal/relay/agent_conn.go:64` `s.registry.Remove(openFrame.SessionID)` → `s.removeSession(openFrame.SessionID)`
- `internal/relay/adopt.go:58` `s.registry.Remove(id)` → `s.removeSession(id)`
- `internal/relay/uplink_conn.go:343` `s.registry.Remove(id)` → `s.removeSession(id)`
- `internal/relay/uplink_conn.go:357` `s.registry.Remove(id)` → `s.removeSession(id)`
- `internal/relay/uplink_conn.go:466` `s.registry.Remove(f.SessionID)` → `s.removeSession(f.SessionID)`

- [ ] **Step 4: Run test + full relay package**

Run: `go test ./internal/relay/ -run TestRemoveSessionPrunesSeen -v`
Expected: PASS
Run: `go test ./internal/relay/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/relay/server.go internal/relay/agent_conn.go internal/relay/adopt.go internal/relay/uplink_conn.go internal/relay/remove_session_test.go
git commit -m "relay: funnel session removal through removeSession + prune seen"
```

---

## Task 8: `POST /api/sessions/seen` endpoint

**Files:**
- Create: `internal/relay/sessions_seen_http.go`
- Modify: `internal/relay/server.go` (register route inside the `if cfg.Resolver != nil && cfg.Store != nil` block, ~160-170)
- Test: `internal/relay/sessions_seen_http_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package relay

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionsSeenEndpoint(t *testing.T) {
	// newClientTestStore wires users A/B + cookies. Build the full server
	// handler the same way other *_http_test.go files do (e.g. me_http_test.go
	// / auth_http_test.go), add an owned session with attention_at, POST
	// /api/sessions/seen {"all":true} with A's cookie + CSRF, then assert a
	// seen row exists for A's session and the response is 204.
	//
	// Also assert: POSTing B's session id under A's cookie does NOT create a
	// seen row for that session (cross-user ids are silently dropped), and a
	// request without CSRF returns 403/401.
	_ = bytes.NewReader
	_ = httptest.NewRequest
	_ = http.StatusNoContent
}
```

> NOTE: model the request wiring (cookie + `X-CSRF-Token`) on `me_http_test.go`'s `signupAndLogin` flow. The CSRF token for a logged-in cookie is available there.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestSessionsSeenEndpoint -v`
Expected: FAIL — route not registered (404) / handler undefined.

- [ ] **Step 3: Implement the handler**

Create `internal/relay/sessions_seen_http.go`:

```go
package relay

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// handleSessionsSeenHTTP implements POST /api/sessions/seen. Body is either
// {"all": true} (mark every session the caller owns) or
// {"session_ids": ["..."]} (mark a specific set; ids not owned by the caller
// are silently ignored). Auth is the cookie session (wrapped in RequireCSRF
// at registration).
func (s *Server) handleSessionsSeenHTTP(w http.ResponseWriter, r *http.Request) {
	p := s.cfg.Resolver.Resolve(r)
	if !p.IsUser() {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	var body struct {
		All        bool     `json:"all"`
		SessionIDs []string `json:"session_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var ids []string
	if body.All {
		for _, ss := range s.registry.List() {
			if ss.OwnerUserID == p.UserID {
				ids = append(ids, ss.ID.String())
			}
		}
	} else {
		for _, raw := range body.SessionIDs {
			id, err := uuid.Parse(raw)
			if err != nil {
				continue
			}
			if ss, ok := s.registry.Get(id); ok && ss.OwnerUserID == p.UserID {
				ids = append(ids, ss.ID.String())
			}
		}
	}

	if len(ids) > 0 {
		if err := s.cfg.Store.SetSeen(r.Context(), p.UserID, ids, time.Now().Unix()); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Register the route**

In `internal/relay/server.go`, inside the existing `if cfg.Resolver != nil && cfg.Store != nil { ... }` block (where `authSrv.RegisterInto(s.mux)` is called):

```go
		s.mux.Handle("POST /api/sessions/seen",
			RequireCSRF(cfg.Resolver, http.HandlerFunc(s.handleSessionsSeenHTTP)))
```

> Confirm the resolver variable name in scope is `cfg.Resolver` (not `s.cfg.Resolver`) at that point in the constructor; match the surrounding lines.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestSessionsSeenEndpoint -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/relay/sessions_seen_http.go internal/relay/server.go internal/relay/sessions_seen_http_test.go
git commit -m "relay: POST /api/sessions/seen (mark all / mark ids read)"
```

---

## Task 9: suppress Web Push while a session is watched

**Files:**
- Modify: `internal/relay/uplink_conn.go` (`notifySession` ~172; `DispatchCommandFinished` site ~502)
- Test: `internal/relay/uplink_webhook_test.go` or a new `internal/relay/notify_suppress_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/relay/notify_suppress_test.go`. Use a fake/spy `WebPush` and `Webhook` (the existing `uplink_webhook_test.go` already constructs a webhook spy — mirror that for WebPush). Drive a command-finished event for a session that HAS a subscriber, and assert:
- WebPush dispatch was NOT called.
- Webhook dispatch WAS called.
Then with zero subscribers, assert WebPush IS called.

```go
package relay

import "testing"

func TestNotificationSuppressedWhenWatched(t *testing.T) {
	// 1. Build a server with a spy WebPush + spy Webhook (see uplink_webhook_test.go).
	// 2. Create a mirror session, attach one subscriber (sess.Subscribe(...)).
	// 3. Fire the command-finished path that calls DispatchCommandFinished.
	// 4. Assert spyWebPush.calls == 0 and spyWebhook.calls == 1.
	// 5. Remove the subscriber; fire again; assert spyWebPush.calls == 1.
	_ = t
}
```

> NOTE: wire the spies the same way `uplink_webhook_test.go` does. If WebPush has no spy yet, define a tiny struct implementing the `WebPush` interface used by `Config` that counts calls.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestNotificationSuppressedWhenWatched -v`
Expected: FAIL — push fires even with a subscriber attached.

- [ ] **Step 3: Add the suppression guards**

In `internal/relay/uplink_conn.go`, in `notifySession`, extend the early return:

```go
	notifySession := func(ms *mirrorState, info proto.SessionInfo, notificationType string, idleForSeconds int) {
		if s.cfg.WebPush == nil || ms == nil {
			return
		}
		if ms.sess.SubscriberCount() > 0 { // ADD: watching == read == no push
			return
		}
		...
```

At the `DispatchCommandFinished` site (~502), gate ONLY the WebPush call, leaving the webhook call untouched:

```go
	if s.cfg.WebPush != nil && ms.sess.SubscriberCount() == 0 {
		s.cfg.WebPush.DispatchCommandFinished(ms.sess.OwnerUserID, webpush.CommandFinished{
			...
		})
	}
	if s.cfg.Webhook != nil {
		s.cfg.Webhook.DispatchCommandFinished(ms.sess.OwnerUserID, webhook.CommandFinished{
			... // unchanged — webhooks are M2M, not human attention
		})
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestNotificationSuppressedWhenWatched -v`
Expected: PASS

- [ ] **Step 5: Run the full relay package**

Run: `go test ./internal/relay/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/relay/uplink_conn.go internal/relay/notify_suppress_test.go
git commit -m "relay: suppress Web Push while a session is being watched"
```

---

## Final verification

- [ ] **Run the full backend test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Vet**

Run: `go vet ./internal/...`
Expected: no findings

---

## Spec coverage check

| Spec section | Task |
| --- | --- |
| §2 unread predicate | Task 5 (compute), Tasks 2/3 (inputs) |
| §4 attention_at derivation | Task 3 |
| §5 seen write: attach | Task 6 |
| §5 seen write: mark all / single | Task 8 |
| §5 seen storage | Task 4 |
| §6 notification de-noising | Task 9 |
| §9 migration/compat | Task 4 (additive migration); omitempty fields (Task 1) |
| §3 protocol fields | Task 1 (note: META carries attention_at only, not unread — see deviation above) |

Out of scope for this plan (frontend rendering — separate plan): §7 (inbox UI, mark-read controls, host rollup, TabBar unread dot), §8 frontend tests.
