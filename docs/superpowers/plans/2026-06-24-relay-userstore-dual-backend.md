# Relay userstore 双后端(SQLite + Postgres)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `internal/userstore` 同时支持 SQLite 与 Postgres 两个后端,所有现有 `Store` 方法在两后端行为一致,relay 通过环境变量选择后端;现有 SQLite 行为与全部现有测试保持不变。

**Architecture:** DML(增删改查 Go 代码)共享一套实现,通过一个 `dialect` 适配占位符(`?`→`$N`)与唯一冲突错误判定;DDL/迁移分 `migrations/sqlite/` 与 `migrations/postgres/` 两套目录,各自维护到相同最终 schema;`strftime('%s','now')` 这类时间函数下沉到 Go 层(传 `time.Now().Unix()`)以消除方言差异。Postgres 走 `database/sql` + `github.com/jackc/pgx/v5/stdlib` 驱动。

**Tech Stack:** Go,`database/sql`,`modernc.org/sqlite`(现有),`github.com/jackc/pgx/v5`(新增),Postgres 16。

## Global Constraints

- **不破坏现有 SQLite 行为**:每个 task 结束时 `go test ./internal/userstore/... ./internal/relay/... ./cmd/...` 必须全过(回归)。
- **不重命名破坏调用方**:大量代码依赖具体类型 `*userstore.SQLiteStore`(`internal/relay/server.go:197,307,417,440`、`opaque_auth.go:33`、`feishu_http.go:20`、`feishu_bindstore.go:13`、`opaque_server.go:89`、`cmd/atterm-relay/bootstrap_admin.go:37`,以及数十个 `_test.go`)。核心 struct 重命名为 `DBStore`,并保留类型别名 `type SQLiteStore = DBStore`,使所有现有引用零改动继续编译。
- **DML 一套、DDL 两套**:不为运行时查询写两份 SQL;只有建表 DDL 分方言。
- **时间下沉**:运行时不再使用 `strftime`;时间戳由 Go 计算后作为参数传入。
- **Postgres 用整数存布尔**:现有 Go 把 `is_admin` 等按 `int` 扫描(`isAdmin != 0`),Postgres schema 对应列用 `BIGINT`(存 0/1),不用 `BOOLEAN`,以保持共享扫描代码不变。
- **Postgres 用 BYTEA 存 BLOB**;`COLLATE NOCASE` 在 Postgres 去掉(email 已在 Go 层 `strings.ToLower`,用普通 `UNIQUE`)。
- **Postgres 测试 DSN** 来自环境变量 `ATTERM_TEST_PG_DSN`;未设置时相关测试 `t.Skip`(本地可用 §Task 5 的 docker 命令起库)。
- **命名债**:`SQLiteStore` 别名保留为兼容层,彻底改名留待后续 plan,本计划不处理。
- **`DB()` 暂保留**:spec §3.1 要求收敛泄漏抽象 `DBStore.DB() *sql.DB`,但 `cmd/atterm-relay/bootstrap_admin_test.go:29,38,63` 依赖它做内部断言。本 plan 保留 `DB()` 不动(它对双后端功能无影响),其移除/收敛留待后续清理 plan。此处显式记录,以免被当作遗漏。

---

## File Structure

- `internal/userstore/dialect.go`(新增):`dialect` 接口 + `sqliteDialect` / `postgresDialect`,`Rebind` 与 `IsUniqueViolation`。
- `internal/userstore/dialect_test.go`(新增):dialect 单元测试。
- `internal/userstore/migrations/sqlite/*.sql`(移动自 `migrations/*.sql`)。
- `internal/userstore/migrations/postgres/*.sql`(新增):Postgres 最终 schema。
- `internal/userstore/migrations.go`(修改):embed 两套目录。
- `internal/userstore/store.go`(修改):`DBStore` 改名 + 别名、`dia` 字段、`OpenPostgres`、`migrate` 按 dialect 读目录并下沉时间。
- `internal/userstore/users.go`(修改):rebind + 错误 helper + 时间下沉(横切示范文件)。
- 其余 store 方法文件(修改):接入 rebind(机械,见 Task 4 清单)。
- `internal/userstore/contract_test.go`(新增):双后端契约测试。
- `cmd/atterm-relay/main.go`(修改):按 `ATTERM_RELAY_DB_DRIVER`/`ATTERM_RELAY_DB_DSN` 选择后端。
- `go.mod` / `go.sum`(修改):加 `github.com/jackc/pgx/v5`。

---

### Task 1: dialect 抽象(rebind + 唯一冲突判定)

