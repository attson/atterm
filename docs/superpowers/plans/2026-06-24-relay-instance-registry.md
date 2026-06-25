# Relay 实例注册 + 账号级节点选择实现计划(阶段二 · 子项目 B)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** relay 侧建立实例注册表(心跳)+ 账号级用户可选节点(`user_home`),登录 finalize 下发 `home_instance_url`,提供 `GET /api/nodes` / `PUT /api/me/home`,`/healthz` 带实例身份。纯 relay,无客户端改动。

**Architecture:** 每节点用配置的 `ATTERM_RELAY_INSTANCE_PUBLIC_URL` 作 `instance_id`,周期性 UPSERT 心跳到共享 DB 的 `relay_instances`;`user_home` 存账号级节点选择;登录时解析 home(已选且存活→其 URL;已选但死→空让客户端重选;未选→自动落到登录所在节点)。

**Tech Stack:** Go,既有 userstore 双后端 + dialect,relay HTTP(Go 1.22 method-prefixed mux + `requireSession`)。

## Global Constraints

- **依赖 A(已栈在 A 之上)**:`loginFinalizeResponse` 已含 `realm_id`;`OpaqueAuthHandler` 已有 `realmID` 字段(`NewOpaqueAuthHandler(store, srv, bootstrapEmail, realmID)`);Server `Config` 已有 `RealmID`。
- **纯 relay,无客户端改动**:对现有单机客户端零行为变化(单机不配 `INSTANCE_PUBLIC_URL` → 不注册、登录 `home_instance_url` 为空,客户端忽略)。
- **`instance_id` = 节点的 `public_url`**(配置的对外域名);未配则不注册、不参与选择。
- **死节点登录 → 返回空 `home_instance_url`**(客户端 C 重选;不擅自搬迁)。
- **存活窗口 90s,心跳间隔 30s**(常量 `InstanceLivenessWindow`)。
- **不破坏现有行为**:每 task 结束 `GOTOOLCHAIN=local go test ./internal/... ./cmd/...` 全过;Postgres 契约用 `ATTERM_TEST_PG_DSN`(未设则 skip)。Go 1.23.0 不变,无新依赖。
- **迁移号**:sqlite `0009_node_selection.sql`、postgres `0008_node_selection.sql`(A 后最新为 0008_relay_realm / 0007_relay_realm)。

---

## File Structure

- `internal/userstore/migrations/{sqlite/0009,postgres/0008}_node_selection.sql`(新增):`relay_instances` + `user_home` 两表
- `internal/userstore/relay_instances.go`(新增):`RelayInstance` + `UpsertInstanceHeartbeat` + `ListLiveInstances`
- `internal/userstore/user_home.go`(新增):`GetUserHome` + `SetUserHome`
- `internal/userstore/store.go`(修改):Store 接口加方法
- `internal/userstore/contract_test.go`(修改):契约子测试
- `internal/relay/node_home.go`(新增):`InstanceLivenessWindow` 常量 + `resolveHomeInstanceURL`
- `internal/relay/nodes_http.go`(新增):`handleNodesHTTP`、`handleSetHomeHTTP`
- `internal/relay/server.go`(修改):`Config.InstancePublicURL`、注册 2 路由
- `internal/relay/opaque_auth.go`(修改):`loginFinalizeResponse.HomeInstanceURL`、handler 持 `instancePublicURL`、登录解析 home
- `internal/relay/health_http.go`(修改):/healthz 实例身份
- `cmd/atterm-relay/main.go`(修改):env + Config + 心跳 goroutine

---

### Task 1: userstore 节点选择存储层(两表 + 方法 + 契约测试)

**Files:**
- Create: `internal/userstore/migrations/sqlite/0009_node_selection.sql`、`internal/userstore/migrations/postgres/0008_node_selection.sql`
- Create: `internal/userstore/relay_instances.go`、`internal/userstore/user_home.go`
- Modify: `internal/userstore/store.go`、`internal/userstore/contract_test.go`

**Interfaces:**
- Produces:
  - `type RelayInstance struct { InstanceID, PublicURL string; LastHeartbeat int64 }`
  - `UpsertInstanceHeartbeat(ctx, instanceID, publicURL string, nowUnix int64) error`
  - `ListLiveInstances(ctx, minHeartbeat int64) ([]RelayInstance, error)` — `last_heartbeat >= minHeartbeat`,按 instance_id 升序。
  - `GetUserHome(ctx, userID string) (instanceID string, ok bool, err error)` — 无行 ok=false。
  - `SetUserHome(ctx, userID, instanceID string) error` — upsert。

