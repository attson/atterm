# Wire 协议规范 (v1)

atterm 所有跨进程通信走单一二进制 WebSocket 帧协议。同一份协议被三类连接复用：`/agent`（CLI wrapper）、`/uplink`（桌面 lazy 控制连）、`/client`（attach 接管）。

## 传输

- WebSocket，binary message（**不**用 text）
- 一帧 = 一 WS message。**不要**在一个 message 里塞多帧
- 鉴权：三类 Principal（详见 §鉴权）：浏览器走 `atterm_session` HTTP-only cookie（mutating endpoint 额外要 `X-CSRF-Token`）；桌面 / CLI / 移动 web 走 `Authorization: Bearer atk_…`，WebSocket 升级也支持 `Sec-WebSocket-Protocol: atterm-token.atk_…` / `atterm-token-b64.<base64url(utf8 token)>` 以避免 token 进入 URL 日志；admin 是 `is_admin=true` 的用户 principal（浏览器 admin UI 用 cookie，自动化可用 admin 用户 API token；仅在 `/admin/*` 有效）。服务端所有鉴权接口都不接受 `?token=<urlencoded>` query token。`internal/relay.Config.Resolver == nil` 是本地 / dev 嵌入场景的不鉴权降级。
- CORS：`/api/sessions` 等 REST 端点回 `Access-Control-Allow-Origin: *`；WebSocket Origin 由 `AllowedOrigins` 控制。公网部署必须设置 `--origins https://relay.example.com` / `ATTERM_ORIGINS` 并套 HTTPS/WSS 反向代理，除非显式 `--dev-insecure`。`--origins` 可写完整 URL 或 host pattern；relay 会按 WebSocket 库要求规范成 Origin host pattern，并在启用白名单时自动允许 Wails 桌面客户端的本地 asset hosts。
- 安全头：relay 统一返回 CSP、`Referrer-Policy: no-referrer`、`X-Content-Type-Options: nosniff`、`Permissions-Policy`。`web/` 客户端应用代码必须只加载同源静态资源；Vue/xterm/Naive UI 等 npm 依赖由 Vite 打包成同源 assets，PWA service worker 只预缓存这些静态产物。CSP 允许 inline style 仅用于 xterm.js 运行时布局样式；script-src 允许同源脚本，并预留 Cloudflare Web Analytics beacon 源，但不允许 unsafe eval/inline script 或应用代码引入 CDN 依赖。

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
    TypeReplayProgress Type = 0x13 // relay → client
    TypePing          Type = 0x20
    TypePong          Type = 0x21
    TypeAnnounce      Type = 0x30  // uplink → relay  (Phase 1.5)
    TypeStreamRequest Type = 0x31  // relay → uplink
    TypeStreamStop    Type = 0x32  // relay → uplink
    TypePasteImage      Type = 0x33  // client → relay → desktop PTY host
    TypeClaimDriver     Type = 0x34  // client → relay (viewer claims driver role)
    TypeCommandEvent    Type = 0x35  // uplink → relay (Web Push notification trigger)
    TypeViewers         Type = 0x36  // relay → uplink (mirror remote subscriber count)

    // Auth frames (server → client).
    TypeAuthInfo        Type = 0x40  // relay → uplink; UTF-8 JSON {user_id}
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
{
  "cwd": "/var/log",
  "title": "log",
  "driver_client_id": "<uuid>",
  "driver_client_name": "Alice's MacBook",
  "cols": 132,
  "rows": 39,
  "task_state": "running",
  "current_command": "go test ./...",
  "command_started_at": 1715234567,
  "command_ended_at": 0,
  "command_duration_ms": 0,
  "last_output_at": 1715234568
}
```

字段都 optional。agent 在 cwd / title 变化时发；relay 在 driver 变化或 PTY 尺寸（`UpdateSize`）变化时也会自行 broadcast 一帧。subscriber 收到后：

- `driver_client_id` 与本地生成的 `ATTACH.client_id` 比对，决定自己是 driver 还是 viewer
- `driver_client_name` 是 driver 当前 attach 时报上来的 `ATTACH.client_name`（典型是其 hostname），viewer 端用它在遮罩里显示 "by &lt;hostname&gt;"
- `cols` / `rows` 是 PTY 当前真实尺寸；viewer 把自己的 xterm `term.resize(cols, rows)` 锁到这个值（不跑 FitAddon）
- `task_state` 是任务状态：`idle` / `running` / `waiting_input` / `completed` / `failed` / `disconnected` / `closed`
- `current_command` / `command_started_at` / `command_ended_at` / `command_duration_ms` / `command_exit_code` 来自 OSC 133 命令生命周期；`command_exit_code` 只在命令结束后出现，`0` 表示 completed，非 0 表示 failed
- `last_output_at` 是 relay 最近看到该 session OUT 字节的 unix 秒时间戳

每个新 subscriber 在 `ATTACH` 后会立即收到一帧 snapshot META，包含当前 driver_client_id / driver_client_name / cols / rows / task metadata，作为初始状态。

### `CLOSE` (0x06) — 会话结束

```json
{ "exit_code": 0, "reason": "process exited" }
```

agent 发出后 relay 移除 session，所有 subscriber 收到 CLOSE 后断开。

### `ATTACH` (0x10) — client 接管 session

```json
{
  "session_id": "<uuid string>",
  "since_seq": 0,
  "client_id": "<uuid>",
  "client_name": "Alice's MacBook"
}
```

session_id 同时填到帧 header 的 `session_id` 字段（冗余但便于路由）。`since_seq` 0 = 全量 scrollback；非 0 = 只补发 seq > N 的帧。

`client_id` 是 client 在创建 `SessionConnection` 时自己生成的 UUID，每个 connection 实例一个。relay 把它存在对应 `Subscriber` 上，并在 `META.driver_client_id` 里回放，让 client 通过本地 ID 比对识别自己是不是当前 driver。该字段可选——旧版 client 不发 client_id 时 relay 仍接受订阅，只是 client 永远不会渲染成 driver（始终 viewer 视觉）；服务端 driver 指针仍正确指向该 sub，IN/RESIZE 仍可通过，UI 行为退化为静默 driver。

`client_name` 是人类可读的客户端标识（典型来自 `getHostInfo().host` 也就是本机 hostname）。relay 把它存在 `Subscriber` 上，并在 `META.driver_client_name` 里回放，让 viewer 在遮罩里显示 "by &lt;name&gt;"。该字段可选。

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
  "user": "alice",
  "remote_permission": "full",
  "task_state": "completed",
  "current_command": "go test ./...",
  "command_started_at": 1715234567,
  "command_ended_at": 1715234579,
  "command_duration_ms": 12500,
  "command_exit_code": 0,
  "last_output_at": 1715234579
}]
```

