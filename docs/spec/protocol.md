# Wire 协议规范 (v1)

> **Audience**: 实现 WS 帧或 HTTP API 客户端的工程师
> **Last updated**: 2026-09-23
> **Status**: stable
> **See also**: [auth.md](./auth.md) · [architecture.md](./architecture.md)

atterm 的 terminal data path 走单一二进制 WebSocket 帧协议。同一份协议被三类连接复用：`/agent`（CLI wrapper）、`/uplink`（桌面 lazy 控制连）、`/client`（attach 接管）。辅助通道（例如 `/direct-signal`）使用各自独立且有版本号的 schema，不占用 terminal `proto.Type`。

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
    TypeSessionCreate   Type = 0x3b  // client → relay → desktop uplink
    TypeSessionCreated  Type = 0x3c  // desktop uplink → relay → requester client
    TypeServiceOpen     Type = 0x3d  // client → relay → desktop uplink
    TypeServiceOpened   Type = 0x3e  // desktop uplink → relay → requester client
    TypeServiceClose    Type = 0x3f  // client → relay; revoke Preview lease

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

### Remote Web Preview (`SERVICE_OPEN` 0x3d / `SERVICE_OPENED` 0x3e / `SERVICE_CLOSE` 0x3f)

Preview 控制帧沿已 attach session 路由；实际 HTTP/TCP 字节不进入本帧协议，
而走独立 `/service-client` / `/service-host` WebSocket。该独立通道不创建
session subscriber，不触发 `STREAM_REQUEST/STOP`。

`SERVICE_OPEN` payload：

```json
{
  "request_id": "uuid",
  "service_id": "uuid",
  "host_ticket": "relay-injected-one-time-ticket",
  "sealed": "<base64 AEAD envelope>"
}
```

- client 生成 `request_id/service_id`；`host_ticket` 留空并由 relay 覆盖。
- `sealed` 打开后通常是 `{ "port": 3000, "scheme": "http", "host": "localhost" }`，
  AAD 鉴别字节 `0x3d`。`host` 只接受 `localhost`、`127.0.0.1` 或 `::1`，
  缺省为 `localhost`（桌面 UI 默认选择 `127.0.0.1`）；它只选择 owner 的
  loopback 地址族，不能指向任意内网主机。
- relay 只在 client 已 attach、当前为 driver、session 的
  `remote_permission=full` 时转发；desktop 按自己的 raw permission 再验一次。

一个多端口 Preview 会为每个映射分别发送一条 `SERVICE_OPEN`，随后在 client
侧本地 gateway 按路径前缀汇聚这些 lease；现有帧结构和 relay 鉴权语义不变。
例如 `/`、`/api`、`/ws` 分别对应三个独立的 `service_id`。

`SERVICE_OPENED` payload：

```json
{
  "request_id": "uuid",
  "service_id": "uuid",
  "ok": true,
  "error": "",
  "client_ticket": "relay-injected-one-time-ticket"
}
```

响应只路由给发起请求的 `/client`；relay 只在成功响应中注入
`client_ticket`。两个 ticket 只允许在对应 service WS 的第一条注册消息中使用
一次，不进 URL/日志。service WS 接受注册后先回 JSON `{ "ok": true }` ACK；
端点收到 ACK 后才宣告 ready 或发 multiplex packet，拒绝则直接关闭 WS。
`SERVICE_CLOSE {service_id}` 幂等撤销 lease；client
连接、owner uplink、session 或任一 service WS 断开同样撤销。

Service data message 的 AES-GCM multiplex 格式、额度和 key derivation 见
[`2026-08-29-remote-web-preview-phase1-design.md`](../superpowers/specs/2026-08-29-remote-web-preview-phase1-design.md) §3–4。

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
| `/direct-signal` | GET (Upgrade: websocket) | 可选的 WebRTC direct signaling JSON 通道 |
| `/service-client` | GET (Upgrade: websocket) | Preview 客户端 E2EE multiplex 数据通道 |
| `/service-host` | GET (Upgrade: websocket) | owner desktop E2EE multiplex 数据通道 |
| `/api/sessions` | GET | JSON 列表（local + mirror） |
| `/api/me/traffic` | GET | 当前账号最多 180 天的 Relay 帧级/日级与 P2P 日级流量；只从认证上下文取 user id |
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

## WebRTC direct signaling（独立 JSON 协议）

`/direct-signal` 是 v0.6 Relay-assisted P2P 的控制面，不承载 terminal frame 或 PTY 字节。它使用 text WebSocket JSON，每条消息都有独立的 `version: 1`；因此 `internal/proto.Version` 仍为 `1`，现有 `/client`、`/uplink` 和 frame payload 均不改变。

Relay 端默认关闭该端点，关闭时 upgrade 返回 `404`。操作员必须显式传 `--direct-signal` 或设置 `ATTERM_DIRECT_SIGNAL_ENABLED=1`。端点复用现有 session auth、Origin allow-list、HTTP/WS rate limit 与连接数限制；token 仍只允许放在 `Authorization: Bearer` 或 `Sec-WebSocket-Protocol`，不允许 query token。

### 连接 hello 与 host 注册

连接建立后 10 秒内必须发送第一条 `hello`，否则 Relay 关闭连接。单条 text message 上限 64 KiB；未知 JSON 字段忽略。

Client hello：

```json
{"version":1,"kind":"hello","role":"client","client_instance_id":"install-or-boot-scoped-id"}
```

Host hello 同时注册当前可直连的 session：

```json
{"version":1,"kind":"hello","role":"host","host_id":"host-uuid","session_ids":["session-uuid"]}
```

Relay 返回 `{"version":1,"kind":"hello_ok"}`。host 可在会话集合变化后发送 `host_register`（同样携带 `host_id/session_ids`），成功时收到带原 `request_id` 的 `host_registered`。一次最多注册 256 个 session；每个 session 必须属于当前账户、其发布的 `host_id` 必须匹配，并且对应 `/uplink` 必须在线。注册表按 `session_id` 路由，不按 `host_id` 去重会话。

### Direct attempt

已通过 Relay attach 的 client 发送：

```json
{
  "version": 1,
  "kind": "direct_request",
  "request_id": "client-request-id",
  "session_id": "session-uuid",
  "since_seq": 42
}
```

Relay 再次校验 session owner 与 owning uplink，随后向 client 和已注册 host 各发送相同的 `direct_attempt`：

