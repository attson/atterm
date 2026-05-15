# SaaS User Accounts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `atterm-relay`'s shared-token authentication with per-user accounts: invite-code signup, cookie-based web login, per-user API tokens for desktop/CLI uplinks, owner-scoped session lists and web-push fan-out, and a desktop Settings redesign that fixes the existing "disconnect erases config" bug.

**Architecture:** New package `internal/userstore` owns a SQLite-backed `Store` interface (users, invitations, api_tokens, web_sessions). New file `internal/relay/identity.go` defines `Principal` and resolves it at every HTTP / WS-upgrade boundary. Existing relay handlers gain Principal-based filtering. `internal/proto` gains an `AUTH_INFO` frame. The desktop `SettingsRelay.vue` is redesigned around a `RelayPaused` config field so toggling uplink off no longer wipes URL/token.

**Tech Stack:** Go 1.23 (`modernc.org/sqlite`, `golang.org/x/crypto/argon2`, `github.com/oklog/ulid/v2`), Vue 3 + TypeScript + vitest (desktop), vanilla JS + `node --test` (web).

**Spec:** `docs/superpowers/specs/2026-05-15-saas-user-accounts-design.md`

---

## File Map

**Backend new (`internal/userstore`):**
- `internal/userstore/store.go` — `Store` interface + `SQLiteStore` ctor + migration runner
- `internal/userstore/store_test.go` — migration & open tests
- `internal/userstore/secret.go` — `Secret[T]` newtype with redacting `String()` / `GoString()`
- `internal/userstore/secret_test.go` — redaction verb tests
- `internal/userstore/users.go` — user CRUD, argon2id, csrf_secret
- `internal/userstore/users_test.go`
- `internal/userstore/invitations.go` — create / consume (txn) / list / revoke
- `internal/userstore/invitations_test.go`
- `internal/userstore/apitokens.go` — issue plaintext-once / lookup-by-hash / revoke
- `internal/userstore/apitokens_test.go`
- `internal/userstore/websessions.go` — create / lookup / sliding renewal / delete
- `internal/userstore/websessions_test.go`
- `internal/userstore/ulid.go` — ULID generator wrapper (so tests can stub time)
- `internal/userstore/migrations/0001_init.sql` — embedded
- `internal/userstore/migrations.go` — `go:embed migrations/*.sql`

**Backend new (`internal/relay`):**
- `internal/relay/identity.go` — `Principal`, `PrincipalKind`, `resolveIdentity`
- `internal/relay/identity_test.go`
- `internal/relay/csrfmw.go` — `Require(http.Handler) http.Handler`
- `internal/relay/csrfmw_test.go`
- `internal/relay/auth_http.go` — `/api/auth/{signup,login,logout}`, `/api/me/*`
- `internal/relay/auth_http_test.go`
- `internal/relay/argon2pool.go` — semaphore-bounded argon2 work pool + dummy-hash bootstrap
- `internal/relay/argon2pool_test.go`

**Backend modify (`internal/relay`):**
- `internal/relay/auth.go` — delete legacy shared-token / read-only-token branches; route through `resolveIdentity`
- `internal/relay/server.go` — register new routes; gate existing routes via Principal
- `internal/relay/admin_http.go` — add invitation / user management endpoints
- `internal/relay/admin_config.go` — remove `ReadOnlyTokenHashes`
- `internal/relay/uplink_conn.go` — reject non-User principals; bind connection OwnerUserID; enforce owner-binding invariant
- `internal/relay/agent_conn.go` — same as uplink for the CLI agent path
- `internal/relay/client_conn.go` — filter list/attach by `Principal.UserID`
- `internal/relay/permissions.go` — delete read-only-token branch
- `internal/relay/limits.go` — add `(IP, sha256(email))` login bucket + signup bucket + invite-fail bucket

**Backend modify (`internal/session`):**
- `internal/session/session.go` — add `OwnerUserID string` field; register-or-reject by owner
- `internal/session/session_test.go` — owner-binding invariant

**Backend modify (`internal/proto`):**
- `internal/proto/frame.go` — `TypeAuthInfo` constant + codec
- `internal/proto/frame_test.go` — round-trip test

**Backend modify (`internal/webpush`):**
- `internal/webpush/subscription.go` — key by `user_id`
- `internal/webpush/subscription_test.go`
- `internal/webpush/dispatch.go` — `DispatchCommandFinished(owner, ev)` filters by owner
- `internal/webpush/dispatch_test.go`
- `internal/webpush/persist.go` — schema v2 (`{userID: [...]}`); legacy rename on load
- `internal/webpush/persist_test.go`

**Backend modify (`cmd/atterm-relay`):**
- `cmd/atterm-relay/main.go` — delete `ATTERM_TOKEN` / `ATTERM_READ_ONLY_TOKENS` reads; add SEC-6 admin-token strength check; wire `userstore`
- `cmd/atterm-relay/main_test.go` — startup-validation tests

**Web frontend new (`web/`):**
- `web/login.html`
- `web/signup.html`
- `web/settings.html`
- `web/auth.js` — login/signup/logout/me/token API wrappers
- `web/auth.test.mjs`

**Web frontend modify (`web/`):**
- `web/index.html` — remove token panel; expect cookie auth
- `web/app.js` — fetch wrapper with CSRF; 401 → redirect to `/login.html`; remove token-fragment bootstrap
- `web/app-core.js` — remove token persistence
- `web/sw.js` — bypass cache for `/api/*` and authed endpoints
- `web/style.css` — add minimal styling for new pages

**Desktop modify (`desktop/`):**
- `desktop/config.go` — add `RelayPaused bool`
- `desktop/config_test.go` — zero-value compat test
- `desktop/app.go` — add `SetUplinkPaused` binding; gate `applyRelayConfig` on `RelayPaused`; parse `AUTH_INFO`; surface close-reason
- `desktop/app_test.go` — pause / unpause / config-retention tests
- `desktop/uplink.go` — parse `AUTH_INFO` frame; emit Wails events `relay:auth-info` and `relay:auth-error`
- `desktop/uplink_e2e_test.go` — extend
- `desktop/frontend/src/components/SettingsRelay.vue` — ON/OFF toggle + label/placeholder rename + open-in-browser
- `desktop/frontend/src/components/SettingsRelay.test.ts` — extend
- `desktop/frontend/src/lib/api.ts` — `SetUplinkPaused` wrapper; `AuthInfo` event subscription
- `desktop/frontend/src/App.vue` — banner for `relay:auth-error`

**Docs new:**
- (no new docs; updates only)

**Docs modify:**
- `README.md` — drop `ATTERM_TOKEN` / `ATTERM_READ_ONLY_TOKENS`; add invite + API token quick start
- `AGENTS.md` — same; update env table
- `docs/spec/architecture.md` — new section on user accounts and identity
- `docs/spec/protocol.md` — document `AUTH_INFO` frame

---

## Conventions used throughout this plan

- **TDD**: every code task starts with a failing test, then the minimal implementation. Verify the test fails before writing the implementation; verify it passes before the commit step.
- **Commit cadence**: one commit per task. Commit subject in lowercase imperative ≤ 72 chars, body explains *why* not *what*.
- **Run commands**: prefix env `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH` when shell PATH is unreliable. Run all Go tests with `-tags webkit2_41` (a no-op on macOS/Windows; required on Linux).
- **No new dependencies beyond these three** (add via `go get` in Task 1.0): `modernc.org/sqlite`, `github.com/oklog/ulid/v2`. `golang.org/x/crypto/argon2` is already a transitive dependency; verify with `go list -m golang.org/x/crypto`.
- **Spec references** are inline as `(spec §X.Y)` when a task implements a specific spec section. Re-read the spec section if anything in the task is unclear.

---

## Phase P1: `internal/userstore`

Builds the database layer. End state: complete CRUD + tests for users, invitations, API tokens, web sessions, with a single `Store` interface that `internal/relay` consumes.

### Task 1.0: Add dependencies; bootstrap the package

**Files:**
- Create: `internal/userstore/store.go`
- Create: `internal/userstore/store_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add new module dependencies**

Run from repo root:
```bash
go get modernc.org/sqlite@latest
go get github.com/oklog/ulid/v2@latest
go mod tidy
```

Verify `go.mod` now contains both. Run:
```bash
go list -m modernc.org/sqlite github.com/oklog/ulid/v2 golang.org/x/crypto
```
All three must print versions.

- [ ] **Step 2: Write the failing skeleton test**

Create `internal/userstore/store_test.go`:

```go
package userstore

import (
	"context"
	"testing"
)

