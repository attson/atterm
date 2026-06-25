# Relay realm 身份 + E2EE 重锚定实现计划(阶段二 · 子项目 A)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** relay 暴露一个稳定的集群级 `realm_id`(共享 DB,生成一次),认证 finalize 响应下发它,桌面把 E2EE `account_key` 按 `realm_id` 锚定(而非物理 relay origin),使密钥跨节点/域名可移植。

**Architecture:** 新增 `relay_realm_state` 单例表(沿用 `opaque_server_state` 模式),`LoadOrInitRealm` 在启动时 first-writer-wins 生成或读取(可选 `ATTERM_RELAY_REALM_ID` env pin,与 DB 冲突则启动报错)。realm_id 流入 OPAQUE 认证处理器,注册/登录 finalize 响应携带它;e2eeclient 解析进 `LoginResult.RealmID`;桌面 keychain 账户名从 `{origin}|{userID}` 改为 `{realmID}|{userID}`,config 持久化 `RelayRealmID` 以便启动离线解锁。**无存量迁移**(无现有用户)。

**Tech Stack:** Go,既有 userstore 双后端 + dialect,`github.com/google/uuid`(已是依赖),桌面 Wails + safekeyring,前端 TS。

## Global Constraints

- **依赖 Plan 1/2/3**(本分支栈在 Plan 3 之上):userstore 双后端、dialect `Rebind`、分方言迁移 `migrations/{sqlite,postgres}/`、契约测试基建;`OpaqueServer`/`LoadOrInitOpaqueServer` 单例先例。
- **realm_id 不可变**:生成一次、永不变更。多节点靠共享 DB 自动一致(无需逐节点配);`ATTERM_RELAY_REALM_ID` 仅特殊场景 pin,与 DB 已存值不一致 → **启动 fatal**。
- **无存量迁移**(YAGNI,无现有用户):桌面直接按 realm 锚定;旧 `{origin}|{userID}` 槽不迁移。
- **不破坏现有行为**:每个 task 结束 `GOTOOLCHAIN=local go test ./internal/... ./cmd/... ./desktop/...` 全过(desktop/hookinstall 预存基线失败除外);Postgres 契约用例需 `ATTERM_TEST_PG_DSN`(未设则 skip)。
- **Go 1.23**:`go.mod` `go 1.23.0` 不变,无新依赖。
- **迁移号**:sqlite `0008_relay_realm.sql`、postgres `0007_relay_realm.sql`(本分支当前最新分别为 0007_relay_config / 0006_relay_config)。
- **移动/Web 不改密钥存储**:仅解析并(移动端)存下 `realm_id`。

---

## File Structure

- `internal/userstore/migrations/{sqlite/0008,postgres/0007}_relay_realm.sql`(新增)
- `internal/userstore/realm.go`(新增):`RealmState`、`ErrRealmStateMissing`、`GetRealmState`、`EnsureRealmState`
- `internal/userstore/store.go`(修改):Store 接口加方法
- `internal/userstore/contract_test.go`(修改):realm 契约子测试
- `internal/relay/realm.go`(新增):`LoadOrInitRealm`、`newRealmID`
- `internal/relay/realm_test.go`(新增)
- `internal/relay/server.go`(修改):Config 加 `RealmID`,传入 `NewOpaqueAuthHandler`
- `internal/relay/opaque_auth.go`(修改):handler 持 realmID,register/login finalize 响应加 `realm_id`
- `cmd/atterm-relay/main.go`(修改):`ATTERM_RELAY_REALM_ID` env + `LoadOrInitRealm` + 设 `cfg.RealmID`
- `internal/e2eeclient/client.go`(修改):`LoginResult.RealmID` + wire + 解析
- `desktop/account_key_store.go`(修改):realm 锚定;`desktop/account_key_store_test.go`(修改)
- `desktop/config.go`(修改):`RelayRealmID`
- `desktop/app.go`(修改):启动加载、两条登录路径持久化、退役 URL 迁移块、`persistAccountKey`
- `desktop/frontend/src/platform/capacitor.ts`(修改)、Web 登录解析(修改)

---