`remote_permission` 是 owner desktop 发布的可选字段；缺省表示 `full`，保持旧客户端兼容。

任务字段均为可选 additive metadata。缺失 `task_state` 的旧 publisher 按 `idle` 处理。

| 值 | 远程允许 | relay/host 拦截 |
|----|----------|-----------------|
| `view` | list / attach / 接收输出与历史 | `IN` / `RESIZE` / `PASTE_IMAGE` |
| `control` | `view` + `IN` + `RESIZE` | `PASTE_IMAGE` |
| `full` 或空 | `control` + `PASTE_IMAGE` | 无 |

实践中很少用（前端走 REST `/api/sessions` 更直接）。

### `REPLAY_PROGRESS` (0x13) — attach 历史回放进度

relay 在 client `ATTACH` 后、初始 scrollback 回放期间发送。payload = JSON：

```json
{ "phase": "start", "bytes": 0, "total_bytes": 4194304, "seq": 0 }
{ "phase": "chunk", "bytes": 1048576, "total_bytes": 4194304, "seq": 123 }
{ "phase": "end", "bytes": 4194304, "total_bytes": 4194304, "seq": 456 }
```

- `phase=start`：回放开始，client 可显示 loading history。
- `phase=chunk`：回放中间进度；`bytes` 是已回放 PTY 字节数，`total_bytes` 是本次 ATTACH 需要回放的 PTY 字节数。
- `phase=end`：回放结束，此后 subscriber 已切到实时流。

`REPLAY_PROGRESS` 不改变 `OUT.seq` 语义；老 client 收到未知帧应忽略。relay `/client` writer 在 replay 期间会按字节批次短暂停顿，让浏览器能绘制进度条，避免大历史会话看起来卡在 connecting。

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

### `CLAIM_DRIVER` (0x34) — client → relay

viewer 想接管成为 driver 时发。payload = JSON：

```json
{ "client_id": "<uuid>", "client_name": "Alice's MacBook" }
```

`client_id` 应与发送方 `ATTACH.client_id` 相同（end-to-end 标识）；`client_name` 同 `ATTACH.client_name`。relay 把这两个字段原样写进新一帧 META 的 `driver_client_id` / `driver_client_name` 广播给所有 subscriber。无需当前 driver 确认——立即生效。

