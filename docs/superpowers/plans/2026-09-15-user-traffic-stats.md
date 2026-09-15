# 用户网络流量统计监控 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 relay 埋点统计每个用户经 relay 的收发字节数（按帧类型+方向），每日汇总落库，admin 页面按明细/分组/汇总三视图查看。

**Architecture:** relay 收发路径埋点（入站 readFrame、出站新 writeFrame helper）→ 内存 `trafficMeter` 累计 → 60s ticker + 关闭时 flush 到 `relay_traffic_daily` 表（多实例 UPSERT 累加）→ admin API 按 view 聚合（分组用静态 frameCategory 映射）→ AdminPanel 新 traffic tab。

**Tech Stack:** Go（relay + userstore/database/sql，sqlite + postgres 双方言）、Vue 3 + Naive UI + TypeScript（admin 前端）、vitest（前端测试）、go test（后端，`-race`）。

**Spec:** `docs/superpowers/specs/2026-09-15-user-traffic-stats-design.md`

## Global Constraints

- 字节口径：`wireBytes = 22 + len(f.Payload)`（6B 头 + 16B session UUID，不含 WS 帧头）。全栈统一。
- `direction`：`0 = in`（relay 收），`1 = out`（relay 发）。
- `day` 格式：`YYYY-MM-DD`，UTC。
- Store 是接口，唯一实现是 `*DBStore`（sqlite + postgres 同一实现，`dia.Rebind` 处理占位符）。无独立 memory 实现——`store_iface_test.go` 只是编译断言。
- migration 成对：每个 `sqlite/NNNN_*.sql` 必须有同名 `postgres/NNNN_*.sql`。下一个编号是 `0012`。
- SQL 占位符一律写 `?`，运行时用 `s.dia.Rebind(query)` 转换；UPSERT 用 `ON CONFLICT (...) DO UPDATE`（两方言均支持）。
- 新增用户可见前端文案必须同时加 `en.ts` + `zh-CN.ts`。
- 埋点函数永不返回 error、永不 panic、永不阻塞收发路径。
- Server 无 context 参数；`trafficMeter` 自持 ticker + stop channel，新增 `Server.Close()` 停 ticker 并 flush，由 `cmd/atterm-relay/main.go` shutdown 块调用。

---

## File Structure

新增：
- `internal/relay/traffic_category.go` — `frameCategory(proto.Type) string` 静态映射（单一事实源）
- `internal/relay/traffic_meter.go` — `trafficMeter` 内存聚合器 + ticker 生命周期
- `internal/relay/admin_traffic.go` — `GET /admin/api/traffic` handler + view 聚合
- `internal/userstore/traffic.go` — `AddTrafficDeltas` / `QueryTraffic` 实现 + `TrafficDelta`/`TrafficRow` 类型
- `internal/userstore/migrations/sqlite/0012_relay_traffic.sql`
- `internal/userstore/migrations/postgres/0012_relay_traffic.sql`
- `desktop/frontend/src/components/admin/Traffic.vue`

修改：
- `internal/userstore/store.go` — Store 接口加两方法
- `internal/relay/server.go` — trafficMeter 字段 + NewServer 启动 + Close() + readFrame 计量 + writeFrame helper
- `internal/relay/uplink_conn.go` / `client_conn.go` / `client_sessions_conn.go` — 换 writeFrame + 读循环计量
- `cmd/atterm-relay/main.go` — shutdown 块调 `srv.Close()`
- `web/src/shared/api/admin.ts` + `web/src/shared/api/types.ts` — getTrafficStats + 类型
- `desktop/frontend/src/components/AdminPanel.vue` — traffic tab
- `desktop/frontend/src/i18n/messages/en.ts` + `zh-CN.ts` — 文案

---

## Task 1: 帧类型 → 语义类别映射

**Files:**
- Create: `internal/relay/traffic_category.go`
- Test: `internal/relay/traffic_category_test.go`

**Interfaces:**
- Produces: `func frameCategory(t proto.Type) string`（返回 `"terminal"|"state"|"config"|"fs"|"preview"|"other"`）

- [ ] **Step 1: Write the failing test**

```go
package relay

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
)

func TestFrameCategory(t *testing.T) {
	cases := map[proto.Type]string{
		proto.TypeIn:            "terminal",
		proto.TypeOut:           "terminal",
		proto.TypeResize:        "terminal",
		proto.TypePasteImage:    "terminal",
		proto.TypePasteFile:     "terminal",
		proto.TypeMeta:          "state",
		proto.TypeList:          "state",
		proto.TypeListResp:      "state",
		proto.TypeReplayProgress: "state",
		proto.TypeAnnounce:      "state",
		proto.TypeViewers:       "state",
		proto.TypeOpen:          "state",
		proto.TypeClose:         "state",
		proto.TypeStreamRequest: "state",
		proto.TypeStreamStop:    "state",
		proto.TypeCommandEvent:  "state",
		proto.TypeSessionCreate: "state",
		proto.TypeSessionCreated: "state",
		proto.TypeAttach:        "state",
		proto.TypeClaimDriver:   "state",
		proto.TypePing:          "state",
		proto.TypePong:          "state",
		proto.TypePrefsChanged:  "config",
		proto.TypeAuthInfo:      "config",
		proto.TypeFSRequest:     "fs",
		proto.TypeFSResponse:    "fs",
		proto.TypeFSEvent:       "fs",
		proto.TypeServiceOpen:   "preview",
		proto.TypeServiceOpened: "preview",
		proto.TypeServiceClose:  "preview",
	}
	for ft, want := range cases {
		if got := frameCategory(ft); got != want {
			t.Errorf("frameCategory(0x%02x) = %q, want %q", byte(ft), got, want)
		}
	}
	// Unknown type falls back to "other".
	if got := frameCategory(proto.Type(0xff)); got != "other" {
		t.Errorf("frameCategory(unknown) = %q, want other", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestFrameCategory -v`
Expected: FAIL（`undefined: frameCategory`）

- [ ] **Step 3: Write the implementation**