```json
{
  "version": 1,
  "kind": "direct_attempt",
  "request_id": "client-request-id",
  "attempt_id": "random-uuid",
  "ticket": "32-byte-base64url-no-padding",
  "session_id": "session-uuid",
  "host_id": "host-uuid",
  "client_instance_id": "install-or-boot-scoped-id",
  "user_id": "account-user-id",
  "permission": "view|control|full",
  "since_seq": 42,
  "expires_at_unix_ms": 1800000000000
}
```

ticket 有效期 30 秒且单次使用。Relay 内存只保留 ticket 的 SHA-256 hash 和精确 claims，最多保留 1024 个全局 attempt、每个 signaling peer 最多 8 个。ticket 仅证明 Relay 授权，DataChannel 建立后双方还必须按 direct transport handshake 使用 `account_key` 证明；只持有 ticket 不能伪造直连端点。

### SDP / ICE 路由与终止

双方只可对属于自己的 `attempt_id` 发送以下消息；Relay 从允许字段重建转发消息，不转发额外字段：

```json
{"version":1,"kind":"signal","attempt_id":"uuid","signal_type":"offer|answer|ice_candidate|ice_end","payload":"opaque string"}
{"version":1,"kind":"cancel","attempt_id":"uuid","code":"ice_failed"}
{"version":1,"kind":"consumed","attempt_id":"uuid"}
{"version":1,"kind":"direct_result","code":"route_lost"}
{"version":1,"kind":"direct_stats","bytes_avoided":263168,"bytes_sent":262144,"bytes_received":1024}
```

- client 只能发送一次 offer，host 只能在 offer 后发送一次 answer；两者各最多发送 64 个 ICE candidate 和一次 `ice_end`。
- offer/answer payload 最大 32 KiB，单个 ICE candidate 最大 8 KiB；`ice_end` payload 必须为空。
- `cancel` 终止并移除 attempt；`consumed` 只能由 host 发送，表示 ticket 已由 host 原子消费，同样移除 attempt。
- 已认证 client 在活跃直连异常断开时可发送一次 `direct_result/route_lost`；已认证 host 可发送 `direct_stats` 的累计字节增量。host 在增量达到 256 KiB、每 5 秒存在未上报增量或直连关闭时发送，确保低流量活跃连接也能及时进入看板。`bytes_sent` / `bytes_received` 是 host 视角的双向已接受 terminal frame wire bytes，每个非零字段单条最多 64 MiB；`bytes_avoided` 是给旧 Relay 的兼容总量，新 Relay 在双向字段存在时忽略它。Relay 按认证账号和 UTC 日聚合 attempts / successes / fallbacks / bytes，个人看板刷新时会先落盘当前内存增量；统计不携带 session id、candidate 地址或 terminal 内容，也不参与路由/授权决策。
- signaling peer 断开会取消与它相关的全部 attempt；过期 attempt 在下一次相关操作时清理，并向仍在线的两端发送 `cancel` / `ticket_expired`。
- Relay 不解析 SDP/ICE 做授权决策，也不得记录 ticket、SDP、ICE、proof、key 或 DataChannel payload。

Stage 1 endpoint 默认用 `stun:stun.cloudflare.com:3478` 发现 server-reflexive ICE candidate，不提供 TURN。STUN 不承载 terminal data，但服务方可观察请求源 IP/时序；WebRTC 对端会获得建立直连所需的 candidate 地址。公共 STUN 或 P2P UDP 不可用时，client 必须回退已有 Relay data path。

Client transport 按运行时分层：Web/Capacitor 使用浏览器
`RTCPeerConnection`；Wails desktop 使用 Go/Pion client，并由 platform bridge
把认证完成、诊断、DIRECT_READY 和现有 protocol frame 事件交给共享
`SessionConnection` 状态机。桌面端的 `account_key` 直接从 Go 内存读取，不随
bridge event 传输。两种实现使用完全相同的 signaling、handshake、record 与
fallback 语义。

错误以 `{"version":1,"kind":"error","request_id":"...","code":"...","message":"..."}` 返回。协议/边界错误只拒绝当前消息或 attempt；hello 非法才关闭 signaling 连接。稳定 fallback 类别及 direct handshake/record/handover 细节见 [Relay-assisted P2P Acceleration Design](../superpowers/specs/2026-09-22-relay-p2p-acceleration-design.md)。

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

Relay session 信封的 AAD = `uuid(16B) || frame_type(1B)`。`frame_type` 字节**等于该 sealed 字段所在帧的 `Type` 字节**，把信封绑死到帧类型上——攻击者就算偷到一条合法信封，也无法把它替换到别的帧里（cipher 解开会因 AAD 不匹配直接失败）。`0xF0..0xF3` 是不上 Relay frame 的合成 namespace，使用表中列出的独立上下文。

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
| `0x3d` `SERVICE_OPEN` | `ServiceOpenPayload.sealed`（base64） | JSON `SealedServiceOpenFields { port, scheme }` |
| `0xF0` （合成，不上 wire） | `ssh_hosts_encrypted` 偏好值 | JSON `sshSyncPayload { hosts, keys }` |
| `0xF1` （合成，不上 wire） | `profiles_encrypted` 偏好值 | JSON `profilesSyncPayload { profiles, default_profile_id }` |
| `0xF2` （合成，不上 wire） | Peer config payload | `space_id(16B) || 0xF2 || key_class(1B) || epoch(be64) || len16+collection || len16+record_id || len16+op_id` |
| `0xF3` （合成，不上 wire） | Peer epoch key envelope | `space_id(16B) || 0xF3 || key_class(1B) || epoch(be64) || recipient_peer_id(32B) || recipient_wrap_public_key(65B) || ephemeral_public_key(65B)` |

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

## Peer Space signed documents

Peer Space 身份、成员关系和邀请独立于 Relay 账户与 `account_key`。当前文档格式版本为
`1`，不占用 `internal/proto.Type`，也不改变现有 Relay WebSocket frame。

七类 token 使用相同信封：

```text
<prefix>.<base64url(exact-json-bytes)>.<base64url(p1363-signature)>
```

| Prefix | Document | Purpose |
|---|---|---|
| `apg1` | `SpaceGenesis` | 不可变 Space trust anchor |
| `apm1` | `DeviceGrant` | 设备 membership/capability certificate |
| `atp1` | `CapabilityTicket` | 与网络路径无关的单次预签邀请 |
| `apj1` | `JoinRequest` | 新设备对 subject private key 的短期持有证明 |
| `atc1` | `ConnectionBundle` | invitation 与当前可达 route hints 的短期签名包装 |
| `arv1` | `Revocation` | deny-wins member/grant/issuer-scoped invitation batch 撤销 |
| `akr1` | `EpochRotation` | sync/vault epoch key 的成员封装与确定性轮换候选 |

