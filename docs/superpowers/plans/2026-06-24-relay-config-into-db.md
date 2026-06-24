# Relay 配置入库(relay.json + web-push.json → DB)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 relay 的文件态配置(`relay.json` via `AdminConfigStore`)与 web push 状态(`web-push.json` via webpush `Service`)迁入数据库(`userstore.DBStore`,SQLite/Postgres 双后端),使数据库成为所有共享配置的唯一来源,多实例共享同一份配置与订阅;单机行为不变。

**Architecture:** 新增三张表 —— `relay_config`(单例,带单调 `version` 列)、`web_push_keys`(单例 VAPID 密钥对)、`web_push_subscriptions`(`(user_id,endpoint)` 复合主键)。`AdminConfigStore` 改为 DB 背书,后台 TTL 刷新器轮询 `relay_config.version`,变更时把新值应用到既有的内存热缓存(`SetAllowedOrigins`/`SetDebug`/`applyRuntimeLimits`/`SetSecretCipher`),实现跨实例最终一致。webpush `Service` 改为 DB 背书:VAPID 密钥首启生成入库、订阅增删读全部走 DB、dispatch 时按用户读库。退役两个 JSON 文件及其文件清理 cron。

**Tech Stack:** Go,`database/sql`,既有 `userstore` dialect 抽象,SQLite + Postgres。

## Global Constraints

- **依赖 Plan 1**:`userstore.DBStore`(别名 `SQLiteStore`)、dialect 抽象(`s.dia.Rebind`/`s.dia.IsUniqueViolation`)、分方言迁移目录 `migrations/{sqlite,postgres}/`、`OpenPostgres`、契约测试基建均已存在。
- **不破坏单机行为**:每个 task 结束 `GOTOOLCHAIN=local go test ./internal/userstore/... ./internal/relay/... ./cmd/...` 全过;契约测试新增项对 SQLite 与(env `ATTERM_TEST_PG_DSN` 提供时)Postgres 双跑。
- **Go 1.23**:`go.mod` `go 1.23.0` 不变,无新增需 Go≥1.24 的依赖。
- **单例表用 `id=1` + `INSERT ... ON CONFLICT(id) DO UPDATE`**(沿用 `opaque_server_state` 模式,见 `internal/userstore/opaque.go` 的 `GetOpaqueServerState`/`StoreOpaqueServerState`)。SQLite 用 `INTEGER`/`BLOB`,Postgres 用 `BIGINT`/`BYTEA`、布尔用 `BIGINT`(0/1)。时间戳 Go 计算传入(不在 SQL 用 `strftime`)。
- **`feishu_encrypt_key` 入库**(经评审的有意识取舍,见 spec §3.4):它与所加密的飞书密文同库。
- **多实例一致性边界**:`rate_limit_per_minute`/`max_connections_per_key` 的计数器仍 per-instance(集群总量 = 阈值 × 实例数);本计划只让**配置值**跨实例一致(通过 TTL 刷新),不集中化计数器。
- **`read_only_tokens` 不迁移**:勘探确认该字段在代码中从未被读取/强制(dead);不进 `relay_config` 表,`AdminConfig` 结构中保留该字段但不持久化到 DB(留待后续清理)。
- 关于 `vapid_subject`:历史上"持久化但仅启动生效"。本计划将其纳入 `relay_config`,语义不变(webpush 在启动消费一次);改它仍需重启,文档说明。

---

## File Structure

- `internal/userstore/migrations/sqlite/0007_relay_config.sql`、`migrations/postgres/0006_relay_config.sql`(新增):三张表。
- `internal/userstore/relay_config.go`(新增):`RelayConfig` 类型 + `GetRelayConfig`/`SetRelayConfig`(单例,version 自增)。
- `internal/userstore/webpush_store.go`(新增):`VAPIDKeys` 类型 + `GetVAPIDKeys`/`SetVAPIDKeys`;`WebPushSubscription` 类型 + `AddWebPushSubscription`/`RemoveWebPushSubscription`/`ListWebPushSubscriptions`。
- `internal/userstore/store.go`(修改):`Store` 接口加上述方法。
- `internal/userstore/contract_test.go`(修改):新增 relay_config / webpush 子测试,双后端跑。
- `internal/relay/admin_config.go`(修改):`AdminConfigStore` 改 DB 背书(去文件 I/O),加 `version`/刷新支持。
- `internal/relay/config_refresh.go`(新增):后台 TTL 刷新器,把 DB 变更应用到 Server 内存缓存。
- `internal/relay/server.go`(修改):启动刷新器;`ApplyFeishuConfig` 不变。
- `internal/webpush/service.go`、`persist.go`、`dispatch.go`(修改):`Open` 改 `(store, subject)`,VAPID/订阅走 DB;删 `web-push.json` 文件态与 `CleanupLegacy`。
- `cmd/atterm-relay/main.go`(修改):构造 `AdminConfigStore`/`webpush.Open` 改用 store;env 种子写入 DB;删 JSON 文件路径与 web push legacy cron。

---

### Task 1: 三张配置表的迁移(sqlite + postgres)

**Files:**
- Create: `internal/userstore/migrations/sqlite/0007_relay_config.sql`
- Create: `internal/userstore/migrations/postgres/0006_relay_config.sql`

**Interfaces:**
- Produces: 表 `relay_config`(单例 id=1)、`web_push_keys`(单例 id=1)、`web_push_subscriptions`(PK `(user_id,endpoint)`)。`relay_config` 含 `version BIGINT` 单调列。

- [ ] **Step 1: 写 SQLite 迁移**

