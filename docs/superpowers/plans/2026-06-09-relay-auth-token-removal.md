# Relay Auth Token Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace relay's three credential systems (`atk_` API tokens, `cfg.Token` global share-secret, `atterm_session` HttpOnly cookies) with a single rule — every client logs in with email + password, receives a `session_token`, and carries it as `Authorization: Bearer <token>` (HTTP) or `Sec-WebSocket-Protocol: atterm-token.<token>` (browser WS). Pairing codes survive but now mint session tokens instead of API tokens. Operator drops the old database on redeploy — no migration code.

**Architecture:** A new `requireSession` middleware in `internal/relay/auth.go` replaces every `authorize*` function. `web_sessions` table is renamed `sessions` and reused as the single session store for all three clients. The `api_tokens` table, `users.csrf_secret` column, `pairing_tokens.source` column, `Config.Token` / `ReadOnlyTokens` / `ReadOnlyTokenHashes` fields, `internal/relay/csrfmw.go`, `cmd/atterm-agent/`, and `internal/agent/` are deleted outright. Browsers reach the WS endpoints by passing the session token in `Sec-WebSocket-Protocol`, since the spec forbids setting `Authorization` on WS upgrades.

**Tech Stack:** Go (relay, userstore), Wails (desktop), Vue 3 + Pinia (web + Capacitor frontend), Capacitor (iOS), SQLite (relay store).

**Spec:** `docs/superpowers/specs/2026-06-09-relay-auth-token-removal-design.md`.

---

## File structure

Plan organised into 5 phases that map 1-to-1 onto the 5 PRs from the spec. Phases must merge in order — every later phase depends on the API shape introduced in Phase 1.

**Phase 1 — Backend auth rewrite (PR-1)**
- `internal/userstore/migrations/0001_init.sql` (rewrite): new fresh-deploy schema. No `api_tokens`, no `csrf_secret`, no `pairing_tokens.source`; `web_sessions` renamed `sessions`.
- `internal/userstore/users.go` (modify): drop `csrfSecret` field + `CSRFSecret()` method; adjust `VerifyPassword`.
- `internal/userstore/sessions.go` (rename from `websessions.go`): rename type and methods; add `LookupSession` that joins user.
- `internal/userstore/pairing.go` (modify): drop `source` parameter; `ConsumePairingToken` returns user only (no token minting).
- `internal/userstore/apitokens.go` (delete).
- `internal/relay/auth.go` (rewrite): delete the `authorize*` family; add `requireSession` middleware; keep `tokenFromRequest` and `tokenFromSubprotocol`.
- `internal/relay/auth_http.go` (modify): `handleLogin` / `handleSignup` / `handleSetup` return `{session_token, expires_at, user}` in body, no `Set-Cookie`; `handleLogout` reads Bearer; delete `/api/me/tokens` handlers; delete `CSRFToken(...)` call.
- `internal/relay/pair_http.go` (modify): `handlePairConsume` returns `{session_token, expires_at, user, relay_url}`.
- `internal/relay/server.go` (modify): delete `Config.Token` / `ReadOnlyTokens` / `ReadOnlyTokenHashes`; wrap five routes with `requireSession`; delete cookie helpers.
- `internal/relay/csrfmw.go` (delete) and `csrfmw_test.go` (delete).
- `internal/relay/me_*_http.go` (modify): re-mount under `requireSession`.
- `cmd/atterm-relay/main.go` (modify): delete `--token` / `--readonly-tokens` flags + matching env vars; extend bootstrap to optionally emit a one-time session token.
- `internal/relay/bootstrap_admin.go` (modify): return a fresh session token alongside the user.
- Tests: rewrite `internal/relay/auth_test.go`, `auth_http_test.go`, `pair_http_test.go`; remove cases that assert on `api_token` / CSRF / share-secret in remaining `*_test.go` files.

**Phase 2 — Delete `cmd/atterm-agent` (PR-2)**
- `cmd/atterm-agent/` (delete directory).
- `internal/agent/` (delete directory).
- `AGENTS.md`, `README.md`, `scripts/dev.sh`, `Dockerfile.relay` (modify): remove every reference.

**Phase 3 — Desktop client (PR-3)**
- `desktop/config.go` (modify): `RelayToken` → `RelaySessionToken`.
- `desktop/uplink.go` (modify): field rename in struct.
- `desktop/app.go` (modify): add `LoginRemoteRelay` Wails method; rename existing token-typed args.
- `desktop/relay_host.go` (modify): stop random share-secret; on first start call bootstrap that returns a session token, write it to config.
- `desktop/frontend/src/...` (modify): add remote-relay login dialog; existing settings UI replaces "paste token" with "email + password"; renamed config fields.

**Phase 4 — Web client (PR-4)**
- `web/src/shared/api/client.ts` (modify): remove web/mobile branch; always read `session_token` from storage; always send `Authorization: Bearer`; `credentials: 'omit'`; delete CSRF cache.
- `web/src/shared/api/auth.ts` (modify): login persists `session_token` to localStorage; logout clears it.
- `web/src/shared/api/relay-config.ts` (modify): add `session_token` field to stored config.
- `web/src/shared/...` (modify): 401 interceptor clears storage and redirects to login.
- Browser WebSocket sites (modify): pass `Sec-WebSocket-Protocol: atterm-token.<token>` when opening WS.

**Phase 5 — Mobile Capacitor (PR-5)**
- `desktop/frontend/src/platform/capacitor.ts` (modify): `consumePairing` parses `session_token` / `expires_at`; persists to Keychain under the new key.
- `desktop/frontend/src/platform/secureStorage.ts` (modify): rename storage key.
- `desktop/frontend/src/mobile/PairingConsume.vue` (modify): consume new response shape.

---

## Phase 1 — Backend auth rewrite (PR-1)

This phase is the contract change. Until it lands, every other phase is blocked. Each task ends in one commit; all commits go onto a single branch `feat/relay-session-token-backend`.

### Task 1.1: Rewrite fresh-deploy SQL schema

Replace `0001_init.sql` with the new target schema. Older deployments are out of scope (operator drops the DB and redeploys per the spec).

**Files:**
- Modify: `internal/userstore/migrations/0001_init.sql`
- Test: `internal/userstore/migrations_test.go` (if a schema assertion test exists; otherwise covered by downstream store tests)

- [ ] **Step 1: Read current migration to see what columns exist now**

```bash
cd /Users/attson/code/github.com.attson/atterm && cat internal/userstore/migrations/0001_init.sql
```

- [ ] **Step 2: Write the new schema**

Replace `internal/userstore/migrations/0001_init.sql` with:

```sql
-- 0001_init.sql — fresh-deploy schema for relay auth (single session token).
-- Operator must drop any pre-existing database before applying.

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    disabled_at   INTEGER
);

CREATE TABLE sessions (
    id_hash      TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT '',
    ip_prefix    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);

CREATE TABLE pairing_tokens (
    token_hash   TEXT PRIMARY KEY,
    prefix       TEXT NOT NULL,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    consumed_at  INTEGER
);
CREATE INDEX pairing_tokens_user_idx ON pairing_tokens(user_id);

CREATE TABLE invitations (
    code       TEXT PRIMARY KEY,
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    consumed_at INTEGER,
    consumed_by TEXT REFERENCES users(id) ON DELETE SET NULL
);
```

- [ ] **Step 3: Verify migrations dir is clean**

```bash
ls /Users/attson/code/github.com.attson/atterm/internal/userstore/migrations/
```

Expected: only `0001_init.sql`. If any other file exists, delete it:

```bash
cd /Users/attson/code/github.com.attson/atterm && ls internal/userstore/migrations/ | grep -v '^0001_init.sql$' | xargs -I{} rm internal/userstore/migrations/{}
```

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add internal/userstore/migrations/
git commit -m "userstore: rewrite 0001_init.sql for session-token schema (no api_tokens, no csrf_secret, rename web_sessions → sessions)"
```

---

### Task 1.2: Trim `users.go` — drop `csrfSecret`

**Files:**
- Modify: `internal/userstore/users.go` (User struct ~lines 25-34; `VerifyPassword` ~lines 137-162; `CreateUser` and any helper that reads/writes `csrf_secret`)

- [ ] **Step 1: Write the failing test**

Add to `internal/userstore/users_test.go` (create the file if it doesn't exist; if it does, append):

```go
func TestUser_HasNoCSRFSecretField(t *testing.T) {
    // Reflection-free check: a User instance should compile and serialise
    // without csrfSecret being part of its public or private API.
    u := User{ID: "u_1", Email: "x@example.com"}
    _ = u
    // The presence of the test is the test — if csrf_secret survives in
    // the User struct, downstream callers (auth_http.go) still reference
    // it and the package won't compile.
}
```

- [ ] **Step 2: Run, expect compile failure once `csrf_secret` is removed elsewhere**

Skip — this test is structural. Run after Step 3.

- [ ] **Step 3: Modify `User` struct**

In `internal/userstore/users.go`:

- Delete the `csrfSecret []byte` field from the `User` struct (currently around line 30).
- Delete the `CSRFSecret() []byte` accessor method.
- In `VerifyPassword`, remove every reference to `csrfSecret` (no longer selected from DB, no longer returned to caller).
- In `CreateUser`, drop any `csrf_secret` insert column.

- [ ] **Step 4: Update callers**

Use `grep` to find every remaining reference:

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'CSRFSecret\|csrf_secret\|csrfSecret' internal/ cmd/ desktop/ web/
```

