# Feishu App-Mode Integration (Relay M0+M1+M2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the legacy `Format=feishu` outbound webhook path with a self-built Feishu application: per-user encrypted app credentials in the relay store, tenant_access_token cache, interactive cards for `command_finished` and `waiting_input`, inbound event callback for one-time short-code binding and `ack` card action.

**Architecture:** New `internal/feishu/` package owns Feishu specifics (token cache, IM client, card schemas, AES decrypt, envelope verify). New `internal/userstore/feishu_bindings` + `feishu_pending_binds` tables hold per-user state with ChaCha20-Poly1305 field encryption (key from `ATTERM_FEISHU_ENCRYPT_KEY`). Relay wires the service into existing dispatch sites in `uplink_conn.go` and registers `/v1/feishu/bindings/me` (CRUD + begin-pair) plus `/v1/feishu/events/{app_id_hash}` (unauth, verified by encrypt_key) HTTP routes.

**Tech Stack:** Go 1.22, modernc.org/sqlite (existing), golang.org/x/crypto/chacha20poly1305 (new dep), golang.org/x/sync/singleflight (new dep), stdlib `net/http` + `httptest`. No Feishu SDK dependency — we implement the thin client ourselves (two endpoints + decrypt routine).

**Spec:** [`docs/superpowers/specs/2026-06-17-feishu-app-integration-design.md`](../specs/2026-06-17-feishu-app-integration-design.md)

---

## File Structure

**New files:**

| Path | Responsibility |
|---|---|
| `internal/userstore/migrations/0005_feishu.sql` | Create `feishu_bindings` + `feishu_pending_binds`; delete legacy `Format=feishu` webhook rows |
| `internal/userstore/secret_encrypt.go` | `SecretCipher` — ChaCha20-Poly1305 AEAD wrapper used by feishu store CRUD |
| `internal/userstore/secret_encrypt_test.go` | Round-trip, distinct nonces, wrong-key fails |
| `internal/userstore/feishu_bindings.go` | `FeishuBinding` type + `UpsertFeishuBinding`, `GetFeishuBinding`, `GetFeishuBindingByAppIDHash`, `MarkFeishuBindingBound`, `MarkFeishuBindingDisabled`, `ClearFeishuBindingDisabled`, `DeleteFeishuBinding` |
| `internal/userstore/feishu_bindings_test.go` | CRUD + encryption + `UNIQUE(app_id_hash)` + 404 sentinel |
| `internal/userstore/feishu_pending_binds.go` | `PutFeishuPendingBind`, `ConsumeFeishuPendingBind`, `SweepExpiredFeishuPendingBinds` |
| `internal/userstore/feishu_pending_binds_test.go` | atomic consume, expiry, `UNIQUE(user_id)` overwrite |
| `internal/feishu/token.go` | `TenantTokenCache` — fetch + memo + singleflight + auth-class detection |
| `internal/feishu/token_test.go` | cache hit/miss/expiry, singleflight, auth-class disable after 3 fails |
| `internal/feishu/card.go` | `RenderCommandFinishedCard`, `RenderWaitingInputCard`, `RenderAckUpdateCard` |
| `internal/feishu/card_test.go` | golden-shape JSON, sealed variant, deep-link string |
| `internal/feishu/event.go` | `DecryptEnvelope`, `ParseEnvelope`, `URLVerification`, `MessageEvent`, `CardAction`, `Envelope` types |
| `internal/feishu/event_test.go` | AES round-trip, tampered ciphertext, url_verification short-circuit, schema parsing |
| `internal/feishu/client.go` | `Client` — `MintTenantToken`, `SendInteractiveToOpenID`, `SendTextToOpenID` |
| `internal/feishu/client_test.go` | httptest.Server stubs Feishu; request envelope + 429/5xx handling |
| `internal/feishu/service.go` | `Service` — aggregate `SendCommandFinished`, `SendSessionNotification`, `HandleEvent`, `HandleCardCallback` |
| `internal/feishu/service_test.go` | in-memory fake store + fake transport; outbound + inbound dispatch |
| `internal/relay/feishu_http.go` | HTTP handlers for `/v1/feishu/bindings/me*` and `/v1/feishu/events/{app_id_hash}` |
| `internal/relay/feishu_http_test.go` | per-route tests with stubbed Feishu |
| `internal/relay/uplink_feishu_test.go` | uplink integration: CMD-EVENT → service called |

**Modified files:**

| Path | Change |
|---|---|
| `internal/userstore/store.go` | Add `cipher *SecretCipher` field + `WithSecretCipher` option; thread cipher into feishu CRUD; widen `Open` signature |
| `internal/userstore/webhooks.go` | Doc-comment `Format` to `"generic"` only |
| `internal/webhook/render.go` | Delete `renderFeishu` and the `format == "feishu"` branch in `renderForFormat` |
| `internal/webhook/render_test.go` | Drop test cases that exercised the feishu branch |
| `internal/webhook/dispatch_test.go` | Replace `Format: "feishu"` test rows with `Format: "generic"` |
| `internal/relay/server.go` | Register feishu HTTP routes; add `Feishu *feishu.Service` to Config |
| `internal/relay/uplink_conn.go` | Add feishu service calls next to existing `webpush.Dispatch*` calls (`:524` and `:180`/`SessionNotification` site) |
| `cmd/relay/main.go` | Load `ATTERM_FEISHU_ENCRYPT_KEY`, construct `feishu.Service`, wire into store + relay config |
| `go.mod` / `go.sum` | Add `golang.org/x/sync` (singleflight). `chacha20poly1305` is under `golang.org/x/crypto` which we may already pull in transitively; verify and add if missing |

**Dependencies (build order):**

```
T1  migration                  ┐
T2  secret_encrypt             ├─▶ T3  feishu_bindings store
T3  feishu_bindings store      ┤
T4  feishu_pending_binds store ┘

T5  feishu/token  ─┐
T6  feishu/card   ─┤
T7  feishu/event  ─┼─▶ T8  feishu/client ─▶ T9  feishu/service
                   │                              │
T10 webhook cleanup (independent, parallel-safe)  │
                                                  ▼
                                T11 relay feishu_http bindings CRUD
                                T12 relay feishu_http events callback
                                T13 uplink integration
                                T14 main.go assembly + manual e2e
```

---

## Conventions (read once)

- All Go test files run via `go test ./...`. Per-package: `go test ./internal/feishu/...`
- Test helper `newTestStore(t)` exists in `internal/userstore/testutil.go`. Modify it in T2 to pass a real `SecretCipher` (deterministic test key).
- ID generation uses `defaultIDs.New()` (ULID) — see `internal/userstore/ulid.go:33`.
- Existing code uses no testify; assert with `if got != want { t.Fatalf(...) }`.
- Commits use [Conventional Commits](https://www.conventionalcommits.org/): `feat(scope): ...`, `test(scope): ...`, `refactor(scope): ...`, `fix(scope): ...`.
- After each task, run `go build ./...` and the affected package's tests before committing.

---

## Task 1: DB migration + legacy feishu webhook removal

**Files:**
- Create: `internal/userstore/migrations/0005_feishu.sql`

- [ ] **Step 1: Write the migration**

```sql
-- 0005_feishu.sql
-- Add Feishu app-mode bindings + pending pair codes.
-- Replace the legacy Format=feishu custom-bot URL path (no backward-compat).

CREATE TABLE feishu_bindings (
    user_id          TEXT    PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    app_id_hash      TEXT    NOT NULL UNIQUE,
    app_id_enc       BLOB    NOT NULL,
    app_secret_enc   BLOB    NOT NULL,
    encrypt_key_enc  BLOB    NOT NULL,
    verify_token_enc BLOB    NOT NULL,
    open_id          TEXT,
    bound_at         INTEGER,
    disabled_at      INTEGER,
    created_at       INTEGER NOT NULL
);

CREATE TABLE feishu_pending_binds (
    user_id    TEXT    PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    code       TEXT    NOT NULL UNIQUE,
    expires_at INTEGER NOT NULL
);
CREATE INDEX feishu_pending_binds_expires ON feishu_pending_binds(expires_at);

DELETE FROM webhooks WHERE format = 'feishu';
```

- [ ] **Step 2: Verify migration applies cleanly**

Run:

```bash
go test ./internal/userstore/ -run TestOpenAppliesMigrations
```

If no such test exists yet, fall back to:

```bash
go test ./internal/userstore/...
```

Expected: ALL TESTS PASS — existing tests should still pass because the new tables don't touch existing schema; the `DELETE FROM webhooks` is a no-op on a fresh test DB.

- [ ] **Step 3: Commit**

```bash
git add internal/userstore/migrations/0005_feishu.sql
git commit -m "feat(userstore): migration 0005 — feishu_bindings + pending_binds + drop legacy feishu webhook rows"
```

---

## Task 2: `SecretCipher` AEAD helper

**Files:**
- Create: `internal/userstore/secret_encrypt.go`
- Create: `internal/userstore/secret_encrypt_test.go`
- Modify: `internal/userstore/store.go` (add `cipher` field + `WithSecretCipher` option)
- Modify: `internal/userstore/testutil.go` (pass a deterministic test cipher to `newTestStore`)
- Modify: `go.mod` (add `golang.org/x/crypto/chacha20poly1305` import — likely already transitively present)

- [ ] **Step 1: Write the test file**

```go
// internal/userstore/secret_encrypt_test.go
package userstore

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return k
}

func TestSecretCipher_RoundTrip(t *testing.T) {
	c, err := NewSecretCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewSecretCipher: %v", err)
	}
	for _, plain := range []string{"", "a", "app_secret_token_123", "中文 ☃"} {
		ct, err := c.Encrypt([]byte(plain))
		if err != nil {
			t.Fatalf("encrypt %q: %v", plain, err)
		}
		got, err := c.Decrypt(ct)
		if err != nil {
			t.Fatalf("decrypt %q: %v", plain, err)
		}
		if string(got) != plain {
			t.Fatalf("round-trip mismatch: want %q got %q", plain, string(got))
		}
	}
}

func TestSecretCipher_DistinctNonces(t *testing.T) {
	c, _ := NewSecretCipher(testKey(t))
	a, _ := c.Encrypt([]byte("same"))
	b, _ := c.Encrypt([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatalf("encrypting same plaintext twice produced identical ciphertext")
	}
}

func TestSecretCipher_WrongKeyFails(t *testing.T) {
	c1, _ := NewSecretCipher(testKey(t))
	c2, _ := NewSecretCipher(testKey(t))
	ct, _ := c1.Encrypt([]byte("hello"))
	if _, err := c2.Decrypt(ct); err == nil {
		t.Fatalf("decrypt with wrong key should fail")
	}
}

func TestSecretCipher_BadKeyLength(t *testing.T) {
	if _, err := NewSecretCipher(make([]byte, 16)); err == nil {
		t.Fatalf("16-byte key should be rejected")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail to compile**

Run: `go test ./internal/userstore/ -run TestSecretCipher`
Expected: FAIL — `undefined: NewSecretCipher`

- [ ] **Step 3: Implement `secret_encrypt.go`**

```go
// internal/userstore/secret_encrypt.go
package userstore

import (
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// SecretCipher wraps a single ChaCha20-Poly1305 AEAD with a random
// per-message nonce. Output layout: nonce(12B) || ciphertext || tag(16B).
//
// One cipher per relay process; key sourced from the
// ATTERM_FEISHU_ENCRYPT_KEY env var by the relay main. Missing /
// wrong-length key fails fast at startup — there is no plaintext fallback.
type SecretCipher struct {
	aead interface {
		Seal(dst, nonce, plaintext, additionalData []byte) []byte
		Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
		NonceSize() int
	}
}

// NewSecretCipher constructs a cipher from a 32-byte key.
func NewSecretCipher(key []byte) (*SecretCipher, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("secret cipher: key length must be %d, got %d", chacha20poly1305.KeySize, len(key))
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("chacha20poly1305.New: %w", err)
	}
	return &SecretCipher{aead: aead}, nil
}

// Encrypt returns nonce||ct||tag.
func (c *SecretCipher) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	out := c.aead.Seal(nonce, nonce, plain, nil)
	return out, nil
}

// Decrypt inverts Encrypt.
func (c *SecretCipher) Decrypt(blob []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(blob) < n {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := blob[:n], blob[n:]
	plain, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("aead open: %w", err)
	}
	return plain, nil
}
```

- [ ] **Step 4: Wire the cipher into `SQLiteStore`**

Modify `internal/userstore/store.go` — add field and option:

```go
// add to SQLiteStore struct
type SQLiteStore struct {
	db     *sql.DB
	cipher *SecretCipher // nil = feishu CRUD will error
}

// add option type + setter near Open()
type OpenOption func(*SQLiteStore)

func WithSecretCipher(c *SecretCipher) OpenOption {
	return func(s *SQLiteStore) { s.cipher = c }
}
```

Change `Open(ctx, path)` to `Open(ctx, path string, opts ...OpenOption)`:

```go
func Open(ctx context.Context, path string, opts ...OpenOption) (*SQLiteStore, error) {
	// ... existing body up to the `return &SQLiteStore{db: db}, nil` line ...
	s := &SQLiteStore{db: db}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}
```

Search for callers of `userstore.Open(` across the repo and confirm none break (variadic option pattern is backward compatible).

```bash
grep -Rn "userstore.Open(" --include='*.go' .
```

- [ ] **Step 5: Update `newTestStore` to pass a deterministic cipher**

Modify `internal/userstore/testutil.go`:

```go
// at top of file, with other imports
import (
	// ... existing ...
)

// testCipher is shared by all tests so that feishu_bindings round-trip
// across goroutines / subtests without each test wiring its own.
var testCipher = mustTestCipher()

func mustTestCipher() *SecretCipher {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i) // deterministic, NOT for production
	}
	c, err := NewSecretCipher(key)
	if err != nil {
		panic(err)
	}
	return c
}
```

Then update the existing `newTestStore` function — wherever it calls `Open`, add the cipher option. Example modification (exact existing body may differ; preserve other args):

```go
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := Open(context.Background(), ":memory:", WithSecretCipher(testCipher))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/userstore/...`
Expected: PASS — both the new SecretCipher tests and all pre-existing userstore tests still green.

- [ ] **Step 7: Commit**

```bash
git add internal/userstore/secret_encrypt.go \
        internal/userstore/secret_encrypt_test.go \
        internal/userstore/store.go \
        internal/userstore/testutil.go \
        go.mod go.sum
git commit -m "feat(userstore): add SecretCipher (ChaCha20-Poly1305) for field-level secret encryption"
```

---

## Task 3: `feishu_bindings` CRUD

**Files:**
- Create: `internal/userstore/feishu_bindings.go`
- Create: `internal/userstore/feishu_bindings_test.go`

- [ ] **Step 1: Write the test file**

```go
// internal/userstore/feishu_bindings_test.go
package userstore

