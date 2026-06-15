# Relay E2EE M1a — Server-side OPAQUE Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add server-side OPAQUE registration and login endpoints to atterm-relay, plus the storage schema for OPAQUE records and account-key wrapping blobs. No client SDK in this slice — endpoints are exercised via Go-level tests that drive both client and server sides of the OPAQUE protocol with `bytemare/opaque`.

**Architecture:** Drop bcrypt/argon2 password verification entirely (per `feedback_no_backward_compat`). Replace `/api/auth/signup` and `/api/auth/login` with a four-endpoint OPAQUE flow: `register/init` → `register/finalize` and `login/init` → `login/finalize`. Two new tables hold the OPAQUE envelope and the AEAD-wrapped account-key blob; the relay stores both but cannot decrypt the wrap without the user's password.

**Tech Stack:** Go 1.22, `github.com/bytemare/opaque` (CFRG draft-12 aligned, P-256 + SHA-256 + Argon2id suite), `github.com/google/uuid`, modernc.org/sqlite.

**Spec:** [docs/superpowers/specs/2026-06-15-relay-e2ee-design.md](../specs/2026-06-15-relay-e2ee-design.md) §4 and §12 M1.

**Out of scope for this slice (handled in later M1 sub-milestones):**
- Desktop / mobile / web client SDKs (M1c, M1d)
- UI flows for register / login / logout (M1e)
- Admin-side password reset UX (M1f)
- Argon2 client-side wrapping of `account_key` (M1b — this slice only stores the blob the client uploads, treating it as opaque bytes)

---

## File Structure

**Create:**
- `internal/userstore/migrations/0003_opaque_auth.sql` — schema
- `internal/userstore/opaque.go` — store methods for OPAQUE records + wrap blobs
- `internal/userstore/opaque_test.go`
- `internal/relay/opaque_auth.go` — HTTP handlers
- `internal/relay/opaque_auth_test.go`
- `internal/relay/opaque_server.go` — singleton `*opaque.Server` wiring + OPRF seed persistence

**Modify:**
- `internal/userstore/migrations.go` — register migration 0003
- `internal/userstore/users.go` — delete `hashPassword` / `verifyPassword` / `CreateUser(password)` / `VerifyPassword` / `ChangePassword` / `ResetPasswordByEmail` (per no-backward-compat). Keep `GetUser`, `ListUsers`, `DisableUser`, `ResetUserPassword` (latter renamed and rewired in Task 13)
- `internal/userstore/users_delete.go` — drop password references in tests
- `internal/relay/auth_http.go` — delete `handleSignup` / `handleLogin` / shared helpers (replaced by `opaque_auth.go`)
- `internal/relay/server.go` — register new routes; remove old routes
- `cmd/atterm-relay/bootstrap_admin.go` — use new OPAQUE registration path for bootstrap admin user
- `cmd/atterm-relay/main.go` — instantiate `opaque_server.go` singleton during boot
- `go.mod` / `go.sum` — add `bytemare/opaque`

---

## Task 1: Add `bytemare/opaque` dependency and lock the cipher suite

**Files:**
- Modify: `go.mod`, `go.sum`

The library version + cipher suite choice is load-bearing — every other task depends on the exact API surface and the deterministic protocol bytes. Locking this first prevents churn.

- [ ] **Step 1: Add dependency**

```bash
cd /Users/attson/code/github.com.attson/atterm
go get github.com/bytemare/opaque@v0.10.0
go mod tidy
```

- [ ] **Step 2: Confirm cipher suite availability**

Write `internal/relay/opaque_server_smoke_test.go` (will be deleted in Task 3 — purely a doc check):

```go
package relay

import (
    "testing"

    "github.com/bytemare/opaque"
)

// Smoke test: confirm the cipher suite we plan to use is available
// in this library version. Locks the choice so the rest of M1a can
// reference it without surprise.
func TestOPAQUESuiteAvailable(t *testing.T) {
    conf := opaque.DefaultConfiguration()
    if conf.OPRF == 0 || conf.KDF == 0 || conf.MAC == 0 || conf.Hash == 0 || conf.AKE == 0 {
        t.Fatalf("default OPAQUE configuration has unset field: %+v", conf)
    }
    if _, err := conf.Server(); err != nil {
        t.Fatalf("conf.Server: %v", err)
    }
    if _, err := conf.Client(); err != nil {
        t.Fatalf("conf.Client: %v", err)
    }
}
```

- [ ] **Step 3: Run the smoke test**

Run: `go test ./internal/relay/ -run TestOPAQUESuiteAvailable -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/relay/opaque_server_smoke_test.go
git commit -m "chore: add bytemare/opaque dependency for OPAQUE auth"
```

---

## Task 2: Schema migration for OPAQUE records and account-key wraps

**Files:**
- Create: `internal/userstore/migrations/0003_opaque_auth.sql`
- Modify: `internal/userstore/migrations.go`
- Modify: `internal/userstore/users.go` (drop `password_hash` column from `User` struct)

- [ ] **Step 1: Write migration SQL**

Create `internal/userstore/migrations/0003_opaque_auth.sql`:

```sql
-- 0003_opaque_auth.sql — replace bcrypt password column with OPAQUE auth.
-- Per feedback_no_backward_compat, this migration drops password_hash
-- entirely; existing accounts must re-register.

ALTER TABLE users DROP COLUMN password_hash;
ALTER TABLE users ADD COLUMN auth_mode TEXT NOT NULL DEFAULT 'opaque';

CREATE TABLE user_opaque_records (
    user_id    TEXT PRIMARY KEY,
    record     BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE user_account_key_wraps (
    user_id    TEXT NOT NULL,
    method     TEXT NOT NULL,
    wrapped    BLOB NOT NULL,
    nonce      BLOB NOT NULL,
    salt       BLOB NOT NULL,
    kdf_params TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, method),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- OPAQUE OPRF seed for this relay instance; generated on first boot.
CREATE TABLE opaque_server_state (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    oprf_seed     BLOB NOT NULL,
    server_ake_sk BLOB NOT NULL,
    server_ake_pk BLOB NOT NULL,
    suite         TEXT NOT NULL,
    created_at    INTEGER NOT NULL
);
```

- [ ] **Step 2: Register migration in `migrations.go`**