Create `internal/userstore/migrations/sqlite/0007_relay_config.sql`:
```sql
-- 0007_relay_config.sql — move relay.json + web-push.json into the DB.
CREATE TABLE relay_config (
    id                      INTEGER PRIMARY KEY CHECK (id = 1),
    rate_limit_per_minute   INTEGER NOT NULL DEFAULT 0,
    max_connections_per_key INTEGER NOT NULL DEFAULT 0,
    allowed_origins         TEXT    NOT NULL DEFAULT '[]',  -- JSON array
    vapid_subject           TEXT    NOT NULL DEFAULT '',
    debug                   INTEGER NOT NULL DEFAULT 0,
    debug_payload           INTEGER NOT NULL DEFAULT 0,
    feishu_enabled          INTEGER NOT NULL DEFAULT 0,
    feishu_encrypt_key      TEXT    NOT NULL DEFAULT '',
    feishu_base_url         TEXT    NOT NULL DEFAULT '',
    version                 INTEGER NOT NULL DEFAULT 1,
    updated_at              INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE web_push_keys (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    private_key TEXT    NOT NULL,
    public_key  TEXT    NOT NULL,
    created_at  INTEGER NOT NULL
);

CREATE TABLE web_push_subscriptions (
    user_id    TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   TEXT    NOT NULL,
    p256dh     TEXT    NOT NULL,
    auth       TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, endpoint)
);
CREATE INDEX web_push_subscriptions_user ON web_push_subscriptions(user_id);
```

- [ ] **Step 2: 写 Postgres 迁移**

Create `internal/userstore/migrations/postgres/0006_relay_config.sql`:
```sql
-- 0006_relay_config.sql (postgres) — booleans/timestamps as BIGINT.
CREATE TABLE relay_config (
    id                      BIGINT PRIMARY KEY CHECK (id = 1),
    rate_limit_per_minute   BIGINT NOT NULL DEFAULT 0,
    max_connections_per_key BIGINT NOT NULL DEFAULT 0,
    allowed_origins         TEXT   NOT NULL DEFAULT '[]',
    vapid_subject           TEXT   NOT NULL DEFAULT '',
    debug                   BIGINT NOT NULL DEFAULT 0,
    debug_payload           BIGINT NOT NULL DEFAULT 0,
    feishu_enabled          BIGINT NOT NULL DEFAULT 0,
    feishu_encrypt_key      TEXT   NOT NULL DEFAULT '',
    feishu_base_url         TEXT   NOT NULL DEFAULT '',
    version                 BIGINT NOT NULL DEFAULT 1,
    updated_at              BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE web_push_keys (
    id          BIGINT PRIMARY KEY CHECK (id = 1),
    private_key TEXT   NOT NULL,
    public_key  TEXT   NOT NULL,
    created_at  BIGINT NOT NULL
);

CREATE TABLE web_push_subscriptions (
    user_id    TEXT   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   TEXT   NOT NULL,
    p256dh     TEXT   NOT NULL,
    auth       TEXT   NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, endpoint)
);
CREATE INDEX web_push_subscriptions_user ON web_push_subscriptions(user_id);
```

- [ ] **Step 3: 验证迁移在两后端建表**

Run (SQLite via existing tests opening `:memory:` which run all migrations):
```bash
GOTOOLCHAIN=local go test ./internal/userstore/ -run TestStoreContract -v
```
Expected: existing subtests still PASS (migrations apply cleanly; new tables created). If Postgres DSN is set, postgres subtest also applies 0006 without error.

- [ ] **Step 4: 提交**

```bash
git add internal/userstore/migrations/
git commit -m "feat(userstore): migrations for relay_config + web_push tables"
```

---

### Task 2: relay_config store 方法 + 契约测试

**Files:**
- Create: `internal/userstore/relay_config.go`
- Modify: `internal/userstore/store.go`（Store 接口加方法）
- Modify: `internal/userstore/contract_test.go`（新增子测试）

**Interfaces:**
- Produces:
  - `type RelayConfig struct { RateLimitPerMinute, MaxConnectionsPerKey int; AllowedOrigins []string; VAPIDSubject string; Debug, DebugPayload, FeishuEnabled bool; FeishuEncryptKey, FeishuBaseURL string; Version int64 }`
  - `func (s *DBStore) GetRelayConfig(ctx context.Context) (RelayConfig, error)` — 单例不存在时返回零值 `RelayConfig{Version:0}` 且 `nil` error（表示"尚未配置"）。
  - `func (s *DBStore) SetRelayConfig(ctx context.Context, cfg RelayConfig) (RelayConfig, error)` — upsert id=1，`version = old.version + 1`，`updated_at = now`，返回写入后的 cfg（含新 version）。

- [ ] **Step 1: 写失败测试**

Add to `internal/userstore/contract_test.go` inside `runStoreContract` (new sub-subtest):
```go
	t.Run("relay_config get-default then set bumps version", func(t *testing.T) {
		st := open(t)
		got, err := st.GetRelayConfig(ctx)
		if err != nil {
			t.Fatalf("get default: %v", err)
		}
		if got.Version != 0 {
			t.Fatalf("unconfigured version = %d, want 0", got.Version)
		}
		in := RelayConfig{
			RateLimitPerMinute: 600, MaxConnectionsPerKey: 64,
			AllowedOrigins: []string{"https://a.example", "https://b.example"},
			VAPIDSubject:   "mailto:x@y.z", Debug: true,
			FeishuEnabled:  true, FeishuEncryptKey: "k", FeishuBaseURL: "https://f",
		}
		w1, err := st.SetRelayConfig(ctx, in)
		if err != nil {
			t.Fatalf("set: %v", err)
		}
		if w1.Version != 1 {
			t.Fatalf("first set version = %d, want 1", w1.Version)
		}
		got2, _ := st.GetRelayConfig(ctx)
		if got2.RateLimitPerMinute != 600 || len(got2.AllowedOrigins) != 2 ||
			got2.AllowedOrigins[0] != "https://a.example" || !got2.Debug ||
			!got2.FeishuEnabled || got2.FeishuEncryptKey != "k" || got2.Version != 1 {
			t.Fatalf("readback mismatch: %+v", got2)
		}
		w2, _ := st.SetRelayConfig(ctx, in)
		if w2.Version != 2 {
			t.Fatalf("second set version = %d, want 2", w2.Version)
		}
	})
```

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/userstore/ -run TestStoreContract/sqlite -v`
Expected: 编译失败（`undefined: RelayConfig` / `GetRelayConfig`）。

- [ ] **Step 3: 实现**

Create `internal/userstore/relay_config.go`:
```go
package userstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RelayConfig is the singleton relay-wide config row (relay_config, id=1).
// It is the DB-backed replacement for relay.json. Version is a monotonic
// counter bumped on every SetRelayConfig, used by instances to detect and
// pull changes made by another instance's admin API.
type RelayConfig struct {
	RateLimitPerMinute   int
	MaxConnectionsPerKey int
	AllowedOrigins       []string
	VAPIDSubject         string
	Debug                bool
	DebugPayload         bool
	FeishuEnabled        bool
	FeishuEncryptKey     string
	FeishuBaseURL        string
	Version              int64
}