Remove every hit — they will all be in code being deleted in later tasks anyway, but any non-deleted reference here means we missed a file.

- [ ] **Step 5: Compile**

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./internal/userstore/...
```

Expected: success.

- [ ] **Step 6: Run userstore tests**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/userstore/ -run TestUser_ -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add internal/userstore/users.go internal/userstore/users_test.go
git commit -m "userstore: drop csrf_secret field from User"
```

---

### Task 1.3: Rename `WebSession` → `Session` + add `LookupSession`

`LookupSession` is the new join used by `requireSession` middleware: token → row → user.

**Files:**
- Rename: `internal/userstore/websessions.go` → `internal/userstore/sessions.go`
- Modify: every reference in `internal/relay/` and tests
- Test: `internal/userstore/sessions_test.go` (rename from `websessions_test.go` if present)

- [ ] **Step 1: Write the failing test**

Create or replace `internal/userstore/sessions_test.go`:

```go
package userstore

import (
    "context"
    "testing"
    "time"
)

func TestLookupSession_HitReturnsUser(t *testing.T) {
    st := newTestStore(t) // helper from existing test files; create if missing
    ctx := context.Background()
    u, _, err := st.CreateUser(ctx, "alice@example.com", "Correct-Horse-Battery-Staple-1!")
    if err != nil {
        t.Fatalf("CreateUser: %v", err)
    }
    tok, sess, err := st.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", 24*time.Hour)
    if err != nil {
        t.Fatalf("CreateSession: %v", err)
    }
    if tok == "" {
        t.Fatal("expected plaintext session token")
    }
    got, gotUser, err := st.LookupSession(ctx, tok)
    if err != nil {
        t.Fatalf("LookupSession: %v", err)
    }
    if got.IDHash != sess.IDHash {
        t.Fatalf("session mismatch: got %q want %q", got.IDHash, sess.IDHash)
    }
    if gotUser.ID != u.ID {
        t.Fatalf("user mismatch: got %q want %q", gotUser.ID, u.ID)
    }
}

func TestLookupSession_MissReturnsNotFound(t *testing.T) {
    st := newTestStore(t)
    ctx := context.Background()
    _, _, err := st.LookupSession(ctx, "ses_garbage")
    if err == nil {
        t.Fatal("expected error for missing session")
    }
}

func TestLookupSession_ExpiredReturnsNotFound(t *testing.T) {
    st := newTestStore(t)
    ctx := context.Background()
    u, _, _ := st.CreateUser(ctx, "x@example.com", "Correct-Horse-Battery-Staple-1!")
    tok, _, _ := st.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", -1*time.Second)
    if _, _, err := st.LookupSession(ctx, tok); err == nil {
        t.Fatal("expected error for expired session")
    }
}
```

- [ ] **Step 2: Run, expect compile failure (LookupSession doesn't exist yet)**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/userstore/ -run TestLookupSession_ -v
```

Expected: compile error referencing `LookupSession`, `CreateSession`.

- [ ] **Step 3: Rename file and types**

```bash
cd /Users/attson/code/github.com.attson/atterm && git mv internal/userstore/websessions.go internal/userstore/sessions.go
```

In `internal/userstore/sessions.go`:

- Rename type `UserWebSession` → `Session`.
- Rename `CreateWebSession` → `CreateSession` (signature unchanged).
- Rename `LookupWebSession` → `LookupSession` (signature unchanged: returns `(*Session, *User, error)`).
- Rename `DeleteWebSession` → `DeleteSession`.
- Rename `ListWebSessions` → `ListSessions`.
- Replace every `web_sessions` SQL reference with `sessions`.

If `LookupWebSession` currently returns only `(*UserWebSession, error)`, extend the signature to also return `*User` by JOINing `users`:

```sql
SELECT s.id_hash, s.user_id, s.created_at, s.expires_at, s.last_seen_at, s.user_agent, s.ip_prefix,
       u.id, u.email, u.is_admin, u.created_at, u.disabled_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.id_hash = ? AND s.expires_at > ?
```

- [ ] **Step 4: Update every relay caller**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'WebSession\|web_sessions\|CreateWebSession\|LookupWebSession\|DeleteWebSession\|ListWebSessions' internal/ cmd/
```

For each hit, rename to the new symbol. Note: callers under `internal/relay/` that today do `LookupWebSession` followed by a separate `GetUser` can collapse into the new `LookupSession`.

- [ ] **Step 5: Build + test**

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./... && go test ./internal/userstore/ -run TestLookupSession_ -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A internal/userstore/ internal/relay/
git commit -m "userstore: rename WebSession → Session; LookupSession returns user via JOIN"
```

---

### Task 1.4: Delete `APIToken` store + simplify `pairing.go`

**Files:**
- Delete: `internal/userstore/apitokens.go`
- Delete: `internal/userstore/apitokens_test.go` (if present)
- Modify: `internal/userstore/pairing.go` (drop `source` parameter and column reference)

- [ ] **Step 1: Delete the file**

```bash
cd /Users/attson/code/github.com.attson/atterm && rm internal/userstore/apitokens.go
```

If a corresponding `apitokens_test.go` exists:

```bash
cd /Users/attson/code/github.com.attson/atterm && rm internal/userstore/apitokens_test.go 2>/dev/null || true
```

- [ ] **Step 2: Simplify `pairing.go`**

Open `internal/userstore/pairing.go`. The current `ConsumePairingToken` (~line 114) calls `CreateAPITokenWithSource(..., "pairing")`. Rewrite it so it:

- Marks the pairing token consumed.
- Returns `(*User, error)` only — no token minting.

Example final shape:

```go
// ConsumePairingToken validates a pair code, marks it consumed, and returns
// the owning user. The caller is responsible for minting a session token.
func (s *Store) ConsumePairingToken(ctx context.Context, plain string) (*User, error) {
    h := hashToken(plain)
    var userID string
    var expiresAt int64
    var consumedAt sql.NullInt64
    err := s.db.QueryRowContext(ctx, `
        SELECT user_id, expires_at, consumed_at
        FROM pairing_tokens WHERE token_hash = ?`, h).
        Scan(&userID, &expiresAt, &consumedAt)
    if err == sql.ErrNoRows {
        return nil, ErrPairingNotFound
    }
    if err != nil {
        return nil, err
    }
    if consumedAt.Valid {
        return nil, ErrPairingConsumed
    }
    if time.Now().Unix() > expiresAt {
        return nil, ErrPairingExpired
    }
    res, err := s.db.ExecContext(ctx, `
        UPDATE pairing_tokens SET consumed_at = ?
        WHERE token_hash = ? AND consumed_at IS NULL`,
        time.Now().Unix(), h)
    if err != nil {
        return nil, err
    }
    n, _ := res.RowsAffected()
    if n == 0 {
        return nil, ErrPairingConsumed
    }
    return s.GetUser(ctx, userID)
}
```

Also delete any `source` column reference from `CreatePairingToken` and the struct definition.

- [ ] **Step 3: Update relay caller (will fully rewire in Task 1.10)**

Don't touch `internal/relay/pair_http.go` yet. Build will break — that's fine; Task 1.10 fixes it.

For now, run just the userstore unit tests:

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/userstore/ -v
```

Expected: PASS (after creating fixture data accordingly in existing tests).

- [ ] **Step 4: Commit (build broken, will be fixed in Task 1.10)**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A internal/userstore/
git commit -m "userstore: delete api_tokens store; ConsumePairingToken returns user only"
```

Note: this is one of two intentional "broken middle" commits in the branch (the other is Task 1.6). Reviewers should treat the full branch as the unit; only the branch tip needs `go build ./...` to succeed.

---

### Task 1.5: Rewrite `internal/relay/auth.go` — delete `authorize*`, add `requireSession`

**Files:**
- Rewrite: `internal/relay/auth.go`
- Test: `internal/relay/auth_test.go` (rewrite)

- [ ] **Step 1: Write the failing test**

Replace `internal/relay/auth_test.go` with:

```go
package relay

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/attson/atterm/internal/userstore"
)