Open `internal/userstore/migrations.go`, locate the embedded migrations list, append `0003_opaque_auth.sql` in the same pattern as `0002_user_preferences.sql`. The exact code style matches whatever the existing file uses (don't reinvent).

- [ ] **Step 3: Update `User` struct in `users.go`**

Delete the `PasswordHash` field from `type User struct`. Add `AuthMode string` field. Update all `SELECT`/`INSERT` SQL that currently references `password_hash` — for many functions in `users.go` this means they need rewriting (handled in Task 13).

For this step, the minimal change is:

```go
type User struct {
    ID        string
    Email     string
    AuthMode  string  // new: "opaque" only in v1
    CreatedAt time.Time
    DisabledAt *time.Time
}
```

Compile will break in many places — that's expected; Tasks 13 and 14 fix the cascade.

- [ ] **Step 4: Run migrations on a fresh test DB**

```bash
go test ./internal/userstore/ -run TestMigrations -v
```

(`TestMigrations` is the existing migration-runner test. If it does not exist, add a one-liner test that opens an in-memory SQLite, runs `ApplyAll`, and asserts no error.)

Expected: PASS — schema applies cleanly.

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/migrations/0003_opaque_auth.sql \
        internal/userstore/migrations.go \
        internal/userstore/users.go
git commit -m "feat(userstore): add OPAQUE auth schema (drops password_hash)"
```

---

## Task 3: OPAQUE server singleton + OPRF seed persistence

**Files:**
- Create: `internal/relay/opaque_server.go`
- Create: `internal/relay/opaque_server_test.go`
- Delete: `internal/relay/opaque_server_smoke_test.go` (replaced)

The relay needs a single `*opaque.Server` instance whose OPRF seed and AKE keys persist across restarts. First boot generates them; subsequent boots load from the new `opaque_server_state` table.

- [ ] **Step 1: Write failing test for `LoadOrInitServer`**

Create `internal/relay/opaque_server_test.go`:

```go
package relay

import (
    "context"
    "testing"

    "github.com/attson/atterm/internal/userstore"
)

func TestLoadOrInitServer_GeneratesOnFirstBoot(t *testing.T) {
    store := userstore.NewTestStore(t) // existing helper opens in-memory SQLite
    ctx := context.Background()

    srv1, err := LoadOrInitOpaqueServer(ctx, store)
    if err != nil {
        t.Fatalf("first boot: %v", err)
    }
    if srv1 == nil {
        t.Fatalf("nil server")
    }

    srv2, err := LoadOrInitOpaqueServer(ctx, store)
    if err != nil {
        t.Fatalf("second boot: %v", err)
    }

    // Both boots must produce a server with the same public key.
    pk1 := srv1.AkePublicKey()
    pk2 := srv2.AkePublicKey()
    if string(pk1) != string(pk2) {
        t.Fatalf("AKE public key changed across boots: %x vs %x", pk1, pk2)
    }
}
```

- [ ] **Step 2: Implement `LoadOrInitOpaqueServer`**

Create `internal/relay/opaque_server.go`:

```go
package relay

import (
    "context"
    "crypto/rand"
    "errors"
    "fmt"
    "time"

    "github.com/bytemare/opaque"

    "github.com/attson/atterm/internal/userstore"
)

// OpaqueServer wraps the bytemare/opaque server with the relay's
// persisted OPRF seed and AKE keypair.
type OpaqueServer struct {
    conf      *opaque.Configuration
    serverID  []byte // relay's server identity, hashed from DSN or chosen name
    oprfSeed  []byte
    akeSecret []byte
    akePublic []byte
}

func defaultConfig() *opaque.Configuration {
    // Locked at M1a: see Task 1 smoke test.
    return opaque.DefaultConfiguration()
}

// LoadOrInitOpaqueServer reads the persisted OPAQUE seed + AKE keys from
// the store, or generates them on first boot.
func LoadOrInitOpaqueServer(ctx context.Context, store *userstore.SQLiteStore) (*OpaqueServer, error) {
    state, err := store.GetOpaqueServerState(ctx)
    switch {
    case err == nil:
        return &OpaqueServer{
            conf:      defaultConfig(),
            serverID:  []byte("atterm-relay"),
            oprfSeed:  state.OPRFSeed,
            akeSecret: state.AKEServerSecret,
            akePublic: state.AKEServerPublic,
        }, nil
    case errors.Is(err, userstore.ErrOpaqueStateMissing):
        // first boot: generate
    default:
        return nil, fmt.Errorf("load opaque state: %w", err)
    }

    conf := defaultConfig()
    seed := make([]byte, conf.OPRF.Group().ScalarLength())
    if _, err := rand.Read(seed); err != nil {
        return nil, fmt.Errorf("rand oprf seed: %w", err)
    }

    sv, err := conf.Server()
    if err != nil {
        return nil, fmt.Errorf("opaque server: %w", err)
    }
    sk, pk := sv.KeyGen()

    if err := store.StoreOpaqueServerState(ctx, userstore.OpaqueServerState{
        OPRFSeed:        seed,
        AKEServerSecret: sk,
        AKEServerPublic: pk,
        Suite:           "default-v1",
        CreatedAt:       time.Now(),
    }); err != nil {
        return nil, fmt.Errorf("persist opaque state: %w", err)
    }

    return &OpaqueServer{
        conf:      conf,
        serverID:  []byte("atterm-relay"),
        oprfSeed:  seed,
        akeSecret: sk,
        akePublic: pk,
    }, nil
}

// AkePublicKey returns the relay's static AKE public key (for clients
// who want to pin it).
func (o *OpaqueServer) AkePublicKey() []byte {
    return append([]byte(nil), o.akePublic...)
}

// newServer constructs a fresh per-request opaque.Server bound to this
// instance's seed and key material.
func (o *OpaqueServer) newServer() (*opaque.Server, error) {
    sv, err := o.conf.Server()
    if err != nil {
        return nil, err
    }
    if err := sv.SetKeyMaterial(o.serverID, o.akeSecret, o.akePublic, o.oprfSeed); err != nil {
        return nil, err
    }
    return sv, nil
}
```

(Note: `SetKeyMaterial` signature here matches the `bytemare/opaque@v0.10.0` API. If the import errors say otherwise, adapt — but do not change the structure of `OpaqueServer`.)

- [ ] **Step 3: Delete smoke test**

```bash
git rm internal/relay/opaque_server_smoke_test.go
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/relay/ -run TestLoadOrInitServer -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/opaque_server.go internal/relay/opaque_server_test.go
git commit -m "feat(relay): persistent OPAQUE server with OPRF seed + AKE keys"
```

---

## Task 4: userstore methods for OPAQUE record and server state

**Files:**
- Create: `internal/userstore/opaque.go`
- Create: `internal/userstore/opaque_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/userstore/opaque_test.go`:

```go
package userstore

import (
    "context"
    "errors"
    "testing"
    "time"
)

func TestOpaqueServerStateRoundTrip(t *testing.T) {
    store := NewTestStore(t)
    ctx := context.Background()

    _, err := store.GetOpaqueServerState(ctx)
    if !errors.Is(err, ErrOpaqueStateMissing) {
        t.Fatalf("expected ErrOpaqueStateMissing, got %v", err)
    }

    want := OpaqueServerState{
        OPRFSeed:        []byte("seed-bytes"),
        AKEServerSecret: []byte("sk"),
        AKEServerPublic: []byte("pk"),
        Suite:           "default-v1",
        CreatedAt:       time.Now().UTC().Truncate(time.Second),
    }
    if err := store.StoreOpaqueServerState(ctx, want); err != nil {
        t.Fatalf("store: %v", err)
    }
    got, err := store.GetOpaqueServerState(ctx)
    if err != nil {
        t.Fatalf("get: %v", err)
    }
    if string(got.OPRFSeed) != string(want.OPRFSeed) ||
        string(got.AKEServerSecret) != string(want.AKEServerSecret) ||
        string(got.AKEServerPublic) != string(want.AKEServerPublic) ||
        got.Suite != want.Suite {
        t.Fatalf("round-trip mismatch: %+v vs %+v", got, want)
    }
}

func TestOpaqueRecordRoundTrip(t *testing.T) {
    store := NewTestStore(t)
    ctx := context.Background()

    user := mustMakeUser(t, store, "alice@example.com")

    if _, err := store.GetOpaqueRecord(ctx, user.ID); !errors.Is(err, ErrOpaqueRecordMissing) {
        t.Fatalf("expected missing, got %v", err)
    }
    rec := []byte("opaque-envelope-bytes")
    if err := store.StoreOpaqueRecord(ctx, user.ID, rec); err != nil {
        t.Fatalf("store record: %v", err)
    }
    got, err := store.GetOpaqueRecord(ctx, user.ID)
    if err != nil {
        t.Fatalf("get record: %v", err)
    }
    if string(got) != string(rec) {
        t.Fatalf("record mismatch")
    }
}
```

(The helper `mustMakeUser` does not exist yet because Task 13 replaces `CreateUser`. For this task, write a minimal local helper that inserts an empty user row directly via the store's `*sql.DB` so this test compiles without depending on the not-yet-written user-creation path.)

- [ ] **Step 2: Implement userstore methods**

Create `internal/userstore/opaque.go`:

```go
package userstore

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "time"
)