// GetRelayConfig reads the singleton row. If no row exists yet (fresh DB),
// it returns a zero RelayConfig with Version==0 and a nil error — callers
// treat Version==0 as "not configured yet".
func (s *DBStore) GetRelayConfig(ctx context.Context) (RelayConfig, error) {
	var (
		c          RelayConfig
		originsRaw string
		debug, debugPayload, feishuEnabled int64
	)
	err := s.db.QueryRowContext(ctx, s.dia.Rebind(
		`SELECT rate_limit_per_minute, max_connections_per_key, allowed_origins,
		        vapid_subject, debug, debug_payload, feishu_enabled,
		        feishu_encrypt_key, feishu_base_url, version
		 FROM relay_config WHERE id = 1`)).
		Scan(&c.RateLimitPerMinute, &c.MaxConnectionsPerKey, &originsRaw,
			&c.VAPIDSubject, &debug, &debugPayload, &feishuEnabled,
			&c.FeishuEncryptKey, &c.FeishuBaseURL, &c.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return RelayConfig{Version: 0}, nil
	}
	if err != nil {
		return RelayConfig{}, fmt.Errorf("get relay_config: %w", err)
	}
	c.Debug = debug != 0
	c.DebugPayload = debugPayload != 0
	c.FeishuEnabled = feishuEnabled != 0
	if originsRaw != "" {
		if err := json.Unmarshal([]byte(originsRaw), &c.AllowedOrigins); err != nil {
			return RelayConfig{}, fmt.Errorf("decode allowed_origins: %w", err)
		}
	}
	return c, nil
}