签名算法是 P-256 ECDSA + SHA-256，signature 为 64-byte IEEE P1363 `r || s`，且只接受
low-S。签名覆盖 payload 中的原始 JSON bytes；验证端不得先反序列化再序列化。P-256
public key 使用 65-byte uncompressed SEC1/WebCrypto raw 格式，`peer_id` 是该字节串的
`base64url(SHA-256(public_key))`。

每个设备还持有一把独立的静态 P-256 ECDH wrapping identity，只用于接收 sync/vault
epoch key envelope，绝不复用 ECDSA signing private key。wrapping public key 同样使用
65-byte uncompressed SEC1/WebCrypto raw 格式，并作为
`subject_wrapping_public_key` 同时写入 `apj1` JoinRequest 和 `apm1` DeviceGrant。两份文档的
签名都覆盖该字段；membership 签发时必须逐字节复制已验证 JoinRequest 的 wrapping key。

`CapabilityTicket` 包含 `invite_id`、`batch_id`、`space_id`、
`redemption_peer_id`、`space_genesis_hash`、`issuer_peer_id`、完整
`issuer_membership`、32-byte `pairing_secret`、有效期、`max_uses=1`、permission、session
scope 和可选 delegation capability。它不包含 Relay URL/token、Quick Tunnel URL 或
Rendezvous 地址；后续连接地址放在独立的 signed `ConnectionBundle`，所以 route 轮换不会
使 membership 或预签 ticket 失效。

`ConnectionBundle` 包含完整 `apg1` genesis、`bundle_id`、issuer membership/peer id、
`routes[]`、创建与过期时间，并由 issuer peer 签名。首次分享时还携带完整 `atp1` ticket；
全新设备可直接从 bundle 取得 trust anchor 并核对 ticket 的 genesis hash，不依赖外部目录。
邀请核销后，route refresh bundle 省略一次性 ticket，只用持久 issuer membership 建链；连接
client 仍须在 handshake 证明自己的有效 membership。因此 invitation 过期或已消费都不要求
重签 membership。bundle 默认有效 10 分钟、最长 24
小时，且绝不超过 invitation 到期时间。当前 route kind 是 `quick_tunnel` 与
`rendezvous`：Quick Tunnel 只接受无 userinfo/port/query/fragment、root path 且 hostname
恰为单个合法 DNS label 的 `https://<label>.trycloudflare.com`；Rendezvous 只接受
`https`/`wss` service origin（只含 scheme + host + 可选 port，root path）且必须携带 32-byte
opaque topic。客户端统一从 origin 派生 `/v1/connect`，不允许 route 自带 API path。URL 轮换只
创建新的 `atc1`，不创建 invitation 或 membership。

新设备生成自己的 P-256 signing identity 和独立 P-256 ECDH wrapping identity 后，将完整
`atp1` invitation、subject peer/signing public key、wrapping public key、32-byte nonce 和创建
时间放入 `apj1`，并以 subject signing private key 签名。签发设备验证 invitation chain、
subject key proof 与 wrapping public key，且只接受 ticket 指定的 `redemption_peer_id`，随后
签发持久 `apm1` membership。JoinRequest 只在创建时间前后 5 分钟内有效。

签发设备在本地加密账本中用跨进程原子锁核销 `invite_id`，并将已签发 membership 与核销
记录一同原子落盘。同一 subject 重试返回第一次签发的同一 membership；其它 subject 重放
返回 already-consumed。撤销只影响尚未消费的 ticket，不能通过撤销旧 invitation 反向撤销
已经签发的 membership；成员撤销走独立的 member/grant revocation。当前实现位于
`internal/peercrypto`、`internal/peerproto`、`internal/peerstore`，桌面绑定位于
`desktop/peer_space.go`。Web 的 non-exportable IndexedDB identity 和 iOS Keychain adapter
位于 `desktop/frontend/src/lib/peer/identity.ts` 与
`desktop/frontend/src/platform/capacitorPeerIdentity.ts`；它们不向 Wails 桌面前端暴露桌面
private key。Web 的 signing 与 wrapping private key 都是 non-exportable `CryptoKey`；旧 v1
record 原地新增 ECDH key pair，不轮换 signing key 或 `peer_id`。iOS v2 record 将两把 PKCS#8
private key 只序列化到 Keychain，运行时重新导入为 non-exportable key；v1 迁移同样只新增
wrapping identity。

首次加入通过 Quick Tunnel 同源的 `POST /peer/v1/join` 完成。HTTP body 只暴露版本、
`invite_id`、XChaCha20-Poly1305 nonce 和 ciphertext；完整 `apj1`、`atp1`、pairing secret、
membership 与 epoch envelope 都不会以明文经过 tunnel。request/response key 分别由
HKDF-SHA256(`pairing_secret`, salt=`invite_id`,
info=`atterm-quick-tunnel-join-v1\x00<direction>`) 派生，AAD 为
`atterm-quick-tunnel-join-v1\x00<direction>\x00<invite_id>`，两个方向不能互换重放。
生产客户端只使用签名 `atc1` 中的 HTTPS Quick Tunnel route；HTTP override 仅允许 loopback
测试。服务端所有失败统一返回拒绝，不暴露 invitation 是否存在、已消费或已撤销。

解密后的响应只包含完整 genesis、新签 membership、membership directory、revocations、
epoch rotations，以及为新设备 wrapping key 单独封装的当前 `ake1` bootstrap envelopes。
加入新成员不旋转 epoch；客户端验证 genesis 与 `atc1` trust anchor 完全一致、membership 的
subject/wrapping key 和 ticket capability 完全一致、全部 governance token 与 rotation DAG，
再解开当前 sync（及获准时 vault）key并核对 rotation key commitment。Desktop 的
`PreviewPeerConnectionBundle` 只返回上述签名元数据供用户确认，`JoinPeerSpace` 必须收到原样
回传的 `SHA256:<genesis hash>` 后才发网络请求；邀请只能放在原始 token 或 URL fragment，拒绝
query 携带。epoch keys 先写 secure storage，最后才一次性创建加密 Peer store；任一验证或
解封失败都不能留下已初始化 Space。bootstrap envelopes 也保存在加密 store 中，Keychain
epoch entry 丢失后可重新解封恢复。