import (
	"context"
	"errors"
	"testing"
)

func TestFeishuBinding_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, err := s.CreateOpaqueUser(ctx, "fbu@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	in := FeishuBindingCredentials{
		AppID:       "cli_aabbccdd",
		AppSecret:   "secret-xyz",
		EncryptKey:  "encryptkey-32-bytes-string-here!",
		VerifyToken: "tok-abcdef",
	}
	if err := s.UpsertFeishuBinding(ctx, u.ID, in); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetFeishuBinding(ctx, u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.UserID != u.ID || got.OpenID != "" || got.BoundAt != 0 || got.DisabledAt != 0 {
		t.Fatalf("unexpected binding row: %+v", got)
	}
	if got.AppID != in.AppID || got.AppSecret != in.AppSecret || got.EncryptKey != in.EncryptKey || got.VerifyToken != in.VerifyToken {
		t.Fatalf("decrypt mismatch: %+v vs %+v", got.FeishuBindingCredentials, in)
	}
	if got.AppIDHash == "" || len(got.AppIDHash) != 64 {
		t.Fatalf("expected SHA256 hex hash, got %q", got.AppIDHash)
	}
}

func TestFeishuBinding_GetByAppIDHash(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "fbh@example.com")
	in := FeishuBindingCredentials{AppID: "cli_zzz", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}
	_ = s.UpsertFeishuBinding(ctx, u.ID, in)

	full, err := s.GetFeishuBinding(ctx, u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	by, err := s.GetFeishuBindingByAppIDHash(ctx, full.AppIDHash)
	if err != nil {
		t.Fatalf("get by hash: %v", err)
	}
	if by.UserID != u.ID {
		t.Fatalf("by-hash returned wrong user: %s vs %s", by.UserID, u.ID)
	}
}

func TestFeishuBinding_GetByAppIDHash_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, err := s.GetFeishuBindingByAppIDHash(ctx, "deadbeef")
	if !errors.Is(err, ErrFeishuBindingNotFound) {
		t.Fatalf("want ErrFeishuBindingNotFound, got %v", err)
	}
}

func TestFeishuBinding_UniqueAppIDHash(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u1, _ := s.CreateOpaqueUser(ctx, "a@example.com")
	u2, _ := s.CreateOpaqueUser(ctx, "b@example.com")
	in := FeishuBindingCredentials{AppID: "cli_shared", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}
	if err := s.UpsertFeishuBinding(ctx, u1.ID, in); err != nil {
		t.Fatalf("u1 upsert: %v", err)
	}
	if err := s.UpsertFeishuBinding(ctx, u2.ID, in); !errors.Is(err, ErrFeishuAppIDConflict) {
		t.Fatalf("want ErrFeishuAppIDConflict, got %v", err)
	}
}

func TestFeishuBinding_MarkBoundAndDisable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "fbm@example.com")
	in := FeishuBindingCredentials{AppID: "cli_mb", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}
	_ = s.UpsertFeishuBinding(ctx, u.ID, in)

	if err := s.MarkFeishuBindingBound(ctx, u.ID, "ou_open123"); err != nil {
		t.Fatalf("mark bound: %v", err)
	}
	got, _ := s.GetFeishuBinding(ctx, u.ID)
	if got.OpenID != "ou_open123" || got.BoundAt == 0 {
		t.Fatalf("bound state not persisted: %+v", got)
	}

	if err := s.MarkFeishuBindingDisabled(ctx, u.ID); err != nil {
		t.Fatalf("mark disabled: %v", err)
	}
	got, _ = s.GetFeishuBinding(ctx, u.ID)
	if got.DisabledAt == 0 {
		t.Fatalf("disabled_at not set")
	}

	if err := s.ClearFeishuBindingDisabled(ctx, u.ID); err != nil {
		t.Fatalf("clear disabled: %v", err)
	}
	got, _ = s.GetFeishuBinding(ctx, u.ID)
	if got.DisabledAt != 0 {
		t.Fatalf("disabled_at not cleared")
	}
}

func TestFeishuBinding_Delete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "fbd@example.com")
	in := FeishuBindingCredentials{AppID: "cli_d", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}
	_ = s.UpsertFeishuBinding(ctx, u.ID, in)

	if err := s.DeleteFeishuBinding(ctx, u.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := s.GetFeishuBinding(ctx, u.ID)
	if !errors.Is(err, ErrFeishuBindingNotFound) {
		t.Fatalf("want not-found after delete, got %v", err)
	}
}

func TestFeishuBinding_RequiresCipher(t *testing.T) {
	ctx := context.Background()
	// Bypass newTestStore — open without WithSecretCipher.
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	u, _ := s.CreateOpaqueUser(ctx, "noc@example.com")
	err = s.UpsertFeishuBinding(ctx, u.ID, FeishuBindingCredentials{AppID: "x", AppSecret: "y", EncryptKey: "z", VerifyToken: "t"})
	if err == nil || !errorContains(err, "cipher") {
		t.Fatalf("expected cipher-required error, got %v", err)
	}
}

func errorContains(err error, sub string) bool {
	if err == nil {
		return false
	}
	for i := 0; i+len(sub) <= len(err.Error()); i++ {
		if err.Error()[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail to compile**

Run: `go test ./internal/userstore/ -run TestFeishuBinding`
Expected: FAIL — undefined types/funcs.

- [ ] **Step 3: Implement `feishu_bindings.go`**

```go
// internal/userstore/feishu_bindings.go
package userstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrFeishuBindingNotFound is returned by Get* methods when no row exists.
var ErrFeishuBindingNotFound = errors.New("userstore: feishu binding not found")

// ErrFeishuAppIDConflict is returned by Upsert when another user already
// holds the same app_id (UNIQUE constraint on app_id_hash).
var ErrFeishuAppIDConflict = errors.New("userstore: feishu app_id already bound by another user")

// FeishuBindingCredentials carries the user-supplied secrets we encrypt
// before persisting. Returned by Get* with plaintext fields populated.
type FeishuBindingCredentials struct {
	AppID       string
	AppSecret   string
	EncryptKey  string
	VerifyToken string
}

// FeishuBinding is the full row.
type FeishuBinding struct {
	UserID    string
	AppIDHash string
	FeishuBindingCredentials
	OpenID     string
	BoundAt    int64 // unix seconds, 0 if not bound
	DisabledAt int64 // unix seconds, 0 if not disabled
	CreatedAt  int64
}

func hashAppID(appID string) string {
	sum := sha256.Sum256([]byte(appID))
	return hex.EncodeToString(sum[:])
}

func (s *SQLiteStore) requireCipher() error {
	if s.cipher == nil {
		return fmt.Errorf("userstore: secret cipher not configured (WithSecretCipher required for feishu CRUD)")
	}
	return nil
}

// UpsertFeishuBinding inserts or replaces the row for userID. open_id /
// bound_at / disabled_at are preserved on upsert (only credentials are
// rewritten).
func (s *SQLiteStore) UpsertFeishuBinding(ctx context.Context, userID string, c FeishuBindingCredentials) error {
	if err := s.requireCipher(); err != nil {
		return err
	}
	hash := hashAppID(c.AppID)
	encA, err := s.cipher.Encrypt([]byte(c.AppID))
	if err != nil {
		return fmt.Errorf("encrypt app_id: %w", err)
	}
	encS, err := s.cipher.Encrypt([]byte(c.AppSecret))
	if err != nil {
		return fmt.Errorf("encrypt app_secret: %w", err)
	}
	encK, err := s.cipher.Encrypt([]byte(c.EncryptKey))
	if err != nil {
		return fmt.Errorf("encrypt encrypt_key: %w", err)
	}
	encV, err := s.cipher.Encrypt([]byte(c.VerifyToken))
	if err != nil {
		return fmt.Errorf("encrypt verify_token: %w", err)
	}

	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO feishu_bindings(user_id, app_id_hash, app_id_enc, app_secret_enc, encrypt_key_enc, verify_token_enc, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   app_id_hash      = excluded.app_id_hash,
		   app_id_enc       = excluded.app_id_enc,
		   app_secret_enc   = excluded.app_secret_enc,
		   encrypt_key_enc  = excluded.encrypt_key_enc,
		   verify_token_enc = excluded.verify_token_enc,
		   disabled_at      = NULL`,
		userID, hash, encA, encS, encK, encV, now,
	)
	if err != nil {
		if isUniqueViolation(err, "feishu_bindings.app_id_hash") {
			return ErrFeishuAppIDConflict
		}
		return fmt.Errorf("upsert feishu binding: %w", err)
	}
	return nil
}

// isUniqueViolation detects modernc.org/sqlite UNIQUE constraint errors.
// The driver surfaces them in the error message text; we match on the
// table.column substring for stability.
func isUniqueViolation(err error, qualifiedCol string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "UNIQUE constraint failed: "+qualifiedCol)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func (s *SQLiteStore) GetFeishuBinding(ctx context.Context, userID string) (*FeishuBinding, error) {
	return s.getFeishuBinding(ctx,
		`SELECT user_id, app_id_hash, app_id_enc, app_secret_enc, encrypt_key_enc, verify_token_enc,
		        IFNULL(open_id, ''), IFNULL(bound_at, 0), IFNULL(disabled_at, 0), created_at
		 FROM feishu_bindings WHERE user_id = ?`,
		userID,
	)
}

func (s *SQLiteStore) GetFeishuBindingByAppIDHash(ctx context.Context, hash string) (*FeishuBinding, error) {
	return s.getFeishuBinding(ctx,
		`SELECT user_id, app_id_hash, app_id_enc, app_secret_enc, encrypt_key_enc, verify_token_enc,
		        IFNULL(open_id, ''), IFNULL(bound_at, 0), IFNULL(disabled_at, 0), created_at
		 FROM feishu_bindings WHERE app_id_hash = ?`,
		hash,
	)
}

func (s *SQLiteStore) getFeishuBinding(ctx context.Context, q string, arg string) (*FeishuBinding, error) {
	if err := s.requireCipher(); err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, q, arg)
	var b FeishuBinding
	var encA, encS, encK, encV []byte
	err := row.Scan(&b.UserID, &b.AppIDHash, &encA, &encS, &encK, &encV,
		&b.OpenID, &b.BoundAt, &b.DisabledAt, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFeishuBindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan feishu binding: %w", err)
	}
	if plain, err := s.cipher.Decrypt(encA); err != nil {
		return nil, fmt.Errorf("decrypt app_id: %w", err)
	} else {
		b.AppID = string(plain)
	}
	if plain, err := s.cipher.Decrypt(encS); err != nil {
		return nil, fmt.Errorf("decrypt app_secret: %w", err)
	} else {
		b.AppSecret = string(plain)
	}
	if plain, err := s.cipher.Decrypt(encK); err != nil {
		return nil, fmt.Errorf("decrypt encrypt_key: %w", err)
	} else {
		b.EncryptKey = string(plain)
	}
	if plain, err := s.cipher.Decrypt(encV); err != nil {
		return nil, fmt.Errorf("decrypt verify_token: %w", err)
	} else {
		b.VerifyToken = string(plain)
	}
	return &b, nil
}

func (s *SQLiteStore) MarkFeishuBindingBound(ctx context.Context, userID, openID string) error {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE feishu_bindings SET open_id = ?, bound_at = ? WHERE user_id = ?`,
		openID, now, userID,
	)
	if err != nil {
		return fmt.Errorf("mark bound: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrFeishuBindingNotFound
	}
	return nil
}

func (s *SQLiteStore) MarkFeishuBindingDisabled(ctx context.Context, userID string) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		`UPDATE feishu_bindings SET disabled_at = ? WHERE user_id = ?`, now, userID)
	if err != nil {
		return fmt.Errorf("mark disabled: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ClearFeishuBindingDisabled(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE feishu_bindings SET disabled_at = NULL WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("clear disabled: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteFeishuBinding(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM feishu_bindings WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/userstore/ -run TestFeishuBinding -v`
Expected: PASS for all 7 sub-tests.

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/feishu_bindings.go internal/userstore/feishu_bindings_test.go
git commit -m "feat(userstore): feishu_bindings CRUD with AEAD-encrypted secret columns"
```

---

## Task 4: `feishu_pending_binds` CRUD

**Files:**
- Create: `internal/userstore/feishu_pending_binds.go`
- Create: `internal/userstore/feishu_pending_binds_test.go`

- [ ] **Step 1: Write the test file**

```go
// internal/userstore/feishu_pending_binds_test.go
package userstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPendingBind_PutAndConsume(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "pb1@example.com")

	exp := time.Now().Add(15 * time.Minute).Unix()
	if err := s.PutFeishuPendingBind(ctx, u.ID, "AB3CD7", exp); err != nil {
		t.Fatalf("put: %v", err)
	}

	uid, err := s.ConsumeFeishuPendingBind(ctx, "AB3CD7")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if uid != u.ID {
		t.Fatalf("consume returned wrong user: %s vs %s", uid, u.ID)
	}

	// Second consume must fail.
	_, err = s.ConsumeFeishuPendingBind(ctx, "AB3CD7")
	if !errors.Is(err, ErrFeishuPendingBindNotFound) {
		t.Fatalf("want ErrFeishuPendingBindNotFound on second consume, got %v", err)
	}
}

func TestPendingBind_OverwriteOnRePut(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "pb2@example.com")

	exp := time.Now().Add(time.Hour).Unix()
	_ = s.PutFeishuPendingBind(ctx, u.ID, "OLD123", exp)
	if err := s.PutFeishuPendingBind(ctx, u.ID, "NEW999", exp); err != nil {
		t.Fatalf("re-put: %v", err)
	}

	if _, err := s.ConsumeFeishuPendingBind(ctx, "OLD123"); !errors.Is(err, ErrFeishuPendingBindNotFound) {
		t.Fatalf("OLD code should be gone")
	}
	uid, err := s.ConsumeFeishuPendingBind(ctx, "NEW999")
	if err != nil {
		t.Fatalf("NEW consume: %v", err)
	}
	if uid != u.ID {
		t.Fatalf("wrong user")
	}
}

func TestPendingBind_Expired(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "pb3@example.com")

	exp := time.Now().Add(-time.Second).Unix()
	_ = s.PutFeishuPendingBind(ctx, u.ID, "EXPIRED", exp)

	_, err := s.ConsumeFeishuPendingBind(ctx, "EXPIRED")
	if !errors.Is(err, ErrFeishuPendingBindNotFound) {
		t.Fatalf("expired code should consume as not-found, got %v", err)
	}
}

