# 用户网络流量统计监控 — 设计

- 日期：2026-09-15
- 修订：2026-09-23（增加个人看板与 P2P 流量）
- 状态：已批准（brainstorming 分节确认 + 关键决策拍板）
- 范围：Relay/P2P 埋点 + 聚合 + 日汇总落库 + admin API + 当前账号个人看板

## 目标

在 admin 页面新增「用户网络流量统计监控」，把每个用户经 relay 的收发字节数按帧类型
统计，并支持按「明细（帧类型）/ 分组（语义类别）/ 汇总」三个维度查看。语义类别包括
终端转发、状态同步、配置同步、文件浏览、远程预览。

## 已定决策（brainstorming 拍板）

| 决策点 | 结论 |
|--------|------|
| 保留与粒度 | 每日汇总落库，保留历史，支持按日查看 |
| 落库最细粒度 | 帧类型级：`(user_id, day, frame_type, direction)`；分组/汇总查询时聚合 |
| 分类维度 | 明细 / 分组 / 汇总 三视图；分组 = 帧类型 → 语义类别的静态映射 |
| 统计范围 | uplink（桌面 host）+ client（浏览器 viewer）都计，按方向 in/out 分开 |
| flush 策略 | 定时 flush（60s）+ graceful shutdown 再 flush 一次 |
| 多实例 | 共享表，各实例对同一行 UPSERT 累加（bytes += delta） |
| 埋点方式 | 新增独立计量钩子，入站在 readFrame、出站包 writeFrame helper；不复用 debugFrame |
| 可见范围 | admin 可看全局 Relay 明细；普通用户只能看自己的 Relay/P2P 日汇总 |
| P2P 口径 | host 视角的 terminal frame wire bytes，发送/接收分开；不含 ICE/SDP/DataChannel 开销 |
| 隐私边界 | 个人 API 不返回 user id/email；P2P 统计不含 session id、candidate、token 或 terminal 内容 |

## 架构总览

```
[埋点层] readFrame(入站) + 新 writeFrame helper(出站)
    │  按 (userID, frameType, direction) 记 wireBytes / frameCount
    ▼
[聚合层] Server.trafficMeter (内存 map + mutex)
    │  每 60s snapshot(读走增量并清零) + shutdown flush
    ▼
[存储层] userstore: relay_traffic_daily 表
    │  AddTrafficDeltas → UPSERT bytes += delta (多实例共享行累加)
    ▼
[API 层] GET /admin/api/traffic?from=&to=&view=detail|group|summary
    │  按 view 决定 GROUP BY;分组用静态 frameCategory 映射
    ▼
[前端层] AdminPanel "traffic" tab → admin/Traffic.vue
```

P2P 数据旁路：已认证 host 按阈值或连接关闭发送 `direct_stats` 聚合增量，Relay 按
认证上下文中的 user id 累加到 `directMetrics`，随同一 60s flush 写入
`direct_traffic_daily`。该上报由账号自己的 host 产生，可用于个人可观测性和平台容量趋势，
但不能作为计费或配额的可信依据。

数据流关键点：
- 埋点只在**真正过网**时计数：出站在 `c.Write` 返回 nil 后计，入站在 `readFrame` 成功
  返回后计。
- `userID` 从连接建立时已知的 `ownerUserID` 直接拿（uplink `handleUplinkHTTP` /
  client `handleClient`），无需查库。
- 字节口径：`wireBytes = 22 + len(f.Payload)`（含 6B 头 + 16B session UUID，**不含**
  WebSocket 帧头 2–14B）。这是已知口径，spec 与 UI 都以此为准。
- 语义类别**不落库**，只落帧类型明细，分组/汇总在查询层用 `frameCategory` 映射算出。
- admin 看到的是「已 flush 到库」的数据，相对实时最多滞后 60s（flush 间隔）。

## 组件设计

### 1. 埋点层（`internal/relay/`）

- 新增 `internal/relay/traffic_meter.go`：
  - `type trafficDir uint8`（`trafficIn=0` / `trafficOut=1`）
  - `func (s *Server) recordTraffic(userID string, t proto.Type, dir trafficDir, wireBytes int)`
    —— 空 userID 直接 return（握手前 / 匿名帧不计；见 Open Questions 已解）。
- 入站：uplink / client / agent 三条连接的读循环在 `readFrame` 成功返回后调
  `recordTraffic(ownerUserID, f.Type, trafficIn, 22+len(f.Payload))`。
  - `agent_conn.go` 直连 PTY 路径也带 `ownerUserID`（若可得；否则该连接不计，spec 记为可接受）。
- 出站：新增 helper
  `func writeFrame(ctx, component, userID string, c *websocket.Conn, f proto.Frame, meter recorder) error`
  收敛 9 处内联 `c.Write(ctx, websocket.MessageBinary, proto.Marshal(f))`，在 Write 返回
  nil 后 `recordTraffic(userID, f.Type, trafficOut, len(marshaled))`。
  - 收敛点清单（来自调研）：`uplink_conn.go:101,466`、`agent_conn.go:99`、
    `client_conn.go:123,154,218,500`、`client_sessions_conn.go:70,94`。
  - 每个写点的 `userID` 由该连接的 `ownerUserID` 提供（client-sessions 连接同样有已认证 user）。

