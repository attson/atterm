# Relay 跨库搬迁子命令(migrate --from --to)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `atterm-relay migrate --from <dsn> --to <dsn>` 子命令,把一个 userstore 后端(SQLite 或 Postgres)的全部数据**忠实**(原始行,保留 hash / 加密 blob / 时间戳)复制到另一个后端,服务后续选型切换。

**Architecture:** 复制逻辑放在 `userstore` 包内(可直接访问 `*DBStore` 的私有 `db`/`dia`),做泛型表级复制:对每张表 `SELECT *` → 读列名 → 扫描进 `[]interface{}`(驱动回填 `[]byte`/`int64`/`string`/`nil`)→ 经 `dia.Rebind` 的 `INSERT` 写入目标,按外键拓扑序(父表先行)在单事务内完成。`cmd/atterm-relay` 在 `main()` 顶部拦截 `migrate` 子命令,用独立 FlagSet 解析 `--from`/`--to`,按 DSN scheme 打开两个 store(打开即建空 schema),调用复制,打印每表行数。

**Tech Stack:** Go,`database/sql`,既有 `userstore` 双后端 + dialect。

## Global Constraints

- **依赖 Plan 1 + Plan 2**:`userstore.DBStore`(别名 `SQLiteStore`)、`Open(ctx,path,opts)`/`OpenPostgres(ctx,dsn,opts)`(均运行迁移,返回 `*DBStore`)、`s.dia.Rebind`、`s.dia.Name()`、`DB()`。全部 15 张数据表(含 Plan 2 的 relay_config/web_push_keys/web_push_subscriptions)已存在于两方言迁移。
- **忠实复制**:不经领域方法(`CreateSession` 会重新生成 token/hash);用原始 `SELECT *`/`INSERT` 保留每个字段原值。
- **目标必须为空**:`Copy` 在写入前校验目标所有数据表为空,非空则拒绝(避免覆盖/冲突);打开一个全新目标后其表本就为空(迁移只建表,单例行运行时才惰性生成)。
- **排除 `schema_migrations`**:目标打开时已自建该表,不复制。
- **拓扑序**:`users` 先于所有 FK 子表;无 FK 表(单例 / claim_tokens / session_seen)任意位置。单事务,FK 在插入时即可解析(父行已先插)。
- **不破坏现有行为**:`migrate` 子命令在 `main()` 顶部短路,正常 relay 启动路径不受影响。每个 task 结束 `GOTOOLCHAIN=local go test ./internal/userstore/... ./cmd/...` 全过。
- **Go 1.23**:`go.mod` `go 1.23.0` 不变,无新依赖。
- **Postgres 测试 DSN** 来自 `ATTERM_TEST_PG_DSN`;未设置时跨后端用例 `t.Skip`。

---

## File Structure

- `internal/userstore/migrate.go`(新增):`Copy(ctx, src, dst *DBStore) (map[string]int64, error)`、`copyTablesInOrder`、`isEmptyForCopy`、私有 `copyTable`。
- `internal/userstore/migrate_test.go`(新增):双后端忠实度 + 非空目标拒绝测试。
- `internal/userstore/store.go`(修改):新增 `OpenFromDSN(ctx, dsn) (*DBStore, error)`(按 scheme 选 Open/OpenPostgres)。
- `cmd/atterm-relay/migrate.go`(新增):`migrateCmd(args []string) int` —— FlagSet + 调用 Copy + 打印摘要。
- `cmd/atterm-relay/main.go`(修改):`main()` 顶部拦截 `os.Args[1] == "migrate"`。
- `cmd/atterm-relay/migrate_test.go`(新增):sqlite 文件 → sqlite 文件端到端。

---

### Task 1: userstore.Copy(泛型表级忠实复制)

**Files:**
- Create: `internal/userstore/migrate.go`
- Test: `internal/userstore/migrate_test.go`

**Interfaces:**
- Produces:
  - `var copyTablesInOrder []string` —— FK 安全的复制顺序(15 张表,不含 schema_migrations)。
  - `func Copy(ctx context.Context, src, dst *DBStore) (map[string]int64, error)` —— 校验目标为空 → 单事务按序复制每表 → 返回 `表名→行数`。目标非空返回错误。
  - `func (s *DBStore) isEmptyForCopy(ctx context.Context) (bool, error)` —— 所有数据表均无行时 true。