- [ ] **Step 1: 写迁移**

Create `internal/userstore/migrations/sqlite/0009_node_selection.sql`:
```sql
-- 0009_node_selection.sql — relay instance registry + account-level home node.
CREATE TABLE relay_instances (
    instance_id    TEXT    PRIMARY KEY,
    public_url     TEXT    NOT NULL,
    last_heartbeat INTEGER NOT NULL
);

CREATE TABLE user_home (
    user_id     TEXT    PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    instance_id TEXT    NOT NULL,
    updated_at  INTEGER NOT NULL
);
```
Create `internal/userstore/migrations/postgres/0008_node_selection.sql`:
```sql
-- 0008_node_selection.sql (postgres)
CREATE TABLE relay_instances (
    instance_id    TEXT   PRIMARY KEY,
    public_url     TEXT   NOT NULL,
    last_heartbeat BIGINT NOT NULL
);

CREATE TABLE user_home (
    user_id     TEXT   PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    instance_id TEXT   NOT NULL,
    updated_at  BIGINT NOT NULL
);
```

- [ ] **Step 2: 写失败测试**

Add to `internal/userstore/contract_test.go` inside `runStoreContract`:
```go
	t.Run("instance heartbeat + live list", func(t *testing.T) {
		st := open(t)
		if err := st.UpsertInstanceHeartbeat(ctx, "https://a.example", "https://a.example", 1000); err != nil {
			t.Fatalf("upsert a: %v", err)
		}
		if err := st.UpsertInstanceHeartbeat(ctx, "https://b.example", "https://b.example", 500); err != nil {
			t.Fatalf("upsert b: %v", err)
		}
		// Re-heartbeat a (upsert updates last_heartbeat).
		if err := st.UpsertInstanceHeartbeat(ctx, "https://a.example", "https://a.example", 2000); err != nil {
			t.Fatalf("re-upsert a: %v", err)
		}
		live, err := st.ListLiveInstances(ctx, 1000) // cutoff excludes b (500)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(live) != 1 || live[0].InstanceID != "https://a.example" || live[0].LastHeartbeat != 2000 {
			t.Fatalf("live = %+v", live)
		}
	})

	t.Run("user home get/set", func(t *testing.T) {
		st := open(t)
		u, err := st.CreateOpaqueUser(ctx, "home@example.com")
		if err != nil {
			t.Fatalf("user: %v", err)
		}
		if _, ok, err := st.GetUserHome(ctx, u.ID); err != nil || ok {
			t.Fatalf("fresh home: ok=%v err=%v", ok, err)
		}
		if err := st.SetUserHome(ctx, u.ID, "https://a.example"); err != nil {
			t.Fatalf("set: %v", err)
		}
		id, ok, err := st.GetUserHome(ctx, u.ID)
		if err != nil || !ok || id != "https://a.example" {
			t.Fatalf("get: id=%q ok=%v err=%v", id, ok, err)
		}
		if err := st.SetUserHome(ctx, u.ID, "https://b.example"); err != nil {
			t.Fatalf("reset: %v", err)
		}
		id2, _, _ := st.GetUserHome(ctx, u.ID)
		if id2 != "https://b.example" {
			t.Fatalf("reset home = %q", id2)
		}
	})
```

- [ ] **Step 3: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/userstore/ -run TestStoreContract/sqlite -v`
Expected: 编译失败（`undefined: RelayInstance`/`UpsertInstanceHeartbeat`/`GetUserHome`）。

- [ ] **Step 4: 实现**

Create `internal/userstore/relay_instances.go`:
```go
package userstore

import (
	"context"
	"fmt"
)

// RelayInstance is one row of the relay_instances heartbeat registry. The
// instance_id is the node's configured public URL.
type RelayInstance struct {
	InstanceID    string
	PublicURL     string
	LastHeartbeat int64
}