// SetRelayConfig upserts the singleton row, bumping version by 1 and stamping
// updated_at. Returns the persisted config including the new version.
func (s *DBStore) SetRelayConfig(ctx context.Context, cfg RelayConfig) (RelayConfig, error) {
	origins := cfg.AllowedOrigins
	if origins == nil {
		origins = []string{}
	}
	originsJSON, err := json.Marshal(origins)
	if err != nil {
		return RelayConfig{}, fmt.Errorf("encode allowed_origins: %w", err)
	}
	now := time.Now().Unix()
	var newVersion int64
	err = s.db.QueryRowContext(ctx, s.dia.Rebind(
		`INSERT INTO relay_config(
		     id, rate_limit_per_minute, max_connections_per_key, allowed_origins,
		     vapid_subject, debug, debug_payload, feishu_enabled,
		     feishu_encrypt_key, feishu_base_url, version, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     rate_limit_per_minute   = excluded.rate_limit_per_minute,
		     max_connections_per_key = excluded.max_connections_per_key,
		     allowed_origins         = excluded.allowed_origins,
		     vapid_subject           = excluded.vapid_subject,
		     debug                   = excluded.debug,
		     debug_payload           = excluded.debug_payload,
		     feishu_enabled          = excluded.feishu_enabled,
		     feishu_encrypt_key      = excluded.feishu_encrypt_key,
		     feishu_base_url         = excluded.feishu_base_url,
		     version                 = relay_config.version + 1,
		     updated_at              = excluded.updated_at
		 RETURNING version`),
		cfg.RateLimitPerMinute, cfg.MaxConnectionsPerKey, string(originsJSON),
		cfg.VAPIDSubject, b2i(cfg.Debug), b2i(cfg.DebugPayload), b2i(cfg.FeishuEnabled),
		cfg.FeishuEncryptKey, cfg.FeishuBaseURL, now).Scan(&newVersion)
	if err != nil {
		return RelayConfig{}, fmt.Errorf("set relay_config: %w", err)
	}
	cfg.Version = newVersion
	return cfg, nil
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
```

Note: `INSERT ... ON CONFLICT ... RETURNING` is supported by both modernc SQLite (3.35+) and Postgres. The `version` starts at 1 on first insert and `relay_config.version + 1` on update, so first Set → 1, second → 2.

Then add to the `Store` interface in `internal/userstore/store.go` (in the interface block, near the preferences methods):
```go
	// Relay-wide singleton config (DB-backed replacement for relay.json).
	GetRelayConfig(ctx context.Context) (RelayConfig, error)
	SetRelayConfig(ctx context.Context, cfg RelayConfig) (RelayConfig, error)
```

- [ ] **Step 4: 运行,确认通过(两后端)**

Run:
```bash
GOTOOLCHAIN=local go test ./internal/userstore/ -run TestStoreContract -v
```
Expected: sqlite `relay_config...` subtest PASS; postgres SKIP without DSN. With `ATTERM_TEST_PG_DSN` set + a running PG, postgres subtest PASS too.

- [ ] **Step 5: 提交**

```bash
git add internal/userstore/relay_config.go internal/userstore/store.go internal/userstore/contract_test.go
git commit -m "feat(userstore): relay_config store methods (versioned singleton)"
```

---

### Task 3: VAPID 密钥 + web push 订阅 store 方法 + 契约测试

**Files:**
- Create: `internal/userstore/webpush_store.go`
- Modify: `internal/userstore/store.go`
- Modify: `internal/userstore/contract_test.go`

**Interfaces:**
- Produces:
  - `type VAPIDKeys struct { PrivateKey, PublicKey string }`
  - `func (s *DBStore) GetVAPIDKeys(ctx) (VAPIDKeys, bool, error)` — bool=false 表示尚未生成（无 row）。
  - `func (s *DBStore) SetVAPIDKeys(ctx, VAPIDKeys) error` — upsert id=1。
  - `type WebPushSubscription struct { Endpoint, P256dh, Auth string; CreatedAt int64 }`
  - `func (s *DBStore) AddWebPushSubscription(ctx, userID string, sub WebPushSubscription) error` — upsert `(user_id,endpoint)`。
  - `func (s *DBStore) RemoveWebPushSubscription(ctx, userID, endpoint string) error`
  - `func (s *DBStore) ListWebPushSubscriptions(ctx, userID string) ([]WebPushSubscription, error)`

- [ ] **Step 1: 写失败测试**

Add to `runStoreContract` in `contract_test.go`:
```go
	t.Run("vapid keys upsert + subscriptions crud", func(t *testing.T) {
		st := open(t)
		if _, ok, err := st.GetVAPIDKeys(ctx); err != nil || ok {
			t.Fatalf("fresh vapid: ok=%v err=%v, want ok=false", ok, err)
		}
		if err := st.SetVAPIDKeys(ctx, VAPIDKeys{PrivateKey: "priv", PublicKey: "pub"}); err != nil {
			t.Fatalf("set vapid: %v", err)
		}
		k, ok, err := st.GetVAPIDKeys(ctx)
		if err != nil || !ok || k.PrivateKey != "priv" || k.PublicKey != "pub" {
			t.Fatalf("get vapid: ok=%v err=%v k=%+v", ok, err, k)
		}

		u, err := st.CreateOpaqueUser(ctx, "push@example.com")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		sub := WebPushSubscription{Endpoint: "https://push/ep1", P256dh: "p", Auth: "a", CreatedAt: 123}
		if err := st.AddWebPushSubscription(ctx, u.ID, sub); err != nil {
			t.Fatalf("add sub: %v", err)
		}
		// Upsert same endpoint is idempotent (no duplicate).
		if err := st.AddWebPushSubscription(ctx, u.ID, sub); err != nil {
			t.Fatalf("re-add sub: %v", err)
		}
		list, err := st.ListWebPushSubscriptions(ctx, u.ID)
		if err != nil || len(list) != 1 || list[0].Endpoint != "https://push/ep1" {
			t.Fatalf("list: err=%v list=%+v", err, list)
		}
		if err := st.RemoveWebPushSubscription(ctx, u.ID, "https://push/ep1"); err != nil {
			t.Fatalf("remove: %v", err)
		}
		list2, _ := st.ListWebPushSubscriptions(ctx, u.ID)
		if len(list2) != 0 {
			t.Fatalf("after remove len = %d, want 0", len(list2))
		}
	})
```

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/userstore/ -run TestStoreContract/sqlite -v`
Expected: 编译失败（`undefined: VAPIDKeys` 等）。

- [ ] **Step 3: 实现**

Create `internal/userstore/webpush_store.go`:
```go
package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// VAPIDKeys is the singleton Web Push signing keypair (web_push_keys, id=1).
type VAPIDKeys struct {
	PrivateKey string
	PublicKey  string
}

// GetVAPIDKeys returns the keypair and ok=false if none has been generated.
func (s *DBStore) GetVAPIDKeys(ctx context.Context) (VAPIDKeys, bool, error) {
	var k VAPIDKeys
	err := s.db.QueryRowContext(ctx, s.dia.Rebind(
		`SELECT private_key, public_key FROM web_push_keys WHERE id = 1`)).
		Scan(&k.PrivateKey, &k.PublicKey)
	if errors.Is(err, sql.ErrNoRows) {
		return VAPIDKeys{}, false, nil
	}
	if err != nil {
		return VAPIDKeys{}, false, fmt.Errorf("get vapid keys: %w", err)
	}
	return k, true, nil
}

// SetVAPIDKeys upserts the singleton keypair.
func (s *DBStore) SetVAPIDKeys(ctx context.Context, k VAPIDKeys) error {
	_, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`INSERT INTO web_push_keys(id, private_key, public_key, created_at)
		 VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     private_key = excluded.private_key,
		     public_key  = excluded.public_key,
		     created_at  = excluded.created_at`),
		k.PrivateKey, k.PublicKey, nowUnix())
	if err != nil {
		return fmt.Errorf("set vapid keys: %w", err)
	}
	return nil
}

// WebPushSubscription is one browser push subscription for a user.
type WebPushSubscription struct {
	Endpoint  string
	P256dh    string
	Auth      string
	CreatedAt int64
}

// AddWebPushSubscription upserts a subscription keyed by (user_id, endpoint).
func (s *DBStore) AddWebPushSubscription(ctx context.Context, userID string, sub WebPushSubscription) error {
	_, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`INSERT INTO web_push_subscriptions(user_id, endpoint, p256dh, auth, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, endpoint) DO UPDATE SET
		     p256dh = excluded.p256dh,
		     auth   = excluded.auth`),
		userID, sub.Endpoint, sub.P256dh, sub.Auth, sub.CreatedAt)
	if err != nil {
		return fmt.Errorf("add web push subscription: %w", err)
	}
	return nil
}

// RemoveWebPushSubscription deletes one (user_id, endpoint) subscription.
func (s *DBStore) RemoveWebPushSubscription(ctx context.Context, userID, endpoint string) error {
	_, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`DELETE FROM web_push_subscriptions WHERE user_id = ? AND endpoint = ?`),
		userID, endpoint)
	if err != nil {
		return fmt.Errorf("remove web push subscription: %w", err)
	}
	return nil
}

// ListWebPushSubscriptions returns all subscriptions for a user.
func (s *DBStore) ListWebPushSubscriptions(ctx context.Context, userID string) ([]WebPushSubscription, error) {
	rows, err := s.db.QueryContext(ctx, s.dia.Rebind(
		`SELECT endpoint, p256dh, auth, created_at
		 FROM web_push_subscriptions WHERE user_id = ?`), userID)
	if err != nil {
		return nil, fmt.Errorf("list web push subscriptions: %w", err)
	}
	defer rows.Close()
	var out []WebPushSubscription
	for rows.Next() {
		var sub WebPushSubscription
		if err := rows.Scan(&sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
```

Add a small helper at the bottom of `internal/userstore/store.go` (used by webpush_store.go and reusable):
```go
func nowUnix() int64 { return time.Now().Unix() }
```
(Confirm `time` is imported in store.go — it is.)

Add to the `Store` interface in `store.go`:
```go
	// Web Push: VAPID keypair singleton + per-user subscriptions (DB-backed
	// replacement for web-push.json).
	GetVAPIDKeys(ctx context.Context) (VAPIDKeys, bool, error)
	SetVAPIDKeys(ctx context.Context, k VAPIDKeys) error
	AddWebPushSubscription(ctx context.Context, userID string, sub WebPushSubscription) error
	RemoveWebPushSubscription(ctx context.Context, userID, endpoint string) error
	ListWebPushSubscriptions(ctx context.Context, userID string) ([]WebPushSubscription, error)
```

- [ ] **Step 4: 运行,确认通过(两后端)**

Run: `GOTOOLCHAIN=local go test ./internal/userstore/ -run TestStoreContract -v`
Expected: sqlite new subtest PASS; postgres PASS with DSN (else SKIP).

- [ ] **Step 5: 提交**

```bash
git add internal/userstore/webpush_store.go internal/userstore/store.go internal/userstore/contract_test.go
git commit -m "feat(userstore): VAPID keys + web push subscription store methods"
```

---

### Task 4: AdminConfigStore 改 DB 背书 + TTL 刷新器

**Files:**
- Modify: `internal/relay/admin_config.go`
- Create: `internal/relay/config_refresh.go`
- Modify: `internal/relay/server.go`（启动刷新器）
- Modify: `cmd/atterm-relay/main.go`（构造方式 + env 种子写 DB）
- Test: `internal/relay/config_refresh_test.go`（新增）

**Interfaces:**
- Consumes: `userstore.Store.GetRelayConfig/SetRelayConfig`、`RelayConfig`（Task 2）。
- Produces:
  - `NewAdminConfigStore(store userstore.Store, initial AdminConfig) *AdminConfigStore`（签名改为接收 store,不再接收 path）。
  - `AdminConfigStore.LoadFromDB(ctx) (AdminConfig, error)`、`Set(ctx, AdminConfig) error`（写 DB,bump version）、`Snapshot() AdminConfig`、`Version() int64`。
  - `func (s *Server) startConfigRefresher(ctx context.Context, interval time.Duration)`：后台 goroutine 周期性 `GetRelayConfig`,若 `version` 变化则把新值应用到 `SetAllowedOrigins`/`SetDebug`/`applyRuntimeLimits`/`ApplyFeishuConfig`。

**关键映射**:`AdminConfig`(relay 内现有结构,见 `admin_config.go:23-47`)↔ `userstore.RelayConfig`(Task 2)。两者字段一一对应(除 `ReadOnlyTokens`,不入库)。

- [ ] **Step 1: 写失败测试(刷新器把 DB 变更应用到内存缓存)**

Create `internal/relay/config_refresh_test.go`:
```go
package relay

import (
	"context"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

func TestConfigRefresherAppliesRemoteChange(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Seed initial config (origins A).
	if _, err := store.SetRelayConfig(ctx, userstore.RelayConfig{
		AllowedOrigins: []string{"https://a.example"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := newConfigRefreshTestServer(t, store) // see note below
	if got := srv.currentAllowedOrigins(); len(got) != 1 || got[0] != "https://a.example" {
		t.Fatalf("initial origins = %v", got)
	}

	// Simulate another instance changing config in the shared DB.
	if _, err := store.SetRelayConfig(ctx, userstore.RelayConfig{
		AllowedOrigins: []string{"https://a.example", "https://b.example"},
	}); err != nil {
		t.Fatalf("remote change: %v", err)
	}

	// Run one refresh tick directly (no sleep): refreshOnce returns true if applied.
	applied, err := srv.refreshConfigOnce(ctx)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !applied {
		t.Fatalf("expected refresh to apply a version change")
	}
	if got := srv.currentAllowedOrigins(); len(got) != 2 || got[1] != "https://b.example" {
		t.Fatalf("after refresh origins = %v", got)
	}
}
```
Note: add a `newConfigRefreshTestServer(t, store)` helper in this test file that builds a `*Server` wired to `store`. The package already has a Server-constructing test helper — `internal/relay/admin_http_test.go:newAdminTestServer(t)` returns `(*Server, *userstore.SQLiteStore, ...)`. Adapt that pattern but inject the passed-in `store` as `cfg.Store` and the `AdminConfigStore` built from it, then call `srv.applyConfigToCaches(srv.cfg.AdminConfigStore.Snapshot())` once so the initial origins are loaded. Reuse the existing Server construction; do not invent a new one. Keep it minimal — it only needs Store + AdminConfigStore + the limiter/origins/debug caches initialized.

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/relay/ -run TestConfigRefresher -v`
Expected: 编译失败（`refreshConfigOnce` / `refreshConfigOnce` undefined）。

- [ ] **Step 3: 实现 AdminConfigStore(DB 背书)**

Rewrite the file-I/O parts of `internal/relay/admin_config.go`. Replace the `path`-based struct and methods:
```go
type AdminConfigStore struct {
	mu    sync.Mutex
	store userstore.Store
	cfg   AdminConfig
}

func NewAdminConfigStore(store userstore.Store, initial AdminConfig) *AdminConfigStore {
	return &AdminConfigStore{store: store, cfg: initial}
}

// LoadFromDB reads relay_config and replaces the in-memory cfg. If the DB has
// no config yet (Version==0), the in-memory cfg is left as the seeded initial.
func (s *AdminConfigStore) LoadFromDB(ctx context.Context) (AdminConfig, error) {
	rc, err := s.store.GetRelayConfig(ctx)
	if err != nil {
		return AdminConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if rc.Version > 0 {
		s.cfg = relayConfigToAdmin(rc)
	}
	return cloneAdminConfig(s.cfg), nil
}

func (s *AdminConfigStore) Snapshot() AdminConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAdminConfig(s.cfg)
}

func (s *AdminConfigStore) Version() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.version
}