### Task 1: userstore relay_realm_state + RealmState 方法

**Files:**
- Create: `internal/userstore/migrations/sqlite/0008_relay_realm.sql`、`internal/userstore/migrations/postgres/0007_relay_realm.sql`
- Create: `internal/userstore/realm.go`
- Modify: `internal/userstore/store.go`（Store 接口）
- Modify: `internal/userstore/contract_test.go`

**Interfaces:**
- Produces:
  - `type RealmState struct { RealmID string; CreatedAt time.Time }`
  - `var ErrRealmStateMissing error`
  - `func (s *DBStore) GetRealmState(ctx) (RealmState, error)` — 无行返回 `ErrRealmStateMissing`。
  - `func (s *DBStore) EnsureRealmState(ctx, candidateRealmID string) (RealmState, error)` — first-writer-wins:`INSERT ... ON CONFLICT(id) DO NOTHING` 后 `GetRealmState`;已有行则忽略 candidate,返回既有值。

- [ ] **Step 1: 写迁移**

Create `internal/userstore/migrations/sqlite/0008_relay_realm.sql`:
```sql
-- 0008_relay_realm.sql — stable cluster realm identity (singleton).
CREATE TABLE relay_realm_state (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    realm_id   TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);
```
Create `internal/userstore/migrations/postgres/0007_relay_realm.sql`:
```sql
-- 0007_relay_realm.sql (postgres) — stable cluster realm identity (singleton).
CREATE TABLE relay_realm_state (
    id         BIGINT PRIMARY KEY CHECK (id = 1),
    realm_id   TEXT   NOT NULL,
    created_at BIGINT NOT NULL
);
```

- [ ] **Step 2: 写失败测试**

Add to `internal/userstore/contract_test.go` inside `runStoreContract`:
```go
	t.Run("realm state ensure is first-writer-wins", func(t *testing.T) {
		st := open(t)
		if _, err := st.GetRealmState(ctx); err != ErrRealmStateMissing {
			t.Fatalf("fresh realm: want ErrRealmStateMissing, got %v", err)
		}
		rs1, err := st.EnsureRealmState(ctx, "realm-A")
		if err != nil || rs1.RealmID != "realm-A" {
			t.Fatalf("ensure A: %v %+v", err, rs1)
		}
		// Second ensure with a different candidate must NOT overwrite.
		rs2, err := st.EnsureRealmState(ctx, "realm-B")
		if err != nil || rs2.RealmID != "realm-A" {
			t.Fatalf("ensure B must keep A: %v %+v", err, rs2)
		}
		got, err := st.GetRealmState(ctx)
		if err != nil || got.RealmID != "realm-A" {
			t.Fatalf("get: %v %+v", err, got)
		}
	})
```

- [ ] **Step 3: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/userstore/ -run TestStoreContract/sqlite -v`
Expected: 编译失败（`undefined: RealmState` / `GetRealmState`）。

- [ ] **Step 4: 实现**

Create `internal/userstore/realm.go`:
```go
package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrRealmStateMissing is returned by GetRealmState when the singleton row
// has not been initialized yet (fresh relay).
var ErrRealmStateMissing = errors.New("userstore: realm state not initialized")

// RealmState is the stable cluster-wide realm identity (relay_realm_state,
// id=1). It is generated once and never changed; clients anchor their E2EE
// account_key to RealmID instead of the physical relay origin.
type RealmState struct {
	RealmID   string
	CreatedAt time.Time
}

// GetRealmState reads the singleton realm row, or ErrRealmStateMissing if absent.
func (s *DBStore) GetRealmState(ctx context.Context) (RealmState, error) {
	var (
		rs        RealmState
		createdAt int64
	)
	err := s.db.QueryRowContext(ctx, s.dia.Rebind(
		`SELECT realm_id, created_at FROM relay_realm_state WHERE id = 1`)).
		Scan(&rs.RealmID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RealmState{}, ErrRealmStateMissing
	}
	if err != nil {
		return RealmState{}, fmt.Errorf("get realm state: %w", err)
	}
	rs.CreatedAt = time.Unix(createdAt, 0)
	return rs, nil
}