// UpsertInstanceHeartbeat records (or refreshes) this node's liveness row.
func (s *DBStore) UpsertInstanceHeartbeat(ctx context.Context, instanceID, publicURL string, nowUnix int64) error {
	_, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`INSERT INTO relay_instances(instance_id, public_url, last_heartbeat)
		 VALUES (?, ?, ?)
		 ON CONFLICT(instance_id) DO UPDATE SET
		     public_url     = excluded.public_url,
		     last_heartbeat = excluded.last_heartbeat`),
		instanceID, publicURL, nowUnix)
	if err != nil {
		return fmt.Errorf("upsert instance heartbeat: %w", err)
	}
	return nil
}

// ListLiveInstances returns all instances whose last_heartbeat >= minHeartbeat,
// ordered by instance_id.
func (s *DBStore) ListLiveInstances(ctx context.Context, minHeartbeat int64) ([]RelayInstance, error) {
	rows, err := s.db.QueryContext(ctx, s.dia.Rebind(
		`SELECT instance_id, public_url, last_heartbeat
		 FROM relay_instances WHERE last_heartbeat >= ? ORDER BY instance_id`), minHeartbeat)
	if err != nil {
		return nil, fmt.Errorf("list live instances: %w", err)
	}
	defer rows.Close()
	var out []RelayInstance
	for rows.Next() {
		var inst RelayInstance
		if err := rows.Scan(&inst.InstanceID, &inst.PublicURL, &inst.LastHeartbeat); err != nil {
			return nil, fmt.Errorf("scan instance: %w", err)
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}
```
Create `internal/userstore/user_home.go`:
```go
package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetUserHome returns the user's selected home instance_id, ok=false if unset.
func (s *DBStore) GetUserHome(ctx context.Context, userID string) (string, bool, error) {
	var instanceID string
	err := s.db.QueryRowContext(ctx, s.dia.Rebind(
		`SELECT instance_id FROM user_home WHERE user_id = ?`), userID).Scan(&instanceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get user home: %w", err)
	}
	return instanceID, true, nil
}

// SetUserHome upserts the user's selected home instance (account-level).
func (s *DBStore) SetUserHome(ctx context.Context, userID, instanceID string) error {
	_, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`INSERT INTO user_home(user_id, instance_id, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		     instance_id = excluded.instance_id,
		     updated_at  = excluded.updated_at`),
		userID, instanceID, nowUnix())
	if err != nil {
		return fmt.Errorf("set user home: %w", err)
	}
	return nil
}
```
(`nowUnix()` was added to store.go in Plan 2; reuse it.)

Add to the `Store` interface in `internal/userstore/store.go` (near the realm methods):
```go
	// Instance registry + account-level home node (phase 2 node selection).
	UpsertInstanceHeartbeat(ctx context.Context, instanceID, publicURL string, nowUnix int64) error
	ListLiveInstances(ctx context.Context, minHeartbeat int64) ([]RelayInstance, error)
	GetUserHome(ctx context.Context, userID string) (string, bool, error)
	SetUserHome(ctx context.Context, userID, instanceID string) error
```

- [ ] **Step 5: 运行,确认通过(两后端)**

Run:
```bash
ATTERM_TEST_PG_DSN='postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable' \
  GOTOOLCHAIN=local go test ./internal/userstore/ -run TestStoreContract -v
```
Expected: 新子测试 sqlite + postgres PASS（无 DSN 则 postgres SKIP）。

- [ ] **Step 6: 提交**

```bash
git add internal/userstore/migrations/ internal/userstore/relay_instances.go internal/userstore/user_home.go internal/userstore/store.go internal/userstore/contract_test.go
git commit -m "feat(userstore): relay_instances heartbeat + user_home node selection"
```

---

### Task 2: 节点自注册(Config + env + 心跳 + /healthz 身份)

**Files:**
- Modify: `internal/relay/server.go`（`Config.InstancePublicURL`）
- Modify: `internal/relay/health_http.go`（/healthz 实例身份）
- Modify: `cmd/atterm-relay/main.go`（env + Config + 心跳 goroutine）
- Create: `internal/relay/node_home.go`（仅 `InstanceLivenessWindow` 常量这一步;`resolveHomeInstanceURL` 在 Task 3 加）

**Interfaces:**
- Consumes: `userstore.UpsertInstanceHeartbeat`（Task 1）。
- Produces: `Config.InstancePublicURL string`;`relay.InstanceLivenessWindow time.Duration`(=90s)。

- [ ] **Step 1: 加常量 + Config 字段**

Create `internal/relay/node_home.go`:
```go
package relay

import "time"