// Set validates, writes to the DB (bumping version), and updates the cache.
func (s *AdminConfigStore) Set(ctx context.Context, cfg AdminConfig) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	written, err := s.store.SetRelayConfig(ctx, adminToRelayConfig(cfg))
	if err != nil {
		return err
	}
	applied := relayConfigToAdmin(written)
	s.mu.Lock()
	s.cfg = applied
	s.mu.Unlock()
	return nil
}
```
Add a private `version int64` field to the `AdminConfig` struct (unexported, json:"-") to carry the DB version through Snapshot, OR keep version on the store only and have Snapshot expose it via `Version()` reading `s.cfg.version`. Use the struct field approach:
```go
type AdminConfig struct {
	// ... existing exported fields ...
	version int64 `json:"-"` // DB relay_config.version; 0 = unconfigured
}
```
Add the mappers in `admin_config.go`:
```go
func relayConfigToAdmin(rc userstore.RelayConfig) AdminConfig {
	return AdminConfig{
		RateLimitPerMinute:   rc.RateLimitPerMinute,
		MaxConnectionsPerKey: rc.MaxConnectionsPerKey,
		AllowedOrigins:       append([]string(nil), rc.AllowedOrigins...),
		VAPIDSubject:         rc.VAPIDSubject,
		Debug:                rc.Debug,
		DebugPayload:         rc.DebugPayload,
		FeishuEnabled:        rc.FeishuEnabled,
		FeishuEncryptKey:     rc.FeishuEncryptKey,
		FeishuBaseURL:        rc.FeishuBaseURL,
		version:              rc.Version,
	}
}