var (
    ErrOpaqueStateMissing  = errors.New("userstore: opaque server state not initialized")
    ErrOpaqueRecordMissing = errors.New("userstore: opaque record not found")
)

type OpaqueServerState struct {
    OPRFSeed        []byte
    AKEServerSecret []byte
    AKEServerPublic []byte
    Suite           string
    CreatedAt       time.Time
}

func (s *SQLiteStore) GetOpaqueServerState(ctx context.Context) (OpaqueServerState, error) {
    var (
        seed, sk, pk []byte
        suite        string
        createdAt    int64
    )
    err := s.db.QueryRowContext(ctx,
        `SELECT oprf_seed, server_ake_sk, server_ake_pk, suite, created_at
         FROM opaque_server_state WHERE id = 1`).Scan(&seed, &sk, &pk, &suite, &createdAt)
    if errors.Is(err, sql.ErrNoRows) {
        return OpaqueServerState{}, ErrOpaqueStateMissing
    }
    if err != nil {
        return OpaqueServerState{}, fmt.Errorf("query opaque_server_state: %w", err)
    }
    return OpaqueServerState{
        OPRFSeed:        seed,
        AKEServerSecret: sk,
        AKEServerPublic: pk,
        Suite:           suite,
        CreatedAt:       time.Unix(createdAt, 0).UTC(),
    }, nil
}

func (s *SQLiteStore) StoreOpaqueServerState(ctx context.Context, st OpaqueServerState) error {
    _, err := s.db.ExecContext(ctx,
        `INSERT INTO opaque_server_state(id, oprf_seed, server_ake_sk, server_ake_pk, suite, created_at)
         VALUES (1, ?, ?, ?, ?, ?)
         ON CONFLICT(id) DO UPDATE SET
             oprf_seed     = excluded.oprf_seed,
             server_ake_sk = excluded.server_ake_sk,
             server_ake_pk = excluded.server_ake_pk,
             suite         = excluded.suite,
             created_at    = excluded.created_at`,
        st.OPRFSeed, st.AKEServerSecret, st.AKEServerPublic, st.Suite, st.CreatedAt.Unix())
    if err != nil {
        return fmt.Errorf("upsert opaque_server_state: %w", err)
    }
    return nil
}

func (s *SQLiteStore) GetOpaqueRecord(ctx context.Context, userID string) ([]byte, error) {
    var rec []byte
    err := s.db.QueryRowContext(ctx,
        `SELECT record FROM user_opaque_records WHERE user_id = ?`, userID).Scan(&rec)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, ErrOpaqueRecordMissing
    }
    if err != nil {
        return nil, fmt.Errorf("query opaque record: %w", err)
    }
    return rec, nil
}