- [ ] **Step 1: 写失败测试**

Create `internal/userstore/migrate_test.go`:
```go
package userstore

import (
	"context"
	"os"
	"testing"
	"time"
)

func seedSource(t *testing.T, st *DBStore) (userID string) {
	t.Helper()
	ctx := context.Background()
	u, err := st.CreateOpaqueUser(ctx, "migrate@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, _, err := st.CreateSession(ctx, u.ID, "ua", "127.0.0", time.Hour); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.StoreAccountKeyWrap(ctx, AccountKeyWrap{
		UserID: u.ID, Method: "password",
		Wrapped: []byte{1, 2, 3}, Nonce: []byte{4, 5}, Salt: []byte{6, 7},
		KDFParams: `{"t":1}`,
	}); err != nil {
		t.Fatalf("store wrap: %v", err)
	}
	if _, err := st.SetRelayConfig(ctx, RelayConfig{
		RateLimitPerMinute: 600, AllowedOrigins: []string{"https://a.example"},
		FeishuEnabled: true, FeishuEncryptKey: "k",
	}); err != nil {
		t.Fatalf("set relay config: %v", err)
	}
	if err := st.SetVAPIDKeys(ctx, VAPIDKeys{PrivateKey: "priv", PublicKey: "pub"}); err != nil {
		t.Fatalf("set vapid: %v", err)
	}
	if err := st.AddWebPushSubscription(ctx, u.ID, WebPushSubscription{
		Endpoint: "https://push/ep", P256dh: "p", Auth: "a", CreatedAt: 99,
	}); err != nil {
		t.Fatalf("add sub: %v", err)
	}
	return u.ID
}

func assertTarget(t *testing.T, dst *DBStore, userID string) {
	t.Helper()
	ctx := context.Background()
	if u, err := dst.GetUser(ctx, userID); err != nil || u.Email != "migrate@example.com" {
		t.Fatalf("target user: %v %+v", err, u)
	}
	if sess, err := dst.ListSessions(ctx, userID); err != nil || len(sess) != 1 {
		t.Fatalf("target sessions: %v %+v", err, sess)
	}
	if w, err := dst.GetAccountKeyWrap(ctx, userID, "password"); err != nil || string(w.Wrapped) != string([]byte{1, 2, 3}) {
		t.Fatalf("target wrap: %v %+v", err, w)
	}
	rc, err := dst.GetRelayConfig(ctx)
	if err != nil || rc.RateLimitPerMinute != 600 || !rc.FeishuEnabled || rc.FeishuEncryptKey != "k" || len(rc.AllowedOrigins) != 1 {
		t.Fatalf("target relay_config: %v %+v", err, rc)
	}
	k, ok, err := dst.GetVAPIDKeys(ctx)
	if err != nil || !ok || k.PrivateKey != "priv" {
		t.Fatalf("target vapid: ok=%v err=%v %+v", ok, err, k)
	}
	if subs, err := dst.ListWebPushSubscriptions(ctx, userID); err != nil || len(subs) != 1 || subs[0].Endpoint != "https://push/ep" {
		t.Fatalf("target subs: %v %+v", err, subs)
	}
}

func TestCopySQLiteToSQLite(t *testing.T) {
	ctx := context.Background()
	src, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	t.Cleanup(func() { src.Close() })
	dst, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open dst: %v", err)
	}
	t.Cleanup(func() { dst.Close() })

	userID := seedSource(t, src)
	counts, err := Copy(ctx, src, dst)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if counts["users"] != 1 || counts["sessions"] != 1 || counts["web_push_subscriptions"] != 1 ||
		counts["relay_config"] != 1 || counts["web_push_keys"] != 1 || counts["user_account_key_wraps"] != 1 {
		t.Fatalf("counts: %+v", counts)
	}
	assertTarget(t, dst, userID)

	// Second copy into the now-non-empty target must be refused.
	if _, err := Copy(ctx, src, dst); err == nil {
		t.Fatalf("expected refusal copying into non-empty target")
	}
}

func TestCopySQLiteToPostgres(t *testing.T) {
	dsn := os.Getenv("ATTERM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("ATTERM_TEST_PG_DSN not set; skipping cross-backend copy test")
	}
	ctx := context.Background()
	src, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	t.Cleanup(func() { src.Close() })
	// Clean PG schema, then open (runs migrations → empty tables).
	reset, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("open reset: %v", err)
	}
	if _, err := reset.db.ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		reset.Close()
		t.Fatalf("reset schema: %v", err)
	}
	reset.Close()
	dst, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("open dst: %v", err)
	}
	t.Cleanup(func() { dst.Close() })

	userID := seedSource(t, src)
	if _, err := Copy(ctx, src, dst); err != nil {
		t.Fatalf("copy: %v", err)
	}
	assertTarget(t, dst, userID)
}
```

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./internal/userstore/ -run TestCopySQLiteToSQLite -v`
Expected: 编译失败(`undefined: Copy`)。

- [ ] **Step 3: 实现**

Create `internal/userstore/migrate.go`:
```go
package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// copyTablesInOrder lists every data table in foreign-key-safe order
// (parents before children) for a raw row copy. schema_migrations is
// intentionally excluded — the target builds its own when opened.
var copyTablesInOrder = []string{
	"users",
	"sessions",
	"pairing_tokens",
	"invitations",
	"session_seen",
	"user_preferences",
	"user_opaque_records",
	"user_account_key_wraps",
	"feishu_bindings",
	"feishu_pending_binds",
	"web_push_subscriptions",
	"opaque_server_state",
	"claim_tokens",
	"relay_config",
	"web_push_keys",
}