### 2. 聚合层（`Server.trafficMeter`）

- 挂到 `Server` struct 新字段 `trafficMeter *trafficMeter`。
- 结构：`map[trafficKey]*trafficCell`，`trafficKey{userID, frameType, dir}`，
  cell 持 `bytes int64` + `frames int64`。`sync.Mutex` 保护（flush 低频，简单锁足够，
  不做无锁优化 — YAGNI）。
- `add(userID, t, dir, bytes)`：加锁 upsert cell 累加。
- `snapshot() []TrafficDelta`：加锁读出全部 cell 并**清空 map**（读走增量，避免下次重复累加）。
- 生命周期：`server.go` 启动一个 60s ticker goroutine，每 tick `snapshot()` →
  `Store.AddTrafficDeltas`；`Server.Shutdown/Close` 路径再 flush 一次。

### 3. 存储层（`internal/userstore/`）

新 migration `0012_relay_traffic.sql`（sqlite + postgres 各一份）：

```sql
CREATE TABLE relay_traffic_daily (
  user_id    TEXT    NOT NULL,
  day        TEXT    NOT NULL,          -- 'YYYY-MM-DD' UTC
  frame_type INTEGER NOT NULL,          -- proto.Type 数值
  direction  INTEGER NOT NULL,          -- 0=in, 1=out
  bytes      BIGINT  NOT NULL DEFAULT 0,
  frames     BIGINT  NOT NULL DEFAULT 0,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (user_id, day, frame_type, direction)
);
CREATE INDEX idx_relay_traffic_day ON relay_traffic_daily (day);
```

Store 接口（`store.go`）新增，所有实现同步（sqlite/postgres），并更新 `store_iface_test.go`：

```go
type TrafficDelta struct {
    UserID    string
    FrameType int
    Direction int
    Bytes     int64
    Frames    int64
}
type TrafficRow struct {
    UserID    string
    Day       string
    FrameType int
    Direction int
    Bytes     int64
    Frames    int64
}
AddTrafficDeltas(ctx context.Context, day string, deltas []TrafficDelta) error
QueryTraffic(ctx context.Context, from, to string) ([]TrafficRow, error)
```

- `AddTrafficDeltas` 用 `INSERT ... ON CONFLICT (user_id,day,frame_type,direction)
  DO UPDATE SET bytes = bytes + excluded.bytes, frames = frames + excluded.frames,
  updated_at = ...`（sqlite / postgres 方言差异走现有 `dialect.go` 模式）。
  多实例共享行累加天然汇总。
- `day` 由聚合层传当天 UTC（flush 时刻决定），避免跨零点误归。

### 4. API 层（`internal/relay/admin_traffic.go` 新文件）

- `GET /admin/api/traffic?from=YYYY-MM-DD&to=YYYY-MM-DD&view=detail|group|summary`
- 挂在 `AdminServer.RegisterInto`（仿 `admin_users.go`），父级已包 `requireSession` +
  `is_admin`。
- `from`/`to` 缺省：`to`=今天，`from`=今天-6（近 7 天）。校验 RFC date 格式。
- 后端拉 `QueryTraffic(from,to)` 后在内存里按 view 聚合：
  - `detail`：user × frame_type × direction（frame_type 附带 `frameTypeName`）
  - `group`：user × category × direction（`frameCategory(proto.Type)` 折叠）
  - `summary`：user × direction
- email 由 `Store.ListUsers` 建 id→email 映射补齐（仿 `handleListUsers`）。
- 响应：
  ```json
  {
    "view": "group",
    "from": "...", "to": "...",
    "rows": [{"user_id":"","email":"","day":"","frame_type":0,"frame_type_name":"",
              "category":"","direction":0,"bytes":0,"frames":0}]
  }
  ```
  （字段按 view 取舍：detail 有 frame_type/name，group 有 category，summary 都无。）

### 5. 归类映射（`internal/relay/traffic_category.go`）

单一事实源，`func frameCategory(t proto.Type) string`：

| 类别 | 帧类型 |
|------|--------|
| `terminal`（终端转发） | IN, OUT, RESIZE, PASTE_IMAGE, PASTE_FILE |
| `state`（状态同步） | META, LIST, LIST_RESP, REPLAY_PROGRESS, ANNOUNCE, VIEWERS, OPEN, CLOSE, STREAM_REQUEST, STREAM_STOP, COMMAND_EVENT, SESSION_CREATE, SESSION_CREATED, PING, PONG, ATTACH, CLAIM_DRIVER |
| `config`（配置同步） | PREFS_CHANGED, AUTH_INFO |
| `fs`（文件浏览） | FS_REQUEST, FS_RESPONSE, FS_EVENT |
| `preview`（远程预览） | SERVICE_OPEN, SERVICE_OPENED, SERVICE_CLOSE |