// EnsureRealmState inserts the realm row (id=1) with candidateRealmID if no
// row exists yet, then returns the effective state. First writer wins: if a
// row already exists (or a concurrent node inserted first), candidateRealmID
// is ignored and the existing realm is returned. Concurrent first-boot nodes
// converge on a single realm_id.
func (s *DBStore) EnsureRealmState(ctx context.Context, candidateRealmID string) (RealmState, error) {
	if _, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`INSERT INTO relay_realm_state(id, realm_id, created_at)
		 VALUES (1, ?, ?)
		 ON CONFLICT(id) DO NOTHING`),
		candidateRealmID, time.Now().Unix()); err != nil {
		return RealmState{}, fmt.Errorf("ensure realm state: %w", err)
	}
	return s.GetRealmState(ctx)
}
```
Add to the `Store` interface in `internal/userstore/store.go` (near the relay-config methods):
```go
	// Cluster realm identity (singleton); anchors E2EE keys across nodes.
	GetRealmState(ctx context.Context) (RealmState, error)
	EnsureRealmState(ctx context.Context, candidateRealmID string) (RealmState, error)
```

- [ ] **Step 5: 运行,确认通过(两后端)**

Run:
```bash
ATTERM_TEST_PG_DSN='postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable' \
  GOTOOLCHAIN=local go test ./internal/userstore/ -run TestStoreContract -v
```
Expected: sqlite `realm state...` 子测试 PASS;postgres PASS（有 DSN）或 SKIP。

- [ ] **Step 6: 提交**

```bash
git add internal/userstore/migrations/ internal/userstore/realm.go internal/userstore/store.go internal/userstore/contract_test.go
git commit -m "feat(userstore): relay_realm_state singleton + RealmState store methods"
```

---

### Task 2: relay LoadOrInitRealm + 启动接线

**Files:**
- Create: `internal/relay/realm.go`、`internal/relay/realm_test.go`
- Modify: `internal/relay/server.go`（Config 加 `RealmID`）
- Modify: `cmd/atterm-relay/main.go`

**Interfaces:**
- Consumes: `userstore.GetRealmState`/`EnsureRealmState`/`ErrRealmStateMissing`（Task 1）。
- Produces:
  - `func LoadOrInitRealm(ctx context.Context, store userstore.Store, envRealmID string) (string, error)` — 返回有效 realm_id;env 与 DB 冲突返回错误。
  - relay `Server` 的 Config 新增 `RealmID string` 字段。

- [ ] **Step 1: 写失败测试**

Create `internal/relay/realm_test.go`:
```go
package relay

import (
	"context"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

func TestLoadOrInitRealm(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// First boot generates a realm.
	r1, err := LoadOrInitRealm(ctx, store, "")
	if err != nil || r1 == "" {
		t.Fatalf("first init: %v %q", err, r1)
	}
	// Idempotent: second call returns the same realm.
	r2, err := LoadOrInitRealm(ctx, store, "")
	if err != nil || r2 != r1 {
		t.Fatalf("idempotent: %v %q != %q", err, r2, r1)
	}
	// Matching env pin is accepted.
	r3, err := LoadOrInitRealm(ctx, store, r1)
	if err != nil || r3 != r1 {
		t.Fatalf("matching env: %v %q", err, r3)
	}
	// Conflicting env pin is a hard error.
	_, err = LoadOrInitRealm(ctx, store, "some-other-realm")
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting env: want conflict error, got %v", err)
	}
}

func TestLoadOrInitRealm_EnvPinFreshBoot(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	r, err := LoadOrInitRealm(ctx, store, "pinned-realm")
	if err != nil || r != "pinned-realm" {
		t.Fatalf("env pin on fresh boot: %v %q", err, r)
	}
}
```

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/relay/ -run TestLoadOrInitRealm -v`
Expected: 编译失败（`undefined: LoadOrInitRealm`）。

- [ ] **Step 3: 实现**