// isEmptyForCopy reports whether every data table is empty. Copy refuses a
// non-empty target to avoid clobbering or PK conflicts.
func (s *DBStore) isEmptyForCopy(ctx context.Context) (bool, error) {
	for _, table := range copyTablesInOrder {
		var n int
		// Table names are fixed constants from copyTablesInOrder (not user
		// input), so the interpolation is safe.
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			return false, fmt.Errorf("count %s: %w", table, err)
		}
		if n > 0 {
			return false, nil
		}
	}
	return true, nil
}

// Copy faithfully copies all data from src to dst (raw rows, preserving
// hashes, encrypted blobs and timestamps). The target MUST be empty. Returns
// table -> rows-copied. Both stores must already be open (schema migrated).
func Copy(ctx context.Context, src, dst *DBStore) (map[string]int64, error) {
	empty, err := dst.isEmptyForCopy(ctx)
	if err != nil {
		return nil, err
	}
	if !empty {
		return nil, errors.New("migrate: target is not empty (refusing to overwrite); point --to at a freshly-created database")
	}

	tx, err := dst.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	counts := make(map[string]int64, len(copyTablesInOrder))
	for _, table := range copyTablesInOrder {
		n, err := copyTable(ctx, src, dst, tx, table)
		if err != nil {
			return nil, fmt.Errorf("copy %s: %w", table, err)
		}
		counts[table] = n
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return counts, nil
}

// copyTable streams every row of one table from src into dst's tx using a
// generic SELECT */INSERT. Values scan into []interface{} as []byte/int64/
// string/nil, which both modernc.org/sqlite and jackc/pgx/v5/stdlib accept
// when re-inserted into the equivalent BLOB/BYTEA, INTEGER/BIGINT, TEXT, or
// NULL columns.
func copyTable(ctx context.Context, src, dst *DBStore, tx *sql.Tx, table string) (int64, error) {
	rows, err := src.db.QueryContext(ctx, "SELECT * FROM "+table)
	if err != nil {
		return 0, fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("columns: %w", err)
	}
	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insertSQL := dst.dia.Rebind(fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		table, strings.Join(cols, ", "), strings.Join(placeholders, ", ")))

	var n int64
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return n, fmt.Errorf("scan: %w", err)
		}
		if _, err := tx.ExecContext(ctx, insertSQL, vals...); err != nil {
			return n, fmt.Errorf("insert: %w", err)
		}
		n++
	}
	return n, rows.Err()
}
```

- [ ] **Step 4: 运行,确认通过(sqlite→sqlite)**

Run: `GOTOOLCHAIN=local go test ./internal/userstore/ -run TestCopySQLiteToSQLite -v`
Expected: PASS（含非空目标拒绝）。

- [ ] **Step 5: 跨后端验证(sqlite→postgres)**

Run (real PG on :5433):
```bash
ATTERM_TEST_PG_DSN='postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable' \
  GOTOOLCHAIN=local go test ./internal/userstore/ -run TestCopySQLiteToPostgres -v