Peer Space 的加密本地 state schema v6 包含 grow-only `memberships[]` 目录（最多 256 个候选）
和 recipient-bound `epoch_envelopes[]`。
新建 Space、邀请核销和读取 v1-v4 state 时，都会从 local membership、已核销 invitation 以及
每个 token 内嵌的 issuer chain 补齐目录；真实 genesis 下每个 token 必须按签发时刻验证完整签名
与 capability narrowing。同一 token 重放幂等，同一 membership serial 对应不同有效 token 时整批
fail closed。历史 token 即使已经过期仍留在目录用于 anti-entropy，但当前授权和 rotation recipient
view 会按当前时间重新验证，只选择每个 peer 的 canonical active grant（最新 `issued_at`，再按
serial/token 确定性决胜），不会合并多个 grant 的 capability。member/grant revocation 在 canonical
选择后执行 deny-wins；canonical grant 被撤销时不会回退到同一 peer 的旧 grant。

`arv1` revocation 是不可变 grow-only governance operation，包含 `revocation_id`、Space/genesis
锚、kind、target、actor membership 和创建时间。`member` 撤销永久拒绝 peer identity，`grant`
撤销拒绝单个 membership serial；两者要求 actor 具有 `permission=full && can_invite=true`。
`invitation_batch` 允许 `can_invite=true` 的 actor 撤销自己签发的 batch，作用域实际是
`(actor_peer_id, batch_id)`，相同 UUID 不会影响其它 issuer。所有有效撤销做集合并集，不存在
un-revoke，也不走普通配置 LWW；重复 token 幂等，同一 `revocation_id` 对应不同有效 token 时
fail closed。设备保留全部 token 供后续 anti-entropy，并从中派生 member/grant deny map；本地
redemption ledger 只把 actor membership 与本机 membership 完全一致的 batch 撤销应用到未消费
ticket。已消费 invitation 对应的 membership 不会因 batch 撤销失效，必须显式发布 member/grant
撤销。当前 membership 目录、撤销日志和派生集合随加密 `peer-space.json` 原子持久化。明文 state
schema 为 v6；读取 v1-v5 后在下一次写入时迁移，外层加密 envelope 与 AAD 保持 v1，避免破坏
已有本地 Space。

Desktop 发起 `member` 撤销时，必须在同一个 Peer store 文件事务内持久化 `arv1`，并基于已应用
该撤销后的 active membership view 同时生成、校验和追加 sync/vault 两条后继 `akr1`。任一 rotation
构造或授权失败时整个事务回滚，不能留下“成员已撤销但未来数据仍使用旧 epoch key”的状态。成功
提交后本机从新 rotation envelope 恢复两类 key、失效 config runtime，并立即重验当前 Quick Tunnel
attempt；目标成员的现有连接关闭，后续握手仍由 deny-wins member view 拒绝。

`akr1` rotation document 包含 `rotation_id`、`space_id`、`key_class`、
`previous_epoch`、`previous_rotation_hash`、新 `epoch`、epoch key 的 SHA-256 commitment、
actor membership、按 `peer_id` 排序的 recipient envelopes 和创建时间。actor 必须持有当前有效的
`permission=full && can_invite=true` membership；接收端还必须用已应用 deny-wins revocation 的
active membership view 复核 actor 和完整 recipient 集合。sync rotation 必须覆盖所有 active
members；vault rotation 只覆盖 `can_sync_secrets=true` members。每个 recipient 同时绑定 grant
serial、membership 中的 wrapping public key 与 `ake1` envelope，缺少、多出或替换任一项都拒绝。

初始 rotation 是 `previous_epoch=0`、空 parent hash、`epoch=1`。后续 rotation 必须满足
`epoch=previous_epoch+1`，并显式引用父 `akr1` token 的 SHA-256 hash。分区中的两个 admin 若为
同一 parent 生成候选，lexicographically smaller operation hash 胜出；losing branch 及其后继
保留供诊断/anti-entropy，但不能进入 canonical chain，必须基于 winner 重新轮换。设备解开自己的
`ake1` 后还要核对 key commitment，防止同一 rotation 给不同 recipient 包裹不同 epoch key。

配置与治理状态通过 transport-independent anti-entropy JSON 消息交换，不占用 Relay
`proto.Type`。接收端先发送 `AntiEntropyInventory{v,space_id,vector,membership_hashes,
revocation_hashes,rotation_hashes}`：config history 用 contiguous version vector 摘要，grow-only
`apm1`/`arv1`/`akr1` 候选用排序且去重的 SHA-256 token hash 清单摘要（membership 最多 256，
revocation/rotation 每类最多 4096）。发送端从同一 durable snapshot 生成不可变 plan，并严格按
`membership → snapshot → config operation → revocation → rotation` 输出，使接收端在校验 snapshot
creator、operation actor 和 rotation recipients 前先原子持久化新成员目录。默认 batch 上限
256 KiB，可协商范围 1 KiB–16 MiB；上限按完整 JSON 编码后的实际字节数检查，不按 token 原始
长度估算。

每个 batch 带 `start` cursor、可选 `next` cursor、`done`、发送端 durable ack frontier 和若干
`{kind,token_hash,offset,total,data}` chunk。超过 batch 上限的单个 signed token（包括最大 32 MiB
snapshot）跨 batch 连续切分；接收端只在完整重组、SHA-256 相符、原 token 签名/Space 锚/结构
全部验证后才向上层交付。cursor 必须连续，同一 plan 各页 ack 必须相同；仅允许最近一次已接受
batch 原字节重发且作为幂等 no-op。任一 chunk 错误不推进 assembler 状态。snapshot 只有在其
cover vector 覆盖接收端 inventory vector 时才能规划；双方 vector 并发时先反向补齐独有 op 再
重建 plan，禁止覆盖接收端状态。该逻辑位于 `internal/configsync/anti_entropy.go`。Peer
DataChannel adapter 在同一条可靠有序 DataChannel 上使用独立 config logical channel；配置消息
只进入 config callback，不进入 terminal frame callback，也不创建 `session.Subscribe`，因此不改变
terminal subscriber lifecycle。

配置 logical channel 使用 direct encrypted record kind，不占用 Relay `proto.Type`，且保留 Stage 1
已有 kind 的 wire byte：

| Record kind | Byte | Plaintext |
|---|---:|---|
| `FRAME` | `0x01` | 单个 marshaled terminal `proto.Frame` |
| `FRAGMENT` | `0x02` | Stage 1 terminal frame fragment |
| `DIRECT_READY` | `0x03` | initial replay frontier |
| `PING` / `PONG` / `CLOSE` | `0x04` / `0x05` / `0x06` | direct route control |
| `CONFIG_INVENTORY` | `0x07` | JSON `AntiEntropyInventory` |
| `CONFIG_BATCH` | `0x08` | JSON `AntiEntropyBatch` |
| `CONFIG_ACK` | `0x09` | JSON `{v,space_id,cursor,durable,done}` |
| `CONFIG_FRAGMENT` | `0x0a` | 分片后的任一 config logical message |
| `SIGNAL` | `0x0b` | JSON `Signal{v,type,payload}`，仅用于加密后的 SDP/ICE |
| `SIGNAL_FRAGMENT` | `0x0c` | 分片后的单个 signaling message |