// InstanceLivenessWindow is how recent an instance's heartbeat must be to be
// considered live in node selection. Heartbeats are written every ~30s.
const InstanceLivenessWindow = 90 * time.Second
```
In `internal/relay/server.go`, add to the `Config` struct (after `RealmID` at ~line 89):
```go
	// InstancePublicURL is this node's client-reachable URL (also its
	// instance_id in the relay_instances registry). Empty disables node
	// registration / selection (single-instance/dev).
	InstancePublicURL string
```

- [ ] **Step 2: /healthz 实例身份**

In `internal/relay/health_http.go`, replace the `handleHealthz` response map:
```go
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"version":     s.cfg.Version,
		"instance_id": s.cfg.InstancePublicURL,
	})
}
```

- [ ] **Step 3: main.go env + Config + 心跳**

In `cmd/atterm-relay/main.go`:
- Read the env near the other env reads (after realmID at ~line 106):
  ```go
  instancePublicURL := strings.TrimSpace(os.Getenv("ATTERM_RELAY_INSTANCE_PUBLIC_URL"))
  ```
- Add to the `relay.Config{...}` literal (after `RealmID: realmID,` at ~line 200):
  ```go
  		InstancePublicURL: instancePublicURL,
  ```
- After the server is constructed and other periodic goroutines are started (mirror the Feishu sweep goroutine ~line 242), add a heartbeat goroutine guarded on non-empty URL:
  ```go
  	if instancePublicURL != "" {
  		// Immediate first heartbeat so the node is selectable without a 30s wait.
  		if err := store.UpsertInstanceHeartbeat(ctx, instancePublicURL, instancePublicURL, time.Now().Unix()); err != nil {
  			log.Printf("relay: initial instance heartbeat: %v", err)
  		}
  		go func() {
  			t := time.NewTicker(30 * time.Second)
  			defer t.Stop()
  			for {
  				select {
  				case <-t.C:
  					if err := store.UpsertInstanceHeartbeat(ctx, instancePublicURL, instancePublicURL, time.Now().Unix()); err != nil {
  						log.Printf("relay: instance heartbeat: %v", err)
  					}
  				case <-ctx.Done():
  					return
  				}
  			}
  		}()
  	}
  ```
  (`os`, `strings`, `log`, `time` already imported.)

- [ ] **Step 4: 编译 + 回归 + 提交**

Run:
```bash
GOTOOLCHAIN=local go build ./... && GOTOOLCHAIN=local go test ./internal/relay/ ./cmd/...
```
Expected: PASS（/healthz 现含 instance_id;无 INSTANCE_PUBLIC_URL 时为空字符串,行为不变）。

```bash
git add internal/relay/node_home.go internal/relay/server.go internal/relay/health_http.go cmd/atterm-relay/main.go
git commit -m "feat(relay): instance public URL config + heartbeat + /healthz identity"
```

---

### Task 3: 登录 finalize 下发 home_instance_url

**Files:**
- Modify: `internal/relay/opaque_auth.go`（`loginFinalizeResponse.HomeInstanceURL`、handler 持 `instancePublicURL`、解析 home）
- Modify: `internal/relay/node_home.go`（`resolveHomeInstanceURL`）
- Modify: `internal/relay/server.go`（`NewOpaqueAuthHandler` 调用传 `cfg.InstancePublicURL`)
- Test: `internal/relay/node_home_test.go`（新增）

**Interfaces:**
- Consumes: `userstore.GetUserHome`/`SetUserHome`/`ListLiveInstances`（Task 1）、`InstanceLivenessWindow`（Task 2）、`cfg.InstancePublicURL`。
- Produces: `loginFinalizeResponse.HomeInstanceURL`;`resolveHomeInstanceURL(ctx, store, userID, thisInstanceID string) (string, error)`。

- [ ] **Step 1: 写失败测试**

Create `internal/relay/node_home_test.go`:
```go
package relay

