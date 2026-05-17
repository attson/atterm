# Web UI Redesign — PR C: Settings redesign + new APIs

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Restructure `/settings.html` into a 4-tab layout (API Tokens / Change Password / Signed-in devices / Danger zone) and add the backing user-facing APIs: list/revoke web sessions, sign-out-others, and hard-delete account. Backend gets last-admin protection and password re-verification on account deletion.

**Architecture:** Adds 4 store methods (`ListUserWebSessions`, `DeleteUserWebSessionByIDHash`, `DeleteOtherWebSessionsForUser`, `DeleteUser`) on `*SQLiteStore`. Four new HTTP endpoints under `/api/me/*` wired through CSRF middleware. Settings page becomes a tabbed shell — each tab is a `<section class="card">` panel, JS swaps which is visible via a hash route (`#api-tokens` / `#change-password` / `#sessions` / `#danger`) so deep links work. New `Signed-in devices` panel calls the list/revoke/sign-out-others endpoints; new `Danger zone` panel collects email + current password before hard-deleting.

**Tech Stack:** Go (SQLite via existing userstore), pure ESM JS in `web/`. Reuses existing `auth.js::authFetch` + the CSRF token in `cachedCSRF`. No new deps.

**Spec:** `docs/superpowers/specs/2026-05-17-web-ui-redesign-design.md` (sections "User-facing API additions" + IA "Settings sub-tabs" + Security audit items 1 & 2).

---

## File Map

**Create:**
- `internal/userstore/sessions_admin.go` — `ListUserWebSessions` / `DeleteUserWebSessionByIDHash` / `DeleteOtherWebSessionsForUser`
- `internal/userstore/users_delete.go` — `DeleteUser` (invitations cascade nullify + delete)
- `internal/userstore/sessions_admin_test.go` + `users_delete_test.go` — covering both
- `internal/relay/me_sessions_http.go` — three session endpoints (list, delete one, sign-out-others)
- `internal/relay/me_delete_http.go` — `DELETE /api/me`
- `internal/relay/me_sessions_http_test.go` + `me_delete_http_test.go` — endpoint tests including last-admin 409 and password re-verify
- `web/settings-sessions.js` — Signed-in devices panel logic
- `web/settings-danger.js` — Danger zone panel logic