func TestPendingBind_Sweep(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u1, _ := s.CreateOpaqueUser(ctx, "sw1@example.com")
	u2, _ := s.CreateOpaqueUser(ctx, "sw2@example.com")
	_ = s.PutFeishuPendingBind(ctx, u1.ID, "OLD111", time.Now().Add(-time.Minute).Unix())
	_ = s.PutFeishuPendingBind(ctx, u2.ID, "FRSH22", time.Now().Add(time.Hour).Unix())

	n, err := s.SweepExpiredFeishuPendingBinds(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 swept, got %d", n)
	}
	// FRSH22 still there.
	if _, err := s.ConsumeFeishuPendingBind(ctx, "FRSH22"); err != nil {
		t.Fatalf("FRSH should still work: %v", err)
	}
}

func TestGenerateFeishuPairCode(t *testing.T) {
	for i := 0; i < 50; i++ {
		code := GenerateFeishuPairCode()
		if len(code) != 6 {
			t.Fatalf("code length: %q", code)
		}
		const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
		for _, r := range code {
			if !strings.ContainsRune(alphabet, r) {
				t.Fatalf("code %q has illegal char %q", code, r)
			}
		}
	}
}
```

- [ ] **Step 2: Run to verify compile failure**

Run: `go test ./internal/userstore/ -run TestPendingBind`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement `feishu_pending_binds.go`**

```go
// internal/userstore/feishu_pending_binds.go
package userstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrFeishuPendingBindNotFound is returned by Consume when no matching
// non-expired row exists. We do not distinguish "wrong code" from
// "expired" — both surface as the same error to avoid an oracle.
var ErrFeishuPendingBindNotFound = errors.New("userstore: feishu pending bind not found or expired")

// feishuPairAlphabet excludes I, O, 0, 1, L (visually confusable).
const feishuPairAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateFeishuPairCode returns a 6-char short-code drawn from the
// confusable-free alphabet. Entropy: ~30 bits (32^6 ≈ 1 billion);
// safe against brute force inside a 15-minute window.
func GenerateFeishuPairCode() string {
	buf := make([]byte, 6)
	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		// rand.Read should never fail; panicking matches stdlib practice.
		panic(fmt.Sprintf("rand.Read: %v", err))
	}
	for i, b := range rb {
		buf[i] = feishuPairAlphabet[int(b)%len(feishuPairAlphabet)]
	}
	return string(buf)
}

// PutFeishuPendingBind upserts the user's pending code (one row per user).
// expiresAt is unix seconds.
func (s *SQLiteStore) PutFeishuPendingBind(ctx context.Context, userID, code string, expiresAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO feishu_pending_binds(user_id, code, expires_at)
		 VALUES(?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   code = excluded.code,
		   expires_at = excluded.expires_at`,
		userID, code, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("put pending bind: %w", err)
	}
	return nil
}

// ConsumeFeishuPendingBind atomically deletes a non-expired row matching
// code and returns the owning user_id. Concurrent callers race on the
// DELETE — only one wins.
func (s *SQLiteStore) ConsumeFeishuPendingBind(ctx context.Context, code string) (string, error) {
	now := time.Now().Unix()
	var userID string
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM feishu_pending_binds
		 WHERE code = ? AND expires_at > ?
		 RETURNING user_id`,
		code, now,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrFeishuPendingBindNotFound
	}
	if err != nil {
		return "", fmt.Errorf("consume pending bind: %w", err)
	}
	return userID, nil
}

// SweepExpiredFeishuPendingBinds deletes all expired rows. Returns count.
func (s *SQLiteStore) SweepExpiredFeishuPendingBinds(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM feishu_pending_binds WHERE expires_at <= ?`,
		time.Now().Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("sweep: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/userstore/ -run "TestPendingBind|TestGenerateFeishuPairCode" -v`
Expected: PASS for all 5 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/feishu_pending_binds.go internal/userstore/feishu_pending_binds_test.go
git commit -m "feat(userstore): feishu_pending_binds CRUD with atomic RETURNING consume"
```

---

## Task 5: `feishu/token` — tenant_access_token cache

**Files:**
- Create: `internal/feishu/token.go`
- Create: `internal/feishu/token_test.go`
- Modify: `go.mod` (add `golang.org/x/sync/singleflight`)

- [ ] **Step 1: Write the test**

```go
// internal/feishu/token_test.go
package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTokenStubServer(t *testing.T, code int, token string, expiresIn int) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":               code,
			"msg":                "ok",
			"tenant_access_token": token,
			"expire":             expiresIn,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestTokenCache_FetchAndReuse(t *testing.T) {
	srv, calls := newTokenStubServer(t, 0, "t_abc", 7200)
	cache := NewTenantTokenCache(srv.URL, srv.Client(), func() time.Time { return time.Unix(1_000_000, 0) })

	tok, err := cache.Get(context.Background(), "app1", "secret1")
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	if tok != "t_abc" {
		t.Fatalf("token: %q", tok)
	}

	tok2, _ := cache.Get(context.Background(), "app1", "secret1")
	if tok2 != "t_abc" {
		t.Fatalf("cached token: %q", tok2)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected 1 upstream call, got %d", got)
	}
}

func TestTokenCache_Expires(t *testing.T) {
	srv, calls := newTokenStubServer(t, 0, "t_short", 60)
	now := int64(1_000_000)
	cache := NewTenantTokenCache(srv.URL, srv.Client(), func() time.Time { return time.Unix(atomic.LoadInt64(&now), 0) })

	_, _ = cache.Get(context.Background(), "app1", "s")
	atomic.StoreInt64(&now, 1_000_000+60+1) // past expire
	_, _ = cache.Get(context.Background(), "app1", "s")

	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("expected 2 calls after expiry, got %d", got)
	}
}

func TestTokenCache_Singleflight(t *testing.T) {
	srv, calls := newTokenStubServer(t, 0, "t_sf", 7200)
	cache := NewTenantTokenCache(srv.URL, srv.Client(), time.Now)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cache.Get(context.Background(), "app1", "s")
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("singleflight should coalesce; got %d upstream calls", got)
	}
}

func TestTokenCache_AuthClassError(t *testing.T) {
	srv, _ := newTokenStubServer(t, 99991663, "", 0) // invalid app_secret
	cache := NewTenantTokenCache(srv.URL, srv.Client(), time.Now)
	_, err := cache.Get(context.Background(), "app1", "s")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsAuthClassError(err) {
		t.Fatalf("expected auth-class error, got %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/feishu/...`
Expected: build failure (package does not exist yet).

- [ ] **Step 3: Implement `token.go`**

```go
// internal/feishu/token.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// authClassCodes are Feishu error codes that mean "your app credentials
// are wrong / disabled" — the relay turns these into a "disable binding"
// signal upstream. List drawn from open.feishu.cn /docs (auth errors).
var authClassCodes = map[int]bool{
	99991663: true, // invalid app_secret
	99991664: true, // invalid app_id
	99991665: true, // app disabled
}

// AuthClassError wraps an upstream auth-related code.
type AuthClassError struct {
	Code int
	Msg  string
}

func (e *AuthClassError) Error() string {
	return fmt.Sprintf("feishu auth-class error: code=%d msg=%s", e.Code, e.Msg)
}

// IsAuthClassError reports whether err (or anything it wraps) is *AuthClassError.
func IsAuthClassError(err error) bool {
	var e *AuthClassError
	return errors.As(err, &e)
}

// TenantTokenCache memoizes tenant_access_token per app_id.
type TenantTokenCache struct {
	baseURL string
	httpC   *http.Client
	now     func() time.Time
	sf      singleflight.Group
	mu      sync.Mutex
	entries map[string]cachedToken
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

func NewTenantTokenCache(baseURL string, httpC *http.Client, now func() time.Time) *TenantTokenCache {
	if httpC == nil {
		httpC = &http.Client{Timeout: 10 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	return &TenantTokenCache{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpC:   httpC,
		now:     now,
		entries: map[string]cachedToken{},
	}
}

// Get returns a valid token, fetching if necessary. Concurrent callers
// for the same app_id share the same upstream request via singleflight.
func (c *TenantTokenCache) Get(ctx context.Context, appID, appSecret string) (string, error) {
	if cur, ok := c.read(appID); ok {
		return cur, nil
	}
	out, err, _ := c.sf.Do(appID, func() (any, error) {
		if cur, ok := c.read(appID); ok {
			return cur, nil
		}
		tok, expSec, err := c.fetch(ctx, appID, appSecret)
		if err != nil {
			return "", err
		}
		c.store(appID, tok, expSec)
		return tok, nil
	})
	if err != nil {
		return "", err
	}
	return out.(string), nil
}

// Invalidate drops the cached entry for appID (used after binding delete).
func (c *TenantTokenCache) Invalidate(appID string) {
	c.mu.Lock()
	delete(c.entries, appID)
	c.mu.Unlock()
}

func (c *TenantTokenCache) read(appID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[appID]
	if !ok {
		return "", false
	}
	// Refresh 5 min before actual expiry to absorb clock drift.
	if c.now().After(e.expiresAt.Add(-5 * time.Minute)) {
		return "", false
	}
	return e.token, true
}

func (c *TenantTokenCache) store(appID, token string, expiresInSec int) {
	c.mu.Lock()
	c.entries[appID] = cachedToken{
		token:     token,
		expiresAt: c.now().Add(time.Duration(expiresInSec) * time.Second),
	}
	c.mu.Unlock()
}

type tenantTokenResp struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"`
}

func (c *TenantTokenCache) fetch(ctx context.Context, appID, appSecret string) (string, int, error) {
	body, _ := json.Marshal(map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	})
	req, _ := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/open-apis/auth/v3/tenant_access_token/internal",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpC.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("tenant_access_token request: %w", err)
	}
	defer resp.Body.Close()
	var r tenantTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", 0, fmt.Errorf("decode token resp: %w", err)
	}
	if r.Code != 0 {
		if authClassCodes[r.Code] {
			return "", 0, &AuthClassError{Code: r.Code, Msg: r.Msg}
		}
		return "", 0, fmt.Errorf("feishu tenant_access_token: code=%d msg=%s", r.Code, r.Msg)
	}
	if r.TenantAccessToken == "" {
		return "", 0, fmt.Errorf("feishu tenant_access_token: empty token")
	}
	return r.TenantAccessToken, r.Expire, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go mod tidy
go test ./internal/feishu/... -v -run TestTokenCache
```

Expected: 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/token.go internal/feishu/token_test.go go.mod go.sum
git commit -m "feat(feishu): TenantTokenCache with singleflight + auth-class error detection"
```

---

## Task 6: `feishu/card` — interactive card renderers

**Files:**
- Create: `internal/feishu/card.go`
- Create: `internal/feishu/card_test.go`

- [ ] **Step 1: Write the test**

```go
// internal/feishu/card_test.go
package feishu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestRenderCommandFinishedCard_Success(t *testing.T) {
	sid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	card := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: sid,
		ExitCode:  0,
		ElapsedMS: 2500,
		Label:     "go test",
	})
	s := mustJSON(t, card)
	for _, want := range []string{`"interactive"`, `"green"`, `"go test"`, "atterm://session/" + sid.String(), `"kind":"ack"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in: %s", want, s)
		}
	}
}

func TestRenderCommandFinishedCard_Failure(t *testing.T) {
	card := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: uuid.New(),
		ExitCode:  1,
		ElapsedMS: 60500,
		Label:     "make build",
	})
	s := mustJSON(t, card)
	if !strings.Contains(s, `"red"`) {
		t.Fatalf("non-zero exit should color the header red: %s", s)
	}
	if !strings.Contains(s, "1m00s") {
		t.Fatalf("elapsed should render as 1m00s; got %s", s)
	}
}

func TestRenderCommandFinishedCard_Sealed(t *testing.T) {
	card := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID:  uuid.New(),
		SealedBody: []byte{0xAA, 0xBB}, // any non-empty
	})
	s := mustJSON(t, card)
	if !strings.Contains(s, "仅本机可见") {
		t.Fatalf("sealed variant should include 仅本机可见; got %s", s)
	}
	// MUST NOT leak exit_code/label values in sealed variant.
	if strings.Contains(s, `"exit"`) || strings.Contains(s, "make build") {
		t.Fatalf("sealed variant must not include plaintext fields: %s", s)
	}
}

func TestRenderWaitingInputCard(t *testing.T) {
	sid := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	card := RenderWaitingInputCard(WaitingInputInput{
		SessionID:      sid,
		IdleForSeconds: 42,
	})
	s := mustJSON(t, card)
	for _, want := range []string{`"orange"`, "42", "atterm://session/" + sid.String(), "waiting_input"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in: %s", want, s)
		}
	}
}