```go
package relay

import "github.com/attson/atterm/internal/proto"

// Traffic semantic categories. Kept as a single source of truth so the
// admin "group" view and any future consumer classify frames identically.
const (
	catTerminal = "terminal" // PTY byte transport: keystrokes, output, resize, paste
	catState    = "state"    // session lifecycle + metadata sync
	catConfig   = "config"   // preferences / identity sync
	catFS       = "fs"       // remote file explorer
	catPreview  = "preview"  // remote web preview control
	catOther    = "other"    // unclassified / future frame types
)

// frameCategory maps a wire frame type to its semantic traffic category.
// Every proto.Type defined today has an explicit case; new types fall
// through to catOther until classified (guarded by an exhaustive test).
func frameCategory(t proto.Type) string {
	switch t {
	case proto.TypeIn, proto.TypeOut, proto.TypeResize,
		proto.TypePasteImage, proto.TypePasteFile:
		return catTerminal
	case proto.TypeMeta, proto.TypeList, proto.TypeListResp,
		proto.TypeReplayProgress, proto.TypeAnnounce, proto.TypeViewers,
		proto.TypeOpen, proto.TypeClose, proto.TypeStreamRequest,
		proto.TypeStreamStop, proto.TypeCommandEvent, proto.TypeSessionCreate,
		proto.TypeSessionCreated, proto.TypeAttach, proto.TypeClaimDriver,
		proto.TypePing, proto.TypePong:
		return catState
	case proto.TypePrefsChanged, proto.TypeAuthInfo:
		return catConfig
	case proto.TypeFSRequest, proto.TypeFSResponse, proto.TypeFSEvent:
		return catFS
	case proto.TypeServiceOpen, proto.TypeServiceOpened, proto.TypeServiceClose:
		return catPreview
	default:
		return catOther
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestFrameCategory -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/relay/traffic_category.go internal/relay/traffic_category_test.go
git commit -m "feat(traffic): frame type to semantic category mapping"
```

---

## Task 2: 内存流量聚合器 trafficMeter

**Files:**
- Create: `internal/relay/traffic_meter.go`
- Test: `internal/relay/traffic_meter_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type trafficDelta struct { UserID string; FrameType int; Direction int; Bytes int64; Frames int64 }`
  - `type trafficMeter struct { ... }`
  - `func newTrafficMeter() *trafficMeter`
  - `func (m *trafficMeter) add(userID string, t proto.Type, dir int, wireBytes int)`（`userID==""` 时 no-op）
  - `func (m *trafficMeter) snapshot() []trafficDelta`（读走增量并清空）
  - 常量 `trafficIn = 0`, `trafficOut = 1`

- [ ] **Step 1: Write the failing test**

```go
package relay

import (
	"sync"
	"testing"

	"github.com/attson/atterm/internal/proto"
)

func TestTrafficMeterAddAndSnapshot(t *testing.T) {
	m := newTrafficMeter()
	m.add("u1", proto.TypeOut, trafficOut, 100)
	m.add("u1", proto.TypeOut, trafficOut, 50)   // same key accumulates
	m.add("u1", proto.TypeIn, trafficIn, 10)     // different direction
	m.add("u2", proto.TypeMeta, trafficOut, 30)  // different user
	m.add("", proto.TypeOut, trafficOut, 999)    // empty user is dropped

	got := m.snapshot()
	// Build a lookup for assertion order-independence.
	type k struct{ u string; ft, dir int }
	sum := map[k]int64{}
	for _, d := range got {
		sum[k{d.UserID, d.FrameType, d.Direction}] += d.Bytes
	}
	if sum[k{"u1", int(proto.TypeOut), trafficOut}] != 150 {
		t.Errorf("u1 OUT bytes = %d, want 150", sum[k{"u1", int(proto.TypeOut), trafficOut}])
	}
	if sum[k{"u1", int(proto.TypeIn), trafficIn}] != 10 {
		t.Errorf("u1 IN bytes = %d, want 10", sum[k{"u1", int(proto.TypeIn), trafficIn}])
	}
	if sum[k{"u2", int(proto.TypeMeta), trafficOut}] != 30 {
		t.Errorf("u2 META bytes = %d, want 30", sum[k{"u2", int(proto.TypeMeta), trafficOut}])
	}
	if _, ok := sum[k{"", int(proto.TypeOut), trafficOut}]; ok {
		t.Error("empty userID should be dropped")
	}
	// Frames counted: u1 OUT saw 2 adds.
	var u1OutFrames int64
	for _, d := range got {
		if d.UserID == "u1" && d.FrameType == int(proto.TypeOut) && d.Direction == trafficOut {
			u1OutFrames = d.Frames
		}
	}
	if u1OutFrames != 2 {
		t.Errorf("u1 OUT frames = %d, want 2", u1OutFrames)
	}
}

func TestTrafficMeterSnapshotClearsIncrement(t *testing.T) {
	m := newTrafficMeter()
	m.add("u1", proto.TypeOut, trafficOut, 100)
	_ = m.snapshot()
	// Second snapshot with no adds in between must be empty (increment drained).
	if got := m.snapshot(); len(got) != 0 {
		t.Errorf("second snapshot len = %d, want 0", len(got))
	}
}

func TestTrafficMeterConcurrentAdd(t *testing.T) {
	m := newTrafficMeter()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.add("u1", proto.TypeOut, trafficOut, 1)
		}()
	}
	wg.Wait()
	var total int64
	for _, d := range m.snapshot() {
		total += d.Bytes
	}
	if total != 100 {
		t.Errorf("concurrent add total = %d, want 100", total)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestTrafficMeter -race -v`
Expected: FAIL（`undefined: newTrafficMeter`）

- [ ] **Step 3: Write the implementation**

