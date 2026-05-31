# Mobile pairing via QR code — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a logged-in desktop user generate a 5-minute, single-use QR code that hands a fresh atterm mobile install its relay URL and a dedicated API token, replacing the manual "type URL + paste token" flow.

**Architecture:** Three independent tiers. (1) Relay (Go) adds a new `pairing_tokens` table + an `api_tokens.source` column, and serves `POST /api/pair/create` (auth-gated) and `POST /api/pair/consume` (public, token-as-bearer). (2) Desktop adds a `CreatePairingToken` Wails binding (mirror of the existing `FetchRelayMe`) and a `PairingPanel.vue` embedded in Settings → Relay. (3) Mobile adds a "Scan QR" entry in `MobileSetup.vue` that uses `@capacitor-mlkit/barcode-scanning`, delegates to a new `PairingConsume.vue` view, and writes the resulting credentials through the existing `platform.relay.save()` helper.

**Tech Stack:** Go + SQLite (`modernc.org/sqlite`) for the relay; Vue 3 + TypeScript + Wails for desktop; Vue 3 + Capacitor 8 for mobile; vitest for frontend tests; standard `go test` for backend tests; `qrcode@^1.5` (desktop) and `@capacitor-mlkit/barcode-scanning@^7` (mobile) as new deps.

**Reference spec:** `docs/superpowers/specs/2026-05-31-pairing-qr-design.md`

---

## File map

### Backend (relay)
- **Create:** `internal/userstore/migrations/0004_pairing_tokens.sql`
- **Create:** `internal/userstore/pairing.go`
- **Create:** `internal/userstore/pairing_test.go`
- **Modify:** `internal/userstore/store.go` (Store interface gains pairing methods)
- **Modify:** `internal/userstore/apitokens.go` (gain `CreateAPITokenWithSource`)
- **Create:** `internal/relay/pair_http.go`
- **Create:** `internal/relay/pair_http_test.go`
- **Modify:** `internal/relay/limits.go` (new buckets for `pair_create`, `pair_consume`)
- **Modify:** `internal/relay/auth_http.go` (mount `/api/pair/*` routes in `RegisterInto`)
- **Modify:** `internal/relay/server.go` (no logic change — confirm routes propagate)

### Desktop (Wails + Vue)
- **Modify:** `desktop/app.go` (add `CreatePairingToken` binding, mirror of `FetchRelayMe`)
- **Create:** `desktop/app_pairing_test.go`
- **Modify:** `desktop/frontend/package.json` (add `qrcode`, `@types/qrcode` devDep)
- **Modify:** `desktop/frontend/src/lib/api.ts` (TS shim for the new binding)
- **Create:** `desktop/frontend/src/components/PairingPanel.vue`
- **Create:** `desktop/frontend/src/components/__tests__/PairingPanel.test.ts`
- **Modify:** `desktop/frontend/src/components/SettingsRelay.vue` (embed `PairingPanel`)

### Mobile (Capacitor + Vue)
- **Modify:** `desktop/frontend/package.json` (add `@capacitor-mlkit/barcode-scanning`)
- **Modify:** `mobile/ios/App/App/Info.plist` (`NSCameraUsageDescription`)
- **Modify:** `desktop/frontend/src/platform/types.ts` (extend `RelayBridge` with `consumePairing`)
- **Modify:** `desktop/frontend/src/platform/capacitor.ts` (implement `consumePairing`)
- **Modify:** `desktop/frontend/src/platform/wails.ts` (stub `consumePairing` — throw "desktop-only" so types stay total)
- **Modify:** `desktop/frontend/src/mobile/MobileSetup.vue` (add Scan QR button)
- **Create:** `desktop/frontend/src/mobile/PairingConsume.vue`
- **Create:** `desktop/frontend/src/mobile/__tests__/PairingConsume.test.ts`

### Shared (i18n)
- **Modify:** `desktop/frontend/src/i18n/messages/en.ts`
- **Modify:** `desktop/frontend/src/i18n/messages/zh-CN.ts`

---

## Phase A — Spec correction

### Task A1: Correct spec §5.2/§5.4 (Wails binding required)

**Files:**
- Modify: `docs/superpowers/specs/2026-05-31-pairing-qr-design.md`

The spec claimed "no Wails/Go changes" but the desktop renderer cannot reach the relay over fetch without sharing the API token, and the established pattern (`FetchRelayMe`) routes through a Wails binding. Update the spec inline.

- [ ] **Step 1: Edit §5.2 of the spec**

Replace the `lib/api.ts` snippet block with:

```ts
export function createPairingToken(): Promise<{
  token: string;
  expires_at: number;
  qr_url: string;
}> {
  // Wails binding (App.CreatePairingToken in desktop/app.go) — the Go side
  // holds the relay URL and API token in cfgStore and signs the request,
  // mirroring the existing FetchRelayMe pattern.
  return bindings().CreatePairingToken();
}
```

- [ ] **Step 2: Edit §5.4 of the spec**

Replace the section body with:

> A new Wails binding `App.CreatePairingToken` is added to `desktop/app.go`,
> following the pattern set by `App.FetchRelayMe`. It reads the relay URL and
> API token from the existing config store, calls `POST /api/pair/create` with
> a Bearer header, and returns the JSON response to the renderer. No other Go
> code is changed.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-05-31-pairing-qr-design.md
git commit -m "spec(pairing): correct §5.2/§5.4 to require a Wails binding"
```

---

## Phase B — Userstore: token model

### Task B1: Add migration `0004_pairing_tokens.sql`

**Files:**
- Create: `internal/userstore/migrations/0004_pairing_tokens.sql`
- Test: `internal/userstore/store_test.go` (existing — re-run to confirm migration applies)

- [ ] **Step 1: Create the migration file**

```sql
-- 0004_pairing_tokens.sql: short-lived QR pairing tokens + api_tokens.source

CREATE TABLE pairing_tokens (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash   TEXT NOT NULL UNIQUE,
    prefix       TEXT NOT NULL,           -- first 12 chars of plaintext (audit only)
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,        -- unix seconds
    expires_at   INTEGER NOT NULL,
    consumed_at  INTEGER                  -- NULL = unused
);
CREATE INDEX idx_pairing_tokens_user ON pairing_tokens(user_id);

ALTER TABLE api_tokens ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
```

- [ ] **Step 2: Run the existing store-open test to confirm migration applies**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/userstore/ -run TestOpenInMemory_RunsMigrations -v`
Expected: PASS — the existing test counts `schema_migrations` rows and only asserts ≥ 1; the new file lands in the embedded FS automatically because `migrations.go` uses `//go:embed migrations/*.sql`.

- [ ] **Step 3: Commit**

```bash
git add internal/userstore/migrations/0004_pairing_tokens.sql
git commit -m "userstore: add pairing_tokens table + api_tokens.source migration"
```

---

### Task B2: Add `CreatePairingToken` to the store (test-first)

**Files:**
- Create: `internal/userstore/pairing.go`
- Create: `internal/userstore/pairing_test.go`
- Modify: `internal/userstore/store.go` (extend `Store` interface)

- [ ] **Step 1: Write the failing test**

Append to a new file `internal/userstore/pairing_test.go`:

