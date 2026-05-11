# Wire 协议规范 (v1)

atterm 所有跨进程通信走单一二进制 WebSocket 帧协议。同一份协议被三类连接复用：`/agent`（CLI wrapper）、`/uplink`（桌面 lazy 控制连）、`/client`（attach 接管）。

## 传输

- WebSocket，binary message（**不**用 text）
- 一帧 = 一 WS message。**不要**在一个 message 里塞多帧
- 鉴权：HTTP `Authorization: Bearer <token>`（agent / uplink / 直连 client）；浏览器 client 无法跨源带 header，所以也支持 `?token=<urlencoded>` query 参数和 `Sec-WebSocket-Protocol: atterm-token.<token>`。`cmd/atterm-relay` 未设置 `ATTERM_TOKEN` 时会自动生成强 token 并打印到日志；`internal/relay.Config.Token == ""` 仅供本地/dev 嵌入场景表示不鉴权。浏览器 web client 读取 `?token=` 后会保存到本地存储并清理地址栏。
- CORS：`/api/sessions` 等 REST 端点回 `Access-Control-Allow-Origin: *`；WebSocket Origin 由 `AllowedOrigins` 控制。公网部署建议设置 `--origins https://relay.example.com` 并套 HTTPS/WSS 反向代理。
- 安全头：relay 统一返回 CSP、`Referrer-Policy: no-referrer`、`X-Content-Type-Options: nosniff`、`Permissions-Policy`。`web/` 客户端必须只加载同源静态资源；xterm 资源 vendored 在 `web/vendor/`。

## 帧格式

```
+----+----+----+----+----+----+
| ver| typ|     payload_len   |  6 bytes header
|    |    |    (u32 BE)       |
+----+----+----+----+----+----+
|        session_id           |  16 bytes (UUIDv4 raw)
|        (16 bytes)           |
+-----------------------------+
|         payload             |  payload_len bytes（可为 0）
|         (variable)          |
+-----------------------------+
```

- `ver = 0x01` (常量 `proto.Version`)
- `typ` = 单字节帧类型 enum
- `payload_len` 是 big-endian uint32，**仅** payload 长度，**不**含 header 和 session_id
- `session_id` 是 16 字节 UUIDv4 binary 表示。LIST/LIST_RESP 等无 session 上下文的帧用全 0 作占位
- 接收方实现：`payload_len > 16 MiB` 拒绝，`ver != 1` 拒绝并断开

实现：`internal/proto/codec.go::Marshal` / `Unmarshal`。

## 帧类型枚举

```
const (
    TypeOpen          Type = 0x01  // agent → relay
    TypeIn            Type = 0x02  // client → relay → agent
    TypeOut           Type = 0x03  // agent → relay → client
    TypeResize        Type = 0x04  // client → relay → agent
    TypeMeta          Type = 0x05  // agent → relay → client
    TypeClose         Type = 0x06  // agent → relay → client
    TypeAttach        Type = 0x10  // client → relay
    TypeList          Type = 0x11  // client → relay
    TypeListResp      Type = 0x12  // relay → client
    TypePing          Type = 0x20
    TypePong          Type = 0x21
    TypeAnnounce      Type = 0x30  // uplink → relay  (Phase 1.5)
    TypeStreamRequest Type = 0x31  // relay → uplink
    TypeStreamStop    Type = 0x32  // relay → uplink
    TypePasteImage    Type = 0x33  // client → relay → desktop PTY host
)
```

新增帧类型必须用未占用的字节，且更新本文档。

## 帧 schema

### `OPEN` (0x01) — agent → relay

agent 连接 `/agent` 后的第一帧（uplink 不发 OPEN）。

```json
{
  "cols": 80, "rows": 24,
  "command": "bash -c 'sleep 5'",
  "cwd": "/home/user",
  "title": "bash",
  "host_id": "<uuid>",
  "host": "myhost",
  "user": "alice"
}
```

session_id 由 agent 在帧 header 里给出（同 uplink 重连可复用相同 sid 恢复 session）。

### `IN` (0x02) — 用户键入

payload = 原始 UTF-8 字节（无包裹）。

### `OUT` (0x03) — PTY 输出

payload = 8 字节 `seq` (u64 BE) + 原始字节流。

`seq` 从 1 单调递增，每帧 +1，**不是字节偏移**。重连/续传依赖 seq 而非 byte offset。**不**在 reconnect/reattach 时重置。

### `RESIZE` (0x04) — 窗口尺寸变更

payload = 4 字节：`cols` (u16 BE) | `rows` (u16 BE)。

### `META` (0x05) — 元数据更新

```json
{ "cwd": "/var/log", "title": "log" }
```

字段都 optional，agent 在 cwd / title 变化时发。relay 收到后 `UpdateMeta` 并 broadcast 给所有 subscriber。

### `CLOSE` (0x06) — 会话结束

```json
{ "exit_code": 0, "reason": "process exited" }
```

agent 发出后 relay 移除 session，所有 subscriber 收到 CLOSE 后断开。

### `ATTACH` (0x10) — client 接管 session