Create `internal/relay/realm.go`:
```go
package relay

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/attson/atterm/internal/userstore"
)

// newRealmID generates a fresh opaque realm identifier.
func newRealmID() string { return uuid.NewString() }

// LoadOrInitRealm returns the stable cluster realm_id, generating and
// persisting one on first boot. envRealmID (ATTERM_RELAY_REALM_ID) is an
// optional operator pin: if the DB already holds a different realm, this is a
// hard error to avoid silently orphaning every client's account_key.
// Concurrent first-boot nodes converge on one realm via EnsureRealmState.
func LoadOrInitRealm(ctx context.Context, store userstore.Store, envRealmID string) (string, error) {
	envRealmID = strings.TrimSpace(envRealmID)

	existing, err := store.GetRealmState(ctx)
	switch {
	case err == nil:
		if envRealmID != "" && envRealmID != existing.RealmID {
			return "", fmt.Errorf("ATTERM_RELAY_REALM_ID %q conflicts with persisted realm %q", envRealmID, existing.RealmID)
		}
		return existing.RealmID, nil
	case errors.Is(err, userstore.ErrRealmStateMissing):
		candidate := envRealmID
		if candidate == "" {
			candidate = newRealmID()
		}
		rs, err := store.EnsureRealmState(ctx, candidate)
		if err != nil {
			return "", err
		}
		if envRealmID != "" && rs.RealmID != envRealmID {
			return "", fmt.Errorf("ATTERM_RELAY_REALM_ID %q conflicts with concurrently-initialized realm %q", envRealmID, rs.RealmID)
		}
		return rs.RealmID, nil
	default:
		return "", err
	}
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run: `GOTOOLCHAIN=local go test ./internal/relay/ -run TestLoadOrInitRealm -v`
Expected: PASS。

- [ ] **Step 5: Server Config 加 RealmID + main.go 接线**

In `internal/relay/server.go`, add a `RealmID string` field to the Server config struct (the same struct that has `OpaqueServer`, `BootstrapAdminEmail`, `Store` — search for `BootstrapAdminEmail` in server.go and add `RealmID string` beside it).

In `cmd/atterm-relay/main.go`, after `opaqueSrv, err := relay.LoadOrInitOpaqueServer(ctx, store)` (~line 102) and its error check, add:
```go
	realmID, err := relay.LoadOrInitRealm(ctx, store, os.Getenv("ATTERM_RELAY_REALM_ID"))
	if err != nil {
		log.Fatalf("init realm: %v", err)
	}
```
Then where the relay Server config is constructed (the struct literal setting `OpaqueServer: opaqueSrv`, `BootstrapAdminEmail: ...`), add `RealmID: realmID,`. (`os` and `log` are already imported.)

- [ ] **Step 6: 编译 + 回归 + 提交**

Run:
```bash
GOTOOLCHAIN=local go build ./... && GOTOOLCHAIN=local go test ./internal/relay/ ./cmd/...
```
Expected: PASS。

```bash
git add internal/relay/realm.go internal/relay/realm_test.go internal/relay/server.go cmd/atterm-relay/main.go
git commit -m "feat(relay): LoadOrInitRealm — generate-once cluster realm id + env pin"
```

---

### Task 3: 认证 finalize 响应下发 realm_id + e2eeclient 解析

**Files:**
- Modify: `internal/relay/opaque_auth.go`（handler 持 realmID;register/login finalize 响应加 `realm_id`）
- Modify: `internal/relay/server.go`（`NewOpaqueAuthHandler` 调用传 `cfg.RealmID`）
- Modify: `internal/e2eeclient/client.go`（`LoginResult.RealmID` + wire + 解析）
- Test: `internal/relay/opaque_auth_realm_test.go`（新增,或并入现有 opaque auth 测试）

**Interfaces:**
- Consumes: `cfg.RealmID`（Task 2）。
- Produces:
  - `loginFinalizeResponse` 与 `registerFinalizeResponse` 含 `RealmID string json:"realm_id"`。
  - `e2eeclient.LoginResult.RealmID string`,在 `Login` 与 `Register` 两路均填充。

- [ ] **Step 1: relay 端改动**

In `internal/relay/opaque_auth.go`:
- Add a `realmID string` field to the `OpaqueAuthHandler` struct (near `bootstrapEmail` at line ~39):
  ```go
  	realmID string
  ```
- Change `NewOpaqueAuthHandler` (line 67) to accept and store it:
  ```go
  func NewOpaqueAuthHandler(store *userstore.SQLiteStore, srv *OpaqueServer, bootstrapEmail, realmID string) *OpaqueAuthHandler {
  	return &OpaqueAuthHandler{store: store, srv: srv, bootstrapEmail: strings.TrimSpace(bootstrapEmail), realmID: realmID}
  }
  ```
- Add `RealmID` to both response structs:
  ```go
  type registerFinalizeResponse struct {
  	UserID       string `json:"user_id"`
  	SessionToken string `json:"session_token"`
  	RealmID      string `json:"realm_id"`
  }
  ```
  ```go
  type loginFinalizeResponse struct {
  	UserID         string                `json:"user_id"`
  	SessionToken   string                `json:"session_token"`
  	AccountKeyWrap accountKeyWrapPayload `json:"account_key_wrap"`
  	RealmID        string                `json:"realm_id"`
  }
  ```
- At the two encode sites, set `RealmID: h.realmID`:
  - register finalize encode (~line 325): add `RealmID: h.realmID,` to the `registerFinalizeResponse{...}` literal.
  - login finalize encode (~line 521): add `RealmID: h.realmID,` to the `loginFinalizeResponse{...}` literal.

In `internal/relay/server.go` (line ~234), pass the realm:
```go
				opaqueAuth := NewOpaqueAuthHandler(sqliteStore, cfg.OpaqueServer, cfg.BootstrapAdminEmail, cfg.RealmID)