**Modify:**
- `internal/userstore/store.go` — add 4 interface methods
- `internal/relay/auth_http.go` — register the 4 new mux entries (or do it via a new `MeServer` if AuthServer is getting crowded; existing AuthServer already owns the /api/me/* family, so add here)
- `internal/userstore/admin.go` — `countAdmins` (used by `DeleteUser` last-admin check) was added in PR A; verify it exists. If not, add the helper here.
- `web/settings.html` — replace single-card stack with 4-tab + 4-panel layout
- `web/settings.js` — tab switching + hash routing; delegate panel-specific logic to settings-sessions.js and settings-danger.js
- `web/style.css` — new `.subtabs`, `.subtab.active`, `.danger-zone` styling
- `web/sw.js` — add `./settings-sessions.js` + `./settings-danger.js` to ASSETS; hash bump
- `web/settings.test.mjs` — assert new tab structure + panel ids; replace any stale assertions about single-card layout

**Delete:** none.

---

## Phase 1 — Backend store methods

### Task 1 — `ListUserWebSessions(ctx, userID)` store method

**Files:**
- Modify: `internal/userstore/store.go` (interface)
- Create: `internal/userstore/sessions_admin.go` (impl)
- Create: `internal/userstore/sessions_admin_test.go`

- [ ] **Step 1: Add interface method**

In `internal/userstore/store.go`, in the "Web sessions" section of the `Store` interface, after `PurgeExpiredWebSessions`, add:

```go
// ListUserWebSessions returns all non-expired sessions for userID,
// ordered by created_at DESC. Used by the Settings → Signed-in devices
// panel.
ListUserWebSessions(ctx context.Context, userID string) ([]UserWebSession, error)
```

Define the response type in the same file (next to the Web sessions section, or in websessions.go — wherever the existing WebSession-related types live; if there are none, put it in sessions_admin.go):

```go
// UserWebSession is the public view of a row in web_sessions for the
// owning user. id_hash is opaque (already hashed); plaintext cookies
// are not stored or exposed.
type UserWebSession struct {
    IDHash    string
    UserAgent string
    IPPrefix  string
    CreatedAt time.Time
    ExpiresAt time.Time
}
```

- [ ] **Step 2: Write failing test**

Create `internal/userstore/sessions_admin_test.go`:

```go
package userstore

import (
    "context"
    "testing"
    "time"
)

func TestListUserWebSessions_OrderAndFields(t *testing.T) {
    ctx := context.Background()
    s, err := Open(ctx, ":memory:")
    if err != nil { t.Fatal(err) }
    defer s.Close()

    u, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")

    s1, _ := s.CreateWebSession(ctx, u.ID, "ua/firefox", "203.0.113.0/24")
    time.Sleep(2 * time.Millisecond) // ensure created_at ordering is deterministic
    s2, _ := s.CreateWebSession(ctx, u.ID, "ua/chrome", "203.0.113.0/24")

    list, err := s.ListUserWebSessions(ctx, u.ID)
    if err != nil { t.Fatal(err) }
    if len(list) != 2 {
        t.Fatalf("len=%d; want 2", len(list))
    }
    if list[0].UserAgent != "ua/chrome" || list[1].UserAgent != "ua/firefox" {
        t.Errorf("ordering: got %q,%q; want newest first (ua/chrome,ua/firefox)",
            list[0].UserAgent, list[1].UserAgent)
    }
    if list[0].IDHash == "" || list[1].IDHash == "" {
        t.Error("IDHash empty")
    }
    if list[0].IPPrefix != "203.0.113.0/24" {
        t.Errorf("IPPrefix not populated: %q", list[0].IPPrefix)
    }
    if list[0].ExpiresAt.Before(time.Now().Add(29 * 24 * time.Hour)) {
        t.Errorf("ExpiresAt too soon: %v", list[0].ExpiresAt)
    }
    _ = s1
    _ = s2
}

func TestListUserWebSessions_ScopedToUser(t *testing.T) {
    ctx := context.Background()
    s, err := Open(ctx, ":memory:")
    if err != nil { t.Fatal(err) }
    defer s.Close()

    u1, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
    u2, _ := s.CreateUser(ctx, "b@example.com", "passphrase-1234")
    _, _ = s.CreateWebSession(ctx, u1.ID, "ua-u1", "1.2.3.0/24")
    _, _ = s.CreateWebSession(ctx, u2.ID, "ua-u2", "5.6.7.0/24")

    list1, _ := s.ListUserWebSessions(ctx, u1.ID)
    if len(list1) != 1 || list1[0].UserAgent != "ua-u1" {
        t.Errorf("u1 list leaked u2 rows or missing own: %+v", list1)
    }
}
```

- [ ] **Step 3: Run, expect compile failure**

```bash
go test ./internal/userstore/ 2>&1 | tail -5
```

Expected: `*SQLiteStore does not implement Store (missing method ListUserWebSessions)`.

- [ ] **Step 4: Implement**

Create `internal/userstore/sessions_admin.go`:

```go
package userstore

import (
    "context"
    "fmt"
    "time"
)

// ListUserWebSessions returns all non-expired sessions for userID,
// ordered by created_at DESC.
func (s *SQLiteStore) ListUserWebSessions(ctx context.Context, userID string) ([]UserWebSession, error) {
    nowMs := time.Now().UnixMilli()
    rows, err := s.db.QueryContext(ctx,
        `SELECT id_hash, COALESCE(user_agent, ''), COALESCE(ip_prefix, ''), created_at, expires_at
         FROM web_sessions
         WHERE user_id = ? AND expires_at >= ?
         ORDER BY created_at DESC`,
        userID, nowMs,
    )
    if err != nil {
        return nil, fmt.Errorf("list web_sessions: %w", err)
    }
    defer rows.Close()
    var out []UserWebSession
    for rows.Next() {
        var (
            idHash    string
            ua        string
            ipPrefix  string
            createdMs int64
            expiresMs int64
        )
        if err := rows.Scan(&idHash, &ua, &ipPrefix, &createdMs, &expiresMs); err != nil {
            return nil, fmt.Errorf("scan web_session: %w", err)
        }
        out = append(out, UserWebSession{
            IDHash:    idHash,
            UserAgent: ua,
            IPPrefix:  ipPrefix,
            CreatedAt: time.UnixMilli(createdMs),
            ExpiresAt: time.UnixMilli(expiresMs),
        })
    }
    return out, rows.Err()
}
```

- [ ] **Step 5: Run tests**

```bash
go test -run TestListUserWebSessions -v ./internal/userstore/ 2>&1 | tail -15
go test -count=1 ./internal/userstore/ 2>&1 | tail -5
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/userstore/store.go internal/userstore/sessions_admin.go internal/userstore/sessions_admin_test.go
git commit -m "userstore: ListUserWebSessions for Settings → Signed-in devices"
```

---

### Task 2 — `DeleteUserWebSessionByIDHash(ctx, userID, idHash)` store method

**Files:**
- Modify: `internal/userstore/store.go`
- Modify: `internal/userstore/sessions_admin.go`
- Modify: `internal/userstore/sessions_admin_test.go`

- [ ] **Step 1: Add interface method**

In the Web sessions section of `Store`:

```go
// DeleteUserWebSessionByIDHash revokes the session with the given
// id_hash, ONLY IF it belongs to userID. Returns (false, nil) if no
// such session exists or it belongs to a different user — never
// reveal cross-user existence.
DeleteUserWebSessionByIDHash(ctx context.Context, userID, idHash string) (deleted bool, err error)
```

- [ ] **Step 2: Write failing tests**

Append to `internal/userstore/sessions_admin_test.go`:

```go
func TestDeleteUserWebSessionByIDHash_Success(t *testing.T) {
    ctx := context.Background()
    s, _ := Open(ctx, ":memory:")
    defer s.Close()
    u, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
    _, _ = s.CreateWebSession(ctx, u.ID, "ua1", "")
    list, _ := s.ListUserWebSessions(ctx, u.ID)
    if len(list) != 1 { t.Fatalf("setup: list len %d; want 1", len(list)) }
    deleted, err := s.DeleteUserWebSessionByIDHash(ctx, u.ID, list[0].IDHash)
    if err != nil { t.Fatal(err) }
    if !deleted { t.Error("deleted=false; want true") }
    after, _ := s.ListUserWebSessions(ctx, u.ID)
    if len(after) != 0 { t.Errorf("session still listed after delete: %+v", after) }
}

func TestDeleteUserWebSessionByIDHash_CrossUserIsNoop(t *testing.T) {
    ctx := context.Background()
    s, _ := Open(ctx, ":memory:")
    defer s.Close()
    u1, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
    u2, _ := s.CreateUser(ctx, "b@example.com", "passphrase-1234")
    _, _ = s.CreateWebSession(ctx, u2.ID, "ua-u2", "")
    list2, _ := s.ListUserWebSessions(ctx, u2.ID)
    // Attempt to delete u2's session as u1.
    deleted, err := s.DeleteUserWebSessionByIDHash(ctx, u1.ID, list2[0].IDHash)
    if err != nil { t.Fatal(err) }
    if deleted { t.Error("deleted=true for cross-user delete; want false") }
    after, _ := s.ListUserWebSessions(ctx, u2.ID)
    if len(after) != 1 { t.Errorf("u2's session was wrongly deleted: %+v", after) }
}

func TestDeleteUserWebSessionByIDHash_UnknownIDHashNoop(t *testing.T) {
    ctx := context.Background()
    s, _ := Open(ctx, ":memory:")
    defer s.Close()
    u, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
    deleted, err := s.DeleteUserWebSessionByIDHash(ctx, u.ID, "deadbeef00")
    if err != nil { t.Fatal(err) }
    if deleted { t.Error("deleted=true for unknown id_hash; want false") }
}
```

- [ ] **Step 3: Run, expect failure**

```bash
go test -run TestDeleteUserWebSessionByIDHash -v ./internal/userstore/ 2>&1 | tail -10
```

Expected: compile failure (missing method).

- [ ] **Step 4: Implement**

Append to `internal/userstore/sessions_admin.go`:

```go
// DeleteUserWebSessionByIDHash revokes the session ONLY IF owned by userID.
// The (user_id, id_hash) WHERE clause is the security boundary.
func (s *SQLiteStore) DeleteUserWebSessionByIDHash(ctx context.Context, userID, idHash string) (bool, error) {
    res, err := s.db.ExecContext(ctx,
        `DELETE FROM web_sessions WHERE user_id = ? AND id_hash = ?`,
        userID, idHash,
    )
    if err != nil {
        return false, fmt.Errorf("delete web_session by id_hash: %w", err)
    }
    n, err := res.RowsAffected()
    if err != nil {
        return false, fmt.Errorf("rows affected: %w", err)
    }
    return n > 0, nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test -run TestDeleteUserWebSessionByIDHash -v ./internal/userstore/ 2>&1 | tail -15
go test -count=1 ./internal/userstore/ 2>&1 | tail -5
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/userstore/store.go internal/userstore/sessions_admin.go internal/userstore/sessions_admin_test.go
git commit -m "userstore: DeleteUserWebSessionByIDHash with owner-scoped WHERE"
```

---

### Task 3 — `DeleteOtherWebSessionsForUser(ctx, userID, exceptIDHash)`

**Files:**
- Modify: `internal/userstore/store.go`
- Modify: `internal/userstore/sessions_admin.go`
- Modify: `internal/userstore/sessions_admin_test.go`

- [ ] **Step 1: Add interface method**

```go
// DeleteOtherWebSessionsForUser deletes every session for userID
// except the one whose id_hash matches exceptIDHash. Returns the
// number of rows deleted. Used by Settings → Sign out everywhere
// except this device.
DeleteOtherWebSessionsForUser(ctx context.Context, userID, exceptIDHash string) (int64, error)
```

- [ ] **Step 2: Write failing test**

Append to `internal/userstore/sessions_admin_test.go`:

```go
func TestDeleteOtherWebSessionsForUser_KeepsExceptOnly(t *testing.T) {
    ctx := context.Background()
    s, _ := Open(ctx, ":memory:")
    defer s.Close()
    u, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
    _, _ = s.CreateWebSession(ctx, u.ID, "ua1", "")
    _, _ = s.CreateWebSession(ctx, u.ID, "ua2", "")
    _, _ = s.CreateWebSession(ctx, u.ID, "ua3", "")
    list, _ := s.ListUserWebSessions(ctx, u.ID)
    if len(list) != 3 { t.Fatalf("setup: %d; want 3", len(list)) }
    keep := list[1].IDHash // arbitrary middle row
    n, err := s.DeleteOtherWebSessionsForUser(ctx, u.ID, keep)
    if err != nil { t.Fatal(err) }
    if n != 2 { t.Errorf("deleted=%d; want 2", n) }
    after, _ := s.ListUserWebSessions(ctx, u.ID)
    if len(after) != 1 || after[0].IDHash != keep {
        t.Errorf("after: %+v; want only %q", after, keep)
    }
}

func TestDeleteOtherWebSessionsForUser_ExceptUnknownDeletesAll(t *testing.T) {
    ctx := context.Background()
    s, _ := Open(ctx, ":memory:")
    defer s.Close()
    u, _ := s.CreateUser(ctx, "a@example.com", "passphrase-1234")
    _, _ = s.CreateWebSession(ctx, u.ID, "ua1", "")
    _, _ = s.CreateWebSession(ctx, u.ID, "ua2", "")
    n, err := s.DeleteOtherWebSessionsForUser(ctx, u.ID, "no-such-hash")
    if err != nil { t.Fatal(err) }
    if n != 2 { t.Errorf("deleted=%d; want 2 (unknown exceptIDHash drops nothing)", n) }
    after, _ := s.ListUserWebSessions(ctx, u.ID)
    if len(after) != 0 { t.Errorf("after: %+v; want empty", after) }
}
```

- [ ] **Step 3: Run, expect failure** → Step 4: implement.

Append to `internal/userstore/sessions_admin.go`:

```go
// DeleteOtherWebSessionsForUser drops every row owned by userID except
// the one matching exceptIDHash. The caller is expected to pass the
// current request's session id_hash so the operator stays signed in.
func (s *SQLiteStore) DeleteOtherWebSessionsForUser(ctx context.Context, userID, exceptIDHash string) (int64, error) {
    res, err := s.db.ExecContext(ctx,
        `DELETE FROM web_sessions WHERE user_id = ? AND id_hash != ?`,
        userID, exceptIDHash,
    )
    if err != nil {
        return 0, fmt.Errorf("delete other web_sessions: %w", err)
    }
    n, err := res.RowsAffected()
    if err != nil {
        return 0, fmt.Errorf("rows affected: %w", err)
    }
    return n, nil
}
```

- [ ] **Step 5: Run tests + commit**

```bash
go test -run TestDeleteOtherWebSessionsForUser -v ./internal/userstore/ 2>&1 | tail -15
git add internal/userstore/store.go internal/userstore/sessions_admin.go internal/userstore/sessions_admin_test.go
git commit -m "userstore: DeleteOtherWebSessionsForUser for sign-out-everywhere-else"
```

---

### Task 4 — `DeleteUser(ctx, userID)` store method

**Files:**
- Modify: `internal/userstore/store.go`
- Create: `internal/userstore/users_delete.go`
- Create: `internal/userstore/users_delete_test.go`

- [ ] **Step 1: Add interface method**

In the "Users" section of `Store`:

```go
// DeleteUser hard-deletes userID. api_tokens and web_sessions cascade
// via the existing FK. invitations.consumed_by is REFERENCES users(id)
// without cascade (history field), so this method first sets that
// column to NULL for every invitation consumed by the user, then
// deletes the users row, in one transaction.
DeleteUser(ctx context.Context, userID string) error
```

- [ ] **Step 2: Write failing tests**

Create `internal/userstore/users_delete_test.go`:

```go
package userstore

import (
    "context"
    "testing"
    "time"
)

func TestDeleteUser_CascadesAndNullsConsumedBy(t *testing.T) {
    ctx := context.Background()
    s, _ := Open(ctx, ":memory:")
    defer s.Close()

    // Create an admin to issue invites; consumer is the user we'll delete.
    issuer, _ := s.CreateUser(ctx, "admin@example.com", "passphrase-1234")
    invite, _, _ := s.CreateInvitation(ctx, ptrTime(time.Now().Add(24*time.Hour)), "for delete test")

    consumer, _ := s.CreateUser(ctx, "victim@example.com", "passphrase-1234")
    if err := s.ConsumeInvitation(ctx, invite.Expose(), consumer.ID); err != nil {
        t.Fatal(err)
    }

    // Give the consumer an api_token and a web_session so we can verify cascade.
    _, _, _ = s.CreateAPIToken(ctx, consumer.ID, "test-token")
    _, _ = s.CreateWebSession(ctx, consumer.ID, "ua", "")

    // Sanity: invitation is consumed by consumer.
    invs, _ := s.ListInvitations(ctx)
    var ours *Invitation
    for i := range invs {
        if invs[i].ConsumedBy != nil && *invs[i].ConsumedBy == consumer.ID {
            ours = &invs[i]
        }
    }
    if ours == nil { t.Fatal("setup: invitation not consumed by victim") }

    // Delete the consumer.
    if err := s.DeleteUser(ctx, consumer.ID); err != nil {
        t.Fatal(err)
    }

    // User row gone.
    if _, err := s.GetUser(ctx, consumer.ID); err == nil {
        t.Error("user still present after DeleteUser")
    }
    // api_tokens / web_sessions cascade gone.
    toks, _ := s.ListAPITokens(ctx, consumer.ID)
    if len(toks) != 0 { t.Errorf("api tokens not cascaded: %d remain", len(toks)) }
    sess, _ := s.ListUserWebSessions(ctx, consumer.ID)
    if len(sess) != 0 { t.Errorf("web sessions not cascaded: %d remain", len(sess)) }

    // Invitation still exists, but consumed_by is now NULL.
    invsAfter, _ := s.ListInvitations(ctx)
    var foundAfter *Invitation
    for i := range invsAfter {
        if invsAfter[i].CodePrefix == ours.CodePrefix {
            foundAfter = &invsAfter[i]
        }
    }
    if foundAfter == nil {
        t.Fatal("invitation disappeared after victim delete")
    }
    if foundAfter.ConsumedBy != nil {
        t.Errorf("invitation.consumed_by not nulled: %v", *foundAfter.ConsumedBy)
    }

    _ = issuer
}

func ptrTime(t time.Time) *time.Time { return &t }
```

(The exact `Invitation` struct fields may differ; check `internal/userstore/invitations.go` for the type definition — `CodePrefix` / `ConsumedBy` are the names this plan assumes. Adjust if the real fields differ.)

- [ ] **Step 3: Run, expect failure**

```bash
go test -run TestDeleteUser -v ./internal/userstore/ 2>&1 | tail -10
```

Expected: compile failure (missing method).

- [ ] **Step 4: Implement**

Create `internal/userstore/users_delete.go`:

```go
package userstore

import (
    "context"
    "fmt"
)

// DeleteUser hard-deletes userID. Wraps the invitation-null + user-delete
// pair in a transaction so a partial failure doesn't leave the DB in a
// half-deleted state.
func (s *SQLiteStore) DeleteUser(ctx context.Context, userID string) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer func() {
        if err != nil {
            tx.Rollback()
        }
    }()

    if _, err = tx.ExecContext(ctx,
        `UPDATE invitations SET consumed_by = NULL WHERE consumed_by = ?`,
        userID,
    ); err != nil {
        return fmt.Errorf("null invitations.consumed_by: %w", err)
    }
    if _, err = tx.ExecContext(ctx,
        `DELETE FROM users WHERE id = ?`,
        userID,
    ); err != nil {
        return fmt.Errorf("delete user: %w", err)
    }
    if err = tx.Commit(); err != nil {
        return fmt.Errorf("commit delete user: %w", err)
    }
    return nil
}
```

- [ ] **Step 5: Run + commit**

```bash
go test -run TestDeleteUser -v ./internal/userstore/ 2>&1 | tail -15
go test -count=1 ./internal/userstore/ 2>&1 | tail -5

git add internal/userstore/store.go internal/userstore/users_delete.go internal/userstore/users_delete_test.go
git commit -m "userstore: DeleteUser nullifies invitations.consumed_by then drops the user row"
```

---

## Phase 2 — HTTP endpoints

### Task 5 — `GET /api/me/sessions` handler

**Files:**
- Modify: `internal/relay/auth_http.go` (mux registration)
- Create: `internal/relay/me_sessions_http.go`
- Create: `internal/relay/me_sessions_http_test.go`

- [ ] **Step 1: Register the route in `auth_http.go::RegisterInto`**

Add (in the order block where `/api/me/tokens` is registered, around line 66):

```go
mux.Handle("GET /api/me/sessions", http.HandlerFunc(a.handleListSessions))
```

- [ ] **Step 2: Implement handler**

Create `internal/relay/me_sessions_http.go`:

```go
package relay

import (
    "encoding/json"
    "net/http"

    "github.com/attson/atterm/internal/userstore"
)

// sessionRow is the JSON view sent to the client.
type sessionRow struct {
    IDHash    string `json:"id_hash"`
    UserAgent string `json:"user_agent"`
    IPPrefix  string `json:"ip_prefix"`
    CreatedAt int64  `json:"created_at"` // unix milliseconds
    ExpiresAt int64  `json:"expires_at"` // unix milliseconds
    IsCurrent bool   `json:"is_current"`
}

// handleListSessions implements GET /api/me/sessions. Returns the
// caller's web_sessions rows. Marks the row that matches the current
// cookie as is_current=true so the UI hides Revoke on that row.
func (a *AuthServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
    p, ok := a.requireUser(w, r)
    if !ok { return }

    rows, err := a.Store.ListUserWebSessions(r.Context(), p.UserID)
    if err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }

    // Compute the current cookie's hash so we can mark is_current.
    var currentHash string
    if c, cerr := r.Cookie("atterm_session"); cerr == nil && c.Value != "" {
        currentHash = userstore.SessionHash(c.Value)
    }

    out := make([]sessionRow, 0, len(rows))
    for _, s := range rows {
        out = append(out, sessionRow{
            IDHash:    s.IDHash,
            UserAgent: s.UserAgent,
            IPPrefix:  s.IPPrefix,
            CreatedAt: s.CreatedAt.UnixMilli(),
            ExpiresAt: s.ExpiresAt.UnixMilli(),
            IsCurrent: s.IDHash == currentHash,
        })
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(out)
}
```

You may need to export `sessionHash` from userstore. Look in `internal/userstore/websessions.go` — there's an unexported `sessionHash(plaintext string) string`. Either:

a) Export it as `SessionHash(plaintext string) string` — wrap the existing unexported version.
b) Move the hashing here by importing `crypto/sha256` and `encoding/hex`.

Prefer (a): one source of truth for the hash algorithm. Modify `internal/userstore/websessions.go`:

```go
// SessionHash exposes the same hash used by CreateWebSession /
// LookupWebSession so HTTP handlers can compute the id_hash of a
// cookie they hold without round-tripping the store.
func SessionHash(plaintext string) string { return sessionHash(plaintext) }
```

- [ ] **Step 3: Write test**

Create `internal/relay/me_sessions_http_test.go`:

```go
package relay

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/attson/atterm/internal/userstore"
)

func TestListSessions_ReturnsRowsWithIsCurrent(t *testing.T) {
    srv, store := newTestAuthServer(t)
    handler := srv.Routes()
    cookie, userID, cookieValue := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")

    // Create a second session for the same user (simulates another device).
    _, _ = store.CreateWebSession(context.Background(), userID, "other-device", "1.2.3.0/24")

    req := httptest.NewRequest(http.MethodGet, "/api/me/sessions", nil)
    req.AddCookie(cookie)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusOK { t.Fatalf("status %d: %s", rec.Code, rec.Body.String()) }

    var rows []map[string]any
    json.NewDecoder(rec.Body).Decode(&rows)
    if len(rows) != 2 { t.Fatalf("rows=%d; want 2", len(rows)) }

    currentHash := userstore.SessionHash(cookieValue)
    var foundCurrent bool
    for _, r := range rows {
        if r["id_hash"] == currentHash && r["is_current"] == true { foundCurrent = true }
    }
    if !foundCurrent { t.Error("current session not marked is_current=true") }
}

func TestListSessions_RequiresAuth(t *testing.T) {
    srv, _ := newTestAuthServer(t)
    handler := srv.Routes()
    req := httptest.NewRequest(http.MethodGet, "/api/me/sessions", nil)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusUnauthorized {
        t.Errorf("status=%d; want 401", rec.Code)
    }
}
```

(Reuse `newTestAuthServer` and `signupAndLogin` from `me_http_test.go` / `auth_http_test.go`.)

- [ ] **Step 4: Run + commit**

```bash
go test -run "TestListSessions" -v ./internal/relay/ 2>&1 | tail -15

git add internal/relay/auth_http.go internal/relay/me_sessions_http.go internal/relay/me_sessions_http_test.go internal/userstore/websessions.go
git commit -m "relay: GET /api/me/sessions with is_current flag"
```

---

### Task 6 — `DELETE /api/me/sessions/{id_hash}` handler

**Files:**
- Modify: `internal/relay/auth_http.go` (route registration)
- Modify: `internal/relay/me_sessions_http.go` (new handler)
- Modify: `internal/relay/me_sessions_http_test.go` (new tests)

- [ ] **Step 1: Register route — CSRF-gated**

In `auth_http.go::RegisterInto`:

```go
mux.Handle("DELETE /api/me/sessions/{id_hash}",
    RequireCSRF(a.Resolver, http.HandlerFunc(a.handleDeleteSession)))
```

- [ ] **Step 2: Handler**

Append to `internal/relay/me_sessions_http.go`:

```go
// handleDeleteSession revokes a single session owned by the caller.
// Returns 204 if a row was deleted, 404 if no matching session
// belonged to this user. Cross-user attempts are indistinguishable
// from "doesn't exist".
func (a *AuthServer) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
    p, ok := a.requireUser(w, r)
    if !ok { return }
    idHash := r.PathValue("id_hash")
    if idHash == "" {
        http.Error(w, "missing id_hash", http.StatusBadRequest)
        return
    }
    deleted, err := a.Store.DeleteUserWebSessionByIDHash(r.Context(), p.UserID, idHash)
    if err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    if !deleted {
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 3: Tests**

Append to `internal/relay/me_sessions_http_test.go`:

```go
func TestDeleteSession_OwnerDeletes_204(t *testing.T) {
    srv, store := newTestAuthServer(t)
    handler := srv.Routes()
    cookie, userID, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
    csrf := csrfTokenFor(t, handler, cookie)
    _, _ = store.CreateWebSession(context.Background(), userID, "other-device", "")
    list, _ := store.ListUserWebSessions(context.Background(), userID)
    var target string
    for _, s := range list {
        if s.UserAgent == "other-device" { target = s.IDHash }
    }
    if target == "" { t.Fatal("setup: other-device session missing") }

    req := httptest.NewRequest(http.MethodDelete, "/api/me/sessions/"+target, nil)
    req.AddCookie(cookie)
    req.Header.Set("X-CSRF-Token", csrf)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusNoContent {
        t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
    }
    after, _ := store.ListUserWebSessions(context.Background(), userID)
    if len(after) != 1 { t.Errorf("expected 1 session left, got %d", len(after)) }
}

func TestDeleteSession_OtherUserSession_404(t *testing.T) {
    srv, store := newTestAuthServer(t)
    handler := srv.Routes()
    cookieA, _, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
    csrfA := csrfTokenFor(t, handler, cookieA)

    userB, _ := store.CreateUser(context.Background(), "b@example.com", "passphrase-1234")
    _, _ = store.CreateWebSession(context.Background(), userB.ID, "ua-b", "")
    listB, _ := store.ListUserWebSessions(context.Background(), userB.ID)

    req := httptest.NewRequest(http.MethodDelete, "/api/me/sessions/"+listB[0].IDHash, nil)
    req.AddCookie(cookieA)
    req.Header.Set("X-CSRF-Token", csrfA)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusNotFound {
        t.Errorf("status=%d; want 404 (cross-user)", rec.Code)
    }
    // B's session must still exist.
    afterB, _ := store.ListUserWebSessions(context.Background(), userB.ID)
    if len(afterB) != 1 { t.Errorf("B's session lost: %+v", afterB) }
}
```

- [ ] **Step 4: Run + commit**

```bash
go test -run "TestDeleteSession" -v ./internal/relay/ 2>&1 | tail -15

git add internal/relay/auth_http.go internal/relay/me_sessions_http.go internal/relay/me_sessions_http_test.go
git commit -m "relay: DELETE /api/me/sessions/{id_hash} with owner-scoped revoke"
```

---

### Task 7 — `POST /api/me/sessions/sign-out-others` handler

**Files:**
- Modify: `internal/relay/auth_http.go`
- Modify: `internal/relay/me_sessions_http.go`
- Modify: `internal/relay/me_sessions_http_test.go`

- [ ] **Step 1: Register route — CSRF-gated**

```go
mux.Handle("POST /api/me/sessions/sign-out-others",
    RequireCSRF(a.Resolver, http.HandlerFunc(a.handleSignOutOthers)))
```

- [ ] **Step 2: Handler**

```go
// handleSignOutOthers deletes every web_session for the caller except
// the one matching the current cookie. Returns 200 + {"deleted": N}.
func (a *AuthServer) handleSignOutOthers(w http.ResponseWriter, r *http.Request) {
    p, ok := a.requireUser(w, r)
    if !ok { return }
    c, err := r.Cookie("atterm_session")
    if err != nil || c.Value == "" {
        // requireUser already authed via cookie OR api token; without a
        // cookie we can't preserve "this device", so just error out.
        http.Error(w, "current session not cookie-based", http.StatusBadRequest)
        return
    }
    currentHash := userstore.SessionHash(c.Value)
    n, err := a.Store.DeleteOtherWebSessionsForUser(r.Context(), p.UserID, currentHash)
    if err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]int64{"deleted": n})
}
```

- [ ] **Step 3: Test**

```go
func TestSignOutOthers_DeletesAllButCurrent(t *testing.T) {
    srv, store := newTestAuthServer(t)
    handler := srv.Routes()
    cookie, userID, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
    csrf := csrfTokenFor(t, handler, cookie)

    _, _ = store.CreateWebSession(context.Background(), userID, "device-2", "")
    _, _ = store.CreateWebSession(context.Background(), userID, "device-3", "")

    req := httptest.NewRequest(http.MethodPost, "/api/me/sessions/sign-out-others", nil)
    req.AddCookie(cookie)
    req.Header.Set("X-CSRF-Token", csrf)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
    }
    var resp map[string]int64
    json.NewDecoder(rec.Body).Decode(&resp)
    if resp["deleted"] != 2 { t.Errorf("deleted=%d; want 2", resp["deleted"]) }

    after, _ := store.ListUserWebSessions(context.Background(), userID)
    if len(after) != 1 { t.Errorf("expected 1 session left, got %d", len(after)) }
}
```

- [ ] **Step 4: Run + commit**

```bash
go test -run "TestSignOutOthers" -v ./internal/relay/ 2>&1 | tail -10

git add internal/relay/auth_http.go internal/relay/me_sessions_http.go internal/relay/me_sessions_http_test.go
git commit -m "relay: POST /api/me/sessions/sign-out-others"
```

---

### Task 8 — `DELETE /api/me` with password re-verify + last-admin guard

**Files:**
- Modify: `internal/relay/auth_http.go`
- Create: `internal/relay/me_delete_http.go`
- Create: `internal/relay/me_delete_http_test.go`

- [ ] **Step 1: Register route — CSRF-gated**

```go
mux.Handle("DELETE /api/me",
    RequireCSRF(a.Resolver, http.HandlerFunc(a.handleDeleteMe)))
```

- [ ] **Step 2: Handler**

Create `internal/relay/me_delete_http.go`:

```go
package relay

import (
    "encoding/json"
    "net/http"
)

// handleDeleteMe hard-deletes the calling user. Both `email` (typo
// protection) and `password` (anti-CSRF-via-stolen-cookie) are
// required. Admins who are the last remaining admin are refused with
// 409 last_admin to avoid locking the deploy out.
func (a *AuthServer) handleDeleteMe(w http.ResponseWriter, r *http.Request) {
    p, ok := a.requireUser(w, r)
    if !ok { return }

    var body struct {
        Email    string `json:"email"`
        Password string `json:"password"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        writeError(w, http.StatusBadRequest, "invalid_request")
        return
    }

    user, err := a.Store.GetUser(r.Context(), p.UserID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "internal_error")
        return
    }
    if body.Email != user.Email {
        writeError(w, http.StatusBadRequest, "email_mismatch")
        return
    }

    // Re-verify password — attacker with cookie + CSRF still needs plaintext.
    v, err := a.Store.VerifyPassword(r.Context(), user.Email, body.Password)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "internal_error")
        return
    }
    if v == nil {
        writeError(w, http.StatusUnauthorized, "password_incorrect")
        return
    }

    // Last-admin protection.
    if user.IsAdmin {
        admins, cerr := countAdmins(r.Context(), a.Store)
        if cerr != nil {
            writeError(w, http.StatusInternalServerError, "internal_error")
            return
        }
        if admins <= 1 {
            writeError(w, http.StatusConflict, "last_admin")
            return
        }
    }

    if err := a.Store.DeleteUser(r.Context(), p.UserID); err != nil {
        writeError(w, http.StatusInternalServerError, "internal_error")
        return
    }

    // Clear the session cookie so the browser stops sending it.
    setSessionCookie(w, r, "", -1)
    w.WriteHeader(http.StatusNoContent)
}
```

`countAdmins` already exists from PR A in `internal/relay/admin_http.go`. Confirm with `grep -n "func countAdmins" internal/relay/`. If it's elsewhere (e.g. exported), import as needed.

`writeError` and `setSessionCookie` are existing helpers.

- [ ] **Step 3: Tests**

Create `internal/relay/me_delete_http_test.go`:

```go
package relay

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func deleteMeReq(body any, cookie *http.Cookie, csrf string) *http.Request {
    b, _ := json.Marshal(body)
    r := httptest.NewRequest(http.MethodDelete, "/api/me", strings.NewReader(string(b)))
    r.Header.Set("Content-Type", "application/json")
    r.AddCookie(cookie)
    r.Header.Set("X-CSRF-Token", csrf)
    return r
}

func TestDeleteMe_Success(t *testing.T) {
    srv, store := newTestAuthServer(t)
    handler := srv.Routes()
    cookie, userID, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
    csrf := csrfTokenFor(t, handler, cookie)

    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, deleteMeReq(map[string]string{"email": "a@example.com", "password": "passphrase-1234"}, cookie, csrf))
    if rec.Code != http.StatusNoContent {
        t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
    }
    if _, err := store.GetUser(context.Background(), userID); err == nil {
        t.Error("user still exists after DELETE /api/me")
    }
}

func TestDeleteMe_WrongEmail_400(t *testing.T) {
    srv, store := newTestAuthServer(t)
    handler := srv.Routes()
    cookie, _, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
    csrf := csrfTokenFor(t, handler, cookie)

    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, deleteMeReq(map[string]string{"email": "other@example.com", "password": "passphrase-1234"}, cookie, csrf))
    if rec.Code != http.StatusBadRequest {
        t.Fatalf("status=%d; want 400", rec.Code)
    }
}

func TestDeleteMe_WrongPassword_401(t *testing.T) {
    srv, store := newTestAuthServer(t)
    handler := srv.Routes()
    cookie, _, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
    csrf := csrfTokenFor(t, handler, cookie)

    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, deleteMeReq(map[string]string{"email": "a@example.com", "password": "wrong"}, cookie, csrf))
    if rec.Code != http.StatusUnauthorized {
        t.Fatalf("status=%d; want 401", rec.Code)
    }
}

func TestDeleteMe_LastAdmin_409(t *testing.T) {
    srv, store := newTestAuthServer(t)
    handler := srv.Routes()
    cookie, userID, _ := signupAndLogin(t, handler, store, "admin@example.com", "passphrase-1234")
    _ = store.SetUserAdmin(context.Background(), userID, true)
    csrf := csrfTokenFor(t, handler, cookie)

    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, deleteMeReq(map[string]string{"email": "admin@example.com", "password": "passphrase-1234"}, cookie, csrf))
    if rec.Code != http.StatusConflict {
        t.Fatalf("status=%d; want 409", rec.Code)
    }
    if _, err := store.GetUser(context.Background(), userID); err != nil {
        t.Error("last admin was deleted; should have been refused")
    }
}

func TestDeleteMe_AdminButNotLast_Succeeds(t *testing.T) {
    srv, store := newTestAuthServer(t)
    handler := srv.Routes()
    cookie, userID, _ := signupAndLogin(t, handler, store, "admin@example.com", "passphrase-1234")
    _ = store.SetUserAdmin(context.Background(), userID, true)
    csrf := csrfTokenFor(t, handler, cookie)
    // Second admin so the first isn't the last.
    other, _ := store.CreateUser(context.Background(), "other@example.com", "passphrase-1234")
    _ = store.SetUserAdmin(context.Background(), other.ID, true)

    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, deleteMeReq(map[string]string{"email": "admin@example.com", "password": "passphrase-1234"}, cookie, csrf))
    if rec.Code != http.StatusNoContent {
        t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
    }
}
```

- [ ] **Step 4: Run + commit**

```bash
go test -run "TestDeleteMe" -v ./internal/relay/ 2>&1 | tail -20

git add internal/relay/auth_http.go internal/relay/me_delete_http.go internal/relay/me_delete_http_test.go
git commit -m "relay: DELETE /api/me with password reverify + last-admin guard"
```

---

## Phase 3 — Frontend: settings sub-tabs + new panels

### Task 9 — Restructure `web/settings.html` into 4 sub-tabs + 4 panels

**Files:**
- Modify: `web/settings.html`

- [ ] **Step 1: Edit the file**

Replace the body's `<div class="settings-wrap">` section with the new tab structure. Existing API Tokens + Change Password sections stay; add Signed-in devices + Danger zone panels.

```html
<body class="settings-page">
<header id="topbar"></header>

<div class="settings-wrap">
  <nav class="subtabs" aria-label="Settings sections">
    <a href="#api-tokens" data-tab="api-tokens" class="subtab active">API Tokens</a>
    <a href="#change-password" data-tab="change-password" class="subtab">Change Password</a>
    <a href="#sessions" data-tab="sessions" class="subtab">Signed-in devices</a>
    <a href="#danger" data-tab="danger" class="subtab danger-tab">Danger zone</a>
  </nav>

  <section id="panel-api-tokens" class="settings-card" data-panel="api-tokens">
    <!-- Existing API Tokens content moved INSIDE this panel container; keep all the form/list ids unchanged. -->
    <h2>API Tokens</h2>
    <ul id="token-list"><li id="token-list-empty">Loading…</li></ul>
    <form id="create-token-form" autocomplete="off">
      <label>Token name
        <div class="form-row">
          <input id="token-name" type="text" required placeholder="e.g. my-laptop" autocomplete="off" />
          <button type="submit">Create</button>
        </div>
      </label>
    </form>
    <div id="plaintext-section" hidden>
      <p>Your new token (shown once — copy it now):</p>
      <code id="plaintext-display"></code>
      <button id="copy-token-btn" type="button">Copy</button>
    </div>
  </section>

  <section id="panel-change-password" class="settings-card" data-panel="change-password" hidden>
    <h2>Change Password</h2>
    <form id="change-password-form" autocomplete="off">
      <label>Current password
        <input id="current-password" type="password" required autocomplete="current-password" />
      </label>
      <label>New password (≥ 12 chars)
        <input id="new-password" type="password" required minlength="12" autocomplete="new-password" />
      </label>
      <button type="submit">Change password</button>
      <p id="password-error" hidden></p>
    </form>
  </section>

  <section id="panel-sessions" class="settings-card" data-panel="sessions" hidden>
    <h2>Signed-in devices</h2>
    <p class="muted">Each row is a browser or PWA where this account is signed in.</p>
    <ul id="sessions-list"><li id="sessions-empty">Loading…</li></ul>
    <button id="sign-out-others" class="btn-danger" type="button">Sign out everywhere except this device</button>
    <p id="sessions-error" hidden></p>
  </section>

  <section id="panel-danger" class="settings-card danger-zone" data-panel="danger" hidden>
    <h2>Danger zone</h2>
    <p>Permanently delete this account. This cannot be undone. API tokens, web sessions, and account data are removed. Invitations you've consumed stay (their "consumed by" field is cleared).</p>
    <form id="delete-account-form" autocomplete="off">
      <label>Confirm by typing your full email
        <input id="delete-email" type="email" required autocomplete="off" />
      </label>
      <label>Current password
        <input id="delete-password" type="password" required autocomplete="current-password" />
      </label>
      <button type="submit" class="btn-danger">Delete my account</button>
      <p id="delete-error" hidden></p>
    </form>
  </section>
</div>

<p class="page-version" id="version">version dev</p>
<script type="module" src="./layout.js"></script>
<script type="module" src="./settings.js"></script>
<script type="module" src="./settings-sessions.js"></script>
<script type="module" src="./settings-danger.js"></script>
</body>
</html>
```

(The `<p class="page-version">` is the existing version label that was already in settings.html — keep it. layout.js will populate it.)

- [ ] **Step 2: Manual verify**

```bash
grep -nE 'data-panel="(api-tokens|change-password|sessions|danger)"' web/settings.html
```

Expected: 4 matches.

- [ ] **Step 3: Commit**

```bash
git add web/settings.html
git commit -m "web(settings): 4-tab structure (api-tokens / change-password / sessions / danger)"
```

(JS to actually switch tabs lands in Task 11.)

---

### Task 10 — `.subtabs` + `.danger-zone` styling in `web/style.css`

**Files:**
- Modify: `web/style.css`

- [ ] **Step 1: Append styles**

Append to `web/style.css`:

```css
/* Settings sub-tabs */
.subtabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 16px;
}
.subtab {
  padding: 8px 14px;
  color: var(--fg-mute, var(--fg-dim));
  text-decoration: none;
  border-bottom: 2px solid transparent;
  font-size: 13px;
}
.subtab.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
}
.subtab:hover {
  color: var(--fg);
}
.subtab.danger-tab {
  margin-left: auto;
  color: var(--bad);
}
.subtab.danger-tab.active {
  color: var(--bad);
  border-bottom-color: var(--bad);
}