relay 拒绝以下情形（debug log 但不发错误帧给 client）：
- 未 attach
- 读权限 token（`authRead` scope）
- session 的 `remote_permission == view`
- payload 不是合法 JSON

桌面 app 的 uplink 收到 CLAIM_DRIVER 后调 `relayHost.ClaimLocalDriver`，把本地 mini relay 上的 uplink subscriber 提升为 driver（同时把 end-to-end `client_id` 透传）；多跳时 driver 状态由最远端 client 的 ID 决定。

### `COMMAND_EVENT` (0x35) — uplink → relay only (Web Push notification trigger)

Direction: uplink → relay only. Not forwarded to clients.

Payload (JSON):

```json
{
  "exit_code": 0,
  "elapsed_ms": 12500,
  "label": "atterm"
}
```

- `session_id` rides the frame header (existing pattern).
- `host_id` is intentionally not in the payload. The relay reconstructs it from the sender's ANNOUNCE manifest at handler time, which makes cross-uplink spoofing impossible.
- The relay drops the frame silently when `session_id` is not present in the sender's current manifest.
- `label` is truncated to 256 bytes before being forwarded into a notification payload.

### `VIEWERS` (0x36) — relay → uplink only (remote viewer count)

Direction: relay → uplink only. Not forwarded to clients.

Payload (JSON):

```json
{
  "session_id": "…",
  "count": 2
}
```