```
Expected: PASS。若某列类型在跨驱动 scan/insert 时出错,定位该表并在 plan 范围内修(应不需要——所有列是 TEXT/INTEGER/BLOB)。然后跑全量 `GOTOOLCHAIN=local go test ./internal/userstore/` 确认无回归。

- [ ] **Step 6: 提交**

```bash
git add internal/userstore/migrate.go internal/userstore/migrate_test.go
git commit -m "feat(userstore): Copy — faithful raw cross-backend data migration"
```

---

### Task 2: migrate 子命令 + DSN 打开器

**Files:**
- Modify: `internal/userstore/store.go`（新增 `OpenFromDSN`）
- Create: `cmd/atterm-relay/migrate.go`
- Modify: `cmd/atterm-relay/main.go`（`main()` 顶部拦截）
- Test: `cmd/atterm-relay/migrate_test.go`

**Interfaces:**
- Consumes: `userstore.Copy`（Task 1）、`Open`/`OpenPostgres`。
- Produces:
  - `func OpenFromDSN(ctx context.Context, dsn string) (*DBStore, error)` —— `postgres://`/`postgresql://` → `OpenPostgres`;`sqlite:<path>` → `Open(path)`;其余按文件路径 → `Open`。
  - `func migrateCmd(args []string) int` —— 解析 `--from`/`--to`、打开两 store、调用 `Copy`、打印每表行数;成功返回 0。

- [ ] **Step 1: 写失败测试(端到端 sqlite 文件 → sqlite 文件)**

Create `cmd/atterm-relay/migrate_test.go`:
```go
package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

func TestMigrateCmdSQLiteFileToFile(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")
	dstPath := filepath.Join(dir, "dst.db")
	ctx := context.Background()

	// Seed the source file DB.
	src, err := userstore.Open(ctx, srcPath)
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	u, err := src.CreateOpaqueUser(ctx, "cli@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, _, err := src.CreateSession(ctx, u.ID, "ua", "127.0.0", time.Hour); err != nil {
		t.Fatalf("session: %v", err)
	}
	src.Close()

	// Run the subcommand.
	code := migrateCmd([]string{"--from", "sqlite:" + srcPath, "--to", "sqlite:" + dstPath})
	if code != 0 {
		t.Fatalf("migrateCmd exit = %d, want 0", code)
	}

	// Verify the target file received the data.
	dst, err := userstore.Open(ctx, dstPath)
	if err != nil {
		t.Fatalf("open dst: %v", err)
	}
	t.Cleanup(func() { dst.Close() })
	if got, err := dst.GetUser(ctx, u.ID); err != nil || got.Email != "cli@example.com" {
		t.Fatalf("target user: %v %+v", err, got)
	}
	if sess, err := dst.ListSessions(ctx, u.ID); err != nil || len(sess) != 1 {
		t.Fatalf("target sessions: %v %+v", err, sess)
	}
}
```

- [ ] **Step 2: 运行,确认失败**

Run: `GOTOOLCHAIN=local go test ./cmd/atterm-relay/ -run TestMigrateCmd -v`
Expected: 编译失败(`undefined: migrateCmd`)。

- [ ] **Step 3: 实现 OpenFromDSN**