/* Danger zone visual treatment */
.danger-zone {
  border-color: var(--bad);
}
.btn-danger {
  background: var(--bad);
  color: white;
  border: none;
  padding: 8px 14px;
  border-radius: 6px;
  cursor: pointer;
}
.btn-danger:hover {
  opacity: 0.9;
}

/* Sessions list */
#sessions-list {
  list-style: none;
  padding: 0;
}
#sessions-list li {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px;
  border-bottom: 1px solid var(--border-mute, var(--border));
}
#sessions-list .session-ua {
  font-weight: 500;
}
#sessions-list .session-meta {
  font-size: 11px;
  color: var(--fg-mute, var(--fg-dim));
}
#sessions-list .session-current {
  color: var(--good);
  font-size: 11px;
}
```

The `--fg-mute`, `--border-mute`, `--bad` tokens may or may not exist yet (spec mentions them in PR E). Use fallbacks (`var(--fg-mute, var(--fg-dim))`) so this works whether or not the new tokens have been added.

- [ ] **Step 2: Commit**

```bash
git add web/style.css
git commit -m "web(css): subtabs + danger-zone + sessions-list styling"
```

---

### Task 11 — `web/settings.js`: tab switching + hash routing

**Files:**
- Modify: `web/settings.js`

- [ ] **Step 1: Add tab logic at the top of settings.js**

```js
// Tab switching: each anchor in .subtabs has data-tab matching a
// data-panel on a <section>. URL hash sync makes deep links work
// (#sessions opens directly to Signed-in devices).