```json
{ "session_id": "<uuid string>", "since_seq": 0 }
```

session_id 同时填到帧 header 的 `session_id` 字段（冗余但便于路由）。`since_seq` 0 = 全量 scrollback；非 0 = 只补发 seq > N 的帧。

### `LIST` (0x11) / `LIST_RESP` (0x12)

LIST 空 payload。LIST_RESP payload = `[]SessionInfo` JSON 数组：

```json
[{
  "id": "<uuid>",
  "command": "bash",
  "cwd": "/home/user",
  "title": "bash",
  "cols": 80, "rows": 24,
  "started_at": 1715234567,
  "host_id": "<uuid>",
  "host": "myhost",
  "user": "alice"
}]
```

实践中很少用（前端走 REST `/api/sessions` 更直接）。

### `PING` (0x20) / `PONG` (0x21)

应用层心跳。除此之外 nhooyr WS 库会自动处理控制帧 ping/pong；这两个枚举仅给老客户端兼容。

### `ANNOUNCE` (0x30) — uplink → relay

桌面 app 通过 `/uplink` 控制连发的本机会话快照。**全量**——每次发都覆盖 relay 端 manifest，**不要**用 diff 格式。

```json
{
  "host_id": "<uuid>",
  "host": "myhost",
  "user": "alice",
  "sessions": [SessionInfo, ...]
}
```

发送时机：
- 控制连建立后立即一次
- 30 秒心跳一次
- 本地 session 增/删时事件驱动一次（`relayHost.notifyChange`）

relay 收到后 reconcile：新出现的 session 创建 mirror、消失的 session 移除 mirror。

### `STREAM_REQUEST` (0x31) — relay → uplink

relay 端 mirror session 第一个 subscriber 出现时发，请求桌面 app 开始上传该 session 的字节。

```json
{ "session_id": "<uuid>", "since_seq": 0 }
```

桌面 app 收到后 `SubscribeLocal(sid)` 在本地 mini relay 订阅，把 OUT/META/CLOSE 帧通过 WS 转发。

### `STREAM_STOP` (0x32) — relay → uplink

relay 端 mirror session 最后一个 subscriber 离开时发，请求桌面 app 停止上传。

```json
{ "session_id": "<uuid>" }
```

桌面 app 收到后取消 forwarder goroutine 并 `UnsubscribeLocal`。session 仍在本地活动，仅停止往远程上传字节。

### `PASTE_IMAGE` (0x33) — client → relay → desktop PTY host

远程 web/mobile client 粘贴图片时发送。relay 将其按 session inbound 路径转发给拥有该 PTY 的 desktop host；desktop host 保存图片并尽量模拟本机图片粘贴（设置宿主机系统剪贴板图片后向 PTY 发送 `Ctrl-V`），不支持原生图片剪贴板的平台回退为向 PTY 粘贴临时文件路径。当前原生剪贴板路径：

- macOS: `osascript`
- Linux: 优先 `wl-copy`，再尝试 `xclip` / `xsel`
- Windows: PowerShell STA + `System.Windows.Forms.Clipboard`

payload = JSON：

```json
{
  "filename": "clipboard-image.png",
  "content_type": "image/png",
  "data": "<base64 image bytes>"
}
```

`data` 解码后最大 10 MiB（JSON/base64 后仍需低于协议 16 MiB payload 上限）。`content_type` 必须是 `image/*`。

## HTTP 端点（非帧协议）

| 路径 | 方法 | 用途 |
|------|------|------|
| `/agent` | GET (Upgrade: websocket) | agent 上行 |
| `/uplink` | GET (Upgrade: websocket) | 桌面 app 控制连 |
| `/client` | GET (Upgrade: websocket) | client attach |
| `/client-sessions` | GET (Upgrade: websocket) | session 列表推送 |
| `/api/sessions` | GET | JSON 列表（local + mirror） |
| `/api/version` | GET | JSON 版本信息 |
| `/` (relay 自带) | GET | 静态 web 客户端（仅 `cmd/atterm-relay --web web`） |

CORS：所有路径自动响应 `Access-Control-Allow-Origin: *`，`OPTIONS` 直接 204。非 `OPTIONS`
请求进入 mux 前会经过按远端 IP/token 计算的固定窗口 rate limit；WebSocket upgrade
还会经过同一 key 的活跃连接数限制。

## 鉴权

```
Authorization: Bearer <token>
```

或：

```
GET /client?token=<urlencoded_token>
```

或（仅浏览器 WS subprotocol）：

```
Sec-WebSocket-Protocol: atterm-token.<token>
```

token 由部署方设置（环境变量 `ATTERM_TOKEN`）。`atterm-relay`
启动时若未设置会自动生成高强度 token 并打印到日志；`internal/relay`
作为库使用时 `Config.Token == ""` 仍表示无鉴权（仅用于本地/dev 嵌入场景）。

scope 目前是 relay 内部鉴权结果，不改变 wire frame 格式：