func TestRequireSession_Bearer_Hit(t *testing.T) {
    s, tok := serverWithSession(t)
    var called bool
    h := s.requireSession(func(w http.ResponseWriter, r *http.Request) {
        called = true
        if u, ok := UserFromContext(r.Context()); !ok || u == nil {
            t.Fatal("expected user in context")
        }
        w.WriteHeader(http.StatusNoContent)
    })
    req := httptest.NewRequest("GET", "/x", nil)
    req.Header.Set("Authorization", "Bearer "+tok)
    rec := httptest.NewRecorder()
    h(rec, req)
    if !called {
        t.Fatal("handler not called")
    }
    if rec.Code != http.StatusNoContent {
        t.Fatalf("status: got %d want 204", rec.Code)
    }
}

func TestRequireSession_Subprotocol_Hit(t *testing.T) {
    s, tok := serverWithSession(t)
    h := s.requireSession(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusNoContent)
    })
    req := httptest.NewRequest("GET", "/x", nil)
    req.Header.Set("Sec-WebSocket-Protocol", "atterm-token."+tok)
    rec := httptest.NewRecorder()
    h(rec, req)
    if rec.Code != http.StatusNoContent {
        t.Fatalf("status: got %d want 204", rec.Code)
    }
}

func TestRequireSession_NoToken_401(t *testing.T) {
    s, _ := serverWithSession(t)
    h := s.requireSession(func(w http.ResponseWriter, r *http.Request) {
        t.Fatal("handler must not be called")
    })
    req := httptest.NewRequest("GET", "/x", nil)
    rec := httptest.NewRecorder()
    h(rec, req)
    if rec.Code != http.StatusUnauthorized {
        t.Fatalf("status: got %d want 401", rec.Code)
    }
}

func TestRequireSession_BadToken_401(t *testing.T) {
    s, _ := serverWithSession(t)
    h := s.requireSession(func(w http.ResponseWriter, r *http.Request) {
        t.Fatal("handler must not be called")
    })
    req := httptest.NewRequest("GET", "/x", nil)
    req.Header.Set("Authorization", "Bearer ses_not_a_real_token")
    rec := httptest.NewRecorder()
    h(rec, req)
    if rec.Code != http.StatusUnauthorized {
        t.Fatalf("status: got %d want 401", rec.Code)
    }
}

// serverWithSession returns a Server backed by an in-memory store + a
// pre-created user with one fresh session, returning the plaintext token.
func serverWithSession(t *testing.T) (*Server, string) {
    t.Helper()
    store := userstore.NewInMemory(t) // existing helper from userstore tests
    ctx := context.Background()
    u, _, err := store.CreateUser(ctx, "a@b", "Correct-Horse-Battery-Staple-1!")
    if err != nil {
        t.Fatalf("CreateUser: %v", err)
    }
    tok, _, err := store.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", 24*time.Hour)
    if err != nil {
        t.Fatalf("CreateSession: %v", err)
    }
    return NewServer(Config{Store: store}), tok
}
```

If `userstore.NewInMemory` doesn't exist, this task adds a thin sqlite-in-memory test helper to `internal/userstore/testutil_test.go` reused across tests. Add it now as part of Step 3.

- [ ] **Step 2: Run, expect compile failure**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestRequireSession_ -v
```

Expected: compile error (`requireSession`, `UserFromContext` undefined).

- [ ] **Step 3: Rewrite `auth.go`**

Replace `internal/relay/auth.go` with:

```go
package relay

import (
    "context"
    "encoding/base64"
    "errors"
    "net/http"
    "strings"

    "github.com/attson/atterm/internal/userstore"
)

type userCtxKey struct{}

// UserFromContext returns the user attached by requireSession middleware.
func UserFromContext(ctx context.Context) (*userstore.User, bool) {
    u, ok := ctx.Value(userCtxKey{}).(*userstore.User)
    return u, ok && u != nil
}

// requireSession extracts a session token from the request, looks it up
// in the store, and injects the owning user into the request context.
// On miss, expired, or revoked: writes 401 and aborts.
func (s *Server) requireSession(h http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        tok := tokenFromRequest(r)
        if tok == "" {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        _, user, err := s.cfg.Store.LookupSession(r.Context(), tok)
        if err != nil || user == nil {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        h(w, r.WithContext(context.WithValue(r.Context(), userCtxKey{}, user)))
    }
}

// tokenFromRequest accepts a session token via either:
//   1. Authorization: Bearer <token>
//   2. Sec-WebSocket-Protocol: atterm-token.<token>
//   3. Sec-WebSocket-Protocol: atterm-token-b64.<base64url(token)>
// URL query tokens are intentionally rejected.
func tokenFromRequest(r *http.Request) string {
    if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
        return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
    }
    return tokenFromSubprotocol(r)
}

func tokenFromSubprotocol(r *http.Request) string {
    p := r.Header.Get("Sec-WebSocket-Protocol")
    if p == "" {
        return ""
    }
    for _, part := range strings.Split(p, ",") {
        part = strings.TrimSpace(part)
        if strings.HasPrefix(part, "atterm-token.") {
            return strings.TrimPrefix(part, "atterm-token.")
        }
        if strings.HasPrefix(part, "atterm-token-b64.") {
            decoded, err := base64.RawURLEncoding.DecodeString(
                strings.TrimPrefix(part, "atterm-token-b64."))
            if err == nil {
                return string(decoded)
            }
        }
    }
    return ""
}

var errNoSession = errors.New("relay: no session in context")
```

Delete every other function previously in this file: `authScope`, `authorize`, `authorizeClient`, `authorizeWithScope`, `authorizeWithScopeAndHashes`, `authorizeWithScopeAndHashesFromToken`, `authorizeClientWithConfig`, `authorizeClientWebSocketWithConfig`, `tokenEqual`, `tokenMatchesHash`, `tokenFromRequestNoQuery`.

- [ ] **Step 4: Add `userstore.NewInMemory(t)` test helper if missing**

In `internal/userstore/testutil_test.go` (create if absent):

```go
package userstore

import (
    "context"
    "testing"

    _ "github.com/mattn/go-sqlite3"
)

// NewInMemory opens an in-memory sqlite store and runs migrations.
func NewInMemory(t *testing.T) *Store {
    t.Helper()
    s, err := Open(context.Background(), ":memory:")
    if err != nil {
        t.Fatalf("Open: %v", err)
    }
    t.Cleanup(func() { _ = s.Close() })
    return s
}
```

(Adjust `Open` signature to match the existing constructor.)

- [ ] **Step 5: Build + test**

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./internal/relay/... && go test ./internal/relay/ -run TestRequireSession_ -v
```

Expected: PASS (`go build` may still fail on routes that haven't been rewired — Task 1.7. Continue to 1.6/1.7 in sequence.)

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add internal/relay/auth.go internal/relay/auth_test.go internal/userstore/testutil_test.go
git commit -m "relay: requireSession middleware replaces authorize* family"
```

---

### Task 1.6: Delete CSRF middleware and all call sites

**Files:**
- Delete: `internal/relay/csrfmw.go`
- Delete: `internal/relay/csrfmw_test.go`
- Modify: `internal/relay/auth_http.go` (delete `CSRFToken(...)` call at ~line 332; delete `RequireCSRF` wraps at ~lines 65/67/69)
- Modify: `internal/relay/server.go` (delete `RequireCSRF` wrap at ~lines 152-155)

- [ ] **Step 1: Delete the files**

```bash
cd /Users/attson/code/github.com.attson/atterm && rm internal/relay/csrfmw.go internal/relay/csrfmw_test.go
```

- [ ] **Step 2: Strip `RequireCSRF` usage**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'RequireCSRF\|CSRFToken\|cachedCsrf' internal/relay/
```

For each hit:
- If a route used `RequireCSRF(handler)`, replace with the bare `handler` (auth happens via `requireSession` which we're about to wire in Task 1.7).
- The `CSRFToken(c.Value, p.CSRFSecret)` line in `auth_http.go` near line 332 — delete the surrounding block that adds `resp["csrf_token"]`.

- [ ] **Step 3: Build**

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./internal/relay/...
```