import (
	"context"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

func TestResolveHomeInstanceURL(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	u, _ := store.CreateOpaqueUser(ctx, "h@example.com")
	now := time.Now().Unix()

	// Single-instance (thisInstanceID empty) → empty home, no assignment.
	if url, err := resolveHomeInstanceURL(ctx, store, u.ID, ""); err != nil || url != "" {
		t.Fatalf("single-instance: %q %v", url, err)
	}
	if _, ok, _ := store.GetUserHome(ctx, u.ID); ok {
		t.Fatalf("single-instance must not assign a home")
	}

	// Unset home → auto-assign the serving node and return its URL.
	this := "https://this.example"
	url, err := resolveHomeInstanceURL(ctx, store, u.ID, this)
	if err != nil || url != this {
		t.Fatalf("auto-assign: %q %v", url, err)
	}
	if id, ok, _ := store.GetUserHome(ctx, u.ID); !ok || id != this {
		t.Fatalf("home not persisted: %q %v", id, ok)
	}

	// Already-selected + alive → returns its URL.
	_ = store.UpsertInstanceHeartbeat(ctx, this, this, now)
	if url, err := resolveHomeInstanceURL(ctx, store, u.ID, this); err != nil || url != this {
		t.Fatalf("selected+alive: %q %v", url, err)
	}

	// Selected node is DEAD (point home at a stale instance) → empty.
	_ = store.SetUserHome(ctx, u.ID, "https://dead.example")
	_ = store.UpsertInstanceHeartbeat(ctx, "https://dead.example", "https://dead.example", now-int64(InstanceLivenessWindow/time.Second)-100)
	other := "https://other.example"
	if url, err := resolveHomeInstanceURL(ctx, store, u.ID, other); err != nil || url != "" {
		t.Fatalf("dead home must return empty: %q %v", url, err)
	}
}
```

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/relay/ -run TestResolveHomeInstanceURL -v`
Expected: 编译失败（`undefined: resolveHomeInstanceURL`）。

- [ ] **Step 3: 实现 resolveHomeInstanceURL**

Append to `internal/relay/node_home.go`:
```go
import (
	"context"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// resolveHomeInstanceURL computes the home_instance_url for a login on the node
// identified by thisInstanceID (its public URL; "" for single-instance/dev).
//   - thisInstanceID == "": no node selection → "".
//   - user_home set and that instance is live → its public URL.
//   - user_home set but instance is dead → "" (client re-selects; we don't relocate).
//   - user_home unset → auto-assign the serving node and return its URL.
func resolveHomeInstanceURL(ctx context.Context, store userstore.Store, userID, thisInstanceID string) (string, error) {
	if thisInstanceID == "" {
		return "", nil
	}
	minHB := time.Now().Add(-InstanceLivenessWindow).Unix()
	live, err := store.ListLiveInstances(ctx, minHB)
	if err != nil {
		return "", err
	}
	liveURL := make(map[string]string, len(live)+1)
	for _, inst := range live {
		liveURL[inst.InstanceID] = inst.PublicURL
	}
	// The node serving this login is reachable by definition, even if its
	// heartbeat row hasn't been written yet. instance_id == public_url.
	liveURL[thisInstanceID] = thisInstanceID

	homeID, ok, err := store.GetUserHome(ctx, userID)
	if err != nil {
		return "", err
	}
	if ok {
		if url, alive := liveURL[homeID]; alive {
			return url, nil
		}
		return "", nil // selected node is dead
	}
	if err := store.SetUserHome(ctx, userID, thisInstanceID); err != nil {
		return "", err
	}
	return thisInstanceID, nil
}
```
(Update `node_home.go`'s import block to include the two new imports alongside `time`.)

- [ ] **Step 4: 接入 loginFinalizeResponse + handler**

In `internal/relay/opaque_auth.go`:
- Add `HomeInstanceURL string json:"home_instance_url"` to `loginFinalizeResponse` (after `RealmID`).
- Add an `instancePublicURL string` field to the `OpaqueAuthHandler` struct (beside `realmID`).
- Change `NewOpaqueAuthHandler` to take a 5th param:
  ```go
  func NewOpaqueAuthHandler(store *userstore.SQLiteStore, srv *OpaqueServer, bootstrapEmail, realmID, instancePublicURL string) *OpaqueAuthHandler {
  	return &OpaqueAuthHandler{store: store, srv: srv, bootstrapEmail: strings.TrimSpace(bootstrapEmail), realmID: realmID, instancePublicURL: instancePublicURL}
  }
  ```
- In `handleLoginFinalize`, before the `json.NewEncoder(w).Encode(loginFinalizeResponse{...})` (~line 528), resolve home:
  ```go
  	homeURL, err := resolveHomeInstanceURL(ctx, h.store, pending.userID, h.instancePublicURL)
  	if err != nil {
  		http.Error(w, "internal error", http.StatusInternalServerError)
  		return
  	}
  ```
  and add `HomeInstanceURL: homeURL,` to the response literal.

In `internal/relay/server.go` (~line 238), pass the instance URL:
```go
				opaqueAuth := NewOpaqueAuthHandler(sqliteStore, cfg.OpaqueServer, cfg.BootstrapAdminEmail, cfg.RealmID, cfg.InstancePublicURL)
```

- [ ] **Step 5: 更新被签名变化打破的调用方**

`NewOpaqueAuthHandler` 5→ now takes 5 args. Update every other caller to pass `""` for instancePublicURL (these tests don't exercise node selection):
- `internal/relay/opaque_stepup_test.go` (all `NewOpaqueAuthHandler(store, srv, "")` → add `, "", ""`)
- `internal/relay/opaque_auth_test.go` (the helper returning `NewOpaqueAuthHandler(store, srv, bootstrapEmail)` → add `, "", ""`)
- `internal/relay/opaque_e2e_test.go`
- `internal/relay/opaque_auth_realm_test.go` (`NewOpaqueAuthHandler(nil, nil, "", "realm-xyz")` → add `, ""`)
- `internal/e2eeclient/client_test.go` (the 3 `NewOpaqueAuthHandler(store, opaqueSrv, "", "test-realm")` → add `, ""`)
- `desktop/app_login_test.go` (the `newOPAQUERelay` constructing it → add `, ""`)
Run `GOTOOLCHAIN=local go build ./...` and fix any remaining call site the compiler flags.

- [ ] **Step 6: 运行 + 回归 + 提交**

Run:
```bash
GOTOOLCHAIN=local go test ./internal/relay/ -run TestResolveHomeInstanceURL -v
GOTOOLCHAIN=local go build ./... && GOTOOLCHAIN=local go test ./internal/relay/ ./internal/e2eeclient/ ./desktop/ ./cmd/...
```
Expected: PASS（`desktop/hookinstall` 预存基线失败除外）。

```bash
git add internal/relay/node_home.go internal/relay/opaque_auth.go internal/relay/server.go internal/relay/node_home_test.go
git commit -m "feat(relay): resolve and return home_instance_url on login finalize"
```

---

### Task 4: 节点列表 + 设置归属 API

**Files:**
- Create: `internal/relay/nodes_http.go`
- Modify: `internal/relay/server.go`（注册 2 路由)
- Test: `internal/relay/nodes_http_test.go`（新增）

**Interfaces:**
- Consumes: `userstore.ListLiveInstances`/`SetUserHome`、`UserFromContext`、`requireSession`、`InstanceLivenessWindow`、`cfg.InstancePublicURL`。
- Produces: `GET /api/nodes`、`PUT /api/me/home`。

- [ ] **Step 1: 写失败测试**

Create `internal/relay/nodes_http_test.go` — reuse the package's existing session-server test helper. Inspect `internal/relay/sessions_seen_http_test.go` (`newTestSeenServer`) and `helpers_test.go` (`createUserWithSession`) for the pattern that builds a `*Server` and an authenticated session token, then:
```go
package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNodesAndSetHome(t *testing.T) {
	srv, store := newTestSeenServer(t) // returns (*Server, *userstore.SQLiteStore)
	ctx := context.Background()
	token, userID := createUserWithSession(t, store, "nodes@example.com")
	_ = store.UpsertInstanceHeartbeat(ctx, "https://a.example", "https://a.example", time.Now().Unix())

	// GET /api/nodes
	req := httptest.NewRequest("GET", "/api/nodes", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/nodes = %d", rec.Code)
	}
	var listResp struct {
		Nodes []struct {
			InstanceID string `json:"instance_id"`
			PublicURL  string `json:"public_url"`
		} `json:"nodes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	if len(listResp.Nodes) != 1 || listResp.Nodes[0].InstanceID != "https://a.example" {
		t.Fatalf("nodes = %+v", listResp.Nodes)
	}

	// PUT /api/me/home with a live instance succeeds.
	body := strings.NewReader(`{"instance_id":"https://a.example"}`)
	req2 := httptest.NewRequest("PUT", "/api/me/home", body)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("PUT /api/me/home = %d (%s)", rec2.Code, rec2.Body.String())
	}
	if id, ok, _ := store.GetUserHome(ctx, userID); !ok || id != "https://a.example" {
		t.Fatalf("home not set: %q %v", id, ok)
	}

	// PUT with a non-live instance is rejected.
	bad := strings.NewReader(`{"instance_id":"https://ghost.example"}`)
	req3 := httptest.NewRequest("PUT", "/api/me/home", bad)
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("PUT bad instance = %d, want 400", rec3.Code)
	}
}
```
Note (verified): `newTestSeenServer(t) (*Server, *userstore.SQLiteStore)` (sessions_seen_http_test.go:17) and `createUserWithSession(t, store, email) (token, userID string)` (helpers_test.go:122) exist. `*Server` implements `http.Handler` via `func (s *Server) ServeHTTP` (server.go:276) — dispatch with `srv.ServeHTTP(rec, req)` (as web_push_http_test.go:53 does).

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/relay/ -run TestNodesAndSetHome -v`
Expected: 失败（404/route 不存在 或编译失败 `undefined: handleNodesHTTP`）。

