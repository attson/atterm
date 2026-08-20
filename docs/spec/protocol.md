# Wire 协议规范 (v1)

> **Audience**: 实现 WS 帧或 HTTP API 客户端的工程师
> **Last updated**: 2026-07-29
> **Status**: stable
> **See also**: [auth.md](./auth.md) · [architecture.md](./architecture.md)

atterm 所有跨进程通信走单一二进制 WebSocket 帧协议。同一份协议被三类连接复用：`/agent`（CLI wrapper）、`/uplink`（桌面 lazy 控制连）、`/client`（attach 接管）。

## 传输

- WebSocket，binary message（**不**用 text）
- 一帧 = 一 WS message。**不要**在一个 message 里塞多帧
- 鉴权：见 §Auth in transit（携带姿势）与 [auth.md](./auth.md)（完整模型：Principal、生命周期、错误码、Bootstrap）。`internal/relay.Config.Resolver == nil` 是本地 / dev 嵌入场景的不鉴权降级。
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
    TypePrefsChanged  Type = 0x14  // relay → client-sessions
    TypePing          Type = 0x20
    TypePong          Type = 0x21
    TypeAnnounce      Type = 0x30  // uplink → relay  (Phase 1.5)
    TypeStreamRequest Type = 0x31  // relay → uplink
    TypeStreamStop    Type = 0x32  // relay → uplink
    TypePasteImage      Type = 0x33  // client → relay → desktop PTY host
    TypeClaimDriver     Type = 0x34  // client → relay (viewer claims driver role)
    TypeCommandEvent    Type = 0x35  // uplink → relay (Web Push notification trigger)
    TypeViewers         Type = 0x36  // relay → uplink (mirror remote subscriber count)
    TypePasteFile       Type = 0x37  // client → relay → desktop PTY host (generic file attachment)
    TypeFSRequest       Type = 0x38  // client → relay → desktop uplink (remote file explorer)
    TypeFSResponse      Type = 0x39  // desktop uplink → relay → requester client
    TypeFSEvent         Type = 0x3a  // desktop uplink → relay → requester client

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