func TestRenderAckUpdateCard(t *testing.T) {
	out := RenderAckUpdateCard(AckUpdateInput{
		Event:     "command_finished",
		SessionID: uuid.MustParse("00000000-0000-0000-0000-000000000003").String(),
	})
	s := mustJSON(t, out)
	for _, want := range []string{`"update_multi":true`, `"toast"`, "已确认", `"grey"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in: %s", want, s)
		}
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/feishu/... -run TestRender`
Expected: undefined types/funcs failure.

- [ ] **Step 3: Implement `card.go`**

```go
// internal/feishu/card.go
package feishu

import (
	"fmt"

	"github.com/google/uuid"
)

// CommandFinishedInput mirrors the relevant fields from
// internal/webhook/CommandFinished (and webpush's). When SealedBody is
// non-empty we render the E2EE-safe variant — no exit code, no label,
// no elapsed, only a generic "see your device" body.
type CommandFinishedInput struct {
	SessionID  uuid.UUID
	ExitCode   int
	ElapsedMS  int
	Label      string
	SealedBody []byte
}

func (in CommandFinishedInput) sealed() bool { return len(in.SealedBody) > 0 }

type WaitingInputInput struct {
	SessionID      uuid.UUID
	IdleForSeconds int
}

type AckUpdateInput struct {
	Event     string // "command_finished" or "waiting_input"
	SessionID string
}

// Card is the JSON message body we POST to im/v1/messages — wrapped
// in {msg_type:"interactive", card:{...}}.
type Card struct {
	MsgType string         `json:"msg_type"`
	Card    map[string]any `json:"card"`
}

// AckResponse is the inline reply for a card.action.trigger callback —
// Feishu reads the body and updates the original card if `card` is set.
type AckResponse struct {
	Toast map[string]any `json:"toast,omitempty"`
	Card  map[string]any `json:"card"`
}

func deepLink(sessionID uuid.UUID) string {
	return "atterm://session/" + sessionID.String()
}

func formatElapsed(ms int) string {
	if ms < 60_000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	totalSec := ms / 1000
	return fmt.Sprintf("%dm%02ds", totalSec/60, totalSec%60)
}

func RenderCommandFinishedCard(in CommandFinishedInput) Card {
	if in.sealed() {
		return Card{
			MsgType: "interactive",
			Card: map[string]any{
				"config": map[string]any{"wide_screen_mode": true},
				"header": map[string]any{
					"title":    map[string]any{"tag": "plain_text", "content": "命令完成（仅本机可见）"},
					"template": "grey",
				},
				"elements": []any{
					map[string]any{
						"tag":  "div",
						"text": map[string]any{"tag": "lark_md", "content": "命令详情仅本机可见 · 用本机端打开查看"},
					},
					actionRow(in.SessionID, "command_finished"),
				},
			},
		}
	}
	template := "green"
	if in.ExitCode != 0 {
		template = "red"
	}
	label := in.Label
	if label == "" {
		label = "command"
	}
	return Card{
		MsgType: "interactive",
		Card: map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "命令完成"},
				"template": template,
			},
			"elements": []any{
				map[string]any{
					"tag": "div",
					"text": map[string]any{
						"tag":     "lark_md",
						"content": fmt.Sprintf("**`%s`** 退出码 `%d` · 用时 %s", label, in.ExitCode, formatElapsed(in.ElapsedMS)),
					},
				},
				actionRow(in.SessionID, "command_finished"),
			},
		},
	}
}

func RenderWaitingInputCard(in WaitingInputInput) Card {
	return Card{
		MsgType: "interactive",
		Card: map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Session 等待输入"},
				"template": "orange",
			},
			"elements": []any{
				map[string]any{
					"tag": "div",
					"text": map[string]any{
						"tag":     "lark_md",
						"content": fmt.Sprintf("Agent 在等待你回复（已闲置 %ds）", in.IdleForSeconds),
					},
				},
				actionRow(in.SessionID, "waiting_input"),
			},
		},
	}
}

func actionRow(sessionID uuid.UUID, event string) map[string]any {
	return map[string]any{
		"tag": "action",
		"actions": []any{
			map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "跳回打开 session"},
				"type": "primary",
				"url":  deepLink(sessionID),
			},
			map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "确认"},
				"type": "default",
				"value": map[string]any{
					"kind":       "ack",
					"session_id": sessionID.String(),
					"event":      event,
				},
			},
		},
	}
}

func RenderAckUpdateCard(in AckUpdateInput) AckResponse {
	shortID := in.SessionID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return AckResponse{
		Toast: map[string]any{"type": "success", "content": "已确认"},
		Card: map[string]any{
			"config": map[string]any{"update_multi": true, "wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": fmt.Sprintf("已确认（%s #%s）", in.Event, shortID)},
				"template": "grey",
			},
			"elements": []any{
				map[string]any{
					"tag":  "div",
					"text": map[string]any{"tag": "plain_text", "content": "你已在飞书确认此事件。"},
				},
			},
		},
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/feishu/... -run TestRender -v`
Expected: 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/card.go internal/feishu/card_test.go
git commit -m "feat(feishu): interactive card renderers — command_finished, waiting_input, ack-update"
```

---

## Task 7: `feishu/event` — decrypt + verify + envelope parsing

**Files:**
- Create: `internal/feishu/event.go`
- Create: `internal/feishu/event_test.go`

- [ ] **Step 1: Write the test**

```go
// internal/feishu/event_test.go
package feishu

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// feishuTestEncrypt mirrors the Feishu encryption scheme so tests can
// craft ciphertext to feed DecryptEnvelope.
//   key = SHA256(encryptKey)
//   iv  = random 16 bytes
//   ct  = iv || AES-256-CBC(PKCS7(plain))
//   wrap{ "encrypt": base64(ct) }
func feishuTestEncrypt(t *testing.T, encryptKey string, plain []byte) []byte {
	t.Helper()
	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	iv := bytes.Repeat([]byte{0x42}, aes.BlockSize) // deterministic for test
	padLen := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append([]byte{}, plain...)
	for i := 0; i < padLen; i++ {
		padded = append(padded, byte(padLen))
	}
	ct := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ct, padded)
	combined := append(append([]byte{}, iv...), ct...)
	body, _ := json.Marshal(map[string]string{"encrypt": base64.StdEncoding.EncodeToString(combined)})
	return body
}

func TestDecryptEnvelope_HappyPath(t *testing.T) {
	plain := []byte(`{"header":{"event_type":"im.message.receive_v1","token":"vtok"},"event":{}}`)
	body := feishuTestEncrypt(t, "my-encrypt-key", plain)
	got, err := DecryptEnvelope(body, "my-encrypt-key")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("decrypted mismatch: %s vs %s", string(got), string(plain))
	}
}

func TestDecryptEnvelope_WrongKey(t *testing.T) {
	body := feishuTestEncrypt(t, "key-A", []byte(`{"x":1}`))
	if _, err := DecryptEnvelope(body, "key-B"); err == nil {
		t.Fatalf("expected decrypt failure with wrong key")
	}
}

func TestDecryptEnvelope_NoEncryptField(t *testing.T) {
	if _, err := DecryptEnvelope([]byte(`{"hello":"world"}`), "k"); !errors.Is(err, ErrNotEncryptedBody) {
		t.Fatalf("want ErrNotEncryptedBody, got %v", err)
	}
}

func TestParseEnvelope_URLVerification(t *testing.T) {
	plain := []byte(`{"type":"url_verification","challenge":"abc123","token":"vtok"}`)
	env, err := ParseEnvelope(plain)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env.URLVerification == nil {
		t.Fatalf("expected url_verification")
	}
	if env.URLVerification.Challenge != "abc123" || env.URLVerification.Token != "vtok" {
		t.Fatalf("fields: %+v", env.URLVerification)
	}
}

func TestParseEnvelope_MessageReceive(t *testing.T) {
	plain := []byte(`{
	  "header":{
	    "event_type":"im.message.receive_v1",
	    "token":"vtok",
	    "app_id":"cli_abc"
	  },
	  "event":{
	    "sender":{"sender_id":{"open_id":"ou_xyz"}},
	    "message":{"content":"{\"text\":\"/bind AB3CD7\"}","message_type":"text"}
	  }
	}`)
	env, err := ParseEnvelope(plain)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env.Header == nil || env.Header.EventType != "im.message.receive_v1" {
		t.Fatalf("header: %+v", env.Header)
	}
	if env.Message == nil || env.Message.SenderOpenID != "ou_xyz" {
		t.Fatalf("message: %+v", env.Message)
	}
	if env.Message.Text != "/bind AB3CD7" {
		t.Fatalf("text: %q", env.Message.Text)
	}
}

func TestParseEnvelope_CardAction(t *testing.T) {
	plain := []byte(`{
	  "header":{"event_type":"card.action.trigger","token":"vtok","app_id":"cli_abc"},
	  "event":{
	    "action":{"value":{"kind":"ack","session_id":"sid-1","event":"command_finished"}},
	    "operator":{"open_id":"ou_xyz"}
	  }
	}`)
	env, err := ParseEnvelope(plain)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env.CardAction == nil {
		t.Fatalf("expected card action")
	}
	if env.CardAction.Kind != "ack" || env.CardAction.SessionID != "sid-1" || env.CardAction.Event != "command_finished" {
		t.Fatalf("action fields: %+v", env.CardAction)
	}
}

func TestVerifyEnvelopeToken(t *testing.T) {
	env := &Envelope{Header: &EnvelopeHeader{Token: "vtok"}}
	if err := VerifyEnvelopeToken(env, "vtok"); err != nil {
		t.Fatalf("match should pass: %v", err)
	}
	if err := VerifyEnvelopeToken(env, "other"); !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("mismatch should ErrTokenMismatch, got %v", err)
	}
	if err := VerifyEnvelopeToken(&Envelope{URLVerification: &URLVerification{Token: "vtok"}}, "vtok"); err != nil {
		t.Fatalf("url_verification token should pass: %v", err)
	}
}

func TestParseEnvelope_BadJSON(t *testing.T) {
	if _, err := ParseEnvelope([]byte("not-json")); err == nil || !strings.Contains(err.Error(), "parse envelope") {
		t.Fatalf("want parse error, got %v", err)
	}
}
```

- [ ] **Step 2: Confirm failure**

Run: `go test ./internal/feishu/... -run "TestDecrypt|TestParseEnvelope|TestVerifyEnvelopeToken"`
Expected: build failure.

- [ ] **Step 3: Implement `event.go`**

```go
// internal/feishu/event.go
package feishu

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrNotEncryptedBody is returned by DecryptEnvelope when the request
// body lacks the top-level `encrypt` string. The relay rejects such
// requests because our spec mandates the user enable event encryption.
var ErrNotEncryptedBody = errors.New("feishu: body is not encrypted (no `encrypt` field)")

// ErrTokenMismatch indicates the envelope's verify token did not match
// the binding's stored verify_token.
var ErrTokenMismatch = errors.New("feishu: envelope verify-token mismatch")

// outerEncrypted is the shape Feishu sends for encrypted events.
type outerEncrypted struct {
	Encrypt string `json:"encrypt"`
}

// DecryptEnvelope decrypts the outer body using Feishu's documented
// scheme: AES-256-CBC with key=SHA256(encryptKey), IV=ciphertext[:16],
// data=ciphertext[16:], PKCS7-padded.
func DecryptEnvelope(body []byte, encryptKey string) ([]byte, error) {
	var outer outerEncrypted
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, fmt.Errorf("feishu: parse outer body: %w", err)
	}
	if outer.Encrypt == "" {
		return nil, ErrNotEncryptedBody
	}
	raw, err := base64.StdEncoding.DecodeString(outer.Encrypt)
	if err != nil {
		return nil, fmt.Errorf("feishu: base64 decode: %w", err)
	}
	if len(raw) < aes.BlockSize+aes.BlockSize {
		return nil, fmt.Errorf("feishu: ciphertext too short")
	}
	keyHash := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return nil, fmt.Errorf("feishu: aes cipher: %w", err)
	}
	iv, ct := raw[:aes.BlockSize], raw[aes.BlockSize:]
	if len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("feishu: ciphertext not block-aligned")
	}
	plain := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)
	// PKCS7 unpad
	if len(plain) == 0 {
		return nil, fmt.Errorf("feishu: empty plaintext")
	}
	padLen := int(plain[len(plain)-1])
	if padLen == 0 || padLen > aes.BlockSize || padLen > len(plain) {
		return nil, fmt.Errorf("feishu: invalid padding")
	}
	for i := len(plain) - padLen; i < len(plain); i++ {
		if int(plain[i]) != padLen {
			return nil, fmt.Errorf("feishu: invalid padding bytes")
		}
	}
	return plain[:len(plain)-padLen], nil
}

// Envelope is the parsed plaintext we operate on after decryption.
// At most one of (URLVerification, Header+Event-class fields) is set.
type Envelope struct {
	URLVerification *URLVerification
	Header          *EnvelopeHeader
	Message         *MessageReceive
	CardAction      *CardActionTrigger
}

type URLVerification struct {
	Challenge string `json:"challenge"`
	Token     string `json:"token"`
}

type EnvelopeHeader struct {
	EventType string `json:"event_type"`
	Token     string `json:"token"`
	AppID     string `json:"app_id"`
}

type MessageReceive struct {
	SenderOpenID string
	Text         string
}

type CardActionTrigger struct {
	OperatorOpenID string
	Kind           string
	SessionID      string
	Event          string
}