- Reports the number of remote `/client` subscribers currently attached to the session's **mirror** on the relay (web / mobile / other desktops). The driver is included — it is still a connected remote.
- Sent on every attach/detach (the mirror `Session`'s subscriber-count hook), so the count is exact and live. Emission is synchronous-and-ordered on the relay side; the desktop must treat the latest frame as authoritative.
- The desktop uplink surfaces it as a `relay:viewers` Wails event; the UI shows a per-session "👁 N" badge (owner-side awareness only).
- The owner's own desktop attaches to its local mini-relay, not the central mirror, so it is never counted.

### `AUTH_INFO` (0x40) — relay → uplink only

Direction: relay → uplink only. Not sent on `/client` or `/agent` connections.

Emitted immediately after successful auth on `/uplink`, **before** the relay reads the first `ANNOUNCE` frame from the client. Only sent when the uplink authenticated as a `PrincipalUser` (i.e. via a per-user API token). The dev / loopback path with no resolver does not emit this frame.

`internal/proto.Version` remains 1 — this is a new frame type only, no change to existing frame semantics.

Payload (UTF-8 JSON):

```json
{
  "user_id": "01HXABCDEF"
}
```

- `user_id` is the ULID of the authenticated user.
- Unknown JSON keys MUST be ignored by clients (forward-compat).
- The desktop fetches the user's email separately via `/api/me` (see Task 8.1).

## Driver / Viewer 模型

每个 session 在任意时刻最多有一个 driver subscriber。driver 是唯一允许把 `IN` / `RESIZE` / `PASTE_IMAGE` 转发到 PTY 的连接；其它都是 viewer（只收 `OUT` / `META` / `CLOSE` / `REPLAY_PROGRESS`）。

- **自动晋升**：第一个 `Subscribe` 上来的 subscriber（不论 loopback 还是 uplink）自动 driver。
- **接管**：viewer 端按空格 → `CLAIM_DRIVER` → relay 切 driver → META 广播。
- **解绑**：driver subscriber 断开（disconnect / 慢消费被踢 / session 关闭）时 driver 字段清空、`driver_client_id` 广播为 `""`；之后第一个 claim 的 viewer 胜出。
- **多跳**：公网 relay 当前不在自己一层做仲裁，把所有 subscriber 当作集合代理到 uplink；多个 mobile/web 同时连同一 session 时它们共享"远端 driver"位（cooperative 客户端 v1，后续可在公网 relay 加 driver 状态机做仲裁）。
- **权限交互**：`remote_permission` 是 session 策略，driver 是运行时角色，两者正交。`view` 权限的 subscriber 永远不能 claim；`control` / `full` 可以。
- **viewer 视觉**：xterm.js 不跑 FitAddon，`term.resize(meta.cols, meta.rows)` 锁到 PTY 尺寸；`disableStdin=true` 阻止 IN 转发；右下角 badge "viewer · press space to take over"。

新 attach 上来：relay 在 `REPLAY_PROGRESS end` 之后立即发一帧 snapshot `META`，让 client 拿到当前 driver_client_id 和 PTY cols/rows。

## HTTP 端点（非帧协议）

| 路径 | 方法 | 用途 |
|------|------|------|
| `/agent` | GET (Upgrade: websocket) | agent 上行 |
| `/uplink` | GET (Upgrade: websocket) | 桌面 app 控制连 |
| `/client` | GET (Upgrade: websocket) | client attach |
| `/client-sessions` | GET (Upgrade: websocket) | session 列表推送 |
| `/api/sessions` | GET | JSON 列表（local + mirror） |
| `/api/version` | GET | JSON 版本信息 |
| `/`, `/login.html`, `/signup.html`, `/settings.html`, `/setup.html`, `/admin/` | GET | 静态 Vue MPA web/PWA 客户端（默认使用 embedded `internal/relay/web-dist/`；开发可用 `--web web/dist`） |

CORS：所有路径自动响应 `Access-Control-Allow-Origin: *`，`OPTIONS` 直接 204。非 `OPTIONS`
请求进入 mux 前会经过按远端 IP/token 计算的固定窗口 rate limit；WebSocket upgrade
还会经过同一 key 的活跃连接数限制。WebSocket Origin 白名单匹配的是 Origin
host（例如 `relay.example.com`、`*.example.com`）；`cmd/atterm-relay` 接受完整 URL
输入并规范成 host，同时追加 Wails 桌面客户端需要的 `wails` / `wails.localhost`
host pattern，这样桌面客户端和同源 web 客户端都能连接。

## 鉴权

### 用户账号模式（生产推荐）

`cmd/atterm-relay` 以用户账号模式启动时（默认），所有端点通过 `IdentityResolver`
鉴权，支持以下三种凭证来源：

**Browser（Web 客户端）**：HTTP-only cookie `atterm_session`，由
`POST /api/auth/login` 或 `POST /api/auth/signup` 签发。变更状态的端点
（logout、token 创建/撤销、push 订阅等）额外需要 `X-CSRF-Token` 请求头；
token 由 `GET /api/me` 的 `csrf_token` 字段返回。

**Desktop/CLI（API token）**：

```
Authorization: Bearer atk_…
```

或（仅 WebSocket 升级，避免 token 进入 URL 日志）：

```
Sec-WebSocket-Protocol: atterm-token.atk_…
Sec-WebSocket-Protocol: atterm-token-b64.<base64url(utf8 token)>
```

API token 由 `POST /api/me/tokens`（CSRF-gated）创建，前缀固定为 `atk_`。
可连接 `/agent`、`/uplink`、`/client`、`/client-sessions`，可发送
`IN`、`RESIZE`、`PASTE_IMAGE`。不携带 CSRF secret（无 cookie），故不能
调用需要 CSRF 的端点（改用 cookie session 登录后操作）。

**Admin（Administrator）**：

```
Cookie: atterm_session (with user.is_admin=true)
```

或 admin 用户的 API token（`Authorization: Bearer atk_…`）。仅在 `/admin/*`
路径有效（由 `bootstrapAdmin` 在启动时配置，通过环境变量
`ATTERM_BOOTSTRAP_ADMIN_EMAIL` 和 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` 初始化）。
浏览器 admin UI 使用 cookie；需要 CSRF 的 mutating endpoint 只接受 cookie + CSRF。
用户账号管理端点（`/admin/api/invitations`、`/admin/api/users`）使用此 principal。
admin 用户也可通过 `POST /admin/api/users/{id}/admin` 晋升其他用户。

### Principal 类型

| Principal | 来源 | 可用路径 |
|---|---|---|
| `PrincipalUser` | cookie session 或 API token | `/agent` `/uplink` `/client` `/client-sessions` `/api/*` |
| `PrincipalAdmin` | `PrincipalUser` with `is_admin=true` | `/admin/*` |
| `PrincipalNone` | 无效/过期凭证 | — (401) |

每个用户只能看到自己注册的 session（`/api/sessions`、WebSocket LIST 帧、
`/client` ATTACH 均按 ownerUserID 过滤）。

### 用户账号 HTTP 端点

| 路径 | 方法 | 鉴权 | 说明 |
|------|------|------|------|
| `/api/auth/signup` | POST | 公开（需邀请码） | 注册用户，签发 cookie session |
| `/api/auth/login` | POST | 公开 | 登录，签发 cookie session |
| `/api/auth/logout` | POST | Cookie + CSRF | 注销当前 session |
| `/api/me` | GET | Cookie 或 API token | 返回用户信息和 csrf_token |
| `/api/me/tokens` | GET/POST | Cookie (POST 需 CSRF) | 列出/创建 API token |
| `/api/me/tokens/{id}` | DELETE | Cookie + CSRF | 撤销 API token |
| `/api/me/password` | POST | Cookie + CSRF | 修改密码（清除所有 session） |
| `/api/push/key` | GET | 任意已认证 Principal | 获取 VAPID 公钥 |
| `/api/push/subscribe` | POST | Cookie + CSRF | 注册 push 订阅 |
| `/api/push/unsubscribe` | POST | Cookie + CSRF | 取消 push 订阅 |
| `/api/push/test` | POST | Cookie + CSRF | 发送测试 push 通知 |
| `/api/me/webhooks` | GET/POST | Cookie (POST 需 CSRF) | 列出/创建 outbound webhook |
| `/api/me/webhooks/{id}` | DELETE | Cookie + CSRF | 删除 outbound webhook |
| `/admin/api/invitations` | GET/POST | Admin principal | 列出/创建邀请码 |
| `/admin/api/users` | GET | Admin principal | 列出用户 |
| `/admin/api/users/{id}/reset-password` | POST | Admin principal | 重置用户密码 |
| `/admin/api/users/{id}/disable` | POST | Admin principal | 禁用用户 |
| `/admin/api/users/{id}/admin` | POST/DELETE | Admin cookie + CSRF | 晋升 / 取消 admin |
| `/admin/api/config` | GET/PUT | Admin principal（PUT 需 CSRF） | 查看 / 更新 relay 运行限流配置 |

### 启动安全策略

- 公网监听时必须设置 `ATTERM_BOOTSTRAP_ADMIN_EMAIL`；如果该用户不存在并需要 bootstrap 创建，`ATTERM_BOOTSTRAP_ADMIN_PASSWORD` 必须非空且足够强（长度 ≥ 16，至少 3 类字符，且不在弱密码黑名单内），除非显式 `--dev-insecure`。
- 公网监听未设置 `--origins` / `ATTERM_ORIGINS` 时拒绝启动，除非显式 `--dev-insecure`。
- `--rate-limit-per-minute` / `ATTERM_RATE_LIMIT_PER_MINUTE`：HTTP 请求与 WS upgrade 先按远端 IP 限流；鉴权成功后再按远端 IP + token hash 限流。`0` 用默认值，负数禁用。
- `--max-connections-per-key` / `ATTERM_MAX_CONNECTIONS_PER_KEY`：每个远端 IP/token 的活跃 WS 连接上限；`0` 用默认值，负数禁用。
- `--config` / `ATTERM_RELAY_CONFIG`：持久化 relay admin JSON 配置路径，仅保存运行参数；用户账号和 session 保存在 SQLite（users.db）。
- `ATTERM_BOOTSTRAP_ADMIN_EMAIL` / `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`：初始化 admin 用户。bootstrap 每次启动都会确保该 email 是 admin；用户已存在时只提权并忽略 password env。现有用户无法通过重启改变密码，需改用 web UI `/settings.html`。
- `--dev-insecure` 只用于开发/可信内网，会打印明文传输和弱鉴权风险警告。

持久化 admin config 示例（仅保存运行参数，不保存 token 明文）：

```json
{
  "rate_limit_per_minute": 600,
  "max_connections_per_key": 64
}
```

## 重连与续传

### Agent 短线重连

agent 用同一 `session_id` 重连发 OPEN，relay 识别并复用既有 session（`Registry.Add` 在同 id 存在时替换，旧 session.Close）。**实际上 agent 进程崩溃就会让 PTY 死亡**，session 不会跨 agent 进程恢复。

### Client 短线重连

client 重连后发 `ATTACH(session_id, since_seq=最后收到的 seq)`，relay 从 ringbuf 取 `seq > since_seq` 的帧补发，并用 `REPLAY_PROGRESS` 标记补发进度，再切到实时流。

### 间隙补不上

ringbuf 容量 4 MiB（按字节预算丢最老）。client 请求的 `since_seq` 老于 ringbuf 最老 seq 时，或首次 attach（`since_seq=0`）但会话开头已经被 ringbuf 淘汰时，relay 发：

```
OUT(seq=0, payload="\x1b[2J\x1b[H")  // ANSI clear screen + cursor home
```

如果 relay 记录到会话当前处于 alternate screen，marker 会先进入 alternate screen（`\x1b[?1049h`）再清屏归位。随后从 ringbuf 当前最老 seq 开始补，让 client 重置渲染状态。

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
- 浏览器协议层：`web/src/shared/ws/protocol.ts` / `web/src/shared/ws/client-conn.ts`
- 桌面前端协议层：`desktop/frontend/src/lib/proto.ts` / `desktop/frontend/src/lib/connection.ts`