未知 / 未来新增帧类型：归 `other`。穷举测试保证所有当前 `proto.Type` 都有显式归类。

### 6. 前端层（`desktop/frontend/src/components/admin/`）

- `AdminPanel.vue`：`AdminTabKey` 加 `"traffic"`；tabs 数组 + body `v-if` 各加一行；
  import `admin/Traffic.vue`。
- 新 `admin/Traffic.vue`：
  - 日期范围（from/to）+ 视图切换（明细/分组/汇总）
  - 表格：用户、（帧类型/类别）、方向、字节（人类可读）、帧数
  - 分组视图可加轻量条形（复用现有 Naive UI 组件，不引图表库 — YAGNI）
- `web/src/shared/api/admin.ts` 加 `getTrafficStats(params)` 走 `apiFetch`；类型加到
  `web/src/shared/api/types.ts`（`AdminTrafficRow` / `AdminTrafficResponse`）。
- i18n：`desktop/frontend/src/i18n/messages/{en,zh-CN}.ts` 加 `admin.trafficTab` 及表头
  label（中英两套）。

### 7. 个人 Relay/P2P 看板（2026-09-23 修订）

- `GET /api/me/traffic?from=&to=` 由 `requireSession` 保护，user id 只取请求上下文；日期
  最多 90 天。响应不带 user id/email，Relay 按日折叠为 in/out，P2P 按日返回
  attempts/successes/fallbacks/bytes_sent/bytes_received。
- `direct_traffic_daily` 主键为 `(user_id, day)`，使用 additive UPSERT，支持多 Relay
  实例共享 Postgres 后自然汇总。
- host 的 `direct_stats` 同时携带 `bytes_sent` / `bytes_received`；`bytes_avoided` 仅为旧
  Relay 兼容字段。每个非零方向的单条增量最大 64 MiB，只有 host role 可以上报。
- Web/Capacitor 的 `SettingsAccount.vue` 与桌面端 `SettingsRelay.vue` 都只在认证成功后挂载
  同一个紧凑看板，支持 7/30/90 天范围，展示 Relay、P2P 总量、直连成功率和每日双序列
  趋势。未登录时不发起统计请求。

## 错误处理

- 埋点：`recordTraffic` 永不返回 error、永不 panic、永不阻塞收发路径（加锁累加是唯一开销）。
  userID 为空静默跳过。
- flush：`AddTrafficDeltas` 失败记 warning 日志，**保留本次 snapshot 未写的增量**？——
  不。snapshot 已清零，失败即丢该窗口（最多 60s）。这是刻意取舍（简单 > 完美），spec 明示。
  若后续要求不丢，可改为失败时把 delta 合并回 meter，留作未来增强。
- API：日期格式非法 → 400；Store 查询失败 → 500；非 admin → 403（父中间件）。

## 测试策略

- 埋点：`recordTraffic` 累加正确、方向区分、空 userID 跳过；`writeFrame` 只在 Write 成功后计。
- 聚合：`snapshot()` 读走后清零、并发 add 安全（`-race`）。
- 存储：`AddTrafficDeltas` UPSERT 累加（同键多次相加）、`QueryTraffic` 日期范围过滤 ——
  sqlite + postgres 双跑（仿现有 store 测试）。
- API：三 view 结构、日期默认值、日期非法 400、非 admin 403。
- 归类：`frameCategory` 对所有已定义 `proto.Type` 有显式归类（穷举测试）。

## 明确不做（out of scope / YAGNI）

- 分钟级时间序列、实时流量曲线
- 限流 / 配额 / 告警
- 将 P2P 客户端上报用于计费或安全审计
- 图表库依赖
- 历史数据保留期自动清理（可后续加 cron）

## 涉及文件清单

新增：
- `internal/relay/traffic_meter.go`
- `internal/relay/traffic_category.go`
- `internal/relay/admin_traffic.go`
- `internal/userstore/migrations/sqlite/0012_relay_traffic.sql`
- `internal/userstore/migrations/postgres/0012_relay_traffic.sql`
- `internal/userstore/traffic.go`（Store 方法实现，或并入现有文件）
- `desktop/frontend/src/components/admin/Traffic.vue`
- `internal/relay/me_traffic.go`
- `internal/userstore/migrations/{sqlite,postgres}/0013_direct_traffic.sql`
- `desktop/frontend/src/components/AccountTrafficDashboard.vue`

修改：
- `internal/relay/server.go`（trafficMeter 字段 + ticker + shutdown flush + readFrame 计量 + writeFrame 收敛）
- `internal/relay/uplink_conn.go` / `agent_conn.go` / `client_conn.go` / `client_sessions_conn.go`（换 writeFrame + 读循环计量）
- `internal/userstore/store.go`（接口 + TrafficDelta/TrafficRow 类型）
- `internal/userstore/store_iface_test.go`（同步）
- `desktop/frontend/src/components/AdminPanel.vue`
- `web/src/shared/api/admin.ts` + `web/src/shared/api/types.ts`
- `desktop/frontend/src/i18n/messages/en.ts` + `zh-CN.ts`