// ParseEnvelope inspects plaintext (already decrypted) and routes it to
// one of the typed sub-payloads. Unknown event_types parse the header
// only; callers can ignore them.
func ParseEnvelope(plaintext []byte) (*Envelope, error) {
	var probe struct {
		Type            string `json:"type"`
		Challenge       string `json:"challenge"`
		Token           string `json:"token"`
		Header          *EnvelopeHeader `json:"header"`
		Event           json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(plaintext, &probe); err != nil {
		return nil, fmt.Errorf("feishu: parse envelope: %w", err)
	}
	env := &Envelope{}
	if probe.Type == "url_verification" {
		env.URLVerification = &URLVerification{Challenge: probe.Challenge, Token: probe.Token}
		return env, nil
	}
	env.Header = probe.Header
	if probe.Header == nil {
		return env, nil
	}
	switch probe.Header.EventType {
	case "im.message.receive_v1":
		var ev struct {
			Sender  struct {
				SenderID struct {
					OpenID string `json:"open_id"`
				} `json:"sender_id"`
			} `json:"sender"`
			Message struct {
				Content     string `json:"content"`
				MessageType string `json:"message_type"`
			} `json:"message"`
		}
		if err := json.Unmarshal(probe.Event, &ev); err != nil {
			return nil, fmt.Errorf("feishu: parse im.message: %w", err)
		}
		// content is itself JSON string like {"text":"..."}
		var inner struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal([]byte(ev.Message.Content), &inner)
		env.Message = &MessageReceive{
			SenderOpenID: ev.Sender.SenderID.OpenID,
			Text:         inner.Text,
		}
	case "card.action.trigger":
		var ev struct {
			Action struct {
				Value struct {
					Kind      string `json:"kind"`
					SessionID string `json:"session_id"`
					Event     string `json:"event"`
				} `json:"value"`
			} `json:"action"`
			Operator struct {
				OpenID string `json:"open_id"`
			} `json:"operator"`
		}
		if err := json.Unmarshal(probe.Event, &ev); err != nil {
			return nil, fmt.Errorf("feishu: parse card.action: %w", err)
		}
		env.CardAction = &CardActionTrigger{
			OperatorOpenID: ev.Operator.OpenID,
			Kind:           ev.Action.Value.Kind,
			SessionID:      ev.Action.Value.SessionID,
			Event:          ev.Action.Value.Event,
		}
	}
	return env, nil
}

// VerifyEnvelopeToken asserts that the envelope's token (either from
// header for events or from url_verification) matches the binding's
// verify_token. Constant-time compare to avoid timing oracles.
func VerifyEnvelopeToken(env *Envelope, verifyToken string) error {
	var have string
	switch {
	case env.URLVerification != nil:
		have = env.URLVerification.Token
	case env.Header != nil:
		have = env.Header.Token
	default:
		return ErrTokenMismatch
	}
	if !constantTimeEq(have, verifyToken) {
		return ErrTokenMismatch
	}
	return nil
}

func constantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/feishu/... -run "TestDecrypt|TestParseEnvelope|TestVerifyEnvelopeToken" -v`
Expected: 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/event.go internal/feishu/event_test.go
git commit -m "feat(feishu): event decrypt + envelope parsing + verify-token check"
```

---

## Task 8: `feishu/client` — IM send HTTP client

**Files:**
- Create: `internal/feishu/client.go`
- Create: `internal/feishu/client_test.go`

- [ ] **Step 1: Write the test**

```go
// internal/feishu/client_test.go
package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_SendInteractiveToOpenID(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"message_id":"om_xxx"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.SendInteractiveToOpenID(context.Background(), "tt_token", "ou_dest", []byte(`{"msg_type":"interactive","card":{"x":1}}`)); err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.HasPrefix(gotPath, "/open-apis/im/v1/messages?") {
		t.Fatalf("path: %s", gotPath)
	}
	if !strings.Contains(gotPath, "receive_id_type=open_id") {
		t.Fatalf("missing receive_id_type query: %s", gotPath)
	}
	if gotAuth != "Bearer tt_token" {
		t.Fatalf("auth header: %q", gotAuth)
	}
	if gotBody["receive_id"] != "ou_dest" || gotBody["msg_type"] != "interactive" {
		t.Fatalf("body: %+v", gotBody)
	}
}

func TestClient_SendInteractive_FeishuError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":99991663,"msg":"invalid access_token"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, srv.Client())
	err := c.SendInteractiveToOpenID(context.Background(), "tt", "ou", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsAuthClassError(err) {
		t.Fatalf("expected auth-class, got %v", err)
	}
}

func TestClient_SendText(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, srv.Client())
	if err := c.SendTextToOpenID(context.Background(), "tt", "ou_x", "hello"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	if gotBody["msg_type"] != "text" {
		t.Fatalf("msg_type: %v", gotBody["msg_type"])
	}
	content, _ := gotBody["content"].(string)
	if !strings.Contains(content, "hello") {
		t.Fatalf("content: %s", content)
	}
}
```

- [ ] **Step 2: Confirm failure**

Run: `go test ./internal/feishu/... -run TestClient`
Expected: undefined `NewClient`.

- [ ] **Step 3: Implement `client.go`**

```go
// internal/feishu/client.go
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client wraps Feishu's IM API. One client per relay process, used by
// feishu.Service to actually POST cards.
type Client struct {
	baseURL string
	httpC   *http.Client
}

func NewClient(baseURL string, httpC *http.Client) *Client {
	if httpC == nil {
		httpC = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), httpC: httpC}
}

// SendInteractiveToOpenID posts an interactive card to a single open_id.
// cardBody must be a JSON object with at least {"msg_type":"interactive","card":...}.
// The function wraps it with receive_id + msg_type + content per
// Feishu's im/v1/messages contract.
func (c *Client) SendInteractiveToOpenID(ctx context.Context, tenantToken, openID string, cardBody []byte) error {
	// The Feishu API expects:
	//   { receive_id, msg_type:"interactive", content: <stringified card JSON> }
	// We unmarshal cardBody to extract the card sub-object so it can be
	// re-marshaled into the `content` field as a string.
	var c0 struct {
		Card json.RawMessage `json:"card"`
	}
	if err := json.Unmarshal(cardBody, &c0); err != nil {
		return fmt.Errorf("parse card body: %w", err)
	}
	wrapper := map[string]any{
		"receive_id": openID,
		"msg_type":   "interactive",
		"content":    string(c0.Card),
	}
	return c.postIM(ctx, tenantToken, wrapper)
}

func (c *Client) SendTextToOpenID(ctx context.Context, tenantToken, openID, text string) error {
	content, _ := json.Marshal(map[string]string{"text": text})
	wrapper := map[string]any{
		"receive_id": openID,
		"msg_type":   "text",
		"content":    string(content),
	}
	return c.postIM(ctx, tenantToken, wrapper)
}

func (c *Client) postIM(ctx context.Context, tenantToken string, wrapper map[string]any) error {
	body, _ := json.Marshal(wrapper)
	req, _ := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/open-apis/im/v1/messages?receive_id_type=open_id",
		bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tenantToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpC.Do(req)
	if err != nil {
		return fmt.Errorf("im POST: %w", err)
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
		return fmt.Errorf("feishu im send: code=%d msg=%s", r.Code, r.Msg)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/feishu/... -run TestClient -v`
Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/client.go internal/feishu/client_test.go
git commit -m "feat(feishu): IM client — SendInteractiveToOpenID + SendTextToOpenID"
```

---

## Task 9: `feishu/service` — aggregate Send + Handle

**Files:**
- Create: `internal/feishu/service.go`
- Create: `internal/feishu/service_test.go`

- [ ] **Step 1: Write the test**

```go
// internal/feishu/service_test.go
package feishu

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// fakeStore implements the BindingStore interface for service tests.
type fakeStore struct {
	mu          sync.Mutex
	byHash      map[string]*Binding
	byUser      map[string]*Binding
	pending     map[string]string // code -> user_id
	boundCalls  []string          // user_ids passed to MarkBound
	disabled    map[string]bool
	bindError   error
	tokenAuthFail int
}

func newFakeStore() *fakeStore {
	return &fakeStore{byHash: map[string]*Binding{}, byUser: map[string]*Binding{}, pending: map[string]string{}, disabled: map[string]bool{}}
}

func (f *fakeStore) addBinding(b *Binding) {
	f.byHash[b.AppIDHash] = b
	f.byUser[b.UserID] = b
}

func (f *fakeStore) GetBindingByAppIDHash(ctx context.Context, hash string) (*Binding, error) {
	if b, ok := f.byHash[hash]; ok {
		return b, nil
	}
	return nil, ErrBindingNotFound
}
func (f *fakeStore) GetBindingByUserID(ctx context.Context, userID string) (*Binding, error) {
	if b, ok := f.byUser[userID]; ok {
		return b, nil
	}
	return nil, ErrBindingNotFound
}
func (f *fakeStore) MarkBound(ctx context.Context, userID, openID string) error {
	f.boundCalls = append(f.boundCalls, userID)
	if b, ok := f.byUser[userID]; ok {
		b.OpenID = openID
	}
	return nil
}
func (f *fakeStore) MarkDisabled(ctx context.Context, userID string) error {
	f.disabled[userID] = true
	return nil
}
func (f *fakeStore) ConsumePendingBind(ctx context.Context, code string) (string, error) {
	if u, ok := f.pending[code]; ok {
		delete(f.pending, code)
		return u, nil
	}
	return "", ErrFeishuPendingBindNotFoundService
}

// fakeIM captures Send calls.
type fakeIM struct {
	mu sync.Mutex
	sentInteractive []struct {
		Token, OpenID string
		Body          []byte
	}
	sentText []struct{ Token, OpenID, Text string }
	err      error
	authFail bool
}

func (f *fakeIM) SendInteractiveToOpenID(ctx context.Context, token, openID string, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.authFail {
		return &AuthClassError{Code: 99991663, Msg: "fake"}
	}
	if f.err != nil {
		return f.err
	}
	f.sentInteractive = append(f.sentInteractive, struct{ Token, OpenID string; Body []byte }{token, openID, body})
	return nil
}
func (f *fakeIM) SendTextToOpenID(ctx context.Context, token, openID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentText = append(f.sentText, struct{ Token, OpenID, Text string }{token, openID, text})
	return nil
}

// fakeToken always returns a fixed token; used in service tests.
type fakeToken struct{ tok string; err error }

func (f *fakeToken) Get(ctx context.Context, appID, secret string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.tok, nil
}
func (f *fakeToken) Invalidate(string) {}

func newSvc(store BindingStore, im IMClient, tok TokenSource) *Service {
	return NewService(ServiceConfig{Store: store, IM: im, Token: tok})
}

func TestService_SendCommandFinished_NoBinding(t *testing.T) {
	st := newFakeStore()
	im := &fakeIM{}
	s := newSvc(st, im, &fakeToken{tok: "tt"})
	s.SendCommandFinished(context.Background(), "user-missing", CommandFinishedInput{SessionID: uuid.New()})
	if len(im.sentInteractive) != 0 {
		t.Fatalf("should not send for missing binding")
	}
}

func TestService_SendCommandFinished_HappyPath(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v", AppIDHash: "h", OpenID: "ou_x"})
	im := &fakeIM{}
	s := newSvc(st, im, &fakeToken{tok: "tt"})
	// Sync variant for deterministic tests; production callers use the
	// goroutined SendCommandFinished wrapper.
	s.sendCommandFinishedSync(context.Background(), "u1", CommandFinishedInput{SessionID: uuid.New(), ExitCode: 0, Label: "x"})
	if len(im.sentInteractive) != 1 {
		t.Fatalf("expected 1 send, got %d", len(im.sentInteractive))
	}
	got := im.sentInteractive[0]
	if got.Token != "tt" || got.OpenID != "ou_x" {
		t.Fatalf("send args: %+v", got)
	}
}

func TestService_SendCommandFinished_SkipsDisabled(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", OpenID: "ou_x", AppIDHash: "h", DisabledAt: 1})
	im := &fakeIM{}
	s := newSvc(st, im, &fakeToken{tok: "tt"})
	s.sendCommandFinishedSync(context.Background(), "u1", CommandFinishedInput{SessionID: uuid.New()})
	if len(im.sentInteractive) != 0 {
		t.Fatalf("disabled binding should be skipped")
	}
}

func TestService_HandleEvent_BindMessageHappy(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", AppID: "a", AppSecret: "s", EncryptKey: "kk", VerifyToken: "vv", AppIDHash: "hash1", OpenID: ""})
	st.pending["AB3CD7"] = "u1"
	im := &fakeIM{}
	svc := newSvc(st, im, &fakeToken{tok: "tt"})

	// Build encrypted body with this binding's encrypt_key.
	plain := []byte(`{"header":{"event_type":"im.message.receive_v1","token":"vv","app_id":"a"},"event":{"sender":{"sender_id":{"open_id":"ou_user1"}},"message":{"content":"{\"text\":\"/bind AB3CD7\"}","message_type":"text"}}}`)
	body := feishuTestEncrypt(t, "kk", plain)

	resp, err := svc.HandleEvent(context.Background(), "hash1", body)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if resp.Status != HandleStatusOK {
		t.Fatalf("status: %v", resp.Status)
	}
	if len(st.boundCalls) != 1 || st.boundCalls[0] != "u1" {
		t.Fatalf("MarkBound calls: %v", st.boundCalls)
	}
	// And a confirmation text should have been sent.
	if len(im.sentText) != 1 {
		t.Fatalf("expected 1 confirmation text, got %d", len(im.sentText))
	}
}

func TestService_HandleEvent_UnknownHash(t *testing.T) {
	st := newFakeStore()
	svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})
	resp, err := svc.HandleEvent(context.Background(), "no-such-hash", []byte(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Status != HandleStatusOK || resp.Reason != "unknown_app_id_hash" {
		t.Fatalf("resp: %+v", resp)
	}
}

func TestService_HandleEvent_URLVerification(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", EncryptKey: "kk", VerifyToken: "vv", AppIDHash: "hash1"})
	svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})

	plain := []byte(`{"type":"url_verification","challenge":"abc","token":"vv"}`)
	body := feishuTestEncrypt(t, "kk", plain)
	resp, err := svc.HandleEvent(context.Background(), "hash1", body)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if resp.URLChallenge != "abc" {
		t.Fatalf("challenge: %q", resp.URLChallenge)
	}
}

func TestService_HandleEvent_CardAck(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", EncryptKey: "kk", VerifyToken: "vv", AppIDHash: "h"})
	svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})

	plain := []byte(`{"header":{"event_type":"card.action.trigger","token":"vv","app_id":"a"},"event":{"action":{"value":{"kind":"ack","session_id":"sid-1","event":"command_finished"}},"operator":{"open_id":"ou_x"}}}`)
	body := feishuTestEncrypt(t, "kk", plain)
	resp, err := svc.HandleEvent(context.Background(), "h", body)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if resp.CardUpdate == nil {
		t.Fatalf("expected card update response")
	}
}

func TestService_HandleEvent_DecryptFailure(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", EncryptKey: "right-key", VerifyToken: "vv", AppIDHash: "h"})
	svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})
	body := feishuTestEncrypt(t, "wrong-key", []byte(`{}`))
	resp, err := svc.HandleEvent(context.Background(), "h", body)
	if err != nil {
		t.Fatalf("handle should not return err (must reply 200): %v", err)
	}
	if resp.Status != HandleStatusOK || !errors.Is(resp.LogError, ErrDecryptFailed) {
		t.Fatalf("resp: %+v", resp)
	}
}
```

- [ ] **Step 2: Confirm failure**

Run: `go test ./internal/feishu/... -run TestService`
Expected: undefined symbols.

- [ ] **Step 3: Implement `service.go`**

```go
// internal/feishu/service.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
)

// Binding is the service's view of a feishu binding (subset of the
// userstore row — only what the service needs).
type Binding struct {
	UserID      string
	AppID       string
	AppSecret   string
	EncryptKey  string
	VerifyToken string
	AppIDHash   string
	OpenID      string
	DisabledAt  int64
}

// ErrBindingNotFound is the sentinel the BindingStore returns when no
// row matches.
var ErrBindingNotFound = errors.New("feishu: binding not found")

// ErrFeishuPendingBindNotFoundService is the sentinel returned by
// ConsumePendingBind. Renamed to avoid colliding with the userstore
// symbol while keeping the meaning crystal-clear at this layer.
var ErrFeishuPendingBindNotFoundService = errors.New("feishu: pending bind not found")

// ErrDecryptFailed wraps any decryption-side error.
var ErrDecryptFailed = errors.New("feishu: decrypt failed")

// BindingStore is what the service needs from userstore.
type BindingStore interface {
	GetBindingByAppIDHash(ctx context.Context, hash string) (*Binding, error)
	GetBindingByUserID(ctx context.Context, userID string) (*Binding, error)
	MarkBound(ctx context.Context, userID, openID string) error
	MarkDisabled(ctx context.Context, userID string) error
	ConsumePendingBind(ctx context.Context, code string) (string, error)
}

// IMClient is what the service needs from the HTTP client.
type IMClient interface {
	SendInteractiveToOpenID(ctx context.Context, token, openID string, cardBody []byte) error
	SendTextToOpenID(ctx context.Context, token, openID, text string) error
}

// TokenSource is what the service needs from TenantTokenCache.
type TokenSource interface {
	Get(ctx context.Context, appID, secret string) (string, error)
	Invalidate(appID string)
}

// ServiceConfig groups the moving parts.
type ServiceConfig struct {
	Store BindingStore
	IM    IMClient
	Token TokenSource
}

// Service is the aggregate layer. Methods are safe for concurrent use.
type Service struct {
	cfg          ServiceConfig
	authFailMu   chan struct{} // semaphore-of-1 for the auth-fail counter map
	authFailures map[string]int
}

func NewService(cfg ServiceConfig) *Service {
	return &Service{
		cfg:          cfg,
		authFailMu:   make(chan struct{}, 1),
		authFailures: map[string]int{},
	}
}

// SendCommandFinished spawns a goroutine that renders the card and
// posts it. Always fire-and-forget; any error is logged. Tests should
// call sendCommandFinishedSync to avoid timing races.
func (s *Service) SendCommandFinished(ctx context.Context, userID string, in CommandFinishedInput) {
	go s.sendCommandFinishedSync(ctx, userID, in)
}

func (s *Service) sendCommandFinishedSync(ctx context.Context, userID string, in CommandFinishedInput) {
	s.send(ctx, userID, func() ([]byte, error) {
		card := RenderCommandFinishedCard(in)
		return json.Marshal(card)
	})
}