**E2EE**：当 agent 持有 `account_key` 时，`原始字节流` 是 [§E2EE 信封](#e2ee-信封) 描述的 AEAD 信封，AAD 鉴别字节 = `0x03`（即帧类型本身）。relay 不解开、不做 OSC 解析，按 `session.MarkContentOpaque()` 关掉 OSC 路径。解密后的明文才是真实 PTY 字节，客户端走原本的 xterm 写入路径。

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
  "last_output_at": 1715234568,
  "type": "test",
  "summary": {
    "recent_output": "FAIL  ./internal/foo  0.123s\n",
    "error_lines": ["FAIL  ./internal/foo  0.123s"],
    "captured_at": 1715234580
  },
  "sealed": "<base64 AEAD envelope, optional>"
}
```

字段都 optional。agent 在 cwd / title 变化时发；relay 在 driver 变化或 PTY 尺寸（`UpdateSize`）变化时也会自行 broadcast 一帧。subscriber 收到后：

- `driver_client_id` 与本地生成的 `ATTACH.client_id` 比对，决定自己是 driver 还是 viewer
- `driver_client_name` 是 driver 当前 attach 时报上来的 `ATTACH.client_name`（典型是其 hostname），viewer 端用它在遮罩里显示 "by &lt;hostname&gt;"
- `cols` / `rows` 是 PTY 当前真实尺寸；viewer 把自己的 xterm `term.resize(cols, rows)` 锁到这个值（不跑 FitAddon）
- `task_state` 是任务状态：`idle` / `running` / `waiting_input` / `completed` / `failed` / `disconnected` / `closed`
- `current_command` / `command_started_at` / `command_ended_at` / `command_duration_ms` / `command_exit_code` 来自 OSC 133 命令生命周期；`command_exit_code` 只在命令结束后出现，`0` 表示 completed，非 0 表示 failed
- `last_output_at` 是 relay 最近看到该 session OUT 字节的 unix 秒时间戳
- `type` 是 session workload 分类：`shell` / `ai` / `test` / `build` / `deploy`；relay 在 OSC 133 `C` 时按命令 base name 计算（详见 `internal/session/classify.go`），并应用 sticky-non-shell 语义——一旦升过 non-shell 就不会再退回 `shell`
- `summary` 是上一条命令的 ANSI-stripped 尾部输出 + 抽取的错误行；只在 OSC 133 `D` 事件触发时刷新（recent_output ≤ 4 KiB；error_lines 仅在 `command_exit_code != 0` 时填充，最多 5 行）。subscriber 应当显示 `summary.error_lines[0]` 作为失败任务一行错误摘要。详细生成规则见 `internal/session/summary.go`

每个新 subscriber 在 `ATTACH` 后会立即收到一帧 snapshot META，包含当前 driver_client_id / driver_client_name / cols / rows / task metadata + type + summary，作为初始状态。

**E2EE（`sealed` 字段）**：当 agent 持有 `account_key` 时，`title` / `cwd` / `current_command` 三个明文字段被擦掉（写回 ""），同样的内容以 `SealedMetaFields { title, cwd, current_command }` JSON 形式封装进 [§E2EE 信封](#e2ee-信封)，base64-std 编码后写在 `sealed` 字段里。AAD 鉴别字节 = `0x05`（即 META 帧类型）。客户端在拿到 `account_key` 后解开 `sealed`，把字段 overlay 回去；不持有 key 的客户端只能看到 routing/驱动相关字段。`driver_client_id` / `task_state` / `cols/rows` / 时间戳 / `type` / `summary.captured_at` 仍走明文（routing + UI 必需），`summary.recent_output` / `summary.error_lines` 在 sealed 路径下由 agent 直接擦零；客户端如需富文本，等命令完成时通过 `CommandEventPayload.SealedBody` 拿到。

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
  "last_output_at": 1715234579,
  "type": "test",
  "summary": {
    "recent_output": "PASS\nok  ./internal/foo  0.123s\n",
    "captured_at": 1715234579
  },
  "sealed": "<base64 AEAD envelope, optional>"
}]
```

`remote_permission` 是 owner desktop 发布的可选字段；缺省表示 `full`，保持旧客户端兼容。

任务字段均为可选 additive metadata。缺失 `task_state` 的旧 publisher 按 `idle` 处理。`type` / `summary` 字段与 META 帧同语义（详见 §`META`），新增于 P2.11 / P2.12。