**Files:**
- Create: `internal/userstore/dialect.go`
- Test: `internal/userstore/dialect_test.go`
- Modify: `go.mod`(加 pgx 依赖)

**Interfaces:**
- Produces:
  - `type dialect interface { Name() string; Rebind(query string) string; IsUniqueViolation(err error) bool }`
  - `type sqliteDialect struct{}`、`type postgresDialect struct{}`,均实现 `dialect`。
  - 包级单例:`var dialectSQLite dialect = sqliteDialect{}`、`var dialectPostgres dialect = postgresDialect{}`。

- [ ] **Step 1: 添加 pgx 依赖**

Run:
```bash
cd /Volumes/Project/attson/atterm
go get github.com/jackc/pgx/v5@latest
```
Expected: `go.mod` 新增 `github.com/jackc/pgx/v5`。

- [ ] **Step 2: 写失败测试**

Create `internal/userstore/dialect_test.go`:
```go
package userstore

import "testing"

func TestSQLiteRebindIsNoop(t *testing.T) {
	got := dialectSQLite.Rebind("SELECT a FROM t WHERE x = ? AND y = ?")
	want := "SELECT a FROM t WHERE x = ? AND y = ?"
	if got != want {
		t.Fatalf("sqlite rebind = %q, want %q", got, want)
	}
}

func TestPostgresRebindNumbersPlaceholders(t *testing.T) {
	got := dialectPostgres.Rebind("INSERT INTO t(a,b,c) VALUES(?, ?, ?)")
	want := "INSERT INTO t(a,b,c) VALUES($1, $2, $3)"
	if got != want {
		t.Fatalf("postgres rebind = %q, want %q", got, want)
	}
}

func TestDialectNames(t *testing.T) {
	if dialectSQLite.Name() != "sqlite" {
		t.Fatalf("sqlite name = %q", dialectSQLite.Name())
	}
	if dialectPostgres.Name() != "postgres" {
		t.Fatalf("postgres name = %q", dialectPostgres.Name())
	}
}
```

- [ ] **Step 3: 运行测试,确认失败**

Run: `go test ./internal/userstore/ -run 'TestSQLiteRebind|TestPostgresRebind|TestDialectNames' -v`
Expected: 编译失败(`undefined: dialectSQLite` 等)。

- [ ] **Step 4: 写实现**

Create `internal/userstore/dialect.go`:
```go
package userstore

import (
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// dialect adapts the small set of SQL differences between SQLite and
// Postgres. DML (the actual queries in users.go, sessions.go, ...) is
// shared; only placeholder syntax and unique-violation error detection
// differ at runtime. DDL differences live in the per-dialect migration
// directories, not here.
type dialect interface {
	// Name is the migration subdirectory name ("sqlite" / "postgres").
	Name() string
	// Rebind converts '?' positional placeholders to the dialect's form.
	// SQLite keeps '?'; Postgres rewrites to $1, $2, ... in order.
	Rebind(query string) string
	// IsUniqueViolation reports whether err is a UNIQUE/primary-key
	// constraint violation. Callers already know which constraint they
	// could have hit (each insert touches a single unique key), so this
	// does not distinguish columns.
	IsUniqueViolation(err error) bool
}

type sqliteDialect struct{}

func (sqliteDialect) Name() string          { return "sqlite" }
func (sqliteDialect) Rebind(q string) string { return q }
func (sqliteDialect) IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite surfaces "UNIQUE constraint failed: <table>.<col>"
	// and "PRIMARY KEY constraint failed" in the error string.
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: PRIMARY KEY") ||
		strings.Contains(msg, "PRIMARY KEY constraint failed")
}

type postgresDialect struct{}

func (postgresDialect) Name() string { return "postgres" }

func (postgresDialect) Rebind(q string) string {
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

func (postgresDialect) IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" // unique_violation
	}
	return false
}

var (
	dialectSQLite   dialect = sqliteDialect{}
	dialectPostgres dialect = postgresDialect{}
)
```

- [ ] **Step 5: 运行测试,确认通过**