// SendSessionNotification mirrors SendCommandFinished for the
// waiting_input event. Same fire-and-forget semantics.
func (s *Service) SendSessionNotification(ctx context.Context, userID string, in WaitingInputInput) {
	go s.sendSessionNotificationSync(ctx, userID, in)
}

func (s *Service) sendSessionNotificationSync(ctx context.Context, userID string, in WaitingInputInput) {
	s.send(ctx, userID, func() ([]byte, error) {
		card := RenderWaitingInputCard(in)
		return json.Marshal(card)
	})
}

func (s *Service) send(ctx context.Context, userID string, render func() ([]byte, error)) {
	b, err := s.cfg.Store.GetBindingByUserID(ctx, userID)
	if errors.Is(err, ErrBindingNotFound) {
		return
	}
	if err != nil {
		log.Printf("feishu: lookup binding for %s: %v", userID, err)
		return
	}
	if b.OpenID == "" || b.DisabledAt != 0 {
		return
	}
	cardBody, err := render()
	if err != nil {
		log.Printf("feishu: render card: %v", err)
		return
	}
	tok, err := s.cfg.Token.Get(ctx, b.AppID, b.AppSecret)
	if err != nil {
		s.recordSendError(ctx, b, err)
		return
	}
	if err := s.cfg.IM.SendInteractiveToOpenID(ctx, tok, b.OpenID, cardBody); err != nil {
		s.recordSendError(ctx, b, err)
	}
}

// recordSendError counts consecutive auth-class failures per binding;
// 3-in-a-row → mark disabled.
func (s *Service) recordSendError(ctx context.Context, b *Binding, err error) {
	if !IsAuthClassError(err) {
		log.Printf("feishu: send to %s: %v", b.UserID, err)
		return
	}
	s.authFailMu <- struct{}{}
	s.authFailures[b.UserID]++
	count := s.authFailures[b.UserID]
	<-s.authFailMu
	log.Printf("feishu: auth failure #%d for %s: %v", count, b.UserID, err)
	if count >= 3 {
		if err := s.cfg.Store.MarkDisabled(ctx, b.UserID); err != nil {
			log.Printf("feishu: mark disabled: %v", err)
		}
		s.cfg.Token.Invalidate(b.AppID)
	}
}

// HandleStatus is the high-level result for the HTTP handler.
type HandleStatus int

const (
	HandleStatusOK HandleStatus = iota
)

// HandleResult is what HandleEvent returns; the HTTP handler always
// replies 200 but inspects fields to emit the right body.
type HandleResult struct {
	Status       HandleStatus
	Reason       string         // short tag for logs
	URLChallenge string         // non-empty → reply { "challenge": ... }
	CardUpdate   *AckResponse   // non-nil → reply JSON of this object
	LogError     error          // attached for tests; HTTP handler logs it
}

// HandleEvent is the inbound entry point. The HTTP handler calls this
// with the raw request body + the app_id_hash from the URL path.
//
// Contract: HandleEvent never returns an error that should produce a
// non-200 HTTP response. Decryption / verification failures are
// reported via HandleResult.LogError.
func (s *Service) HandleEvent(ctx context.Context, appIDHash string, body []byte) (*HandleResult, error) {
	b, err := s.cfg.Store.GetBindingByAppIDHash(ctx, appIDHash)
	if errors.Is(err, ErrBindingNotFound) {
		return &HandleResult{Reason: "unknown_app_id_hash"}, nil
	}
	if err != nil {
		return &HandleResult{Reason: "store_error", LogError: err}, nil
	}
	plain, err := DecryptEnvelope(body, b.EncryptKey)
	if err != nil {
		return &HandleResult{Reason: "decrypt_failed", LogError: fmt.Errorf("%w: %v", ErrDecryptFailed, err)}, nil
	}
	env, err := ParseEnvelope(plain)
	if err != nil {
		return &HandleResult{Reason: "parse_failed", LogError: err}, nil
	}
	if err := VerifyEnvelopeToken(env, b.VerifyToken); err != nil {
		return &HandleResult{Reason: "verify_token_mismatch", LogError: err}, nil
	}
	if env.URLVerification != nil {
		return &HandleResult{URLChallenge: env.URLVerification.Challenge}, nil
	}
	if env.Header == nil {
		return &HandleResult{Reason: "no_header"}, nil
	}
	switch env.Header.EventType {
	case "im.message.receive_v1":
		if env.Message == nil {
			return &HandleResult{Reason: "no_message"}, nil
		}
		s.handleBindMessage(ctx, b, env.Message)
		return &HandleResult{Reason: "im_message_dispatched"}, nil
	case "card.action.trigger":
		if env.CardAction == nil || env.CardAction.Kind != "ack" {
			return &HandleResult{Reason: "ignored_card_action"}, nil
		}
		ack := RenderAckUpdateCard(AckUpdateInput{Event: env.CardAction.Event, SessionID: env.CardAction.SessionID})
		return &HandleResult{CardUpdate: &ack, Reason: "card_ack"}, nil
	default:
		return &HandleResult{Reason: "ignored_event_type"}, nil
	}
}

// handleBindMessage processes "/bind <CODE>" text messages. Async-safe;
// HTTP handler does NOT wait on this.
func (s *Service) handleBindMessage(ctx context.Context, b *Binding, msg *MessageReceive) {
	text := strings.TrimSpace(msg.Text)
	if !strings.HasPrefix(text, "/bind ") {
		return
	}
	code := strings.TrimSpace(strings.TrimPrefix(text, "/bind "))
	uid, err := s.cfg.Store.ConsumePendingBind(ctx, code)
	if err != nil {
		log.Printf("feishu: consume pending bind: %v", err)
		s.sendBindReply(ctx, b, msg.SenderOpenID, "❌ 短码无效或已过期")
		return
	}
	if uid != b.UserID {
		// The code belongs to a different atterm user — should not happen
		// because the binding is per-user, but guard.
		log.Printf("feishu: pending bind user mismatch: %s vs %s", uid, b.UserID)
		s.sendBindReply(ctx, b, msg.SenderOpenID, "❌ 短码无效或已过期")
		return
	}
	if err := s.cfg.Store.MarkBound(ctx, b.UserID, msg.SenderOpenID); err != nil {
		log.Printf("feishu: mark bound: %v", err)
		s.sendBindReply(ctx, b, msg.SenderOpenID, "❌ 服务端错误,请稍后再试")
		return
	}
	s.sendBindReply(ctx, b, msg.SenderOpenID, "✅ 已绑定到 atterm")
}

func (s *Service) sendBindReply(ctx context.Context, b *Binding, openID, text string) {
	tok, err := s.cfg.Token.Get(ctx, b.AppID, b.AppSecret)
	if err != nil {
		log.Printf("feishu: bind reply token: %v", err)
		return
	}
	if err := s.cfg.IM.SendTextToOpenID(ctx, tok, openID, text); err != nil {
		log.Printf("feishu: bind reply: %v", err)
	}
}

// guardUUID is a sanity helper for tests that mint UUIDs inline.
var _ = uuid.Nil
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/feishu/... -run TestService -v`
Expected: 7 tests PASS deterministically — production `SendCommandFinished` / `SendSessionNotification` stay goroutined; tests bypass that by calling the `sendCommandFinishedSync` / `sendSessionNotificationSync` siblings (same package, lower-case name = test-only access from outside the package, but service_test.go is in the same package so the call is plain).

- [ ] **Step 5: Commit**

```bash
git add internal/feishu/service.go internal/feishu/service_test.go
git commit -m "feat(feishu): Service aggregate — Send + HandleEvent + bind dispatch"
```

---

## Task 10: Webhook cleanup — remove legacy feishu format

**Files:**
- Modify: `internal/webhook/render.go`
- Modify: `internal/webhook/render_test.go`
- Modify: `internal/webhook/dispatch_test.go`
- Modify: `internal/userstore/webhooks.go` (doc comment only)

- [ ] **Step 1: Identify the surface**

```bash
grep -RIn 'renderFeishu\|format == "feishu"\|Format: "feishu"\|Format:    "feishu"' internal/
```

- [ ] **Step 2: Update `internal/webhook/render.go`** — delete `renderFeishu` and the feishu branch in `renderForFormat`:

```go
// internal/webhook/render.go (after edit)
package webhook

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// CommandFinished is the event shape, identical to webpush.CommandFinished
// so the dispatch site can construct both from the same decoded uplink
// payload.
type CommandFinished struct {
	SessionID  uuid.UUID
	HostID     uuid.UUID
	ExitCode   int
	ElapsedMS  int
	Label      string
	SealedBody []byte
}

func formatElapsed(ms int) string {
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	totalSec := ms / 1000
	return fmt.Sprintf("%dm%02ds", totalSec/60, totalSec%60)
}

func (ev CommandFinished) sealed() bool { return len(ev.SealedBody) > 0 }

func humanText(ev CommandFinished) string {
	if ev.sealed() {
		return "Session command finished"
	}
	label := ev.Label
	if label == "" {
		label = "command"
	}
	return fmt.Sprintf("%s finished (exit %d, %s)", label, ev.ExitCode, formatElapsed(ev.ElapsedMS))
}

func renderGeneric(ev CommandFinished) []byte {
	payload := map[string]any{
		"event":      "command_finished",
		"session_id": ev.SessionID.String(),
		"host_id":    ev.HostID.String(),
		"text":       humanText(ev),
	}
	if ev.sealed() {
		payload["sealed_body"] = base64.StdEncoding.EncodeToString(ev.SealedBody)
	} else {
		payload["exit_code"] = ev.ExitCode
		payload["elapsed_ms"] = ev.ElapsedMS
		payload["label"] = ev.Label
	}
	b, _ := json.Marshal(payload)
	return b
}