**E2EE（`sealed` 字段）**：M3b 之后 agent 在 ANNOUNCE 时把 `title` / `cwd` / `command` / `current_command` 写零，等价的 `SealedSessionFields { title, cwd, command, current_command }` 封装进 [§E2EE 信封](#e2ee-信封) 并 base64 存进 `sealed` 字段。AAD 鉴别字节 = `0x12`（LIST_RESP 帧类型）。`summary.error_lines` 在 sealed 路径下也由 agent 直接擦零，避免泄漏失败行内容；客户端解开 `sealed` 后把字段 overlay 回 list 渲染。

| 值 | 远程允许 | relay/host 拦截 |
|----|----------|-----------------|
| `view` | list / attach / 接收输出与历史 | `IN` / `RESIZE` / `PASTE_IMAGE` / `PASTE_FILE` |
| `control` | `view` + `IN` + `RESIZE` | `PASTE_IMAGE` / `PASTE_FILE` |
| `full` 或空 | `control` + `PASTE_IMAGE` + `PASTE_FILE` | 无 |

实践中很少用（前端走 REST `/api/sessions` 更直接）。

### `PREFS_CHANGED` (0x14) — relay → client-sessions

`/client-sessions` 连接上的轻量通知帧，payload 为空。任一客户端成功 `PUT /api/me/preferences` 后，relay 向同一 `user_id` 的 `/client-sessions` 订阅者发送该帧；客户端收到后复用现有 prefs sync `GET /api/me/preferences` / pull 流程，再在本地触发 `prefs:changed` 让 UI 重读。该帧不携带偏好内容，避免在 WS 上复制 LWW/校验逻辑；老客户端按未知 frame 忽略即可。

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

### `PING` (0x20) / `PONG` (0x21) — 应用层 RTT 探测

双向:任意一端可发起 PING,另一端必须将收到的 payload 原样作为 PONG 回送。

| 帧 | Payload |
|----|---------|
| `PING (0x20)` | 空 **或** 8 字节大端无符号毫秒 monotonic 时间戳 |
| `PONG (0x21)` | 收到的 `PING` payload 原样回送 |

8 字节形式让发起方用自己的时钟双端测 RTT,不依赖对端时钟:

```
rtt_ms = now_ms_local() - decoded(payload)
```

兼容矩阵:

- **旧客户端 + 新 relay**:旧客户端发空 PING → relay 回空 PONG。无 RTT,只当 liveness。
- **新客户端 + 旧 relay**:新客户端发 8B PING → 旧 relay 可能回空 PONG。客户端检测 payload 长度 ≠ 8 时丢弃此 sample,只用连接状态/重连计数。

使用:

- 桌面 uplink、web/PWA、mobile 各端每 5s 发一次,接收 PONG 后计算 RTT,在标题栏/header 显示 `ConnHealthPill`(绿 < 150ms、黄 150–500ms、红 > 500ms)。详见 `internal/connhealth/` 与 `web/src/shared/connhealth/`。
- relay 在 `/uplink` 和 `/client` 的 reader 路径里 echo;`/client-sessions` 单向不读不 echo。
- WebSocket 控制帧 ping/pong(nhooyr `Conn.Ping`)仍用于底层 keepalive,与这两帧独立。

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

远程 web/mobile client 粘贴或显式选择图片时发送。relay 将其按 session inbound 路径转发给拥有该 PTY 的 desktop host；desktop host 保存图片并尽量模拟本机图片粘贴（设置宿主机系统剪贴板图片后向 PTY 发送 `Ctrl-V`），不支持原生图片剪贴板的平台回退为向 PTY 粘贴临时文件路径。当前原生剪贴板路径：

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

权限与角色：只有当前 driver 且 `remote_permission = "full"` 的 subscriber 可以发送；`view` / `control` 或 viewer 状态必须被 relay 拦截，desktop host 执行前也要二次校验。

### `PASTE_FILE` (0x37) — client → relay → desktop PTY host

远程 client 显式选择/拖入的通用文件（PDF / log / diff / 任意二进制），路由与 `PASTE_IMAGE` 同构：driver + `full` 权限方可发送，非 driver 或不足权限静默 drop（日志 `not_driver` / `permission_denied`）。desktop 收到后 sanitize filename（strip 目录部分/控制字符、NFC normalize、≤128 字符、Windows 保留名前缀 `_`），落盘到 `<cache-root>/paste-files/<sid>/<safe-name>`（冲名追加 ` (N)`，`O_EXCL` 原子创建），然后把结果**绝对路径**直接 `Write` 进 PTY 主端（**无 CR，无引号**）。

payload = JSON：

```json
{
  "filename": "notes.pdf",
  "content_type": "application/pdf",
  "data": "<base64 file bytes>"
}
```

- `filename`：用户可见文件名（不含目录）。wire 值可以脏，desktop 强制 sanitize + dedup 才落盘。
- `content_type`：客户端 best-effort，服务器不校验、不据此路由。允许任意 mime（含 `application/octet-stream`）。
- `data`：原始字节。解码后 `≤ 10 MiB`（`maxPasteFileBytes`）；desktop 侧 backstop 与前端预检同数值。协议层仍受 payload 16 MiB 上限约束。
- **E2EE**：当持有 `account_key` 时，整个 `PasteFilePayload` JSON 可以走 [§E2EE 信封](#e2ee-信封) 加密，AAD 鉴别字节 = `0x37`。**当前 Go 侧 attach 客户端已支持**（另一台 atterm desktop attach 时）；`web` / `Capacitor` 前端当前与 PASTE_IMAGE 同 posture，发送明文 JSON（独立 spec 再做 browser sealed paste）。
- **权限**：`remote_permission = "full"` 才允许；`view` / `control` 被 relay 拒绝，同 PASTE_IMAGE。
- **driver-only**：非当前 driver subscriber 的 PASTE_FILE 被 relay 静默 drop。

区别于 PASTE_IMAGE：不塞 native clipboard、不发 `Ctrl-V`；文件名对 AI/shell 可见（保留 sanitized 原名，方便后续读取时通过 mime 或后缀推断类型）。

### Remote File Explorer (`FS_REQUEST` 0x38 / `FS_RESPONSE` 0x39 / `FS_EVENT` 0x3a)

远程文件浏览器使用三个 additive JSON 帧，均复用帧 header 里的 `session_id` 作为目标会话。它只定义协议载荷；relay/client/uplink 的具体处理在后续实现中接入。

#### Flow

1. 已 attach 的 client 为一次文件操作生成 `request_id`，发送 `FS_REQUEST` 到 relay。
2. relay 按 session 的 uplink 路由把请求转发到拥有本地 PTY/文件系统的 desktop uplink。
3. desktop uplink 调用本机受限文件访问层，返回同 `request_id` 的 `FS_RESPONSE`。
4. 对目录 watch，desktop uplink 后续用 `FS_EVENT` 向 requester client 推送变更；client 收到后重新发 `list_dir` / `file_meta` 等请求刷新。

#### Permissions

文件浏览会读取 owner 机器上的路径，权限必须是 owner 显式发布的 `remote_permission = "full"`。relay 和 desktop host 都必须按当前 session 的 `remote_permission` 拦截：

- `view` / `control`：不允许浏览或读取远程文件，所有 `FS_REQUEST` 都必须拒绝。
- `full`：允许只读浏览、预览、分块读取、目录 watch。

只读操作不要求当前 driver 状态：`list_dir` / `file_meta` / `read_file` / `read_chunk` / `watch_dir` / `unwatch_dir` 在 `remote_permission = "full"` 下可由已授权 client 发起。`open_external` 会在 owner 机器触发 OS 打开动作，因此额外要求发送方是当前 driver；relay 侧需按 session driver 状态拒绝，desktop uplink 执行前再拦一次，保持与 `IN` / `RESIZE` / `PASTE_IMAGE` / `PASTE_FILE` 的本机动作防线一致。

#### `FS_REQUEST` payload

```json
{
  "request_id": "uuid-or-random-string",
  "client_id": "relay-injected-attacher-client-id",
  "op": "list_dir",
  "path": "/Users/alice/project",
  "max_bytes": 2097152,
  "offset": 0,
  "length": 262144,
  "watch_id": "server-watch-id"
}
```

- `request_id`：client 生成的关联 ID；desktop 原样带回。
- `client_id`：relay 转发前按已 attach subscriber 的身份注入/覆盖；浏览器传入的值不可信且会被忽略。desktop host 只用它对 `open_external` 做当前 driver 二次校验，普通浏览器 client 不需要也不应该自行设置。
- `op`：`list_dir` / `file_meta` / `read_file` / `read_chunk` / `watch_dir` / `unwatch_dir` / `open_external`。
- `path`：owner 机器上的路径；desktop 必须走本地 allow-root/path-clean 校验。
- `max_bytes`：`read_file` 的最大返回字节数，host 仍有 hard cap。
- `offset` / `length`：`read_chunk` 的分块范围。
- `watch_id`：`unwatch_dir` 使用 desktop 之前返回的 watch id。

#### `FS_RESPONSE` payload

```json
{
  "request_id": "same-as-request",
  "ok": true,
  "error": "",
  "entries": [
    { "name": "src", "isDir": true, "size": 0, "modTime": 1760000000000 }
  ],
  "meta": { "path": "/Users/alice/project/README.md", "size": 1234, "modTime": 1760000000000, "isBinary": false },
  "content": { "path": "/Users/alice/project/README.md", "data": "base64", "isBinary": false, "truncatedAt": 0 },
  "chunk": { "path": "/Users/alice/project/logo.png", "data": "base64", "offset": 0, "length": 262144, "eof": false, "contentType": "image/png" },
  "watch_id": "server-watch-id"
}
```

- `ok=false` 时 `error` 是 user-visible-ish 的简短错误字符串；其它 result 字段可省略。
- `entries` 用于 `list_dir`，元素 schema 为 `DirEntry { name, isDir, size, modTime }`。
- `meta` 用于 `file_meta`，schema 为 `FileMetaInfo { path, size, modTime, isBinary }`。
- `content` 用于 `read_file`，schema 为 `FileContent { path, data, isBinary, truncatedAt }`。
- `chunk` 用于 `read_chunk`，schema 为 `FSChunkPayload { path, data, offset, length, eof, contentType }`。
- Go JSON 的 `[]byte` 字段按标准 base64 字符串编码。

#### `FS_EVENT` payload

```json
{
  "watch_id": "server-watch-id",
  "path": "/Users/alice/project",
  "event": "changed"
}
```

`event` 当前只要求支持 `changed`。watch 是 requester-scoped：relay 不应把一个 client 的 watch event 广播给其它 client。

#### Plaintext / E2EE posture

FS 三帧的 payload **不是**单个 JSON 文档，而是分段结构：

```
payload := segment_count(1B) || segment*
segment := length(4B BE) || bytes
```

segment 0 恒为明文 JSON，只放 relay 转发和鉴权真正需要的字段：`FS_REQUEST` 的 `request_id` / `op` / `client_id` / `max_bytes` / `offset` / `length` / `watch_id`，`FS_RESPONSE` 的 `request_id` / `ok` / `watch_id`，`FS_EVENT` 的 `watch_id` / `event`。relay 靠 `op` 执行只读白名单（`isReadOnlyFSOperation`）、靠 `request_id` / `watch_id` 路由，全程不持有 key——它通过 `proto.DecodeFSHead` / `EncodeFSHead` 只读写 segment 0。

其余字段（路径、文件名、目录列表、metadata、agent 侧 error 文本、文件内容字节）走后续 segment 的 [§E2EE 信封](#e2ee-信封)，AAD 鉴别字节见该节表格。文件字节单独占一个 segment 且**不经 base64**——`FileContent.Data` 本来就被 `encoding/json` base64 过一次，若再把信封 base64 进 JSON 字段会叠成 1.78× 膨胀；分段后维持在约 1.0×。

三条与其它 sealed 路径不同的规则：

- **持有 key 的一端恒定发出 sealed segment**，哪怕内容为空（例如 `unwatch_dir` 的响应）。segment 数表达的是 key 状态，不是"这条响应有没有数据"。
- **seal 失败 fail-closed**，不走 §612 的明文回退。因为 `.env*` 的放开条件正是"sealing 生效"，静默回退等于在守卫失效的瞬间把密钥送上线。此时返回一条不含路径的错误响应。
- **`.env*` 在远程侧仅当 sealing 生效时可读**。判据是 agent 自己的 key 状态（`fsAccess.denyEnv`），与任何入站字段无关，所以 relay 无法通过篡改请求把会话降级成明文。本地 Wails 直连不产生帧，恒可读。`.ssh` / `.gnupg` / `.aws` 两侧恒拒。

relay 仍可做 payload 大小限制（信封长度可见），但不再能审计路径。完整设计见 [../superpowers/specs/2026-08-07-fs-frame-e2ee-design.md](../superpowers/specs/2026-08-07-fs-frame-e2ee-design.md)。

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
  "label": "atterm",
  "sealed_body": "<base64 AEAD envelope, optional>"
}
```

- `session_id` rides the frame header (existing pattern).
- `host_id` is intentionally not in the payload. The relay reconstructs it from the sender's ANNOUNCE manifest at handler time, which makes cross-uplink spoofing impossible.
- The relay drops the frame silently when `session_id` is not present in the sender's current manifest.
- `label` is truncated to 256 bytes before being forwarded into a notification payload.

**E2EE（`sealed_body` 字段）**：当 agent 持有 `account_key` 时，`SealedPushBody { label, exit_code, elapsed_ms }` 封装进 [§E2EE 信封](#e2ee-信封)，AAD 鉴别字节 = `0x35`（COMMAND_EVENT 帧类型）；同时 agent 把 `label` 写空、`exit_code` / `elapsed_ms` 写零（M6-final）。relay 不解开，把 `sealed_body` 经 base64 透传到 Web Push payload 的 `sealedBody` 字段、webhook 的 `sealed_body` 字段。service worker 走 [MessageChannel 桥](../superpowers/specs/2026-06-15-relay-e2ee-design.md) 找可见 client 解密渲染富文本；无可见 client 时退化为通用 `AT Term · Session command finished`。

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

Emitted immediately after successful auth on `/uplink`, **before** the relay reads the first `ANNOUNCE` frame from the client. Only sent when the uplink authenticated as a `PrincipalUser` (i.e. via a session token). The dev / loopback path with no resolver does not emit this frame.

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

每个 session 在任意时刻最多有一个 driver subscriber。driver 是唯一允许把 `IN` / `RESIZE` / `PASTE_IMAGE` / `PASTE_FILE` 转发到 PTY 的连接；其它都是 viewer（只收 `OUT` / `META` / `CLOSE` / `REPLAY_PROGRESS`）。

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
| `/api/pair/create` | POST | 桌面端 owner 签发一次性 pairing token（详见 [auth.md](./auth.md)） |
| `/api/pair/consume` | POST | 移动端用 pairing token 换 relay URL + session token（详见 [auth.md](./auth.md)） |
| `/healthz` | GET | 公开 liveness 探测；返回 `{ok, version}`，无鉴权 |
| `/admin/health` | GET | admin-only 运维健康检查页（HTML） |
| `/admin/api/health` | GET | admin-only HealthPayload JSON（详见 §health endpoint） |
| `/` | GET | Web/PWA 主入口；加载 `web/src/main-web.ts`，复用桌面 `App.vue`（Settings / Admin 内嵌主界面） |
| `/login.html`, `/signup.html`, `/setup.html`, `/firstrun.html`, `/pair` | GET | 静态 Vue MPA 辅助页面（默认使用 embedded `internal/relay/web-dist/`；开发可用 `--web web/dist`） |

CORS：所有路径自动响应 `Access-Control-Allow-Origin: *`，`OPTIONS` 直接 204。非 `OPTIONS`
请求进入 mux 前会经过按远端 IP/token 计算的固定窗口 rate limit；WebSocket upgrade
还会经过同一 key 的活跃连接数限制。WebSocket Origin 白名单匹配的是 Origin
host（例如 `relay.example.com`、`*.example.com`）；`cmd/atterm-relay` 接受完整 URL
输入并规范成 host，同时追加 Wails 桌面客户端需要的 `wails` / `wails.localhost`
host pattern，这样桌面客户端和同源 web 客户端都能连接。

## Auth in transit

所有 protected endpoint 由 `requireSession` 中间件统一拦截。Token 通过以下姿势携带：

- HTTP `Authorization: Bearer <token>`
- WS `Sec-WebSocket-Protocol: atterm-token.<token>` 或 `atterm-token-b64.<base64url(token)>`

不接受 `?token=` URL query。不接受 cookie。

完整鉴权模型（Principal、生命周期、错误码、Bootstrap 流程、客户端实现要点）见 [auth.md](./auth.md)。

## E2EE 信封

agent 持有 `account_key` 时，下列字段以**统一 AEAD 信封**封装：`OUT` 帧的字节流、`META.sealed` / `SessionInfo.sealed` / `CommandEventPayload.sealed_body`。relay 不解开，只按 routing 必需的字段（session_id / 时间戳 / `task_state` / `cols/rows` / `driver_client_id` / `host_id`）做转发与限流。

### 信封 wire 格式

```text
envelope = cipher_id(1B)  ‖  nonce(24B)  ‖  XChaCha20-Poly1305_ciphertext(N + 16B tag)

cipher_id   = 0x01  (XChaCha20-Poly1305)
ciphertext  = encrypt(
                key       = HKDF-SHA256(account_key, info = "atterm-session-v1" ‖ session_uuid_bytes),
                nonce     = nonce,
                plaintext = JSON 序列化的 sealed 字段（或 OUT 帧的原始字节流）,
                aad       = session_uuid_bytes(16) ‖ frame_type(1B),
              )
```

key 派生：每 session 独立 `session_key = HKDF-SHA256(salt=nil, ikm=account_key, info=b"atterm-session-v1" ‖ session_uuid_bytes, length=32)`。session_id 不同 → key 不同；同 session 多次连接 / 重连用同一把 key。`account_key` 永远在 main thread / Keychain / Keyring 内，**不**进 URL / 日志 / IndexedDB / SW 全局（[AGENTS.md](../../AGENTS.md) §21）。

`cipher_id = 0x01` 是当前唯一已分配值。如果将来要换 cipher（AES-256-GCM-SIV、ChaCha20-Poly1305-RFC8439 等）就用 `0x02 / 0x03 / …`，让旧客户端通过 `cipher_id` 检测并优雅降级到明文回退路径。

### AAD 鉴别表（cross-type replay 防线）

AAD = `uuid(16B) || frame_type(1B)`。`frame_type` 字节**等于该 sealed 字段所在帧的 `Type` 字节**，把信封绑死到帧类型上——攻击者就算偷到一条合法信封，也无法把它替换到别的帧里（cipher 解开会因 AAD 不匹配直接失败）。

| frame_type | 出现位置 | sealed 内容 |
|------------|----------|-------------|
| `0x02` `IN` | IN 帧 payload（整体，无 seq） | 原始 UTF-8 键入字节；relay→agent 方向，desktop `openInboundFrame` 已支持解封，尚无发送端（web/desktop 前端）产出该信封 |
| `0x03` `OUT` | OUT 帧 `seq` 后的字节流 | 原始 PTY 输出字节 |
| `0x05` `META` | `MetaPayload.sealed`（base64） | JSON `SealedMetaFields { title, cwd, current_command }` |
| `0x12` `LIST_RESP` | `SessionInfo.sealed`（base64） | JSON `SealedSessionFields { title, cwd, command, current_command }` |
| `0x33` `PASTE_IMAGE` | PASTE_IMAGE 帧 payload（整体） | JSON `PasteImagePayload { filename, content_type, data }`；relay→agent 方向，desktop `openInboundFrame` 已支持解封，尚无发送端产出该信封 |
| `0x35` `COMMAND_EVENT` | `CommandEventPayload.sealed_body`（base64） | JSON `SealedPushBody { label, exit_code, elapsed_ms }` |
| `0x37` `PASTE_FILE` | PASTE_FILE 帧 payload（整体） | JSON `PasteFilePayload { filename, content_type, data }` |
| `0x38` `FS_REQUEST` | 分段 payload 的 segment 1（裸二进制，非 base64） | JSON `SealedFSRequestFields { path, new_path }` |
| `0x39` `FS_RESPONSE` | segment 1（元数据）+ segment 2（文件字节） | segment 1 = JSON `SealedFSResponseFields { entries, meta, error, content, chunk }`；segment 2 = 原始文件字节，不经 base64 |
| `0x3a` `FS_EVENT` | 分段 payload 的 segment 1（裸二进制） | JSON `SealedFSEventFields { path }` |
| `0xF0` （合成，不上 wire） | `ssh_hosts_encrypted` 偏好值 | JSON `sshSyncPayload { hosts, keys }` |
| `0xF1` （合成，不上 wire） | `profiles_encrypted` 偏好值 | JSON `profilesSyncPayload { profiles, default_profile_id }` |

**红线**：加新 sealed 帧时**必须**给一个**唯一**的 `frame_type` 字节，并在这张表里增行；不允许复用（[AGENTS.md](../../AGENTS.md) §22）。

### Plaintext strip 与 fallback

Agent seal 成功后**必须**把对应明文字段擦零（[AGENTS.md](../../AGENTS.md) §23）：

| 信封 | 同时清零 / 清空的明文字段 |
|------|---------------------------|
| `META.sealed` | `MetaPayload.title` / `cwd` / `current_command` 写为 `""` |
| `SessionInfo.sealed` | `SessionInfo.title` / `cwd` / `command` / `current_command` 写为 `""`；`summary.error_lines` 清空 |
| `CommandEventPayload.sealed_body` | `label` 写空、`exit_code` / `elapsed_ms` 写零 |
| `OUT` 帧 | 整条字节流就是密文（没有明文 fallback） |

`account_key` 解锁失败 / seal 出错 / 客户端是旧版本 → agent 走 fallback：sealed 字段不发，明文字段照常 publish。这是 "no key = no encryption" 的对称路径，让 dev 模式和未注册账号下的体验不受影响。

### 实现指针

- 编解码：`internal/proto/codec.go`（`CommandEventPayload.SealedBody`、`EncodeCommandEvent` / `DecodeCommandEvent`）+ `internal/proto/frame.go`（`SessionInfo.Sealed`、`MetaPayload.Sealed`）
- 通用 seal / open：`internal/e2eecrypto/sessionkey.go::DeriveSessionKey` + `envelope.go::SealOut / OpenOut`（seq-bound）/ `SealUnsequenced / OpenUnsequenced`
- agent seal helper：`desktop/uplink_seal_fields.go`（SessionInfo + META）、`desktop/uplink_seal_push.go`（CommandEvent）
- relay 拒绝 OSC 解析：`internal/relay/uplink_conn.go::looksLikeEncryptedOut` → `session.MarkContentOpaque()`
- 客户端 open：Web `web/src/shared/lib/opaque.ts::openSessionFields / openMetaFields / openPushBodyFields`；iOS `desktop/frontend/src/lib/opaque.ts`（同源镜像）；Service Worker 走 `web/src/shared/sw-bridge.ts` 的 MessageChannel 桥
- 完整设计与威胁模型：[../superpowers/specs/2026-06-15-relay-e2ee-design.md](../superpowers/specs/2026-06-15-relay-e2ee-design.md)

## Health endpoint

公开 liveness：`GET /healthz` 始终公开、无鉴权，返回最小 JSON `{ok: true,
version: "<v>"}`，专供 LB / k8s probe。

管理员健康检查页：`GET /admin/health` (HTML) 和 `GET /admin/api/health` (JSON)
都需要 admin principal。JSON 契约 (`HealthPayload`)：

```json
{
  "version": "v0.2.33",
  "uptime_seconds": 12345,
  "https": true,
  "configured_origins": ["https://relay.example.com", "capacitor://localhost"],
  "origins_open": false,
  "bootstrap_admin_configured": true,
  "rate_limit_per_minute": 600,
  "max_connections_per_key": 64,
  "active_uplinks": 3,
  "mobile_origin_compatible": true,
  "generated_at": "2026-06-05T03:14:15Z",
  "health_check_warnings": []
}
```

约束：

- 没有 PII / token 明文 / 文件路径 / 客户端 IP——每个字段是 operator-
  configured 值或聚合计数，可以安全粘贴进 issue。
- `https` 来自 `r.TLS != nil || X-Forwarded-Proto == "https"`，所以 reverse
  proxy 必须正确传递这个头才能显示 true。
- `mobile_origin_compatible` 检查 `configured_origins` 是否包含
  `capacitor://*` / `ionic://*` / `https://localhost*` / `null` 之一，或
  origin 白名单为空（"open"，会另外触发一条 warning）。
- HTML 页面用 `internal/relay/templates/health.gohtml` 内嵌模板渲染；前端
  无额外 JS，所以即使 webview / web build 损坏也能看到诊断信息。

实现：`internal/relay/health_http.go`。

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
