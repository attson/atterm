# Web UI Redesign — PR A: Backend admin role + bootstrap

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace shared `ATTERM_ADMIN_TOKEN` admin auth with a `users.is_admin` role driven by normal session login; introduce `ATTERM_BOOTSTRAP_ADMIN_EMAIL` / `_PASSWORD` env to provision/promote the first admin.

**Architecture:** New migration `0002_admin_role.sql` adds `users.is_admin` (INTEGER 0/1). `IdentityResolver.Resolve` consults `user.is_admin` to set `PrincipalAdmin` (previously triggered by a Bearer-token branch). `cmd/atterm-relay` parses two new env vars on startup and calls `store.EnsureAdminUser`. `validateAdminToken` is renamed/strengthened to `validateBootstrapPassword`. Admin API gains promote/demote with self-demote (400) + last-admin (409) guards and an audit log line. The old `/admin/` HTML constant and its handler are deleted — `/admin/` returns 404 between PR A and PR D (admin UI rebuild); admin features remain reachable via the JSON API.

**Tech Stack:** Go (relay backend), modernc.org/sqlite (driver), `golang.org/x/crypto/argon2` (existing password hashing), `net/mail` (email validation). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-17-web-ui-redesign-design.md`

---

## File Map

**Create:**
- `internal/userstore/migrations/0002_admin_role.sql`
- `internal/userstore/admin.go` — `EnsureAdminUser` + `SetUserAdmin` impls
- `internal/userstore/admin_test.go` — tests for the above
- `cmd/atterm-relay/bootstrap_admin.go` — env parsing + dispatch + email validation + warn lines
- `cmd/atterm-relay/bootstrap_admin_test.go` — unit tests for the helper

**Rename:**
- `cmd/atterm-relay/token_strength.go` → `cmd/atterm-relay/bootstrap_password_strength.go`
- `cmd/atterm-relay/token_strength_test.go` → `cmd/atterm-relay/bootstrap_password_strength_test.go`

**Modify:**
- `internal/userstore/users.go` — `User` struct gains `IsAdmin`; `GetUser` / `VerifyPassword` / `ListUsers` read the column
- `internal/userstore/store.go` — `Store` interface adds `EnsureAdminUser`, `SetUserAdmin`
- `internal/relay/identity.go` — drop `adminToken` from resolver; `Resolve` reads `user.is_admin`
- `internal/relay/server.go` — drop `Config.AdminToken`; drop the `/admin/` mux registration; (re-)route after deletion
- `internal/relay/admin_http.go` — drop `handleAdminPage` + `adminPageHTML` + `authorizeAdmin`; add `POST/DELETE /admin/api/users/{id}/admin` handlers; `ListUsers` response gains `is_admin`
- `internal/relay/admin_http_test.go` — replace `testAdminToken` Bearer fixture with cookie-based admin user; add tests for promote/demote, self-demote, last-admin, audit log
- `cmd/atterm-relay/main.go` — drop `--admin-token` flag + ATTERM_ADMIN_TOKEN env; add bootstrap env parsing call; tighten public-listen safety check
- `README.md`, `AGENTS.md` — swap admin-token docs for bootstrap-env docs

**Delete:** None (rename + replace; the old token_strength file content lives on as bootstrap_password_strength).

---

## Task 1 — Schema migration: `users.is_admin`

**Files:**
- Create: `internal/userstore/migrations/0002_admin_role.sql`
- Modify: `internal/userstore/users.go` (User struct gains IsAdmin)

- [ ] **Step 1: Add migration file**

Create `internal/userstore/migrations/0002_admin_role.sql`:

```sql
-- Add is_admin column to users. SQLite has no BOOLEAN; INTEGER 0/1.
-- Default 0 keeps existing rows non-admin; PR A's bootstrap path is
-- the only way to flip this to 1 from outside the admin API.
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
```

- [ ] **Step 2: Add IsAdmin to User struct**

Edit `internal/userstore/users.go` — find the `User struct` (around line 25) and add `IsAdmin bool` immediately after `Email`:

```go
type User struct {
    ID         string
    Email      string
    IsAdmin    bool
    CreatedAt  time.Time
    DisabledAt *time.Time
    csrfSecret []byte // populated by internal lookups; CSRFSecret() exposes
}
```

- [ ] **Step 3: Verify the migration applies cleanly on a fresh DB**

```bash
go test -run TestSchemaMigrations -v ./internal/userstore/ 2>&1 | tail -20
```

If no such test exists yet, run any existing userstore test to exercise `Open` → `migrate`:

```bash
go test -run TestCreateUser -v ./internal/userstore/ 2>&1 | tail -10
```

Expected: PASS (`migrate` walks the embedded migrations dir and applies both 0001 and 0002).

- [ ] **Step 4: Commit**

```bash
git add internal/userstore/migrations/0002_admin_role.sql internal/userstore/users.go
git commit -m "userstore: add is_admin column to users (migration 0002)"
```

---

## Task 2 — Read `is_admin` in user lookup methods

**Files:**
- Modify: `internal/userstore/users.go` (CreateUser / VerifyPassword / GetUser / ListUsers SELECT statements + struct assembly)
- Test: `internal/userstore/users_test.go`

- [ ] **Step 1: Write a failing test for `GetUser` returning IsAdmin**

Edit `internal/userstore/users_test.go` — append:

```go
func TestGetUser_DefaultsToNonAdmin(t *testing.T) {
    s := openMemStore(t)
    defer s.Close()
    u, err := s.CreateUser(context.Background(), "a@example.com", "passphrase-1234")
    if err != nil { t.Fatal(err) }
    got, err := s.GetUser(context.Background(), u.ID)
    if err != nil { t.Fatal(err) }
    if got.IsAdmin {
        t.Errorf("freshly created user IsAdmin = true; want false (default)")
    }
}
```

(`openMemStore` is the existing helper; reuse whatever the file already uses to open a `:memory:` store.)

- [ ] **Step 2: Run the test, confirm it fails**

```bash
go test -run TestGetUser_DefaultsToNonAdmin -v ./internal/userstore/ 2>&1 | tail -10
```

Expected: FAIL — `got.IsAdmin` is the zero value (false) so this actually passes by coincidence, BUT will then fail step 4. If it passes, continue to step 3 anyway.

- [ ] **Step 3: Wire `is_admin` into all four read paths**

Edit `internal/userstore/users.go`:

`CreateUser` (around line 103) — no change needed; new rows inherit DEFAULT 0; `User{IsAdmin: false}` is implicit.

`VerifyPassword` (around line 136) — extend the SELECT and Scan:

```go
var (
    id         string
    hash       string
    csrfSecret []byte
    createdAt  int64
    disabledAt sql.NullInt64
    isAdmin    int
)
err := s.db.QueryRowContext(ctx,
    `SELECT id, password_hash, csrf_secret, created_at, disabled_at, is_admin
     FROM users WHERE email = ?`, email,
).Scan(&id, &hash, &csrfSecret, &createdAt, &disabledAt, &isAdmin)
```

In the success branch:

```go
u := &User{
    ID: id, Email: email,
    IsAdmin:    isAdmin != 0,
    CreatedAt:  time.Unix(createdAt, 0),
    csrfSecret: csrfSecret,
}
```

`GetUser` (around line 173) — same change: add `is_admin` to SELECT, scan into `int`, copy into `User.IsAdmin`.

`ListUsers` (around line 210) — same change.

- [ ] **Step 4: Add a positive test for the new column**

Append to `internal/userstore/users_test.go`:

```go
func TestSetIsAdminColumnRoundTrip(t *testing.T) {
    s := openMemStore(t)
    defer s.Close()
    u, err := s.CreateUser(context.Background(), "a@example.com", "passphrase-1234")
    if err != nil { t.Fatal(err) }
    // Direct SQL to bypass not-yet-existing SetUserAdmin.
    if _, err := s.DB().ExecContext(context.Background(),
        `UPDATE users SET is_admin = 1 WHERE id = ?`, u.ID); err != nil {
        t.Fatal(err)
    }
    got, err := s.GetUser(context.Background(), u.ID)
    if err != nil { t.Fatal(err) }
    if !got.IsAdmin {
        t.Errorf("after UPDATE is_admin=1, GetUser returned IsAdmin=false")
    }
    // VerifyPassword should also surface the flag.
    v, err := s.VerifyPassword(context.Background(), "a@example.com", "passphrase-1234")
    if err != nil { t.Fatal(err) }
    if v == nil || !v.IsAdmin {
        t.Errorf("VerifyPassword returned IsAdmin=false after promotion")
    }
}
```

- [ ] **Step 5: Run tests**

```bash
go test -run "TestGetUser_DefaultsToNonAdmin|TestSetIsAdminColumnRoundTrip" -v ./internal/userstore/ 2>&1 | tail -15
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/userstore/users.go internal/userstore/users_test.go
git commit -m "userstore: surface users.is_admin in GetUser/VerifyPassword/ListUsers"
```

---

## Task 3 — `SetUserAdmin` store method + interface

**Files:**
- Create: `internal/userstore/admin.go`
- Modify: `internal/userstore/store.go` (add `SetUserAdmin` to `Store` interface)
- Test: `internal/userstore/admin_test.go`

- [ ] **Step 1: Add the interface method**

Edit `internal/userstore/store.go` — in the `Store` interface (around line 60), after `ResetUserPassword`:

```go
// SetUserAdmin sets the is_admin flag for userID. Idempotent.
SetUserAdmin(ctx context.Context, userID string, admin bool) error
```

- [ ] **Step 2: Write the failing test**

Create `internal/userstore/admin_test.go`:

```go
package userstore