Expected: build succeeds, except for downstream `Set-Cookie` references that are removed in Task 1.7.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A internal/relay/
git commit -m "relay: delete csrfmw and all RequireCSRF call sites"
```

---

### Task 1.7: Rewrite `auth_http.go` — login/signup/setup return session_token, drop cookies, drop /api/me/tokens

**Files:**
- Modify: `internal/relay/auth_http.go`
- Test: `internal/relay/auth_http_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/relay/auth_http_test.go`:

```go
func TestLogin_ReturnsSessionTokenAndNoSetCookie(t *testing.T) {
    s := serverFixture(t) // existing helper; ensures store has user "alice"
    body := strings.NewReader(`{"email":"alice@example.com","password":"Correct-Horse-Battery-Staple-1!"}`)
    req := httptest.NewRequest("POST", "/api/auth/login", body)
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()
    s.handleLogin(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
    }
    if cookies := rec.Result().Cookies(); len(cookies) > 0 {
        t.Fatalf("login must not Set-Cookie; got %d cookies", len(cookies))
    }
    var resp struct {
        SessionToken string `json:"session_token"`
        ExpiresAt    int64  `json:"expires_at"`
        User         struct {
            ID    string `json:"id"`
            Email string `json:"email"`
        } `json:"user"`
    }
    if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if resp.SessionToken == "" {
        t.Fatal("missing session_token")
    }
    if resp.ExpiresAt == 0 {
        t.Fatal("missing expires_at")
    }
    if resp.User.Email != "alice@example.com" {
        t.Fatalf("user.email: got %q want alice@example.com", resp.User.Email)
    }
}

func TestLogout_RevokesBearerSession(t *testing.T) {
    s, tok := serverWithSession(t)
    req := httptest.NewRequest("POST", "/api/auth/logout", nil)
    req.Header.Set("Authorization", "Bearer "+tok)
    rec := httptest.NewRecorder()
    s.requireSession(s.handleLogout)(rec, req)
    if rec.Code != http.StatusNoContent {
        t.Fatalf("status: %d", rec.Code)
    }
    // After logout, the session must no longer resolve.
    _, _, err := s.cfg.Store.LookupSession(context.Background(), tok)
    if err == nil {
        t.Fatal("session still resolves after logout")
    }
}

func TestMeTokens_RoutesGone(t *testing.T) {
    s, tok := serverWithSession(t)
    for _, method := range []string{"GET", "POST", "DELETE"} {
        req := httptest.NewRequest(method, "/api/me/tokens", nil)
        req.Header.Set("Authorization", "Bearer "+tok)
        rec := httptest.NewRecorder()
        s.Handler().ServeHTTP(rec, req)
        if rec.Code != http.StatusNotFound {
            t.Fatalf("%s /api/me/tokens: got %d want 404", method, rec.Code)
        }
    }
}
```

- [ ] **Step 2: Run, expect failure**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run 'TestLogin_|TestLogout_|TestMeTokens_' -v
```

Expected: failures referencing `session_token`, cookies, or 200 responses for `/api/me/tokens`.

- [ ] **Step 3: Rewrite `handleLogin` (auth_http.go:243-287)**

Replace the body so that on successful password verification it:

1. Calls `s.cfg.Store.CreateSession(ctx, user.ID, userAgent, ipPrefix, sessionTTL)` and gets the plaintext token + Session row.
2. Writes JSON `{session_token, expires_at, user: {id, email, is_admin}}`.
3. Removes any `setSessionCookie` / `w.Header().Set("Set-Cookie", …)` lines.

Reference shape:

```go
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
    var in struct {
        Email    string `json:"email"`
        Password string `json:"password"`
    }
    if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
        http.Error(w, "bad json", http.StatusBadRequest)
        return
    }
    user, err := s.cfg.Store.VerifyPassword(r.Context(), in.Email, in.Password)
    if err != nil {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }
    tok, sess, err := s.cfg.Store.CreateSession(r.Context(), user.ID,
        r.UserAgent(), ipPrefix(r), sessionTTL)
    if err != nil {
        http.Error(w, "session", http.StatusInternalServerError)
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{
        "session_token": tok,
        "expires_at":    sess.ExpiresAt.Unix(),
        "user": map[string]any{
            "id":       user.ID,
            "email":    user.Email,
            "is_admin": user.IsAdmin,
        },
    })
}
```

- [ ] **Step 4: Rewrite `handleSignup` and `handleSetup`**

Apply the same change: after creating the user, mint a session and return the same JSON shape. Delete any `setSessionCookie` calls.

- [ ] **Step 5: Rewrite `handleLogout`**

Replace cookie-reading code with:

```go
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
    tok := tokenFromRequest(r)
    if tok == "" {
        w.WriteHeader(http.StatusNoContent)
        return
    }
    _ = s.cfg.Store.DeleteSession(r.Context(), tok)
    w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 6: Delete `/api/me/tokens` handlers and route registrations**

In `auth_http.go`:
- Delete `handleMeTokensList` (~lines 344-385)
- Delete `handleMeTokensCreate` (~lines 392-422)
- Delete `handleMeTokensDelete` (~lines 428-451)
- In `RegisterInto` (~lines 62-80), delete the three lines that mount these routes.

- [ ] **Step 7: Remove `setSessionCookie` helper if defined**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -n 'setSessionCookie\|clearSessionCookie\|atterm_session' internal/relay/
```

Delete every match (function bodies and call sites). If the cookie name `atterm_session` is referenced in tests, those tests are deleted by Task 1.11.