func (s *SQLiteStore) StoreOpaqueRecord(ctx context.Context, userID string, record []byte) error {
    now := time.Now().Unix()
    _, err := s.db.ExecContext(ctx,
        `INSERT INTO user_opaque_records(user_id, record, created_at)
         VALUES (?, ?, ?)
         ON CONFLICT(user_id) DO UPDATE SET
             record     = excluded.record,
             created_at = excluded.created_at`,
        userID, record, now)
    if err != nil {
        return fmt.Errorf("upsert opaque record: %w", err)
    }
    return nil
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/userstore/ -run "TestOpaqueServerState|TestOpaqueRecord" -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/userstore/opaque.go internal/userstore/opaque_test.go
git commit -m "feat(userstore): OPAQUE record and server-state persistence"
```

---

## Task 5: userstore methods for account-key wrap blobs

**Files:**
- Modify: `internal/userstore/opaque.go` (append)
- Modify: `internal/userstore/opaque_test.go` (append)

Per the spec §4.5 the wrap blob is opaque to the relay — it just stores and returns bytes plus the KDF parameters.

- [ ] **Step 1: Write failing test**

Append to `internal/userstore/opaque_test.go`:

```go
func TestAccountKeyWrapRoundTrip(t *testing.T) {
    store := NewTestStore(t)
    ctx := context.Background()

    user := mustMakeUser(t, store, "bob@example.com")

    if _, err := store.GetAccountKeyWrap(ctx, user.ID, "password"); !errors.Is(err, ErrAccountKeyWrapMissing) {
        t.Fatalf("expected missing, got %v", err)
    }
    wrap := AccountKeyWrap{
        UserID:    user.ID,
        Method:    "password",
        Wrapped:   []byte("aead-ciphertext"),
        Nonce:     []byte("12345678901234567890abcd"),
        Salt:      []byte("salt-16-bytes-here"),
        KDFParams: `{"alg":"argon2id","m":67108864,"t":3,"p":1}`,
        CreatedAt: time.Now().UTC().Truncate(time.Second),
    }
    if err := store.StoreAccountKeyWrap(ctx, wrap); err != nil {
        t.Fatalf("store wrap: %v", err)
    }
    got, err := store.GetAccountKeyWrap(ctx, user.ID, "password")
    if err != nil {
        t.Fatalf("get wrap: %v", err)
    }
    if string(got.Wrapped) != string(wrap.Wrapped) ||
        string(got.Nonce) != string(wrap.Nonce) ||
        string(got.Salt) != string(wrap.Salt) ||
        got.KDFParams != wrap.KDFParams {
        t.Fatalf("wrap round-trip mismatch")
    }
}
```

- [ ] **Step 2: Implement methods**

Append to `internal/userstore/opaque.go`:

```go
var ErrAccountKeyWrapMissing = errors.New("userstore: account key wrap not found")

type AccountKeyWrap struct {
    UserID    string
    Method    string
    Wrapped   []byte
    Nonce     []byte
    Salt      []byte
    KDFParams string
    CreatedAt time.Time
}

func (s *SQLiteStore) GetAccountKeyWrap(ctx context.Context, userID, method string) (AccountKeyWrap, error) {
    var (
        w           AccountKeyWrap
        createdAt   int64
    )
    err := s.db.QueryRowContext(ctx,
        `SELECT user_id, method, wrapped, nonce, salt, kdf_params, created_at
         FROM user_account_key_wraps WHERE user_id = ? AND method = ?`,
        userID, method).Scan(&w.UserID, &w.Method, &w.Wrapped, &w.Nonce, &w.Salt, &w.KDFParams, &createdAt)
    if errors.Is(err, sql.ErrNoRows) {
        return AccountKeyWrap{}, ErrAccountKeyWrapMissing
    }
    if err != nil {
        return AccountKeyWrap{}, fmt.Errorf("query account key wrap: %w", err)
    }
    w.CreatedAt = time.Unix(createdAt, 0).UTC()
    return w, nil
}

func (s *SQLiteStore) StoreAccountKeyWrap(ctx context.Context, w AccountKeyWrap) error {
    if w.CreatedAt.IsZero() {
        w.CreatedAt = time.Now()
    }
    _, err := s.db.ExecContext(ctx,
        `INSERT INTO user_account_key_wraps(user_id, method, wrapped, nonce, salt, kdf_params, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT(user_id, method) DO UPDATE SET
             wrapped    = excluded.wrapped,
             nonce      = excluded.nonce,
             salt       = excluded.salt,
             kdf_params = excluded.kdf_params,
             created_at = excluded.created_at`,
        w.UserID, w.Method, w.Wrapped, w.Nonce, w.Salt, w.KDFParams, w.CreatedAt.Unix())
    if err != nil {
        return fmt.Errorf("upsert account key wrap: %w", err)
    }
    return nil
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/userstore/ -run TestAccountKeyWrap -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/userstore/opaque.go internal/userstore/opaque_test.go
git commit -m "feat(userstore): account_key wrap blob persistence"
```

---

## Task 6: HTTP request/response types and routing skeleton

**Files:**
- Create: `internal/relay/opaque_auth.go`

This task defines the wire schemas + handler stubs. Endpoints return 501 until later tasks fill them in.

- [ ] **Step 1: Implement skeleton**

Create `internal/relay/opaque_auth.go`:

```go
package relay

import (
    "encoding/json"
    "net/http"

    "github.com/attson/atterm/internal/userstore"
)

// OpaqueAuthHandler owns the OPAQUE-based registration and login HTTP
// endpoints. It is wired by server.go after the OpaqueServer singleton
// has loaded its persisted seed.
type OpaqueAuthHandler struct {
    store *userstore.SQLiteStore
    srv   *OpaqueServer
}

func NewOpaqueAuthHandler(store *userstore.SQLiteStore, srv *OpaqueServer) *OpaqueAuthHandler {
    return &OpaqueAuthHandler{store: store, srv: srv}
}

// ----- Wire types -----

type registerInitRequest struct {
    Email          string `json:"email"`
    RegistrationKE []byte `json:"registration_ke"` // KE1 bytes from client
}

type registerInitResponse struct {
    RegistrationResponse []byte `json:"registration_response"` // KE2 bytes
}

type registerFinalizeRequest struct {
    Email                string                  `json:"email"`
    RegistrationRecord   []byte                  `json:"registration_record"` // KE3-derived envelope
    AccountKeyWrap       accountKeyWrapPayload   `json:"account_key_wrap"`
}

type accountKeyWrapPayload struct {
    Method    string `json:"method"`
    Wrapped   []byte `json:"wrapped"`
    Nonce     []byte `json:"nonce"`
    Salt      []byte `json:"salt"`
    KDFParams string `json:"kdf_params"`
}

type registerFinalizeResponse struct {
    UserID       string `json:"user_id"`
    SessionToken string `json:"session_token"`
}

type loginInitRequest struct {
    Email   string `json:"email"`
    LoginKE []byte `json:"login_ke"` // KE1
}

type loginInitResponse struct {
    LoginResponse []byte `json:"login_response"` // KE2
    SessionID     string `json:"session_id"`     // server-side OPAQUE session id for the multi-step flow
}

type loginFinalizeRequest struct {
    Email     string `json:"email"`
    SessionID string `json:"session_id"`
    LoginKE3  []byte `json:"login_ke3"`
}

type loginFinalizeResponse struct {
    UserID         string                `json:"user_id"`
    SessionToken   string                `json:"session_token"`
    AccountKeyWrap accountKeyWrapPayload `json:"account_key_wrap"`
}

// ----- Handlers (stubs filled in by Tasks 7-10) -----

func (h *OpaqueAuthHandler) handleRegisterInit(w http.ResponseWriter, r *http.Request)     { writeNotImpl(w) }
func (h *OpaqueAuthHandler) handleRegisterFinalize(w http.ResponseWriter, r *http.Request) { writeNotImpl(w) }
func (h *OpaqueAuthHandler) handleLoginInit(w http.ResponseWriter, r *http.Request)        { writeNotImpl(w) }
func (h *OpaqueAuthHandler) handleLoginFinalize(w http.ResponseWriter, r *http.Request)    { writeNotImpl(w) }

func writeNotImpl(w http.ResponseWriter) {
    w.WriteHeader(http.StatusNotImplemented)
    _ = json.NewEncoder(w).Encode(map[string]string{"error": "not implemented"})
}

// Register adds the four OPAQUE routes to mux. Caller should ensure the
// routes are gated by CSRF and rate limiting consistent with the existing
// auth_http.go.
func (h *OpaqueAuthHandler) Register(mux *http.ServeMux) {
    mux.HandleFunc("POST /api/auth/register/init", h.handleRegisterInit)
    mux.HandleFunc("POST /api/auth/register/finalize", h.handleRegisterFinalize)
    mux.HandleFunc("POST /api/auth/login/init", h.handleLoginInit)
    mux.HandleFunc("POST /api/auth/login/finalize", h.handleLoginFinalize)
}
```

- [ ] **Step 2: Verify compiles**

```bash
go build ./internal/relay/
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/relay/opaque_auth.go
git commit -m "feat(relay): OPAQUE auth handler skeleton + wire types"
```

---

## Task 7: Implement `handleRegisterInit`

**Files:**
- Modify: `internal/relay/opaque_auth.go` (replace `handleRegisterInit`)
- Create: `internal/relay/opaque_auth_test.go`

`registerInit` consumes the client's KE1, runs the server's OPAQUE registration response (KE2), and returns it. No user row is created until `finalize`.

- [ ] **Step 1: Write failing integration test**

Create `internal/relay/opaque_auth_test.go`:

```go
package relay

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/bytemare/opaque"

    "github.com/attson/atterm/internal/userstore"
)

func newTestHandler(t *testing.T) (*OpaqueAuthHandler, *opaque.Configuration) {
    t.Helper()
    store := userstore.NewTestStore(t)
    srv, err := LoadOrInitOpaqueServer(context.Background(), store)
    if err != nil {
        t.Fatalf("LoadOrInitOpaqueServer: %v", err)
    }
    return NewOpaqueAuthHandler(store, srv), defaultConfig()
}

func TestRegisterInit_ReturnsKE2(t *testing.T) {
    h, conf := newTestHandler(t)

    client, err := conf.Client()
    if err != nil {
        t.Fatalf("client: %v", err)
    }
    ke1 := client.RegistrationInit([]byte("hunter2"))

    body, _ := json.Marshal(registerInitRequest{
        Email:          "alice@example.com",
        RegistrationKE: ke1.Serialize(),
    })
    req := httptest.NewRequest(http.MethodPost, "/api/auth/register/init", bytes.NewReader(body))
    rec := httptest.NewRecorder()

    h.handleRegisterInit(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
    }
    var resp registerInitResponse
    if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if len(resp.RegistrationResponse) == 0 {
        t.Fatalf("empty KE2")
    }
}
```

- [ ] **Step 2: Implement handler**

Replace `handleRegisterInit` in `internal/relay/opaque_auth.go`:

```go
func (h *OpaqueAuthHandler) handleRegisterInit(w http.ResponseWriter, r *http.Request) {
    var req registerInitRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "bad json", http.StatusBadRequest)
        return
    }
    if req.Email == "" || len(req.RegistrationKE) == 0 {
        http.Error(w, "missing fields", http.StatusBadRequest)
        return
    }

    sv, err := h.srv.newServer()
    if err != nil {
        http.Error(w, "internal: opaque server", http.StatusInternalServerError)
        return
    }

    ke1 := h.srv.conf.OPRF.Group().NewElement()
    // bytemare/opaque parses KE1 from bytes via DeserializeRegistrationRequest;
    // exact call depends on library; below is intent. Engineer adapts to
    // the actual API.
    regReq, err := h.srv.conf.DeserializeRegistrationRequest(req.RegistrationKE)
    if err != nil {
        http.Error(w, "bad registration_ke", http.StatusBadRequest)
        return
    }
    _ = ke1
    ke2 := sv.RegistrationResponse(regReq, h.srv.akePublic, []byte(req.Email), h.srv.oprfSeed)

    _ = json.NewEncoder(w).Encode(registerInitResponse{
        RegistrationResponse: ke2.Serialize(),
    })
}
```

- [ ] **Step 3: Run test**

```bash
go test ./internal/relay/ -run TestRegisterInit -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/relay/opaque_auth.go internal/relay/opaque_auth_test.go
git commit -m "feat(relay): implement OPAQUE register init endpoint"
```

---

## Task 8: Implement `handleRegisterFinalize`

**Files:**
- Modify: `internal/relay/opaque_auth.go` (replace `handleRegisterFinalize`)
- Modify: `internal/relay/opaque_auth_test.go` (append)
- Create or modify: `internal/userstore/users.go` — add `CreateOpaqueUser(ctx, email) (*User, error)` that inserts a user row with no password column (replaces the old password-taking `CreateUser`)

`registerFinalize` parses the registration record from the client, persists it alongside the wrapped account_key, creates the user row, and returns a session token (reuse existing session-token issuance from `auth.go`).

- [ ] **Step 1: Write failing test**

Append to `internal/relay/opaque_auth_test.go`:

```go
func TestRegisterFinalize_PersistsRecordAndWrap(t *testing.T) {
    h, conf := newTestHandler(t)

    client, _ := conf.Client()
    ke1 := client.RegistrationInit([]byte("hunter2"))

    // init round
    body, _ := json.Marshal(registerInitRequest{Email: "alice@example.com", RegistrationKE: ke1.Serialize()})
    req := httptest.NewRequest(http.MethodPost, "/api/auth/register/init", bytes.NewReader(body))
    rec := httptest.NewRecorder()
    h.handleRegisterInit(rec, req)

    var initResp registerInitResponse
    if err := json.NewDecoder(rec.Body).Decode(&initResp); err != nil {
        t.Fatalf("decode init resp: %v", err)
    }

    ke2, err := conf.DeserializeRegistrationResponse(initResp.RegistrationResponse)
    if err != nil {
        t.Fatalf("ke2: %v", err)
    }

    record, _ := client.RegistrationFinalize(ke2, []byte("alice@example.com"), []byte("atterm-relay"))

    body2, _ := json.Marshal(registerFinalizeRequest{
        Email:              "alice@example.com",
        RegistrationRecord: record.Serialize(),
        AccountKeyWrap: accountKeyWrapPayload{
            Method:    "password",
            Wrapped:   []byte("ciphertext"),
            Nonce:     []byte("xchacha-nonce-24-bytes-aa"),
            Salt:      []byte("argon-salt-16byt"),
            KDFParams: `{"alg":"argon2id","m":67108864,"t":3,"p":1}`,
        },
    })
    req2 := httptest.NewRequest(http.MethodPost, "/api/auth/register/finalize", bytes.NewReader(body2))
    rec2 := httptest.NewRecorder()
    h.handleRegisterFinalize(rec2, req2)

    if rec2.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200; body=%s", rec2.Code, rec2.Body.String())
    }
    var finResp registerFinalizeResponse
    if err := json.NewDecoder(rec2.Body).Decode(&finResp); err != nil {
        t.Fatalf("decode fin resp: %v", err)
    }
    if finResp.UserID == "" || finResp.SessionToken == "" {
        t.Fatalf("missing user_id or session_token: %+v", finResp)
    }

    // Verify persistence
    rec3, err := h.store.GetOpaqueRecord(context.Background(), finResp.UserID)
    if err != nil {
        t.Fatalf("opaque record not persisted: %v", err)
    }
    if len(rec3) == 0 {
        t.Fatalf("empty opaque record")
    }
    wrap, err := h.store.GetAccountKeyWrap(context.Background(), finResp.UserID, "password")
    if err != nil {
        t.Fatalf("wrap not persisted: %v", err)
    }
    if string(wrap.Wrapped) != "ciphertext" {
        t.Fatalf("wrap mismatch: %s", wrap.Wrapped)
    }
}
```

- [ ] **Step 2: Add `CreateOpaqueUser` to `users.go`**

```go
// CreateOpaqueUser inserts a user row with no password column. The caller
// is responsible for separately storing the OPAQUE record and the
// account_key wrap in the same transaction (out of scope here — handler
// composes the calls).
func (s *SQLiteStore) CreateOpaqueUser(ctx context.Context, email string) (*User, error) {
    id := uuid.NewString()
    now := time.Now()
    _, err := s.db.ExecContext(ctx,
        `INSERT INTO users(id, email, auth_mode, created_at) VALUES (?, ?, ?, ?)`,
        id, email, "opaque", now.Unix())
    if err != nil {
        return nil, fmt.Errorf("insert user: %w", err)
    }
    return &User{ID: id, Email: email, AuthMode: "opaque", CreatedAt: now}, nil
}
```

- [ ] **Step 3: Implement `handleRegisterFinalize`**

Replace `handleRegisterFinalize` in `opaque_auth.go`. Pseudocode (engineer fills in exact `bytemare/opaque` calls; intent locked here):

```go
func (h *OpaqueAuthHandler) handleRegisterFinalize(w http.ResponseWriter, r *http.Request) {
    var req registerFinalizeRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "bad json", http.StatusBadRequest)
        return
    }
    if req.Email == "" || len(req.RegistrationRecord) == 0 ||
        len(req.AccountKeyWrap.Wrapped) == 0 || len(req.AccountKeyWrap.Salt) == 0 ||
        len(req.AccountKeyWrap.Nonce) == 0 || req.AccountKeyWrap.KDFParams == "" {
        http.Error(w, "missing fields", http.StatusBadRequest)
        return
    }

    ctx := r.Context()

    // Parse OPAQUE record bytes (validates format).
    if _, err := h.srv.conf.DeserializeRegistrationRecord(req.RegistrationRecord); err != nil {
        http.Error(w, "bad registration record", http.StatusBadRequest)
        return
    }

    // Create user + persist OPAQUE record + persist wrap atomically.
    user, err := h.store.CreateOpaqueUser(ctx, req.Email)
    if err != nil {
        // distinguish unique-constraint from other errors
        http.Error(w, "email taken or db error", http.StatusConflict)
        return
    }
    if err := h.store.StoreOpaqueRecord(ctx, user.ID, req.RegistrationRecord); err != nil {
        http.Error(w, "internal: store record", http.StatusInternalServerError)
        return
    }
    if err := h.store.StoreAccountKeyWrap(ctx, userstore.AccountKeyWrap{
        UserID:    user.ID,
        Method:    req.AccountKeyWrap.Method,
        Wrapped:   req.AccountKeyWrap.Wrapped,
        Nonce:     req.AccountKeyWrap.Nonce,
        Salt:      req.AccountKeyWrap.Salt,
        KDFParams: req.AccountKeyWrap.KDFParams,
    }); err != nil {
        http.Error(w, "internal: store wrap", http.StatusInternalServerError)
        return
    }

    // Mint session token using existing helper from auth.go
    token, err := mintSessionToken(ctx, h.store, user.ID)
    if err != nil {
        http.Error(w, "internal: token", http.StatusInternalServerError)
        return
    }

    _ = json.NewEncoder(w).Encode(registerFinalizeResponse{
        UserID:       user.ID,
        SessionToken: token,
    })
}
```

(`mintSessionToken` is a thin wrapper around the existing token-issuance code in `auth.go` — extract whatever the current `handleLogin` uses; consolidate so both new and old paths share it.)

- [ ] **Step 4: Run tests**

```bash
go test ./internal/relay/ -run TestRegisterFinalize -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/opaque_auth.go internal/relay/opaque_auth_test.go internal/userstore/users.go
git commit -m "feat(relay): implement OPAQUE register finalize endpoint"
```

---

## Task 9: Implement `handleLoginInit` with multi-step session state

**Files:**
- Modify: `internal/relay/opaque_auth.go`
- Modify: `internal/relay/opaque_auth_test.go`

Login is two HTTP round-trips: `init` (server → KE2), `finalize` (server verifies KE3 + returns wrap). Between the two, the server must remember the OPAQUE login state for this email. Use an in-memory `sync.Map` keyed by a fresh `session_id` (separate from auth session tokens), with a 30-second TTL janitor.

- [ ] **Step 1: Add login-session state to handler**

In `opaque_auth.go`, add to the struct:

```go
type OpaqueAuthHandler struct {
    store         *userstore.SQLiteStore
    srv           *OpaqueServer
    loginSessions sync.Map // session_id -> *loginPending
}