- [ ] **Step 3: 实现 handlers**

Create `internal/relay/nodes_http.go`:
```go
package relay

import (
	"encoding/json"
	"net/http"
	"time"
)

type nodeEntry struct {
	InstanceID string `json:"instance_id"`
	PublicURL  string `json:"public_url"`
}

// handleNodesHTTP serves GET /api/nodes — the live instance list for the
// client-side node picker (ping latency is measured client-side).
func (s *Server) handleNodesHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHB := time.Now().Add(-InstanceLivenessWindow).Unix()
	live, err := s.cfg.Store.ListLiveInstances(r.Context(), minHB)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nodes := make([]nodeEntry, 0, len(live))
	for _, inst := range live {
		nodes = append(nodes, nodeEntry{InstanceID: inst.InstanceID, PublicURL: inst.PublicURL})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"nodes": nodes})
}

// handleSetHomeHTTP serves PUT /api/me/home — set the account-level home node.
func (s *Server) handleSetHomeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := UserFromContext(r.Context())
	if !ok || u == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	var req struct {
		InstanceID string `json:"instance_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InstanceID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Validate the target is a live instance.
	minHB := time.Now().Add(-InstanceLivenessWindow).Unix()
	live, err := s.cfg.Store.ListLiveInstances(r.Context(), minHB)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	found := false
	for _, inst := range live {
		if inst.InstanceID == req.InstanceID {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "unknown or dead instance", http.StatusBadRequest)
		return
	}
	if err := s.cfg.Store.SetUserHome(r.Context(), u.ID, req.InstanceID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
```
In `internal/relay/server.go`, register the routes beside the push routes (~line 196):
```go
	s.mux.HandleFunc("GET /api/nodes", s.requireSession(s.handleNodesHTTP))
	s.mux.HandleFunc("PUT /api/me/home", s.requireSession(s.handleSetHomeHTTP))
```

- [ ] **Step 4: 运行 + 回归 + 提交**

Run:
```bash
GOTOOLCHAIN=local go test ./internal/relay/ -run TestNodesAndSetHome -v
GOTOOLCHAIN=local go test ./internal/relay/ ./cmd/...
```
Expected: PASS。

```bash
git add internal/relay/nodes_http.go internal/relay/server.go internal/relay/nodes_http_test.go
git commit -m "feat(relay): GET /api/nodes + PUT /api/me/home node selection APIs"
```

---

## 收尾

- [ ] 全量回归(含 Postgres 契约):起 PG,`ATTERM_TEST_PG_DSN=... GOTOOLCHAIN=local go test ./internal/... ./cmd/...`(desktop/hookinstall 预存基线除外)。
- [ ] 手测多节点:两个 relay 进程各配不同 `ATTERM_RELAY_INSTANCE_PUBLIC_URL`、同一 PG → `GET /api/nodes` 返回两个;登录后 `home_instance_url` 落到登录节点;`PUT /api/me/home` 切换。
- [ ] `docker rm -f atterm-pg` 清理。

## 后续(子项目 C —— 不在本 plan)

- 三端节点选择器 UI(`GET /api/nodes` + 客户端测 ping + 切换 `PUT /api/me/home` 全账号生效)+ 读 `home_instance_url` 路由 `/uplink`+`/client` 到所选节点 + 归属变化重连。见 spec §6。