单个 record plaintext 上限仍是 16 KiB。超过上限的配置消息用独立格式分片：

```text
"ACF1"(4B) || message_id(be64) || 0xffffffff(4B) || original_kind(1B) ||
offset(be32) || total(be32) || data
```

`original_kind` 只允许 `0x07..0x09`；`total` 必须大于 16 KiB 且不超过 16 MiB。固定
`0xffffffff` 同时使 config fragment 无法被 terminal fragment reassembler 接受。terminal 与
config 各自只允许一个连续消息重组，状态完全分离；乱序、交错、超时、越界、未知类型、AEAD
篡改或上层 JSON/授权失败都关闭当前 Peer route，不把 payload 投递到另一逻辑通道。

Quick Tunnel signaling 使用第三套独立分片格式：

```text
"ASF1"(4B) || message_id(be64) || 0xfffffffe(4B) ||
offset(be32) || total(be32) || data
```

`total` 必须大于 16 KiB 且不超过 64 KiB。`ASF1` 和 `0xfffffffe` 使 signaling fragment
不能被 terminal/config reassembler 接受；每个方向同样只允许一个连续 signaling message。

双方在 membership handshake 成功后各自发送 inventory。收到 inventory 的一方从同一 durable
snapshot 建 plan 并发送 batch；接收方只有在完整 token 已验证、持久化和必要的最终 projection
成功后才返回 ack。发送方只接受与 outstanding batch 的 accepted cursor 和 `done` 精确匹配的
ack；丢 ack 时重发原始 batch JSON，依赖 assembler 的 exact-replay idempotency 返回同一 durable
ack。transport adapter 只从完成 Peer handshake 的 channel 读取远端 membership token，不能由
调用者另传一个 token 替换认证身份。

完整 inbound batch 的 durable ack 成功发出，或 outbound plan 的 `done` ack 被严格校验后，设备才把
该认证成员的 `peer_id`、其确认的 version vector 和成功时间写入加密的本地 Peer store。该状态
不含 membership/config/epoch token，也不上 wire 之外的服务。Settings 中“待同步”操作数定义为：
当前本机 durable vector 中，尚未被任一**当前有效远端成员**的确认向量覆盖的 counter 总和；多个
有效成员的确认向量按 actor 取最大值合并。成员过期或 deny-wins 撤销后，其旧确认立即不再计入，
因此 UI 不会把仅存在于失去信任设备上的副本误报为已有可用备份。没有在线 Peer 时，操作继续
保存在本机并显示 pending，不上传到隐藏的中心存储。

Peer DataChannel 复用 Stage 1 的四步 handshake、ECDH traffic key、record 和 fragment
codec，只替换 `HandshakeAuthenticator`。Relay account authenticator 的 proof 继续是 32-byte
HMAC；Peer membership authenticator 对相同 transcript 使用设备 P-256 identity 产生 64-byte
low-S P1363 signature，并验证 client/host membership 均锚定到同一 genesis。genesis hash
和两份 membership token 的 hash 共同进入 ephemeral-ECDH traffic-key binding。identity
private key 只签名、不跨算法复用为 ECDH，兼容 WebCrypto non-exportable ECDSA key。proof
长度由 authenticator 声明，所以 Relay v0.6 的现有 wire bytes 不变。

### Quick Tunnel Peer signaling

Quick Tunnel 本机 gateway 只在 `GET /peer/v1/connect` 接受 WebSocket，并要求
`Sec-WebSocket-Protocol: atterm-peer-v1`。Cloudflare 可见的首条 text message 是有界路由信封：

```json
{
  "v": 1,
  "kind": "open",
  "attempt_id": "<uuid>",
  "ticket": "<base64 32 bytes>",
  "session_id": "<uuid>",
  "client_peer_id": "<peer id>",
  "client_instance_id": "<instance id>"
}
```

`ticket` 每次连接随机生成；信封不携带 genesis、membership token、SDP/ICE、终端或配置明文。
主机通过 `client_peer_id` 查找本地 canonical active membership，并独立验证 genesis、
deny-wins revocation、client/host membership、host identity 归属、session scope、本机 session
归属，以及 effective permission 不超过 client grant、host grant 和桌面 owner policy 三者的最小值。
通过后返回：

```json
{
  "v": 1,
  "kind": "authorized",
  "user_id": "<space id>",
  "host_id": "<host peer id>",
  "permission": 2,
  "expires_at_unix_millis": 1790000000000
}
```

这些字段与 `attempt_id`、`ticket`、`session_id`、`client_instance_id` 一起构成现有
Peer membership handshake 的 exact `Authorization`；authorization lifetime 是 30 秒。随后双方在
同一 WebSocket 上交换四步 binary handshake，派生方向隔离的 XChaCha20-Poly1305 record keys。
handshake 完成后的初始 signaling 模式只接受 encrypted `SIGNAL` / `SIGNAL_FRAGMENT` record；
text message、AEAD 篡改、sequence gap 或上层校验失败都会关闭 channel。

WebRTC signaling 的 `Signal.type` 允许 `offer`、`answer`、`ice_candidate`、`ice_end`。每个方向
最多一个 offer、一个 answer、64 个 ICE candidate 和一个 `ice_end`；offer/answer payload 最大
32 KiB，candidate 最大 8 KiB，`ice_end.payload` 必须为空。单个序列化 signal 最大 64 KiB。
客户端只有在用户允许 fallback 时才可发送一次空 payload 的 `wss_fallback`；主机确认支持后先
回复一次空 payload 的 `wss_ready`。`wss_fallback` 只允许 client→host，`wss_ready` 只允许
host→client。两端利用 WebSocket 的有序交付，在 ready record 收发完成后才切换模式，避免主机
replay 早于客户端切换而被误判成 signaling record。

完成 signaling handshake 后，其 exact `Authorization` 与 Peer authenticator 可以驱动现有 Pion
host/client attempt。offer/answer 只经上述 encrypted signal record 交换；Pion DataChannel 建立后
仍执行自己的一次四步 membership handshake，使用新的 ephemeral ECDH 和独立 record counters，
不复用 signaling channel 的 traffic key/nonce。`SIGNAL` / `SIGNAL_FRAGMENT` 在 Pion DataChannel
上必须拒绝。未启用 fallback 时，Pion 失败/关闭会关闭 signaling channel；启用 fallback 后保留
该 channel，等待客户端的显式 fallback 决策。