// renderForFormat selects the renderer; only "generic" remains after
// the Feishu app-mode integration replaced the legacy custom-bot path.
// Unknown formats fall back to generic.
func renderForFormat(format string, ev CommandFinished) []byte {
	return renderGeneric(ev)
}
```

- [ ] **Step 3: Update tests** — `internal/webhook/render_test.go` and `internal/webhook/dispatch_test.go`. For each test that referenced `Format: "feishu"` or asserted the feishu JSON shape, switch to `"generic"` and the generic shape. Drop any feishu-specific test.

  Open the two files and search-replace:

```bash
sed -i.bak 's/"feishu"/"generic"/g' internal/webhook/render_test.go internal/webhook/dispatch_test.go
rm internal/webhook/*.bak
```

  Then manually inspect: any test that depended on feishu's `msg_type:text` body shape needs to be removed or replaced with a generic assertion. Commit only after the test file `go test ./internal/webhook/...` passes.

- [ ] **Step 4: Update `internal/userstore/webhooks.go` doc comment**

```go
// Format describes how the relay renders the event body.
// Only "generic" is supported. The legacy "feishu" custom-bot URL
// path was removed when the relay gained Feishu app-mode integration
// (see internal/feishu/).
Format string
```

- [ ] **Step 5: Run package tests**

```bash
go test ./internal/webhook/... ./internal/userstore/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/webhook/render.go internal/webhook/render_test.go \
        internal/webhook/dispatch_test.go internal/userstore/webhooks.go
git commit -m "refactor(webhook): drop legacy Format=feishu path; only generic remains"
```

---

## Task 11: Relay HTTP — bindings CRUD + begin-pair

**Files:**
- Create: `internal/relay/feishu_http.go`
- Create: `internal/relay/feishu_http_test.go`
- Modify: `internal/relay/server.go` — add `Feishu *feishu.Service` to Config, register routes
- Create: a small adapter so `userstore.SQLiteStore` satisfies the `feishu.BindingStore` interface. Put it in `internal/relay/feishu_bindstore.go` (or inline in main if you prefer; the plan picks a separate file)

This task is the largest single one — split steps carefully.

### 11.0 Adapter

- [ ] **Step 1: Create `internal/relay/feishu_bindstore.go`**

```go
// internal/relay/feishu_bindstore.go
package relay

import (
	"context"

	"github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/userstore"
)

// feishuBindStore adapts *userstore.SQLiteStore to feishu.BindingStore.
type feishuBindStore struct{ st *userstore.SQLiteStore }

// NewFeishuBindStore wraps a userstore for feishu.Service consumption.
func NewFeishuBindStore(st *userstore.SQLiteStore) feishu.BindingStore {
	return &feishuBindStore{st: st}
}

func (a *feishuBindStore) GetBindingByAppIDHash(ctx context.Context, hash string) (*feishu.Binding, error) {
	row, err := a.st.GetFeishuBindingByAppIDHash(ctx, hash)
	if err == userstore.ErrFeishuBindingNotFound {
		return nil, feishu.ErrBindingNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToBinding(row), nil
}

func (a *feishuBindStore) GetBindingByUserID(ctx context.Context, userID string) (*feishu.Binding, error) {
	row, err := a.st.GetFeishuBinding(ctx, userID)
	if err == userstore.ErrFeishuBindingNotFound {
		return nil, feishu.ErrBindingNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToBinding(row), nil
}

func (a *feishuBindStore) MarkBound(ctx context.Context, userID, openID string) error {
	return a.st.MarkFeishuBindingBound(ctx, userID, openID)
}

func (a *feishuBindStore) MarkDisabled(ctx context.Context, userID string) error {
	return a.st.MarkFeishuBindingDisabled(ctx, userID)
}

func (a *feishuBindStore) ConsumePendingBind(ctx context.Context, code string) (string, error) {
	uid, err := a.st.ConsumeFeishuPendingBind(ctx, code)
	if err == userstore.ErrFeishuPendingBindNotFound {
		return "", feishu.ErrFeishuPendingBindNotFoundService
	}
	return uid, err
}

func rowToBinding(r *userstore.FeishuBinding) *feishu.Binding {
	return &feishu.Binding{
		UserID:      r.UserID,
		AppID:       r.AppID,
		AppSecret:   r.AppSecret,
		EncryptKey:  r.EncryptKey,
		VerifyToken: r.VerifyToken,
		AppIDHash:   r.AppIDHash,
		OpenID:      r.OpenID,
		DisabledAt:  r.DisabledAt,
	}
}
```

### 11.1 HTTP handlers — bindings CRUD

- [ ] **Step 2: Write the test for `POST /v1/feishu/bindings/me` validation**

```go
// internal/relay/feishu_http_test.go (start)
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/userstore"
)

// stubFeishuServer mimics the Feishu /open-apis routes used by the
// relay's validation step (token mint) and the bind reply (im send).
type stubFeishuServer struct {
	tokenCode int
	tokenStr  string
}

func newStubFeishu(t *testing.T, sf *stubFeishuServer) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": sf.tokenCode, "msg": "ok",
			"tenant_access_token": sf.tokenStr, "expire": 7200,
		})
	})
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestFeishuHTTP_UpsertBinding_GoodCreds(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "fhh@example.com")
	stub := newStubFeishu(t, &stubFeishuServer{tokenCode: 0, tokenStr: "tt"})

	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st),
		IM:    feishu.NewClient(stub.URL, stub.Client()),
		Token: feishu.NewTenantTokenCache(stub.URL, stub.Client(), nil),
	})

	h := NewFeishuHTTPHandler(st, svc, "https://relay.example.com")
	body, _ := json.Marshal(map[string]string{
		"app_id": "cli_xx", "app_secret": "ss",
		"encrypt_key": "ekekek", "verify_token": "vtvt",
	})
	req := httptest.NewRequest("POST", "/v1/feishu/bindings/me", bytes.NewReader(body))
	req = req.WithContext(ContextWithUserID(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		AppIDHash   string `json:"app_id_hash"`
		CallbackURL string `json:"callback_url"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.AppIDHash == "" || !strings.HasSuffix(resp.CallbackURL, resp.AppIDHash) {
		t.Fatalf("resp: %+v", resp)
	}
}

func TestFeishuHTTP_UpsertBinding_BadCreds(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "fhb@example.com")
	stub := newStubFeishu(t, &stubFeishuServer{tokenCode: 99991663, tokenStr: ""})
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: feishu.NewClient(stub.URL, stub.Client()),
		Token: feishu.NewTenantTokenCache(stub.URL, stub.Client(), nil),
	})
	h := NewFeishuHTTPHandler(st, svc, "https://relay.example.com")

	body, _ := json.Marshal(map[string]string{"app_id": "cli_x", "app_secret": "bad", "encrypt_key": "k", "verify_token": "v"})
	req := httptest.NewRequest("POST", "/v1/feishu/bindings/me", bytes.NewReader(body))
	req = req.WithContext(ContextWithUserID(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestFeishuHTTP_BeginPair(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "bp@example.com")
	stub := newStubFeishu(t, &stubFeishuServer{tokenCode: 0, tokenStr: "tt"})
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: feishu.NewClient(stub.URL, stub.Client()),
		Token: feishu.NewTenantTokenCache(stub.URL, stub.Client(), nil),
	})
	h := NewFeishuHTTPHandler(st, svc, "https://relay.example.com")

	// Need an existing binding first.
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
		AppID: "cli_bp", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
	})
	req := httptest.NewRequest("POST", "/v1/feishu/bindings/me/begin-pair", nil)
	req = req.WithContext(ContextWithUserID(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Code      string `json:"code"`
		ExpiresAt int64  `json:"expires_at"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Code) != 6 || resp.ExpiresAt == 0 {
		t.Fatalf("resp: %+v", resp)
	}
}

func TestFeishuHTTP_GetBindingMe(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "gm@example.com")
	stub := newStubFeishu(t, &stubFeishuServer{tokenCode: 0, tokenStr: "tt"})
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: feishu.NewClient(stub.URL, stub.Client()),
		Token: feishu.NewTenantTokenCache(stub.URL, stub.Client(), nil),
	})
	h := NewFeishuHTTPHandler(st, svc, "https://relay.example.com")

	// Empty case.
	req := httptest.NewRequest("GET", "/v1/feishu/bindings/me", nil)
	req = req.WithContext(ContextWithUserID(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var resp1 struct {
		Configured bool `json:"configured"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp1)
	if resp1.Configured {
		t.Fatalf("should not be configured")
	}

	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{AppID: "x", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var resp2 struct {
		Configured  bool   `json:"configured"`
		Bound       bool   `json:"bound"`
		CallbackURL string `json:"callback_url"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp2)
	if !resp2.Configured || resp2.Bound || resp2.CallbackURL == "" {
		t.Fatalf("resp: %+v", resp2)
	}
}

func TestFeishuHTTP_DeleteBinding(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "del@example.com")
	stub := newStubFeishu(t, &stubFeishuServer{tokenCode: 0, tokenStr: "tt"})
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: feishu.NewClient(stub.URL, stub.Client()),
		Token: feishu.NewTenantTokenCache(stub.URL, stub.Client(), nil),
	})
	h := NewFeishuHTTPHandler(st, svc, "https://relay.example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{AppID: "x", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})

	req := httptest.NewRequest("DELETE", "/v1/feishu/bindings/me", nil)
	req = req.WithContext(ContextWithUserID(req.Context(), u.ID))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 204 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := st.GetFeishuBinding(ctx, u.ID); err != userstore.ErrFeishuBindingNotFound {
		t.Fatalf("expected gone: %v", err)
	}
}

// Tiny helper used by the rest of the file; if a relay-level user-id
// context helper exists already, prefer it.
type userIDCtxKey struct{}

func ContextWithUserID(parent context.Context, id string) context.Context {
	return context.WithValue(parent, userIDCtxKey{}, id)
}
func userIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(userIDCtxKey{}).(string)
	return v
}

// Used by all tests above.
func newTestUserStoreWithCipher(t *testing.T) *userstore.SQLiteStore {
	t.Helper()
	// We need a cipher; reuse userstore's exported helper if present.
	// If userstore exposes only a test-internal helper, build a deterministic
	// cipher here via NewSecretCipher.
	key := bytes.Repeat([]byte{0x11}, 32)
	c, err := userstore.NewSecretCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	st, err := userstore.Open(context.Background(), ":memory:", userstore.WithSecretCipher(c))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

var _ = fmt.Println
var _ io.Reader = nil
```

Note: if `relay` already has a context-user-id helper (search `grep -n "userIDFromContext\|UserIDFromCtx" internal/relay/`), replace `ContextWithUserID`/`userIDFromContext` calls with it and delete the inline helper. Adjust the production `feishu_http.go` to use the same helper.

- [ ] **Step 3: Confirm failure**

Run: `go test ./internal/relay/... -run TestFeishuHTTP`
Expected: undefined `NewFeishuHTTPHandler`, build failure.

- [ ] **Step 4: Implement `internal/relay/feishu_http.go` — bindings CRUD**

```go
// internal/relay/feishu_http.go
package relay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/userstore"
)

// FeishuHTTPHandler exposes /v1/feishu/* routes.
type FeishuHTTPHandler struct {
	store      *userstore.SQLiteStore
	svc        *feishu.Service
	relayBase  string // e.g. "https://relay.example.com"
}

func NewFeishuHTTPHandler(store *userstore.SQLiteStore, svc *feishu.Service, relayBase string) *FeishuHTTPHandler {
	return &FeishuHTTPHandler{
		store:     store,
		svc:       svc,
		relayBase: strings.TrimRight(relayBase, "/"),
	}
}

func (h *FeishuHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/v1/feishu/bindings/me" && r.Method == http.MethodGet:
		h.handleGetMe(w, r)
	case p == "/v1/feishu/bindings/me" && r.Method == http.MethodPost:
		h.handleUpsert(w, r)
	case p == "/v1/feishu/bindings/me" && r.Method == http.MethodDelete:
		h.handleDelete(w, r)
	case p == "/v1/feishu/bindings/me/begin-pair" && r.Method == http.MethodPost:
		h.handleBeginPair(w, r)
	case strings.HasPrefix(p, "/v1/feishu/events/") && r.Method == http.MethodPost:
		h.handleEventCallback(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *FeishuHTTPHandler) callbackURL(hash string) string {
	return h.relayBase + "/v1/feishu/events/" + hash
}

func (h *FeishuHTTPHandler) handleGetMe(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromContext(r.Context())
	if uid == "" {
		http.Error(w, "unauthorized", 401)
		return
	}
	b, err := h.store.GetFeishuBinding(r.Context(), uid)
	if errors.Is(err, userstore.ErrFeishuBindingNotFound) {
		writeJSON(w, 200, map[string]any{"configured": false, "bound": false})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	resp := map[string]any{
		"configured":   true,
		"bound":        b.OpenID != "",
		"open_id":      b.OpenID,
		"disabled_at":  b.DisabledAt,
		"callback_url": h.callbackURL(b.AppIDHash),
	}
	writeJSON(w, 200, resp)
}

type upsertReq struct {
	AppID       string `json:"app_id"`
	AppSecret   string `json:"app_secret"`
	EncryptKey  string `json:"encrypt_key"`
	VerifyToken string `json:"verify_token"`
}

func (h *FeishuHTTPHandler) handleUpsert(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromContext(r.Context())
	if uid == "" {
		http.Error(w, "unauthorized", 401)
		return
	}
	var in upsertReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json: "+err.Error(), 400)
		return
	}
	if in.AppID == "" || in.AppSecret == "" || in.EncryptKey == "" || in.VerifyToken == "" {
		http.Error(w, "all four fields are required", 400)
		return
	}
	// Validate by minting a token; if Feishu rejects, surface 400.
	tok, err := h.svc.MintTokenForCreds(r.Context(), in.AppID, in.AppSecret)
	if err != nil {
		// Auth-class → 400 (bad credentials); other → 502.
		if feishu.IsAuthClassError(err) {
			http.Error(w, "feishu rejected app_id/app_secret: "+err.Error(), 400)
			return
		}
		http.Error(w, "feishu unreachable: "+err.Error(), 502)
		return
	}
	_ = tok // we don't need the value here; just proof of life
	if err := h.store.UpsertFeishuBinding(r.Context(), uid, userstore.FeishuBindingCredentials{
		AppID: in.AppID, AppSecret: in.AppSecret, EncryptKey: in.EncryptKey, VerifyToken: in.VerifyToken,
	}); err != nil {
		if errors.Is(err, userstore.ErrFeishuAppIDConflict) {
			http.Error(w, "this Feishu app is already bound to another atterm user", 409)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	full, _ := h.store.GetFeishuBinding(r.Context(), uid)
	writeJSON(w, 200, map[string]any{
		"app_id_hash":  full.AppIDHash,
		"callback_url": h.callbackURL(full.AppIDHash),
	})
}

func (h *FeishuHTTPHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromContext(r.Context())
	if uid == "" {
		http.Error(w, "unauthorized", 401)
		return
	}
	b, err := h.store.GetFeishuBinding(r.Context(), uid)
	if err == nil {
		h.svc.InvalidateTokenForAppID(b.AppID)
	}
	if err := h.store.DeleteFeishuBinding(r.Context(), uid); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}

func (h *FeishuHTTPHandler) handleBeginPair(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromContext(r.Context())
	if uid == "" {
		http.Error(w, "unauthorized", 401)
		return
	}
	if _, err := h.store.GetFeishuBinding(r.Context(), uid); err != nil {
		http.Error(w, "configure credentials first", 400)
		return
	}
	code := userstore.GenerateFeishuPairCode()
	expires := time.Now().Add(15 * time.Minute).Unix()
	if err := h.store.PutFeishuPendingBind(r.Context(), uid, code, expires); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, map[string]any{
		"code":       code,
		"expires_at": expires,
	})
}

func (h *FeishuHTTPHandler) handleEventCallback(w http.ResponseWriter, r *http.Request) {
	// Implementation in Task 12.
	w.WriteHeader(http.StatusNotImplemented)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// userIDFromContext: reuse existing relay helper if present. The plan
// uses a local placeholder so the file compiles; replace with the
// canonical one (see grep in step 11.1).
func init() {
	_ = path.Clean
	_ = context.Canceled
}
```

- [ ] **Step 5: Add the two helper methods to `feishu.Service`**

In `internal/feishu/service.go`, append:

```go
// MintTokenForCreds is a thin wrapper around the configured TokenSource;
// used by the relay HTTP handler to validate user-pasted credentials.
func (s *Service) MintTokenForCreds(ctx context.Context, appID, appSecret string) (string, error) {
	return s.cfg.Token.Get(ctx, appID, appSecret)
}

// InvalidateTokenForAppID drops a cached token; called by the HTTP
// handler after DELETE /v1/feishu/bindings/me.
func (s *Service) InvalidateTokenForAppID(appID string) {
	s.cfg.Token.Invalidate(appID)
}
```

- [ ] **Step 6: Wire the handler into `internal/relay/server.go`**

Find where existing routes are registered (search `webhooks_http\|mux.Handle\|http.Handler`); add:

```go
// in Config struct:
type Config struct {
	// ... existing ...
	Feishu *feishu.Service // nil → /v1/feishu/* returns 404
}

// in NewServer / startup wiring, alongside other handlers:
if s.cfg.Feishu != nil {
	h := NewFeishuHTTPHandler(s.cfg.Store, s.cfg.Feishu, s.cfg.PublicBaseURL)
	mux.Handle("/v1/feishu/", h)
}
```

(Field names like `cfg.Store`, `cfg.PublicBaseURL`, `mux` must match what server.go actually uses — adjust to the file's existing pattern.)

- [ ] **Step 7: Run tests**

```bash
go test ./internal/relay/... -run TestFeishuHTTP -v
```

Expected: 5 tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/relay/feishu_http.go internal/relay/feishu_http_test.go \
        internal/relay/feishu_bindstore.go internal/relay/server.go \
        internal/feishu/service.go
git commit -m "feat(relay): /v1/feishu/bindings/me CRUD + begin-pair endpoints"
```

---

## Task 12: Relay HTTP — events callback

**Files:**
- Modify: `internal/relay/feishu_http.go` (replace stub `handleEventCallback`)
- Modify: `internal/relay/feishu_http_test.go` (add callback tests)

- [ ] **Step 1: Write callback tests**

Append to `internal/relay/feishu_http_test.go`:

```go
import "crypto/aes"
import "crypto/cipher"
import "crypto/sha256"
import "encoding/base64"
// (only if not already imported)

func feishuEncryptForTest(t *testing.T, encryptKey string, plain []byte) []byte {
	t.Helper()
	key := sha256.Sum256([]byte(encryptKey))
	block, _ := aes.NewCipher(key[:])
	iv := bytes.Repeat([]byte{0x33}, aes.BlockSize)
	padLen := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append([]byte{}, plain...)
	for i := 0; i < padLen; i++ {
		padded = append(padded, byte(padLen))
	}
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)
	combined := append(append([]byte{}, iv...), ct...)
	body, _ := json.Marshal(map[string]string{"encrypt": base64.StdEncoding.EncodeToString(combined)})
	return body
}

func TestFeishuHTTP_EventCallback_URLVerification(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "uv@example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
		AppID: "cli_uv", AppSecret: "s", EncryptKey: "ek-uv", VerifyToken: "vt-uv",
	})
	b, _ := st.GetFeishuBinding(ctx, u.ID)

	stub := newStubFeishu(t, &stubFeishuServer{tokenCode: 0, tokenStr: "tt"})
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: feishu.NewClient(stub.URL, stub.Client()),
		Token: feishu.NewTenantTokenCache(stub.URL, stub.Client(), nil),
	})
	h := NewFeishuHTTPHandler(st, svc, "https://relay.example.com")

	plain := []byte(`{"type":"url_verification","challenge":"xyz","token":"vt-uv"}`)
	body := feishuEncryptForTest(t, "ek-uv", plain)
	req := httptest.NewRequest("POST", "/v1/feishu/events/"+b.AppIDHash, bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Challenge string `json:"challenge"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Challenge != "xyz" {
		t.Fatalf("challenge: %q", resp.Challenge)
	}
}

func TestFeishuHTTP_EventCallback_UnknownHash(t *testing.T) {
	st := newTestUserStoreWithCipher(t)
	stub := newStubFeishu(t, &stubFeishuServer{tokenCode: 0, tokenStr: "tt"})
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: feishu.NewClient(stub.URL, stub.Client()),
		Token: feishu.NewTenantTokenCache(stub.URL, stub.Client(), nil),
	})
	h := NewFeishuHTTPHandler(st, svc, "https://relay.example.com")
	req := httptest.NewRequest("POST", "/v1/feishu/events/00000000000000000000000000000000", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("unknown hash should still 200, got %d", rr.Code)
	}
}

func TestFeishuHTTP_EventCallback_CardAckUpdates(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "ack@example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{
		AppID: "cli_ack", AppSecret: "s", EncryptKey: "ek-ack", VerifyToken: "vt-ack",
	})
	b, _ := st.GetFeishuBinding(ctx, u.ID)
	stub := newStubFeishu(t, &stubFeishuServer{tokenCode: 0, tokenStr: "tt"})
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: feishu.NewClient(stub.URL, stub.Client()),
		Token: feishu.NewTenantTokenCache(stub.URL, stub.Client(), nil),
	})
	h := NewFeishuHTTPHandler(st, svc, "https://relay.example.com")

	plain := []byte(`{"header":{"event_type":"card.action.trigger","token":"vt-ack","app_id":"cli_ack"},"event":{"action":{"value":{"kind":"ack","session_id":"sid-1","event":"command_finished"}},"operator":{"open_id":"ou_x"}}}`)
	body := feishuEncryptForTest(t, "ek-ack", plain)
	req := httptest.NewRequest("POST", "/v1/feishu/events/"+b.AppIDHash, bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "已确认") || !strings.Contains(rr.Body.String(), `"update_multi":true`) {
		t.Fatalf("expected update card in body: %s", rr.Body.String())
	}
}
```

- [ ] **Step 2: Confirm failures** (stub `handleEventCallback` currently returns 501)

Run: `go test ./internal/relay/... -run TestFeishuHTTP_EventCallback`
Expected: FAIL (501).

- [ ] **Step 3: Implement `handleEventCallback`**

Replace the stub in `feishu_http.go`:

```go
func (h *FeishuHTTPHandler) handleEventCallback(w http.ResponseWriter, r *http.Request) {
	// /v1/feishu/events/<app_id_hash>
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/feishu/events/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "missing hash", 400)
		return
	}
	hash := parts[0]

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		// Even read errors must 200 — log only.
		w.WriteHeader(200)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2500*time.Millisecond)
	defer cancel()
	resp, _ := h.svc.HandleEvent(ctx, hash, body)
	if resp == nil {
		w.WriteHeader(200)
		return
	}
	if resp.URLChallenge != "" {
		writeJSON(w, 200, map[string]string{"challenge": resp.URLChallenge})
		return
	}
	if resp.CardUpdate != nil {
		writeJSON(w, 200, resp.CardUpdate)
		return
	}
	w.WriteHeader(200)
}
```

Add `io` to the file's imports if missing.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/relay/... -run TestFeishuHTTP -v`
Expected: 5 prior + 3 new = 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/feishu_http.go internal/relay/feishu_http_test.go
git commit -m "feat(relay): /v1/feishu/events callback — url_verification + card.ack updates"
```

---

## Task 13: Uplink integration — wire feishu into dispatch sites

**Files:**
- Modify: `internal/relay/uplink_conn.go`
- Create: `internal/relay/uplink_feishu_test.go`

- [ ] **Step 1: Locate the dispatch sites**

```bash
grep -n "WebPush.DispatchCommandFinished\|WebPush.DispatchSessionNotification\|Webhook.DispatchCommandFinished" internal/relay/uplink_conn.go
```

Confirm two sites:
- `s.cfg.WebPush.DispatchCommandFinished(...)` around line 524
- `s.cfg.WebPush.DispatchSessionNotification(...)` around line 180

The plan adds a `feishu.Service` call right after each.

- [ ] **Step 2: Write the test**

```go
// internal/relay/uplink_feishu_test.go
package relay

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
)

// fakeIMClient counts SendInteractiveToOpenID calls.
type fakeIMClient struct{ n int32 }

func (f *fakeIMClient) SendInteractiveToOpenID(ctx context.Context, token, openID string, body []byte) error {
	atomic.AddInt32(&f.n, 1)
	return nil
}
func (f *fakeIMClient) SendTextToOpenID(ctx context.Context, token, openID, text string) error {
	return nil
}

type fakeTokSrc struct{}

func (fakeTokSrc) Get(ctx context.Context, a, b string) (string, error) { return "tt", nil }
func (fakeTokSrc) Invalidate(string)                                    {}

func TestUplinkFeishu_SendOnCommandFinished(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "ul@example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = st.MarkFeishuBindingBound(ctx, u.ID, "ou_user")

	imc := &fakeIMClient{}
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: imc, Token: fakeTokSrc{},
	})
	// Simulate the dispatch site directly. (Avoid full uplink fixture.)
	svc.SendCommandFinished(ctx, u.ID, feishu.CommandFinishedInput{SessionID: uuid.New(), ExitCode: 0, Label: "x"})

	// Give the goroutine a moment.
	for i := 0; i < 50 && atomic.LoadInt32(&imc.n) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&imc.n) != 1 {
		t.Fatalf("expected 1 IM call, got %d", imc.n)
	}
}

func TestUplinkFeishu_SkipsUnboundUser(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "ul2@example.com")
	// No binding at all.

	imc := &fakeIMClient{}
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: imc, Token: fakeTokSrc{},
	})
	svc.SendCommandFinished(ctx, u.ID, feishu.CommandFinishedInput{SessionID: uuid.New()})
	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&imc.n) != 0 {
		t.Fatalf("expected 0 IM calls, got %d", imc.n)
	}
}
```

- [ ] **Step 3: Run to verify it compiles + passes the no-uplink-modification baseline**

```bash
go test ./internal/relay/... -run TestUplinkFeishu -v
```

Expected: 2 tests PASS — these don't depend on uplink_conn.go yet.

- [ ] **Step 4: Modify `uplink_conn.go`**

At the dispatch site that currently calls `s.cfg.WebPush.DispatchCommandFinished(...)`, add immediately after:

```go
if s.cfg.Feishu != nil {
	s.cfg.Feishu.SendCommandFinished(context.Background(), ms.sess.OwnerUserID, feishu.CommandFinishedInput{
		SessionID:  uuid.MustParse(ms.sess.ID),
		ExitCode:   ev.ExitCode,
		ElapsedMS:  ev.ElapsedMS,
		Label:      ev.Label,
		SealedBody: ev.SealedBody,
	})
}
```

Adjust `ms.sess.ID` and `ms.sess.OwnerUserID` to whatever the surrounding code uses; preserve any nil-guards already present (search the `:524` site, copy the pattern of the existing `s.cfg.Webhook != nil` check).

At the SessionNotification site around `:180`:

```go
if s.cfg.Feishu != nil && /* notification type is waiting_input */ notif.NotificationType == webpush.NotificationWaitingInput {
	s.cfg.Feishu.SendSessionNotification(context.Background(), ms.sess.OwnerUserID, feishu.WaitingInputInput{
		SessionID:      uuid.MustParse(ms.sess.ID),
		IdleForSeconds: notif.IdleForSeconds,
	})
}
```

Adjust field references to the local variable names actually in scope (`ev`, `notif`, or whatever the file uses). Add the `feishu` import.

- [ ] **Step 5: Run full relay tests**

```bash
go test ./internal/relay/... -v
```

Expected: ALL PASS, including the existing webhook/webpush tests (they shouldn't observe the new feishu path unless they configured one).

- [ ] **Step 6: Commit**

```bash
git add internal/relay/uplink_conn.go internal/relay/uplink_feishu_test.go
git commit -m "feat(relay): dispatch command_finished + waiting_input to feishu.Service"
```

---

## Task 14: Assembly + env var + manual e2e checklist

**Files:**
- Modify: `cmd/relay/main.go`
- Create: `scripts/feishu-e2e-checklist.md`

- [ ] **Step 1: Update `cmd/relay/main.go`**

Find where the store is opened and the Config struct is populated. Insert:

```go
// Near other env-var loading:
encKeyB64 := os.Getenv("ATTERM_FEISHU_ENCRYPT_KEY")
if encKeyB64 == "" {
	log.Fatal("ATTERM_FEISHU_ENCRYPT_KEY is required (32 random bytes, base64-encoded)")
}
encKey, err := base64.StdEncoding.DecodeString(encKeyB64)
if err != nil || len(encKey) != 32 {
	log.Fatalf("ATTERM_FEISHU_ENCRYPT_KEY: want 32 base64-decoded bytes, got %d (err=%v)", len(encKey), err)
}
cipher, err := userstore.NewSecretCipher(encKey)
if err != nil {
	log.Fatalf("secret cipher: %v", err)
}