```go
package userstore

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreatePairingToken_ReturnsPlaintextOnceAndStoresHash(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	u, err := s.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	secret, row, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}
	plain := secret.Expose()
	if !strings.HasPrefix(plain, "pair_") {
		t.Fatalf("plaintext does not start with pair_: %q", plain)
	}
	if len(plain) < 30 {
		t.Fatalf("plaintext too short: %d", len(plain))
	}
	if strings.Contains(row.Hash, plain[5:]) {
		t.Fatalf("hash leaks plaintext body: %s", row.Hash)
	}
	if row.UserID != u.ID {
		t.Fatalf("UserID: got %q want %q", row.UserID, u.ID)
	}
	if row.ExpiresAt.Before(time.Now().Add(4 * time.Minute)) {
		t.Fatalf("ExpiresAt too soon: %v", row.ExpiresAt)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/userstore/ -run TestCreatePairingToken -v`
Expected: FAIL with "undefined: CreatePairingToken" (or interface compile error if added to Store first).

- [ ] **Step 3: Implement `CreatePairingToken`**

Create `internal/userstore/pairing.go`:

```go
package userstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrPairingInvalid is returned when a pairing token is unknown, expired, or
// already consumed. The three conditions are deliberately indistinguishable
// to prevent oracle attacks on token validity.
var ErrPairingInvalid = errors.New("userstore: pairing token invalid, expired, or already consumed")

// PairingToken is the row shape exposed to callers. It never carries the
// plaintext — only the stored hash and a short prefix for audit logging.
type PairingToken struct {
	ID         int64
	Hash       string
	Prefix     string
	UserID     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

func pairingHash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// CreatePairingToken mints a new single-use pairing token for userID with the
// given TTL. The plaintext is returned exactly once through Secret.Expose()
// and is never persisted server-side.
//
// Plaintext format: "pair_" + base64url-no-padding(32 random bytes) ≈ 47 chars.
func (s *SQLiteStore) CreatePairingToken(ctx context.Context, userID string, ttl time.Duration) (Secret, *PairingToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Secret{}, nil, fmt.Errorf("rand: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	plaintext := "pair_" + body

	hash := pairingHash(plaintext)
	prefix := plaintext[:12] // "pair_" (5) + first 7 body chars

	now := time.Now()
	expires := now.Add(ttl)

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pairing_tokens(token_hash, prefix, user_id, created_at, expires_at, consumed_at)
		 VALUES(?, ?, ?, ?, ?, NULL)`,
		hash, prefix, userID, now.Unix(), expires.Unix(),
	)
	if err != nil {
		return Secret{}, nil, fmt.Errorf("insert pairing_token: %w", err)
	}
	id, _ := res.LastInsertId()

	return NewSecret(plaintext, "pair_"), &PairingToken{
		ID:        id,
		Hash:      hash,
		Prefix:    prefix,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: expires,
	}, nil
}
```

- [ ] **Step 4: Extend the Store interface in `internal/userstore/store.go`**

Add to the `Store` interface (after the existing `// API tokens` block):

```go
	// Pairing tokens (mobile QR code)
	CreatePairingToken(ctx context.Context, userID string, ttl time.Duration) (Secret, *PairingToken, error)
	ConsumePairingToken(ctx context.Context, plaintext string) (apiToken Secret, userID string, err error)
```

(`ConsumePairingToken` will be implemented in Task B3 — its signature is fixed here so the interface compiles after B3 lands. Skip the second line for now to keep the build green, OR include both and add a stub in Task B3 — choose the stub approach: include both lines now and add a temporary no-op below to keep build green.)

To keep the interface and code in sync within one task, add this stub to the bottom of `internal/userstore/pairing.go`:

```go
// ConsumePairingToken stub — real implementation lands in Task B3.
func (s *SQLiteStore) ConsumePairingToken(ctx context.Context, plaintext string) (Secret, string, error) {
	return Secret{}, "", ErrPairingInvalid
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/userstore/ -run TestCreatePairingToken -v`
Expected: PASS.

Run the full userstore suite to confirm nothing else broke:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/userstore/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/userstore/pairing.go internal/userstore/pairing_test.go internal/userstore/store.go
git commit -m "userstore: add CreatePairingToken with hash-only storage"
```

---

### Task B3: Add `ConsumePairingToken` (test-first)

**Files:**
- Modify: `internal/userstore/pairing.go` (replace stub, add real implementation)
- Modify: `internal/userstore/apitokens.go` (add `CreateAPITokenWithSource`)
- Modify: `internal/userstore/pairing_test.go` (add three tests)

- [ ] **Step 1: Write failing tests**

Append to `internal/userstore/pairing_test.go`:

```go
func TestConsumePairingToken_HappyPath_MintsTokenWithSourcePairing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	u, err := s.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	pairSecret, _, err := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}

	apiSecret, userID, err := s.ConsumePairingToken(ctx, pairSecret.Expose())
	if err != nil {
		t.Fatalf("ConsumePairingToken: %v", err)
	}
	if userID != u.ID {
		t.Fatalf("userID: got %q want %q", userID, u.ID)
	}
	plain := apiSecret.Expose()
	if !strings.HasPrefix(plain, "atk_") {
		t.Fatalf("api token does not start with atk_: %q", plain)
	}

	// Verify source column was set to 'pairing'.
	var source string
	if err := s.db.QueryRowContext(ctx,
		`SELECT source FROM api_tokens WHERE token_hash = ?`,
		tokenHash(plain),
	).Scan(&source); err != nil {
		t.Fatalf("select source: %v", err)
	}
	if source != "pairing" {
		t.Fatalf("source: got %q want %q", source, "pairing")
	}

	// The minted token must authenticate as the original user.
	gotTokenID, gotUserID, err := s.LookupAPIToken(ctx, plain)
	if err != nil {
		t.Fatalf("LookupAPIToken: %v", err)
	}
	if gotUserID != u.ID {
		t.Fatalf("LookupAPIToken userID: got %q want %q", gotUserID, u.ID)
	}
	if gotTokenID == "" {
		t.Fatalf("LookupAPIToken: empty tokenID")
	}
}

func TestConsumePairingToken_SecondCallFails(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)

	if _, _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if _, _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); err != ErrPairingInvalid {
		t.Fatalf("second consume: got %v want ErrPairingInvalid", err)
	}
}

func TestConsumePairingToken_Expired(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, -1*time.Second)

	if _, _, err := s.ConsumePairingToken(ctx, pairSecret.Expose()); err != ErrPairingInvalid {
		t.Fatalf("expired consume: got %v want ErrPairingInvalid", err)
	}
}

func TestConsumePairingToken_UnknownTokenString(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, _, err := s.ConsumePairingToken(ctx, "pair_NOTAREALTOKENVALUE"); err != ErrPairingInvalid {
		t.Fatalf("garbage consume: got %v want ErrPairingInvalid", err)
	}
}