In `internal/userstore/store.go`, add (near `OpenPostgres`):
```go
// OpenFromDSN opens a store from a scheme-tagged DSN:
//   - "postgres://..." or "postgresql://..."  → Postgres
//   - "sqlite:<path>"                          → SQLite at <path>
//   - anything else                            → SQLite, treating the DSN
//     as a bare file path
// Used by the migrate subcommand for --from/--to.
func OpenFromDSN(ctx context.Context, dsn string, opts ...OpenOption) (*DBStore, error) {
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return OpenPostgres(ctx, dsn, opts...)
	case strings.HasPrefix(dsn, "sqlite:"):
		path := strings.TrimPrefix(dsn, "sqlite:")
		path = strings.TrimPrefix(path, "//")
		return Open(ctx, path, opts...)
	default:
		return Open(ctx, dsn, opts...)
	}
}
```
(Confirm `strings` is imported in store.go — it is, from Plan 1's pgx/driver work; if not, add it.)

- [ ] **Step 4: 实现 migrate 子命令**

Create `cmd/atterm-relay/migrate.go`:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/attson/atterm/internal/userstore"
)

// migrateCmd implements `atterm-relay migrate --from <dsn> --to <dsn>`:
// a one-shot, offline, faithful copy of all data from one userstore backend
// to another (for SQLite<->Postgres switching). The target must be empty.
func migrateCmd(args []string) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	from := fs.String("from", "", "source DSN: sqlite:<path> | postgres://...")
	to := fs.String("to", "", "target DSN (must be empty): sqlite:<path> | postgres://...")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *from == "" || *to == "" {
		log.Printf("migrate: --from and --to are required")
		fs.Usage()
		return 2
	}

	ctx := context.Background()
	src, err := userstore.OpenFromDSN(ctx, *from)
	if err != nil {
		log.Printf("migrate: open --from: %v", err)
		return 1
	}
	defer src.Close()
	dst, err := userstore.OpenFromDSN(ctx, *to)
	if err != nil {
		log.Printf("migrate: open --to: %v", err)
		return 1
	}
	defer dst.Close()

	counts, err := userstore.Copy(ctx, src, dst)
	if err != nil {
		log.Printf("migrate: %v", err)
		return 1
	}

	var total int64
	fmt.Println("migrate: copied rows per table:")
	for _, table := range userstore.CopyTableOrder() {
		fmt.Printf("  %-24s %d\n", table, counts[table])
		total += counts[table]
	}
	fmt.Printf("migrate: done (%d rows across %d tables)\n", total, len(counts))
	return 0
}
```
This references `userstore.CopyTableOrder()` for stable output ordering. Add it to `internal/userstore/migrate.go`:
```go
// CopyTableOrder returns the table copy order (for callers that want to print
// per-table results deterministically).
func CopyTableOrder() []string {
	return append([]string(nil), copyTablesInOrder...)
}
```

- [ ] **Step 5: 接线 main.go**

In `cmd/atterm-relay/main.go`, at the very top of `func main()` (before any flag definition / `flag.Parse()`), add:
```go
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		os.Exit(migrateCmd(os.Args[2:]))
	}
```
(`os` is already imported. This short-circuits before the relay's global flags are parsed, so migrate's own FlagSet owns `--from`/`--to`.)

- [ ] **Step 6: 运行 + 提交**

Run:
```bash
GOTOOLCHAIN=local go build ./cmd/atterm-relay/
GOTOOLCHAIN=local go test ./cmd/atterm-relay/ -run TestMigrateCmd -v
GOTOOLCHAIN=local go test ./internal/userstore/... ./cmd/...
```
Expected: build clean; migrate e2e PASS; full suites green.

```bash
git add internal/userstore/store.go internal/userstore/migrate.go cmd/atterm-relay/migrate.go cmd/atterm-relay/main.go cmd/atterm-relay/migrate_test.go
git commit -m "feat(relay): atterm-relay migrate --from --to subcommand"
```

---

## 收尾

- [ ] 真实 PG 端到端手测:`./atterm-relay migrate --from sqlite:/tmp/users.db --to 'postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable'`(目标须为空)→ 检查打印的每表行数。
- [ ] 全量回归 `GOTOOLCHAIN=local go test ./...`(desktop hookinstall 预存基线失败除外)。
- [ ] 文档:在部署说明补一句 `migrate` 用法(SQLite↔Postgres 双向选型切换;目标须空)。

## 后续(不在本 plan)

- 阶段二:多实例实时路由(realm 身份、归属调度、E2EE 按 realm 重锚定),见设计 spec §6。这是阶段一(Plan 1/2/3)之后的独立大工程。