Run: `go test ./internal/userstore/ -run 'TestSQLiteRebind|TestPostgresRebind|TestDialectNames' -v`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/userstore/dialect.go internal/userstore/dialect_test.go go.mod go.sum
git commit -m "feat(userstore): add dialect abstraction (rebind + unique-violation)"
```

---

### Task 2: Postgres DDL 迁移文件

**Files:**
- Create: `internal/userstore/migrations/postgres/0001_init.sql`
- Create: `internal/userstore/migrations/postgres/0002_user_preferences.sql`
- Create: `internal/userstore/migrations/postgres/0003_opaque_auth.sql`
- Create: `internal/userstore/migrations/postgres/0004_claim_tokens.sql`
- Create: `internal/userstore/migrations/postgres/0005_feishu.sql`

**Interfaces:**
- Produces: Postgres 最终 schema,语义等价于 SQLite 迁移 0001–0006 应用后的结果。Postgres 从无 `webhooks` 表(SQLite 0006 已 drop 它),故不复刻 webhooks 的建/删;不复刻 0003 的 `ALTER ... DROP password_hash`,直接以最终列建表。

注:Postgres 迁移按本目录自己的 `schema_migrations` 记录,编号与 SQLite 各自独立。无需 0006(无 webhooks 可删)。

- [ ] **Step 1: 写 0001_init.sql**

Create `internal/userstore/migrations/postgres/0001_init.sql`:
```sql
-- 0001_init.sql (postgres) — final auth schema. Booleans stored as BIGINT
-- (0/1) to match the shared Go scan code; timestamps are unix epoch BIGINT.
-- email is lowercased in Go, so a plain UNIQUE replaces SQLite COLLATE NOCASE.

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    is_admin      BIGINT NOT NULL DEFAULT 0,
    auth_mode     TEXT NOT NULL DEFAULT 'opaque',
    created_at    BIGINT NOT NULL,
    disabled_at   BIGINT
);

CREATE TABLE sessions (
    id_hash      TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   BIGINT NOT NULL,
    expires_at   BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL DEFAULT 0,
    user_agent   TEXT NOT NULL DEFAULT '',
    ip_prefix    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);

CREATE TABLE pairing_tokens (
    token_hash   TEXT PRIMARY KEY,
    prefix       TEXT NOT NULL,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   BIGINT NOT NULL,
    expires_at   BIGINT NOT NULL,
    consumed_at  BIGINT
);
CREATE INDEX pairing_tokens_user_idx ON pairing_tokens(user_id);

CREATE TABLE invitations (
    code_hash      TEXT PRIMARY KEY,
    created_by     TEXT NOT NULL,
    created_at     BIGINT NOT NULL,
    expires_at     BIGINT,
    consumed_at    BIGINT,
    consumed_by    TEXT REFERENCES users(id),
    note           TEXT
);

CREATE TABLE session_seen (
    user_id    TEXT   NOT NULL,
    session_id TEXT   NOT NULL,
    seen_at    BIGINT NOT NULL,
    PRIMARY KEY (user_id, session_id)
);
```

- [ ] **Step 2: 写 0002_user_preferences.sql**

Create `internal/userstore/migrations/postgres/0002_user_preferences.sql`:
```sql
-- 0002_user_preferences.sql (postgres)
CREATE TABLE user_preferences (
    user_id     TEXT   NOT NULL,
    key         TEXT   NOT NULL,
    value_json  TEXT   NOT NULL,
    updated_at  BIGINT NOT NULL,
    PRIMARY KEY (user_id, key)
);
CREATE INDEX user_preferences_user ON user_preferences(user_id);
```

- [ ] **Step 3: 写 0003_opaque_auth.sql**

Create `internal/userstore/migrations/postgres/0003_opaque_auth.sql`:
```sql
-- 0003_opaque_auth.sql (postgres) — OPAQUE record + account-key wrap +
-- per-relay OPRF singleton. BLOB -> BYTEA.
CREATE TABLE user_opaque_records (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    record     BYTEA  NOT NULL,
    created_at BIGINT NOT NULL
);

CREATE TABLE user_account_key_wraps (
    user_id    TEXT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    method     TEXT  NOT NULL,
    wrapped    BYTEA NOT NULL,
    nonce      BYTEA NOT NULL,
    salt       BYTEA NOT NULL,
    kdf_params TEXT  NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, method)
);