```

- [ ] **Step 2: e2eeclient 改动**

In `internal/e2eeclient/client.go`:
- Add `RealmID string` to `LoginResult` (line ~97).
- Find the two wire response structs that `Register` (decodes near line 167) and `Login` (decodes near line 224) unmarshal into, and add `RealmID string json:"realm_id"` to each.
- In the `Register` result construction (~line 172) and the `Login` result construction (~line 234), add `RealmID: finResp.RealmID,`.

- [ ] **Step 3: 写测试**

Create `internal/relay/opaque_auth_realm_test.go`:
```go
package relay

import "testing"

// The handler must echo its configured realmID in finalize responses. Assert
// the wiring at the unit level by constructing the handler with a known realm
// and checking it is stored; the full HTTP round-trip is covered by the
// existing opaque auth tests once RealmID flows through.
func TestOpaqueAuthHandlerCarriesRealmID(t *testing.T) {
	h := NewOpaqueAuthHandler(nil, nil, "", "realm-xyz")
	if h.realmID != "realm-xyz" {
		t.Fatalf("realmID not stored: %q", h.realmID)
	}
}
```
Additionally, locate the existing end-to-end OPAQUE login test (e.g. in `internal/relay/` or `internal/e2eeclient/client_test.go`) that performs a real register+login round-trip against a test relay, and extend it to assert the login result's `RealmID` is non-empty and equals the relay's realm. If such a test exists in `internal/e2eeclient/client_test.go`, add an assertion `if res.RealmID == "" { t.Fatalf("expected realm id in login result") }` after a successful Login, and ensure the test relay is constructed with a realm (set `cfg.RealmID` or call `LoadOrInitRealm` in the test harness).

- [ ] **Step 4: 运行 + 回归**

Run:
```bash
GOTOOLCHAIN=local go build ./... && GOTOOLCHAIN=local go test ./internal/relay/ ./internal/e2eeclient/ ./desktop/ ./cmd/...
```
Expected: PASS（更新任何因 `NewOpaqueAuthHandler` 签名变化而编译失败的测试调用,补上 realm 实参,如 `""`）。

- [ ] **Step 5: 提交**

```bash
git add internal/relay/opaque_auth.go internal/relay/server.go internal/relay/opaque_auth_realm_test.go internal/e2eeclient/client.go
git commit -m "feat(relay,e2eeclient): carry realm_id in auth finalize responses"
```

---

### Task 4: 桌面 account_key 按 realm 锚定

**Files:**
- Modify: `desktop/account_key_store.go`、`desktop/account_key_store_test.go`
- Modify: `desktop/config.go`、`desktop/app.go`

**Interfaces:**
- Consumes: `e2eeclient.LoginResult.RealmID`（Task 3,经 desktop 的 relay client）。
- Produces: keychain 账户名 = `{realmID}|{userID}`;config `RelayRealmID` 持久化。

- [ ] **Step 1: account_key_store realm 锚定**

In `desktop/account_key_store.go`, rename the anchor parameter from origin to realm semantics. Change `accountKeyAccount` (line 33) — keep the function name and signature shape but the first parameter is now the realm id:
```go
// accountKeyAccount derives the keychain "account" name from the cluster
// realm id and user id. Anchoring on realm (not the physical relay origin)
// lets the account_key survive node/domain switches. Multiple realms on the
// same desktop stay isolated via the realm prefix.
func accountKeyAccount(realmID, userID string) string {
	realmID = strings.TrimSpace(realmID)
	userID = strings.TrimSpace(userID)
	if realmID == "" || userID == "" {
		return ""
	}
	return realmID + "|" + userID
}
```
The `loadAccountKey`/`saveAccountKey`/`clearAccountKeyFor` signatures keep `(string, string, ...)` — just update their parameter names and doc comments from `relayOrigin` to `realmID` (no body change beyond the renamed first arg passed to `accountKeyAccount`).

- [ ] **Step 2: 更新 account_key_store 测试**

In `desktop/account_key_store_test.go`, update `TestAccountKeyAccount_NamespacesByRelayAndUser` (line 79) to reflect realm semantics — rename to `TestAccountKeyAccount_NamespacesByRealmAndUser` and assert the account name is `{realm}|{user}`:
```go
func TestAccountKeyAccount_NamespacesByRealmAndUser(t *testing.T) {
	a := accountKeyAccount("realm-1", "user-1")
	b := accountKeyAccount("realm-2", "user-1")
	c := accountKeyAccount("realm-1", "user-2")
	if a == b || a == c || b == c {
		t.Fatalf("expected distinct account names, got %q %q %q", a, b, c)
	}
	if a != "realm-1|user-1" {
		t.Fatalf("account name = %q, want realm-1|user-1", a)
	}
}
```
The round-trip / missing / nil / clear tests (lines 20-77) keep working unchanged (they pass two strings; semantics are now realm+user).

- [ ] **Step 3: config RelayRealmID**

In `desktop/config.go`, add after `RelaySessionUserID` (line ~61):
```go
	// RelayRealmID is the relay cluster's stable realm id (from the login
	// response). The account_key is anchored to it in the keychain so it
	// survives relay node/domain switches.
	RelayRealmID string `json:"relay_realm_id,omitempty"`