type loginPending struct {
    email     string
    userID    string
    expectKE2 []byte // KE2 we sent — kept only for replay/log; the actual server state we hold is the *opaque.Server below
    server    *opaque.Server
    expiresAt time.Time
}
```

Add a `time.AfterFunc(30*time.Second, ...)` cleanup per inserted entry.

- [ ] **Step 2: Write failing test**

```go
func TestLoginInit_ReturnsKE2AndSessionID(t *testing.T) {
    h, conf := newTestHandler(t)
    // First register a user.
    user := registerUserForTest(t, h, conf, "alice@example.com", "hunter2")

    client, _ := conf.Client()
    ke1 := client.LoginInit([]byte("hunter2"))

    body, _ := json.Marshal(loginInitRequest{Email: "alice@example.com", LoginKE: ke1.Serialize()})
    req := httptest.NewRequest(http.MethodPost, "/api/auth/login/init", bytes.NewReader(body))
    rec := httptest.NewRecorder()
    h.handleLoginInit(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
    }
    var resp loginInitResponse
    if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if resp.SessionID == "" || len(resp.LoginResponse) == 0 {
        t.Fatalf("missing session_id or login_response: %+v", resp)
    }
    _ = user
}
```

(`registerUserForTest` is a helper that runs Task 7 + 8 in-process; factor it out of the existing register tests.)

- [ ] **Step 3: Implement handler**

```go
func (h *OpaqueAuthHandler) handleLoginInit(w http.ResponseWriter, r *http.Request) {
    var req loginInitRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "bad json", http.StatusBadRequest)
        return
    }
    ctx := r.Context()

    user, err := h.store.GetUserByEmail(ctx, req.Email)
    if err != nil {
        // Constant-time error: do not leak that the user does not exist.
        // Still consume an OPAQUE round to keep timing similar.
        sv, _ := h.srv.newServer()
        // Build a dummy ke1 and ignore output — purely timing equalization.
        if dummy, derr := h.srv.conf.DeserializeKE1(req.LoginKE); derr == nil {
            _ = sv.LoginInit(dummy, []byte(req.Email), nil)
        }
        http.Error(w, "invalid credentials", http.StatusUnauthorized)
        return
    }

    record, err := h.store.GetOpaqueRecord(ctx, user.ID)
    if err != nil {
        http.Error(w, "invalid credentials", http.StatusUnauthorized)
        return
    }
    parsedRecord, err := h.srv.conf.DeserializeRegistrationRecord(record)
    if err != nil {
        http.Error(w, "internal: parse record", http.StatusInternalServerError)
        return
    }
    ke1, err := h.srv.conf.DeserializeKE1(req.LoginKE)
    if err != nil {
        http.Error(w, "bad login_ke", http.StatusBadRequest)
        return
    }

    sv, err := h.srv.newServer()
    if err != nil {
        http.Error(w, "internal: server", http.StatusInternalServerError)
        return
    }
    ke2 := sv.LoginInit(ke1, []byte(req.Email), parsedRecord)

    sessionID := uuid.NewString()
    h.loginSessions.Store(sessionID, &loginPending{
        email:     req.Email,
        userID:    user.ID,
        server:    sv,
        expiresAt: time.Now().Add(30 * time.Second),
    })
    time.AfterFunc(30*time.Second, func() { h.loginSessions.Delete(sessionID) })

    _ = json.NewEncoder(w).Encode(loginInitResponse{
        LoginResponse: ke2.Serialize(),
        SessionID:     sessionID,
    })
}
```

- [ ] **Step 4: Run test**

```bash
go test ./internal/relay/ -run TestLoginInit -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/opaque_auth.go internal/relay/opaque_auth_test.go
git commit -m "feat(relay): implement OPAQUE login init endpoint"
```

---

## Task 10: Implement `handleLoginFinalize`

**Files:**
- Modify: `internal/relay/opaque_auth.go`
- Modify: `internal/relay/opaque_auth_test.go`

`login/finalize` verifies KE3, returns a session token and the wrapped `account_key` so the client can decrypt it locally.

- [ ] **Step 1: Write failing test (full round-trip)**

```go
func TestLoginFinalize_FullRoundTrip(t *testing.T) {
    h, conf := newTestHandler(t)
    _ = registerUserForTest(t, h, conf, "alice@example.com", "hunter2")

    client, _ := conf.Client()
    ke1 := client.LoginInit([]byte("hunter2"))
    body1, _ := json.Marshal(loginInitRequest{Email: "alice@example.com", LoginKE: ke1.Serialize()})
    req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login/init", bytes.NewReader(body1))
    rec1 := httptest.NewRecorder()
    h.handleLoginInit(rec1, req1)
    var initResp loginInitResponse
    _ = json.NewDecoder(rec1.Body).Decode(&initResp)

    ke2, err := conf.DeserializeKE2(initResp.LoginResponse)
    if err != nil {
        t.Fatalf("ke2: %v", err)
    }
    ke3, exportKey, err := client.LoginFinalize(ke2, []byte("alice@example.com"), []byte("atterm-relay"))
    if err != nil {
        t.Fatalf("LoginFinalize: %v", err)
    }
    _ = exportKey

    body2, _ := json.Marshal(loginFinalizeRequest{
        Email:     "alice@example.com",
        SessionID: initResp.SessionID,
        LoginKE3:  ke3.Serialize(),
    })
    req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login/finalize", bytes.NewReader(body2))
    rec2 := httptest.NewRecorder()
    h.handleLoginFinalize(rec2, req2)

    if rec2.Code != http.StatusOK {
        t.Fatalf("status = %d, body=%s", rec2.Code, rec2.Body.String())
    }
    var resp loginFinalizeResponse
    _ = json.NewDecoder(rec2.Body).Decode(&resp)
    if resp.SessionToken == "" {
        t.Fatalf("no session token")
    }
    if len(resp.AccountKeyWrap.Wrapped) == 0 {
        t.Fatalf("no wrap")
    }
}
```

- [ ] **Step 2: Implement handler**

```go
func (h *OpaqueAuthHandler) handleLoginFinalize(w http.ResponseWriter, r *http.Request) {
    var req loginFinalizeRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "bad json", http.StatusBadRequest)
        return
    }
    raw, ok := h.loginSessions.LoadAndDelete(req.SessionID)
    if !ok {
        http.Error(w, "session expired", http.StatusUnauthorized)
        return
    }
    pending := raw.(*loginPending)
    if pending.email != req.Email || time.Now().After(pending.expiresAt) {
        http.Error(w, "session expired", http.StatusUnauthorized)
        return
    }

    ke3, err := h.srv.conf.DeserializeKE3(req.LoginKE3)
    if err != nil {
        http.Error(w, "bad login_ke3", http.StatusBadRequest)
        return
    }
    if err := pending.server.LoginFinish(ke3); err != nil {
        http.Error(w, "invalid credentials", http.StatusUnauthorized)
        return
    }

    ctx := r.Context()
    wrap, err := h.store.GetAccountKeyWrap(ctx, pending.userID, "password")
    if err != nil {
        http.Error(w, "internal: wrap", http.StatusInternalServerError)
        return
    }
    token, err := mintSessionToken(ctx, h.store, pending.userID)
    if err != nil {
        http.Error(w, "internal: token", http.StatusInternalServerError)
        return
    }

    _ = json.NewEncoder(w).Encode(loginFinalizeResponse{
        UserID:       pending.userID,
        SessionToken: token,
        AccountKeyWrap: accountKeyWrapPayload{
            Method:    wrap.Method,
            Wrapped:   wrap.Wrapped,
            Nonce:     wrap.Nonce,
            Salt:      wrap.Salt,
            KDFParams: wrap.KDFParams,
        },
    })
}
```

- [ ] **Step 3: Run test**

```bash
go test ./internal/relay/ -run TestLoginFinalize -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/relay/opaque_auth.go internal/relay/opaque_auth_test.go
git commit -m "feat(relay): implement OPAQUE login finalize endpoint"
```

---

## Task 11: Wire OPAQUE handler into the relay server

**Files:**
- Modify: `internal/relay/server.go`
- Modify: `cmd/atterm-relay/main.go`

- [ ] **Step 1: Wire singleton in `main.go`**

Locate the boot sequence where `userstore.Open` returns the store. Immediately after, add:

```go
opaqueSrv, err := relay.LoadOrInitOpaqueServer(ctx, store)
if err != nil {
    log.Fatalf("opaque server init: %v", err)
}
```

Pass `opaqueSrv` into whatever constructor builds `relay.Server` / route registration.

- [ ] **Step 2: Register routes in `server.go`**

In the function that builds the HTTP mux (currently registers `/api/auth/*` routes), add:

```go
opaqueAuth := relay.NewOpaqueAuthHandler(store, opaqueSrv)
opaqueAuth.Register(mux)
```

Remove the four old route lines for `/api/auth/signup` and `/api/auth/login`.

- [ ] **Step 3: Smoke test the server boots**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/relay/server.go cmd/atterm-relay/main.go
git commit -m "feat(relay): wire OPAQUE auth handler into server boot"
```

---

## Task 12: Delete legacy password auth code

**Files:**
- Delete: `internal/relay/auth_http.go` (`handleSignup`, `handleLogin`, related helpers)
- Modify: `internal/userstore/users.go` — delete `hashPassword`, `verifyPassword`, `HashPasswordForBootstrap`, `VerifyPasswordForBootstrap`, `CreateUser(email, password)`, `VerifyPassword`, `ChangePassword`, `ResetPasswordByEmail`
- Modify: `internal/relay/argon2pool*.go` — delete entirely (no longer needed)
- Modify: `internal/relay/server_test.go`, `internal/userstore/users_test.go` — delete tests referencing deleted symbols
- Modify: `cmd/atterm-relay/bootstrap_admin.go` and `cmd/atterm-relay/bootstrap_password_strength.go` — see Task 13

- [ ] **Step 1: Run compile + identify all callsites**

```bash
go build ./... 2>&1 | head -50
```

Expected output: list of references to deleted symbols across the codebase.

- [ ] **Step 2: Delete files**

```bash
git rm internal/relay/auth_http.go internal/relay/argon2pool.go internal/relay/argon2pool_test.go
```

- [ ] **Step 3: Strip deleted symbols from `users.go`**

Delete the listed functions (search for each name; remove the entire function definition).

- [ ] **Step 4: Strip references from tests**

Walk each compile error from Step 1 and delete the failing test bodies (they tested the old auth path; not needed under OPAQUE).

- [ ] **Step 5: Compile**

```bash
go build ./...
go test ./internal/relay/ ./internal/userstore/ -count=1
```

Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(relay): remove legacy bcrypt/argon2 auth code"
```

---

## Task 13: Migrate bootstrap admin to OPAQUE

**Files:**
- Modify: `cmd/atterm-relay/bootstrap_admin.go`
- Modify: `cmd/atterm-relay/bootstrap_admin_test.go`

The bootstrap admin creates the operator account on first boot. It can no longer take a password because OPAQUE requires a client-side protocol round. Instead, bootstrap_admin now generates a one-time **bootstrap token**: a long random string the operator uses to claim the admin account by completing OPAQUE registration via the regular `/api/auth/register/*` endpoints.

- [ ] **Step 1: Write failing test**

```go
func TestBootstrapAdmin_GeneratesClaimToken(t *testing.T) {
    store := userstore.NewTestStore(t)
    token, email, err := BootstrapAdmin(context.Background(), store, "admin@example.com")
    if err != nil {
        t.Fatalf("BootstrapAdmin: %v", err)
    }
    if token == "" || email != "admin@example.com" {
        t.Fatalf("unexpected: token=%q email=%q", token, email)
    }

    // Verify token row persisted in claim_tokens table.
    user, err := store.LookupClaimToken(context.Background(), token)
    if err != nil {
        t.Fatalf("LookupClaimToken: %v", err)
    }
    if user.Email != email {
        t.Fatalf("token user mismatch")
    }
}
```

- [ ] **Step 2: Add `claim_tokens` table**

Append to `internal/userstore/migrations/0003_opaque_auth.sql` (NOT a new migration — same file, the migration system reads each `.sql` file once):

```sql
CREATE TABLE claim_tokens (
    token_hash TEXT PRIMARY KEY,
    email      TEXT NOT NULL,
    role       TEXT NOT NULL,         -- "admin" for bootstrap
    expires_at INTEGER NOT NULL,
    consumed_at INTEGER
);
```

(If migration 0003 has already shipped via Task 2 commit, create `0004_claim_tokens.sql` instead. Engineer chooses based on whether Task 2 has been pushed.)

- [ ] **Step 3: Add store methods**

In `internal/userstore/claim_tokens.go` (new file):

```go
// CreateClaimToken returns a one-time token the operator uses to complete
// OPAQUE registration as the named email + role.
func (s *SQLiteStore) CreateClaimToken(ctx context.Context, email, role string, ttl time.Duration) (string, error) { ... }

// LookupClaimToken returns the email + role for a token, or
// ErrClaimTokenNotFound. Does NOT consume the token (consumption happens
// when registration completes; the handler calls ConsumeClaimToken).
func (s *SQLiteStore) LookupClaimToken(ctx context.Context, plaintext string) (ClaimTokenRow, error) { ... }

func (s *SQLiteStore) ConsumeClaimToken(ctx context.Context, plaintext string) error { ... }
```

(Follow the same `sha256 + hex` pattern as `pairing.go`.)

- [ ] **Step 4: Rewrite `BootstrapAdmin`**

```go
func BootstrapAdmin(ctx context.Context, store *userstore.SQLiteStore, email string) (token string, _ string, err error) {
    token, err = store.CreateClaimToken(ctx, email, "admin", 7*24*time.Hour)
    if err != nil {
        return "", "", err
    }
    return token, email, nil
}
```

- [ ] **Step 5: Update register/finalize to honor claim tokens**

In `opaque_auth.go` `handleRegisterFinalize`, accept an optional `claim_token` field in `registerFinalizeRequest`. When present:

1. Look up the token via `LookupClaimToken`.
2. Verify `token.email == req.Email`.
3. Promote the new user to admin (`UPDATE users SET role = ?`) — assumes existing role column; if not present, add one in 0003.
4. `ConsumeClaimToken`.

- [ ] **Step 6: Run tests**

```bash
go test ./cmd/atterm-relay/ ./internal/userstore/ ./internal/relay/ -count=1
```

Expected: all green.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat(bootstrap): claim-token admin flow for OPAQUE registration"
```

---

## Task 14: End-to-end OPAQUE round-trip integration test

**Files:**
- Create: `internal/relay/opaque_e2e_test.go`

Run register + login on a single real HTTP server. Asserts:
1. Registration with password `hunter2` succeeds and persists records.
2. Login with `hunter2` succeeds and returns the same wrap.
3. Login with `wrong-password` fails with 401.

- [ ] **Step 1: Write the e2e test**

```go
package relay

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/attson/atterm/internal/userstore"
)

func TestOPAQUE_FullRegisterAndLogin(t *testing.T) {
    store := userstore.NewTestStore(t)
    srv, err := LoadOrInitOpaqueServer(context.Background(), store)
    if err != nil { t.Fatal(err) }
    h := NewOpaqueAuthHandler(store, srv)

    mux := http.NewServeMux()
    h.Register(mux)
    ts := httptest.NewServer(mux)
    defer ts.Close()

    conf := defaultConfig()
    client, _ := conf.Client()

    // ---- registration ----
    ke1 := client.RegistrationInit([]byte("hunter2"))
    initBody, _ := json.Marshal(registerInitRequest{Email: "alice@example.com", RegistrationKE: ke1.Serialize()})
    initResp, _ := http.Post(ts.URL+"/api/auth/register/init", "application/json", bytes.NewReader(initBody))
    var initR registerInitResponse
    json.NewDecoder(initResp.Body).Decode(&initR)
    initResp.Body.Close()

    ke2, _ := conf.DeserializeRegistrationResponse(initR.RegistrationResponse)
    record, _ := client.RegistrationFinalize(ke2, []byte("alice@example.com"), []byte("atterm-relay"))
    finBody, _ := json.Marshal(registerFinalizeRequest{
        Email:              "alice@example.com",
        RegistrationRecord: record.Serialize(),
        AccountKeyWrap: accountKeyWrapPayload{
            Method:    "password",
            Wrapped:   []byte("ct"),
            Nonce:     []byte("nonce-24-byte-fixed-aaaa"),
            Salt:      []byte("salt-16-byteAA"),
            KDFParams: `{"alg":"argon2id"}`,
        },
    })
    finResp, _ := http.Post(ts.URL+"/api/auth/register/finalize", "application/json", bytes.NewReader(finBody))
    if finResp.StatusCode != 200 { t.Fatalf("register: %d", finResp.StatusCode) }

    // ---- login ok ----
    clientL, _ := conf.Client()
    ke1L := clientL.LoginInit([]byte("hunter2"))
    initBodyL, _ := json.Marshal(loginInitRequest{Email: "alice@example.com", LoginKE: ke1L.Serialize()})
    rL1, _ := http.Post(ts.URL+"/api/auth/login/init", "application/json", bytes.NewReader(initBodyL))
    var ir loginInitResponse
    json.NewDecoder(rL1.Body).Decode(&ir)
    rL1.Body.Close()
    ke2L, _ := conf.DeserializeKE2(ir.LoginResponse)
    ke3L, _, err := clientL.LoginFinalize(ke2L, []byte("alice@example.com"), []byte("atterm-relay"))
    if err != nil { t.Fatalf("client LoginFinalize: %v", err) }
    finBodyL, _ := json.Marshal(loginFinalizeRequest{
        Email:     "alice@example.com",
        SessionID: ir.SessionID,
        LoginKE3:  ke3L.Serialize(),
    })
    rL2, _ := http.Post(ts.URL+"/api/auth/login/finalize", "application/json", bytes.NewReader(finBodyL))
    if rL2.StatusCode != 200 { t.Fatalf("login: %d", rL2.StatusCode) }
    var lr loginFinalizeResponse
    json.NewDecoder(rL2.Body).Decode(&lr)
    if string(lr.AccountKeyWrap.Wrapped) != "ct" { t.Fatalf("wrap mismatch") }

    // ---- login wrong password ----
    clientW, _ := conf.Client()
    ke1W := clientW.LoginInit([]byte("wrong"))
    initBodyW, _ := json.Marshal(loginInitRequest{Email: "alice@example.com", LoginKE: ke1W.Serialize()})
    rW1, _ := http.Post(ts.URL+"/api/auth/login/init", "application/json", bytes.NewReader(initBodyW))
    var iw loginInitResponse
    json.NewDecoder(rW1.Body).Decode(&iw)
    rW1.Body.Close()
    ke2W, _ := conf.DeserializeKE2(iw.LoginResponse)
    ke3W, _, err := clientW.LoginFinalize(ke2W, []byte("alice@example.com"), []byte("atterm-relay"))
    if err != nil {
        // Client-side LoginFinalize may itself fail for wrong password — that's also acceptable.
        return
    }
    finBodyW, _ := json.Marshal(loginFinalizeRequest{
        Email:     "alice@example.com",
        SessionID: iw.SessionID,
        LoginKE3:  ke3W.Serialize(),
    })
    rW2, _ := http.Post(ts.URL+"/api/auth/login/finalize", "application/json", bytes.NewReader(finBodyW))
    if rW2.StatusCode != 401 { t.Fatalf("expected 401 on wrong password, got %d", rW2.StatusCode) }
}
```

- [ ] **Step 2: Run e2e test**

```bash
go test ./internal/relay/ -run TestOPAQUE_FullRegisterAndLogin -v
```

Expected: PASS.

- [ ] **Step 3: Run full test suite**

```bash
go test ./...
```

Expected: all green.

- [ ] **Step 4: Commit**

```bash
git add internal/relay/opaque_e2e_test.go
git commit -m "test(relay): end-to-end OPAQUE register+login integration"
```

---

## Self-review checklist (run before handing off to subagent-driven-development)

- [ ] All 14 tasks have concrete code or test bodies; no "TBD" / "similar to" placeholders
- [ ] Every task ends in a green `go test` step and a commit
- [ ] Spec §4 (account-level key) is covered by Tasks 2, 4, 5, 7, 8, 10
- [ ] Spec §4.1 (OPAQUE) is covered by Tasks 1, 3, 6, 7, 8, 9, 10
- [ ] Spec §4.3 (admin reset) is partially out of scope — covered conceptually by deletion of `user_opaque_records` row + new wrap row. Test added in M1f.
- [ ] No frame-level changes (deliberate — M1a is auth only)
- [ ] `feedback_no_backward_compat` honored: Task 2 drops `password_hash`, Task 12 deletes legacy code

## Out of scope (do not implement here)

- Argon2id wrapping of `account_key` on the client side — handled in M1c (desktop Go SDK)
- TS/JS client SDK — handled in M1d
- UI flows — handled in M1e
- `GET /api/me/key` / `PUT /api/me/key` endpoints for password-change wrap rotation — handled in M1b
- Argon2 parameter calibration on mobile — flagged in spec §15 open question 2

## Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-15-relay-e2ee-m1a-server-opaque.md`. Two execution options:

1. **Subagent-Driven (recommended)** — fresh subagent per task, two-stage review per task. Use `superpowers:subagent-driven-development`.
2. **Inline Execution** — execute tasks here with checkpoints. Use `superpowers:executing-plans`.

Engineer choice for next session.