CREATE TABLE opaque_server_state (
    id            BIGINT PRIMARY KEY CHECK (id = 1),
    oprf_seed     BYTEA  NOT NULL,
    server_ake_sk BYTEA  NOT NULL,
    server_ake_pk BYTEA  NOT NULL,
    suite         TEXT   NOT NULL,
    created_at    BIGINT NOT NULL
);
```

- [ ] **Step 4: 写 0004_claim_tokens.sql**

Create `internal/userstore/migrations/postgres/0004_claim_tokens.sql`:
```sql
-- 0004_claim_tokens.sql (postgres)
CREATE TABLE claim_tokens (
    token_hash  TEXT PRIMARY KEY,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL,
    expires_at  BIGINT NOT NULL,
    consumed_at BIGINT
);
CREATE INDEX claim_tokens_email ON claim_tokens(email);
```

- [ ] **Step 5: 写 0005_feishu.sql**

Create `internal/userstore/migrations/postgres/0005_feishu.sql`:
```sql
-- 0005_feishu.sql (postgres) — Feishu bindings + pending pair codes.
-- No webhooks table exists in the postgres schema, so the SQLite
-- "DELETE FROM webhooks" cleanup is omitted.
CREATE TABLE feishu_bindings (
    user_id          TEXT   PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    app_id_hash      TEXT   NOT NULL UNIQUE,
    app_id_enc       BYTEA  NOT NULL,
    app_secret_enc   BYTEA  NOT NULL,
    encrypt_key_enc  BYTEA  NOT NULL,
    verify_token_enc BYTEA  NOT NULL,
    open_id          TEXT,
    bound_at         BIGINT,
    disabled_at      BIGINT,
    created_at       BIGINT NOT NULL
);