```go
package relay

import (
	"sync"

	"github.com/attson/atterm/internal/proto"
)

const (
	trafficIn  = 0
	trafficOut = 1
)

// trafficDelta is one accumulated (user, frame type, direction) bucket
// drained from a trafficMeter snapshot.
type trafficDelta struct {
	UserID    string
	FrameType int
	Direction int
	Bytes     int64
	Frames    int64
}

type trafficKey struct {
	userID string
	ft     int
	dir    int
}

type trafficCell struct {
	bytes  int64
	frames int64
}

// trafficMeter accumulates per-(user, frame type, direction) byte and frame
// counts in memory. It is written on the hot send/receive paths (add) and
// drained on a fixed interval (snapshot). A single mutex is sufficient:
// snapshot runs at flush cadence (~60s), so lock contention on add is
// negligible relative to the WebSocket I/O it accompanies.
type trafficMeter struct {
	mu    sync.Mutex
	cells map[trafficKey]*trafficCell
}

func newTrafficMeter() *trafficMeter {
	return &trafficMeter{cells: make(map[trafficKey]*trafficCell)}
}

// add records wireBytes for one frame. A zero-length userID (pre-auth /
// anonymous frames) is dropped: traffic is only meaningful per account.
func (m *trafficMeter) add(userID string, t proto.Type, dir int, wireBytes int) {
	if userID == "" {
		return
	}
	k := trafficKey{userID: userID, ft: int(t), dir: dir}
	m.mu.Lock()
	c := m.cells[k]
	if c == nil {
		c = &trafficCell{}
		m.cells[k] = c
	}
	c.bytes += int64(wireBytes)
	c.frames++
	m.mu.Unlock()
}

// snapshot returns all accumulated deltas and resets the meter. The caller
// (flush loop) is responsible for persisting them; a failed persist loses
// at most one interval's data (accepted trade-off, see spec §Error handling).
func (m *trafficMeter) snapshot() []trafficDelta {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]trafficDelta, 0, len(m.cells))
	for k, c := range m.cells {
		out = append(out, trafficDelta{
			UserID:    k.userID,
			FrameType: k.ft,
			Direction: k.dir,
			Bytes:     c.bytes,
			Frames:    c.frames,
		})
	}
	m.cells = make(map[trafficKey]*trafficCell)
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/ -run TestTrafficMeter -race -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/relay/traffic_meter.go internal/relay/traffic_meter_test.go
git commit -m "feat(traffic): in-memory per-user traffic meter"
```

---

## Task 3: 存储层 — migration + Store 方法

**Files:**
- Create: `internal/userstore/migrations/sqlite/0012_relay_traffic.sql`
- Create: `internal/userstore/migrations/postgres/0012_relay_traffic.sql`
- Create: `internal/userstore/traffic.go`
- Modify: `internal/userstore/store.go`（Store 接口加两方法）
- Test: `internal/userstore/contract_test.go`（加 traffic 契约测试）

**Interfaces:**
- Consumes: `*DBStore`, `s.dia.Rebind`
- Produces（在 `store.go` 的 Store 接口 + `traffic.go` 实现）:
  - `type TrafficDelta struct { UserID string; FrameType int; Direction int; Bytes int64; Frames int64 }`
  - `type TrafficRow struct { UserID string; Day string; FrameType int; Direction int; Bytes int64; Frames int64 }`
  - `AddTrafficDeltas(ctx context.Context, day string, deltas []TrafficDelta) error`
  - `QueryTraffic(ctx context.Context, from, to string) ([]TrafficRow, error)`

- [ ] **Step 1: Write the migrations**

`internal/userstore/migrations/sqlite/0012_relay_traffic.sql`:

```sql
CREATE TABLE relay_traffic_daily (
  user_id    TEXT    NOT NULL,
  day        TEXT    NOT NULL,
  frame_type INTEGER NOT NULL,
  direction  INTEGER NOT NULL,
  bytes      BIGINT  NOT NULL DEFAULT 0,
  frames     BIGINT  NOT NULL DEFAULT 0,
  updated_at BIGINT  NOT NULL,
  PRIMARY KEY (user_id, day, frame_type, direction)
);
CREATE INDEX idx_relay_traffic_day ON relay_traffic_daily (day);
```

`internal/userstore/migrations/postgres/0012_relay_traffic.sql`:

```sql
CREATE TABLE relay_traffic_daily (
  user_id    TEXT     NOT NULL,
  day        TEXT     NOT NULL,
  frame_type INTEGER  NOT NULL,
  direction  INTEGER  NOT NULL,
  bytes      BIGINT   NOT NULL DEFAULT 0,
  frames     BIGINT   NOT NULL DEFAULT 0,
  updated_at BIGINT   NOT NULL,
  PRIMARY KEY (user_id, day, frame_type, direction)
);
CREATE INDEX idx_relay_traffic_day ON relay_traffic_daily (day);
```

- [ ] **Step 2: Write the Store implementation**

`internal/userstore/traffic.go`:

```go
package userstore

import (
	"context"
	"fmt"
	"time"
)

// TrafficDelta is one increment to fold into the daily rollup.
type TrafficDelta struct {
	UserID    string
	FrameType int
	Direction int
	Bytes     int64
	Frames    int64
}

// TrafficRow is one persisted daily rollup row.
type TrafficRow struct {
	UserID    string
	Day       string
	FrameType int
	Direction int
	Bytes     int64
	Frames    int64
}

// AddTrafficDeltas folds deltas into relay_traffic_daily for the given UTC
// day. Each (user_id, day, frame_type, direction) row is upserted with
// bytes/frames accumulated, so multiple relay instances writing the same
// row converge to a global total.
func (s *DBStore) AddTrafficDeltas(ctx context.Context, day string, deltas []TrafficDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin traffic tx: %w", err)
	}
	now := time.Now().Unix()
	stmt := s.dia.Rebind(`INSERT INTO relay_traffic_daily
		(user_id, day, frame_type, direction, bytes, frames, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, day, frame_type, direction)
		DO UPDATE SET bytes = relay_traffic_daily.bytes + excluded.bytes,
		              frames = relay_traffic_daily.frames + excluded.frames,
		              updated_at = excluded.updated_at`)
	for _, d := range deltas {
		if _, err := tx.ExecContext(ctx, stmt,
			d.UserID, day, d.FrameType, d.Direction, d.Bytes, d.Frames, now); err != nil {
			tx.Rollback()
			return fmt.Errorf("upsert traffic: %w", err)
		}
	}
	return tx.Commit()
}

// QueryTraffic returns all rollup rows with day in [from, to] (inclusive,
// lexicographic on the YYYY-MM-DD strings). Aggregation into detail/group/
// summary views is the caller's job.
func (s *DBStore) QueryTraffic(ctx context.Context, from, to string) ([]TrafficRow, error) {
	rows, err := s.db.QueryContext(ctx, s.dia.Rebind(
		`SELECT user_id, day, frame_type, direction, bytes, frames
		 FROM relay_traffic_daily
		 WHERE day >= ? AND day <= ?
		 ORDER BY day, user_id, frame_type, direction`), from, to)
	if err != nil {
		return nil, fmt.Errorf("query traffic: %w", err)
	}
	defer rows.Close()
	var out []TrafficRow
	for rows.Next() {
		var r TrafficRow
		if err := rows.Scan(&r.UserID, &r.Day, &r.FrameType, &r.Direction, &r.Bytes, &r.Frames); err != nil {
			return nil, fmt.Errorf("scan traffic: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 3: Add the two methods to the Store interface**

在 `internal/userstore/store.go` 的 `Store` 接口内，`Close() error` 之前加一节：

```go
	// Traffic accounting (per-user daily byte/frame rollup by frame type).
	AddTrafficDeltas(ctx context.Context, day string, deltas []TrafficDelta) error
	QueryTraffic(ctx context.Context, from, to string) ([]TrafficRow, error)