- [ ] **Step 8: Run tests**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run 'TestLogin_|TestLogout_|TestMeTokens_' -v
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add internal/relay/auth_http.go internal/relay/auth_http_test.go
git commit -m "relay: login/signup/setup return session_token; logout reads Bearer; drop /api/me/tokens"
```

---

### Task 1.8: Rewrite `pair_http.go` — consume returns session_token

**Files:**
- Modify: `internal/relay/pair_http.go`
- Test: `internal/relay/pair_http_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/pair_http_test.go`:

```go
func TestPairConsume_ReturnsSessionToken(t *testing.T) {
    s, ownerTok := serverWithSession(t)
    // Owner creates a pair code.
    req := httptest.NewRequest("POST", "/api/pair/create", nil)
    req.Header.Set("Authorization", "Bearer "+ownerTok)
    rec := httptest.NewRecorder()
    s.requireSession(s.handlePairCreate)(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("create status: %d", rec.Code)
    }
    var create struct {
        Token string `json:"token"`
    }
    _ = json.Unmarshal(rec.Body.Bytes(), &create)
    if create.Token == "" {
        t.Fatal("no pair token")
    }

    // New device consumes.
    body, _ := json.Marshal(map[string]string{"token": create.Token})
    req = httptest.NewRequest("POST", "/api/pair/consume", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rec = httptest.NewRecorder()
    s.handlePairConsume(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("consume status: %d body=%s", rec.Code, rec.Body.String())
    }
    var resp struct {
        SessionToken string `json:"session_token"`
        ExpiresAt    int64  `json:"expires_at"`
        RelayURL     string `json:"relay_url"`
        User         struct {
            Email string `json:"email"`
        } `json:"user"`
    }
    _ = json.Unmarshal(rec.Body.Bytes(), &resp)
    if resp.SessionToken == "" {
        t.Fatal("missing session_token")
    }
    if resp.User.Email == "" {
        t.Fatal("missing user.email")
    }
    // The returned token must resolve to the original owner.
    _, gotUser, err := s.cfg.Store.LookupSession(context.Background(), resp.SessionToken)
    if err != nil || gotUser.Email != "a@b" {
        t.Fatalf("session does not resolve to owner: %v %v", err, gotUser)
    }
}
```

- [ ] **Step 2: Run, expect failure**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestPairConsume_ -v
```

Expected: failure (current implementation returns `api_token`).

- [ ] **Step 3: Rewrite `handlePairConsume`**

In `internal/relay/pair_http.go` (lines 48-86):

```go
func (s *Server) handlePairConsume(w http.ResponseWriter, r *http.Request) {
    var in struct {
        Token string `json:"token"`
    }
    if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Token == "" {
        http.Error(w, "bad json", http.StatusBadRequest)
        return
    }
    user, err := s.cfg.Store.ConsumePairingToken(r.Context(), in.Token)
    if err != nil {
        status := http.StatusBadRequest
        switch err {
        case userstore.ErrPairingExpired:
            status = http.StatusGone
        case userstore.ErrPairingConsumed:
            status = http.StatusConflict
        case userstore.ErrPairingNotFound:
            status = http.StatusNotFound
        }
        http.Error(w, err.Error(), status)
        return
    }
    tok, sess, err := s.cfg.Store.CreateSession(r.Context(), user.ID,
        r.UserAgent(), ipPrefix(r), sessionTTL)
    if err != nil {
        http.Error(w, "session", http.StatusInternalServerError)
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{
        "session_token": tok,
        "expires_at":    sess.ExpiresAt.Unix(),
        "relay_url":     s.cfg.PublicRelayURL,
        "user": map[string]any{
            "id":    user.ID,
            "email": user.Email,
        },
    })
}
```

- [ ] **Step 4: Run test**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestPairConsume_ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add internal/relay/pair_http.go internal/relay/pair_http_test.go
git commit -m "relay: pair consume mints session_token instead of api_token"
```

---

### Task 1.9: Delete `Config.Token` / `ReadOnlyTokens` / `ReadOnlyTokenHashes`; wire all routes through `requireSession`

**Files:**
- Modify: `internal/relay/server.go`
- Modify: `cmd/atterm-relay/main.go` (delete CLI flag / env var parsing)

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/server_test.go`:

```go
func TestServer_AgentRoute_RejectsWithoutSession(t *testing.T) {
    s, _ := serverWithSession(t)
    req := httptest.NewRequest("GET", "/agent", nil)
    rec := httptest.NewRecorder()
    s.Handler().ServeHTTP(rec, req)
    if rec.Code != http.StatusUnauthorized {
        t.Fatalf("/agent unauth: got %d want 401", rec.Code)
    }
}

func TestServer_AgentRoute_AcceptsSession(t *testing.T) {
    s, tok := serverWithSession(t)
    // /agent expects a WS upgrade; we just check we pass the auth gate.
    req := httptest.NewRequest("GET", "/agent", nil)
    req.Header.Set("Authorization", "Bearer "+tok)
    req.Header.Set("Connection", "Upgrade")
    req.Header.Set("Upgrade", "websocket")
    req.Header.Set("Sec-WebSocket-Version", "13")
    req.Header.Set("Sec-WebSocket-Key", "dGVzdC1rZXktMTIzNDU2Nzg=")
    rec := httptest.NewRecorder()
    s.Handler().ServeHTTP(rec, req)
    // 101 (Switching Protocols) or 400 (websocket lib's handshake error
    // on a synthetic request) both pass the auth gate. 401 is the
    // failure we're asserting against.
    if rec.Code == http.StatusUnauthorized {
        t.Fatalf("/agent rejected a valid session token: %d", rec.Code)
    }
}
```

Replicate the same pair for `/uplink`, `/client`, `/client-sessions`, `/api/sessions`.

- [ ] **Step 2: Run, expect failure**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run 'TestServer_AgentRoute_|TestServer_UplinkRoute_|TestServer_ClientRoute_|TestServer_SessionsRoute_' -v
```

Expected: failure (current code accepts share-secret, not user session).

- [ ] **Step 3: Trim `Config` (server.go:31-82)**

Remove fields:
- `Token string`
- `ReadOnlyTokens []string`
- `ReadOnlyTokenHashes []string`

Leave `Store`, `Resolver`, `PublicRelayURL`, `SessionTTL` etc.

- [ ] **Step 4: Rewrite the five route handlers**

For each of `handleAgentHTTP` (~299-333), `handleUplinkHTTP` (~347-383), `handleClientHTTP` (~385-426), `handleClientSessionsHTTP` (~428-463), `handleSessionsHTTP` (~465-494):

- Delete the `authorize*` call (line numbers 310 / 358 / 400 / 439 / 476).
- Wrap registration in `requireSession`.

Example (in route registration, often the `routes` function):

```go
mux.HandleFunc("/agent", s.requireSession(s.handleAgentHTTP))
mux.HandleFunc("/uplink", s.requireSession(s.handleUplinkHTTP))
mux.HandleFunc("/client", s.requireSession(s.handleClientHTTP))
mux.HandleFunc("/client-sessions", s.requireSession(s.handleClientSessionsHTTP))
mux.HandleFunc("/api/sessions", s.requireSession(s.handleSessionsHTTP))
```

Inside each handler, replace the historical `authorize*` block (lines noted above) with `user, _ := UserFromContext(r.Context())` so downstream code that wants the user has it. (The handler body that did the `authorizeWithScope` check + `s.debugf("http reject ...")` is now redundant — delete those lines.)

- [ ] **Step 5: Remove CLI flag and env parsing in `cmd/atterm-relay/main.go`**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -n 'ATTERM_RELAY_TOKEN\|readonly-tokens\|--token\|Config{Token' cmd/atterm-relay/
```

Delete every match. Make sure the `relay.Config{...}` literal constructed in `main.go` no longer mentions `Token` / `ReadOnlyTokens` / `ReadOnlyTokenHashes`.

- [ ] **Step 6: Build**

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./...
```

Expected: success.

- [ ] **Step 7: Run all relay tests**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -v
```

Expected: PASS. Old tests that build `Config{Token: "dev"}` and use it to bypass auth must already have been updated in Task 1.5; if any are still red, edit them to use `serverWithSession`.

- [ ] **Step 8: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A internal/relay/ cmd/atterm-relay/
git commit -m "relay: delete share-secret config; route all WS/API through requireSession"
```

---

### Task 1.10: Re-mount remaining authenticated routes under `requireSession`

These were guarded by either CSRF (deleted in 1.6) or cookie-reading code (deleted in 1.7). Wire them onto `requireSession` so they require a valid Bearer token.

**Files:**
- Modify: `internal/relay/auth_http.go` (RegisterInto, ~lines 62-80)
- Modify: `internal/relay/me_*_http.go` (every route registration in this package)
- Modify: `internal/relay/web_push_http.go`, `webhooks_http.go`, `admin_http.go` (every registered route's mount line)

- [ ] **Step 1: List every protected route**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'mux\.HandleFunc\|mux\.Handle\b' internal/relay/
```

For each registration, decide: public (login/signup/setup/pair-consume/healthz/version) or protected (everything else). Tag in a scratch list.

- [ ] **Step 2: Wrap every protected registration**

Example:

```go
// before
mux.HandleFunc("/api/me", s.handleMe)
// after
mux.HandleFunc("/api/me", s.requireSession(s.handleMe))
```

Apply to `/api/me`, `/api/me/sessions`, `/api/me/sessions/:id` (delete), `/api/pair/create`, `/api/auth/logout`, `/api/push/*`, `/api/webhooks/*`, `/api/admin/*`, etc.

Leave bare (public): `/api/auth/login`, `/api/auth/register`, `/api/auth/setup`, `/api/pair/consume`, `/healthz`, `/version`, static `/web/...` assets.

- [ ] **Step 3: Remove now-dead per-handler cookie reads**

Inside each protected handler, code that historically did `c, err := r.Cookie("atterm_session"); …; user := …` is replaced with `user, _ := UserFromContext(r.Context())`.

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'r\.Cookie\|atterm_session' internal/relay/
```

Every match should be deleted; the user is now in context.

- [ ] **Step 4: Build + tests**

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./... && go test ./internal/relay/ -v
```

Expected: PASS. Any remaining red tests are exercising the deleted CSRF / cookie semantics — delete those test cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A internal/relay/
git commit -m "relay: re-mount /api/me, /api/pair, /api/push, /api/webhooks, /api/admin under requireSession"
```

---

### Task 1.11: Update bootstrap path to emit first session token

The single-user / self-host case: on a fresh database with `ATTERM_BOOTSTRAP_ADMIN_EMAIL` + `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` set, the relay should create the admin user **and** print a one-time session token to stdout so the desktop app / CI can pick it up.

**Files:**
- Modify: `internal/relay/bootstrap_admin.go`
- Modify: `cmd/atterm-relay/main.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/relay/bootstrap_admin_test.go` (create if missing):

```go
func TestBootstrapAdmin_EmitsSessionToken(t *testing.T) {
    store := userstore.NewInMemory(t)
    tok, user, err := bootstrapAdmin(context.Background(), store,
        "admin@example.com", "Correct-Horse-Battery-Staple-1!")
    if err != nil {
        t.Fatalf("bootstrapAdmin: %v", err)
    }
    if tok == "" {
        t.Fatal("expected session token")
    }
    if user.Email != "admin@example.com" {
        t.Fatalf("user.email: %q", user.Email)
    }
    if _, _, err := store.LookupSession(context.Background(), tok); err != nil {
        t.Fatalf("session does not resolve: %v", err)
    }
}
```

- [ ] **Step 2: Run, expect failure**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestBootstrapAdmin_ -v
```

Expected: signature mismatch (current `bootstrapAdmin` returns only error).

- [ ] **Step 3: Modify `bootstrap_admin.go`**

Change signature to:

```go
func bootstrapAdmin(ctx context.Context, store *userstore.Store, email, pw string) (string, *userstore.User, error)
```

After `EnsureAdminUser` returns the user, mint a session via `store.CreateSession(ctx, user.ID, "bootstrap", "", sessionTTL)` and return `(token, user, nil)`.

- [ ] **Step 4: Update `cmd/atterm-relay/main.go`**

At the bootstrap call (currently `bootstrapAdmin(ctx, store, bootstrapEmail, bootstrapPassword)` ~line 85), capture the token and log it:

```go
bootstrapTok, _, err := bootstrapAdmin(ctx, store, bootstrapEmail, bootstrapPassword)
if err != nil {
    log.Fatalf("bootstrap: %v", err)
}
if bootstrapTok != "" {
    log.Printf("bootstrap admin created; session_token=%s", bootstrapTok)
}
```

- [ ] **Step 5: Run tests + build**

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./... && go test ./internal/relay/ -run TestBootstrapAdmin_ -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add internal/relay/bootstrap_admin.go internal/relay/bootstrap_admin_test.go cmd/atterm-relay/main.go
git commit -m "relay: bootstrap admin emits one-time session_token on stdout"
```

---

### Task 1.12: Sweep remaining backend tests

**Files:**
- `internal/relay/server_test.go`
- `internal/relay/identity_test.go`
- `internal/relay/me_*_test.go`
- `internal/relay/web_push_http_test.go`, `webhooks_http_test.go`, `admin_http_test.go`
- `internal/relay/client_conn_test.go`, `uplink_conn_test.go`, `agent_conn_test.go`

- [ ] **Step 1: Run all backend tests and collect failures**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/... 2>&1 | tee /tmp/relay-test-failures.log
```

- [ ] **Step 2: For each failure, classify**

- Failure asserts on `api_token`, `csrf_token`, `Set-Cookie atterm_session`, `Config.Token`, or `RequireCSRF` → delete the assertion or replace with the session_token equivalent.
- Failure builds a `Config{Token: "..."}` to get past auth → switch to `serverWithSession(t)`.

- [ ] **Step 3: Re-run until green**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./... -race
```

Expected: all green.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A internal/
git commit -m "relay: sweep backend tests for session-token contract"
```

---

### Task 1.13: Open PR-1

- [ ] **Step 1: Push branch**

```bash
cd /Users/attson/code/github.com.attson/atterm && git push -u origin feat/relay-session-token-backend
```

- [ ] **Step 2: Open PR**

```bash
gh pr create --title "feat(relay): unify on session token; remove atk_/CSRF/share-secret" --body "$(cat <<'EOF'
## Summary
- Delete api_tokens / CSRF middleware / Config.Token & ReadOnlyTokens / atterm_session cookie path
- Rename web_sessions → sessions; add LookupSession that joins user
- Introduce requireSession middleware; wire all WS / API routes through it
- /api/auth/login, /signup, /setup return {session_token, expires_at, user}
- /api/pair/consume mints a session_token (no more atk_)
- Bootstrap admin emits a one-time session_token on stdout
- Operator-facing migration story: drop the old DB and redeploy (no SQL migration code)

## Test plan
- [ ] go test ./... -race passes
- [ ] Fresh database boot: ATTERM_BOOTSTRAP_ADMIN_EMAIL/PASSWORD logs a session token
- [ ] Manual: curl /api/auth/login → session_token; curl /api/me with Bearer succeeds
- [ ] WS /agent rejects without Bearer (401); accepts with Bearer (handshake 101 or 400)

Spec: docs/superpowers/specs/2026-06-09-relay-auth-token-removal-design.md
EOF
)"
```

Wait for the PR to land before starting Phase 2 — every later phase imports the new API shape.

---

## Phase 2 — Delete `cmd/atterm-agent` and `internal/agent` (PR-2)

Independent of Phase 1 in code, but easier to verify once the relay branch is on `main` (no risk of "did I break the agent?"). Branch: `chore/remove-atterm-agent`.

### Task 2.1: Delete both directories

**Files:**
- Delete: `cmd/atterm-agent/` (entire directory)
- Delete: `internal/agent/` (entire directory)

- [ ] **Step 1: Delete**

```bash
cd /Users/attson/code/github.com.attson/atterm && rm -rf cmd/atterm-agent internal/agent
```

- [ ] **Step 2: Verify no references**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'atterm-agent\|internal/agent\|ATTERM_TOKEN' --include='*.go' --include='*.md' --include='*.yml' --include='*.sh' --include='Dockerfile*' .
```

For every match in source code (`.go`): delete the import line + the calling code. For matches in docs/scripts: handled in Task 2.2.

- [ ] **Step 3: Build**

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./...
```

Expected: success.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A
git commit -m "chore: remove cmd/atterm-agent and internal/agent (Phase 0 CLI relic)"
```

---

### Task 2.2: Update docs and dev scripts

**Files:**
- Modify: `AGENTS.md` (lines 14-15 and 84-85 and 126 — delete `atterm-agent` entry, command-line section, routing-table row)
- Modify: `README.md` (lines 344-345 and any other mention)
- Modify: `scripts/dev.sh` (line 8: delete the `ATTERM_TOKEN=dev go run ./cmd/atterm-agent` example)
- Modify: `Dockerfile.relay` (if any `--token` flag is used)
- Modify: `docs/spec/protocol.md` (drop `atk_` section, add a `session_token` section)
- Modify: `docs/spec/architecture.md` (drop the token-mode paragraphs; mention session-token + requireSession + bootstrap admin)

- [ ] **Step 1: Edit each file**

For `AGENTS.md`:
- Delete the `cmd/atterm-agent/` line in the file tree (~line 15)
- Delete the "命令行 agent" section (~lines 84-85)
- Delete the routing table row "CLI wrapper 行为" (~line 126)

For `README.md`:
- In the `cmd/` description, drop "和 atterm-agent": `cmd/  atterm-relay 入口`

For `scripts/dev.sh`:
- Delete the `ATTERM_TOKEN=dev go run ./cmd/atterm-agent ...` example.

For `Dockerfile.relay`:
- Remove any `--token` argument; the relay no longer accepts that flag.

For `docs/spec/protocol.md`:
- Delete the `atk_` / "API token" subsection.
- Add a "session_token" subsection: format (`ses_` + 32 random bytes base64url), transport (Bearer for HTTP, `Sec-WebSocket-Protocol: atterm-token.<token>` for browser WS), TTL (`sessions.expires_at`), revocation (DELETE row).

For `docs/spec/architecture.md`:
- Replace any "token mode" / "share-secret" paragraphs with: "All clients authenticate via email + password (or pairing). Successful login returns a `session_token` that the client carries on every HTTP/WS request. The `requireSession` middleware validates the token against the `sessions` table."

- [ ] **Step 2: Verify**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'atterm-agent\|ATTERM_TOKEN' AGENTS.md README.md scripts/ Dockerfile.relay
```

Expected: no output.

- [ ] **Step 3: Commit + open PR**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A
git commit -m "docs: drop atterm-agent references from AGENTS.md, README, scripts"
git push -u origin chore/remove-atterm-agent
gh pr create --title "chore: remove cmd/atterm-agent (Phase 0 relic)" --body "$(cat <<'EOF'
## Summary
- Removes cmd/atterm-agent and internal/agent — superseded by desktop uplink and mobile capacitor.ts
- Updates AGENTS.md, README.md, scripts/dev.sh accordingly

## Test plan
- [ ] go build ./... succeeds
- [ ] grep finds no remaining references to atterm-agent or ATTERM_TOKEN

Spec: docs/superpowers/specs/2026-06-09-relay-auth-token-removal-design.md
EOF
)"
```

---

## Phase 3 — Desktop client (PR-3)

Branch: `feat/desktop-session-token-client`. Depends on Phase 1 being on `main`.

### Task 3.1: Rename `RelayToken` → `RelaySessionToken` across desktop

**Files:**
- Modify: `desktop/config.go` (line 40)
- Modify: `desktop/uplink.go` (line 182 + field declarations)
- Modify: `desktop/app.go` (every reference)
- Modify: `desktop/relay_host.go` (every reference)

- [ ] **Step 1: Rename in `desktop/config.go`**

In the `appConfig` struct (~lines 38-120), rename:

```go
// before
RelayToken string `json:"relay_token,omitempty"`
// after
RelaySessionToken string `json:"relay_session_token,omitempty"`
```

- [ ] **Step 2: Sweep callers**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'RelayToken\|relay_token\b' desktop/
```