func adminToRelayConfig(c AdminConfig) userstore.RelayConfig {
	return userstore.RelayConfig{
		RateLimitPerMinute:   c.RateLimitPerMinute,
		MaxConnectionsPerKey: c.MaxConnectionsPerKey,
		AllowedOrigins:       append([]string(nil), c.AllowedOrigins...),
		VAPIDSubject:         c.VAPIDSubject,
		Debug:                c.Debug,
		DebugPayload:         c.DebugPayload,
		FeishuEnabled:        c.FeishuEnabled,
		FeishuEncryptKey:     c.FeishuEncryptKey,
		FeishuBaseURL:        c.FeishuBaseURL,
	}
}
```
Delete `LoadAdminConfig` (file read), `Save` (file write), and the `path` field. Update `cloneAdminConfig` to also copy `version`. Keep `validate()` and `DecodeFeishuKey()` unchanged. Update every caller of the old `Set(cfg)` / `Save()` (admin_http.go `updateAdminConfig`) to the new `Set(ctx, cfg)` — pass the request context. Update `Load()` callers to `LoadFromDB(ctx)`.

- [ ] **Step 4: 实现刷新器**

Create `internal/relay/config_refresh.go`:
```go
package relay

import (
	"context"
	"log"
	"time"
)

// refreshConfigOnce pulls relay_config from the DB; if its version differs from
// the last-applied version, it applies the new values to the in-memory hot
// caches (origins, debug, limiters, Feishu cipher) and returns true.
func (s *Server) refreshConfigOnce(ctx context.Context) (bool, error) {
	store := s.cfg.AdminConfigStore
	prev := store.Version()
	applied, err := store.LoadFromDB(ctx)
	if err != nil {
		return false, err
	}
	if applied.version == prev {
		return false, nil
	}
	s.applyConfigToCaches(applied)
	return true, nil
}

// applyConfigToCaches pushes config values into the request-time atomic caches
// and limiters. Safe to call repeatedly.
func (s *Server) applyConfigToCaches(cfg AdminConfig) {
	s.SetAllowedOrigins(cfg.AllowedOrigins)
	s.SetDebug(cfg.Debug, cfg.DebugPayload)
	s.applyRuntimeLimits(cfg.RateLimitPerMinute, cfg.MaxConnectionsPerKey)
	if keyBytes, err := cfg.DecodeFeishuKey(); err == nil && cfg.FeishuEnabled {
		s.ApplyFeishuConfig(true, keyBytes, cfg.FeishuBaseURL)
	} else {
		s.ApplyFeishuConfig(false, nil, cfg.FeishuBaseURL)
	}
}