```

- [ ] **Step 4: Write the contract test**

在 `internal/userstore/contract_test.go` 的 `runStoreContract` 函数体内追加一个子测试（该函数已对 sqlite + postgres 双跑）：

```go
	t.Run("Traffic", func(t *testing.T) {
		s := open(t)
		ctx := context.Background()
		day := "2026-09-15"
		// First fold.
		if err := s.AddTrafficDeltas(ctx, day, []TrafficDelta{
			{UserID: "u1", FrameType: 0x03, Direction: 1, Bytes: 100, Frames: 2},
			{UserID: "u1", FrameType: 0x02, Direction: 0, Bytes: 10, Frames: 1},
		}); err != nil {
			t.Fatalf("AddTrafficDeltas #1: %v", err)
		}
		// Second fold on the same key must accumulate, not overwrite.
		if err := s.AddTrafficDeltas(ctx, day, []TrafficDelta{
			{UserID: "u1", FrameType: 0x03, Direction: 1, Bytes: 50, Frames: 1},
		}); err != nil {
			t.Fatalf("AddTrafficDeltas #2: %v", err)
		}
		rows, err := s.QueryTraffic(ctx, "2026-09-01", "2026-09-30")
		if err != nil {
			t.Fatalf("QueryTraffic: %v", err)
		}
		var outBytes, outFrames int64
		for _, r := range rows {
			if r.UserID == "u1" && r.FrameType == 0x03 && r.Direction == 1 {
				outBytes, outFrames = r.Bytes, r.Frames
			}
		}
		if outBytes != 150 || outFrames != 3 {
			t.Errorf("u1 OUT rollup = (%d bytes, %d frames), want (150, 3)", outBytes, outFrames)
		}
		// Out-of-range day excluded.
		if got, _ := s.QueryTraffic(ctx, "2026-10-01", "2026-10-31"); len(got) != 0 {
			t.Errorf("out-of-range query returned %d rows, want 0", len(got))
		}
	})
```

（若 `contract_test.go` 顶部未 import `context`，补上。）

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/userstore/ -run TestStoreContract/Traffic -v`
（若本机无 postgres，sqlite 子跑通过即可；CI 跑 postgres。）
Expected: PASS（sqlite 分支）；整体 `go build ./...` 通过（编译断言 `var _ Store = (*DBStore)(nil)` 成立）。

- [ ] **Step 6: Commit**

```bash
git add internal/userstore/migrations/sqlite/0012_relay_traffic.sql \
        internal/userstore/migrations/postgres/0012_relay_traffic.sql \
        internal/userstore/traffic.go internal/userstore/store.go \
        internal/userstore/contract_test.go
git commit -m "feat(traffic): relay_traffic_daily table + upsert/query store methods"
```

---

## Task 4: 埋点接线 — writeFrame helper + readFrame 计量 + meter 生命周期

**Files:**
- Modify: `internal/relay/server.go`（Server 字段、NewServer、Close、recordTraffic、writeFrame、readFrame 计量）
- Modify: `internal/relay/uplink_conn.go`（读循环 add + 出站换 writeFrame）
- Modify: `internal/relay/client_conn.go`（读循环 add + 出站换 writeFrame）
- Modify: `internal/relay/client_sessions_conn.go`（出站换 writeFrame）
- Modify: `cmd/atterm-relay/main.go`（shutdown 调 srv.Close）
- Test: `internal/relay/traffic_meter_test.go`（追加 flush 生命周期测试）

**Interfaces:**
- Consumes: `newTrafficMeter`, `trafficMeter.add/snapshot`, `frameCategory`, `Store.AddTrafficDeltas`
- Produces:
  - `Server.traffic *trafficMeter` 字段
  - `func (s *Server) recordTraffic(userID string, t proto.Type, dir int, wireBytes int)`
  - `func (s *Server) writeFrame(ctx context.Context, userID string, c *websocket.Conn, f proto.Frame) error`
  - `func (s *Server) Close()`（停 ticker + 最后 flush）
  - `readFrame` 增加计量：改为方法 `func (s *Server) readFrameMetered(ctx, userID string, c *websocket.Conn) (proto.Frame, error)`，或保留 `readFrame` 并在调用址 add（本 plan 采用后者，改动小、语义清晰）

- [ ] **Step 1: 加 Server 字段 + meter 启动 + Close（server.go）**

在 `Server` struct 加字段（`uplinkCount int64` 之后）：

```go
	// traffic accumulates per-user byte/frame counts on the send/receive
	// paths; flushStop signals the flush goroutine to drain and exit.
	traffic   *trafficMeter
	flushStop chan struct{}
	flushDone chan struct{}
```

在 `NewServer` 里，`s := &Server{...}` 之后、返回之前，初始化并启动 flush 循环（仅当 store 存在）：

```go
	s.traffic = newTrafficMeter()
	if cfg.Store != nil {
		s.flushStop = make(chan struct{})
		s.flushDone = make(chan struct{})
		go s.trafficFlushLoop(cfg.Store)
	}
```

新增方法（放 server.go 末尾或 traffic_meter.go；本 plan 放 traffic_meter.go 以聚合流量代码）：