- write token：`ATTERM_TOKEN` / `Config.Token`。可连接 `/agent`、`/uplink`、`/client`、`/client-sessions`，可发送 `IN`、`RESIZE`、`PASTE_IMAGE`。
- read token：`ATTERM_READ_ONLY_TOKENS` / `--read-only-tokens` / `Config.ReadOnlyTokens`。可调用 `/api/sessions`、`/api/version`，可连接 `/client`/`/client-sessions` 并 `LIST`/`ATTACH`/接收输出；relay 会丢弃 `IN`、`RESIZE`、`PASTE_IMAGE`，且拒绝 `/agent`/`/uplink`。
- none：未命中任何 token 时返回 401；`Config.Token == "" && ReadOnlyTokens == nil` 的本地/dev 模式视为 write。

`cmd/atterm-relay` 的启动安全策略：

- 未设置 `ATTERM_TOKEN`：自动生成 32 字节随机 token（base64url）并打印访问 URL。
- 公网监听（例如 `:8080` / `0.0.0.0:8080`）拒绝弱 token（`dev` 或长度 <16），除非显式传 `--dev-insecure`。
- 未设置 `--origins` 时允许启动但打印 warning；浏览器 WS 将接受任意 Origin。生产建议显式设置。
- `--rate-limit-per-minute` / `ATTERM_RATE_LIMIT_PER_MINUTE`：每个远端 IP/token 的 HTTP 请求与 WS upgrade 分钟限额；`0` 用默认值，负数禁用。
- `--max-connections-per-key` / `ATTERM_MAX_CONNECTIONS_PER_KEY`：每个远端 IP/token 的活跃 WS 连接上限；`0` 用默认值，负数禁用。
- `--dev-insecure` 只用于开发/可信内网，会打印明文传输和弱鉴权风险警告。

## 重连与续传

### Agent 短线重连

agent 用同一 `session_id` 重连发 OPEN，relay 识别并复用既有 session（`Registry.Add` 在同 id 存在时替换，旧 session.Close）。**实际上 agent 进程崩溃就会让 PTY 死亡**，session 不会跨 agent 进程恢复。

### Client 短线重连

client 重连后发 `ATTACH(session_id, since_seq=最后收到的 seq)`，relay 从 ringbuf 取 `seq > since_seq` 的帧补发，再切到实时流。

### 间隙补不上

ringbuf 容量 4 MiB（按字节预算丢最老）。client 请求的 `since_seq` 老于 ringbuf 最老 seq 时，relay 发：

```
OUT(seq=0, payload="\x1b[2J\x1b[H")  // ANSI clear screen + cursor home
```

随后从 ringbuf 当前最老 seq 开始补，让 client 重置渲染状态。

### Uplink 断线

`desktop/uplink.go` 维护 exponential backoff（500ms → 8s 上限）。重连后立即发 ANNOUNCE 重建 manifest。已激活的 STREAM_REQUEST 在新连接里需要远程 relay 重新发出（旧 mirror sessions 被关联到旧连接 cleanup 时已 Remove，重连后远程依靠新 ANNOUNCE 重建 mirror，attachers 此时收到 CLOSE，需要自行重新 ATTACH）。

## session 与 host 标识去重

```
session_id（UUIDv4）= 唯一身份。
host_id（机器持久 UUIDv4）= 机器归属，仅用于人类展示与分组。
```

**前端去重原则**：从远程 relay 拉到的 session 列表里，**只过滤 session_id 已在本地列表的项**。

```ts
const localIds = new Set(local.map(s => s.id));
const filteredRemote = remote.filter(s => !localIds.has(s.id));
```

**不要**按 `host_id` 过滤——同机器多桌面 app 实例共享 host_id，但 session_id 不同，按 host_id 过滤会误杀对方 sessions。

## 帧大小与流量

- `payload_len` 限 16 MiB（`maxPayload` 常量），超限 server 拒绝
- relay 端 `agentReadLimit = 17 * 1024 * 1024`（含 header 余量）
- client / uplink 默认 read limit 1-2 MiB
- ringbuf 默认 4 MiB / session（按字节预算）
- 写入限：单 frame 写 timeout 10s（`uplinkWriteTimeout`）

## 协议版本

`Version = 1`（`internal/proto/frame.go`）。

向后兼容规则：
- 新增帧类型 → minor，老 client 收到未知 typ 应当忽略并打日志（`uplink: unexpected frame type 0x%02x`），**不要**断开
- 改既有帧 payload 结构 → major（bump Version 到 2），需要协议协商机制（暂未实现）
- 添加 OpenPayload / SessionInfo 字段 → minor，json 反序列化忽略未知字段

## 实现指针

- 帧编解码：`internal/proto/codec.go`
- 帧类型常量：`internal/proto/frame.go:13-30`
- relay 端处理 `/agent`：`internal/relay/agent_conn.go`
- relay 端处理 `/uplink`：`internal/relay/uplink_conn.go`
- relay 端处理 `/client`：`internal/relay/client_conn.go`
- 桌面 uplink 客户端：`desktop/uplink.go`
- 浏览器协议层：`web/app.js` / `desktop/frontend/src/lib/proto.ts`（保持两份同步！TS 一份是手工移植）