收到 `wss_ready` 后，同一 WebSocket 切换到 WSS data 模式，复用首次 signaling membership
handshake 已派生的 traffic key、nonce counter 和 exact remote membership，不再执行第二次
handshake。此后只接受 `FRAME`..`CONFIG_FRAGMENT`（`0x01..0x0a`），任何 signaling record
或未知类型都 fail closed。terminal/config 的 fragmentation 与 reassembly 规则和 DataChannel
完全相同；配置授权仍绑定 handshake 的 exact membership。WSS writer 使用有界队列，在每个
encrypted record 边界按 `input/control > terminal output > config sync` 调度；同一 fragmented
logical message 内保持同级连续，允许更高优先级 record 在 terminal/config 两套独立 reassembler
之间抢占。Cloudflare 只能观察连接元数据、时序和 ciphertext size，不能读取 record plaintext。

Desktop 只在显式调用 `StartPeerQuickTunnel` 后启动 `cloudflared`，不会随 app 启动自动发布
公网入口。gateway 挂载上述 Peer handler；DataChannel 第二次 membership handshake 完成后，
Desktop 以 `WithoutAutoDrive` 订阅请求的本地 session，转发初始 scrollback/实时
`OUT`/`META`/`CLOSE`，并在 replay end 后发送 `DIRECT_READY`。账户无关 record layer 已提供
应用加密，所以 terminal frame 不再叠加 Relay `account_key` 信封；入站 frame 也不走 Relay
inner-open。Desktop 在 attach、周期授权刷新和每次 config 消息边界重验当前 membership、撤销
状态与 session scope；`CLAIM_DRIVER`、`IN` 和 `RESIZE` 的热路径继续检查实时 owner policy、
effective permission 与 driver 身份，不在每个键入帧访问磁盘/keyring。连接期间降权或撤销会
关闭 attempt。配置 anti-entropy 同时绑定到同一 authenticated membership，但使用独立 config
callback，绝不创建第二个 terminal subscriber。

Quick Tunnel WSS 与 Rendezvous Pion host 共用进程内的 authenticated Peer route lease。租约键为
`(remote_peer_id, session_id, client_instance_id)`：同一 Peer 的不同设备或 pane 可以各自 attach，
同一逻辑客户端的新 route 则必须先重新验证 exact active membership、session scope 和 permission，
完成 scrollback catch-up 后才原子替换旧 subscriber。若旧 subscriber 是 driver，新的 subscriber
继承同一 `driver_client_id` / `driver_client_name`，subscriber count 不经过 0，也不触发 lazy uplink
的假停止/重启。安装新租约后，旧 route 立即失去 `IN`、`RESIZE`、`CLAIM_DRIVER` 和 config sync
权限；旧连接迟到的 close 只能释放自己，不能删除新租约。并发的陈旧候选不能覆盖已经获胜的
route。该规则只约束 host 本地 attachment，不增加 frame/record 类型。

Peer client 默认仍先尝试 Rendezvous direct。Direct 遇到
`signal_endpoint_unavailable` / `webrtc_unavailable` / `timeout` / `host_unavailable` / `ice_failed` /
`direct_disconnected` / `transport_error` 时，会用最多 1 秒向 Go 查询 token-free route capability；
只有目标 session 仍在 authenticated catalog 且对应 signed Quick Tunnel hint 当前有效，才自动以
同一 `client_instance_id` 和 last committed OUT seq 建立 Quick Tunnel。查询和 handover 期间冻结
`IN`/`RESIZE`/`CLAIM_DRIVER`，旧 generation callback 全部丢弃，Quick Tunnel replay 按 OUT seq 去重
后才恢复写入。`authentication_failed` / `protocol_error` / `backpressure` 等安全或完整性错误不得通过
换路降级；一旦回退到 Quick Tunnel，后续断线只在该 route 上退避重试，不自动反向切回 Direct，
避免 route flap。Peer-only handover 不引入 Relay credential、Relay subscriber 或 `account_key`。

`CreatePeerConnectionBundle(invitation)` 只接受本地加密账本中仍开放、未消费、未撤销、未过期且
由当前 active local membership 签发的 invitation；首次加入的 bundle 必须发布当前 Quick Tunnel
URL，也可以附带 Rendezvous hint。空 invitation 生成 member reconnect bundle，可发布 Quick
Tunnel + Rendezvous 或仅 Rendezvous route。route/presence 轮换只生成新的 bundle id/route，
genesis、ticket 和 durable membership 不变。`StopPeerQuickTunnel` 关闭 active Pion/subscriber、
loopback gateway 和 `cloudflared`，但不删除 Peer trust，可再次显式启动。WSS fallback 与
Rendezvous Pion route 都复用上述同一个 Session attach、权限热检查和 config anti-entropy 路径；
config 仍不创建第二个 terminal subscriber。Desktop Settings 已提供 join/bootstrap 确认、独立于
Relay 登录设备的 Peer member directory、不可逆成员撤销，以及 Quick Tunnel start/stop、member
reconnect bundle 复制入口和 official/custom/disabled Rendezvous 运维状态；Web/iOS Peer client
接入、最终用户侧 Rendezvous session discovery/attach 入口与
fallback consent 仍未实现。

### Rendezvous v1 discovery and signaling

Rendezvous 是独立的无账户、无数据库服务，只提供短期 presence、discovery 和 opaque
signaling mailbox。外部入口是 `WSS /v1/connect`，必须协商
`Sec-WebSocket-Protocol: atterm-rendezvous-v1`。它不接受 Relay token、membership token、
config op 或 terminal `proto.Frame`，也不提供 TURN/WSS data fallback。生产二进制默认要求
TLS 证书与显式 Origin allowlist；TLS 终止反代模式只允许监听 loopback，明文公网监听只能通过
显式 `--dev-insecure` 开启。

连接建立后服务端首先发送一次 10 秒有效的 challenge：

```json
{"v":1,"kind":"challenge","challenge":"<base64url 32 bytes>","expires_at":1800000010}
```

客户端为每次连接生成一个临时 P-256 challenge identity 并回复（不复用 durable Peer signing
identity，避免 Rendezvous 通过 public key 跨 presence 时隙关联同一设备）：