```go
// trafficFlushInterval is how often accumulated traffic is folded into the
// daily rollup. A crash loses at most this window (see spec §Error handling).
const trafficFlushInterval = 60 * time.Second

func (s *Server) trafficFlushLoop(store userstore.Store) {
	defer close(s.flushDone)
	t := time.NewTicker(trafficFlushInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.flushTraffic(store)
		case <-s.flushStop:
			s.flushTraffic(store) // final drain
			return
		}
	}
}

func (s *Server) flushTraffic(store userstore.Store) {
	deltas := s.traffic.snapshot()
	if len(deltas) == 0 {
		return
	}
	day := time.Now().UTC().Format("2006-01-02")
	conv := make([]userstore.TrafficDelta, len(deltas))
	for i, d := range deltas {
		conv[i] = userstore.TrafficDelta{
			UserID: d.UserID, FrameType: d.FrameType, Direction: d.Direction,
			Bytes: d.Bytes, Frames: d.Frames,
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.AddTrafficDeltas(ctx, day, conv); err != nil {
		logging.Warn("relay-traffic", "flush failed, dropping %d deltas: %v", len(conv), err)
	}
}

// Close stops the traffic flush loop and drains a final snapshot. Called
// from the binary's shutdown path. Safe to call when the loop never started
// (no store).
func (s *Server) Close() {
	if s.flushStop != nil {
		close(s.flushStop)
		<-s.flushDone
	}
}

// recordTraffic folds one frame's wire size into the meter. Never blocks the
// caller beyond a mutex-guarded map write; a zero userID is dropped.
func (s *Server) recordTraffic(userID string, t proto.Type, dir int, wireBytes int) {
	s.traffic.add(userID, t, dir, wireBytes)
}
```

（`server.go` 需确保 import 了 `time`、`context`、`logging`、`userstore`、`proto` — 前几个已在，按缺补齐。）

- [ ] **Step 2: 加 writeFrame helper（server.go，readFrame 附近）**

```go
// writeFrame marshals f, writes it as one binary WS message, and — on a
// successful write — records its wire size against userID for traffic
// accounting. It centralizes the previously inlined
// c.Write(ctx, MessageBinary, proto.Marshal(f)) calls so every outbound
// frame is metered at exactly one place.
func (s *Server) writeFrame(ctx context.Context, userID string, c *websocket.Conn, f proto.Frame) error {
	b := proto.Marshal(f)
	if err := c.Write(ctx, websocket.MessageBinary, b); err != nil {
		return err
	}
	s.recordTraffic(userID, f.Type, trafficOut, len(b))
	return nil
}
```

- [ ] **Step 3: 出站写点换用 writeFrame**

把以下内联 `c.Write(ctx, websocket.MessageBinary, proto.Marshal(f))`（或等价）替换为 `s.writeFrame(ctx, <userID>, c, f)`，`<userID>` 用该连接已持有的 owner id：

- `uplink_conn.go`：AUTH_INFO 握手写（~:101，userID = ownerUserID）、writeLoop 主写（~:466，userID = u.ownerUserID）。
- `client_conn.go`：targetedOut 写（~:123）、subOut 写（~:154）、LIST_RESP 内联（~:218）、PONG 内联（~:500）。这些连接的 owner 是 `ownerUserID`（`handleClient` 参数）。
- `client_sessions_conn.go`：PREFS_CHANGED（~:70）、LIST_RESP（~:94），userID = 该连接已认证 user id。

注意：`writeLoop`/writer goroutine 若不是 `Server` 的方法，需要能访问 `s`。uplink/client 处理器都由 `Server` 方法启动，`s` 在闭包作用域内可用；若某写循环在独立函数里，改为传入 `s` 或把 owner + `*Server` 一起带进闭包。逐处按编译错误修正。

- [ ] **Step 4: 入站读循环计量**

在每个读循环 `f, err := readFrame(ctx, c)` 成功后加一行：

```go
s.recordTraffic(<ownerUserID>, f.Type, trafficIn, 22+len(f.Payload))
```

- `uplink_conn.go` readLoop（~:481，userID = u.ownerUserID）
- `client_conn.go` 读循环（~:184，userID = ownerUserID）
- `agent_conn.go` 读循环（~:110）：若该连接持有 owner id 则计；`handleAgent` 若无 owner 参数则跳过（传空 userID，被 add 丢弃）——保持行为安全，spec 已记为可接受。

- [ ] **Step 5: main.go shutdown 接线**

在 `cmd/atterm-relay/main.go` 的 `<-ctx.Done()` 之后、`for _, s := range servers { s.Shutdown(...) }` 附近，加对 relay Server 的 Close（变量名是 `srv`）：

```go
	srv.Close() // drain final traffic snapshot before HTTP servers stop
```

- [ ] **Step 6: flush 生命周期测试（traffic_meter_test.go 追加）**

```go
func TestServerFlushTrafficPersists(t *testing.T) {
	store := newContractSQLiteStore(t) // helper that opens :memory: DBStore; if
	// none exists in relay tests, use userstore.Open(context.Background(), ":memory:")
	srv := NewServer(Config{Store: store, Resolver: NewIdentityResolver(store)})
	defer srv.Close()

	srv.recordTraffic("u1", proto.TypeOut, trafficOut, 120)
	srv.flushTraffic(store) // force a flush without waiting for the ticker

	day := time.Now().UTC().Format("2006-01-02")
	rows, err := store.QueryTraffic(context.Background(), day, day)
	if err != nil {
		t.Fatalf("QueryTraffic: %v", err)
	}
	var total int64
	for _, r := range rows {
		if r.UserID == "u1" {
			total += r.Bytes
		}
	}
	if total != 120 {
		t.Errorf("persisted bytes = %d, want 120", total)
	}
}
```

（`newContractSQLiteStore` 若不存在，直接 `store, _ := userstore.Open(context.Background(), ":memory:")`。查 `helpers_test.go` 现有 store 构造方式复用。）

- [ ] **Step 7: Run tests**

Run: `go test ./internal/relay/ -run 'TestTraffic|TestServerFlushTraffic|TestFrameCategory' -race -v`
然后全包编译：`go build ./...`
Expected: PASS；build 通过。

- [ ] **Step 8: Commit**

```bash
git add internal/relay/server.go internal/relay/uplink_conn.go \
        internal/relay/client_conn.go internal/relay/client_sessions_conn.go \
        internal/relay/agent_conn.go internal/relay/traffic_meter.go \
        internal/relay/traffic_meter_test.go cmd/atterm-relay/main.go
git commit -m "feat(traffic): meter send/receive paths and flush to daily rollup"
```

---

## Task 5: admin API — GET /admin/api/traffic

**Files:**
- Create: `internal/relay/admin_traffic.go`
- Modify: `internal/relay/server.go`（注册路由）
- Test: `internal/relay/admin_traffic_test.go`

**Interfaces:**
- Consumes: `Store.QueryTraffic`, `Store.ListUsers`, `frameCategory`, `frameTypeName`（`debug.go` 已有；若不可访问则本 handler 内联一个 name 映射）
- Produces: `func (s *Server) handleAdminTrafficHTTP(w http.ResponseWriter, r *http.Request)` + JSON 响应结构