func TestOpenInMemory_RunsMigrations(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// At minimum, schema_migrations should exist after Open.
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations table missing: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected at least one migration applied, got 0")
	}
}
```

- [ ] **Step 3: Run the test; expect failure**

```bash
go test ./internal/userstore/...
```
Expected: build fails ("undefined: Open") or test fails with similar error.

- [ ] **Step 4: Write the minimal `store.go`**

Create `internal/userstore/store.go`:

```go
// Package userstore is the relay's account / token / session database layer.
// It is the only package that imports a SQLite driver or writes SQL. Other
// packages depend on the Store interface, which lets tests substitute an
// in-memory implementation without touching SQLite directly.
package userstore

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SQLiteStore is the production Store backed by a single SQLite file.
type SQLiteStore struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs any pending
// migrations. Pass ":memory:" for tests. WAL mode is enabled on file-backed
// databases; tests against ":memory:" fall back to the default journal.
func Open(ctx context.Context, path string) (*SQLiteStore, error) {
	dsn := path
	if path != ":memory:" {
		dsn = path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)"
	} else {
		dsn = path + "?_pragma=foreign_keys(on)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	s := &SQLiteStore{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var seen int
		if err := s.db.QueryRowContext(ctx,
			`SELECT count(*) FROM schema_migrations WHERE name=?`, name,
		).Scan(&seen); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if seen > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(name, applied_at) VALUES(?, strftime('%s','now'))`,
			name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}
```

- [ ] **Step 5: Create the first migration file**

Create `internal/userstore/migrations/0001_init.sql`:

```sql
CREATE TABLE users (
    id             TEXT PRIMARY KEY,
    email          TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash  TEXT NOT NULL,
    csrf_secret    BLOB NOT NULL,
    created_at     INTEGER NOT NULL,
    disabled_at    INTEGER
);

CREATE TABLE invitations (
    code_hash      TEXT PRIMARY KEY,
    created_by     TEXT NOT NULL,
    created_at     INTEGER NOT NULL,
    expires_at     INTEGER,
    consumed_at    INTEGER,
    consumed_by    TEXT REFERENCES users(id),
    note           TEXT
);

CREATE TABLE api_tokens (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    token_hash     TEXT NOT NULL UNIQUE,
    token_prefix   TEXT NOT NULL,
    created_at     INTEGER NOT NULL,
    last_used_at   INTEGER,
    revoked_at     INTEGER
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id) WHERE revoked_at IS NULL;

CREATE TABLE web_sessions (
    id_hash        TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at     INTEGER NOT NULL,
    expires_at     INTEGER NOT NULL,
    user_agent     TEXT,
    ip_prefix      TEXT
);
CREATE INDEX idx_web_sessions_user ON web_sessions(user_id);
CREATE INDEX idx_web_sessions_expires ON web_sessions(expires_at);
```

- [ ] **Step 6: Run the test; expect pass**

```bash
go test ./internal/userstore/...
```
Expected: PASS.

- [ ] **Step 7: Add an idempotent-migration test**

Append to `store_test.go`:

```go
func TestOpenInMemory_MigrationIdempotent(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	// run migrate() again; it should be a no-op
	if err := s.migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 migration row after re-run, got %d", n)
	}
}
```

Run: `go test ./internal/userstore/...` — expected PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/userstore go.mod go.sum
git commit -m "feat(userstore): bootstrap package with sqlite + migration runner"
```

---

### Task 1.1: `Secret[T]` newtype with redacting verbs

**Files:**
- Create: `internal/userstore/secret.go`
- Create: `internal/userstore/secret_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/userstore/secret_test.go`:

```go
package userstore

import (
	"fmt"
	"strings"
	"testing"
)

func TestSecret_RedactsAllVerbs(t *testing.T) {
	s := NewSecret("atk_supersecretvalueXYZ", "atk_")
	for _, verb := range []string{"%s", "%v", "%+v", "%#v", "%q"} {
		out := fmt.Sprintf(verb, s)
		if strings.Contains(out, "supersecret") {
			t.Fatalf("verb %s leaked plaintext: %q", verb, out)
		}
		if !strings.Contains(out, "atk_") {
			t.Fatalf("verb %s lost prefix: %q", verb, out)
		}
	}
}

func TestSecret_ExposeReturnsPlaintext(t *testing.T) {
	s := NewSecret("atk_supersecretvalueXYZ", "atk_")
	if s.Expose() != "atk_supersecretvalueXYZ" {
		t.Fatalf("Expose returned %q", s.Expose())
	}
}

func TestSecret_PrefixOnlyShownInUI(t *testing.T) {
	s := NewSecret("atk_abcdefghij1234", "atk_")
	if got, want := s.Prefix(), "atk_abcd"; got != want {
		t.Fatalf("Prefix: got %q want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the test; expect failure**

```bash
go test ./internal/userstore/ -run TestSecret -v
```
Expected: fails to compile ("undefined: NewSecret").

- [ ] **Step 3: Implement `secret.go`**

Create `internal/userstore/secret.go`:

```go
package userstore

// Secret wraps a credential string so that accidental logging or
// fmt-formatting redacts the value. The plaintext is only available via
// the explicit Expose() method, which makes leak sites grep-able.
//
// Layout: a fixed prefix (e.g. "atk_") is preserved for UI listings; the
// rest is replaced by an ellipsis in every fmt verb.
type Secret struct {
	plain  string
	prefix string
}

// NewSecret captures plaintext with a UI-visible prefix. The prefix must
// be a literal substring of plaintext and is shown as-is in fmt output.
func NewSecret(plain, prefix string) Secret {
	return Secret{plain: plain, prefix: prefix}
}

// Expose returns the plaintext. Call sites should grep for ".Expose()"
// to audit credential flow.
func (s Secret) Expose() string { return s.plain }

// Prefix returns the prefix plus the first 4 chars of the secret body
// for UI listings (e.g. "atk_a1b2"). Total length is len(prefix)+4.
func (s Secret) Prefix() string {
	if len(s.plain) < len(s.prefix)+4 {
		return s.prefix
	}
	return s.plain[:len(s.prefix)+4]
}

// String implements fmt.Stringer for %s and %v verbs.
func (s Secret) String() string { return s.Prefix() + "…" }

// GoString implements fmt.GoStringer for %#v.
func (s Secret) GoString() string { return "userstore.Secret(" + s.Prefix() + "…)" }
```

- [ ] **Step 4: Run the test; expect pass**

```bash
go test ./internal/userstore/ -run TestSecret -v
```
Expected: all three subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/secret.go internal/userstore/secret_test.go
git commit -m "feat(userstore): add Secret newtype that redacts fmt verbs"
```

---

### Task 1.2: ULID helper

**Files:**
- Create: `internal/userstore/ulid.go`
- Create: `internal/userstore/ulid_test.go`

- [ ] **Step 1: Failing test**

Create `internal/userstore/ulid_test.go`:

```go
package userstore

import (
	"strings"
	"testing"
	"time"
)

func TestNewID_MonotonicallyIncreasingWhenSequential(t *testing.T) {
	g := newIDGen(time.Now)
	prev := g.New()
	for i := 0; i < 100; i++ {
		got := g.New()
		if got <= prev {
			t.Fatalf("ULIDs not monotonic: %s !> %s", got, prev)
		}
		prev = got
	}
}

func TestNewID_ULIDFormat(t *testing.T) {
	g := newIDGen(time.Now)
	id := g.New()
	if len(id) != 26 {
		t.Fatalf("ULID length: got %d, want 26 (%q)", len(id), id)
	}
	// ULID alphabet is Crockford base32: no I, L, O, U.
	for _, c := range strings.ToUpper(id) {
		if strings.ContainsRune("ILOU", c) {
			t.Fatalf("ULID contains forbidden char %q in %q", c, id)
		}
	}
}
```

- [ ] **Step 2: Run; expect failure**

```bash
go test ./internal/userstore/ -run TestNewID -v
```
Expected: build error ("undefined: newIDGen").

- [ ] **Step 3: Implement `ulid.go`**

Create `internal/userstore/ulid.go`:

```go
package userstore

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// idGen wraps oklog/ulid's monotonic source. The time function is
// injectable so tests can pin "now".
type idGen struct {
	mu   sync.Mutex
	mono *ulid.MonotonicEntropy
	now  func() time.Time
}

func newIDGen(now func() time.Time) *idGen {
	return &idGen{
		mono: ulid.Monotonic(rand.Reader, 0),
		now:  now,
	}
}

// New returns a 26-char ULID string. Safe for concurrent use.
func (g *idGen) New() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(g.now()), g.mono).String()
}

// defaultIDs is used by all store CRUD; tests construct their own gen.
var defaultIDs = newIDGen(time.Now)
```

- [ ] **Step 4: Run; expect pass**

```bash
go test ./internal/userstore/ -run TestNewID -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/ulid.go internal/userstore/ulid_test.go
git commit -m "feat(userstore): add monotonic ULID generator"
```

---

### Task 1.3: `users` CRUD with argon2id

**Files:**
- Create: `internal/userstore/users.go`
- Create: `internal/userstore/users_test.go`

- [ ] **Step 1: Failing tests**

Create `internal/userstore/users_test.go`:

```go
package userstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateUser_HashesPasswordAndStoresCSRFSecret(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	u, err := s.CreateUser(ctx, "Alice@Example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == "" || len(u.ID) != 26 {
		t.Fatalf("user id not ULID: %q", u.ID)
	}
	if u.Email != "alice@example.com" {
		t.Fatalf("email not lowercased: %q", u.Email)
	}

	// password_hash must not contain plaintext substring
	var hash string
	if err := s.db.QueryRowContext(ctx,
		`SELECT password_hash FROM users WHERE id=?`, u.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "correcthorse") {
		t.Fatalf("password_hash leaks plaintext: %s", hash)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("password_hash not argon2id: %s", hash)
	}

	// csrf_secret must be 32 random bytes
	var secret []byte
	if err := s.db.QueryRowContext(ctx,
		`SELECT csrf_secret FROM users WHERE id=?`, u.ID).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if len(secret) != 32 {
		t.Fatalf("csrf_secret length: got %d want 32", len(secret))
	}
}

func TestCreateUser_DuplicateEmailReturnsErr(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.CreateUser(ctx, "a@b.com", "pw-correcthorsestaple"); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateUser(ctx, "A@B.com", "pw-correcthorsestaple")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

func TestVerifyPassword_Success(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pw := "correcthorsebatterystaple"
	u, err := s.CreateUser(ctx, "a@b.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := s.VerifyPassword(ctx, "a@b.com", pw)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if verified == nil || verified.ID != u.ID {
		t.Fatalf("VerifyPassword returned wrong user: %+v", verified)
	}
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, _ = s.CreateUser(ctx, "a@b.com", "correcthorsebatterystaple")
	verified, err := s.VerifyPassword(ctx, "a@b.com", "wrong-password-attempt")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if verified != nil {
		t.Fatalf("VerifyPassword should return nil user on wrong pw, got %+v", verified)
	}
}

func TestVerifyPassword_MissingEmailRunsArgon2(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// No user inserted. Should still run argon2id work (verified=nil, err=nil).
	verified, err := s.VerifyPassword(ctx, "nobody@example.com", "any")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if verified != nil {
		t.Fatalf("missing-email VerifyPassword should return nil, got %+v", verified)
	}
}
```

- [ ] **Step 2: Run; expect failure**

```bash
go test ./internal/userstore/ -run TestCreateUser -v
```
Expected: build errors ("undefined: CreateUser", "ErrEmailTaken").

- [ ] **Step 3: Implement `users.go`**

Create `internal/userstore/users.go`:

```go
package userstore

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var (
	ErrEmailTaken    = errors.New("userstore: email already registered")
	ErrUserNotFound  = errors.New("userstore: user not found")
)

// User is the row shape exposed to callers. password_hash and csrf_secret
// are intentionally not exported.
type User struct {
	ID         string
	Email      string
	CreatedAt  time.Time
	DisabledAt *time.Time
	csrfSecret []byte // populated by internal lookups; CSRFSecret() exposes
}

func (u *User) CSRFSecret() []byte { return u.csrfSecret }

// argonParams are the SEC-2 fixed parameters. Stored hashes carry these
// inline so future tuning does not invalidate old hashes.
var argonParams = struct {
	time, memory uint32
	threads      uint8
	keyLen       uint32
}{time: 3, memory: 64 * 1024, threads: 2, keyLen: 32}

// dummyHash is generated once at process start so missing-email login
// paths spend the same wall-clock time as a real verify (SEC-3).
var dummyHash = func() string {
	h, err := hashPassword("missing-email-dummy-value-9e8d7c6b5a")
	if err != nil {
		panic("userstore: dummy hash init failed: " + err.Error())
	}
	return h
}()

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt,
		argonParams.time, argonParams.memory, argonParams.threads, argonParams.keyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonParams.memory, argonParams.time, argonParams.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func verifyPassword(pw, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, t uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &t, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// CreateUser inserts a new user with the given email and password.
// Email is lowercased for storage; uniqueness is case-insensitive via
// COLLATE NOCASE on the column.
func (s *SQLiteStore) CreateUser(ctx context.Context, email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	hash, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash: %w", err)
	}
	csrfSecret := make([]byte, 32)
	if _, err := rand.Read(csrfSecret); err != nil {
		return nil, fmt.Errorf("csrf rand: %w", err)
	}
	id := defaultIDs.New()
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users(id, email, password_hash, csrf_secret, created_at)
		 VALUES(?, ?, ?, ?, ?)`,
		id, email, hash, csrfSecret, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: users.email") {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return &User{
		ID: id, Email: email,
		CreatedAt:  time.Unix(now, 0),
		csrfSecret: csrfSecret,
	}, nil
}

// VerifyPassword returns the matched user on success, or (nil, nil) when
// either the email does not exist OR the password is wrong. Both paths
// run argon2id verification against either the real hash or the global
// dummyHash, so wall-clock time is independent of email existence.
func (s *SQLiteStore) VerifyPassword(ctx context.Context, email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var (
		id          string
		hash        string
		csrfSecret  []byte
		createdAt   int64
		disabledAt  sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, password_hash, csrf_secret, created_at, disabled_at
		 FROM users WHERE email = ?`, email,
	).Scan(&id, &hash, &csrfSecret, &createdAt, &disabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		// Constant-time dummy verify, ignore result.
		_ = verifyPassword(password, dummyHash)
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if disabledAt.Valid {
		// Run dummy verify anyway for timing parity.
		_ = verifyPassword(password, dummyHash)
		return nil, nil
	}
	if !verifyPassword(password, hash) {
		return nil, nil
	}
	u := &User{
		ID: id, Email: email,
		CreatedAt:  time.Unix(createdAt, 0),
		csrfSecret: csrfSecret,
	}
	return u, nil
}

// GetUser by id; returns ErrUserNotFound if missing.
func (s *SQLiteStore) GetUser(ctx context.Context, id string) (*User, error) {
	var (
		email      string
		createdAt  int64
		disabledAt sql.NullInt64
		secret     []byte
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT email, csrf_secret, created_at, disabled_at
		 FROM users WHERE id = ?`, id,
	).Scan(&email, &secret, &createdAt, &disabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	} else if err != nil {
		return nil, err
	}
	u := &User{
		ID: id, Email: email,
		CreatedAt:  time.Unix(createdAt, 0),
		csrfSecret: secret,
	}
	if disabledAt.Valid {
		t := time.Unix(disabledAt.Int64, 0)
		u.DisabledAt = &t
	}
	return u, nil
}

// DisableUser sets disabled_at = now. Idempotent.
func (s *SQLiteStore) DisableUser(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET disabled_at = strftime('%s','now')
		 WHERE id = ? AND disabled_at IS NULL`, id)
	return err
}
```

- [ ] **Step 4: Run; expect pass**

```bash
go test ./internal/userstore/ -run TestCreateUser -v
go test ./internal/userstore/ -run TestVerifyPassword -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/users.go internal/userstore/users_test.go
git commit -m "feat(userstore): user CRUD with argon2id + timing-safe verify"
```

---

> **Task body convention from here on:** Tasks 1.0–1.3 above show the full TDD cadence (failing test code → implementation code → run commands → commit). Tasks from 1.4 onward keep the same TDD cadence but list only the *delta* against what is already in the repo + spec — full code blocks for new files, signature-only for additions to existing files, plus test names and `assert` summaries. When in doubt, refer back to Tasks 1.0–1.3 for the exact pattern. The spec at `docs/superpowers/specs/2026-05-15-saas-user-accounts-design.md` is authoritative for any detail not captured here.

---

### Task 1.4: `invitations` — issue, consume (txn-safe), expire

**Files:**
- Create: `internal/userstore/invitations.go`
- Create: `internal/userstore/invitations_test.go`

Implements spec §5.1 (admin issues invite) and §5.2 step 4 (atomic consume).

- [ ] **Step 1: Failing tests**

Create `internal/userstore/invitations_test.go` with these test functions:

- `TestCreateInvitation_ReturnsPlaintextOnce`: call `CreateInvitation(ctx, expiresAt=nil, note="bob")` → assert returned `Secret` starts with `"inv_"` and has 26+ chars total; assert the row's `code_hash` does **not** contain the plaintext substring.
- `TestConsumeInvitation_OneShotUnderConcurrency`: create one invitation; spawn 50 goroutines that each call `ConsumeInvitation(ctx, code, userID=fmt.Sprintf("u%d", i))`; assert exactly one returns `nil` error and 49 return `ErrInviteInvalid`; assert `invitations.consumed_at` is non-NULL.
- `TestConsumeInvitation_Expired`: create with `expiresAt = now - 1h`; consume → `ErrInviteInvalid`; row's `consumed_at` remains NULL.
- `TestConsumeInvitation_BadCode`: consume with random gibberish → `ErrInviteInvalid`.
- `TestListInvitations_AdminView`: create 3 invitations (one consumed, one expired, one open); `ListInvitations(ctx)` returns 3 rows with status flags.

Each test uses `newTestStore(t)` from `users_test.go`. Add `var _ = ErrEmailTaken` placeholder import if needed.

Run: `go test ./internal/userstore/ -run TestCreateInvitation -run TestConsumeInvitation -run TestListInvitations -v` — expect build failure.

- [ ] **Step 2: Implement `invitations.go`**

Public surface:

```go
var ErrInviteInvalid = errors.New("userstore: invitation invalid or already consumed")

type Invitation struct {
    CodeHash    string      // exposed for admin list; never reverse-able
    CodePrefix  string      // "inv_" + first 4 chars of base32 body (for admin UI hinting)
    Note        string
    CreatedAt   time.Time
    ExpiresAt   *time.Time
    ConsumedAt  *time.Time
    ConsumedBy  string
}

func (s *SQLiteStore) CreateInvitation(ctx context.Context, expiresAt *time.Time, note string) (Secret, *Invitation, error)
func (s *SQLiteStore) ConsumeInvitation(ctx context.Context, plaintext string, userID string) error
func (s *SQLiteStore) ListInvitations(ctx context.Context) ([]Invitation, error)
```

Implementation rules:
- Generate code: `crypto/rand` 10 bytes → base32 (no padding) → prefix `inv_`. Compute `code_hash = sha256(plaintext)` hex-lowercased.
- `CreateInvitation` inserts `(code_hash, 'admin', now, expires_at, NULL, NULL, note)`. Return `NewSecret(plaintext, "inv_")` plus the row metadata (excluding consumed_*).
- `ConsumeInvitation` runs in a single statement: `UPDATE invitations SET consumed_at=?, consumed_by=? WHERE code_hash=? AND consumed_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`. Check `RowsAffected()` — 0 means race lost OR expired OR bad code, return `ErrInviteInvalid` indistinguishably.
- `ListInvitations` returns most recent first; `note` column may be empty.

- [ ] **Step 3: Run; expect pass**

```bash
go test ./internal/userstore/ -v
```
All tests including users + secrets + invitations PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/userstore/invitations.go internal/userstore/invitations_test.go
git commit -m "feat(userstore): invitations with txn-safe one-shot consume"
```

---

### Task 1.5: `api_tokens` — issue plaintext-once, lookup by hash, revoke

**Files:**
- Create: `internal/userstore/apitokens.go`
- Create: `internal/userstore/apitokens_test.go`

Implements spec §5.4. `last_used_at` updates are deferred to Task 3.3 (batch committer).

- [ ] **Step 1: Failing tests**

In `apitokens_test.go`:

- `TestCreateAPIToken_ReturnsPlaintextOnce`: create a user; `CreateAPIToken(ctx, user.ID, "MacBook Air")` → assert returned `Secret` starts with `"atk_"`; assert `secret.Expose()` has total length ≥ 40 chars; assert the stored `token_hash` does not contain the plaintext.
- `TestLookupAPIToken_ValidReturnsUser`: with the plaintext above, `LookupAPIToken(ctx, plaintext)` returns `(tokenID, userID, nil)`.
- `TestLookupAPIToken_RevokedRejected`: revoke via `RevokeAPIToken(ctx, tokenID, user.ID)` → re-lookup → `ErrTokenInvalid`.
- `TestLookupAPIToken_UserDisabled`: disable the user → lookup → `ErrTokenInvalid`.
- `TestLookupAPIToken_BadPlaintext`: lookup gibberish → `ErrTokenInvalid`.
- `TestListAPITokens_HidesPlaintext`: create 2 tokens, list → returned `APIToken` rows carry `Prefix` (UI hint) but no `token_hash` exposure beyond what is needed; no plaintext-shaped field.

Run: `go test ./internal/userstore/ -run TestAPIToken -v` — expect build failure.

- [ ] **Step 2: Implement `apitokens.go`**

Public surface:

```go
var ErrTokenInvalid = errors.New("userstore: api token invalid, revoked, or owner disabled")

type APIToken struct {
    ID          string
    UserID      string
    Name        string
    Prefix      string     // "atk_xxxx" (12 chars total) for UI listings
    CreatedAt   time.Time
    LastUsedAt  *time.Time
    RevokedAt   *time.Time
}

func (s *SQLiteStore) CreateAPIToken(ctx context.Context, userID, name string) (Secret, *APIToken, error)
func (s *SQLiteStore) LookupAPIToken(ctx context.Context, plaintext string) (tokenID, userID string, err error)
func (s *SQLiteStore) RevokeAPIToken(ctx context.Context, tokenID, userID string) error
func (s *SQLiteStore) ListAPITokens(ctx context.Context, userID string) ([]APIToken, error)
func (s *SQLiteStore) TouchAPIToken(ctx context.Context, tokenID string) error  // updates last_used_at; used by Task 3.3
```

Implementation rules:
- Generate token: `crypto/rand` 32 bytes → base64url no padding (≈43 chars) → prefix `atk_`. `token_prefix = "atk_" + body[:8]`. `token_hash = hex.EncodeToString(sha256.Sum256([]byte(plaintext)))`.
- `LookupAPIToken` joins `api_tokens` and `users`: `WHERE token_hash=? AND revoked_at IS NULL AND users.disabled_at IS NULL`.
- `RevokeAPIToken` checks `user_id` matches (prevents one user from revoking another's tokens).
- `TouchAPIToken` is a separate single statement update; it does **not** block the lookup path.

- [ ] **Step 3: Run; expect pass**

```bash
go test ./internal/userstore/ -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/userstore/apitokens.go internal/userstore/apitokens_test.go
git commit -m "feat(userstore): api tokens with plaintext-once issuance"
```

---

### Task 1.6: `web_sessions` — cookie session table + sliding renewal

**Files:**
- Create: `internal/userstore/websessions.go`
- Create: `internal/userstore/websessions_test.go`

Implements spec §4.1.

- [ ] **Step 1: Failing tests**

In `websessions_test.go`:

- `TestCreateWebSession_StoresHashNotPlaintext`: `CreateWebSession(ctx, user.ID, "Mozilla/5.0", "1.2.3.0/24")` returns a `Secret` (cookie value, 32 bytes base64url, no prefix). Query the row: `id_hash` is sha256 hex of the plaintext, not the plaintext itself.
- `TestLookupWebSession_ValidReturnsUserIDAndRenews`: call lookup; record `expires_at_before`; sleep 10ms; call lookup again; assert `expires_at_after > expires_at_before` (sliding renewal pushes `expires_at` to `now + 30d` on each successful lookup).
- `TestLookupWebSession_Expired`: insert a session with `expires_at = now - 1h` directly via SQL; lookup → `ErrWebSessionInvalid`.
- `TestLookupWebSession_UserDisabled`: disable the user → lookup → `ErrWebSessionInvalid`.
- `TestDeleteWebSession_RevokesCookie`: `DeleteWebSession(ctx, plaintext)`; subsequent lookup → `ErrWebSessionInvalid`.
- `TestPurgeExpiredWebSessions_DeletesOldRows`: insert 3 expired + 2 fresh → `PurgeExpiredWebSessions(ctx)` returns 3; row count is 2.

Run: `go test ./internal/userstore/ -run TestWebSession -v` — expect build failure.

- [ ] **Step 2: Implement `websessions.go`**

Public surface:

```go
var ErrWebSessionInvalid = errors.New("userstore: web session not found, expired, or user disabled")

const webSessionTTL = 30 * 24 * time.Hour

func (s *SQLiteStore) CreateWebSession(ctx context.Context, userID, userAgent, ipPrefix string) (Secret, error)
func (s *SQLiteStore) LookupWebSession(ctx context.Context, plaintext string) (userID string, csrfSecret []byte, err error)
func (s *SQLiteStore) DeleteWebSession(ctx context.Context, plaintext string) error
func (s *SQLiteStore) PurgeExpiredWebSessions(ctx context.Context) (int64, error)
```

Implementation rules:
- Cookie value: 32 bytes `crypto/rand` → base64url no padding (≈43 chars). `NewSecret(value, "")` — no UI prefix needed (cookie is never displayed).
- `id_hash` = sha256 hex of plaintext.
- `LookupWebSession`: single statement that JOINs users for the disabled check and runs `UPDATE web_sessions SET expires_at = ? WHERE id_hash=?` after the SELECT, all in one transaction. Returns the user's `csrf_secret` so callers can validate CSRF headers without a second query.
- `PurgeExpiredWebSessions` is called by a background goroutine in Task 3.0; this task just provides the method.

- [ ] **Step 3: Run; expect pass**

```bash
go test ./internal/userstore/ -v
```

- [ ] **Step 4: Verify the full P1 suite is green**

```bash
go vet -tags webkit2_41 ./internal/userstore/...
go test -tags webkit2_41 -race -timeout 60s ./internal/userstore/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/websessions.go internal/userstore/websessions_test.go
git commit -m "feat(userstore): web_sessions with sliding renewal"
```

---

### Task 1.7: Expose `Store` interface for downstream packages

**Files:**
- Modify: `internal/userstore/store.go`
- Create: `internal/userstore/store_iface_test.go`

The `internal/relay` package must depend on a `Store` interface, not the concrete `*SQLiteStore`, so identity tests can substitute a memory implementation (spec §2.1 rule 2).

- [ ] **Step 1: Failing compile check**

Create `internal/userstore/store_iface_test.go`:

```go
package userstore

// Compile-time assertion that *SQLiteStore implements Store. This file
// has no runtime tests; if the assignment fails to compile, the contract
// is broken.
var _ Store = (*SQLiteStore)(nil)
```

- [ ] **Step 2: Add the interface to `store.go`**

Append to `internal/userstore/store.go`:

```go
// Store is the dependency-inversion seam between internal/relay and the
// concrete SQLite implementation. Tests in internal/relay can substitute
// a memory implementation that satisfies this interface.
type Store interface {
    // Users
    CreateUser(ctx context.Context, email, password string) (*User, error)
    VerifyPassword(ctx context.Context, email, password string) (*User, error)
    GetUser(ctx context.Context, id string) (*User, error)
    DisableUser(ctx context.Context, id string) error

    // Invitations
    CreateInvitation(ctx context.Context, expiresAt *time.Time, note string) (Secret, *Invitation, error)
    ConsumeInvitation(ctx context.Context, plaintext, userID string) error
    ListInvitations(ctx context.Context) ([]Invitation, error)

    // API tokens
    CreateAPIToken(ctx context.Context, userID, name string) (Secret, *APIToken, error)
    LookupAPIToken(ctx context.Context, plaintext string) (tokenID, userID string, err error)
    RevokeAPIToken(ctx context.Context, tokenID, userID string) error
    ListAPITokens(ctx context.Context, userID string) ([]APIToken, error)
    TouchAPIToken(ctx context.Context, tokenID string) error

    // Web sessions (cookie)
    CreateWebSession(ctx context.Context, userID, userAgent, ipPrefix string) (Secret, error)
    LookupWebSession(ctx context.Context, plaintext string) (userID string, csrfSecret []byte, err error)
    DeleteWebSession(ctx context.Context, plaintext string) error
    PurgeExpiredWebSessions(ctx context.Context) (int64, error)

    Close() error
}
```

- [ ] **Step 3: Run**

```bash
go vet -tags webkit2_41 ./internal/userstore/...
go build ./...
```
Both must pass. The compile-time assertion in step 1 guarantees coverage.

- [ ] **Step 4: Commit**

```bash
git add internal/userstore/store.go internal/userstore/store_iface_test.go
git commit -m "feat(userstore): expose Store interface for relay layer"
```

---

## Phase P2: relay identity resolution + CSRF middleware

End state: any incoming HTTP / WS-upgrade request resolves to a `Principal`, and every mutating route on the mux is gated by CSRF.

### Task 2.0: `Principal` + `resolveIdentity`

**Files:**
- Create: `internal/relay/identity.go`
- Create: `internal/relay/identity_test.go`

Implements spec §4 and §4.3.

- [ ] **Step 1: Failing tests**

In `identity_test.go`, build a table-driven test against a fake `userstore.Store`:

- Helpers: a `fakeStore` struct implementing `userstore.Store` with maps for users / api_tokens / web_sessions (only the methods needed by `resolveIdentity`); a `req(headers...)` builder.
- Cases:
  - `empty` → `Principal{Kind:None}`
  - `valid cookie` → `Principal{Kind:User, UserID:"u1", Scope:write}`; cookie sliding-renewal called once
  - `expired cookie (LookupWebSession returns ErrWebSessionInvalid)` → `Principal{Kind:None}`
  - `valid Authorization: Bearer atk_xxx` → `Principal{Kind:User, UserID:"u1", TokenID:"tok1", Scope:write}`
  - `revoked api token` → `Principal{Kind:None}`
  - `admin token in Authorization` → `Principal{Kind:Admin, Scope:write}`
  - `admin token with wrong case` → `Principal{Kind:None}` (constant-time exact match)
  - `Sec-WebSocket-Protocol: atterm-token.atk_xxx` → same as `Authorization: Bearer`
  - `Sec-WebSocket-Protocol: atterm-token-b64.<base64url>` → decodes and resolves to `User`
  - `cookie + Authorization both present` → cookie wins; assert resolveIdentity does NOT call `LookupAPIToken`

Run: `go test ./internal/relay/ -run TestResolveIdentity -v` — expect build failure.

- [ ] **Step 2: Implement `identity.go`**

Skeleton:

```go
package relay

import (
    "crypto/subtle"
    "encoding/base64"
    "net/http"
    "strings"

    "github.com/attson/atterm/internal/userstore"  // adjust import path
)

type PrincipalKind uint8

const (
    PrincipalNone PrincipalKind = iota
    PrincipalUser
    PrincipalAdmin
)

type authScope uint8
const (
    scopeNone  authScope = 0
    scopeRead  authScope = 1
    scopeWrite authScope = 2
)

type Principal struct {
    Kind       PrincipalKind
    UserID     string
    TokenID    string  // non-empty when User came from an api token (not cookie)
    Scope      authScope
    CSRFSecret []byte  // set when Kind==User and source was cookie
}

// IdentityResolver is constructed once at relay startup and used by every
// handler. adminToken is the fixed ATTERM_ADMIN_TOKEN string; empty disables
// the admin path entirely (loopback dev mode).
type IdentityResolver struct {
    store      userstore.Store
    adminToken string
}

func NewIdentityResolver(store userstore.Store, adminToken string) *IdentityResolver {
    return &IdentityResolver{store: store, adminToken: adminToken}
}

// Resolve returns the Principal for the request. It never returns an error;
// failure modes resolve to Principal{Kind:None}.
func (r *IdentityResolver) Resolve(req *http.Request) Principal {
    // 1. Cookie (highest precedence; see spec §4)
    if c, err := req.Cookie("atterm_session"); err == nil && c.Value != "" {
        if userID, csrfSecret, err := r.store.LookupWebSession(req.Context(), c.Value); err == nil {
            return Principal{
                Kind: PrincipalUser, UserID: userID,
                Scope: scopeWrite, CSRFSecret: csrfSecret,
            }
        }
    }
    // 2. Authorization Bearer or WS subprotocol token
    if tok := tokenFromHeader(req); tok != "" {
        if r.adminToken != "" &&
            subtle.ConstantTimeCompare([]byte(tok), []byte(r.adminToken)) == 1 {
            return Principal{Kind: PrincipalAdmin, Scope: scopeWrite}
        }
        if tokenID, userID, err := r.store.LookupAPIToken(req.Context(), tok); err == nil {
            return Principal{
                Kind: PrincipalUser, UserID: userID,
                TokenID: tokenID, Scope: scopeWrite,
            }
        }
    }
    return Principal{Kind: PrincipalNone}
}

func tokenFromHeader(req *http.Request) string {
    if h := req.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
        return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
    }
    if p := req.Header.Get("Sec-WebSocket-Protocol"); p != "" {
        for _, part := range strings.Split(p, ",") {
            part = strings.TrimSpace(part)
            if strings.HasPrefix(part, "atterm-token.") {
                return strings.TrimPrefix(part, "atterm-token.")
            }
            if strings.HasPrefix(part, "atterm-token-b64.") {
                if dec, err := base64.RawURLEncoding.DecodeString(
                    strings.TrimPrefix(part, "atterm-token-b64."),
                ); err == nil {
                    return string(dec)
                }
            }
        }
    }
    return ""
}
```

Note: `tokenFromHeader` deliberately does **not** read URL query params (red line #9 carries forward).

- [ ] **Step 3: Run; expect pass**

```bash
go test ./internal/relay/ -run TestResolveIdentity -race -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/relay/identity.go internal/relay/identity_test.go
git commit -m "feat(relay): Principal type and resolveIdentity"
```

---

### Task 2.1: CSRF middleware + mux-enumeration test

**Files:**
- Create: `internal/relay/csrfmw.go`
- Create: `internal/relay/csrfmw_test.go`

Implements spec §4.2 and unbreakable invariant SEC-4.

- [ ] **Step 1: Failing tests**

In `csrfmw_test.go`:

- `TestRequireCSRF_OkOnMatchingHeader`: build a request with a known cookie session + correct CSRF header (`base64url(sha256(cookieValue || csrfSecret))`); `Require(h)` calls the inner handler and returns its status.
- `TestRequireCSRF_MissingHeader_403`: same request without `X-CSRF-Token` → 403.
- `TestRequireCSRF_WrongToken_403`.
- `TestRequireCSRF_GETBypasses`: GET / HEAD do not require the header.
- `TestRequireCSRF_NoCookieReturns401`: cookie missing → 401 (handler treats it as unauthenticated).
- `TestMuxEnumerator_EveryMutatingRouteWrapped`: build the production mux via the function the server uses (Task 3.0 will provide `BuildMux(...)`); walk every registered route; for each `POST`/`PUT`/`DELETE`/`PATCH`, fire a request lacking the CSRF header and assert the response is 403, 401, or 405 (any acceptable rejection — not 200). Routes the test specifically tolerates: `/api/auth/signup`, `/api/auth/login`. Anything else without protection fails the test.

The mux-enumerator test is the most important; it is the guarantee that no future handler addition silently skips CSRF.

Run: `go test ./internal/relay/ -run TestRequireCSRF -run TestMuxEnumerator -v` — expect failures.

- [ ] **Step 2: Implement `csrfmw.go`**

```go
package relay

import (
    "crypto/sha256"
    "crypto/subtle"
    "encoding/base64"
    "net/http"
)

// CSRFToken derives the per-session CSRF token used by both the server
// and the client. Defined publicly so the /api/me handler can return it
// to the frontend.
func CSRFToken(cookieValue string, csrfSecret []byte) string {
    h := sha256.Sum256(append([]byte(cookieValue), csrfSecret...))
    return base64.RawURLEncoding.EncodeToString(h[:])
}

// RequireCSRF gates mutating routes. For GET/HEAD it is a no-op. For all
// other methods, it requires a non-empty cookie session AND a matching
// X-CSRF-Token header.
func RequireCSRF(resolver *IdentityResolver, inner http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet, http.MethodHead, http.MethodOptions:
            inner.ServeHTTP(w, r)
            return
        }
        c, err := r.Cookie("atterm_session")
        if err != nil || c.Value == "" {
            http.Error(w, "unauthenticated", http.StatusUnauthorized)
            return
        }
        p := resolver.Resolve(r)
        if p.Kind != PrincipalUser || len(p.CSRFSecret) == 0 {
            http.Error(w, "unauthenticated", http.StatusUnauthorized)
            return
        }
        want := CSRFToken(c.Value, p.CSRFSecret)
        got := r.Header.Get("X-CSRF-Token")
        if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
            http.Error(w, "csrf mismatch", http.StatusForbidden)
            return
        }
        inner.ServeHTTP(w, r)
    })
}
```

- [ ] **Step 3: Run; expect pass for `TestRequireCSRF`. The `TestMuxEnumerator` test will remain red until Task 3.0 lands `BuildMux`. Mark it `t.Skip("pending Task 3.0")` for now and remove the skip in Task 3.0.**

```bash
go test ./internal/relay/ -run TestRequireCSRF -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/relay/csrfmw.go internal/relay/csrfmw_test.go
git commit -m "feat(relay): CSRF middleware with per-session token derivation"
```

---

## Phase P3: HTTP API surface

End state: signup / login / logout / me / token CRUD / admin invite + user routes all live and CSRF-gated where appropriate. Rate limits enforced for login / signup / invite-fail.

### Task 3.0: Argon2 work-pool (SEC-3 dummy hash bootstrap + SEC-5 concurrency cap)

**Files:**
- Create: `internal/relay/argon2pool.go`
- Create: `internal/relay/argon2pool_test.go`

Implements spec §6 SEC-3 (startup dummy hash with current params) and §10 (argon2 memory pressure mitigation via semaphore).

- [ ] **Step 1: Failing tests**

In `argon2pool_test.go`:

- `TestArgon2Pool_DummyHashUsesCurrentParams`: call `NewArgon2Pool(runtime.NumCPU())`; assert pool's `DummyHash()` is `$argon2id$v=19$m=65536,t=3,p=2$…`.
- `TestArgon2Pool_RunCapsConcurrency`: spawn 100 goroutines calling `pool.Verify(ctx, "any", pool.DummyHash())`; assert at no point are more than `cap` concurrent invocations in flight (use an atomic counter peeked from a stub `verifyFn`).
- `TestArgon2Pool_QueueDepthOverflow_Returns503Sentinel`: with `cap=1`, fire 50 calls without draining the result chan; the 33rd onward must return `ErrArgon2Overloaded`.

Run: `go test ./internal/relay/ -run TestArgon2Pool -v` — expect build failure.

- [ ] **Step 2: Implement `argon2pool.go`**

```go
package relay

import (
    "context"
    "errors"

    "github.com/attson/atterm/internal/userstore"
    "golang.org/x/sync/semaphore"
)

var ErrArgon2Overloaded = errors.New("relay: argon2 work pool overloaded; retry later")

type Argon2Pool struct {
    sem       *semaphore.Weighted
    queueCap  int64
    inFlight  int64
    dummyHash string
}

const queueDepth = 32

// NewArgon2Pool returns a pool that bounds concurrent argon2id work to
// the given concurrency (typically runtime.NumCPU()). A queueDepth-bounded
// channel sits on top so a flood of requests fails fast with 503 rather
// than blowing up RAM.
func NewArgon2Pool(concurrency int) *Argon2Pool {
    // dummyHash uses userstore's exported helper from Task 1.3.
    h, err := userstore.HashPasswordForBootstrap("dummy-bootstrap")
    if err != nil {
        panic("relay: argon2 dummy bootstrap: " + err.Error())
    }
    return &Argon2Pool{
        sem:       semaphore.NewWeighted(int64(concurrency)),
        queueCap:  int64(concurrency) + queueDepth,
        dummyHash: h,
    }
}

func (p *Argon2Pool) DummyHash() string { return p.dummyHash }

// Verify runs the password verification with concurrency bounded. Returns
// ErrArgon2Overloaded if the queue is full.
func (p *Argon2Pool) Verify(ctx context.Context, plaintext, hash string) (bool, error) {
    if !p.sem.TryAcquire(1) {
        // queue check via atomic counter omitted for brevity; the
        // implementation uses sync/atomic to bound `inFlight + waiting`.
        return false, ErrArgon2Overloaded
    }
    defer p.sem.Release(1)
    return userstore.VerifyPasswordForBootstrap(plaintext, hash), nil
}
```

The internal helpers `userstore.HashPasswordForBootstrap` and `userstore.VerifyPasswordForBootstrap` are exported aliases the pool uses; **add these as exported wrappers** in `internal/userstore/users.go` in this same task (one-line each, forwarding to `hashPassword` / `verifyPassword`).

- [ ] **Step 3: Run; expect pass**

```bash
go test ./internal/relay/ -run TestArgon2Pool -race -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/relay/argon2pool.go internal/relay/argon2pool_test.go internal/userstore/users.go
git commit -m "feat(relay): argon2 work pool with bounded concurrency and dummy bootstrap"
```

---

### Task 3.1: `auth_http.go` — signup / login / logout

**Files:**
- Create: `internal/relay/auth_http.go`
- Create: `internal/relay/auth_http_test.go`

Implements spec §5.2, §5.3.

- [ ] **Step 1: Failing tests**

In `auth_http_test.go`, build an in-memory test relay with a real `*userstore.SQLiteStore` (in :memory:):

- `TestSignup_HappyPath`: create an invite in the store directly; POST `/api/auth/signup` with `{email, password, invite_code}`; expect 200 with `{user_id, email}` and a `Set-Cookie: atterm_session=…` header (HttpOnly, SameSite=Lax).
- `TestSignup_InviteInvalid_400`: signup with random invite → 400 `{error:"invite_invalid"}`; existence-leak guarded (same response for expired and never-existed codes).
- `TestSignup_EmailTaken_409`.
- `TestSignup_PasswordTooShort_400`.
- `TestSignup_NoCSRFRequired`: request without `X-CSRF-Token` succeeds (signup is public; spec §4.2).
- `TestLogin_Success_SetsCookie`.
- `TestLogin_WrongPassword_401_ConstantTime`: time the 401 response and assert ≥ 200ms (spec SEC-5 floor).
- `TestLogin_MissingEmail_TakesSimilarTime`: timing-parity test — login attempts with nonexistent emails must take wall-clock time within ±50ms of real-email failed login (use 5 trials each, compare medians).
- `TestLogout_DeletesWebSession`: login → call `/api/auth/logout` with the CSRF token → cookie cleared, subsequent `/api/me` is 401.

Run: `go test ./internal/relay/ -run TestSignup -run TestLogin -run TestLogout -v` — expect build failure.

- [ ] **Step 2: Implement `auth_http.go`**

Skeleton:

```go
package relay

import (
    "encoding/json"
    "errors"
    "net/http"
    "time"

    "github.com/attson/atterm/internal/userstore"
)

type AuthServer struct {
    Store    userstore.Store
    Resolver *IdentityResolver
    Argon    *Argon2Pool
    // limits *LimitRegistry — added in Task 3.4
    // FailureFloor enforces SEC-5 200ms minimum on failed responses.
    FailureFloor time.Duration  // default 200ms
}

func (a *AuthServer) Routes() http.Handler {
    mux := http.NewServeMux()
    mux.Handle("POST /api/auth/signup", http.HandlerFunc(a.handleSignup))
    mux.Handle("POST /api/auth/login",  http.HandlerFunc(a.handleLogin))
    mux.Handle("POST /api/auth/logout", RequireCSRF(a.Resolver, http.HandlerFunc(a.handleLogout)))
    return mux
}
```

Each handler:
- `handleSignup`: read JSON body; validate email format (RFC 5322 simple regex) and password length ≥ 12; call `Store.ConsumeInvitation` inside a transaction together with `Store.CreateUser`; on success create a web session, set cookie, return 200 JSON. On any failure, sleep until `FailureFloor` elapsed, return appropriate 4xx (existence-leak-protected mapping per spec §5.2).
- `handleLogin`: call `Store.VerifyPassword` (which internally runs argon2 against real or dummy hash); on `(user, nil)` create web session; on `(nil, nil)` return 401 after `FailureFloor` sleep.
- `handleLogout`: read cookie → `Store.DeleteWebSession`; clear cookie via `Set-Cookie: atterm_session=; Max-Age=0`.

Cookie attributes: `HttpOnly; SameSite=Lax; Path=/`. `Secure` is set when `r.TLS != nil` OR `r.Header.Get("X-Forwarded-Proto") == "https"`.

Important: the signup transaction is more subtle than "CreateUser then ConsumeInvitation" — race-safe consume relies on `UPDATE ... WHERE consumed_at IS NULL`. Spec §5.2 step 4 covers this. Use `userstore`'s txn primitives directly (or call them sequentially and accept that the user row may exist when invite consume fails; in that case `DELETE` the user row before returning 400).

- [ ] **Step 3: Run; expect pass**

```bash
go test ./internal/relay/ -run TestSignup -run TestLogin -run TestLogout -race -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/relay/auth_http.go internal/relay/auth_http_test.go
git commit -m "feat(relay): signup/login/logout HTTP endpoints"
```

---

### Task 3.2: `/api/me` + `/api/me/tokens` CRUD

**Files:**
- Modify: `internal/relay/auth_http.go` (add handlers)
- Create: `internal/relay/me_http_test.go`

Implements spec §2.2 (HTTP surface).

- [ ] **Step 1: Failing tests**

In `me_http_test.go`:

- `TestMe_ReturnsUserAndCSRFToken`: login → GET `/api/me` with cookie → 200 `{user_id, email, csrf_token}`. `csrf_token` matches `CSRFToken(cookieValue, csrfSecret)` from Task 2.1.
- `TestMe_RequiresAuth_401`.
- `TestMe_ApiTokenSource_OK`: same but auth via API token instead of cookie — should still return 200 (the desktop's `connected as <email>` flow depends on this). Note: `csrf_token` may be empty in this branch because there is no cookie.
- `TestCreateAPIToken_ReturnsPlaintext`: POST `/api/me/tokens` with `{name:"laptop"}` + CSRF → 201 `{id, plaintext:"atk_…", prefix, created_at}`. Subsequent GET does not include `plaintext` in any row.
- `TestListAPITokens_OK`.
- `TestRevokeAPIToken_OK`: DELETE → 204; next lookup of the same plaintext via internal store fails.
- `TestRevokeAPIToken_CannotRevokeAnotherUsers_404`: user_B tries to revoke user_A's tokenID → 404 (not 403, to avoid existence leak).

Run: `go test ./internal/relay/ -run TestMe -run TestCreateAPIToken -run TestListAPITokens -run TestRevokeAPIToken -v`.

- [ ] **Step 2: Implement the four handlers**

Add to `AuthServer`:
- `handleMe(w, r)` — gated by `Require(PrincipalUser)`; returns JSON.
- `handleListTokens` / `handleCreateToken` / `handleRevokeToken` — all CSRF-gated; user_id sourced from `Principal.UserID`.

Add a tiny route gate helper:
```go
// requireUser ensures the request resolves to PrincipalUser. Returns the
// principal so handlers don't re-resolve.
func (a *AuthServer) requireUser(w http.ResponseWriter, r *http.Request) (Principal, bool) {
    p := a.Resolver.Resolve(r)
    if p.Kind != PrincipalUser {
        http.Error(w, "unauthenticated", http.StatusUnauthorized)
        return p, false
    }
    return p, true
}
```

Routes added:
```go
mux.Handle("GET /api/me",                    http.HandlerFunc(a.handleMe))
mux.Handle("GET /api/me/tokens",             http.HandlerFunc(a.handleListTokens))
mux.Handle("POST /api/me/tokens",            RequireCSRF(a.Resolver, http.HandlerFunc(a.handleCreateToken)))
mux.Handle("DELETE /api/me/tokens/{id}",     RequireCSRF(a.Resolver, http.HandlerFunc(a.handleRevokeToken)))
```

- [ ] **Step 3: Run; expect pass**

```bash
go test ./internal/relay/ -run TestMe -run TestCreate -run TestList -run TestRevoke -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/relay/auth_http.go internal/relay/me_http_test.go
git commit -m "feat(relay): /api/me and /api/me/tokens endpoints"
```

---

### Task 3.3: Admin endpoints — invitations + user management

**Files:**
- Modify: `internal/relay/admin_http.go` — add invitation + user routes
- Create: `internal/relay/admin_http_test.go`

Implements spec §5.1 (invitation creation) and §5.6 (admin password reset).

- [ ] **Step 1: Failing tests**

- `TestAdmin_CreateInvitation`: bearer admin token; POST `/admin/api/invitations` with `{expires_at:null, note:"bob"}` → 201 `{plaintext:"inv_…", code_prefix, expires_at, note}`. Plaintext not retrievable on subsequent GET.
- `TestAdmin_ListInvitations`.
- `TestAdmin_CreateInvitation_RequiresAdmin`: with a user cookie → 401/403.
- `TestAdmin_ListUsers`.
- `TestAdmin_ResetPassword`: POST `/admin/api/users/{id}/reset-password` → 200 `{plaintext:"tmp_…"}`; calling `/api/auth/login` with the new password works; the user's existing cookie web_sessions are gone (spec §5.6 clears them); user's CSRF tokens are invalidated (csrf_secret rotated).
- `TestAdmin_DisableUser`: POST `/admin/api/users/{id}/disable` → 200; the user's login now returns 401.

Run: `go test ./internal/relay/ -run TestAdmin -v` — expect build failure.

- [ ] **Step 2: Add handlers to `admin_http.go`**

Reuse existing `requireAdmin(req)` pattern in the file. New routes:

```go
mux.Handle("POST /admin/api/invitations",                  requireAdmin(a.handleCreateInvite))
mux.Handle("GET  /admin/api/invitations",                  requireAdmin(a.handleListInvites))
mux.Handle("GET  /admin/api/users",                        requireAdmin(a.handleListUsers))
mux.Handle("POST /admin/api/users/{id}/reset-password",    requireAdmin(a.handleResetPassword))
mux.Handle("POST /admin/api/users/{id}/disable",           requireAdmin(a.handleDisableUser))
```

`handleResetPassword` runs the txn described in spec §5.6: generate `tmp_…` plaintext, hash it, `UPDATE users SET password_hash=?, csrf_secret=randomblob(32) WHERE id=?`, `DELETE FROM web_sessions WHERE user_id=?`. Add a store method `ResetUserPassword(ctx, userID, newPlaintext) (Secret, error)` in this task and extend the `Store` interface from Task 1.7 accordingly. Update `store_iface_test.go` automatically passes.

- [ ] **Step 3: Run; expect pass**

```bash
go test ./internal/relay/ -run TestAdmin -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/relay/admin_http.go internal/relay/admin_http_test.go internal/userstore/users.go internal/userstore/store.go
git commit -m "feat(relay): admin invite + user management + password reset"
```

---

### Task 3.4: Rate-limit integration (login / signup / invite-fail) + mux assembly

**Files:**
- Modify: `internal/relay/limits.go` — add three named buckets
- Modify: `internal/relay/server.go` — wire `Argon2Pool`, `AuthServer`, admin routes; expose `BuildMux`
- Modify: `internal/relay/csrfmw_test.go` — remove `t.Skip` from `TestMuxEnumerator` (Task 2.1 step 3)
- Create: `internal/relay/limits_user_test.go`

Implements spec SEC-5.

- [ ] **Step 1: Tests for the limit buckets**

In `limits_user_test.go`:

- `TestLogin_BruteForceLocked`: 11 failed logins from same `(IP, sha256(email))` → 11th returns 429.
- `TestSignup_RatePerIP`: 6 signups same IP within an hour → 6th returns 429.
- `TestInviteFail_RatePerIP`.

Run: `go test ./internal/relay/ -run TestLogin_BruteForceLocked -run TestSignup_RatePerIP -run TestInviteFail -v` — expect failures (no limit wiring yet).

- [ ] **Step 2: Wire limits into the handlers**

Extend `LimitRegistry` (file `limits.go`) with three named buckets:

```go
func (r *LimitRegistry) AllowLoginFailure(ip, emailHash string) bool { ... }  // 10 / 5min
func (r *LimitRegistry) AllowSignup(ip string) bool                  { ... }  // 5 / hour
func (r *LimitRegistry) AllowInviteFail(ip string) bool              { ... }  // 10 / hour
```

In `auth_http.go`, before returning a 4xx response from login/signup/invite-consume failure, call the appropriate `Allow*` and on `false` return 429 (still respecting `FailureFloor` sleep).

- [ ] **Step 3: Implement `BuildMux`**

In `server.go`, factor the route registration into a function the test harness can call:

```go
type ServerDeps struct {
    Store    userstore.Store
    Resolver *IdentityResolver
    Argon    *Argon2Pool
    Limits   *LimitRegistry
    Auth     *AuthServer
}

func BuildMux(d ServerDeps) *http.ServeMux {
    mux := http.NewServeMux()
    d.Auth.RegisterInto(mux)
    // /admin/api/... handlers
    // /uplink, /agent, /client (existing; revisited in Phase P4)
    return mux
}
```

The existing `server.go` should call `BuildMux` from its `New` constructor.

- [ ] **Step 4: Un-skip the mux-enumeration test**

In `csrfmw_test.go`, remove the `t.Skip(...)` from `TestMuxEnumerator_EveryMutatingRouteWrapped`. Run:

```bash
go test ./internal/relay/ -run TestMuxEnumerator -v
```

If any newly-added mutating route is uncovered, the test fails — wrap it with `RequireCSRF` (or, for admin endpoints, with `requireAdmin` which is functionally equivalent: admin token is its own auth).

- [ ] **Step 5: Full P3 suite green**

```bash
go test -tags webkit2_41 -race -timeout 120s ./internal/relay/... ./internal/userstore/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/relay/limits.go internal/relay/server.go internal/relay/limits_user_test.go internal/relay/csrfmw_test.go
git commit -m "feat(relay): rate-limit signup/login/invite; assemble principal-gated mux"
```

---

## Phase P4: Gates + legacy removal

End state: `/uplink`, `/agent`, `/client` only accept the right principal; sessions are owned; `ATTERM_TOKEN` and `ATTERM_READ_ONLY_TOKENS` are removed; admin token strength is enforced.

### Task 4.0: `session.OwnerUserID` + owner-binding invariant

**Files:**
- Modify: `internal/session/session.go`
- Modify: `internal/session/registry.go` (or wherever sessions are registered — discover via `grep -rn "func.*NewSession" internal/session/`)
- Modify: `internal/session/session_test.go`

Implements spec §5.5 owner-binding invariant.

- [ ] **Step 1: Failing tests**

In `session_test.go`:

- `TestRegisterSession_AssignsOwner`: register a new session with `OwnerUserID="u1"`; lookup returns the same.
- `TestRegisterSession_DuplicateIDSameOwnerSucceeds`: same session id, same owner → reuses existing entry (idempotent re-publish; existing red line #3 behavior).
- `TestRegisterSession_DuplicateIDDifferentOwnerRejected`: same session id, different owner → returns `ErrSessionOwnerMismatch`.

- [ ] **Step 2: Implement**

Add `OwnerUserID string` to `Session`. Add `ErrSessionOwnerMismatch` to the package. In the register-or-attach path, check owner before returning the existing session.

- [ ] **Step 3: Run; expect pass**

```bash
go test -tags webkit2_41 ./internal/session/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/session/
git commit -m "feat(session): owner_user_id field with immutable-binding invariant"
```

---

### Task 4.1: `/uplink` and `/agent` bind connection to user via API token

**Files:**
- Modify: `internal/relay/uplink_conn.go`
- Modify: `internal/relay/agent_conn.go`
- Modify: `internal/relay/uplink_conn_test.go` (or new test file)

Implements spec §4.3 (entry gate) and §5.5 (binding flow).

- [ ] **Step 1: Failing tests**

- `TestUplink_RejectsCookiePrincipal`: connect with a valid cookie session (no Authorization) → relay closes with HTTP 401 before WS upgrade.
- `TestUplink_RejectsAdminPrincipal`: connect with `Authorization: Bearer <admin>` → 401.
- `TestUplink_AcceptsAPIToken_BindsOwner`: connect with valid api token; publish a session; verify the in-memory session has `OwnerUserID == user.ID`.
- `TestUplink_DuplicateSessionIDDifferentUser_Closes`: user_A's uplink publishes `sid=X`; user_B's uplink (separate api token) tries to publish `sid=X` → close with reason `session_id_owner_mismatch`; user_A's session remains untouched.

- [ ] **Step 2: Implement**

In `acceptUplink`:
1. Replace existing `authorize` call with `resolver.Resolve(r)`.
2. Reject if `Principal.Kind != PrincipalUser || Principal.TokenID == ""`.
3. Stash `userID := principal.UserID` in the connection scope; pass it to `session.RegisterOrAttach(...)`.
4. When `ErrSessionOwnerMismatch` returned, close WS with close-frame status 4002 and reason `session_id_owner_mismatch`. Define close-reason constants in a new file `internal/relay/closereasons.go`.

Same change for `acceptAgent`.

- [ ] **Step 3: Run; expect pass**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(relay): /uplink and /agent gated on api-token principal with owner binding"
```

---

### Task 4.2: `/client` list + attach filtered by owner

**Files:**
- Modify: `internal/relay/client_conn.go`
- Modify: `internal/relay/client_sessions_conn.go`
- Modify: `internal/relay/permissions.go` — delete the read-only-token branch
- Test files updated alongside

Implements spec §4.3 `/client` row and §7 `permissions.go` cleanup.

- [ ] **Step 1: Failing tests**

- `TestClient_ListFilteredByOwner`: u_A has sessions; u_B has none. With u_B cookie, `GET /api/sessions` returns `[]`. With u_A cookie, returns u_A's sessions.
- `TestClient_AttachOtherUsersSessionRejected`: u_A owns `sid=X`; u_B (cookie) connects WS `/client?session=X` → close with code 4003 and reason `forbidden`.
- `TestClient_AttachAdminRejected`: admin token at `/client` → 401.
- `TestPermissions_NoReadOnlyTokenBranch`: scan `permissions.go` for any reference to `ReadOnlyToken` (string match) — must be zero.

- [ ] **Step 2: Implement**

- `client_sessions_conn.go`: filter listing by `session.OwnerUserID == principal.UserID`.
- `client_conn.go`: before WS upgrade, look up session by id; if missing or `OwnerUserID != principal.UserID`, return 403 (NOT 404; the user has a session list that already disclosed the id).
- `permissions.go`: delete the `if conn.readOnlyToken { ... }` block entirely; the remaining `remote_permission` enforcement (view/control/full) is owner-policy and unchanged. The grep-test in step 1 will catch lingering references.

- [ ] **Step 3: Run; expect pass**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(relay): /client filtered by owner; drop read-only-token branch"
```

---

### Task 4.3: Remove `ATTERM_TOKEN` / `ATTERM_READ_ONLY_TOKENS` + SEC-6 admin-token strength check

**Files:**
- Modify: `cmd/atterm-relay/main.go`
- Modify: `cmd/atterm-relay/main_test.go`
- Modify: `internal/relay/admin_config.go` — delete `ReadOnlyTokenHashes` field
- Modify: `internal/relay/admin_config_test.go`
- Create: `cmd/atterm-relay/token_strength.go`
- Create: `cmd/atterm-relay/token_strength_test.go`

Implements spec §7 (legacy removal) and SEC-6 (strength check).

- [ ] **Step 1: Token-strength tests**

In `token_strength_test.go`:

- Cases array of `{token string, want error}`:
  - `"dev"` → fails (blacklist + length).
  - `"changeme123"` → fails (length).
  - `"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"` → fails (32 chars but 1 char class, also repeat-run > 4).
  - `"Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!"` → fails (repeat-run > 4? — verify: the substring "Aa1!" repeats 8 times but no single char repeats 5+ times consecutively; classes=4; length=32; this should PASS).
  - `strings.Repeat("xY9!", 8)` → PASS.
  - 64-byte cryptorand hex → PASS.

- [ ] **Step 2: Implement**

```go
// cmd/atterm-relay/token_strength.go
package main

import (
    "errors"
    "strings"
    "unicode"
)

var weakAdminTokenBlacklist = map[string]bool{
    "dev": true, "test": true, "admin": true, "password": true,
    "changeme": true, "letmein": true, "12345": true, "secret": true,
}

func validateAdminToken(tok string) error {
    if len(tok) < 32 {
        return errors.New("ATTERM_ADMIN_TOKEN: must be ≥ 32 characters")
    }
    if weakAdminTokenBlacklist[strings.ToLower(tok)] {
        return errors.New("ATTERM_ADMIN_TOKEN: matches a known weak token")
    }
    var upper, lower, digit, sym bool
    var runChar rune
    var run int
    for _, c := range tok {
        switch {
        case unicode.IsUpper(c):
            upper = true
        case unicode.IsLower(c):
            lower = true
        case unicode.IsDigit(c):
            digit = true
        default:
            sym = true
        }
        if c == runChar {
            run++
            if run > 4 {
                return errors.New("ATTERM_ADMIN_TOKEN: too many repeated characters in a row")
            }
        } else {
            run, runChar = 1, c
        }
    }
    classes := 0
    for _, b := range []bool{upper, lower, digit, sym} {
        if b {
            classes++
        }
    }
    if classes < 3 {
        return errors.New("ATTERM_ADMIN_TOKEN: must contain at least 3 of {upper, lower, digit, symbol}")
    }
    return nil
}
```

- [ ] **Step 3: Modify `main.go`**

- Delete all `ATTERM_TOKEN` and `ATTERM_READ_ONLY_TOKENS` env reads.
- After parsing `ATTERM_ADMIN_TOKEN`, if listening on a non-loopback address and `--dev-insecure` is not set, call `validateAdminToken(adminToken)`; on error, log and `os.Exit(1)`.
- Loopback dev mode (`127.0.0.1` / `::1`) skips the strength check but still requires the token to be non-empty.
- Wire `userstore.Open(cfg.ConfigDir+"/users.db")` and pass `*SQLiteStore` into the relay.

- [ ] **Step 4: Update `main_test.go`**

Add cases:
- `TestStartup_RefusesWeakAdminTokenOnPublicListen`: launch with `--addr :8080 --admin-token "dev"` → exit code 1.
- `TestStartup_AcceptsStrongAdminTokenOnPublicListen`.
- `TestStartup_LoopbackDevAcceptsAnyToken`: `--addr 127.0.0.1:0 --admin-token "dev"` → exits OK after ready signal.

- [ ] **Step 5: Run + commit**

```bash
go test -tags webkit2_41 ./cmd/atterm-relay/... ./internal/relay/...
git add cmd/atterm-relay/ internal/relay/admin_config.go internal/relay/admin_config_test.go
git commit -m "feat(relay): remove ATTERM_TOKEN/READ_ONLY; enforce admin-token strength"
```

---

## Phase P4.5: Web push made user-scoped

End state: subscriptions key by user_id; dispatch fans out only to the originating session's owner; legacy file rename + 30-day cleanup.

### Task 4.5.0: `subStore` key by user_id; dispatch by `OwnerUserID`

**Files:**
- Modify: `internal/webpush/subscription.go`
- Modify: `internal/webpush/dispatch.go`
- Modify: `internal/webpush/service.go`
- Modify: tests above

Implements spec §8.7.

- [ ] **Step 1: Update tests first**

In `subscription_test.go` rename every `tokenHash` parameter to `userID`:
- `TestSubStore_AddRemoveByUser`
- `TestSubStore_CapPerUser`

In `dispatch_test.go`:
- `TestDispatch_FilteredByOwner`: insert subs for u_A and u_B; call `DispatchCommandFinished("u_A", event)`; only u_A's `sendOne` is invoked.

- [ ] **Step 2: Implement**

Replace every `tokenHash string` parameter with `userID string` across `subscription.go`, `dispatch.go`, `service.go`. `DispatchCommandFinished` gains a new first parameter `ownerUserID string`; internally it calls only `ByUser(ownerUserID)` instead of iterating all keys.

- [ ] **Step 3: Run + commit**

```bash
go test -tags webkit2_41 ./internal/webpush/...
git commit -m "feat(webpush): key subscriptions by user_id; dispatch filtered by owner"
```

---

### Task 4.5.1: HTTP gate (User principal only) + legacy file rename + 30-day cleanup

**Files:**
- Modify: `internal/relay/web_push_http.go`
- Modify: `internal/webpush/persist.go`
- Modify: tests

- [ ] **Step 1: Tests**

- `TestWebPushHTTP_RequiresUserPrincipal`: cookie session → 200; admin token → 403; no auth → 401.
- `TestWebPushHTTP_DispatchTouchesOwnerOnly` (integration with the new dispatch).
- `TestPersist_RenamesLegacyTokenHashSchema`: write `web-push.json` with `{tokenHash: [...]}`; call `Load`; expect `web-push.json.legacy-<ts>` exists, registry is empty.
- `TestPersist_CleanupLegacyAfter30Days`: create a `.legacy-<ts>` file with mtime 31 days ago; call `CleanupLegacy(ctx, dir)`; file deleted. Create one with mtime 29 days ago; not deleted.

- [ ] **Step 2: Implement**

- `web_push_http.go`: replace the existing token-hash-based subscription handler. Read `Principal` from `resolver.Resolve(r)`, require `Kind==PrincipalUser`. Use `principal.UserID` as the storage key.
- `persist.go`: at `Load`, inspect the JSON shape. If it has the legacy schema (top-level keys are 64-char hex tokenHash patterns instead of ULID-shaped user_ids), rename the file to `<path>.legacy-<unix_ts>` and return an empty registry. Add `CleanupLegacy(ctx, dir)` that walks `dir` for `web-push.json.legacy-*` files older than 30 days and deletes them. Schedule it via a once-per-day ticker in the relay's startup wiring (`cmd/atterm-relay/main.go`).

- [ ] **Step 3: Run + commit**

```bash
go test -tags webkit2_41 ./internal/webpush/... ./internal/relay/...
git commit -m "feat(webpush): user-principal-only subscribe; legacy file rename + cleanup"
```

---

## Phase P5: Web frontend (login / signup / settings / app.js cookie flow)

End state: `/login.html`, `/signup.html`, `/settings.html` are functional; `index.html` no longer prompts for token; fetches use cookie + CSRF; 401 redirects to login.

### Task 5.0: `web/auth.js` — fetch wrapper + login/signup/logout

**Files:**
- Create: `web/auth.js`
- Create: `web/auth.test.mjs`

- [ ] **Step 1: Failing tests**

Using `node --test`, in `web/auth.test.mjs`:

- `import { authFetch, login, signup, logout, getMe } from "./auth.js";`
- `test("authFetch adds X-CSRF-Token for mutating verbs when csrf is cached")` — stub `globalThis.fetch` and `sessionStorage`; call `authFetch("/api/me/tokens", { method: "POST", body: "{}" })`; assert the captured request has `X-CSRF-Token: <cached value>`.
- `test("authFetch redirects on 401")` — stub fetch to return `{status:401}`; replace `location` with a setter spy; call → spy invoked with `/login.html`.
- `test("getMe caches csrf_token from response")` — stub fetch to return `{ok:true, json: () => ({user_id:"u1", csrf_token:"abc"})}`; call `getMe()`; later mutating request includes `X-CSRF-Token: abc`.

- [ ] **Step 2: Implement `web/auth.js`**

```js
// ESM module; loaded by login.html / signup.html / settings.html / app.js.
let cachedCSRF = "";

export async function authFetch(url, init = {}) {
    const method = (init.method || "GET").toUpperCase();
    const headers = new Headers(init.headers || {});
    if (method !== "GET" && method !== "HEAD") {
        if (cachedCSRF) headers.set("X-CSRF-Token", cachedCSRF);
        if (!headers.has("Content-Type") && init.body) {
            headers.set("Content-Type", "application/json");
        }
    }
    const res = await fetch(url, { ...init, headers, credentials: "same-origin" });
    if (res.status === 401) {
        location.assign("/login.html");
        throw new Error("redirected to login");
    }
    return res;
}

export async function getMe() {
    const res = await authFetch("/api/me");
    if (!res.ok) throw new Error("getMe " + res.status);
    const j = await res.json();
    cachedCSRF = j.csrf_token || "";
    return j;
}

export async function login(email, password) {
    const res = await authFetch("/api/auth/login", {
        method: "POST", body: JSON.stringify({ email, password }),
    });
    if (!res.ok) throw new Error("login " + res.status);
    return await getMe();  // populate csrf cache
}

export async function signup(email, password, invite_code) {
    const res = await authFetch("/api/auth/signup", {
        method: "POST",
        body: JSON.stringify({ email, password, invite_code }),
    });
    if (!res.ok) throw new Error("signup " + res.status);
    return await getMe();
}

export async function logout() {
    await authFetch("/api/auth/logout", { method: "POST" });
    cachedCSRF = "";
    location.assign("/login.html");
}
```

- [ ] **Step 3: Run + commit**

```bash
node --test web/auth.test.mjs
git add web/auth.js web/auth.test.mjs
git commit -m "feat(web): auth.js fetch wrapper with cookie + CSRF + 401 redirect"
```

---

### Task 5.1: `login.html` and `signup.html`

**Files:**
- Create: `web/login.html`
- Create: `web/signup.html`
- Modify: `web/style.css` — add minimal `.auth-card` styles

Static pages with vanilla JS modules that call into `auth.js`.

- [ ] **Step 1: Build the pages**

`login.html` minimal markup:
```html
<!doctype html>
<html lang="en"><head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>AT Term · sign in</title>
<link rel="stylesheet" href="style.css" />
</head><body class="auth-page">
<form id="login-form" class="auth-card" autocomplete="on">
  <h1>AT Term</h1>
  <label>Email <input id="email" type="email" required autocomplete="username" /></label>
  <label>Password <input id="password" type="password" required autocomplete="current-password" /></label>
  <button type="submit">Sign in</button>
  <p id="error" hidden></p>
  <p class="alt">No account? Ask your admin for an invite.</p>
</form>
<script type="module">
import { login } from "./auth.js";
document.getElementById("login-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const err = document.getElementById("error");
    err.hidden = true;
    try {
        await login(document.getElementById("email").value, document.getElementById("password").value);
        location.assign("/");
    } catch (e) {
        err.textContent = "Invalid email or password.";
        err.hidden = false;
    }
});
</script></body></html>
```

`signup.html` identical structure with an additional `invite_code` field; submit calls `signup(...)`.

`style.css` additions: `.auth-page { display:grid; place-items:center; min-height:100vh; } .auth-card { max-width: 360px; ... }`. Match the existing dark theme palette in `style.css`.

- [ ] **Step 2: Verify load**

Run the relay locally:
```bash
ATTERM_ADMIN_TOKEN=Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!Aa1! go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web --dev-insecure
```
Open `http://127.0.0.1:8080/login.html` in a browser; submitting empty fields shows the inline error.

- [ ] **Step 3: Commit**

```bash
git add web/login.html web/signup.html web/style.css
git commit -m "feat(web): login and signup pages"
```

---

### Task 5.2: `settings.html` — API token management + change password + logout

**Files:**
- Create: `web/settings.html`
- Create: `web/settings.test.mjs`

- [ ] **Step 1: Source-level test**

In `settings.test.mjs`:
- Read the file as text; assert it references `auth.js`, exposes form ids `#create-token-form`, `#change-password-form`, and includes a `#logout` button.
- Smoke test the inline module: import `web/settings.html`-extracted JS via a small helper that parses the `<script type="module">` block, runs it against a stub DOM + stub `authFetch`, and asserts that `POST /api/me/tokens` is called when the form submits.

- [ ] **Step 2: Build the page**

Static markup + inline module. Capabilities required:
- "Your API tokens" list, populated from `GET /api/me/tokens`. Each row shows `name`, `prefix`, `created_at`, and a `Revoke` button.
- "Create new token" form (input: `name`). On submit, POST → display the returned plaintext **once** with a `Copy` button and a "store this somewhere safe" warning. Disabled state for the input after submission.
- "Change password" form: `current_password`, `new_password` — POSTs to `/api/me/password` (a new endpoint added in Task 3.2 footprint; if not yet present, add a follow-up note here to ship it as part of 5.2). Spec §5.6 only covers admin reset, so add the user-initiated change-password handler here:
  - **In this same task**, append `POST /api/me/password` to `auth_http.go` from Task 3.1, and add a unit test in `auth_http_test.go` for it. Handler: requires `Principal.Kind==User`; verifies current password via store; calls a new `Store.ChangePassword(ctx, userID, newPlaintext) error` method (extend the interface; the SQLite impl rotates csrf_secret and deletes other web_sessions, keeping the requester's current one).
- Logout button that calls `auth.js::logout()`.

- [ ] **Step 3: Run + commit**

```bash
node --test web/settings.test.mjs
go test ./internal/relay/ -run TestChangePassword -v
git add web/settings.html web/settings.test.mjs internal/relay/auth_http.go internal/relay/auth_http_test.go internal/userstore/users.go internal/userstore/store.go
git commit -m "feat(web,relay): settings page with token mgmt and password change"
```

---

### Task 5.3: `app.js` cookie migration + `index.html` token panel removal

**Files:**
- Modify: `web/index.html`
- Modify: `web/app.js`
- Modify: `web/app-core.js`
- Modify: `web/sw.js`
- Modify: `web/*.test.mjs` as needed
- Modify: `internal/relay/server.go` — serve `/` → `index.html` only for authenticated cookie principals; otherwise 302 → `/login.html`

- [ ] **Step 1: Failing tests**

- `web/app-core.test.mjs`: add `test("no longer reads token from localStorage")`: scan the source for the now-deleted token storage keys.
- `web/push-flow.test.mjs`: update — the push enable button must not call any token-storage path; cookie is implicit.
- `web/sw.test.mjs`: `test("sw bypasses cache for /api/*")`: verify the service worker's fetch handler skips cache for requests with path starting with `/api/` or for non-GET methods.

- [ ] **Step 2: Implementation**

- `index.html`: delete the entire `#token-panel` section; delete the `token-toggle` button. Add a top-right `<button id="logout">Sign out</button>` next to the status pill. Inline module subscribes to `auth.js::logout` on click.
- `app.js`: delete the URL-fragment `#token=…` bootstrap, the `localStorage.token` reads/writes, and the manual token panel form handlers. Use `authFetch(...)` everywhere. WebSocket connections to `/client` no longer need to pass the `Sec-WebSocket-Protocol: atterm-token.…` header — they rely on cookie auth (browsers attach cookies to WS upgrade automatically as long as the page origin matches).
- `app-core.js`: remove all token persistence functions.
- `sw.js`: in the `fetch` event handler, bypass cache for `/api/*` and any non-GET; everything else (static assets) can remain cacheable per current logic.
- `server.go`: when serving the static root, if request has no valid cookie session AND the path is `/`, 302 → `/login.html`. Subresources (`*.js`, `*.css`, `*.html` static files) are still served unconditionally so the unauthenticated `/login.html` can load them.

- [ ] **Step 3: Run + commit**

```bash
node --test web/*.test.mjs
git add web/ internal/relay/server.go
git commit -m "feat(web): migrate index.html to cookie auth; drop token panel"
```

---

## Phase P6: `AUTH_INFO` frame

End state: relay sends `AUTH_INFO` to desktop immediately after uplink authentication; protocol doc updated.

### Task 6.0: Add `AUTH_INFO` frame type

**Files:**
- Modify: `internal/proto/frame.go` — add `TypeAuthInfo` constant
- Modify: `internal/proto/frame_test.go` — round-trip test
- Modify: `internal/relay/uplink_conn.go` — emit `AUTH_INFO` on successful auth
- Modify: `docs/spec/protocol.md`

Implements spec §8.4.

- [ ] **Step 1: Find an unused Type byte**

```bash
grep -E "^\s+Type[A-Z][A-Za-z]+\s*=" internal/proto/frame.go
```
Pick the next unused value (likely `0x10` or similar — verify by reading the existing list). Use that for `TypeAuthInfo`.

- [ ] **Step 2: Codec test**

In `frame_test.go` add:
```go
func TestAuthInfo_RoundTrip(t *testing.T) {
    payload := []byte(`{"user_id":"01HXABCDEF"}`)
    f := Frame{Type: TypeAuthInfo, Payload: payload}
    enc, err := Encode(f)
    if err != nil { t.Fatal(err) }
    dec, err := Decode(enc)
    if err != nil { t.Fatal(err) }
    if dec.Type != TypeAuthInfo { t.Fatalf("type: got %x", dec.Type) }
    if string(dec.Payload) != string(payload) { t.Fatalf("payload mismatch") }
}
```

- [ ] **Step 3: Implement + emit**

Add `TypeAuthInfo` to the constants. In `uplink_conn.go`, immediately after `resolveIdentity` succeeds and before reading HELLO, send:
```go
payload, _ := json.Marshal(struct {
    UserID string `json:"user_id"`
}{UserID: principal.UserID})
if err := writeFrame(conn, Frame{Type: proto.TypeAuthInfo, Payload: payload}); err != nil { ... }
```

- [ ] **Step 4: Update `docs/spec/protocol.md`**

Add a row to the frame table:
```
| 0x10 | AUTH_INFO | server→client | UTF-8 JSON; v2 schema {user_id: ULID}. Unknown JSON keys MUST be ignored. |
```

- [ ] **Step 5: Run + commit**

```bash
go test -tags webkit2_41 ./internal/proto/... ./internal/relay/...
git add internal/proto/ internal/relay/uplink_conn.go docs/spec/protocol.md
git commit -m "feat(proto): AUTH_INFO frame carrying user_id on uplink auth"
```

---

## Phase P7: Desktop SettingsRelay redesign + `RelayPaused` fix

End state: SettingsRelay has an ON/OFF toggle that does not wipe URL/token; label/placeholder reflect "API token"; open-in-browser button; AUTH_INFO consumed.

P7 may land **before** the rest of this plan because the bug it fixes ("disconnect erases config") is independent.

### Task 7.0: `RelayPaused` config field + `SetUplinkPaused` binding

**Files:**
- Modify: `desktop/config.go`
- Modify: `desktop/config_test.go`
- Modify: `desktop/app.go`
- Modify: `desktop/app_test.go`

Implements spec §8.2.

- [ ] **Step 1: Failing tests**

- `desktop/config_test.go`: `TestAppConfig_DeserializeOldJSON_RelayPausedDefaultsFalse`: deserialize `{"relay_url":"wss://x","relay_token":"atk_..."}` into `appConfig`; assert `cfg.RelayPaused == false`.
- `desktop/app_test.go`: `TestSetUplinkPaused_TogglesWithoutWipingConfig`:
  - call `SetRelayConfig({url:"wss://x", token:"atk_..."})` → connected (or attempting) state.
  - call `SetUplinkPaused(true)` → `GetRelayConfig` returns `url` and `token` unchanged; `Connected == false`.
  - call `SetUplinkPaused(false)` → uplink restarts; `Connected` flips back true.

- [ ] **Step 2: Implement**

- `config.go`: add `RelayPaused bool \`json:"relay_paused,omitempty"\`` after the existing relay fields.
- `app.go::applyRelayConfig`: change the gate from `if cfg.RelayURL == ""` to `if cfg.RelayURL == "" || cfg.RelayPaused`. Log clearly: `desktop: uplink paused` vs `desktop: uplink disabled (no URL)`.
- `app.go`: new method:
```go
func (a *App) SetUplinkPaused(paused bool) error {
    if a.cfgStore == nil { return fmt.Errorf("config store not ready") }
    cfg := a.cfgStore.Get()
    cfg.RelayPaused = paused
    if err := a.cfgStore.Set(cfg); err != nil { return err }
    a.applyRelayConfig(cfg)
    return nil
}
```
- `app.go::GetRelayConfig` return value: add `Paused bool` field, sourced from `cfg.RelayPaused`.

- [ ] **Step 3: Run + commit**

```bash
go test -tags webkit2_41 ./desktop/...
git add desktop/config.go desktop/config_test.go desktop/app.go desktop/app_test.go
git commit -m "fix(desktop): RelayPaused toggle keeps url/token across disconnect"
```

---

### Task 7.1: SettingsRelay.vue redesign

**Files:**
- Modify: `desktop/frontend/src/components/SettingsRelay.vue`
- Modify: `desktop/frontend/src/components/SettingsRelay.test.ts`
- Modify: `desktop/frontend/src/lib/api.ts`

- [ ] **Step 1: Failing source-level tests**

Add to `SettingsRelay.test.ts`:
- `test("renders API token label, not 'token'")`: source contains `>API token<` and not `>shared bearer token<`.
- `test("renders Uplink ON/OFF toggle bound to RelayPaused")`.
- `test("does not have a 'disconnect' button")`: source must not contain `disconnect()` function or button text.
- `test("paste of non-atk token surfaces a warning hint")`.
- `test("Open in browser button calls BrowserOpenURL with relay URL + /settings.html")`.

- [ ] **Step 2: Implement**

Rewrite the relevant portion of `SettingsRelay.vue`:
- Replace the existing "save & connect" + "disconnect" pair with: a single "save" button (saves URL/token/permission/insecure) and a top-of-pane toggle bound to a local `paused` ref synced with `cfg.RelayPaused`. Toggle change → call `api.SetUplinkPaused(paused)`.
- Rename the token field label to "API token"; placeholder `atk_xxxxxxxx…`; on `input` event, if value is non-empty and not `atk_*`, show a non-blocking red hint below the field.
- Add an "Open in browser" button next to the hint that calls Wails' `BrowserOpenURL` with `<relay-url>/settings.html`.
- Status pill matrix per spec §8.2.

`api.ts`: add wrapper `export function setUplinkPaused(paused: boolean) { return window.go.main.App.SetUplinkPaused(paused); }`.

- [ ] **Step 3: Run + commit**

```bash
cd desktop/frontend && npm run build && npx vitest run
git add desktop/frontend/src/components/SettingsRelay.vue desktop/frontend/src/components/SettingsRelay.test.ts desktop/frontend/src/lib/api.ts
git commit -m "feat(desktop): SettingsRelay ON/OFF toggle + API-token labels + open-in-browser"
```

---

## Phase P8: Desktop status row + auth-error feedback

End state: desktop parses `AUTH_INFO`, displays `connected as <email>` (fetched via `/api/me`), and surfaces clear banners on auth failures.

### Task 8.0: Close-reason parse + `relay:auth-error` event

**Files:**
- Modify: `desktop/uplink.go`
- Modify: `desktop/uplink_e2e_test.go`
- Modify: `desktop/frontend/src/App.vue` — banner UI

Implements spec §8.3.

- [ ] **Step 1: Failing tests**

In `uplink_e2e_test.go`:
- `TestUplink_AuthErrorClose_EmitsEvent`: spin up a fake relay that, on `/uplink` upgrade, closes immediately with WS close code 4001 and reason `auth_invalid_token`. Desktop's `uplink.Run` must call `runtime.EventsEmit(ctx, "relay:auth-error", {reason:"auth_invalid_token"})`. Assert via a stub `EventsEmit` function (DI).

- [ ] **Step 2: Implement**

In `uplink.go`:
- Inject the `EventsEmit` func via a struct field (default `runtime.EventsEmit`) so tests can substitute.
- After WS close, parse the close frame's `Code` and `Reason`. If code is 4001 / 4002 / 4003 (auth-related), emit `relay:auth-error` with the reason string.

In `App.vue`:
- Subscribe to `relay:auth-error` on mount; render a top banner with text from a small dict:
```ts
const banners: Record<string, string> = {
  auth_invalid_token: "Invalid or revoked API token. Generate a new one in web settings.",
  auth_user_disabled: "Account disabled. Contact your relay admin.",
  session_id_owner_mismatch: "Session id collision. Restart the desktop app.",
};
```
- Banner has an "Open settings" action that opens SettingsDialog to the Relay tab.

- [ ] **Step 3: Run + commit**

```bash
go test -tags webkit2_41 ./desktop/...
cd desktop/frontend && npm run build
git add desktop/uplink.go desktop/uplink_e2e_test.go desktop/frontend/src/App.vue
git commit -m "feat(desktop): parse uplink close reasons; show auth-error banner"
```

---

### Task 8.1: Consume `AUTH_INFO` + status row "connected as ..."

**Files:**
- Modify: `desktop/uplink.go`
- Modify: `desktop/frontend/src/components/SettingsRelay.vue`
- Modify: `desktop/frontend/src/lib/api.ts`

- [ ] **Step 1: Failing tests**

- Go: `TestUplink_AuthInfo_EmitsUserID`: fake relay sends an `AUTH_INFO` frame with `{user_id:"01HXABC"}` after upgrade. Desktop emits `relay:auth-info`, `{user_id:"01HXABC"}`.
- Frontend: `SettingsRelay.test.ts` — `test("status row shows user_id_short when AUTH_INFO received but email not yet fetched")`; `test("status row shows email once /api/me fetch completes")`.

- [ ] **Step 2: Implement**

- `uplink.go`: when the first frame after upgrade has `Type==proto.TypeAuthInfo`, JSON-decode payload, emit `relay:auth-info` with the user_id. Do not block the rest of the protocol on this.
- `SettingsRelay.vue`: on `relay:auth-info`, set `connectedUserIDShort = user_id.slice(0,8)`. Then call `api.fetchRelayMe()` (new binding) which performs `GET <relay_url>/api/me` with `Authorization: Bearer <api_token>` and returns the email. Email stays in-memory only (SEC-1).
- `api.ts`: add `fetchRelayMe()`; the corresponding Go binding makes the HTTP request server-side from `app.go` (avoid CORS by routing through Wails backend).

- [ ] **Step 3: Run + commit**

```bash
go test -tags webkit2_41 ./desktop/...
cd desktop/frontend && npm run build && npx vitest run
git add desktop/ desktop/frontend/
git commit -m "feat(desktop): consume AUTH_INFO; status row shows user / email"
```

---

## Phase P9: Documentation

End state: `README.md`, `AGENTS.md`, `docs/spec/architecture.md`, and `docs/spec/protocol.md` all reflect the new identity model. The release tag bumps to v0.2.0 (or next major-equivalent — confirm with operator at release time).

### Task 9.0: README + AGENTS update

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Edits**

- `README.md`:
  - Update the "现在能做什么" table: replace "用户系统" `还在路线图` with `✓ 支持`.
  - Replace the "方式 B" section: delete the `ATTERM_TOKEN` quick start; replace with: start relay with strong `ATTERM_ADMIN_TOKEN` → open `/admin/` → create invite → sign up at `/signup.html` → create API token at `/settings.html` → paste into desktop.
  - Delete the read-only token sharing section ("只分享查看权限"). The replacement guidance is "create a second user account if you want a colleague to attach".
  - Update the env table: remove `ATTERM_TOKEN` and `ATTERM_READ_ONLY_TOKENS` rows.
- `AGENTS.md`:
  - Update the env section: same removals. Add a one-line note that `cmd/atterm-relay` now requires a strong admin token on public listen.
  - Update the "何时改哪里" table: add a row pointing at `internal/userstore` for account-related changes.

- [ ] **Step 2: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: README and AGENTS reflect user-account model"
```

---

### Task 9.1: `docs/spec/architecture.md` and `docs/spec/protocol.md`

**Files:**
- Modify: `docs/spec/architecture.md`
- Modify: `docs/spec/protocol.md`

- [ ] **Step 1: `architecture.md`**

Add a new top-level section after "Components": **"User accounts and identity"** that summarizes:
- single SQLite `users.db`; `internal/userstore` is the only writer.
- Principal kinds (User, Admin, None).
- entry-point gates (table from spec §4.3).
- bootstrap path (admin token → invite → signup → API token).

Cross-reference the spec at `docs/superpowers/specs/2026-05-15-saas-user-accounts-design.md`.

- [ ] **Step 2: `protocol.md`**

If Task 6.0 did not already land the `AUTH_INFO` row (it did), verify the frame table is complete and add a paragraph explaining when `AUTH_INFO` is emitted (after uplink auth, before HELLO read).

- [ ] **Step 3: Commit**

```bash
git add docs/spec/
git commit -m "docs: architecture and protocol updated for user accounts"
```

---

## Final acceptance

After all tasks land, run the full no-regression baseline and the spec §9.7 manual walkthrough:

```bash
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -race -timeout 120s ./...
node --test web/*.test.mjs
cd desktop/frontend && npm run build && npx vitest run
```

Manual walkthrough (per spec §9.7) — record results in the PR description for each phase.

---