```json
{
  "v": 1,
  "kind": "register",
  "topic": "<base64url 32 bytes>",
  "presence_id": "<base64url 32 bytes>",
  "role": "host",
  "public_key": "<base64url 65-byte SEC1 P-256 key>",
  "signature": "<base64url 64-byte low-S P1363 signature>"
}
```

`role` 只允许 `host` / `member`。签名 plaintext 按以下顺序拼接，每段前置一个
big-endian u32 长度：

```text
"atterm-rendezvous-register-v1" || challenge(32B) || topic(32B) ||
presence_id(32B) || role
```

服务验证 challenge freshness、固定长度、canonical raw base64url、公钥合法性和签名；它不保留
public key / peer id，也不把签名成功解释成 Space membership。`topic` 和 `presence_id` 都必须由
客户端从 Space material 派生为不可关联的高熵值；后续 Peer transport handshake 仍须独立验证
双方 membership、撤销状态和 capability。

客户端从当前 `sync` epoch key 派生 routing coordinates。`LP(x)` 表示 `big-endian u32(len(x))
|| x`，`epoch` / `slot` 都编码为 big-endian u64，`slot = floor(unix_seconds / 900)`：

```text
topic = HMAC-SHA256(K_sync_epoch,
  LP("atterm-peer-rendezvous-topic-v1") || LP(space_id) || LP(epoch))

presence_id = HMAC-SHA256(K_sync_epoch,
  LP("atterm-peer-rendezvous-presence-v1") || LP(space_id) || LP(epoch) ||
  LP(peer_id) || LP(slot))
```

两者在 wire 上都使用 raw base64url 32-byte digest。`topic` 在一个 sync epoch 内稳定，让同一
Space 的在线成员能相遇；`presence_id` 每 15 分钟轮换。解析 presence 时只枚举当前 active
membership，并接受本地 current/previous/next 三个 slot 以容忍时钟偏差。成员撤销会先旋转
sync epoch key，因此旧 topic/presence 立即不能被剩余成员解析。派生值不写入 Peer store、Relay
prefs 或配置副本。

注册成功返回当前已激活的同 topic presence：

```json
{"v":1,"kind":"registered","presence":[{"presence_id":"<opaque id>","role":"member"}]}
```

之后 presence 上下线以 `{"v":1,"kind":"presence","event":"online|offline",...}` 通知。
这些事件只代表当前进程观察到的可达性，不是成员目录或授权真相源；进程重启会立即丢失全部
presence。客户端也只把 snapshot/online/offline 保存到内存 reachability directory；连接关闭、
时隙轮换或本地 expiry 清理只删除可达路由，不删除 membership、grant、revocation、epoch key
或 config operation。

配置同步拨号以 active member directory 为白名单，再把可达路由与本地持久化的
`ConfigSyncPeers[peer_id].Acknowledged` 合并。active member 总数不超过 8 时选择全部可达远端；
更大的 Space 每轮默认最多选择 4 个，优先选择缺少本地 operation 更多的 peer，再选择从未同步或
更久未同步的 peer。同优先级 peer 按 15 分钟 slot 确定性轮换，避免形成永久 hub。这个结果只是
现有 authenticated Peer config channel 的拨号计划，不能绕过第二次 membership handshake。

发送方用 16-byte message id 投递 ciphertext：

```json
{
  "v": 1,
  "kind": "publish",
  "message_id": "<base64url 16 bytes>",
  "to": "<target presence_id>",
  "payload": "<base64url ciphertext, decoded max 64 KiB>"
}
```

在线目标收到 `kind=signal`，包含原 `message_id`、opaque `from`、`payload` 和
`stored_at`；发送方收到 `kind=ack`，`state` 为 `delivered` 或 `queued`。`payload` 必须由 Peer
客户端在投递前完成端到端加密，服务只验证 canonical base64url 和大小，不解析 SDP/ICE。
离线 mailbox 与 `(topic, from, message_id)` 去重记录都只保留 120 秒；相同 id 的重试返回第一次
结果但不重复投递。进程重启、容量淘汰或 TTL 到期都不会持久化，客户端必须把信令视为可重试的
短期消息。

每对 active Peer 用双方 membership 中已签名的 wrapping key 做静态 P-256 ECDH，再从当前
`sync` epoch 派生独立信令密钥。令 `peer_lo` / `peer_hi` 为两个 `peer_id` 的字典序排序结果：

```text
K_signal = HKDF-SHA256(
  IKM = P-256-ECDH(local_wrapping_private, remote_wrapping_public),
  salt = K_sync_epoch,
  info = LP("atterm-peer-rendezvous-signal-key-v1") || LP(space_id) ||
         LP(peer_lo) || LP(peer_hi) || LP(u64be(epoch)),
  length = 32)
```

信令信封为 `version(0x01) || nonce(24B) || XChaCha20-Poly1305 ciphertext+tag`。每条消息的
AAD 为：

```text
LP("atterm-peer-rendezvous-signal-aad-v1") || LP(topic) ||
LP(from_presence_id) || LP(to_presence_id) || LP(message_id)
```

因此同 Space 的第三个成员即使持有 epoch key，也不能解开另外两个成员的 SDP/ICE；Rendezvous
也不能把 ciphertext 替换到另一个 topic、方向或 message id。客户端对
`(from_presence_id, message_id)` 使用 2048 条有界 replay window。

解密后的 route message 有八种：`catalog_request`、`catalog_response`、
`catalog_request_routes`、`catalog_response_routes`、`open`、`authorized`、`signal`、`error`。
`open` 携带短期 attempt ticket、目标 session 和 client identity；`authorized`
只返回该 attempt 的有效期与 effective permission；`signal` 携带 Pion offer/answer/ICE；`error`
只返回稳定类别。上述字段全部位于加密信封内。Genesis、membership token、
revocation/config operation 和 terminal frame 从不发给 Rendezvous；双方仍必须在 Pion
DataChannel 上完成现有
`PeerMembershipAuthenticator` 握手，握手通过后才允许创建 terminal subscriber 和
`peerConfigChannel`。

配置 anti-entropy 使用保留的虚拟 session id
`ffffffff-ffff-4fff-bfff-ffffffffffff` 建立 config-only `open`。该 id 只作为握手 transcript
namespace，不对应 registry 中的 PTY，也不受 membership 的 `allowed_session_ids` 限制；host 仍须
验证双方是当前 active member，并把握手权限固定为 `view`。config-only attempt 只接受
`CONFIG_INVENTORY/BATCH/ACK` 记录，收到 terminal record 立即关闭，且整个生命周期不调用
`Session.Subscribe`。旧客户端会把该 id 当不存在的 terminal session 并安全拒绝。