- [ ] **Step 1: Write the failing test**

```go
package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdminTrafficView(t *testing.T) {
	srv, adminTok, _ := newAdminTestServer(t) // see note below
	// Seed rollup rows directly through the store.
	day := time.Now().UTC().Format("2006-01-02")
	if err := srv.cfg.Store.AddTrafficDeltas(context.Background(), day, []TrafficDelta_relay{}); err != nil {
		// NOTE: use userstore.TrafficDelta via the store; adjust import/type.
	}
	_ = srv.cfg.Store.(interface {
		AddTrafficDeltas(context.Context, string, []userstoreTrafficDelta) error
	})

	req := httptest.NewRequest("GET", "/admin/api/traffic?view=group&from="+day+"&to="+day, nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		View string `json:"view"`
		Rows []struct {
			Category  string `json:"category"`
			Direction int    `json:"direction"`
			Bytes     int64  `json:"bytes"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.View != "group" {
		t.Errorf("view = %q, want group", resp.View)
	}
}

func TestAdminTrafficRejectsNonAdmin(t *testing.T) {
	srv, _, userTok := newAdminTestServer(t) // userTok is a non-admin session
	req := httptest.NewRequest("GET", "/admin/api/traffic", nil)
	req.Header.Set("Authorization", "Bearer "+userTok)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("non-admin got 200, want 401/403")
	}
}
```

> **实现者注**：上面的测试骨架示意了断言目标，但类型名是占位（`TrafficDelta_relay` / `userstoreTrafficDelta` 不是真实类型）。落地时：(1) 用 `userstore.TrafficDelta` 通过 `srv.cfg.Store.AddTrafficDeltas` 播种数据；(2) `newAdminTestServer` 若不存在，参照 `helpers_test.go:109` 的 `NewServer(Config{Store, Resolver})` + 现有的建 admin 用户 / 建 session token 辅助（`firstrun_admin_test.go` / `admin_http_test.go` 里有先例，复用其建 admin+token 的方式），返回 `(srv, adminToken, nonAdminToken)`。先把测试写到能编译并 FAIL 于「路由未注册 → 404/401」。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/ -run TestAdminTraffic -v`
Expected: FAIL（handler/路由不存在）

- [ ] **Step 3: Write the handler**

`internal/relay/admin_traffic.go`:

```go
package relay

import (
	"net/http"
	"time"

	"github.com/attson/atterm/internal/proto"
)

type trafficRow struct {
	UserID        string `json:"user_id"`
	Email         string `json:"email,omitempty"`
	FrameType     int    `json:"frame_type,omitempty"`
	FrameTypeName string `json:"frame_type_name,omitempty"`
	Category      string `json:"category,omitempty"`
	Direction     int    `json:"direction"`
	Bytes         int64  `json:"bytes"`
	Frames        int64  `json:"frames"`
}

type trafficResponse struct {
	View string       `json:"view"`
	From string       `json:"from"`
	To   string       `json:"to"`
	Rows []trafficRow `json:"rows"`
}

// handleAdminTrafficHTTP implements GET /admin/api/traffic. Query params:
//   view = detail | group | summary   (default: group)
//   from, to = YYYY-MM-DD             (default: last 7 days ending today UTC)
func (s *Server) handleAdminTrafficHTTP(w http.ResponseWriter, r *http.Request) {
	view := r.URL.Query().Get("view")
	switch view {
	case "detail", "group", "summary":
	case "":
		view = "group"
	default:
		http.Error(w, "invalid view", http.StatusBadRequest)
		return
	}
	today := time.Now().UTC()
	to := r.URL.Query().Get("to")
	from := r.URL.Query().Get("from")
	if to == "" {
		to = today.Format("2006-01-02")
	}
	if from == "" {
		from = today.AddDate(0, 0, -6).Format("2006-01-02")
	}
	if !validDay(from) || !validDay(to) {
		http.Error(w, "invalid date, use YYYY-MM-DD", http.StatusBadRequest)
		return
	}

	rows, err := s.cfg.Store.QueryTraffic(r.Context(), from, to)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	emails := s.userEmailMap(r) // id -> email; best-effort

	// Aggregate by view. Key depends on the view's grouping columns.
	type aggKey struct {
		user string
		disc int // frame_type (detail) or category-index; unused for summary
		cat  string
		dir  int
	}
	agg := map[aggKey]*trafficRow{}
	for _, rr := range rows {
		var k aggKey
		k.user = rr.UserID
		k.dir = rr.Direction
		switch view {
		case "detail":
			k.disc = rr.FrameType
		case "group":
			k.cat = frameCategory(proto.Type(rr.FrameType))
		case "summary":
			// user + direction only
		}
		cell := agg[k]
		if cell == nil {
			cell = &trafficRow{UserID: rr.UserID, Email: emails[rr.UserID], Direction: rr.Direction}
			switch view {
			case "detail":
				cell.FrameType = rr.FrameType
				cell.FrameTypeName = frameTypeName(proto.Type(rr.FrameType))
			case "group":
				cell.Category = k.cat
			}
			agg[k] = cell
		}
		cell.Bytes += rr.Bytes
		cell.Frames += rr.Frames
	}
	out := make([]trafficRow, 0, len(agg))
	for _, c := range agg {
		out = append(out, *c)
	}
	writeJSONStatus(w, http.StatusOK, trafficResponse{View: view, From: from, To: to, Rows: out})
}

func validDay(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// userEmailMap builds a best-effort id->email map for annotating rows.
// On error it returns an empty map (rows just lack emails).
func (s *Server) userEmailMap(r *http.Request) map[string]string {
	m := map[string]string{}
	users, err := s.cfg.Store.ListUsers(r.Context())
	if err != nil {
		return m
	}
	for _, u := range users {
		m[u.ID] = u.Email
	}
	return m
}
```

> **实现者注**：`frameTypeName` 定义在 `internal/relay/debug.go:131`（同包可直接调）。`writeJSONStatus` 是同包已有 helper（`admin_users.go` 在用）。若 `frameTypeName` 签名不符，内联一个 `switch proto.Type` 返回短名。

- [ ] **Step 4: Register the route（server.go）**

在 admin-only 路由区块（`/admin/api/feishu/generate-key` 附近）加：