const TABS = ["api-tokens", "change-password", "sessions", "danger"];

function showTab(name) {
    if (!TABS.includes(name)) name = "api-tokens";
    for (const t of TABS) {
        const link = document.querySelector(`.subtab[data-tab="${t}"]`);
        const panel = document.querySelector(`[data-panel="${t}"]`);
        if (!link || !panel) continue;
        if (t === name) {
            link.classList.add("active");
            panel.hidden = false;
        } else {
            link.classList.remove("active");
            panel.hidden = true;
        }
    }
}

function activeFromHash() {
    const h = (location.hash || "").replace(/^#/, "");
    return TABS.includes(h) ? h : "api-tokens";
}

document.addEventListener("DOMContentLoaded", () => {
    showTab(activeFromHash());
});
window.addEventListener("hashchange", () => {
    showTab(activeFromHash());
});

// Click handlers: anchor's default behavior already updates the hash,
// which triggers hashchange above. Nothing else needed here.
```

Important: keep all the existing API Tokens + Change Password handlers untouched. They reference `#create-token-form`, `#change-password-form`, etc. — those ids are still present, just inside a panel container now.

- [ ] **Step 2: Test interactively**

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL=you@example.com \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web --dev-insecure
```

Log in, visit `/settings.html`:
- API Tokens panel visible by default
- Click "Change Password" → URL becomes `/settings.html#change-password`, that panel shows
- Click each tab → only one panel visible at a time
- Reload `/settings.html#sessions` → Signed-in devices is active immediately

- [ ] **Step 3: Commit**

```bash
git add web/settings.js
git commit -m "web(settings): tab switching + URL hash routing"
```

---

### Task 12 — `web/settings-sessions.js` (Signed-in devices panel logic)

**Files:**
- Create: `web/settings-sessions.js`

- [ ] **Step 1: Implement**

```js
// Settings → Signed-in devices: list, revoke single, sign-out-others.
//
// Loaded as a separate module so the API Tokens / Change Password
// panel logic stays in settings.js. All three modules co-load and
// access the same DOM ids without coordination.

import { authFetch } from "./auth.js";

const listEl = document.getElementById("sessions-list");
const errEl = document.getElementById("sessions-error");
const signOutOthersBtn = document.getElementById("sign-out-others");

function fmtDate(ms) {
    if (!ms) return "";
    return new Date(ms).toLocaleString();
}

function fmtUA(ua) {
    if (!ua) return "Unknown device";
    // Coarse simplification — full UA strings are noisy.
    if (ua.includes("Firefox")) return "Firefox";
    if (ua.includes("Edg/")) return "Edge";
    if (ua.includes("Chrome")) return "Chrome";
    if (ua.includes("Safari")) return "Safari";
    return ua.length > 40 ? ua.slice(0, 40) + "…" : ua;
}

async function loadSessions() {
    if (!listEl) return;
    try {
        const res = await authFetch("/api/me/sessions");
        if (!res.ok) throw new Error("HTTP " + res.status);
        const rows = await res.json();
        if (!rows || rows.length === 0) {
            listEl.innerHTML = '<li id="sessions-empty">No active sessions.</li>';
            return;
        }
        listEl.innerHTML = "";
        for (const row of rows) {
            const li = document.createElement("li");
            const ua = document.createElement("div");
            ua.className = "session-ua";
            ua.textContent = fmtUA(row.user_agent) + (row.is_current ? "  (this device)" : "");
            const meta = document.createElement("div");
            meta.className = "session-meta";
            meta.textContent = `signed in ${fmtDate(row.created_at)} · ${row.ip_prefix || "ip unknown"}`;
            li.appendChild(ua);
            li.appendChild(meta);
            if (!row.is_current) {
                const btn = document.createElement("button");
                btn.className = "btn-danger";
                btn.textContent = "Revoke";
                btn.addEventListener("click", () => revokeSession(row.id_hash, btn));
                li.appendChild(btn);
            } else {
                const tag = document.createElement("span");
                tag.className = "session-current";
                tag.textContent = "current";
                li.appendChild(tag);
            }
            listEl.appendChild(li);
        }
    } catch (e) {
        listEl.innerHTML = "";
        showErr("Failed to load sessions: " + e.message);
    }
}

async function revokeSession(idHash, btn) {
    btn.disabled = true;
    try {
        const res = await authFetch(`/api/me/sessions/${encodeURIComponent(idHash)}`, { method: "DELETE" });
        if (res.status !== 204) throw new Error("HTTP " + res.status);
        await loadSessions();
    } catch (e) {
        btn.disabled = false;
        showErr("Revoke failed: " + e.message);
    }
}

async function signOutOthers() {
    if (!signOutOthersBtn) return;
    if (!confirm("Sign out everywhere except this device?")) return;
    signOutOthersBtn.disabled = true;
    try {
        const res = await authFetch("/api/me/sessions/sign-out-others", { method: "POST" });
        if (!res.ok) throw new Error("HTTP " + res.status);
        await loadSessions();
    } catch (e) {
        showErr("Sign-out-others failed: " + e.message);
    } finally {
        signOutOthersBtn.disabled = false;
    }
}

function showErr(msg) {
    if (!errEl) return;
    errEl.hidden = false;
    errEl.textContent = msg;
}

// Load when the Sessions tab is first shown — also on initial page
// load if that's the active tab. Cheapest: just fetch on module init.
loadSessions();

if (signOutOthersBtn) {
    signOutOthersBtn.addEventListener("click", signOutOthers);
}

// Refresh when the user switches into the Sessions tab.
window.addEventListener("hashchange", () => {
    if (location.hash === "#sessions") loadSessions();
});
```

- [ ] **Step 2: Manual smoke**

Restart the relay, visit `/settings.html#sessions`, verify:
- Your current session is listed with "(this device)" tag and "current" badge
- If you log in from another browser, refresh the page → it appears with a Revoke button
- Click Revoke on the other browser → it disappears from the list
- Click "Sign out everywhere except this device" → other sessions vanish; you stay signed in

- [ ] **Step 3: Commit**

```bash
git add web/settings-sessions.js
git commit -m "web(settings): Signed-in devices panel — list, revoke, sign-out-others"
```

---

### Task 13 — `web/settings-danger.js` (Danger zone panel logic)

**Files:**
- Create: `web/settings-danger.js`

- [ ] **Step 1: Implement**

```js
// Settings → Danger zone: hard-delete account. UI requires email
// match (typo-protection) AND current password (anti-CSRF defense if
// the cookie is somehow stolen). Server also enforces both checks
// plus the last-admin guard.

import { authFetch } from "./auth.js";

const form = document.getElementById("delete-account-form");
const emailEl = document.getElementById("delete-email");
const pwdEl = document.getElementById("delete-password");
const errEl = document.getElementById("delete-error");

function showErr(msg) {
    if (!errEl) return;
    errEl.hidden = false;
    errEl.textContent = msg;
}

if (form) {
    form.addEventListener("submit", async (e) => {
        e.preventDefault();
        errEl.hidden = true;
        if (!confirm("Permanently delete this account? This cannot be undone.")) return;
        try {
            const res = await authFetch("/api/me", {
                method: "DELETE",
                body: JSON.stringify({ email: emailEl.value.trim(), password: pwdEl.value }),
            });
            if (res.status === 204) {
                // Cookie was cleared by the server. Send the user back to
                // the login page; account is gone.
                location.assign("/login.html");
                return;
            }
            let msg = `Delete failed (status ${res.status})`;
            try {
                const body = await res.json();
                if (body.error === "email_mismatch") msg = "Email doesn't match — type your exact email.";
                else if (body.error === "password_incorrect") msg = "Password is incorrect.";
                else if (body.error === "last_admin") msg = "You're the last admin — promote another user first.";
            } catch (_) {}
            showErr(msg);
        } catch (e) {
            showErr("Network error: " + e.message);
        }
    });
}
```

- [ ] **Step 2: Manual smoke**

Create a throw-away user (sign up via an invite), log in as them, visit `/settings.html#danger`:
- Type wrong email → "Email doesn't match — type your exact email."
- Type right email + wrong password → "Password is incorrect."
- Type both correctly → confirm dialog → submit → redirected to /login.html; user gone
- Try the same as the bootstrap admin (only admin) → should fail with "You're the last admin — promote another user first."

- [ ] **Step 3: Commit**

```bash
git add web/settings-danger.js
git commit -m "web(settings): Danger zone — delete account with email + password confirm"
```

---

## Phase 4 — Test polish + sw cache

### Task 14 — Update `web/settings.test.mjs` for the new structure

**Files:**
- Modify: `web/settings.test.mjs`

- [ ] **Step 1: Add tests for the new panels**

Append to `web/settings.test.mjs`:

```js
test("settings.html has all 4 sub-tab anchors with data-tab", () => {
    const html = readFileSync("web/settings.html", "utf8");
    for (const tab of ["api-tokens", "change-password", "sessions", "danger"]) {
        const re = new RegExp(`<a [^>]*data-tab="${tab}"`);
        assert.match(html, re);
    }
});

test("settings.html has all 4 panels with data-panel", () => {
    const html = readFileSync("web/settings.html", "utf8");
    for (const panel of ["api-tokens", "change-password", "sessions", "danger"]) {
        const re = new RegExp(`<section [^>]*data-panel="${panel}"`);
        assert.match(html, re);
    }
});

test("settings.html includes settings-sessions.js + settings-danger.js", () => {
    const html = readFileSync("web/settings.html", "utf8");
    assert.match(html, /src="\.\/?settings-sessions\.js"/);
    assert.match(html, /src="\.\/?settings-danger\.js"/);
});

test("settings-sessions.js calls /api/me/sessions", () => {
    const js = readFileSync("web/settings-sessions.js", "utf8");
    assert.match(js, /\/api\/me\/sessions/);
    assert.match(js, /sign-out-others/);
});

test("settings-danger.js sends DELETE /api/me with email + password", () => {
    const js = readFileSync("web/settings-danger.js", "utf8");
    assert.match(js, /authFetch\("\/api\/me",[^)]*method:\s*"DELETE"/);
    assert.match(js, /\bemail\b/);
    assert.match(js, /\bpassword\b/);
});
```

- [ ] **Step 2: Run + commit**

```bash
node --test web/settings.test.mjs 2>&1 | tail -10

git add web/settings.test.mjs
git commit -m "web(test): settings tab structure + new module wiring"
```

---

### Task 15 — Bump sw cache + add new JS files to ASSETS

**Files:**
- Modify: `web/sw.js`

- [ ] **Step 1: Add new entries**

```js
const ASSETS = [
  "./",
  "./app-core.js",
  "./app.js",
  "./layout.js",
  "./login.js",
  "./signup.js",
  "./settings.js",
  "./settings-danger.js",
  "./settings-sessions.js",
  "./style.css",
  ...
];
```

Alphabetical within the cluster of settings-related files.

- [ ] **Step 2: Run sw-cache-bump test; grab new hash**

```bash
node --test web/sw-cache-bump.test.mjs 2>&1 | grep -A1 "CACHE = "
```

- [ ] **Step 3: Paste new hash into `web/sw.js`**

```js
const CACHE = "at-term-web-<paste>";
```

- [ ] **Step 4: Full test sweep**

```bash
node --test web/*.test.mjs 2>&1 | tail -8
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add web/sw.js
git commit -m "web(sw): precache settings-sessions / settings-danger; hash bump"
```

---

### Task 16 — Full sweep + ship `v0.1.75`

- [ ] **Step 1: All Go tests**

```bash
go test -count=1 -timeout 120s ./... 2>&1 | tail -10
```

Expected: every package OK.

- [ ] **Step 2: All Node web tests**

```bash
node --test web/*.test.mjs 2>&1 | tail -8
```

Expected: 0 failures.

- [ ] **Step 3: Manual end-to-end smoke**

Cover: list sessions, revoke a session, sign-out-others, delete a non-admin user, last-admin block.

- [ ] **Step 4: Push, PR, merge, tag**

```bash
git push -u origin feat/settings-redesign
gh pr create --title "feat(web): settings 4-tab redesign + me-sessions + delete-account APIs" --body "$(cat <<'EOF'
## Summary

PR C of the web UI redesign (spec: docs/superpowers/specs/2026-05-17-web-ui-redesign-design.md).

**Settings page**: now a 4-tab layout — API Tokens / Change Password / **Signed-in devices** / **Danger zone**. Tabs are URL-hash-routable (#sessions, #danger, etc.) so deep links work.

**New user-facing APIs** (CSRF-gated under /api/me/*):
- GET /api/me/sessions — list current user's web_sessions with is_current flag
- DELETE /api/me/sessions/{id_hash} — revoke a single session (owner-scoped: cross-user attempts 404)
- POST /api/me/sessions/sign-out-others — drop every session except the current cookie's
- DELETE /api/me — hard-delete account; requires email match + password re-verify; refuses when caller is the last admin (409 last_admin)

**Store**: 4 new methods on Store (ListUserWebSessions / DeleteUserWebSessionByIDHash / DeleteOtherWebSessionsForUser / DeleteUser). DeleteUser nullifies invitations.consumed_by before dropping the user (FK has no cascade for that column by design — it's a history field).

Executed via subagent-driven-development with spec + code-quality reviews between each commit.

## Test plan

- [x] go test ./... — all packages OK
- [x] node --test web/*.test.mjs — all PASS
- [ ] After deploy: navigate /settings.html — 4 tabs visible
- [ ] /settings.html#sessions lists current device with "current" badge
- [ ] Revoke a session from another browser — disappears from list
- [ ] Sign out everywhere else — confirm dialog → other devices vanish, this stays signed in
- [ ] Delete a non-admin user via the form — typo-protection + password re-verify both fire
- [ ] As the only admin, delete-account is refused with "You're the last admin"
EOF
)"

NUM=<the PR number>
gh pr merge $NUM --squash
git fetch origin main
SHA=$(gh pr view $NUM --json mergeCommit -q .mergeCommit.oid)
git tag v0.1.75 $SHA
git push origin v0.1.75
git push origin --delete feat/settings-redesign
gh run list --limit 3
```

- [ ] **Step 5: Confirm CI green**

Watch the Release workflow at https://github.com/attson/actions for v0.1.75.

---

## Done Criteria

- All 16 tasks complete with green commits.
- All Go tests pass, all 119+ web tests pass.
- v0.1.75 tag pushed; Release workflow succeeded.
- Settings page shows 4 tabs; each panel works end-to-end against the new APIs.
- PR D (admin static UI) can be written.

## Out of Scope

- Admin UI itself (web/admin/) — PR D
- Design tokens audit / login + signup polish — PR E
- Push notifications panel — staying in index topbar bell