// startConfigRefresher runs refreshConfigOnce every interval until ctx is done.
func (s *Server) startConfigRefresher(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := s.refreshConfigOnce(ctx); err != nil {
					log.Printf("relay: config refresh: %v", err)
				}
			}
		}
	}()
}
```
Note: `SetAllowedOrigins`, `SetDebug`, `applyRuntimeLimits`, `ApplyFeishuConfig` already exist (per the runtime map). `applyRuntimeLimits` may be a method on Server taking `(rate, conns int)` — confirm its signature in `admin_http.go:560` and match it. If it lives on a different receiver, expose a thin Server method wrapping it.

- [ ] **Step 5: 运行测试,确认通过**

Run: `GOTOOLCHAIN=local go test ./internal/relay/ -run TestConfigRefresher -v`
Expected: PASS.

- [ ] **Step 6: 接线 main.go + 启动刷新器**

In `cmd/atterm-relay/main.go`:
- Replace `cfgFilePath`/`LoadAdminConfig`/`NewAdminConfigStore(cfgFilePath, ...)` (lines ~78-86) with:
  ```go
  adminStore := relay.NewAdminConfigStore(store, relay.AdminConfig{})
  if _, err := adminStore.LoadFromDB(ctx); err != nil {
      log.Fatalf("load relay config: %v", err)
  }
  ```
- The env-seeding block (lines ~127-160): build an `AdminConfig` from env/flags as today, but persist via `adminStore.Set(ctx, seeded)` instead of the old file `Set`. Only seed when the DB is unconfigured OR an env explicitly overrides (preserve current precedence: env/flags win at boot). Keep it simple: read `adminStore.Snapshot()`, overlay any provided env/flags, `adminStore.Set(ctx, overlaid)`.
- After the Server is built and before serving, start the refresher:
  ```go
  srv.StartConfigRefresher(ctx, 10*time.Second)
  ```
  (Add an exported `func (s *Server) StartConfigRefresher(ctx, d)` wrapping `startConfigRefresher`, or call the unexported one if main is same package — it is not; add the exported wrapper in config_refresh.go.)

- [ ] **Step 7: 全量回归 + 提交**

Run: `GOTOOLCHAIN=local go test ./internal/relay/... ./cmd/...`
Expected: PASS (existing admin_http tests now exercise DB-backed config; fix any that constructed `AdminConfigStore` with a path — update them to pass a store, e.g. `userstore.Open(ctx, ":memory:")`).

```bash
git add internal/relay/admin_config.go internal/relay/config_refresh.go internal/relay/config_refresh_test.go internal/relay/server.go cmd/atterm-relay/main.go
git commit -m "feat(relay): DB-backed admin config + cross-instance TTL refresher"
```

---

### Task 5: webpush Service 改 DB 背书

**Files:**
- Modify: `internal/webpush/service.go`、`internal/webpush/persist.go`、`internal/webpush/dispatch.go`
- Modify: `cmd/atterm-relay/main.go`
- Test: `internal/webpush/service_db_test.go`（新增）

**Interfaces:**
- Consumes: `userstore.Store` 的 VAPID/订阅方法（Task 3）。
- Produces:
  - `func Open(store userstore.Store, vapidSubject string) (*Service, error)`（签名从 `(dir, subject)` 改为 `(store, subject)`）。首启若 `GetVAPIDKeys` 返回 ok=false，则生成密钥对并 `SetVAPIDKeys`。
  - `AddSubscription`/`RemoveSubscription` 写 DB；dispatch 读 DB。

**注意**:webpush 包当前不依赖 userstore。引入 `userstore.Store` 依赖到 webpush 包；确认无 import 环（userstore 不 import webpush —— 单向依赖,安全）。Subscription 在 DB 与 webpush 内部 `Subscription` 类型间转换。

- [ ] **Step 1: 写失败测试**

Create `internal/webpush/service_db_test.go`:
```go
package webpush