Rename every reference to `RelaySessionToken` / `relay_session_token`.

- [ ] **Step 3: Build**

```bash
cd /Users/attson/code/github.com.attson/atterm && cd desktop && go build ./...
```

Expected: success.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A desktop/
git commit -m "desktop: rename RelayToken → RelaySessionToken in config + uplink"
```

---

### Task 3.2: Add `LoginRemoteRelay` Wails method

Lets the Settings dialog accept email + password and persist the returned session token.

**Files:**
- Modify: `desktop/app.go`

- [ ] **Step 1: Write the test (Wails Go side)**

Append to `desktop/app_test.go` (create if absent):

```go
func TestLoginRemoteRelay_PersistsSessionToken(t *testing.T) {
    fake := newFakeRelayServer(t) // helper that responds 200 with session_token
    defer fake.Close()
    app := newTestApp(t)
    err := app.LoginRemoteRelay(fake.URL, "u@example.com", "Correct-Horse-Battery-Staple-1!")
    if err != nil {
        t.Fatalf("LoginRemoteRelay: %v", err)
    }
    cfg := app.GetRelayConfig()
    if cfg.RelaySessionToken == "" {
        t.Fatal("session token not persisted")
    }
    if cfg.RelayURL != fake.URL {
        t.Fatalf("relay url: got %q", cfg.RelayURL)
    }
}
```

- [ ] **Step 2: Add the method**

```go
// LoginRemoteRelay calls POST /api/auth/login on the given relay URL and
// persists the returned session token to local config.
func (a *App) LoginRemoteRelay(relayURL, email, password string) error {
    body, _ := json.Marshal(map[string]string{"email": email, "password": password})
    req, _ := http.NewRequestWithContext(a.ctx, "POST",
        strings.TrimRight(relayURL, "/")+"/api/auth/login", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := a.httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("login: status %d", resp.StatusCode)
    }
    var out struct {
        SessionToken string `json:"session_token"`
        ExpiresAt    int64  `json:"expires_at"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return err
    }
    return a.SetRelayConfig(relayURL, out.SessionToken)
}
```

- [ ] **Step 3: Test**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop && go test ./... -run TestLoginRemoteRelay_ -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add desktop/app.go desktop/app_test.go
git commit -m "desktop: add LoginRemoteRelay Wails method"
```

---

### Task 3.3: Replace local share-secret with bootstrap admin

Local relay no longer accepts a random share-secret. On first run, the desktop app creates a local admin user (or reuses the existing one) and stores its session token.

**Files:**
- Modify: `desktop/relay_host.go` (lines 57-95: `startRelayHost`; lines 421-427: `randomToken`)
- Modify: `desktop/config.go` (add `LocalAdminEmail string` field if needed)

- [ ] **Step 1: Write the failing test**

Append to `desktop/relay_host_test.go`:

```go
func TestStartRelayHost_CreatesLocalAdminAndSessionToken(t *testing.T) {
    dir := t.TempDir()
    cfg := newTestConfig(dir)
    host, err := startRelayHost(context.Background(), cfg)
    if err != nil {
        t.Fatalf("startRelayHost: %v", err)
    }
    defer host.Close()
    ep := host.GetEndpoint()
    if ep.SessionToken == "" {
        t.Fatal("local endpoint missing session token")
    }
    // The token should resolve via the local relay's HTTP API.
    req, _ := http.NewRequest("GET", ep.HTTPURL+"/api/me", nil)
    req.Header.Set("Authorization", "Bearer "+ep.SessionToken)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("/api/me: %v", err)
    }
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("/api/me: %d", resp.StatusCode)
    }
}
```

- [ ] **Step 2: Run, expect failure**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop && go test ./... -run TestStartRelayHost_ -v
```