```go
	s.mux.HandleFunc("GET /admin/api/traffic", s.requireSession(s.requireAdminAccess(s.handleAdminTrafficHTTP)))
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/relay/ -run TestAdminTraffic -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/relay/admin_traffic.go internal/relay/admin_traffic_test.go internal/relay/server.go
git commit -m "feat(traffic): admin API GET /admin/api/traffic with detail/group/summary views"
```

---

## Task 6: 前端 API 客户端 + 类型

**Files:**
- Modify: `web/src/shared/api/admin.ts`
- Modify: `web/src/shared/api/types.ts`

**Interfaces:**
- Consumes: `apiFetch`（`./client`）
- Produces: `getTrafficStats(params)`, `AdminTrafficRow`, `AdminTrafficResponse`, `TrafficView`

- [ ] **Step 1: Add types（types.ts）**

在 `web/src/shared/api/types.ts` 加：

```ts
export type TrafficView = "detail" | "group" | "summary";

export interface AdminTrafficRow {
  user_id: string;
  email?: string;
  frame_type?: number;
  frame_type_name?: string;
  category?: string;
  direction: number; // 0 = in, 1 = out
  bytes: number;
  frames: number;
}

export interface AdminTrafficResponse {
  view: TrafficView;
  from: string;
  to: string;
  rows: AdminTrafficRow[];
}
```

- [ ] **Step 2: Add API function（admin.ts）**

参照同文件现有 `getAdminConfig` 的 `apiFetch` 用法，加：

```ts
import type { AdminTrafficResponse, TrafficView } from "./types";

export async function getTrafficStats(params: {
  view?: TrafficView;
  from?: string;
  to?: string;
}): Promise<AdminTrafficResponse> {
  const q = new URLSearchParams();
  if (params.view) q.set("view", params.view);
  if (params.from) q.set("from", params.from);
  if (params.to) q.set("to", params.to);
  const { data } = await apiFetch<AdminTrafficResponse>(
    `/admin/api/traffic?${q.toString()}`,
  );
  return data;
}
```

（`apiFetch` 的 import 与解构形状照抄同文件既有函数；若返回不是 `{ data }` 而是直接 body，按现有函数一致处理。）

- [ ] **Step 3: Typecheck**

Run: `cd desktop/frontend && npm run type-check`（或仓库既有的 vue-tsc 脚本；若脚本名不同，查 `package.json`）
Expected: 无类型错误。

- [ ] **Step 4: Commit**

```bash
git add web/src/shared/api/admin.ts web/src/shared/api/types.ts
git commit -m "feat(traffic): frontend admin traffic API client + types"
```

---

## Task 7: 前端 Traffic 面板 + tab 接线 + i18n

**Files:**
- Create: `desktop/frontend/src/components/admin/Traffic.vue`
- Modify: `desktop/frontend/src/components/AdminPanel.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Test: `desktop/frontend/src/components/admin/Traffic.spec.ts`（vitest，若目录约定用 `__tests__` 则随之）

**Interfaces:**
- Consumes: `getTrafficStats`, `AdminTrafficRow`, `useI18n`, Naive UI 组件
- Produces: `Traffic.vue` 组件；`AdminPanel` 的 `"traffic"` tab

- [ ] **Step 1: i18n 文案（en.ts + zh-CN.ts）**

在两个文件的 `admin` 段加（键名与现有 `configTab`/`feishuTab` 并列）：

en.ts:
```ts
    trafficTab: "Traffic",
    trafficViewDetail: "Detail",
    trafficViewGroup: "Grouped",
    trafficViewSummary: "Summary",
    trafficColUser: "User",
    trafficColCategory: "Category",
    trafficColFrameType: "Frame type",
    trafficColDirection: "Direction",
    trafficColBytes: "Bytes",
    trafficColFrames: "Frames",
    trafficDirIn: "In",
    trafficDirOut: "Out",
    trafficEmpty: "No traffic recorded for this range.",
```

zh-CN.ts:
```ts
    trafficTab: "流量",
    trafficViewDetail: "明细",
    trafficViewGroup: "分组",
    trafficViewSummary: "汇总",
    trafficColUser: "用户",
    trafficColCategory: "类别",
    trafficColFrameType: "帧类型",
    trafficColDirection: "方向",
    trafficColBytes: "字节",
    trafficColFrames: "帧数",
    trafficDirIn: "接收",
    trafficDirOut: "发送",
    trafficEmpty: "该时间范围内暂无流量记录。",
```

- [ ] **Step 2: Write the component（Traffic.vue）**

参照 `admin/Config.vue` 的结构（`<script setup lang="ts">` + Naive UI + `useI18n` + `useMessage`）。核心：视图切换 + 日期范围 + 表格 + 人类可读字节。

```vue
<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { NButtonGroup, NButton, NDataTable, NSpin } from "naive-ui";
import { useI18n } from "@shared/i18n/useI18n";
import { getTrafficStats } from "@shared/api/admin";
import type { AdminTrafficRow, TrafficView } from "@shared/api/types";

const { t } = useI18n();
const view = ref<TrafficView>("group");
const rows = ref<AdminTrafficRow[]>([]);
const loading = ref(false);