CREATE TABLE feishu_pending_binds (
    user_id    TEXT   PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    code       TEXT   NOT NULL UNIQUE,
    expires_at BIGINT NOT NULL
);
CREATE INDEX feishu_pending_binds_expires ON feishu_pending_binds(expires_at);
```

- [ ] **Step 6: 提交**

```bash
git add internal/userstore/migrations/postgres/
git commit -m "feat(userstore): add postgres DDL migrations"
```

---

### Task 3: 迁移目录移动 + runner 按 dialect 读 + 时间下沉

**Files:**
- Move: `internal/userstore/migrations/000*.sql` → `internal/userstore/migrations/sqlite/`
- Modify: `internal/userstore/migrations.go`
- Modify: `internal/userstore/store.go:160-211`(`migrate` 函数)

**Interfaces:**
- Consumes: `dialect.Name()`(Task 1)、`migrations/<name>/*.sql`(Task 2 + 本任务移动)。
- Produces: `migrate` 按 `s.dia.Name()` 选择目录;`applied_at` 由 Go 计算;迁移记录 INSERT 经 `s.dia.Rebind`。注:本任务引用 `s.dia`,与 Task 4 协同——先在本任务给 `migrate` 改签名为接收 dialect 形参以便独立测试,Task 4 再把 `s.dia` 接上。为避免顺序耦合,本任务直接给 `DBStore` 加 `dia` 字段的最小版本(见 Step 3)。

- [ ] **Step 1: 移动现有迁移到 sqlite 子目录**

Run:
```bash
cd /Volumes/Project/attson/atterm/internal/userstore/migrations
mkdir -p sqlite
git mv 0001_init.sql 0002_user_preferences.sql 0003_opaque_auth.sql 0004_claim_tokens.sql 0005_feishu.sql 0006_drop_webhooks.sql sqlite/
```
Expected: 6 个文件位于 `migrations/sqlite/`,`migrations/postgres/` 已含 Task 2 文件。

- [ ] **Step 2: 更新 embed 指令**

Replace `internal/userstore/migrations.go` body:
```go
package userstore

import "embed"

// migrationsFS holds the embedded SQL migration files, split by dialect
// subdirectory: migrations/sqlite/*.sql and migrations/postgres/*.sql.
// The migration runner reads the subdirectory named by the active
// dialect (see DBStore.migrate).
//
//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationsFS embed.FS
```

- [ ] **Step 3: 给 DBStore 加最小 dia 字段(为 migrate 服务)**

In `internal/userstore/store.go`, the struct is renamed in Task 4; for now add the field to the existing `SQLiteStore` struct (lines 19-28). Change:
```go
type SQLiteStore struct {
	db *sql.DB
	cipher atomic.Pointer[SecretCipher]
}
```
to:
```go
type SQLiteStore struct {
	db  *sql.DB
	dia dialect
	cipher atomic.Pointer[SecretCipher]
}
```
And in `Open` (line 67), set the dialect when constructing:
```go
	s := &SQLiteStore{db: db, dia: dialectSQLite}
```

- [ ] **Step 4: 改写 migrate 按 dialect 读目录 + 下沉时间**

Replace the `migrate` function (`store.go:160-211`) with:
```go
func (s *SQLiteStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at BIGINT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	dir := "migrations/" + s.dia.Name()
	entries, err := migrationsFS.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read embedded migrations %s: %w", dir, err)
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
			s.dia.Rebind(`SELECT count(*) FROM schema_migrations WHERE name=?`), name,
		).Scan(&seen); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if seen > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile(dir + "/" + name)
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
			s.dia.Rebind(`INSERT INTO schema_migrations(name, applied_at) VALUES(?, ?)`),
			name, time.Now().Unix()); err != nil {
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
(`CREATE TABLE IF NOT EXISTS schema_migrations` uses `BIGINT`, which SQLite accepts as an alias for INTEGER affinity — safe for both backends.)

- [ ] **Step 5: 运行 SQLite 回归测试**

Run: `go test ./internal/userstore/... ./internal/relay/... ./cmd/...`
Expected: PASS（迁移目录改名后 SQLite 路径仍可建库;所有现有测试通过）。

- [ ] **Step 6: 提交**

```bash
git add internal/userstore/
git commit -m "refactor(userstore): split migrations by dialect, sink applied_at to Go"
```

---

### Task 4: DBStore 改名 + 接入 dialect + OpenPostgres

**Files:**
- Modify: `internal/userstore/store.go`（struct 改名 + 别名 + `OpenPostgres`）
- Modify: `internal/userstore/users.go:43-51,127-132`（rebind + 错误 helper + 时间下沉）
- Modify（机械 rebind 包裹,清单见 Step 4）: `sessions.go`, `sessions_admin.go`, `invitations.go`, `pairing.go`, `preferences.go`, `seen.go`, `opaque.go`, `claim_tokens.go`, `feishu_bindings.go`, `feishu_pending_binds.go`, `admin.go`, `users_delete.go`

**Interfaces:**
- Consumes: `s.dia`（Task 3）、`dialectPostgres`（Task 1）。
- Produces:
  - `type DBStore struct { db *sql.DB; dia dialect; cipher atomic.Pointer[SecretCipher] }`
  - `type SQLiteStore = DBStore`（别名,保兼容）
  - `func OpenPostgres(ctx context.Context, dsn string, opts ...OpenOption) (*DBStore, error)`

- [ ] **Step 1: struct 改名 + 别名**

In `internal/userstore/store.go`, rename the struct `SQLiteStore` → `DBStore` (the struct declaration only, around line 20) and add an alias directly below it:
```go
// DBStore is the production Store backed by database/sql. It serves both
// SQLite (single file) and Postgres, distinguished by its dialect.
type DBStore struct {
	db  *sql.DB
	dia dialect
	cipher atomic.Pointer[SecretCipher]
}

// SQLiteStore is the historical name for DBStore, kept as an alias so the
// many callers that reference *userstore.SQLiteStore keep compiling.
// TODO(cleanup): migrate callers to DBStore / the Store interface.
type SQLiteStore = DBStore
```
Receiver methods may keep `func (s *SQLiteStore) ...` unchanged — the alias makes them identical to `*DBStore`. Leave all existing receivers as-is.

- [ ] **Step 2: 加 OpenPostgres,并把 import 加上 pgx stdlib**

In `internal/userstore/store.go` imports, add the blank import for the pgx database/sql driver alongside the existing `_ "modernc.org/sqlite"`:
```go
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
```
Add the constructor (below `Open`):
```go
// OpenPostgres opens a Postgres-backed store at dsn (e.g.
// "postgres://user:pass@host:5432/db?sslmode=disable") and runs pending
// postgres migrations. Unlike SQLite, Postgres handles concurrent
// connections, so the pool is not pinned to a single connection.
func OpenPostgres(ctx context.Context, dsn string, opts ...OpenOption) (*DBStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open pgx: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	s := &DBStore{db: db, dia: dialectPostgres}
	for _, o := range opts {
		o(s)
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
```

- [ ] **Step 3: users.go — rebind + 错误 helper + 时间下沉(横切示范)**

In `internal/userstore/users.go`:

(a) `CreateOpaqueUser` (lines 43-51): wrap the query in `s.dia.Rebind` and replace the string match with the helper:
```go
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`INSERT INTO users(id, email, is_admin, auth_mode, created_at) VALUES (?, ?, ?, ?, ?)`),
		id, email, 0, "opaque", now.Unix())
	if err != nil {
		if s.dia.IsUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
```
(b) `GetUserByEmail` (line 75-78) and `GetUser` (line 105-108): wrap each query string with `s.dia.Rebind(...)`.
(c) `DisableUser` (lines 128-131): sink the time function to Go:
```go
func (s *SQLiteStore) DisableUser(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`UPDATE users SET disabled_at = ? WHERE id = ? AND disabled_at IS NULL`),
		time.Now().Unix(), id)
	return err
}
```
(d) `ListUsers` (line 136-137): no `?` placeholders, but wrap with `s.dia.Rebind(...)` for consistency (no-op on sqlite).

After editing, remove the now-unused `strings` import only if nothing else in users.go uses it — `CreateOpaqueUser`/`GetUserByEmail` still call `strings.ToLower`, so keep it.

- [ ] **Step 4: 机械 rebind 包裹其余文件**

For each file below, wrap **every** SQL string literal passed to `QueryContext`/`QueryRowContext`/`ExecContext`/`PrepareContext` with `s.dia.Rebind( ... )`. This is a no-op for SQLite and required for Postgres. Do not change query text otherwise. First enumerate every call site so none is missed:
```bash
grep -rn "QueryContext\|QueryRowContext\|ExecContext\|PrepareContext" internal/userstore --include="*.go" | grep -v _test.go | grep -v store.go
```
Every line that output lists (outside `store.go`, already done in Task 3) must have its query argument wrapped. Files and their query-bearing methods (use the grep output as the authoritative checklist):
- `sessions.go`: `CreateSession`, `LookupSession`, `DeleteSession`, `PurgeExpiredSessions`
- `sessions_admin.go`: `ListSessions`, `DeleteSessionByIDHash`, `DeleteOtherSessionsForUser`
- `invitations.go`: `CreateInvitation`, `ConsumeInvitation`, `ListInvitations`
- `pairing.go`: `CreatePairingToken`, `ConsumePairingToken`
- `preferences.go`: `GetUserPreferences`, `SetUserPreferences` (both the SELECT and the `INSERT ... ON CONFLICT ... DO UPDATE`)
- `seen.go`: `SetSeen`, `SeenAt`, `PruneSeenSession`
- `opaque.go`: `GetAccountKeyWrap`, `StoreAccountKeyWrap`, `GetOpaqueServerState`, `StoreOpaqueServerState` (if present), `GetOpaqueRecord`, `StoreOpaqueRecord`
- `claim_tokens.go`: `CreateClaimToken`, `LookupClaimToken`, `ConsumeClaimToken` (if present)
- `feishu_bindings.go`: all CRUD methods; additionally replace the local `isUniqueViolation(err, qualifiedCol)` usage with `s.dia.IsUniqueViolation(err)` and delete the now-unused local helper (lines ~107-115)
- `feishu_pending_binds.go`: `ConsumeFeishuPendingBind`, `SweepExpiredFeishuPendingBinds`, and the insert/upsert path
- `admin.go`: `AdminExists`, `SetUserAdmin`
- `users_delete.go`: `DeleteUser` (all statements in the transaction)

Also scan each file for any remaining `strftime(` and sink it to a Go-computed `time.Now().Unix()` parameter (grep confirmed only `users.go` had one in DML, but verify after editing):
```bash
grep -rn "strftime" internal/userstore --include="*.go" | grep -v _test.go
```
Expected after this step: no matches.

- [ ] **Step 5: 运行 SQLite 回归测试**

Run: `go test ./internal/userstore/... ./internal/relay/... ./cmd/... ./desktop/...`
Expected: PASS（rebind 对 sqlite 是 no-op,错误 helper 对 sqlite 走字符串匹配,行为不变）。

- [ ] **Step 6: 提交**

```bash
git add internal/userstore/
git commit -m "refactor(userstore): rename to DBStore (+alias), route all SQL through dialect, add OpenPostgres"
```

---

### Task 5: 双后端契约测试

**Files:**
- Create: `internal/userstore/contract_test.go`

**Interfaces:**
- Consumes: `Open`（sqlite `:memory:`）、`OpenPostgres`（env DSN）、`WithSecretCipher`、现有 `Store` 方法。
- Produces: `runStoreContract(t *testing.T, open func(t *testing.T) *DBStore)`,对两后端复用同一组断言。

本地起 Postgres（一次性）:
```bash
docker run -d --name atterm-pg -e POSTGRES_PASSWORD=postgres -p 5433:5432 postgres:16
export ATTERM_TEST_PG_DSN='postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable'
```

- [ ] **Step 1: 写契约测试**

Create `internal/userstore/contract_test.go`:
```go
package userstore

import (
	"context"
	"os"
	"testing"
	"time"
)

// openSQLite returns a fresh in-memory SQLite store with a test cipher so
// Feishu CRUD works.
func openSQLite(t *testing.T) *DBStore {
	t.Helper()
	c, err := NewSecretCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	st, err := Open(context.Background(), ":memory:", WithSecretCipher(c))
	if err != nil {
		t.Fatalf("Open sqlite: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// openPostgres connects to ATTERM_TEST_PG_DSN. Each run drops and recreates
// the public schema so migrations apply to a clean database.
func openPostgres(t *testing.T) *DBStore {
	t.Helper()
	dsn := os.Getenv("ATTERM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("ATTERM_TEST_PG_DSN not set; skipping Postgres contract test")
	}
	c, err := NewSecretCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	// Clean slate: connect raw, reset schema, then open via OpenPostgres.
	reset, err := OpenPostgres(context.Background(), dsn, WithSecretCipher(c))
	if err != nil {
		t.Fatalf("OpenPostgres(reset): %v", err)
	}
	if _, err := reset.db.ExecContext(context.Background(),
		`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		reset.Close()
		t.Fatalf("reset schema: %v", err)
	}
	reset.Close()
	st, err := OpenPostgres(context.Background(), dsn, WithSecretCipher(c))
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestStoreContract(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) { runStoreContract(t, openSQLite) })
	t.Run("postgres", func(t *testing.T) { runStoreContract(t, openPostgres) })
}

func runStoreContract(t *testing.T, open func(t *testing.T) *DBStore) {
	ctx := context.Background()

	t.Run("user CRUD + unique email", func(t *testing.T) {
		st := open(t)
		u, err := st.CreateOpaqueUser(ctx, "Alice@Example.com")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if u.Email != "alice@example.com" {
			t.Fatalf("email not lowercased: %q", u.Email)
		}
		got, err := st.GetUser(ctx, u.ID)
		if err != nil || got.Email != "alice@example.com" {
			t.Fatalf("get: %v %+v", err, got)
		}
		if _, err := st.CreateOpaqueUser(ctx, "alice@example.com"); err != ErrEmailTaken {
			t.Fatalf("want ErrEmailTaken, got %v", err)
		}
	})

	t.Run("session lifecycle", func(t *testing.T) {
		st := open(t)
		u, _ := st.CreateOpaqueUser(ctx, "bob@example.com")
		tok, sess, err := st.CreateSession(ctx, u.ID, "ua", "127.0.0", time.Hour)
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		gotSess, gotUser, err := st.LookupSession(ctx, tok)
		if err != nil || gotUser.ID != u.ID || gotSess.IDHash != sess.IDHash {
			t.Fatalf("lookup: %v u=%+v s=%+v", err, gotUser, gotSess)
		}
		if err := st.DeleteSession(ctx, tok); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, _, err := st.LookupSession(ctx, tok); err == nil {
			t.Fatalf("expected lookup to fail after delete")
		}
	})

	t.Run("preferences LWW upsert", func(t *testing.T) {
		st := open(t)
		u, _ := st.CreateOpaqueUser(ctx, "carol@example.com")
		items := []PreferenceItem{{Key: "locale_preference", ValueJSON: []byte(`"en"`), UpdatedAt: 100}}
		if _, err := st.SetUserPreferences(ctx, u.ID, 0, items); err != nil {
			t.Fatalf("set: %v", err)
		}
		// Stale write (older UpdatedAt) must not overwrite.
		stale := []PreferenceItem{{Key: "locale_preference", ValueJSON: []byte(`"zh"`), UpdatedAt: 50}}
		out, err := st.SetUserPreferences(ctx, u.ID, 0, stale)
		if err != nil {
			t.Fatalf("set stale: %v", err)
		}
		if len(out) != 1 || string(out[0].ValueJSON) != `"en"` {
			t.Fatalf("stale write won: %+v", out)
		}
	})

	t.Run("account key wrap roundtrip", func(t *testing.T) {
		st := open(t)
		u, _ := st.CreateOpaqueUser(ctx, "dave@example.com")
		w := AccountKeyWrap{
			UserID: u.ID, Method: "password",
			Wrapped: []byte("w"), Nonce: []byte("n"), Salt: []byte("s"),
			KDFParams: `{"t":1}`,
		}
		if err := st.StoreAccountKeyWrap(ctx, w); err != nil {
			t.Fatalf("store wrap: %v", err)
		}
		got, err := st.GetAccountKeyWrap(ctx, u.ID, "password")
		if err != nil || string(got.Wrapped) != "w" {
			t.Fatalf("get wrap: %v %+v", err, got)
		}
	})
}
```
Note: if `AccountKeyWrap`'s field names differ from the above (verify in `opaque.go`), adjust the struct literal to match. The intent is a store→load roundtrip across both backends.

- [ ] **Step 2: 运行(SQLite 必跑,Postgres 视 env)**

Run:
```bash
go test ./internal/userstore/ -run TestStoreContract -v
```
Expected: `sqlite` 子测试 PASS;`postgres` 子测试在未设 `ATTERM_TEST_PG_DSN` 时 SKIP。

- [ ] **Step 3: 起 Postgres 跑全量契约**

Run:
```bash
docker run -d --name atterm-pg -e POSTGRES_PASSWORD=postgres -p 5433:5432 postgres:16
sleep 3
ATTERM_TEST_PG_DSN='postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable' \
  go test ./internal/userstore/ -run TestStoreContract -v
```
Expected: `sqlite` 与 `postgres` 子测试均 PASS。修正任何 Postgres 暴露的 schema/扫描不一致,直到通过。

- [ ] **Step 4: 提交**

```bash
git add internal/userstore/contract_test.go
git commit -m "test(userstore): dual-backend Store contract test (sqlite + postgres)"
```

---

### Task 6: relay 启动按环境变量选择后端

**Files:**
- Modify: `cmd/atterm-relay/main.go:94`(`userstore.Open` 装配点)

**Interfaces:**
- Consumes: `Open`（sqlite）/`OpenPostgres`（postgres）。
- Produces: 环境变量契约 —— `ATTERM_RELAY_DB_DRIVER` ∈ {`sqlite`(默认), `postgres`};`postgres` 时必须设 `ATTERM_RELAY_DB_DSN`。

- [ ] **Step 1: 读现有装配上下文**

Run: `sed -n '80,110p' cmd/atterm-relay/main.go`
确认 `dbPath` 计算与 `store, err := userstore.Open(ctx, dbPath)`(第 94 行)及随后对 `store` 的使用(cipher、opaque server 等)。

- [ ] **Step 2: 替换后端选择逻辑**

Replace the single `store, err := userstore.Open(ctx, dbPath)` call (line 94) with:
```go
	var store *userstore.DBStore
	switch driver := strings.ToLower(strings.TrimSpace(os.Getenv("ATTERM_RELAY_DB_DRIVER"))); driver {
	case "", "sqlite":
		store, err = userstore.Open(ctx, dbPath)
	case "postgres":
		dsn := strings.TrimSpace(os.Getenv("ATTERM_RELAY_DB_DSN"))
		if dsn == "" {
			log.Fatal("ATTERM_RELAY_DB_DRIVER=postgres requires ATTERM_RELAY_DB_DSN")
		}
		store, err = userstore.OpenPostgres(ctx, dsn)
	default:
		log.Fatalf("unknown ATTERM_RELAY_DB_DRIVER %q (want sqlite|postgres)", driver)
	}
	if err != nil {
		log.Fatalf("open userstore: %v", err)
	}
```
Ensure `os` and `strings` are imported in `main.go` (add if missing). Keep the original error-handling shape used at line 94 if it differs (match surrounding `log` style).

- [ ] **Step 3: 编译 + SQLite 默认回归**

Run:
```bash
go build ./cmd/atterm-relay/
go test ./cmd/... ./internal/relay/...
```
Expected: 构建通过;默认(未设 env)走 SQLite,所有测试 PASS。

- [ ] **Step 4: 冒烟测试 Postgres 启动**

Run:
```bash
ATTERM_RELAY_DB_DRIVER=postgres \
ATTERM_RELAY_DB_DSN='postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable' \
  ./atterm-relay --addr :0 &
sleep 2; kill %1
```
Expected: 日志显示成功打开 Postgres 并运行迁移,无 fatal。(需 Task 5 的 docker PG 在跑。)

- [ ] **Step 5: 提交**

```bash
git add cmd/atterm-relay/main.go
git commit -m "feat(relay): select userstore backend via ATTERM_RELAY_DB_DRIVER/DSN"
```

---

## 收尾

- [ ] 清理本地 Postgres 容器:`docker rm -f atterm-pg`
- [ ] 确认全量回归:`go test ./...`(Postgres 子测试自动 skip)。
- [ ] 文档(可放入后续配置入库 plan):在 relay 部署说明记录 `ATTERM_RELAY_DB_DRIVER` / `ATTERM_RELAY_DB_DSN` 与"多实例共享同一 DSN、限流为 per-instance"。

## 后续计划(不在本 plan)

- **Plan 2 — 配置入库**:`relay_config` / `web_push_keys` / `web_push_subscriptions` 表 + AdminConfigStore/webpush 改读 DB + TTL 缓存。依赖本 plan 的 `Store` 抽象。
- **Plan 3 — migrate 子命令**:`atterm-relay migrate --from <dsn> --to <dsn>`,双向搬迁。依赖本 plan。