func TestConsumePairingToken_ConcurrentExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	pairSecret, _, _ := s.CreatePairingToken(ctx, u.ID, 5*time.Minute)
	code := pairSecret.Expose()

	const n = 50
	var wg sync.WaitGroup
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := s.ConsumePairingToken(ctx, code)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var wins, losses int
	for err := range results {
		if err == nil {
			wins++
		} else if err == ErrPairingInvalid {
			losses++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 || losses != n-1 {
		t.Fatalf("wins=%d losses=%d, want 1 / %d", wins, losses, n-1)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/userstore/ -run TestConsumePairingToken -v`
Expected: FAIL — the stub returns `ErrPairingInvalid` for every input including the happy path.

- [ ] **Step 3: Add `CreateAPITokenWithSource` to `apitokens.go`**

Refactor `CreateAPIToken` to delegate to a new private helper that accepts `source`, and add a new public method:

```go
// CreateAPITokenWithSource mints an API token with an explicit source label.
// 'pairing' is used by the mobile pairing flow; all other callers should use
// CreateAPIToken which defaults to 'manual'.
func (s *SQLiteStore) CreateAPITokenWithSource(ctx context.Context, userID, name, source string) (Secret, *APIToken, error) {
	return s.createAPIToken(ctx, userID, name, source)
}

// createAPIToken is the shared implementation. Source is one of 'manual' or 'pairing'.
func (s *SQLiteStore) createAPIToken(ctx context.Context, userID, name, source string) (Secret, *APIToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Secret{}, nil, fmt.Errorf("rand: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	plaintext := "atk_" + body

	hash := tokenHash(plaintext)
	prefix := plaintext[:12]

	id := defaultIDs.New()
	now := time.Now()
	nowUnix := now.Unix()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens(id, user_id, name, token_hash, token_prefix, created_at, source)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		id, userID, name, hash, prefix, nowUnix, source,
	)
	if err != nil {
		return Secret{}, nil, fmt.Errorf("insert api_token: %w", err)
	}

	tok := &APIToken{
		ID:        id,
		UserID:    userID,
		Name:      name,
		Prefix:    prefix,
		CreatedAt: now,
	}
	return NewSecret(plaintext, "atk_"), tok, nil
}
```

Then rewrite the existing `CreateAPIToken` body so it delegates:

```go
func (s *SQLiteStore) CreateAPIToken(ctx context.Context, userID, name string) (Secret, *APIToken, error) {
	return s.createAPIToken(ctx, userID, name, "manual")
}
```

- [ ] **Step 4: Implement the real `ConsumePairingToken` in `pairing.go`**

Replace the stub from Task B2 with:

```go
// ConsumePairingToken atomically marks the token consumed, mints a new API
// token for the same user (source='pairing'), and returns it. The three
// failure conditions (unknown, expired, consumed) collapse into a single
// ErrPairingInvalid so callers cannot distinguish them (anti-oracle).
//
// Concurrency: the atomic UPDATE with the consumed_at IS NULL guard makes
// "exactly one consumer wins"; the rest get ErrPairingInvalid even if they
// pass the validity check on the read row.
func (s *SQLiteStore) ConsumePairingToken(ctx context.Context, plaintext string) (Secret, string, error) {
	hash := pairingHash(plaintext)
	now := time.Now().Unix()

	res, err := s.db.ExecContext(ctx,
		`UPDATE pairing_tokens
		 SET consumed_at = ?
		 WHERE token_hash = ?
		   AND consumed_at IS NULL
		   AND expires_at > ?`,
		now, hash, now,
	)
	if err != nil {
		return Secret{}, "", fmt.Errorf("consume pairing: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Secret{}, "", fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return Secret{}, "", ErrPairingInvalid
	}

	// We won the race. Look up the owning user_id and mint a fresh API token.
	var userID string
	if err := s.db.QueryRowContext(ctx,
		`SELECT user_id FROM pairing_tokens WHERE token_hash = ?`, hash,
	).Scan(&userID); err != nil {
		return Secret{}, "", fmt.Errorf("lookup user_id: %w", err)
	}

	secret, _, err := s.CreateAPITokenWithSource(ctx, userID, "mobile (paired)", "pairing")
	if err != nil {
		return Secret{}, "", fmt.Errorf("mint api_token: %w", err)
	}
	return secret, userID, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/userstore/`
Expected: PASS — every existing test plus the five new pairing tests.

- [ ] **Step 6: Commit**

```bash
git add internal/userstore/pairing.go internal/userstore/pairing_test.go internal/userstore/apitokens.go
git commit -m "userstore: add ConsumePairingToken (one-shot, source='pairing')"
```

---

## Phase C — Relay HTTP endpoints

### Task C1: Add rate-limit buckets for pairing

**Files:**
- Modify: `internal/relay/limits.go`

- [ ] **Step 1: Add two limiters to the LimitRegistry struct**

Edit `internal/relay/limits.go`, replace the existing `LimitRegistry` struct and the two `New*` constructors:

```go
type LimitRegistry struct {
	loginFail   *fixedWindowLimiter // 10 / 5min keyed by "ip\x00sha256hex(email)"
	signup      *fixedWindowLimiter // 5 / hour keyed by IP
	inviteFail  *fixedWindowLimiter // 10 / hour keyed by IP
	pairCreate  *fixedWindowLimiter // 10 / minute keyed by userID
	pairConsume *fixedWindowLimiter // 10 / minute keyed by IP
}

func NewLimitRegistry() *LimitRegistry {
	return &LimitRegistry{
		loginFail:   newFixedWindowLimiter(10, 5*time.Minute),
		signup:      newFixedWindowLimiter(5, time.Hour),
		inviteFail:  newFixedWindowLimiter(10, time.Hour),
		pairCreate:  newFixedWindowLimiter(10, time.Minute),
		pairConsume: newFixedWindowLimiter(10, time.Minute),
	}
}

func newLimitRegistryForTest(loginFailLimit, signupLimit, inviteFailLimit int) *LimitRegistry {
	return &LimitRegistry{
		loginFail:   newFixedWindowLimiter(loginFailLimit, 5*time.Minute),
		signup:      newFixedWindowLimiter(signupLimit, time.Hour),
		inviteFail:  newFixedWindowLimiter(inviteFailLimit, time.Hour),
		pairCreate:  newFixedWindowLimiter(1000, time.Minute), // effectively unlimited in tests
		pairConsume: newFixedWindowLimiter(1000, time.Minute),
	}
}
```

Then add the two methods at the bottom of the file (above `sha256Hex`):

```go
// AllowPairCreate returns true if userID has not exceeded the pairing-token
// mint rate limit (10 / minute).
func (r *LimitRegistry) AllowPairCreate(userID string) bool {
	return r.pairCreate.allow(userID)
}

// AllowPairConsume returns true if ip has not exceeded the consume rate limit
// (10 / minute). Defense in depth — the 256-bit token entropy alone already
// makes brute force impractical.
func (r *LimitRegistry) AllowPairConsume(ip string) bool {
	return r.pairConsume.allow(ip)
}
```

- [ ] **Step 2: Run existing limits tests to confirm nothing broke**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run 'Limit|Allow' -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/relay/limits.go
git commit -m "relay/limits: add pair_create and pair_consume buckets (10/min each)"
```

---

### Task C2: Add `POST /api/pair/create` handler (test-first)

**Files:**
- Create: `internal/relay/pair_http.go`
- Create: `internal/relay/pair_http_test.go`
- Modify: `internal/relay/auth_http.go` (register routes via `RegisterInto`)

- [ ] **Step 1: Write the failing test**

Create `internal/relay/pair_http_test.go`:

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

// newTestPairServer reuses newTestAuthServer (from auth_http_test.go); the
// pairing routes are registered by the same AuthServer.RegisterInto call.
func newTestPairServer(t *testing.T) (http.Handler, *AuthServer) {
	t.Helper()
	srv, _ := newTestAuthServer(t)
	srv.Limits = NewLimitRegistry()
	return srv.Routes(), srv
}

// authedRequest builds a request with a Bearer atk_ token for the given user.
func authedRequest(t *testing.T, srv *AuthServer, method, path, body string) *http.Request {
	t.Helper()
	ctx := context.Background()
	u, err := srv.Store.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	secret, _, err := srv.Store.CreateAPIToken(ctx, u.ID, "test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+secret.Expose())
	r.Header.Set("Content-Type", "application/json")
	r.Host = "relay.example.com"
	return r
}

func TestPairCreate_Unauthorized(t *testing.T) {
	handler, _ := newTestPairServer(t)
	r := httptest.NewRequest(http.MethodPost, "/api/pair/create", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPairCreate_HappyPath_ReturnsTokenAndQRURL(t *testing.T) {
	handler, srv := newTestPairServer(t)
	r := authedRequest(t, srv, http.MethodPost, "/api/pair/create", "{}")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
		QRURL     string `json:"qr_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.HasPrefix(resp.Token, "pair_") {
		t.Fatalf("token: got %q", resp.Token)
	}
	if resp.ExpiresAt == 0 {
		t.Fatalf("ExpiresAt zero")
	}
	if !strings.HasPrefix(resp.QRURL, "http://relay.example.com/pair?t=pair_") {
		// httptest.NewRequest has TLS == nil so derivation falls back to http
		t.Fatalf("QRURL: got %q", resp.QRURL)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestPairCreate -v`
Expected: FAIL — both tests 404, route not registered.

- [ ] **Step 3: Implement the handler**

Create `internal/relay/pair_http.go`:

```go
package relay

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// pairingTTL is the lifetime of a freshly minted pairing token.
const pairingTTL = 5 * time.Minute

// publicBaseURL derives the relay's externally reachable origin from the
// request — Host header, plus X-Forwarded-Proto for HTTPS detection behind
// a reverse proxy. Single source of truth shared by qr_url (create) and
// relay_url (consume).
func publicBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// handlePairCreate mints a new pairing token for the authenticated user.
// POST /api/pair/create — requireUser.
func (a *AuthServer) handlePairCreate(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	if a.Limits != nil && !a.Limits.AllowPairCreate(p.UserID) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	secret, row, err := a.Store.CreatePairingToken(r.Context(), p.UserID, pairingTTL)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	plaintext := secret.Expose()
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"token":      plaintext,
		"expires_at": row.ExpiresAt.Unix(),
		"qr_url":     publicBaseURL(r) + "/pair?t=" + plaintext,
	})
}

// handlePairConsume — body lands in Task C3.
func (a *AuthServer) handlePairConsume(w http.ResponseWriter, r *http.Request) {
	// Not implemented yet. Reject so the route is reachable but useless.
	http.Error(w, "not implemented", http.StatusNotImplemented)
	_ = json.NewEncoder // keep import; remove in C3
}
```

- [ ] **Step 4: Register the two routes in `auth_http.go`**

Find the `RegisterInto` method (around `auth_http.go:62`) and append two lines before the closing brace:

```go
	mux.Handle("POST /api/pair/create", http.HandlerFunc(a.handlePairCreate))
	mux.Handle("POST /api/pair/consume", http.HandlerFunc(a.handlePairConsume))
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestPairCreate -v`
Expected: PASS — both tests.

- [ ] **Step 6: Commit**

```bash
git add internal/relay/pair_http.go internal/relay/pair_http_test.go internal/relay/auth_http.go
git commit -m "relay: POST /api/pair/create returns one-time token + qr_url"
```

---

### Task C3: Add `POST /api/pair/consume` handler (test-first)

**Files:**
- Modify: `internal/relay/pair_http.go` (replace stub)
- Modify: `internal/relay/pair_http_test.go` (add three tests)

- [ ] **Step 1: Write failing tests**

Append to `internal/relay/pair_http_test.go`:

```go
func TestPairConsume_HappyPath_ReturnsCredsAndAuthenticates(t *testing.T) {
	handler, srv := newTestPairServer(t)

	// Mint via the authed create endpoint.
	rCreate := authedRequest(t, srv, http.MethodPost, "/api/pair/create", "{}")
	wCreate := httptest.NewRecorder()
	handler.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create: %d", wCreate.Code)
	}
	var created struct {
		Token string `json:"token"`
	}
	json.Unmarshal(wCreate.Body.Bytes(), &created)

	// Consume — no auth header.
	body, _ := json.Marshal(map[string]string{"token": created.Token})
	rConsume := httptest.NewRequest(http.MethodPost, "/api/pair/consume", strings.NewReader(string(body)))
	rConsume.Header.Set("Content-Type", "application/json")
	rConsume.Host = "relay.example.com"
	wConsume := httptest.NewRecorder()
	handler.ServeHTTP(wConsume, rConsume)

	if wConsume.Code != http.StatusOK {
		t.Fatalf("consume: expected 200, got %d: %s", wConsume.Code, wConsume.Body.String())
	}
	var resp struct {
		RelayURL string `json:"relay_url"`
		APIToken string `json:"api_token"`
		User     struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(wConsume.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.HasPrefix(resp.APIToken, "atk_") {
		t.Fatalf("api_token: got %q", resp.APIToken)
	}
	if resp.User.Email != "alice@example.com" {
		t.Fatalf("user.email: got %q", resp.User.Email)
	}
	if resp.RelayURL == "" {
		t.Fatalf("relay_url empty")
	}

	// The minted api_token must authenticate against /api/me.
	rMe := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rMe.Header.Set("Authorization", "Bearer "+resp.APIToken)
	wMe := httptest.NewRecorder()
	handler.ServeHTTP(wMe, rMe)
	if wMe.Code != http.StatusOK {
		t.Fatalf("/api/me with new token: %d %s", wMe.Code, wMe.Body.String())
	}
}

func TestPairConsume_SecondTime_404(t *testing.T) {
	handler, srv := newTestPairServer(t)
	rCreate := authedRequest(t, srv, http.MethodPost, "/api/pair/create", "{}")
	wCreate := httptest.NewRecorder()
	handler.ServeHTTP(wCreate, rCreate)
	var created struct{ Token string `json:"token"` }
	json.Unmarshal(wCreate.Body.Bytes(), &created)

	doConsume := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"token": created.Token})
		r := httptest.NewRequest(http.MethodPost, "/api/pair/consume", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := doConsume(); w.Code != http.StatusOK {
		t.Fatalf("first consume: %d", w.Code)
	}
	w := doConsume()
	if w.Code != http.StatusNotFound {
		t.Fatalf("second consume: expected 404, got %d", w.Code)
	}
	var errBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &errBody)
	if errBody["code"] != "pair_invalid" {
		t.Fatalf("code: got %q want pair_invalid", errBody["code"])
	}
}

func TestPairConsume_UnknownToken_404(t *testing.T) {
	handler, _ := newTestPairServer(t)
	body, _ := json.Marshal(map[string]string{"token": "pair_DEFINITELYNOTREAL"})
	r := httptest.NewRequest(http.MethodPost, "/api/pair/consume", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestPairConsume -v`
Expected: FAIL — the stub returns 501.

- [ ] **Step 3: Implement the handler**

Replace the stub `handlePairConsume` in `internal/relay/pair_http.go` and trim the unused import comment:

```go
package relay

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

const pairingTTL = 5 * time.Minute

func publicBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (a *AuthServer) handlePairCreate(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if a.Limits != nil && !a.Limits.AllowPairCreate(p.UserID) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	secret, row, err := a.Store.CreatePairingToken(r.Context(), p.UserID, pairingTTL)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	plaintext := secret.Expose()
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"token":      plaintext,
		"expires_at": row.ExpiresAt.Unix(),
		"qr_url":     publicBaseURL(r) + "/pair?t=" + plaintext,
	})
}

// handlePairConsume exchanges a pairing token for a fresh API token + relay URL
// + user info. No auth header required: the pairing token IS the credential
// (same trust model as OAuth Device Code Flow).
func (a *AuthServer) handlePairConsume(w http.ResponseWriter, r *http.Request) {
	if a.Limits != nil && !a.Limits.AllowPairConsume(ipPrefix(r)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"code": "pair_invalid"})
		return
	}

	apiSecret, userID, err := a.Store.ConsumePairingToken(r.Context(), body.Token)
	if err != nil {
		if errors.Is(err, userstore.ErrPairingInvalid) {
			writeJSONStatus(w, http.StatusNotFound, map[string]string{"code": "pair_invalid"})
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user, err := a.Store.GetUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSONStatus(w, http.StatusOK, map[string]any{
		"relay_url": publicBaseURL(r),
		"api_token": apiSecret.Expose(),
		"user": map[string]string{
			"id":    user.ID,
			"email": user.Email,
		},
	})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/ -run TestPair -v`
Expected: PASS — all 5 tests (2 from C2, 3 from C3).

Run the full relay suite:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/relay/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/pair_http.go internal/relay/pair_http_test.go
git commit -m "relay: POST /api/pair/consume mints atk_ token, returns user info"
```

---

## Phase D — Desktop side

### Task D1: Add `CreatePairingToken` Wails binding (test-first)

**Files:**
- Modify: `desktop/app.go`
- Create: `desktop/app_pairing_test.go`

- [ ] **Step 1: Write the failing test**

Create `desktop/app_pairing_test.go`:

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreatePairingToken_PostsToRelayWithBearerAndReturnsParsed(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pair/create" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "pair_TESTVALUE",
			"expires_at": int64(1748689200),
			"qr_url":     "http://relay.test/pair?t=pair_TESTVALUE",
		})
	}))
	t.Cleanup(srv.Close)

	app := newTestAppWithRelay(t, srv.URL, "atk_localtest")

	got, err := app.CreatePairingToken()
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}
	if got.Token != "pair_TESTVALUE" {
		t.Errorf("Token: got %q", got.Token)
	}
	if got.ExpiresAt != 1748689200 {
		t.Errorf("ExpiresAt: got %d", got.ExpiresAt)
	}
	if !strings.HasPrefix(got.QRURL, "http://relay.test/pair?t=") {
		t.Errorf("QRURL: got %q", got.QRURL)
	}
	if gotAuth != "Bearer atk_localtest" {
		t.Errorf("Authorization: got %q", gotAuth)
	}
}

func TestCreatePairingToken_NoRelayConfigured_Errors(t *testing.T) {
	app := newTestAppWithRelay(t, "", "")
	if _, err := app.CreatePairingToken(); err == nil {
		t.Fatal("expected error when relay not configured")
	}
}
```

The helper `newTestAppWithRelay` does not exist yet. Search `desktop/app_test.go` for an existing helper to confirm the test pattern, then add a minimal helper at the bottom of `app_pairing_test.go`:

```go
// newTestAppWithRelay builds an App with cfgStore pointing at the given relay.
// The cfgStore type matches the production wiring in desktop/app.go.
func newTestAppWithRelay(t *testing.T, relayURL, relayToken string) *App {
	t.Helper()
	store := &appConfigStore{cfg: appConfig{RelayURL: relayURL, RelayToken: relayToken}}
	return &App{cfgStore: store}
}
```

(If `appConfigStore` already exposes a different constructor — verify by reading `desktop/config.go` — adapt; otherwise add a `Get()` method or struct-literal field assignment that mirrors how production code populates the store. The existing `app_test.go` and `config_test.go` files have working examples.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestCreatePairingToken -v`
Expected: FAIL — `app.CreatePairingToken` undefined.

- [ ] **Step 3: Implement the binding**

Append to `desktop/app.go` (just below `FetchRelayMe`):

```go
// PairingTokenResponse is what the renderer receives when generating a QR code.
// Mirrors the relay's /api/pair/create response body.
type PairingTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	QRURL     string `json:"qr_url"`
}

// CreatePairingToken asks the configured relay to mint a 5-minute single-use
// pairing token for the desktop's current user and returns the response,
// including the qr_url to encode into a QR code.
func (a *App) CreatePairingToken() (PairingTokenResponse, error) {
	if a.cfgStore == nil {
		return PairingTokenResponse{}, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelayToken == "" {
		return PairingTokenResponse{}, fmt.Errorf("no relay configured")
	}
	baseHTTP := strings.Replace(strings.Replace(cfg.RelayURL, "wss://", "https://", 1), "ws://", "http://", 1)
	req, err := http.NewRequest("POST", baseHTTP+"/api/pair/create", strings.NewReader("{}"))
	if err != nil {
		return PairingTokenResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.RelayToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return PairingTokenResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PairingTokenResponse{}, fmt.Errorf("relay /api/pair/create returned status %d", resp.StatusCode)
	}
	var out PairingTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return PairingTokenResponse{}, err
	}
	return out, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestCreatePairingToken -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/app.go desktop/app_pairing_test.go
git commit -m "desktop: add CreatePairingToken Wails binding"
```

---

### Task D2: Add the TypeScript wrapper in `lib/api.ts`

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`

- [ ] **Step 1: Add the type and binding declaration**

In `desktop/frontend/src/lib/api.ts`, immediately after the `RelayMe` interface (around line 37):

```ts
export interface PairingToken {
  token: string;
  expires_at: number;
  qr_url: string;
}
```

In the `AppBindings` interface, add a line beside `FetchRelayMe`:

```ts
  CreatePairingToken(): Promise<PairingToken>;
```

At the bottom of the file (after `fetchRelayMe`):

```ts
// createPairingToken asks the relay to mint a 5-minute single-use pairing
// token via the desktop's existing API-token-authenticated channel. The
// returned qr_url is the value to encode into the QR image.
export function createPairingToken(): Promise<PairingToken> {
  return bindings().CreatePairingToken();
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/lib/api.ts
git commit -m "desktop/frontend: typed createPairingToken wrapper"
```

---

### Task D3: Add the `qrcode` dependency

**Files:**
- Modify: `desktop/frontend/package.json`
- Modify: `desktop/frontend/package-lock.json` (auto-generated)

- [ ] **Step 1: Install the dependency**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm install --save qrcode@^1.5 && npm install --save-dev @types/qrcode`
Expected: lockfile updates; no errors.

- [ ] **Step 2: Smoke check the type-check still passes**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/package.json desktop/frontend/package-lock.json
git commit -m "desktop/frontend: add qrcode + @types/qrcode for pairing UI"
```

---

### Task D4: Add `PairingPanel.vue` (test-first)

**Files:**
- Create: `desktop/frontend/src/components/PairingPanel.vue`
- Create: `desktop/frontend/src/components/__tests__/PairingPanel.test.ts`
- Modify: `desktop/frontend/src/components/SettingsRelay.vue`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/__tests__/PairingPanel.test.ts`:

```ts
import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import PairingPanel from '../PairingPanel.vue'
import * as api from '../../lib/api'

vi.mock('qrcode', () => ({
  default: {
    toDataURL: vi.fn(async (url: string) =>
      'data:image/png;base64,FAKE_' + Buffer.from(url).toString('base64').slice(0, 16))
  },
}))

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

describe('PairingPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.spyOn(api, 'createPairingToken').mockResolvedValue({
      token: 'pair_TESTVAL',
      expires_at: Math.floor(Date.now() / 1000) + 300,
      qr_url: 'https://relay.test/pair?t=pair_TESTVAL',
    })
  })

  it('shows idle state with a Generate button by default', () => {
    const wrapper = mount(PairingPanel)
    expect(wrapper.find('[data-testid="pair-generate"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="pair-qr"]').exists()).toBe(false)
  })

  it('renders the QR image and countdown after Generate clicked', async () => {
    const wrapper = mount(PairingPanel)
    await wrapper.find('[data-testid="pair-generate"]').trigger('click')
    await flushPromises()
    const img = wrapper.find('[data-testid="pair-qr"]')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toMatch(/^data:image\/png/)
    expect(wrapper.text()).toContain('5:00')
  })

  it('shows expired state when countdown reaches zero', async () => {
    const wrapper = mount(PairingPanel)
    await wrapper.find('[data-testid="pair-generate"]').trigger('click')
    await flushPromises()
    vi.advanceTimersByTime(301 * 1000)
    await flushPromises()
    expect(wrapper.find('[data-testid="pair-expired"]').exists()).toBe(true)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/__tests__/PairingPanel.test.ts`
Expected: FAIL — `PairingPanel.vue` does not exist.

- [ ] **Step 3: Implement `PairingPanel.vue`**

Create `desktop/frontend/src/components/PairingPanel.vue`:

```vue
<script lang="ts" setup>
import { computed, onBeforeUnmount, ref } from 'vue'
import QRCode from 'qrcode'
import { createPairingToken, type PairingToken } from '../lib/api'
import { useI18n } from '../i18n/useI18n'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const current = ref<PairingToken | null>(null)
const qrDataUrl = ref('')
const now = ref(Math.floor(Date.now() / 1000))
let tick: ReturnType<typeof setInterval> | null = null

const remaining = computed(() => current.value ? Math.max(0, current.value.expires_at - now.value) : 0)
const countdownText = computed(() => {
  const s = remaining.value
  const m = Math.floor(s / 60)
  const r = s % 60
  return `${m}:${r.toString().padStart(2, '0')}`
})
const expired = computed(() => current.value !== null && remaining.value === 0)

async function generate() {
  loading.value = true
  error.value = ''
  try {
    const tok = await createPairingToken()
    qrDataUrl.value = await QRCode.toDataURL(tok.qr_url, { width: 240, margin: 1 })
    current.value = tok
    now.value = Math.floor(Date.now() / 1000)
    if (tick) clearInterval(tick)
    tick = setInterval(() => { now.value = Math.floor(Date.now() / 1000) }, 1000)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

onBeforeUnmount(() => { if (tick) clearInterval(tick) })
</script>

<template>
  <section class="pairing-panel" data-testid="pairing-panel">
    <div class="title">{{ t('settings.relay.pairing.title') }}</div>
    <p class="hint">{{ t('settings.relay.pairing.hint') }}</p>

    <button
      v-if="!current"
      type="button"
      data-testid="pair-generate"
      :disabled="loading"
      @click="generate"
    >
      {{ loading ? t('settings.relay.pairing.generating') : t('settings.relay.pairing.generate') }}
    </button>

    <div v-else class="qr-wrap">
      <img :src="qrDataUrl" alt="" class="qr" :class="{ dimmed: expired }" data-testid="pair-qr" />
      <div v-if="!expired" class="countdown">
        {{ t('settings.relay.pairing.expiresIn', { time: countdownText }) }}
      </div>
      <div v-else class="countdown expired" data-testid="pair-expired">
        {{ t('settings.relay.pairing.expired') }}
      </div>
      <code class="prefix">{{ current.token.slice(0, 12) }}…</code>
      <button type="button" data-testid="pair-regenerate" @click="generate">
        {{ t('settings.relay.pairing.regenerate') }}
      </button>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
  </section>
</template>

<style scoped>
.pairing-panel { display: flex; flex-direction: column; gap: 10px; padding: 12px; border: 1px solid var(--border); border-radius: 10px; }
.title { font-size: 13px; font-weight: 700; color: var(--fg); }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.45; }
.qr-wrap { display: flex; flex-direction: column; align-items: center; gap: 8px; }
.qr { width: 240px; height: 240px; image-rendering: pixelated; background: #fff; padding: 6px; border-radius: 6px; }
.qr.dimmed { opacity: 0.35; }
.countdown { font-size: 12px; color: var(--fg-dim); }
.countdown.expired { color: var(--bad); }
.prefix { font-family: ui-monospace, Menlo, monospace; font-size: 11px; color: var(--fg-dim); }
.error { color: var(--bad); font-size: 12px; margin: 0; }
button { height: 30px; border: 1px solid var(--accent); border-radius: 7px; background: var(--accent); color: var(--bg); padding: 0 12px; font-size: 12px; font-weight: 700; cursor: pointer; }
button:disabled { opacity: 0.55; }
</style>
```

- [ ] **Step 4: Embed `PairingPanel` in `SettingsRelay.vue`**

In `desktop/frontend/src/components/SettingsRelay.vue`, add an import near line 6:

```ts
import PairingPanel from './PairingPanel.vue'
```

Inside the `<template>` block, immediately before the closing `</template>` tag of the outer `<div class="tab-pane">` (right after the `<p v-if="error" class="error">{{ error }}</p>` line near line 321), add:

```vue
      <PairingPanel />
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/__tests__/PairingPanel.test.ts`
Expected: PASS — all three tests.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/PairingPanel.vue desktop/frontend/src/components/__tests__/PairingPanel.test.ts desktop/frontend/src/components/SettingsRelay.vue
git commit -m "desktop/frontend: PairingPanel embedded in Settings → Relay"
```

---

## Phase E — Mobile side

### Task E1: Install barcode scanner + iOS camera permission

**Files:**
- Modify: `desktop/frontend/package.json`
- Modify: `desktop/frontend/package-lock.json`
- Modify: `mobile/ios/App/App/Info.plist`

- [ ] **Step 1: Install the Capacitor plugin**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm install --save @capacitor-mlkit/barcode-scanning@^7`
Expected: lockfile updates; no errors.

- [ ] **Step 2: Add `NSCameraUsageDescription` to the iOS Info.plist**

Edit `mobile/ios/App/App/Info.plist`. Find the closing `</dict>` near the bottom and insert before it:

```xml
	<key>NSCameraUsageDescription</key>
	<string>atterm uses the camera to scan a pairing QR code from your desktop.</string>
```

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/package.json desktop/frontend/package-lock.json mobile/ios/App/App/Info.plist
git commit -m "mobile: add @capacitor-mlkit/barcode-scanning + camera permission"
```

---

### Task E2: Extend `RelayBridge` with `consumePairing`

**Files:**
- Modify: `desktop/frontend/src/platform/types.ts`
- Modify: `desktop/frontend/src/platform/capacitor.ts`
- Modify: `desktop/frontend/src/platform/wails.ts`

- [ ] **Step 1: Add the bridge contract**

In `desktop/frontend/src/platform/types.ts`, replace the existing `RelayBridge` interface (around line 46) with:

```ts
export interface PairingConsumeResult {
  relay_url: string
  api_token: string
  user: { id: string; email: string }
}

export interface RelayBridge {
  load(): Promise<_RelayConfig | null>
  save(cfg: _RelayConfig): Promise<void>
  clear(): Promise<void>
  fetchMe(): Promise<_RelayMe>
  setUplinkPaused?(paused: boolean): Promise<void>
  consumePairing?(relayBase: string, token: string): Promise<PairingConsumeResult>
}
```

- [ ] **Step 2: Implement `consumePairing` in the Capacitor platform**

In `desktop/frontend/src/platform/capacitor.ts`, inside the `relay:` object literal (around line 47), add a new method after `fetchMe`:

```ts
      consumePairing: async (relayBase, token) => {
        const base = relayBase.replace(/\/$/, '')
        const res = await fetch(base + '/api/pair/consume', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ token }),
          credentials: 'omit',
        })
        if (res.status === 404) {
          const body = await res.json().catch(() => ({}))
          throw new Error(body.code || 'pair_invalid')
        }
        if (!res.ok) throw new Error(`pair_consume_http_${res.status}`)
        return (await res.json()) as { relay_url: string; api_token: string; user: { id: string; email: string } }
      },
```

- [ ] **Step 3: Leave Wails platform alone**

The Wails platform does not implement `consumePairing` (desktop-only side never consumes). The optional `?` in the interface keeps the type total. Skip touching `wails.ts`.

- [ ] **Step 4: Verify type-check passes**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/platform/types.ts desktop/frontend/src/platform/capacitor.ts
git commit -m "platform/capacitor: add relay.consumePairing"
```

---

### Task E3: Add `PairingConsume.vue` (test-first)

**Files:**
- Create: `desktop/frontend/src/mobile/PairingConsume.vue`
- Create: `desktop/frontend/src/mobile/__tests__/PairingConsume.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/mobile/__tests__/PairingConsume.test.ts`:

```ts
import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import PairingConsume from '../PairingConsume.vue'

const consumePairing = vi.fn()
const save = vi.fn()

vi.mock('../../platform', () => ({
  usePlatform: () => ({
    relay: { consumePairing, save, load: vi.fn() },
  }),
}))

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

describe('PairingConsume', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('rejects URL with missing t param', async () => {
    const wrapper = mount(PairingConsume, {
      props: { scannedUrl: 'https://relay.example.com/pair' },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="pair-error"]').exists()).toBe(true)
    expect(consumePairing).not.toHaveBeenCalled()
  })

  it('rejects http URL when allowInsecure is false', async () => {
    const wrapper = mount(PairingConsume, {
      props: { scannedUrl: 'http://relay.example.com/pair?t=pair_X', allowInsecure: false },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="pair-error"]').exists()).toBe(true)
  })

  it('happy path: calls consumePairing, saves config, emits connected', async () => {
    consumePairing.mockResolvedValue({
      relay_url: 'https://relay.example.com',
      api_token: 'atk_NEW',
      user: { id: 'u1', email: 'alice@example.com' },
    })
    const wrapper = mount(PairingConsume, {
      props: { scannedUrl: 'https://relay.example.com/pair?t=pair_VALID' },
    })
    await flushPromises()
    expect(consumePairing).toHaveBeenCalledWith('https://relay.example.com', 'pair_VALID')
    expect(save).toHaveBeenCalledWith(expect.objectContaining({
      url: 'https://relay.example.com',
      token: 'atk_NEW',
    }))
    expect(wrapper.emitted('connected')).toBeTruthy()
  })

  it('renders pair_invalid error when consume rejects', async () => {
    consumePairing.mockRejectedValue(new Error('pair_invalid'))
    const wrapper = mount(PairingConsume, {
      props: { scannedUrl: 'https://relay.example.com/pair?t=pair_BAD' },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="pair-error"]').exists()).toBe(true)
    expect(save).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/PairingConsume.test.ts`
Expected: FAIL — `PairingConsume.vue` does not exist.

- [ ] **Step 3: Implement `PairingConsume.vue`**

Create `desktop/frontend/src/mobile/PairingConsume.vue`:

```vue
<script lang="ts" setup>
import { onMounted, ref } from 'vue'
import { usePlatform } from '../platform'
import { useI18n } from '../i18n/useI18n'

const props = defineProps<{ scannedUrl: string; allowInsecure?: boolean }>()
const emit = defineEmits<{ (e: 'connected'): void; (e: 'cancel'): void }>()

const platform = usePlatform()
const { t } = useI18n()

const status = ref<'pending' | 'error'>('pending')
const errorCode = ref('')

function parseScanned(raw: string, allowInsecure: boolean): { origin: string; token: string } | string {
  let u: URL
  try {
    u = new URL(raw)
  } catch {
    return 'pair_invalid_url'
  }
  if (u.protocol !== 'https:' && !(u.protocol === 'http:' && allowInsecure)) {
    return 'pair_invalid_scheme'
  }
  const token = u.searchParams.get('t')
  if (!token || !u.host) return 'pair_invalid_url'
  return { origin: u.origin, token }
}

async function run() {
  const parsed = parseScanned(props.scannedUrl, !!props.allowInsecure)
  if (typeof parsed === 'string') {
    errorCode.value = parsed
    status.value = 'error'
    return
  }
  try {
    if (!platform.relay.consumePairing) throw new Error('platform_unsupported')
    const result = await platform.relay.consumePairing(parsed.origin, parsed.token)
    await platform.relay.save({
      url: result.relay_url,
      token: result.api_token,
      allow_insecure_relay: !!props.allowInsecure,
      remote_permission: 'full',
      connected: false,
    })
    emit('connected')
  } catch (e) {
    errorCode.value = e instanceof Error ? e.message : String(e)
    status.value = 'error'
  }
}

onMounted(run)
</script>

<template>
  <div class="pair-consume">
    <div v-if="status === 'pending'" class="pending">
      {{ t('mobile.pairing.connecting') }}
    </div>
    <div v-else class="error" data-testid="pair-error">
      <p>{{ t('mobile.pairing.failed') }}</p>
      <p class="code">{{ errorCode }}</p>
      <button type="button" @click="emit('cancel')">{{ t('mobile.pairing.back') }}</button>
    </div>
  </div>
</template>

<style scoped>
.pair-consume { min-height: 100vh; display: flex; align-items: center; justify-content: center; flex-direction: column; gap: 12px; padding: 1.5rem; background: var(--bg, #05070d); color: var(--fg, #e6e7ea); }
.pending { font-size: 0.95rem; color: #8d93a3; }
.error p { margin: 0; }
.error .code { font-family: ui-monospace, Menlo, monospace; color: #f87171; font-size: 0.8rem; }
.error button { margin-top: 12px; height: 42px; padding: 0 18px; border: none; border-radius: 9px; background: #3b82f6; color: #fff; font-weight: 600; }
</style>
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/PairingConsume.test.ts`
Expected: PASS — all four tests.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/mobile/PairingConsume.vue desktop/frontend/src/mobile/__tests__/PairingConsume.test.ts
git commit -m "mobile: PairingConsume view (parse, consume, save, route)"
```

---

### Task E4: Wire the "Scan QR" button into `MobileSetup.vue`

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileSetup.vue`

- [ ] **Step 1: Add the scan-and-route logic**

In `desktop/frontend/src/mobile/MobileSetup.vue`, edit the `<script setup>` block:

Replace the top section with:

```ts
import { ref, computed, onMounted } from 'vue'
import { type LocalePreference } from '../i18n'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { validateRelayBase } from './relay'
import PairingConsume from './PairingConsume.vue'
import { BarcodeScanner } from '@capacitor-mlkit/barcode-scanning'
```

Add `scannedUrl` state near the other refs:

```ts
const scannedUrl = ref<string | null>(null)
```

Add a scan handler before `onConnect`:

```ts
async function onScanQR(): Promise<void> {
  error.value = null
  try {
    const { camera } = await BarcodeScanner.requestPermissions()
    if (camera !== 'granted' && camera !== 'limited') {
      error.value = t('mobile.pairing.cameraDenied')
      return
    }
    const { barcodes } = await BarcodeScanner.scan({ formats: ['QR_CODE' as any] })
    const first = barcodes[0]
    if (!first?.rawValue) {
      error.value = t('mobile.pairing.noQrDetected')
      return
    }
    scannedUrl.value = first.rawValue
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function onConsumeCancelled(): void {
  scannedUrl.value = null
}
```

- [ ] **Step 2: Update the template**

Replace the existing `<template>` content with:

```vue
<template>
  <div class="setup">
    <PairingConsume
      v-if="scannedUrl"
      :scanned-url="scannedUrl"
      :allow-insecure="allowInsecure"
      @connected="emit('connected')"
      @cancel="onConsumeCancelled"
    />
    <template v-else>
      <h1>AT Term</h1>
      <p class="sub">{{ t('mobile.setupSubtitle') }}</p>
      <div v-if="banner" class="banner">{{ banner }}</div>

      <button data-testid="scan-qr" class="btn btn-primary" :disabled="submitting" @click="onScanQR">
        {{ t('mobile.pairing.scan') }}
      </button>
      <p class="or">{{ t('mobile.pairing.orManual') }}</p>

      <label class="field">
        <span>{{ t('settings.general.languageLabel') }}</span>
        <select data-testid="mobile-language" :value="localePreference" :disabled="submitting" @change="onLanguageChange">
          <option v-for="option in localizedLanguageOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
      </label>
      <label class="field">
        <span>{{ t('mobile.relayUrl') }}</span>
        <input data-testid="relay-url" v-model="url" :disabled="submitting" placeholder="https://relay.example.com" autocomplete="off" autocapitalize="off" spellcheck="false" />
      </label>
      <label class="field">
        <span>{{ t('mobile.apiToken') }}</span>
        <input data-testid="relay-token" v-model="token" :disabled="submitting" type="password" placeholder="atk_…" autocomplete="off" />
      </label>
      <label class="row">
        <span>{{ t('mobile.allowInsecure') }}</span>
        <input data-testid="allow-insecure" v-model="allowInsecure" :disabled="submitting" type="checkbox" />
      </label>
      <p v-if="error" class="error">{{ error }}</p>
      <button data-testid="connect" class="btn" :disabled="submitting" @click="onConnect">{{ t('common.connect') }}</button>
    </template>
  </div>
</template>
```

- [ ] **Step 3: Append the new styles**

In the `<style scoped>` block, append:

```css
.btn-primary { background: #2563eb; margin-bottom: 0.75rem; }
.or { text-align: center; color: #8d93a3; font-size: 0.8rem; margin: 0 0 1rem; }
```

- [ ] **Step 4: Run the existing mobile setup tests to confirm nothing broke**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/MobileSetup.test.ts`
Expected: PASS — the existing tests still bypass the scan button via the manual form fields.

(If a test fails because it can't find a form field, the test was matching an element no longer at the top level. Adjust the test selector to use `data-testid` instead of structural traversal — only edit tests if they fail.)

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/mobile/MobileSetup.vue
git commit -m "mobile: MobileSetup adds Scan QR primary button + PairingConsume route"
```

---

## Phase F — i18n + smoke

### Task F1: Add i18n keys (en + zh-CN)

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

- [ ] **Step 1: Locate the `settings.relay` block**

Open `desktop/frontend/src/i18n/messages/en.ts`. Find the `relay: {` object that contains `wizard: {` (around line 190). Inside that `relay` object (sibling of `wizard`), add:

```ts
      pairing: {
        title: 'Pair mobile device',
        hint: 'Scan this QR code with atterm on your phone to connect it to the same relay account.',
        generate: 'Generate QR code',
        generating: 'Generating…',
        regenerate: 'Regenerate',
        expiresIn: 'Expires in {time}',
        expired: 'Expired — generate a new code',
      },
```

- [ ] **Step 2: Locate the `mobile` block in the same file**

Find the top-level `mobile: {` object (around line 290). Add inside:

```ts
    pairing: {
      scan: 'Scan QR code',
      orManual: '— or enter manually —',
      connecting: 'Pairing…',
      failed: 'Pairing failed.',
      back: 'Try again',
      cameraDenied: 'Camera permission required to scan the QR code.',
      noQrDetected: 'No QR code detected — try again.',
    },
```

- [ ] **Step 3: Mirror the same shape in `zh-CN.ts`**

Open `desktop/frontend/src/i18n/messages/zh-CN.ts`. Add to the matching `settings.relay` block:

```ts
      pairing: {
        title: '配对手机',
        hint: '用手机上的 atterm 扫描这个二维码，即可让手机连接到同一个 relay 账号。',
        generate: '生成二维码',
        generating: '生成中…',
        regenerate: '重新生成',
        expiresIn: '{time} 后过期',
        expired: '已过期，请重新生成',
      },
```

And to the matching `mobile:` block:

```ts
    pairing: {
      scan: '扫描二维码',
      orManual: '— 或手动输入 —',
      connecting: '配对中…',
      failed: '配对失败',
      back: '重试',
      cameraDenied: '需要相机权限才能扫描二维码',
      noQrDetected: '未识别到二维码，请重试',
    },
```

- [ ] **Step 4: Run the i18n test to verify key parity**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/i18n/i18n.test.ts`
Expected: PASS — the existing parity test (it walks both locale trees and asserts the same key set) accepts the new keys as long as both files have them.

Then run the full frontend suite:
Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "i18n: add pairing keys (en + zh-CN)"
```

---

### Task F2: End-to-end smoke

**Files:**
- (none — verification only)

- [ ] **Step 1: Run the full backend test suite**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./...`
Expected: PASS.

- [ ] **Step 2: Run the full frontend test suite**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test`
Expected: PASS.

- [ ] **Step 3: Build the desktop bundle to confirm Wails bindings regenerate cleanly**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:wails`
Expected: build succeeds.

- [ ] **Step 4: Build the Capacitor bundle to confirm mobile bundle compiles with the new plugin**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:capacitor`
Expected: build succeeds.

- [ ] **Step 5: Manual / human smoke (not gating)**

Notes for the maintainer running this by hand:
1. Start the relay locally: `go run ./cmd/atterm-relay`.
2. Open the desktop app, sign in or use an existing user, paste a valid `atk_…`, save.
3. Open Settings → Relay; the "Pair mobile device" block appears at the bottom.
4. Click "Generate QR code"; a 240×240 QR image and a 5:00 countdown render.
5. On an iOS device with the dev build installed (or in the iOS Simulator with a fake QR image displayed in another window), tap "Scan QR" on the MobileSetup screen and point the camera at the code.
6. Mobile reaches the home screen connected as the same user. Token revocation in the relay `api_tokens` table shows two rows: one `source=manual` (desktop) and one `source=pairing` (mobile).

No commit needed — Phase F2 is a verification gate.

---

## Self-review notes

- Spec coverage: each spec sub-section maps to at least one task. §3.1/§3.2 → B1+B2. §3.3 → B3 step 3. §3.4 → B3 (tests + impl). §4.1 → C2. §4.2 → C3. §4.3 → covered by B3+C3. §5.1+§5.3 → D4. §5.2 → D2. §5.4 (corrected) → A1+D1. §6.1 → E1. §6.2 → E4. §6.3 → E3. §6.4 → no code (informational; already true). §7.1 → C3 (error code surfaces correctly). §7.2 (logs/metrics) intentionally deferred — not in scope of this plan; tracked separately if needed. §8.1 → B2+B3. §8.2 → C2+C3. §8.3 → D4+E3.
- Placeholder scan: no TBDs; every code step shows the actual code. The B2 step about `newTestStore` reuses the helper already present in the userstore package (see `invitations_test.go`).
- Type consistency: `PairingTokenResponse` (Go) / `PairingToken` (TS) are deliberately named differently to follow the existing `RelayMe` (Go) / `RelayMe` (TS) symmetry — both share field names `token`, `expires_at`, `qr_url`. The TS interface `PairingConsumeResult` exactly matches the Go response body shape from C3.