function humanBytes(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

async function load() {
  loading.value = true;
  try {
    const resp = await getTrafficStats({ view: view.value });
    rows.value = resp.rows;
  } finally {
    loading.value = false;
  }
}

function setView(v: TrafficView) {
  view.value = v;
  load();
}

const columns = computed(() => {
  const cols: any[] = [
    { title: t("admin.trafficColUser"), key: "user", render: (r: AdminTrafficRow) => r.email || r.user_id },
  ];
  if (view.value === "detail") {
    cols.push({ title: t("admin.trafficColFrameType"), key: "ft", render: (r: AdminTrafficRow) => r.frame_type_name || String(r.frame_type) });
  } else if (view.value === "group") {
    cols.push({ title: t("admin.trafficColCategory"), key: "cat", render: (r: AdminTrafficRow) => r.category });
  }
  cols.push(
    { title: t("admin.trafficColDirection"), key: "dir", render: (r: AdminTrafficRow) => r.direction === 0 ? t("admin.trafficDirIn") : t("admin.trafficDirOut") },
    { title: t("admin.trafficColBytes"), key: "bytes", render: (r: AdminTrafficRow) => humanBytes(r.bytes) },
    { title: t("admin.trafficColFrames"), key: "frames", render: (r: AdminTrafficRow) => r.frames },
  );
  return cols;
});

onMounted(load);
</script>

<template>
  <div class="traffic">
    <n-button-group>
      <n-button :type="view === 'detail' ? 'primary' : 'default'" @click="setView('detail')">{{ t("admin.trafficViewDetail") }}</n-button>
      <n-button :type="view === 'group' ? 'primary' : 'default'" @click="setView('group')">{{ t("admin.trafficViewGroup") }}</n-button>
      <n-button :type="view === 'summary' ? 'primary' : 'default'" @click="setView('summary')">{{ t("admin.trafficViewSummary") }}</n-button>
    </n-button-group>
    <n-spin :show="loading">
      <n-data-table
        v-if="rows.length"
        :columns="columns"
        :data="rows"
        :row-key="(r) => `${r.user_id}-${r.frame_type ?? r.category ?? ''}-${r.direction}`"
        data-test="traffic-table"
      />
      <p v-else class="empty" data-test="traffic-empty">{{ t("admin.trafficEmpty") }}</p>
    </n-spin>
  </div>
</template>

<style scoped>
.traffic { display: flex; flex-direction: column; gap: 16px; }
.empty { color: var(--fg-dim); }
</style>
```

（`any[]` 若违反 lint，按 Naive UI 的 `DataTableColumns<AdminTrafficRow>` 类型标注。）

- [ ] **Step 3: Wire the tab（AdminPanel.vue）**

三处改动：
- import：`import Traffic from "./admin/Traffic.vue";`
- 类型：`type AdminTabKey = "invitations" | "users" | "config" | "feishu" | "traffic";`
- tabs 数组加：`{ key: "traffic", label: t("admin.trafficTab") },`
- body 加：`<Traffic v-if="active === 'traffic'" />`

- [ ] **Step 4: Write a component test（Traffic.spec.ts）**

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount } from "@vue/test-utils";
import Traffic from "../Traffic.vue";

vi.mock("@shared/api/admin", () => ({
  getTrafficStats: vi.fn().mockResolvedValue({
    view: "group",
    from: "2026-09-15",
    to: "2026-09-15",
    rows: [
      { user_id: "u1", email: "a@b.c", category: "terminal", direction: 1, bytes: 2048, frames: 5 },
    ],
  }),
}));

describe("Traffic.vue", () => {
  beforeEach(() => vi.clearAllMocks());

  it("renders rows from getTrafficStats", async () => {
    const wrapper = mount(Traffic, { global: { stubs: { NDataTable: false } } });
    await new Promise((r) => setTimeout(r, 0)); // flush onMounted load
    expect(wrapper.find('[data-test="traffic-table"]').exists() || wrapper.text().includes("terminal")).toBe(true);
  });
});
```

> **实现者注**：i18n / Naive UI provider 在测试里可能需要 stub。参照 `admin/` 下其它 `.spec.ts`（若有）的 mount 配置；若组件因缺 provider 报错，用 `global.stubs` 或 mock `useI18n` 返回 `t: (k) => k`。目标是验证「load 调用 getTrafficStats 并渲染出行 / 非空」这条主路径。

- [ ] **Step 5: Run tests + typecheck**

Run: `cd desktop/frontend && npx vitest run src/components/admin/Traffic.spec.ts && npm run type-check`
Expected: PASS，无类型错误。

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/admin/Traffic.vue \
        desktop/frontend/src/components/admin/Traffic.spec.ts \
        desktop/frontend/src/components/AdminPanel.vue \
        desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "feat(traffic): admin Traffic panel with detail/group/summary views"
```

---

## Task 8: 端到端验证 + 收尾

**Files:** 无新增；跑全套验证。

- [ ] **Step 1: 后端全测 + race**

Run: `go test ./internal/relay/... ./internal/userstore/... -race`
Expected: PASS

- [ ] **Step 2: 全包编译**

Run: `go build ./...`
Expected: 无错误（含 `var _ Store = (*DBStore)(nil)` 编译断言、main.go 的 srv.Close 接线）。

- [ ] **Step 3: 前端 typecheck + 相关测试**

Run: `cd desktop/frontend && npm run type-check && npx vitest run src/components/admin/`
Expected: PASS

- [ ] **Step 4: go vet**

Run: `go vet ./internal/relay/... ./internal/userstore/...`
Expected: 无警告

- [ ] **Step 5: 最终 commit（若前几步有 lint/vet 修正）**

```bash
git add -A
git commit -m "test(traffic): full suite green for per-user traffic stats"
```

---

## Self-Review

**Spec coverage：**
- 埋点层（readFrame + writeFrame）→ Task 4 ✓
- 聚合层 trafficMeter → Task 2 ✓
- 存储层 migration + Store 方法 + 多实例 UPSERT → Task 3 ✓
- 归类映射（明细/分组/汇总的分组维度）→ Task 1（映射）+ Task 5（三 view 聚合）✓
- flush 60s + shutdown flush → Task 4（trafficFlushLoop + Close + main.go）✓
- admin API 三 view + 日期默认 + 非 admin 403 → Task 5 ✓
- 前端 tab + 三视图 + i18n 中英 → Task 6 + Task 7 ✓
- 字节口径 22+len(Payload) → Global Constraints + Task 4 ✓
- flush 失败丢一窗口的取舍 → Task 2 snapshot 注释 + Task 4 flushTraffic warning ✓

**Placeholder scan：** 无 TODO/TBD。Task 5、7 的测试骨架含「实现者注」标明需替换的占位类型名/辅助函数——这是刻意的，因为测试辅助（`newAdminTestServer` 等）需按 repo 现有 `helpers_test.go`/`firstrun_admin_test.go` 先例落地，plan 给了定位线索而非虚构签名。

**Type consistency：** `trafficDelta`（relay 内部，小写）与 `userstore.TrafficDelta`（导出）刻意区分——Task 4 flushTraffic 做转换。`direction` 全栈 int（0/1）。`frameCategory` 返回值与 Task 1 常量一致。`getTrafficStats` / `AdminTrafficResponse` 在 Task 6 定义、Task 7 消费，字段名一致。

**已知需实现者按 repo 现状适配的点（非占位，是接线）：**
1. Task 4 各写循环闭包对 `*Server` 的可见性——逐处按编译错误修正 owner id 来源。
2. Task 5 admin 测试辅助——复用 `firstrun_admin_test.go`/`admin_http_test.go` 建 admin+token 的既有方式。
3. Task 7 前端测试的 i18n/provider stub——参照 admin 下既有 spec。