```

- [ ] **Step 4: app.go 接线**

In `desktop/app.go`:
- Startup load (line ~288): change `loadAccountKey(cfg.RelayURL, cfg.RelaySessionUserID)` to `loadAccountKey(cfg.RelayRealmID, cfg.RelaySessionUserID)`.
- `persistAccountKey` (line ~758-766): change `saveAccountKey(cfg.RelayURL, cfg.RelaySessionUserID, key)` to `saveAccountKey(cfg.RelayRealmID, cfg.RelaySessionUserID, key)`.
- Login path 1 — `LoginRemoteRelay` (line ~608): add `cfg.RelayRealmID = res.RealmID` right beside `cfg.RelaySessionUserID = res.UserID`, before `a.cfgStore.Set(cfg)`.
- Login path 2 (line ~694): add `cfg.RelayRealmID = res.RealmID` beside `cfg.RelaySessionUserID = res.UserID`.
- Retire the relay-URL account_key migration block (lines ~531-540, the `if prevURL != cfg.RelayURL && cfg.RelaySessionUserID != "" { ... saveAccountKey ... clearAccountKeyFor ... }`): delete it. The key is now realm-anchored, so changing the relay URL no longer requires re-keying. If `prevURL` (line ~496) becomes unused after deletion, remove its declaration too (the compiler will flag it).

- [ ] **Step 5: 运行 + 回归**

Run:
```bash
GOTOOLCHAIN=local go build ./... && GOTOOLCHAIN=local go test ./desktop/
```
Expected: PASS（`desktop/hookinstall` 预存基线失败除外;`account_key_store_test` 更新后过）。注:本项目在 /Volumes 挂载卷上,验证前确认跑的是新代码（见 [[dev-hmr-volumes-mount]] —— 不过这是 `go test`,不受 wails dev HMR 影响）。

- [ ] **Step 6: 提交**

```bash
git add desktop/account_key_store.go desktop/account_key_store_test.go desktop/config.go desktop/app.go
git commit -m "feat(desktop): anchor account_key to realm_id instead of relay origin"
```

---

### Task 5: 移动 / Web 解析 realm_id

**Files:**
- Modify: `desktop/frontend/src/platform/capacitor.ts`
- Modify: Web 登录解析处（`web/src/shared/api/auth.ts` 或等价文件 — 实现时 grep `session_token` / `account_key_wrap` 定位 finalize 解析）

**Interfaces:**
- Consumes: relay 登录 finalize 响应的 `realm_id`（Task 3）。
- Produces: 移动端把 `realm_id` 存入会话配置(供子项目 C 用);不改 account_key 存储。

- [ ] **Step 1: 移动端解析 + 存储**

In `desktop/frontend/src/platform/capacitor.ts`:
- Extend the `finBody` type (line ~108) to include `realm_id: string`:
  ```ts
  const finBody = (await finRes.json()) as {
    user_id: string
    session_token: string
    account_key_wrap: AccountKeyWrap
    realm_id: string
  }
  ```
- Return `realm_id` from `opaqueLogin` (extend its return shape) and, in the `relay.login` flow that persists the session config (~line 386, where `secureStorage.set(STORAGE_KEY, JSON.stringify(cfg))` happens), include `realmId: finBody.realm_id` in the persisted `RelayConfig` blob. Add a `realmId?: string` field to the `RelayConfig` TS type used there. Do NOT change `ACCOUNT_KEY_KEY` storage.

- [ ] **Step 2: Web 解析**

In the web login finalize parser (grep `account_key_wrap` under `web/src/`), add `realm_id: string` to the parsed response type and store it alongside the existing relay/session config (the web layer's relay config object). Do NOT change `web/src/shared/api/account-key.ts` (account_key stays in sessionStorage as-is). If the web layer has no obvious place to keep `realm_id`, store it in the same `localStorage` relay config blob used for `baseURL`/`sessionToken`.

- [ ] **Step 3: 前端构建校验 + 提交**

Run the frontend build/typecheck used by the project (e.g. `npm run -s typecheck` or `vue-tsc --noEmit` in `desktop/frontend` and `web` as applicable — check each dir's package.json scripts). Expected: no type errors from the added fields.

```bash
git add desktop/frontend/src/platform/capacitor.ts web/src
git commit -m "feat(mobile,web): parse and store realm_id from login response"
```

---

## 收尾

- [ ] 全量回归(含 Postgres 契约):起 PG 容器,`ATTERM_TEST_PG_DSN=... GOTOOLCHAIN=local go test ./internal/... ./cmd/... ./desktop/...`(desktop/hookinstall 预存基线失败除外)。
- [ ] 手测:全新空库启动 relay → 日志/DB 确认 realm 生成一次;`ATTERM_RELAY_REALM_ID` 设冲突值 → 启动 fatal。
- [ ] 桌面手测:登录一次 → keychain 出现 `{realmID}|{userID}` 槽;重启 → 离线用 `RelayRealmID` 解锁成功。
- [ ] `docker rm -f atterm-pg` 清理测试容器。

## 后续(不在本 plan)

- **子项目 B** — 实例注册(`relay_instances` + 心跳)+ 账号级用户可选节点(`user_home`)+ 登录下发所选节点 URL + `/healthz` 实例身份。
- **子项目 C** — 三端节点选择器 UI(列表 + ping + 切换全账号生效)+ 连接路由到所选节点。
- 见设计 spec §7。