`catalog_request` 用独立 request UUID 和从 0 开始的 offset 请求目标 host 当前可见的 session。
host 每次请求都重新读取 signed membership/revocation、双方 session scope、双方 permission ceiling
和 owner `remote_permission`，只在 `catalog_response` 返回交集内的 metadata 与 effective
permission。响应每页最多 16 条、总计最多 512 条；单个 UTF-8 metadata 字段最多 1024 bytes。
每页都绑定 request UUID、请求 offset、目标 Peer route 和 pairwise signal envelope；Rendezvous
只能看到 64 KiB 以内的 ciphertext。catalog 不创建 Pion attempt、terminal subscriber 或 config
channel；v1 `catalog_response` 不携带 `ConnectionBundle`。只有用户选择某条 session 后才发送
`open` 并建立现有 Pion/DataChannel 路径，host 在 attach 时再次执行完整授权检查。

`catalog_request_routes` 是可选的向后兼容能力探测，请求字段与 `catalog_request` 完全相同，
使只认识 v1 字段的旧 host 仍能严格解码后忽略未知 kind。新 client 对未知 Peer 最多等待 750 ms；
收到 `catalog_response_routes` 后在当前 `Route` 生命周期缓存支持状态，未收到则缓存为 v1-only
并重新请求 `catalog_request`。已确认支持的 Peer 若发生瞬时 service/offline 失败，可回退 v1 session
目录但不降级能力缓存；认证或消息结构错误不得通过 v1 绕过。

`catalog_response_routes` 仅在 offset 0 的第一页增加 `connection_bundle`，后续页必须省略。host
每次第一页请求都重新生成由当前 active local membership 签名的无 ticket member reconnect bundle；
它可包含当前 Quick Tunnel + Rendezvous route，也可只含 Rendezvous route。该字段与 session metadata
一起位于 pairwise XChaCha20-Poly1305 信封内，Rendezvous 只能看到不超过 64 KiB 的 ciphertext。
client 在接受该 host 的目录前，必须独立验证 bundle 属于当前 genesis、issuer 精确 membership 仍在
deny-wins active set，且 issuer Peer ID 等于被请求的 Peer；通过后只原子替换该 issuer 的进程内
Quick Tunnel hint，不持久化 URL/token，也不暴露给 renderer。空 bundle 只可能来自 v1 fallback，
此时保留现有 hint 直到自身 expiry。此分发不创建 Pion attempt、terminal subscriber 或 config
channel，也不因 bundle 到达而主动切换 terminal route；只有之后发生上述可恢复 Direct 故障时，
client 才重新查询 capability 并执行自动 Quick Tunnel handover。用户仍可在失败界面显式选择重试。

发送 `open` 后，Rendezvous ACK 为 `queued` 映射 `peer offline`；连接、写入、ACK 超时或 host
容量问题映射 `service unavailable`；Pion ICE/transport 建链失败映射 `ICE failed`；membership、
session scope、permission 或 attempt 校验失败映射 `authentication failed`。这些类别用于调用方
决定是否退避或尝试已启用的 Quick Tunnel；Rendezvous 本身不提供 data fallback。

稳定错误码为 `unauthorized`、`invalid_message`、`message_too_large`、
`presence_conflict`、`topic_capacity`、`mailbox_capacity`、`rate_limited` 和
`server_capacity`。默认上限是全局 1024 连接、每 IP 32 连接、每 topic 32 presence、每 topic
128 条 mailbox、全局 4096 条 mailbox、全局 8192 条近期去重记录，以及每 IP 每分钟 240 次
publish。慢目标的 writer queue 满时消息转入相同的有界 mailbox，不允许无界阻塞。

`GET /healthz` 只返回 `status` 与 `protocol_version`；`GET /metrics` 只返回无 label 的连接、
presence、mailbox、accept/reject/forward/queue/expire 计数。两者以及服务日志都不得输出 topic、
presence id、public key、message id 或 payload。实现位于 `internal/rendezvous/`，独立入口是
`cmd/atterm-rendezvous/`。

客户端配置模式是 `disabled | official | custom`。`official` 当前解析为
`https://rendezvous.atterm.dev`，`custom` 必须是同样只含 scheme/host/port 的 HTTPS/WSS origin；
HTTP/WS 只允许测试工具显式选择 loopback development。服务域名不进入 registration crypto
transcript，官方和自建部署使用完全相同的签名与 wire 契约。

STUN 配置模式是 `default | custom | disabled`。默认值为
`stun:stun.cloudflare.com:3478`；custom 只接受最多 8 个无 credentials/query 的
`stun:`/`stuns:` URL。当前不接受 `turn:`/`turns:`，也不把 Rendezvous 描述为 TURN。STUN
服务可观察来源 IP/时序，WebRTC 对端会获得建连所需的 candidate 地址；这些披露在启用前必须
由客户端 UI 明示。

Browser/WebView 在生成 Peer identity 或创建 `RTCPeerConnection` 前必须依次验证 secure
context、`crypto.subtle` 与 `RTCPeerConnection`，并映射成稳定状态
`insecure_context`、`webcrypto_unavailable`、`webrtc_unavailable`。独立黑盒验收入口
`cmd/atterm-rendezvous-contract` 只依赖 service origin/Origin，官方和自建实例必须通过同一套
health、challenge、presence、delivery、mailbox、dedupe、size limit 与 metrics privacy 检查。

Desktop 启用 Rendezvous 且已有 Peer Space 时会注册 host presence；单次注册超时为 10 秒，失败
按 500 ms 到 8 s 指数退避，连接断开或进入下一个 15 分钟 presence slot 时重新注册。Settings
运行状态使用 `disabled | waiting | connecting | online | error`，错误只暴露
`invalid_config | registration_timeout | authentication_failed | service_unavailable |
registration_failed` 稳定码。诊断导出只包含 service origin 与聚合状态，不包含 topic、presence id、
Peer id、SDP/ICE 或 payload。成员重连的
`ConnectionBundle` 可同时携带 Quick Tunnel 与 Rendezvous，也可以只携带 Rendezvous；首次邀请
核销仍必须包含可用 Quick Tunnel route，Rendezvous 不承担 bootstrap secret 交换。registration
在线期间 Desktop 每 3 秒把当前 host presence 与 signed membership、durable acknowledgement
vector 重新规划，按小 Space 全连接/大 Space bounded fanout 维护 config-only Pion 通道；presence
离线、fanout 轮换或 registration 结束会关闭不再需要的 outbound 通道。

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