// Update the existing userstore.Open call to pass the cipher:
store, err := userstore.Open(ctx, cfg.DBPath, userstore.WithSecretCipher(cipher))
if err != nil { ... }

// Construct feishu service (Feishu API base URL is hard-coded; expose
// FEISHU_BASE_URL env override for tests / on-prem mirror only):
feishuBase := os.Getenv("FEISHU_BASE_URL")
if feishuBase == "" {
	feishuBase = "https://open.feishu.cn"
}
httpC := &http.Client{Timeout: 10 * time.Second}
tokenCache := feishu.NewTenantTokenCache(feishuBase, httpC, time.Now)
imClient := feishu.NewClient(feishuBase, httpC)
feishuSvc := feishu.NewService(feishu.ServiceConfig{
	Store: relay.NewFeishuBindStore(store),
	IM:    imClient,
	Token: tokenCache,
})

// Add to the relay Config:
relayCfg := relay.Config{
	// ... existing fields ...
	Feishu: feishuSvc,
}
```

Add a startup log line:

```go
log.Printf("feishu: app-mode integration enabled (base=%s)", feishuBase)
```

If `cmd/relay/main.go`'s structure differs, adapt these snippets to its actual variable names.

- [ ] **Step 2: Run a sanity build + all tests**

```bash
go build ./...
go test ./...
```

Expected: build OK, all tests PASS.

- [ ] **Step 3: Write the manual e2e checklist**

```bash
mkdir -p scripts
```

Create `scripts/feishu-e2e-checklist.md`:

````markdown
# Feishu App-Mode E2E Checklist

Run before each PR that touches `internal/feishu/`, `internal/userstore/feishu_*`,
or `internal/relay/feishu_http.go`. Not part of CI — requires a real Feishu app.

## Prereqs (one-time per developer)

1. Create a self-built Feishu app at https://open.feishu.cn/app
2. Note app_id / app_secret / encrypt_key / verify_token from "凭证与基础信息" + "事件订阅"
3. In "事件订阅" → 开启"事件订阅加密" (mandatory for our integration)
4. Subscribe events: `im.message.receive_v1`, `card.action.trigger`
5. Grant scopes: `im:message`, `im:message:send_as_bot`
6. Set `ATTERM_FEISHU_ENCRYPT_KEY` to a base64'd 32-byte random key

## Walkthrough

- [ ] Start relay (`go run ./cmd/relay`); confirm log: `feishu: app-mode integration enabled`
- [ ] Open atterm desktop UI → Settings → Feishu tab (UI is in a follow-up PR; for now POST via curl)
- [ ] `curl -X POST .../v1/feishu/bindings/me -d '{...}'` with the four secrets
  - Expect: 200 with `app_id_hash` and `callback_url`
  - Verify: a row appears in `feishu_bindings`
- [ ] Paste `callback_url` into Feishu admin "事件订阅 → 请求地址"
  - Expect: Feishu confirms verification succeeded (url_verification echo)
- [ ] `curl -X POST .../v1/feishu/bindings/me/begin-pair`
  - Expect: 200 with `code` (6 chars) and `expires_at`
- [ ] In Feishu IM, private-chat the bot: `/bind <code>`
  - Expect: bot replies "✅ 已绑定到 atterm"
  - Verify: `feishu_bindings.open_id` populated
- [ ] Trigger a `command_finished` from an attached agent (run a long-ish command)
  - Expect: card arrives in Feishu IM with title "命令完成"
- [ ] Tap "确认" on the card
  - Expect: card updates inline to "已确认"
- [ ] Tap "跳回打开 session"
  - Expect: `atterm://session/<sid>` opens — no-op until follow-up PR registers the scheme handler; **document the no-op in the PR description.**
- [ ] Misconfigure verify_token (POST upsert with wrong value)
  - Expect: subsequent /bind attempt logs "verify-token mismatch", bot does NOT reply

## Sealed (E2EE) variant

- [ ] Run agent with E2EE unlocked → trigger command_finished
- [ ] Expect card title "命令完成（仅本机可见）" and body "命令详情仅本机可见"
- [ ] Verify no exit code or label leaked in the card

## Cleanup

- [ ] `curl -X DELETE .../v1/feishu/bindings/me`
- [ ] Verify row removed; subsequent events log "unknown_app_id_hash"
````

- [ ] **Step 4: Commit**

```bash
git add cmd/relay/main.go scripts/feishu-e2e-checklist.md
git commit -m "feat(cmd/relay): assemble feishu.Service + ATTERM_FEISHU_ENCRYPT_KEY env"
```

- [ ] **Step 5: Final verification**

```bash
go build ./...
go test ./...
```

Expected: ALL PASS.

---

## Post-implementation notes

- The `atterm://session/<id>` deep link is a no-op until a follow-up PR
  registers the URL scheme handler in `desktop/main.go` (wails) and the
  mobile Capacitor shim. Spec §12 risks already calls this out — make
  sure the PR description for this series repeats it.
- Frontend Feishu settings tab (Vue / TS) is a separate follow-up PR
  consuming these new HTTP endpoints. The plan above leaves the UI for
  later; the e2e checklist exercises the API directly.
- The plan deliberately keeps `disabled_at` auto-clearing only on
  successful re-upsert. If you add a "test send" admin button in a
  later PR, also clear `disabled_at` on a successful test send.
- If `go test ./...` reveals existing tests that depended on
  `Format=feishu` rows in the webhooks table (beyond the two files
  Task 10 touches), include those fixes in Task 10's commit rather
  than spinning a new commit.