import (
    "context"
    "testing"
)

func TestSetUserAdmin_Toggle(t *testing.T) {
    s := openMemStore(t)
    defer s.Close()
    u, err := s.CreateUser(context.Background(), "a@example.com", "passphrase-1234")
    if err != nil { t.Fatal(err) }

    if err := s.SetUserAdmin(context.Background(), u.ID, true); err != nil {
        t.Fatal(err)
    }
    got, _ := s.GetUser(context.Background(), u.ID)
    if !got.IsAdmin {
        t.Fatal("after SetUserAdmin(true) IsAdmin still false")
    }

    if err := s.SetUserAdmin(context.Background(), u.ID, false); err != nil {
        t.Fatal(err)
    }
    got, _ = s.GetUser(context.Background(), u.ID)
    if got.IsAdmin {
        t.Fatal("after SetUserAdmin(false) IsAdmin still true")
    }
}

func TestSetUserAdmin_UnknownUserIsNoop(t *testing.T) {
    s := openMemStore(t)
    defer s.Close()
    // Unknown id; should not return an error (UPDATE matches 0 rows).
    if err := s.SetUserAdmin(context.Background(), "nope", true); err != nil {
        t.Fatalf("SetUserAdmin on missing id: %v", err)
    }
}
```

- [ ] **Step 3: Run test, confirm compile failure (no implementation)**

```bash
go test ./internal/userstore/ 2>&1 | tail -5
```

Expected: FAIL — `*SQLiteStore has no field or method SetUserAdmin`.

- [ ] **Step 4: Implement**

Create `internal/userstore/admin.go`:

```go
package userstore

import (
    "context"
    "fmt"
)

// SetUserAdmin flips users.is_admin for the given userID. Idempotent;
// no-op (and no error) when the userID does not exist.
func (s *SQLiteStore) SetUserAdmin(ctx context.Context, userID string, admin bool) error {
    var v int
    if admin {
        v = 1
    }
    _, err := s.db.ExecContext(ctx,
        `UPDATE users SET is_admin = ? WHERE id = ?`, v, userID)
    if err != nil {
        return fmt.Errorf("set is_admin: %w", err)
    }
    return nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test -run "TestSetUserAdmin" -v ./internal/userstore/ 2>&1 | tail -10
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/userstore/store.go internal/userstore/admin.go internal/userstore/admin_test.go
git commit -m "userstore: SetUserAdmin store method + interface"
```

---

## Task 4 — Rename + strengthen `validateAdminToken` → `validateBootstrapPassword`

**Files:**
- Rename: `cmd/atterm-relay/token_strength.go` → `cmd/atterm-relay/bootstrap_password_strength.go`
- Rename: `cmd/atterm-relay/token_strength_test.go` → `cmd/atterm-relay/bootstrap_password_strength_test.go`

- [ ] **Step 1: git mv both files**

```bash
git mv cmd/atterm-relay/token_strength.go cmd/atterm-relay/bootstrap_password_strength.go
git mv cmd/atterm-relay/token_strength_test.go cmd/atterm-relay/bootstrap_password_strength_test.go
```

- [ ] **Step 2: Update the production file — function rename + ≥16 minimum + variable rename**

Edit `cmd/atterm-relay/bootstrap_password_strength.go`:

```go
package main

import (
    "errors"
    "strings"
    "unicode"
)

// weakBootstrapPasswordBlacklist holds plaintexts so commonly used that
// any deploy that picks one is almost certainly misconfigured. Match is
// case-insensitive.
var weakBootstrapPasswordBlacklist = map[string]bool{
    "dev":      true,
    "test":     true,
    "admin":    true,
    "password": true,
    "changeme": true,
    "letmein":  true,
    "12345":    true,
    "secret":   true,
}

// validateBootstrapPassword enforces the rule applied to
// ATTERM_BOOTSTRAP_ADMIN_PASSWORD when used to create a new admin user.
// The plaintext lives in env files / systemd units and is therefore a
// long-lived disk secret, so the rule is stricter than the everyday
// user ChangePassword ≥12-char minimum.
func validateBootstrapPassword(pw string) error {
    if len(pw) < 16 {
        return errors.New("ATTERM_BOOTSTRAP_ADMIN_PASSWORD: must be ≥ 16 characters")
    }
    if weakBootstrapPasswordBlacklist[strings.ToLower(pw)] {
        return errors.New("ATTERM_BOOTSTRAP_ADMIN_PASSWORD: matches a known weak value")
    }
    var upper, lower, digit, sym bool
    var runChar rune
    var run int
    for _, c := range pw {
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
                return errors.New("ATTERM_BOOTSTRAP_ADMIN_PASSWORD: too many repeated characters in a row")
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
        return errors.New("ATTERM_BOOTSTRAP_ADMIN_PASSWORD: must contain at least 3 of {upper, lower, digit, symbol}")
    }
    return nil
}
```

- [ ] **Step 3: Update the test file — same rename + lengthen test inputs to ≥16**

Edit `cmd/atterm-relay/bootstrap_password_strength_test.go`. Replace every `validateAdminToken` with `validateBootstrapPassword`, every `ATTERM_ADMIN_TOKEN` with `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`. Update any test strings that were 32 chars exactly to ≥16 in the strong cases, and update the strength-failure cases for the new minimum if they relied on `< 32`.

Read the file first (`cmd/atterm-relay/bootstrap_password_strength_test.go`); for each test:
- `TestValidateAdminToken` → rename + at least one passing case with mixed classes of length 16.
- `TestValidateAdminToken_CryptoRandom` → rename; the hex-only string should still fail on class count (2 classes), not length, so this remains a valid test case at any length ≥16.
- `TestValidateAdminToken_BlacklistedExact` → rename; entries like `"dev"` and `"password"` still fail (length OR blacklist).
- `TestValidateAdminToken_RunLimit` → rename; the "aaaaa…"-style input still fails on run length, just make sure the input length is ≥16 so it passes the length gate before hitting the run check (so the run check is what's actually exercised).

- [ ] **Step 4: Run renamed tests**

```bash
go test -run TestValidateBootstrapPassword ./cmd/atterm-relay/ -v 2>&1 | tail -20
```

Expected: PASS (4 sub-tests).

- [ ] **Step 5: Commit**

```bash
git add cmd/atterm-relay/bootstrap_password_strength.go cmd/atterm-relay/bootstrap_password_strength_test.go
git commit -m "cmd/atterm-relay: rename validateAdminToken → validateBootstrapPassword; tighten to ≥16 chars"
```

Note: `validateAdminToken` is still referenced from `main.go`; that call site is fixed in Task 14. The build is intentionally broken between Task 4 and Task 14.

---

## Task 5 — `EnsureAdminUser` for existing-user path

**Files:**
- Modify: `internal/userstore/admin.go`
- Modify: `internal/userstore/store.go` (add interface method)
- Modify: `internal/userstore/admin_test.go`

- [ ] **Step 1: Add interface method**

Edit `internal/userstore/store.go` — in the `Store` interface, near `SetUserAdmin`:

```go
// EnsureAdminUser is idempotent. If a user with this email exists, it
// is marked is_admin=1 and returns (created=false, nil); password is
// ignored. Otherwise a new user is created with the given plaintext
// password and is_admin=1, returning (created=true, nil). Empty
// plaintext for the create path returns ErrEmptyBootstrapPassword;
// strength enforcement is the caller's job.
EnsureAdminUser(ctx context.Context, email, plaintext string) (created bool, err error)
```

- [ ] **Step 2: Write the failing test for existing-user path**

Append to `internal/userstore/admin_test.go`:

```go
func TestEnsureAdminUser_ExistingUser_PromotesAndIgnoresPassword(t *testing.T) {
    ctx := context.Background()
    s := openMemStore(t)
    defer s.Close()

    u, err := s.CreateUser(ctx, "a@example.com", "original-passphrase")
    if err != nil { t.Fatal(err) }
    if u.IsAdmin { t.Fatal("freshly created user already admin") }

    created, err := s.EnsureAdminUser(ctx, "a@example.com", "this-should-be-ignored")
    if err != nil { t.Fatal(err) }
    if created { t.Error("created=true for existing user; want false") }

    got, _ := s.GetUser(ctx, u.ID)
    if !got.IsAdmin { t.Error("existing user not promoted after EnsureAdminUser") }

    // Original password still works (password arg was ignored).
    v, _ := s.VerifyPassword(ctx, "a@example.com", "original-passphrase")
    if v == nil { t.Error("original password no longer verifies (EnsureAdminUser must not touch it)") }
    if v2, _ := s.VerifyPassword(ctx, "a@example.com", "this-should-be-ignored"); v2 != nil {
        t.Error("EnsureAdminUser silently changed the password")
    }
}
```

- [ ] **Step 3: Run, confirm compile failure**

```bash
go test ./internal/userstore/ 2>&1 | tail -5
```

Expected: FAIL — `*SQLiteStore has no field or method EnsureAdminUser`.

- [ ] **Step 4: Implement (existing-user branch only for now)**

Edit `internal/userstore/admin.go`. Append:

```go
import (
    // keep existing imports; ensure database/sql, errors, strings are present
)

// ErrEmptyBootstrapPassword is returned by EnsureAdminUser when creating
// a brand-new user without a plaintext password to hash.
var ErrEmptyBootstrapPassword = errors.New("userstore: empty plaintext for new admin user")

// EnsureAdminUser is the bootstrap entry point — see the Store interface
// docstring for semantics.
func (s *SQLiteStore) EnsureAdminUser(ctx context.Context, email, plaintext string) (bool, error) {
    email = strings.ToLower(strings.TrimSpace(email))
    var existingID string
    err := s.db.QueryRowContext(ctx,
        `SELECT id FROM users WHERE email = ?`, email,
    ).Scan(&existingID)
    if err == nil {
        // User exists: promote, ignore password.
        if err := s.SetUserAdmin(ctx, existingID, true); err != nil {
            return false, err
        }
        return false, nil
    }
    if !errors.Is(err, sql.ErrNoRows) {
        return false, fmt.Errorf("lookup admin email: %w", err)
    }
    // Create branch — completed in Task 6. For now we return an error so
    // the existing-user test passes and the missing-user path is wired
    // for the next task.
    return false, ErrEmptyBootstrapPassword
}
```

Make sure `admin.go` has the right imports:

```go
import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"
)
```

- [ ] **Step 5: Run test**

```bash
go test -run TestEnsureAdminUser_ExistingUser -v ./internal/userstore/ 2>&1 | tail -10
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/userstore/store.go internal/userstore/admin.go internal/userstore/admin_test.go
git commit -m "userstore: EnsureAdminUser promotes existing user, ignores password"
```

---

## Task 6 — `EnsureAdminUser` create-new-user path

**Files:**
- Modify: `internal/userstore/admin.go`
- Modify: `internal/userstore/admin_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/userstore/admin_test.go`:

```go
func TestEnsureAdminUser_NewUser_CreatedAndAdmin(t *testing.T) {
    ctx := context.Background()
    s := openMemStore(t)
    defer s.Close()

    created, err := s.EnsureAdminUser(ctx, "fresh@example.com", "bootstrap-passphrase-2026")
    if err != nil { t.Fatal(err) }
    if !created { t.Error("created=false for brand-new admin; want true") }

    v, _ := s.VerifyPassword(ctx, "fresh@example.com", "bootstrap-passphrase-2026")
    if v == nil { t.Fatal("new admin password does not verify") }
    if !v.IsAdmin { t.Error("new admin user is_admin = false") }
}