import (
	"context"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

func TestServiceUsesDBForKeysAndSubs(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// First Open generates + persists a VAPID keypair.
	svc, err := Open(store, "mailto:x@y.z")
	if err != nil {
		t.Fatalf("open svc: %v", err)
	}
	pub := svc.PublicKey()
	if pub == "" {
		t.Fatalf("expected generated public key")
	}
	// Second Open reuses the same persisted key (no regeneration).
	svc2, err := Open(store, "mailto:x@y.z")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if svc2.PublicKey() != pub {
		t.Fatalf("vapid key changed across Open: %q vs %q", svc2.PublicKey(), pub)
	}

	u, err := store.CreateOpaqueUser(ctx, "wp@example.com")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := svc.AddSubscription(u.ID, Subscription{
		Endpoint: "https://push/ep", Keys: SubscriptionKeys{P256dh: "p", Auth: "a"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	subs := svc.SubscriptionsForUser(u.ID)
	if len(subs) != 1 || subs[0].Endpoint != "https://push/ep" {
		t.Fatalf("subs = %+v", subs)
	}
}
```
Note: match the actual `Subscription` / keys struct field names in `internal/webpush` (the explorer reported `Subscription` with `Endpoint` and `Keys{p256dh, auth}` — confirm exact Go field names in subscription.go and adjust the literal).

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/webpush/ -run TestServiceUsesDB -v`
Expected: 编译失败（`Open` 签名不匹配）。

- [ ] **Step 3: 实现**

- In `service.go`: change `Service` to hold `store userstore.Store` instead of `dir string`. Rewrite `Open`:
  ```go
  func Open(store userstore.Store, vapidSubject string) (*Service, error) {
      ctx := context.Background()
      keys, ok, err := store.GetVAPIDKeys(ctx)
      if err != nil {
          return nil, fmt.Errorf("load vapid keys: %w", err)
      }
      if !ok {
          priv, pub, gerr := generateVAPIDKeypair()
          if gerr != nil {
              return nil, fmt.Errorf("generate vapid: %w", gerr)
          }
          keys = userstore.VAPIDKeys{PrivateKey: priv, PublicKey: pub}
          if serr := store.SetVAPIDKeys(ctx, keys); serr != nil {
              return nil, fmt.Errorf("persist vapid: %w", serr)
          }
      }
      s := &Service{subject: vapidSubject, store: store,
          vapidPriv: keys.PrivateKey, vapidPub: keys.PublicKey, tr: newTransport()}
      return s, nil
  }
  ```
  (Drop the in-memory `subStore`; subscriptions now live in the DB. Keep `vapidPriv`/`vapidPub`/`subject`/`tr`.)
- `AddSubscription(userID, sub)`: convert to `userstore.WebPushSubscription{Endpoint: sub.Endpoint, P256dh: sub.Keys.P256dh, Auth: sub.Keys.Auth, CreatedAt: time.Now().Unix()}` and call `s.store.AddWebPushSubscription(ctx, userID, ...)`.
- `RemoveSubscription(userID, endpoint)`: call `s.store.RemoveWebPushSubscription(ctx, userID, endpoint)`.
- `SubscriptionsForUser(userID)` and the dispatch path (`dispatch.go` `DispatchCommandFinished`/`DispatchSessionNotification`/`sendOne`): read via `s.store.ListWebPushSubscriptions(ctx, userID)`, mapping each `userstore.WebPushSubscription` back to the webpush `Subscription` for signing. On 404/410 in `sendOne`, call `s.store.RemoveWebPushSubscription(...)`.
- Delete from `persist.go`: `loadOrInitState`, `regenerateAndPersist`, `saveState`, `persistedState`, `isLegacySubscriptionSchema`, `CleanupLegacy`, and the `stateFilename` const (all file-based). Keep `generateVAPIDKeypair` (move it to `vapid.go` if it lived in persist.go). Remove `persistBestEffort` from service.go.

- [ ] **Step 4: 接线 main.go**

In `cmd/atterm-relay/main.go`:
- Replace `webpush.Open(persistDir, effectiveVapid)` (line ~208) with `webpush.Open(store, effectiveVapid)`.
- Remove the web-push legacy cleanup goroutine (the daily `CleanupLegacy` call, lines ~239-253) — the file no longer exists.
- `effectiveVapid` still comes from `adminStore.Snapshot().VAPIDSubject` / `--vapid-subject`.

- [ ] **Step 5: 运行 + 提交**

Run:
```bash
GOTOOLCHAIN=local go test ./internal/webpush/... ./internal/relay/... ./cmd/...
```
Expected: PASS (update any webpush tests that called the old `Open(dir, subject)` or relied on `web-push.json` — point them at a `userstore.Open(ctx, ":memory:")` store). 

```bash
git add internal/webpush/ cmd/atterm-relay/main.go
git commit -m "feat(webpush): DB-backed VAPID keys + subscriptions; retire web-push.json"
```

---

### Task 6: 退役 JSON 文件路径 + 空库初始化 + 文档

**Files:**
- Modify: `cmd/atterm-relay/main.go`（清理残留文件路径逻辑、persistDir 仅用于 SQLite db）
- Modify: `internal/relay/admin_config.go`（删除残留的 file 注释/常量 `tokenHashPrefix` 保留）
- Modify: `docs/` 或 `docker-compose.yml` 注释（部署说明）

**Interfaces:**
- Consumes: 前序任务的 DB 背书配置/webpush。
- Produces: 启动路径不再读写 `relay.json`/`web-push.json`;空库首启即正常工作(配置走 DB 默认 + env 种子,VAPID 首启生成入库)。

- [ ] **Step 1: 清理 main.go 残留文件逻辑**

In `cmd/atterm-relay/main.go`: remove any remaining references to `cfgFilePath`, `relay.json`, `web-push.json`, the `--config` flag if it only pointed at relay.json (keep `--config-dir`/`persistDir` since SQLite still uses it for `users.db`). Ensure `persistDir` is still created (0700) for the SQLite backend; for the Postgres backend `persistDir` is unused for config but harmless. Verify no dead imports (`encoding/json`, `os` for the removed file ops) remain unused — `go build` will catch these.

- [ ] **Step 2: 空库首启冒烟(SQLite + Postgres)**

Run (SQLite, fresh temp dir):
```bash
GOTOOLCHAIN=local go build -o atterm-relay ./cmd/atterm-relay/
TMP=$(mktemp -d); ATTERM_RELAY_CONFIG_DIR="$TMP" ./atterm-relay --addr :0 --dev-insecure &
sleep 2; kill %1 2>/dev/null
```
Expected: starts clean on an empty DB, generates VAPID into the DB, no `relay.json`/`web-push.json` created in `$TMP` (only `users.db`). Confirm:
```bash
ls "$TMP"   # expect users.db (+ WAL/SHM), NO relay.json / web-push.json
```

- [ ] **Step 3: 部署文档**

Update `docker-compose.yml` comments and/or a deploy note: relay no longer writes `relay.json`/`web-push.json`; all config + web push state live in the DB (`ATTERM_RELAY_DB_DRIVER`/`ATTERM_RELAY_DB_DSN`). For multi-instance, point all instances at the same Postgres; admin config changes propagate to other instances within ~10s (the config refresher TTL). `rate_limit`/`max_connections` remain per-instance.

- [ ] **Step 4: 全量回归 + 提交**

Run:
```bash
GOTOOLCHAIN=local go build ./... && GOTOOLCHAIN=local go test ./internal/userstore/... ./internal/relay/... ./internal/webpush/... ./cmd/...
```
Expected: PASS.

```bash
git add cmd/atterm-relay/main.go internal/relay/admin_config.go docker-compose.yml
git commit -m "chore(relay): retire relay.json/web-push.json; DB is the only config source"
```

---

## 收尾

- [ ] 全量回归(含 Postgres 契约): 起 PG 容器,`ATTERM_TEST_PG_DSN=... GOTOOLCHAIN=local go test ./...`(desktop hookinstall 预存基线失败除外)。
- [ ] 确认空库首启在 SQLite 与 Postgres 两种后端均生成默认配置 + VAPID。
- [ ] `docker rm -f atterm-pg` 清理测试容器。

## 后续(不在本 plan)

- **Plan 3** — `atterm-relay migrate --from <dsn> --to <dsn>` 双向跨库搬迁子命令(依赖 Plan 1)。
- **阶段二** — 多实例实时路由(realm 身份、归属调度、E2EE 按 realm 重锚定),见设计 spec §6。