- [ ] **Step 3: Rewrite `startRelayHost`**

Replace random `Config.Token` injection with bootstrap admin. Skeleton:

```go
func startRelayHost(ctx context.Context, cfg *appConfig) (*relayHost, error) {
    store, err := userstore.Open(ctx, filepath.Join(cfg.dataDir, "userstore.db"))
    if err != nil {
        return nil, err
    }
    // Idempotent admin creation. The desktop app owns the credentials —
    // user never sees them.
    email := "local@atterm.local"
    pw := randomPassword(32) // saved into config the first time only
    if cfg.LocalAdminPassword == "" {
        cfg.LocalAdminPassword = pw
        if err := cfg.Save(); err != nil {
            return nil, err
        }
    }
    sessTok, _, err := bootstrapLocalAdmin(ctx, store, email, cfg.LocalAdminPassword)
    if err != nil {
        return nil, err
    }
    srv := relay.NewServer(relay.Config{Store: store, PublicRelayURL: "(local)"})
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        return nil, err
    }
    go http.Serve(ln, srv.Handler())
    return &relayHost{ln: ln, srv: srv, sessionToken: sessTok}, nil
}
```

Delete the `randomToken` function and any `relay.Config{Token: ...}` literal.

- [ ] **Step 4: Update `GetEndpoint` and call sites**

```go
type localEndpoint struct {
    HTTPURL      string
    WSURL        string
    SessionToken string
}

func (a *App) GetEndpoint() localEndpoint {
    return localEndpoint{
        HTTPURL:      a.host.HTTPURL(),
        WSURL:        a.host.WSURL(),
        SessionToken: a.host.sessionToken,
    }
}
```

Update the frontend `wails-bindings` consumer (`desktop/frontend/src/lib/connection.ts`) to read the new field name.

- [ ] **Step 5: Run tests + build**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop && go test ./... && go build ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A desktop/
git commit -m "desktop: local relay uses bootstrap admin session token; share-secret gone"
```

---

### Task 3.4: Frontend remote-relay login dialog

**Files:**
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`, `zh.ts`

- [ ] **Step 1: Replace "paste API token" UI with email/password form**

In `SettingsDialog.vue`, find the section that today accepts a pasted `atk_` token. Replace with two inputs (`email`, `password`) and a "Login" button that calls `window.go.main.App.LoginRemoteRelay(relayURL, email, password)`.

On success: close the dialog, show toast "已登录远程 relay". On failure: surface the error string.

- [ ] **Step 2: i18n strings**

Add:

```ts
// en
settings: {
  relay: {
    loginTitle: "Connect to remote relay",
    email: "Email",
    password: "Password",
    login: "Log in",
    loginFailed: "Login failed",
    loggedIn: "Connected to remote relay",
  },
},

// zh
settings: {
  relay: {
    loginTitle: "连接远程 relay",
    email: "邮箱",
    password: "密码",
    login: "登录",
    loginFailed: "登录失败",
    loggedIn: "已登录远程 relay",
  },
},
```

- [ ] **Step 3: Run frontend dev to manually verify**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop && wails dev -tags webkit2_41
```

Manually: open Settings → "Connect to remote relay" → enter creds → verify token persisted in `~/.config/atterm/config.json`.

- [ ] **Step 4: Commit and open PR**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A desktop/frontend/
git commit -m "desktop: replace paste-token UI with email/password login form"
git push -u origin feat/desktop-session-token-client
gh pr create --title "feat(desktop): use session token for relay (remote login + local bootstrap admin)" --body "$(cat <<'EOF'
## Summary
- Rename RelayToken → RelaySessionToken in desktop config and uplink
- Add LoginRemoteRelay Wails method for remote relay sign-in
- Local relay no longer uses random share-secret; uses an idempotent local admin user + persisted session token
- Settings dialog: paste-token UI replaced with email/password login

## Test plan
- [ ] go test ./... in desktop/ passes
- [ ] wails dev: first launch boots local relay; Settings shows "logged in" once email/password entered
- [ ] Restart desktop: still logged in (session_token persists in config)

Spec: docs/superpowers/specs/2026-06-09-relay-auth-token-removal-design.md
EOF
)"
```

---

## Phase 4 — Web client (PR-4)

Branch: `feat/web-session-token-client`.

### Task 4.1: Unify `apiFetch` — always Bearer, no cookies, no CSRF

**Files:**
- Modify: `web/src/shared/api/client.ts` (apiFetch ~lines 48-121; CSRF cache ~lines 23-29)
- Modify: `web/src/shared/api/relay-config.ts` (add `sessionToken` field)

- [ ] **Step 1: Replace `apiFetch`**

In `web/src/shared/api/client.ts`:

```ts
// Delete: cachedCsrf, setCsrfToken, clearCsrfToken, isMobileApp branching.
import { loadRelayConfig, clearRelayConfig } from './relay-config'

export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const cfg = loadRelayConfig()
  const headers = new Headers(init.headers ?? {})
  if (cfg?.sessionToken) {
    headers.set('Authorization', `Bearer ${cfg.sessionToken}`)
  }
  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json')
  }
  const url = (cfg?.baseURL ?? '') + path
  const res = await fetch(url, { ...init, headers, credentials: 'omit' })
  if (res.status === 401) {
    clearRelayConfig()
    if (typeof window !== 'undefined') {
      window.location.href = '/login'
    }
  }
  return res
}
```

- [ ] **Step 2: Extend `relay-config.ts`**

```ts
export type RelayConfig = {
  baseURL: string
  sessionToken: string | null
  expiresAt: number | null
}

const STORAGE_KEY = 'atterm.relay'

export function loadRelayConfig(): RelayConfig | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    return JSON.parse(raw)
  } catch {
    return null
  }
}

export function saveRelayConfig(cfg: RelayConfig): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg))
}

export function clearRelayConfig(): void {
  localStorage.removeItem(STORAGE_KEY)
}
```