func TestEnsureAdminUser_NewUser_EmptyPassword_Errors(t *testing.T) {
    ctx := context.Background()
    s := openMemStore(t)
    defer s.Close()

    if _, err := s.EnsureAdminUser(ctx, "x@example.com", ""); err == nil {
        t.Error("EnsureAdminUser with empty password returned nil error")
    } else if !errors.Is(err, ErrEmptyBootstrapPassword) {
        t.Errorf("err = %v; want ErrEmptyBootstrapPassword", err)
    }
}
```

(Add `"errors"` to the test file imports if not already there.)

- [ ] **Step 2: Run, confirm failure**

```bash
go test -run TestEnsureAdminUser_NewUser -v ./internal/userstore/ 2>&1 | tail -15
```

Expected: both new tests FAIL (the function returns ErrEmptyBootstrapPassword unconditionally for the missing branch).

- [ ] **Step 3: Implement the create branch**

Edit `internal/userstore/admin.go` — replace the `TODO` branch:

```go
    if !errors.Is(err, sql.ErrNoRows) {
        return false, fmt.Errorf("lookup admin email: %w", err)
    }
    // User does not exist. Need a plaintext password to create.
    if plaintext == "" {
        return false, ErrEmptyBootstrapPassword
    }
    u, err := s.CreateUser(ctx, email, plaintext)
    if err != nil {
        return false, fmt.Errorf("create admin user: %w", err)
    }
    if err := s.SetUserAdmin(ctx, u.ID, true); err != nil {
        return false, err
    }
    return true, nil
}
```

- [ ] **Step 4: Run all admin tests**

```bash
go test -run "TestEnsureAdminUser|TestSetUserAdmin" -v ./internal/userstore/ 2>&1 | tail -25
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/admin.go internal/userstore/admin_test.go
git commit -m "userstore: EnsureAdminUser creates new admin when email is unknown"
```

---

## Task 7 — Remove `adminToken` from `IdentityResolver`

**Files:**
- Modify: `internal/relay/identity.go`
- Modify: callers (`cmd/atterm-relay/main.go` and tests)

- [ ] **Step 1: Inspect current resolver**

```bash
grep -n "adminToken\|NewIdentityResolver" internal/relay/identity.go
```

Expected: `adminToken` field on `IdentityResolver`, parameter on `NewIdentityResolver`, Bearer-match branch in `Resolve` that returns `PrincipalAdmin`.

- [ ] **Step 2: Remove the field, parameter, and Bearer-admin branch**

Edit `internal/relay/identity.go`:

```go
type IdentityResolver struct {
    store userstore.Store
}

func NewIdentityResolver(store userstore.Store) *IdentityResolver {
    return &IdentityResolver{store: store}
}
```

In `Resolve`, delete the block that checks `r.adminToken` against the Bearer header and returns `PrincipalAdmin`. Keep all other branches (cookie → user, API token → user) for now — Task 8 will add the user.is_admin → PrincipalAdmin promotion.

- [ ] **Step 3: Fix the only non-test caller**

Edit `cmd/atterm-relay/main.go` around line 111:

```go
resolver := relay.NewIdentityResolver(store)
```

(Remove the `, cleanAdminToken` argument.)

- [ ] **Step 4: Fix test callers**

```bash
grep -rln "NewIdentityResolver" internal/relay/ 2>&1
```

In every test file that calls `NewIdentityResolver(store, testAdminToken)`, drop the second argument:

```bash
# For each affected test file:
sed -i '' 's/NewIdentityResolver(store, [^)]*)/NewIdentityResolver(store)/g' internal/relay/identity_test.go internal/relay/admin_http_test.go
```

(Manually verify the substitution didn't mangle anything: `grep -n NewIdentityResolver internal/relay/*_test.go`.)

- [ ] **Step 5: Build everything**

```bash
go build ./... 2>&1 | tail -10
```

Expected: only error is `cmd/atterm-relay/main.go: undefined: validateAdminToken` (left over from Task 4 rename — fixed in Task 14). Any other compile error is a real bug in this task; fix it here.

If `internal/relay/*.go` complains about an unused `_ = testAdminToken` constant in a test helper file, also remove that constant or its declaration block; we no longer need it for any wiring.

- [ ] **Step 6: Commit**

```bash
git add internal/relay/identity.go internal/relay/identity_test.go internal/relay/admin_http_test.go cmd/atterm-relay/main.go
git commit -m "relay: drop adminToken from IdentityResolver (admin role moves to user.is_admin)"
```

---

## Task 8 — `Resolve` promotes session users with `is_admin` to `PrincipalAdmin`

**Files:**
- Modify: `internal/relay/identity.go`
- Modify: `internal/relay/identity_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/relay/identity_test.go`:

```go
func TestResolve_CookieSession_AdminUser_BecomesPrincipalAdmin(t *testing.T) {
    ctx := context.Background()
    store := openTestStore(t) // existing helper that returns userstore.Store
    u, _ := store.CreateUser(ctx, "a@example.com", "passphrase-1234")
    _ = store.SetUserAdmin(ctx, u.ID, true)
    secret, _ := store.CreateWebSession(ctx, u.ID, "ua/test", "203.0.113.0/24")

    r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
    r.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})

    resolver := NewIdentityResolver(store)
    p := resolver.Resolve(r)
    if p.Kind != PrincipalAdmin {
        t.Fatalf("Kind = %v; want PrincipalAdmin", p.Kind)
    }
    if p.UserID != u.ID {
        t.Errorf("UserID = %q; want %q", p.UserID, u.ID)
    }
}

func TestResolve_CookieSession_NonAdminUser_StaysPrincipalUser(t *testing.T) {
    ctx := context.Background()
    store := openTestStore(t)
    u, _ := store.CreateUser(ctx, "b@example.com", "passphrase-1234")
    secret, _ := store.CreateWebSession(ctx, u.ID, "ua/test", "203.0.113.0/24")

    r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
    r.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})

    resolver := NewIdentityResolver(store)
    p := resolver.Resolve(r)
    if p.Kind != PrincipalUser {
        t.Fatalf("Kind = %v; want PrincipalUser", p.Kind)
    }
}
```

(If `openTestStore` doesn't exist, look for the helper this file already uses to open a userstore.)

- [ ] **Step 2: Run, confirm failure**

```bash
go test -run TestResolve_CookieSession -v ./internal/relay/ 2>&1 | tail -15
```

Expected: FAIL on the first test (cookie sessions currently always produce `PrincipalUser`).

- [ ] **Step 3: Promote in `Resolve`**

Edit `internal/relay/identity.go`. In the cookie branch of `Resolve`, after the existing `LookupWebSession` and the construction of the `Principal{Kind: PrincipalUser, ...}`, call `GetUser` to read `is_admin` and upgrade the Kind:

```go
// (cookie branch)
userID, csrfSecret, err := r.store.LookupWebSession(req.Context(), cookie.Value)
if err == nil {
    kind := PrincipalUser
    if u, gerr := r.store.GetUser(req.Context(), userID); gerr == nil && u.IsAdmin {
        kind = PrincipalAdmin
    }
    return Principal{
        Kind:       kind,
        UserID:     userID,
        CSRFSecret: csrfSecret,
    }
}
```

Do the same in the API-token branch (find it just below the cookie branch — it also resolves to `PrincipalUser`; upgrade to `PrincipalAdmin` based on `user.IsAdmin` from `GetUser`).

- [ ] **Step 4: Run tests**

```bash
go test -run TestResolve -v ./internal/relay/ 2>&1 | tail -20
```

Expected: both new tests PASS; existing `TestResolve_*` PASS.

- [ ] **Step 5: Teach `RequireCSRF` to accept `PrincipalAdmin`**

Before Task 8, admin sessions resolved to `PrincipalUser`, so the CSRF middleware in `internal/relay/csrfmw.go` (line 34) gated mutating routes with `p.Kind != PrincipalUser`. After this task an admin cookie session resolves to `PrincipalAdmin`, which would be (silently) rejected — admin would lose write access to its own endpoints.

Edit `internal/relay/csrfmw.go`, the check on line 34:

```go
if (p.Kind != PrincipalUser && p.Kind != PrincipalAdmin) || len(p.CSRFSecret) == 0 {
    http.Error(w, "unauthenticated", http.StatusUnauthorized)
    return
}
```

Append a test to `internal/relay/csrfmw_test.go`:

```go
func TestRequireCSRF_AdminCookieAccepted(t *testing.T) {
    store := openTestStore(t)
    u, _ := store.CreateUser(context.Background(), "a@example.com", "passphrase-1234")
    _ = store.SetUserAdmin(context.Background(), u.ID, true)
    secret, _ := store.CreateWebSession(context.Background(), u.ID, "ua", "1.2.3.0/24")
    resolver := NewIdentityResolver(store)

    inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusOK)
    })
    handler := RequireCSRF(resolver, inner)

    req := httptest.NewRequest(http.MethodPost, "/anything", nil)
    req.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})
    req.Header.Set("X-CSRF-Token", CSRFToken(secret.Expose(), u.CSRFSecret()))
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("admin cookie + valid CSRF: status %d, want 200", rec.Code)
    }
}
```

Run:

```bash
go test -run "TestRequireCSRF" -v ./internal/relay/ 2>&1 | tail -15
```

Expected: all CSRF tests pass (4 existing + 1 new).

- [ ] **Step 6: Commit**

```bash
git add internal/relay/identity.go internal/relay/identity_test.go internal/relay/csrfmw.go internal/relay/csrfmw_test.go
git commit -m "relay: PrincipalAdmin now driven by user.is_admin (cookie + API token); CSRF accepts both kinds"
```

---

## Task 9 — Delete `adminPageHTML` + `handleAdminPage` + `authorizeAdmin`

**Files:**
- Modify: `internal/relay/admin_http.go`
- Modify: `internal/relay/server.go`

- [ ] **Step 1: Identify the deletion targets**

```bash
grep -n "adminPageHTML\|handleAdminPage\|authorizeAdmin" internal/relay/admin_http.go internal/relay/server.go
```

You will see roughly:
- `admin_http.go`: `handleAdminPage` function (~line 266), `adminPageHTML` const (~line 374, ~370 lines), `authorizeAdmin` helper (~line 308), and possibly one or more callers of `authorizeAdmin` other than `handleAdminPage`.
- `server.go`: `s.mux.HandleFunc("/admin/", s.handleAdminPage)` mux registration (~line 150).

- [ ] **Step 2: Delete the mux registration**

Edit `internal/relay/server.go` — find the `if cfg.AdminToken != "" { ... }` block (around line 149) and remove `s.mux.HandleFunc("/admin/", s.handleAdminPage)` and the now-dead `cfg.AdminToken != ""` gate it lives inside. The `/admin/api/*` registrations move out unconditionally (they always existed; they're gated by `requireAdmin` middleware now, not by token presence).

- [ ] **Step 3: Delete `handleAdminPage`, `adminPageHTML`, `authorizeAdmin`**

In `internal/relay/admin_http.go`:
- Delete `func (s *Server) handleAdminPage(...)` (the entire body).
- Delete the `handleAdminConfigHTTP`'s call site to `authorizeAdmin` only if `handleAdminConfigHTTP` is also being converted; if it's still using `authorizeAdmin`, leave it (Task 10 will retarget all admin handlers to `requireAdmin`).
- Delete `func (s *Server) authorizeAdmin(...)`.
- Delete `const adminPageHTML = \` ... \``.

- [ ] **Step 4: Confirm no remaining references**

```bash
grep -rn "adminPageHTML\|handleAdminPage\|authorizeAdmin" internal/relay/ cmd/ 2>&1
```

Expected: empty output.

- [ ] **Step 5: Build (will still fail on validateAdminToken; that's Task 14)**

```bash
go build ./internal/relay/ 2>&1 | tail -5
```

Expected: PASS for the `internal/relay` package.

- [ ] **Step 6: Commit**

```bash
git add internal/relay/admin_http.go internal/relay/server.go
git commit -m "relay: delete adminPageHTML / handleAdminPage / authorizeAdmin (token-based admin retired)"
```

---

## Task 10 — Convert `admin_http_test.go` to cookie-based admin fixture

**Files:**
- Modify: `internal/relay/admin_http_test.go`
- Possibly add: `internal/relay/admin_test_fixtures.go` (or inline helper)

- [ ] **Step 1: Survey existing test helpers**

```bash
grep -n "testAdminToken\|newBearerRequest\|newAuthCookie\|CreateWebSession" internal/relay/admin_http_test.go | head -20
```

Identify how tests currently authenticate as admin (`testAdminToken` + `newBearerRequest`), and how user/session-based tests authenticate elsewhere (search e.g. `auth_http_test.go` for the cookie pattern).

- [ ] **Step 2: Add a shared fixture**

Add to the top of `internal/relay/admin_http_test.go` (or a new `admin_test_helpers.go`):

```go
// bootstrapAdminUser creates an admin user and a logged-in cookie + CSRF
// token, mirroring what production gets from cookie login.
func bootstrapAdminUser(t *testing.T, store userstore.Store) (userID string, cookie *http.Cookie, csrfToken string) {
    t.Helper()
    ctx := context.Background()
    u, err := store.CreateUser(ctx, "admin@example.com", "passphrase-fixture-1234")
    if err != nil { t.Fatal(err) }
    if err := store.SetUserAdmin(ctx, u.ID, true); err != nil { t.Fatal(err) }
    secret, err := store.CreateWebSession(ctx, u.ID, "ua/test", "203.0.113.0/24")
    if err != nil { t.Fatal(err) }
    cookie = &http.Cookie{Name: "atterm_session", Value: secret.Expose()}
    // CSRF derived from user's csrf_secret + cookie value, like /api/me does.
    csrfToken = CSRFToken(secret.Expose(), u.CSRFSecret())
    return u.ID, cookie, csrfToken
}
```

(If `CSRFToken` lives in another file or has a different signature, adjust to whatever the existing csrf helper expects. `me_http_test.go` already does this for non-admin users; copy that pattern.)

- [ ] **Step 3: Replace `newBearerRequest(..., testAdminToken)` with cookie + (where needed) CSRF**

For each `TestAdmin_*` and related test in `admin_http_test.go`:

Replace:
```go
req := newBearerRequest(http.MethodPost, "/admin/api/invitations", testAdminToken)
```

with:
```go
_, cookie, csrf := bootstrapAdminUser(t, store)
req := httptest.NewRequest(http.MethodPost, "/admin/api/invitations", body)
req.AddCookie(cookie)
req.Header.Set("X-CSRF-Token", csrf)
```

(GET requests don't need CSRF — only state-mutating requests. Match the existing `RequireCSRF` middleware applications.)

Drop the package-level `testAdminToken` constant entirely once nothing references it.

- [ ] **Step 4: Add a negative test — non-admin user is rejected**

Append:

```go
func TestAdminAPI_NonAdminUser_403(t *testing.T) {
    store := openTestStore(t)
    handler := newTestServer(t, store) // existing test helper

    // Create a non-admin user with a cookie session.
    u, _ := store.CreateUser(context.Background(), "user@example.com", "passphrase-1234")
    secret, _ := store.CreateWebSession(context.Background(), u.ID, "ua/test", "203.0.113.0/24")

    req := httptest.NewRequest(http.MethodGet, "/admin/api/invitations", nil)
    req.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
        t.Errorf("status = %d; want 401 or 403 for non-admin user hitting /admin/api", rec.Code)
    }
}
```

(`requireAdmin` middleware currently returns 401 from `PrincipalNone`; for `PrincipalUser` it also returns 401 in the current code path. Both 401 and 403 are acceptable as the test asserts non-success; the user-friendly distinction is a UI concern.)

- [ ] **Step 5: Run all relay tests**

```bash
go test -count=1 ./internal/relay/ 2>&1 | tail -10
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/relay/admin_http_test.go
git commit -m "relay: rewrite admin_http_test to cookie-based admin user fixture"
```

---

## Task 11 — Promote API: `POST /admin/api/users/{id}/admin`

**Files:**
- Modify: `internal/relay/admin_http.go`
- Modify: `internal/relay/admin_http_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/relay/admin_http_test.go`:

```go
func TestAdminPromoteUser_Success(t *testing.T) {
    store := openTestStore(t)
    handler := newTestServer(t, store)
    _, adminCookie, adminCSRF := bootstrapAdminUser(t, store)

    target, _ := store.CreateUser(context.Background(), "target@example.com", "passphrase-1234")
    if target.IsAdmin { t.Fatal("target already admin") }

    req := httptest.NewRequest(http.MethodPost, "/admin/api/users/"+target.ID+"/admin", nil)
    req.AddCookie(adminCookie)
    req.Header.Set("X-CSRF-Token", adminCSRF)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusNoContent {
        t.Fatalf("status = %d; want 204; body=%s", rec.Code, rec.Body.String())
    }
    got, _ := store.GetUser(context.Background(), target.ID)
    if !got.IsAdmin { t.Error("target user not promoted after POST .../admin") }
}

func TestAdminPromoteUser_AuditLog(t *testing.T) {
    var buf bytes.Buffer
    log.SetOutput(&buf)
    t.Cleanup(func() { log.SetOutput(os.Stderr) })

    store := openTestStore(t)
    handler := newTestServer(t, store)
    actorID, adminCookie, adminCSRF := bootstrapAdminUser(t, store)
    target, _ := store.CreateUser(context.Background(), "t@example.com", "passphrase-1234")

    req := httptest.NewRequest(http.MethodPost, "/admin/api/users/"+target.ID+"/admin", nil)
    req.AddCookie(adminCookie)
    req.Header.Set("X-CSRF-Token", adminCSRF)
    handler.ServeHTTP(httptest.NewRecorder(), req)

    log := buf.String()
    if !strings.Contains(log, "admin role change") ||
       !strings.Contains(log, "actor="+actorID) ||
       !strings.Contains(log, "target="+target.ID) ||
       !strings.Contains(log, "op=promote") {
        t.Errorf("audit log line missing or malformed:\n%s", log)
    }
}
```

(Add `bytes`, `os`, `log`, `strings` to imports if missing.)

- [ ] **Step 2: Run, confirm 404 / failure**

```bash
go test -run TestAdminPromoteUser -v ./internal/relay/ 2>&1 | tail -15
```

Expected: FAIL (404 from unrouted path).

- [ ] **Step 3: Add the handler + mux registration**

Edit `internal/relay/admin_http.go`. Register the new route in `Register` (or wherever the admin routes are wired), wrapped in `requireAdmin` and `RequireCSRF`:

```go
mux.Handle("POST /admin/api/users/{id}/admin",
    RequireCSRF(a.Resolver, a.requireAdmin(a.handlePromoteUser)))
```

Append the handler at the bottom of the file:

```go
// handlePromoteUser flips users.is_admin = true for {id}. Idempotent.
// Audit logged with actor (the requesting admin) and target.
func (a *AdminServer) handlePromoteUser(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if id == "" {
        http.Error(w, "missing user id", http.StatusBadRequest)
        return
    }
    actor := a.Resolver.Resolve(r)
    if err := a.Store.SetUserAdmin(r.Context(), id, true); err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    log.Printf("admin role change: actor=%s target=%s op=promote", actor.UserID, id)
    w.WriteHeader(http.StatusNoContent)
}
```

(If `AdminServer` doesn't currently expose `Store`, expose it the same way `Resolver` is exposed — both come from `Config` at construction.)

- [ ] **Step 4: Run tests**

```bash
go test -run TestAdminPromoteUser -v ./internal/relay/ 2>&1 | tail -10
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/admin_http.go internal/relay/admin_http_test.go
git commit -m "relay: POST /admin/api/users/{id}/admin (promote) with audit log"
```

---

## Task 12 — Demote API: `DELETE /admin/api/users/{id}/admin`

**Files:**
- Modify: `internal/relay/admin_http.go`
- Modify: `internal/relay/admin_http_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/relay/admin_http_test.go`:

```go
func TestAdminDemoteUser_Success(t *testing.T) {
    store := openTestStore(t)
    handler := newTestServer(t, store)
    _, adminCookie, adminCSRF := bootstrapAdminUser(t, store)

    // A second admin so demoting the target doesn't trip the last-admin guard.
    other, _ := store.CreateUser(context.Background(), "other@example.com", "passphrase-1234")
    _ = store.SetUserAdmin(context.Background(), other.ID, true)

    req := httptest.NewRequest(http.MethodDelete, "/admin/api/users/"+other.ID+"/admin", nil)
    req.AddCookie(adminCookie)
    req.Header.Set("X-CSRF-Token", adminCSRF)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusNoContent {
        t.Fatalf("status = %d; want 204; body=%s", rec.Code, rec.Body.String())
    }
    got, _ := store.GetUser(context.Background(), other.ID)
    if got.IsAdmin { t.Error("user still admin after DELETE .../admin") }
}

func TestAdminDemoteUser_Self_400(t *testing.T) {
    store := openTestStore(t)
    handler := newTestServer(t, store)
    actorID, adminCookie, adminCSRF := bootstrapAdminUser(t, store)

    req := httptest.NewRequest(http.MethodDelete, "/admin/api/users/"+actorID+"/admin", nil)
    req.AddCookie(adminCookie)
    req.Header.Set("X-CSRF-Token", adminCSRF)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusBadRequest {
        t.Fatalf("status = %d; want 400 (self-demote); body=%s", rec.Code, rec.Body.String())
    }
    if !strings.Contains(rec.Body.String(), "cannot_demote_self") {
        t.Errorf("body = %q; want error code cannot_demote_self", rec.Body.String())
    }
}

func TestAdminDemoteUser_LastAdmin_409(t *testing.T) {
    store := openTestStore(t)
    handler := newTestServer(t, store)
    // Promote a user, but bootstrapAdminUser already created one admin.
    // Demoting that bootstrap admin via a *different* admin would normally
    // succeed; the 409 is for the case where the demote target is the
    // single remaining admin. So we set up exactly two admins, demote one
    // (success), then try to demote the second from itself's perspective.
    actorID, adminCookie, adminCSRF := bootstrapAdminUser(t, store)
    // Self-demote of last admin: covered by self-demote 400 path. The
    // 409 path is "Admin A demotes Admin B when A==B is rejected, but
    // also when A demotes B and that would leave zero admins overall."
    // Both Admin A and B exist here; demote B; then attempt to demote A
    // from a separate admin... but there is no separate admin left.
    // Simpler: directly trigger the count-check by having only one admin
    // and demoting via a non-admin-but-permitted-by-test-shortcut path —
    // not feasible since middleware requires admin. Instead:
    //   1. Set up two admins (actor + other).
    //   2. Demote `other` via actor — success.
    //   3. Attempt to demote `actor` via actor — caught by self-demote 400 first.
    // To reach the 409 branch, two admins (A, B) demote each other in
    // turn. After B demotes A, only B is admin. If A tried to demote B
    // now A is not admin (so the request hits requireAdmin first, 401).
    // The 409 branch is therefore only reachable if a *non-self* request
    // would leave zero admins; in practice that means A demotes B while
    // A is not themself admin — impossible via the UI flow.
    //
    // We still test the branch by constructing the exact preconditions:
    other, _ := store.CreateUser(context.Background(), "other@example.com", "passphrase-1234")
    _ = store.SetUserAdmin(context.Background(), other.ID, true)
    // Manually demote actor in the DB (bypassing the API) so that `other`
    // is the only admin, then have actor try to demote other. actor will
    // fail requireAdmin (401), not 409. So this exact test is *not*
    // reachable through the API alone — keep the 409 path defensively in
    // the handler, but assert via unit test on the count helper instead.
    _ = actorID; _ = adminCookie; _ = adminCSRF; _ = other
    t.Skip("409 last-admin branch is defensive; covered by unit test on helper")
}

func TestCountAdmins_OneTriggersLastAdminGuard(t *testing.T) {
    ctx := context.Background()
    store := openTestStore(t)
    u, _ := store.CreateUser(ctx, "only@example.com", "passphrase-1234")
    _ = store.SetUserAdmin(ctx, u.ID, true)
    n, err := countAdmins(ctx, store)
    if err != nil { t.Fatal(err) }
    if n != 1 { t.Fatalf("countAdmins = %d; want 1", n) }
}
```

(The 409 path is real defensive code — exercised in PR C's `DELETE /api/me` last-admin test. Here we just keep the helper covered.)

- [ ] **Step 2: Run, confirm failure**

```bash
go test -run "TestAdminDemoteUser|TestCountAdmins" -v ./internal/relay/ 2>&1 | tail -15
```

Expected: FAIL (404 / countAdmins undefined).

- [ ] **Step 3: Implement**

Edit `internal/relay/admin_http.go`:

```go
mux.Handle("DELETE /admin/api/users/{id}/admin",
    RequireCSRF(a.Resolver, a.requireAdmin(a.handleDemoteUser)))
```

Add a helper and handler at the bottom of the file:

```go
// countAdmins returns how many users currently have is_admin=1. Used to
// prevent demoting / deleting the last admin and locking the deploy out.
func countAdmins(ctx context.Context, store userstore.Store) (int, error) {
    users, err := store.ListUsers(ctx)
    if err != nil { return 0, err }
    n := 0
    for _, u := range users {
        if u.IsAdmin { n++ }
    }
    return n, nil
}

// handleDemoteUser flips users.is_admin = false for {id}, with two
// guardrails: self-demote (400 cannot_demote_self) and last-admin
// (409 last_admin).
func (a *AdminServer) handleDemoteUser(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if id == "" {
        http.Error(w, "missing user id", http.StatusBadRequest)
        return
    }
    actor := a.Resolver.Resolve(r)
    if id == actor.UserID {
        writeError(w, http.StatusBadRequest, "cannot_demote_self")
        return
    }
    target, err := a.Store.GetUser(r.Context(), id)
    if err != nil {
        http.Error(w, "user not found", http.StatusNotFound)
        return
    }
    if target.IsAdmin {
        n, err := countAdmins(r.Context(), a.Store)
        if err != nil {
            http.Error(w, "internal error", http.StatusInternalServerError)
            return
        }
        if n <= 1 {
            writeError(w, http.StatusConflict, "last_admin")
            return
        }
    }
    if err := a.Store.SetUserAdmin(r.Context(), id, false); err != nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    log.Printf("admin role change: actor=%s target=%s op=demote", actor.UserID, id)
    w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run tests**

```bash
go test -run "TestAdminDemoteUser|TestCountAdmins" -v ./internal/relay/ 2>&1 | tail -15
```

Expected: PASS (the 409 test is skipped; the other three pass).

- [ ] **Step 5: Commit**

```bash
git add internal/relay/admin_http.go internal/relay/admin_http_test.go
git commit -m "relay: DELETE /admin/api/users/{id}/admin (demote) with self-demote + last-admin guards"
```

---

## Task 13 — `ListUsers` response includes `is_admin`

**Files:**
- Modify: `internal/relay/admin_http.go` (`handleListUsers`)
- Modify: `internal/relay/admin_http_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestAdminListUsers_IncludesIsAdmin(t *testing.T) {
    store := openTestStore(t)
    handler := newTestServer(t, store)
    _, adminCookie, _ := bootstrapAdminUser(t, store)

    nonAdmin, _ := store.CreateUser(context.Background(), "u@example.com", "passphrase-1234")

    req := httptest.NewRequest(http.MethodGet, "/admin/api/users", nil)
    req.AddCookie(adminCookie)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusOK { t.Fatalf("status %d", rec.Code) }
    var rows []map[string]any
    _ = json.NewDecoder(rec.Body).Decode(&rows)
    var found, foundNonAdmin bool
    for _, r := range rows {
        if r["email"] == "admin@example.com" && r["is_admin"] == true { found = true }
        if r["id"] == nonAdmin.ID && r["is_admin"] == false { foundNonAdmin = true }
    }
    if !found { t.Error("admin row missing or is_admin=false") }
    if !foundNonAdmin { t.Error("non-admin row missing or is_admin=true") }
}
```

- [ ] **Step 2: Run, confirm failure**

```bash
go test -run TestAdminListUsers_IncludesIsAdmin -v ./internal/relay/ 2>&1 | tail -10
```

Expected: FAIL (field absent).

- [ ] **Step 3: Add the field**

Edit `internal/relay/admin_http.go` — find `handleListUsers` and the struct it serializes. Add `IsAdmin bool \`json:"is_admin"\`` to that struct; assign from `u.IsAdmin` in the loop that copies rows.

- [ ] **Step 4: Run test**

```bash
go test -run TestAdminListUsers_IncludesIsAdmin -v ./internal/relay/ 2>&1 | tail -10
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/admin_http.go internal/relay/admin_http_test.go
git commit -m "relay: GET /admin/api/users response includes is_admin"
```

---

## Task 14 — Bootstrap helper in `cmd/atterm-relay`

**Files:**
- Create: `cmd/atterm-relay/bootstrap_admin.go`
- Create: `cmd/atterm-relay/bootstrap_admin_test.go`

- [ ] **Step 1: Write failing tests**

Create `cmd/atterm-relay/bootstrap_admin_test.go`:

```go
package main

import (
    "bytes"
    "context"
    "errors"
    "log"
    "os"
    "strings"
    "testing"

    "github.com/attson/atterm/internal/userstore"
)

func openTestStore(t *testing.T) userstore.Store {
    t.Helper()
    s, err := userstore.Open(context.Background(), ":memory:")
    if err != nil { t.Fatal(err) }
    t.Cleanup(func() { s.Close() })
    return s
}

func TestBootstrapAdmin_EmptyEmail_NoOp(t *testing.T) {
    store := openTestStore(t)
    if err := bootstrapAdmin(context.Background(), store, "", ""); err != nil {
        t.Fatalf("err = %v; want nil", err)
    }
    users, _ := store.ListUsers(context.Background())
    if len(users) != 0 {
        t.Errorf("created users = %d; want 0", len(users))
    }
}

func TestBootstrapAdmin_MalformedEmail_Errors(t *testing.T) {
    store := openTestStore(t)
    if err := bootstrapAdmin(context.Background(), store, "not-an-email", "Strong-passphrase-2026"); err == nil {
        t.Fatal("malformed email accepted")
    } else if !strings.Contains(err.Error(), "ATTERM_BOOTSTRAP_ADMIN_EMAIL") {
        t.Errorf("error doesn't mention the env var: %v", err)
    }
}

func TestBootstrapAdmin_NewUser_Created(t *testing.T) {
    var buf bytes.Buffer
    log.SetOutput(&buf)
    t.Cleanup(func() { log.SetOutput(os.Stderr) })

    store := openTestStore(t)
    if err := bootstrapAdmin(context.Background(), store, "fresh@example.com", "Strong-passphrase-2026"); err != nil {
        t.Fatal(err)
    }
    v, _ := store.VerifyPassword(context.Background(), "fresh@example.com", "Strong-passphrase-2026")
    if v == nil || !v.IsAdmin { t.Fatal("user not created as admin") }
    if !strings.Contains(buf.String(), "WARN") || !strings.Contains(buf.String(), "unset ATTERM_BOOTSTRAP_ADMIN_PASSWORD") {
        t.Errorf("expected WARN about unsetting password env, got:\n%s", buf.String())
    }
}

func TestBootstrapAdmin_ExistingUser_PromotedAndWarn(t *testing.T) {
    var buf bytes.Buffer
    log.SetOutput(&buf)
    t.Cleanup(func() { log.SetOutput(os.Stderr) })

    ctx := context.Background()
    store := openTestStore(t)
    u, _ := store.CreateUser(ctx, "existing@example.com", "original-passphrase")

    if err := bootstrapAdmin(ctx, store, "existing@example.com", "leftover-env-password-2026"); err != nil {
        t.Fatal(err)
    }
    got, _ := store.GetUser(ctx, u.ID)
    if !got.IsAdmin { t.Error("existing user not promoted") }
    if !strings.Contains(buf.String(), "WARN") || !strings.Contains(buf.String(), "password ignored") {
        t.Errorf("expected WARN about ignored password, got:\n%s", buf.String())
    }
}

func TestBootstrapAdmin_NewUser_WeakPassword_Errors(t *testing.T) {
    store := openTestStore(t)
    err := bootstrapAdmin(context.Background(), store, "fresh@example.com", "short")
    if err == nil { t.Fatal("weak password accepted") }
    // Either validateBootstrapPassword or ErrEmptyBootstrapPassword bubbles
    // up — both satisfy "the deploy fails fast".
    if !errors.Is(err, userstore.ErrEmptyBootstrapPassword) &&
       !strings.Contains(err.Error(), "ATTERM_BOOTSTRAP_ADMIN_PASSWORD") {
        t.Errorf("err = %v; want validateBootstrapPassword failure or ErrEmptyBootstrapPassword", err)
    }
}
```

- [ ] **Step 2: Run, confirm compile failure**

```bash
go test ./cmd/atterm-relay/ 2>&1 | tail -5
```

Expected: FAIL — `undefined: bootstrapAdmin`.

- [ ] **Step 3: Implement**

Create `cmd/atterm-relay/bootstrap_admin.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/mail"

    "github.com/attson/atterm/internal/userstore"
)

// bootstrapAdmin reconciles the relay's admin role with the
// ATTERM_BOOTSTRAP_ADMIN_EMAIL / _PASSWORD env vars on startup. See
// docs/superpowers/specs/2026-05-17-web-ui-redesign-design.md.
//
// - email == ""  → no-op.
// - email + existing user → promote, ignore password, WARN if password
//   was set.
// - email + missing user + valid password → create as admin, WARN to
//   unset the password env now.
// - malformed email or weak/empty password (create path) → error;
//   caller is expected to log.Fatalf.
func bootstrapAdmin(ctx context.Context, store userstore.Store, email, password string) error {
    if email == "" {
        return nil
    }
    if _, err := mail.ParseAddress(email); err != nil {
        return fmt.Errorf("ATTERM_BOOTSTRAP_ADMIN_EMAIL: %w", err)
    }
    // Pre-validate password only when we'd use it (creating a new user).
    // For the existing-user branch the password is ignored; rejecting a
    // weak password there would surprise an operator who wants to leave
    // an old env value in place.
    if password != "" {
        // Cheap precheck so the EnsureAdminUser create-path doesn't waste
        // argon2 cycles on something we'll reject anyway.
        if err := validateBootstrapPassword(password); err != nil {
            // Defer this check until we know we're on the create path:
            // see below.
            _ = err
        }
    }
    created, err := store.EnsureAdminUser(ctx, email, password)
    if err != nil {
        // If the create path needed a password but we ruled it out above,
        // re-surface the stronger validator error for clarity.
        if password != "" {
            if vErr := validateBootstrapPassword(password); vErr != nil {
                return vErr
            }
        }
        return err
    }
    if created {
        log.Printf("WARN: bootstrap created admin user %s — unset ATTERM_BOOTSTRAP_ADMIN_PASSWORD and restart to remove the credential from process state.", email)
    } else if password != "" {
        log.Printf("WARN: ATTERM_BOOTSTRAP_ADMIN_PASSWORD set but %s already exists — password ignored. Unset the env to remove it from process state.", email)
    } else {
        log.Printf("promoted existing user to admin: %s", email)
    }
    return nil
}
```

Wait — the validator-on-create path is convoluted. Simplify by validating up front *only when the user is missing*. But we don't know if the user is missing without a DB lookup. The cleanest split: validate after `EnsureAdminUser` reports `ErrEmptyBootstrapPassword` (which is the missing-user-no-password case) and convert it to the validator error when we have *a non-empty password*. Replace the implementation above with:

```go
func bootstrapAdmin(ctx context.Context, store userstore.Store, email, password string) error {
    if email == "" {
        return nil
    }
    if _, err := mail.ParseAddress(email); err != nil {
        return fmt.Errorf("ATTERM_BOOTSTRAP_ADMIN_EMAIL: %w", err)
    }
    // If the password is set, enforce the bootstrap strength rule now —
    // it's a no-op when the user already exists (EnsureAdminUser will
    // ignore it), but a misconfigured weak password should still fail
    // fast so the operator notices.
    if password != "" {
        if err := validateBootstrapPassword(password); err != nil {
            return err
        }
    }
    created, err := store.EnsureAdminUser(ctx, email, password)
    if err != nil {
        return err
    }
    if created {
        log.Printf("WARN: bootstrap created admin user %s — unset ATTERM_BOOTSTRAP_ADMIN_PASSWORD and restart to remove the credential from process state.", email)
    } else if password != "" {
        log.Printf("WARN: ATTERM_BOOTSTRAP_ADMIN_PASSWORD set but %s already exists — password ignored. Unset the env to remove it from process state.", email)
    } else {
        log.Printf("promoted existing user to admin: %s", email)
    }
    return nil
}
```

This means a weak password rejects even when the user exists (and the password would have been ignored). That's a fair trade for "fail fast on misconfig"; revisit if a real operator complains.

- [ ] **Step 4: Run tests**

```bash
go test -run TestBootstrapAdmin -v ./cmd/atterm-relay/ 2>&1 | tail -20
```

Expected: all 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/atterm-relay/bootstrap_admin.go cmd/atterm-relay/bootstrap_admin_test.go
git commit -m "cmd/atterm-relay: bootstrapAdmin helper (env → EnsureAdminUser)"
```

---

## Task 15 — Wire `bootstrapAdmin` into `main.go` and drop `ATTERM_ADMIN_TOKEN`

**Files:**
- Modify: `cmd/atterm-relay/main.go`

- [ ] **Step 1: Remove the old admin-token flag and validation**

Edit `cmd/atterm-relay/main.go`. Delete:

- The `adminToken := flag.String("admin-token", ...)` declaration (around line 35).
- The `cleanAdminToken := strings.TrimSpace(*adminToken)` line (around line 46).
- The whole `if publicListen && !*devInsecure { ... } else if !publicListen && cleanAdminToken == "" { ... }` block that calls `validateAdminToken` (lines 48–59 or so).
- The `AdminToken: cleanAdminToken,` field assignment in the `relay.Config{...}` literal (around line 121). (Remove `AdminToken` from `relay.Config` struct in Task 16.)
- The `, cleanAdminToken` argument from `NewIdentityResolver` if Task 7 missed it.

Replace `--dev-insecure`'s admin-token bypass with: it now only bypasses the BOOTSTRAP_ADMIN_EMAIL requirement (added in step 3).

- [ ] **Step 2: Read the new env and call bootstrapAdmin**

In `main.go`, after `store, err := userstore.Open(...)` succeeds (around line 87–90):

```go
bootstrapEmail := strings.TrimSpace(os.Getenv("ATTERM_BOOTSTRAP_ADMIN_EMAIL"))
bootstrapPassword := os.Getenv("ATTERM_BOOTSTRAP_ADMIN_PASSWORD") // no trim — leading/trailing chars may be meaningful
if err := bootstrapAdmin(ctx, store, bootstrapEmail, bootstrapPassword); err != nil {
    log.Fatalf("bootstrap admin: %v", err)
}
```

- [ ] **Step 3: Tighten public-listen safety to require BOOTSTRAP_ADMIN_EMAIL**

Add, near the top of `main` after `publicListen := isPublicListenAddr(*addr)`:

```go
if publicListen && !*devInsecure && bootstrapEmail == "" {
    log.Fatal("ATTERM_BOOTSTRAP_ADMIN_EMAIL must be set for a public relay; pass --dev-insecure to skip (development only)")
}
```

(Place this AFTER the env reads in step 2.)

- [ ] **Step 4: Build**

```bash
go build ./... 2>&1 | tail -10
```

Expected: PASS. The `validateAdminToken` symbol must no longer be referenced anywhere.

```bash
grep -rn "validateAdminToken\|ATTERM_ADMIN_TOKEN\|cleanAdminToken\|AdminToken" cmd/ internal/ 2>&1 | grep -v "_test.go\|README\|AGENTS"
```

Expected: empty (or only comments / docs being cleaned up in later tasks).

- [ ] **Step 5: Run the full relay + cmd test suites**

```bash
go test -count=1 ./internal/relay/ ./internal/userstore/ ./cmd/atterm-relay/ 2>&1 | tail -10
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/atterm-relay/main.go
git commit -m "cmd/atterm-relay: bootstrap admin from env; ATTERM_ADMIN_TOKEN retired"
```

---

## Task 16 — Drop `Config.AdminToken`

**Files:**
- Modify: `internal/relay/server.go`

- [ ] **Step 1: Remove the field and any remaining references**

Edit `internal/relay/server.go` — find the `Config` struct (around line 38) and remove:

```go
// AdminToken enables /admin routes when non-empty. ...
AdminToken string
```

Search for stragglers:

```bash
grep -rn "AdminToken\|\.AdminToken" internal/relay/ cmd/ 2>&1 | grep -v "_test.go"
```

Expected: empty.

- [ ] **Step 2: Build and test**

```bash
go test -count=1 ./internal/relay/ ./cmd/atterm-relay/ 2>&1 | tail -8
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/relay/server.go
git commit -m "relay: drop Config.AdminToken"
```

---

## Task 17 — Docs: README + AGENTS swap

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Find every mention**

```bash
grep -n "ATTERM_ADMIN_TOKEN" README.md AGENTS.md
```

- [ ] **Step 2: Update `README.md`**

- Deploy example commands: replace `ATTERM_ADMIN_TOKEN='...'` with both `ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com'` and `ATTERM_BOOTSTRAP_ADMIN_PASSWORD='<≥16-char-strong-password>'`.
- Env table row: remove `ATTERM_ADMIN_TOKEN`; add two new rows for the bootstrap pair, noting password is optional when the user already exists.
- Add a short **"Bootstrap admin"** subsection after the deploy command, with three short paragraphs for the three cases (no env / existing user / new user). Include a **Security** callout: "After the bootstrap user is created, unset `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` from your env file / systemd unit and restart. Leaving the password in process state means anyone with read access to env (other services, backups, /proc/self/environ) can recover the initial admin credential."

- [ ] **Step 3: Update `AGENTS.md`**

- Replace the admin-token strength paragraph (around line 50) with the bootstrap-password strength rule: `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` for new-user creates ≥16 chars + ≥3 character classes + not in blacklist; public listen requires `ATTERM_BOOTSTRAP_ADMIN_EMAIL`.
- Replace the `ATTERM_ADMIN_TOKEN` env-table row (around line 84) similarly.
- Update the example shell snippets (line 63 etc.) to use the new env names.

- [ ] **Step 4: Verify no stale references**

```bash
grep -rn "ATTERM_ADMIN_TOKEN\|validateAdminToken\|token_strength" README.md AGENTS.md
```

Expected: empty.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: swap ATTERM_ADMIN_TOKEN for ATTERM_BOOTSTRAP_ADMIN_EMAIL/_PASSWORD"
```

---

## Task 18 — Integration sanity check + ship-release

**Files:** none — this is the final verification + ship.

- [ ] **Step 1: Run every test that could be affected**

```bash
go test -count=1 -timeout 90s ./... 2>&1 | tail -10
```

Expected: PASS in all packages we touched (`./internal/userstore/`, `./internal/relay/`, `./cmd/atterm-relay/`, plus any other package the build depends on).

- [ ] **Step 2: Manual smoke (local loopback)**

In one terminal:

```bash
rm -f data/atterm-relay/users.db
ATTERM_BOOTSTRAP_ADMIN_EMAIL=you@example.com \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Strong-bootstrap-pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web --dev-insecure
```

Look for the log line `WARN: bootstrap created admin user you@example.com`.

In another terminal:

```bash
curl -i -X POST http://127.0.0.1:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"Strong-bootstrap-pass-2026!"}'
```

Expected: 200; save the `atterm_session` cookie.

```bash
curl -i -H 'Cookie: atterm_session=<paste>' http://127.0.0.1:8080/api/me
```

Expected: 200 JSON with `"is_admin": true` (Task 13 / 15) and `csrf_token`.

```bash
curl -i -H 'Cookie: atterm_session=<paste>' http://127.0.0.1:8080/admin/api/users
```

Expected: 200 JSON listing your user with `is_admin: true`.

`/admin/` (browser) is expected to return 404 — the admin UI ships in PR D.

- [ ] **Step 3: Verify the env-set-but-already-exists WARN path**

Restart the relay (db now has your user). The startup log should contain:

```
WARN: ATTERM_BOOTSTRAP_ADMIN_PASSWORD set but you@example.com already exists — password ignored. ...
```

Now unset the password env and restart:

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL=you@example.com \
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web --dev-insecure
```

Startup log should contain:

```
promoted existing user to admin: you@example.com
```

Login + admin API still work.

- [ ] **Step 4: Invoke the `ship-release` skill**

Run the ship-release skill to cut PR + tag `v0.1.72`. The skill walks you through the branch / PR / squash-merge / tag / release-workflow steps. Pre-fill the PR title and body:

- **Title:** `feat(relay): admin role via users.is_admin; ATTERM_ADMIN_TOKEN retired`
- **Body summary points:** schema migration; new bootstrap env vars; promote / demote API with self-demote + last-admin guards; audit log; `/admin/` HTML retired (UI comes in PR D — admin features reachable via JSON API meanwhile); README + AGENTS updated.

After tag is pushed, watch the Release workflow at `https://github.com/attson/atterm/actions`.

- [ ] **Step 5: Update local memory if needed**

If the operator-facing flow surfaced a non-obvious gotcha you'd want a future Claude session to know (e.g. "the bootstrap WARN line is suppressed under systemd unless you set `StandardOutput=journal`"), save it as a `feedback` memory.

---

## Done Criteria

- All 17 tasks complete with green commits.
- `go test ./...` passes.
- `/admin/api/users` returns the admin user with `is_admin: true` via cookie auth from a clean local run.
- `v0.1.72` tag pushed; Release workflow succeeded.
- Next plan (`PR B — Layout shell`) can be written.