- [ ] **Step 3: Update `web/tests/contract/*.mjs`**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'atterm_session\|X-CSRF-Token\|api_token\|cookie' web/tests/contract/
```

For each hit:
- Cookies → set `Authorization: Bearer <token>` header instead.
- CSRF assertions → delete.
- `api_token` field assertions → assert `session_token`.

- [ ] **Step 4: Build and run web contract tests**

```bash
cd /Users/attson/code/github.com.attson/atterm/web && npm run build && npm run test
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add web/src/shared/api/client.ts web/src/shared/api/relay-config.ts web/tests/contract/
git commit -m "web: apiFetch always uses Bearer session_token; drop cookie/CSRF branches"
```

---

### Task 4.2: Login + signup persist session_token

**Files:**
- Modify: `web/src/shared/api/auth.ts`

- [ ] **Step 1: Replace `login` and `signup`**

```ts
import { apiFetch } from './client'
import { saveRelayConfig, loadRelayConfig } from './relay-config'

type LoginResp = {
  session_token: string
  expires_at: number
  user: { id: string; email: string; is_admin: boolean }
}

export async function login(email: string, password: string): Promise<LoginResp> {
  const res = await apiFetch('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
  if (!res.ok) {
    throw new Error(`login failed: ${res.status}`)
  }
  const data = (await res.json()) as LoginResp
  const cfg = loadRelayConfig() ?? { baseURL: '', sessionToken: null, expiresAt: null }
  saveRelayConfig({
    baseURL: cfg.baseURL,
    sessionToken: data.session_token,
    expiresAt: data.expires_at,
  })
  return data
}

export async function logout(): Promise<void> {
  await apiFetch('/api/auth/logout', { method: 'POST' })
  clearRelayConfig()
}
```

- [ ] **Step 2: Run tests**

```bash
cd /Users/attson/code/github.com.attson/atterm/web && npm run test
```

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add web/src/shared/api/auth.ts
git commit -m "web: login/logout persist session_token in localStorage"
```

---

### Task 4.3: Sweep paste-token / "manage tokens" UI in web

**Files:** anything under `web/src/` that still references `api_token` / pasted-token flow / `/api/me/tokens`.

- [ ] **Step 1: Find every reference**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'api_token\|apiToken\|/api/me/tokens\|atk_\|Paste.*token\|Manage.*tokens' web/src/
```

- [ ] **Step 2: Delete every "token management" page / component / route**

If a `TokensPage.vue` (or similar) exists in `web/src/views/` or `web/src/components/`, delete the file and unregister its route from `web/src/router/`.

- [ ] **Step 3: Build**

```bash
cd /Users/attson/code/github.com.attson/atterm/web && npm run build
```

Expected: success.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add -A web/
git commit -m "web: delete API token management pages and pasted-token UI"
```

---

### Task 4.4: WebSocket sites use `atterm-token.<token>` subprotocol

**Files:**
- Modify: every `new WebSocket(url)` site in `web/src/` (locate via grep)

- [ ] **Step 1: Find every WS construction**

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'new WebSocket\|new ReconnectingWebSocket' web/src/
```

- [ ] **Step 2: Add the subprotocol**

For each site:

```ts
// before
const ws = new WebSocket(url)
// after
import { loadRelayConfig } from './shared/api/relay-config'
const cfg = loadRelayConfig()
const subprotocols = cfg?.sessionToken ? [`atterm-token.${cfg.sessionToken}`] : []
const ws = new WebSocket(url, subprotocols)
```

- [ ] **Step 3: Manual test**

```bash
cd /Users/attson/code/github.com.attson/atterm/web && npm run dev
```

Login in the browser; open DevTools → Network → WS; verify the WS upgrade carries `Sec-WebSocket-Protocol: atterm-token.<token>`.

- [ ] **Step 4: Commit + PR**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add web/src/
git commit -m "web: browser WS upgrades carry atterm-token.<token> subprotocol"
git push -u origin feat/web-session-token-client
gh pr create --title "feat(web): session-token-only auth" --body "$(cat <<'EOF'
## Summary
- apiFetch always uses Authorization: Bearer; cookies and CSRF removed
- login/signup persist session_token to localStorage; logout clears it
- 401 from any endpoint clears storage and redirects to /login
- Token management pages deleted
- Browser WS sites use Sec-WebSocket-Protocol: atterm-token.<token>

## Test plan
- [ ] npm test in web/ passes
- [ ] Manual: login in browser; reload page; still logged in
- [ ] Manual: revoke session via relay; next request → redirect to /login

Spec: docs/superpowers/specs/2026-06-09-relay-auth-token-removal-design.md
EOF
)"
```

---

## Phase 5 — Mobile Capacitor (PR-5)

Branch: `feat/mobile-session-token-client`.

### Task 5.1: `consumePairing` parses new response shape

**Files:**
- Modify: `desktop/frontend/src/platform/capacitor.ts` (lines 102-115)

- [ ] **Step 1: Update the type**

```ts
type PairConsumeResp = {
  session_token: string
  expires_at: number
  relay_url: string
  user: { id: string; email: string }
}
```

- [ ] **Step 2: Update `consumePairing`**

```ts
export async function consumePairing(pairBaseURL: string, pairToken: string): Promise<PairConsumeResp> {
  const res = await fetch(
    `${pairBaseURL.replace(/\/$/, '')}/api/pair/consume`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'omit',
      body: JSON.stringify({ token: pairToken }),
    },
  )
  if (!res.ok) throw new Error(`pair consume: ${res.status}`)
  return (await res.json()) as PairConsumeResp
}
```

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add desktop/frontend/src/platform/capacitor.ts
git commit -m "mobile: consumePairing parses session_token + expires_at"
```

---

### Task 5.2: Rename Keychain storage key

**Files:**
- Modify: `desktop/frontend/src/platform/secureStorage.ts` (line 6)

- [ ] **Step 1: Rename**

```ts
// before
const STORAGE_KEY = 'atterm.relay'
// after
const STORAGE_KEY = 'atterm.relay.session'
```

(Different key forces operator-driven re-pair; matches the spec's "no migration" stance.)

- [ ] **Step 2: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add desktop/frontend/src/platform/secureStorage.ts
git commit -m "mobile: rename Keychain key to atterm.relay.session"
```

---

### Task 5.3: `PairingConsume.vue` handles new response

**Files:**
- Modify: `desktop/frontend/src/mobile/PairingConsume.vue` (lines 38-46)

- [ ] **Step 1: Replace destructuring**

```ts
// before
const { relay_url, api_token } = result
saveRelayConfig({ baseURL: relay_url, sessionToken: api_token, expiresAt: null })
// after
const { relay_url, session_token, expires_at } = result
saveRelayConfig({ baseURL: relay_url, sessionToken: session_token, expiresAt: expires_at })
```

- [ ] **Step 2: Manual test on iOS simulator**

```bash
cd /Users/attson/code/github.com.attson/atterm/mobile && npx cap sync ios && npx cap open ios
```

In Xcode, run the app. From desktop, create a pair code (Settings → Generate). Scan with the mobile app camera. Verify the session token lands in Keychain and the session list loads.

- [ ] **Step 3: Commit + PR**

```bash
cd /Users/attson/code/github.com.attson/atterm && git add desktop/frontend/src/mobile/PairingConsume.vue
git commit -m "mobile: PairingConsume reads session_token + expires_at"
git push -u origin feat/mobile-session-token-client
gh pr create --title "feat(mobile): consume session_token from pair flow" --body "$(cat <<'EOF'
## Summary
- consumePairing returns session_token + expires_at
- Keychain key renamed to atterm.relay.session (forces re-pair after upgrade — matches no-migration stance)
- PairingConsume.vue writes new fields to secure storage

## Test plan
- [ ] iOS simulator: scan QR; session list loads; reopen app; still logged in
- [ ] Revoke session via desktop; next mobile request redirects to pair-again flow

Spec: docs/superpowers/specs/2026-06-09-relay-auth-token-removal-design.md
EOF
)"
```

---

## Final verification

After all five PRs land, run an end-to-end smoke test:

- [ ] **Step 1: Fresh relay deployment**

Stop the running relay, delete its DB file, start the new binary with `ATTERM_BOOTSTRAP_ADMIN_EMAIL` / `_PASSWORD` set. Confirm the log shows `bootstrap admin created; session_token=ses_...`.

- [ ] **Step 2: Web login**

Open the relay's web UI in a browser. Login with the bootstrap credentials. Confirm the session list page loads, then reload the page and confirm the session persists (localStorage works).

- [ ] **Step 3: Desktop pair**

Open the desktop app. Settings → Generate Pair Code. Switch to a fresh device (or simulator) running the mobile app. Scan. Confirm the mobile app loads the session list.

- [ ] **Step 4: WS smoke**

Click a session in the web UI. Confirm the terminal opens and streams output (`Sec-WebSocket-Protocol` worked through `requireSession`).

- [ ] **Step 5: 401 handling**

In the relay, manually `DELETE FROM sessions WHERE …` the desktop's session row. Make any action in the desktop app. Confirm the app surfaces the "session expired" path (currently: SetRelayConfig clears + login dialog reopens).

If everything passes, the spec is satisfied.
