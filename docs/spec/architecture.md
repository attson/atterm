# 架构规范

## 一句话总览

atterm 是 **本地桌面终端**（Wails app）+ **可选中央 relay**（独立 server）+ **任意 web/桌面客户端**。三者通过统一的二进制 WebSocket 帧协议通信。本地体验永远独立、可用；远程能力是叠加的，按需启动。

## 组件全图

```
┌─────────────── 桌面 app（AT Term，单进程）──────────────┐
│                                                                │
│  ┌─ 前端 webview（Vue3 + xterm.js）─┐                          │
│  │  TabBar · TerminalView · 设置 · │                          │
│  │  cast 面板（远程发现）           │                          │
│  └─────────────────┬─────────────────┘                          │
│                    │ WS  ws://127.0.0.1:<rand>                  │
│                    ▼                                            │
│  ┌── 本地 mini relay（internal/relay.Server）─────┐             │
│  │  /agent /uplink /client /api/sessions          │             │
│  │  + AdoptSession(PtyHost) 同进程注册            │             │
│  └────────────────┬───────────────────────────────┘             │
│                   │ in-process channels                         │
│                   ▼                                             │
│  ┌── ptyhost.Host 集合 ─────────────┐                          │
│  │  本地 PTY 子进程（bash/zsh/...）  │                          │
│  └──────────────────────────────────┘                          │
│                                                                 │
│  ┌── desktop/uplink.go（可选）──────┐                          │
│  │  control conn → ws://relay/uplink│                          │
│  │  ANNOUNCE 元数据（30s 心跳 + 事件）│                         │
│  │  STREAM_REQ → SubscribeLocal     │                          │
│  │  STREAM_STOP → UnsubscribeLocal  │                          │
│  └──────────────────────────────────┘                          │
└─────────────────────────────────────────────────────────────────┘
                                │
                                │ WSS
                                ▼
┌──────────────── atterm-relay（独立 Go 服务）──────────────────┐
│                                                                │
│  /agent — 旧式 agent（CLI wrapper）注册 session                │
│  /uplink — 桌面 app 控制连，收 ANNOUNCE 维护 mirror sessions  │
│  /client — web/桌面 客户端 attach（ATTACH/IN/RESIZE/收 OUT）  │
│  /api/sessions / /api/version — JSON API                      │
│  CSP/security headers + IP/token rate/connection limits        │
│                                                                │
│  SessionRegistry：                                             │
│   - local sessions（来自 /agent，字节流持续）                   │
│   - mirror sessions（来自 /uplink，subscribe→上传，0 sub→停）  │
└────────────────────────────────────────────────────────────────┘
                                ▲
                                │ ws/http
                                │
       浏览器、其他桌面 app（attach 后浏览/操作任意会话）
```

## 组件职责矩阵

| 组件 | 路径 | 职责 | 不做 |
|------|------|------|------|
| `proto` | `internal/proto/` | 帧协议常量 + 编解码 | 任何业务逻辑 |
| `ringbuf` | `internal/ringbuf/` | 字节预算环形缓冲 | 不知道帧类型 |
| `session` | `internal/session/` | session 数据模型、订阅 fan-out、lifecycle 钩子 | 不开 WS 不读 PTY |
| `relay` | `internal/relay/` | HTTP/WS 服务，处理 4 种连接（agent/uplink/client/sessions） | 不写 PTY、不持久化 |
| `ptyhost` | `internal/ptyhost/` | 纯 PTY 包装，无本地 TTY 副作用 | 不知道 relay 协议 |
| `agent` | `internal/agent/` | CLI wrapper：`ptyhost` + 本地 stdin/stdout 桥接 + `relay` 客户端 | 不被桌面端用 |
| `hostid` | `internal/hostid/` | 机器持久 UUID | 不知道 session |
| `desktop/relay_host.go` | desktop | 启 mini relay，spawn PTY，AdoptSession | 不连远程 |
| `desktop/uplink.go` | desktop | 远程 relay 客户端（lazy 协议） | 不直接拥有 PTY |
| `desktop/updater.go` | desktop | GitHub Releases 自动更新 state machine（check / download / 调用 platform install helper） | 不动 PTY、不动 relay |
| `desktop/scripts/install-{darwin,linux,windows}` | desktop | 平台 install helper，等父 PID 退出后替换 binary 并重启 | 不发网络请求 |
| `desktop/app.go` | desktop | Wails bindings (Session / Relay / Update) | 不实现协议 |
| `web/` | web | vanilla 浏览器/PWA client，使用同源 vendored xterm 资源直连 relay | 不从 CDN 加载 script/style，不持久化除 token 以外的会话状态 |

## 三种核心数据流

### 流 1：本地新建会话

```
前端点 + → app.NewSession(req)
       → relayHost.NewSession(req)
       → ptyhost.Open(bash)
       → relay.Server.AdoptSession(id, info, pty)
              ├─ session.New(id, info) 注册到 registry
              ├─ goroutine A：read PTY → session.PushOut(seq, data)
              └─ goroutine B：select session.Inbound() → 写 PTY
       → 前端 hash → ATTACH WS → 收 scrollback + 实时 OUT
```

### 流 2：用户在浏览器 attach 桌面 app 的会话（lazy 路径）

```
桌面 app 启动时 uplink.Run 已建立控制连接
桌面 NewSession 后 → notifyChange → uplink 发新 ANNOUNCE → relay 建 mirror session

浏览器打开 http://relay/?token=… → ATTACH(sid)
       ↓
relay client_conn → mirror.Subscribe()  ← 触发 lifecycle.onFirstSubscribe
       ↓
uplink_conn 通过控制连发 STREAM_REQUEST(sid)
       ↓
桌面 uplink 收 STREAM_REQUEST → SubscribeLocal(sid) 在本地 mini relay 订阅
       ↓
本地 PTY 输出 → 本地 session.PushOut → fan-out 给 local subscriber + uplink subscriber
                                                              ↓
                                              uplink forwarder → WS → 远程 relay
                                                              ↓
                                              uplink_conn → mirror session.PushOut
                                                              ↓
                                                          fan-out → 浏览器
```

浏览器输入：`IN frame → relay client_conn → mirror.SendInbound → mirror.Inbound() chan → uplink_conn writer → WS → 桌面 uplink → host.SendLocalInbound → 本地 session.SendInbound → 本地 PTY 写入`。

最后一个浏览器 detach 时：`removeSubscriber → lifecycle.onLastUnsubscribe → uplink_conn 发 STREAM_STOP → 桌面 uplink 取消 forwarder`。

### 流 3：本机两个桌面 app 互相 attach

两个 app 都连同一 relay。各自 ANNOUNCE。一方点 cast 面板选另一方的 session：

```
app2 前端 → 拉 remote relay /api/sessions → 看到 app1 的 mirror session
       → 点击卡片 openRemote(sid) → 加 tab → TerminalView 用 remoteEndpoint attach
       → ATTACH 帧到 remote relay → 同流 2 后半段
```

注意：app1 的 host_id 和 app2 相同（同机器），但 session_id 独立，去重按 session_id（详见 protocol.md §去重）。

## 会话生命周期

```
创建：NewSession → ptyhost.Open → AdoptSession → registry.Add → 通知 uplink
活跃：PTY 输出 → session.PushOut → fan-out（含 ringbuf 持久化）
关闭：
  本地原因（用户关 tab）：CloseSession → ptyhost.Close → PTY EOF → AdoptSession
                            读循环收 EOF → broadcast CLOSE → registry.Remove
  PTY 自然退出（exit）：同上
  uplink 断线：远程 mirror session 被 uplink_conn cleanup 移除（远程 attacher 收 CLOSE）
  app 退出：relayHost.Stop → 清所有 PTY + cancel uplink ctx
```

session 保留期：**仅 PTY 进程活动期间**。退出即丢弃 ringbuf。**不持久化到磁盘**（个人自托管定位）。

## 会话标识

每个 session 有三个标识：

| 字段 | 来源 | 用途 |
|------|------|------|
| `session_id` (UUIDv4) | `uuid.New()` 创建时生成 | 唯一身份；attach、去重、路由 |
| `host_id` | `~/.config/atterm/host_id` 或 `ATTERM_HOST_ID` | 机器粒度的归属（人类视角） |
| `host` / `user` | `os.Hostname()` / `user.Current()` | 人类可读展示 |

**关键约束**：
- 多实例同机：`host_id` 相同，`session_id` 独立 → 去重必须按 `session_id`
- session 在迁移/重启间不持久（PTY 退出即销毁）

## phase 完成度（截至当前）

- ✅ Phase 0：协议骨架，命令行 wrapper + relay + vanilla web
- ✅ Phase 1：Wails 桌面壳，多 tab，自动建会话，cwd-driven 标题
- ✅ Phase 1.5：lazy 远程镜像（ANNOUNCE/STREAM_REQUEST/STOP），GUI 设置入口，cast 面板
- ✅ Phase 2：每 tab 1/2/4 pane 分屏（layout pure fns + iTerm-style ⌘N/⌘⇧N 快捷键）；自动更新（GitHub Releases，Ed25519/SHA256 验签，dev 短路，用户手动 force install）
- ⬜ Phase 3：web 端 UI 升级（按 host 分组、host alias、active 指示）；主题、字体、配置 DSL
- ⬜ Phase 4：用户系统、TLS 自动化、平台 codesign/notarization、PWA 移动端

## 桌面端架构细节

```
desktop/main.go            创建 *App，wails.Run；OnStartup→app.startup，OnShutdown→app.shutdown
                           macOS-only: 自定义 NSMenu（保留 App + Edit role，不要 Window
                           submenu，让 ⌘W 留给前端）
                           var Version / UpdateVerifyPublicKey (ldflags 注入)
desktop/app.go             A 持有 *relayHost、*uplink、*configStore、*Updater
                           暴露 13 个 binding：6 session/relay + 6 update +
                           CloseSession sync 注销（cleanup() 同步调，notifyChange 立即
                           推 ANNOUNCE，不等 pty.Wait）
desktop/relay_host.go      启动 relay.NewServer + net.Listen("tcp","127.0.0.1:0")
                           NewSession 起 ptyhost、AdoptSession 到本地 server
                           watchCwd goroutine 每秒 readlink /proc/<pid>/cwd
                           Subscribe/UnsubscribeLocal/SendLocalInbound 给 uplink 用
desktop/uplink.go          连远程 /uplink，发 ANNOUNCE
                           收 STREAM_REQ → SubscribeLocal + 启 forwarder goroutine
                           收 STREAM_STOP → cancel forwarder
                           IN/RESIZE → host.SendLocalInbound
                           所有 conn.Write 通过单一 out channel + writer goroutine 串行化
desktop/relay_security.go  校验远程 relay URL；默认只允许非 loopback 使用 wss://，
                           用户在 Settings 打开 insecure mode 后才允许远程 ws://
desktop/updater.go         自动更新 state machine
                           Start(ctx)：boot 后 2s 跑首次 Check，之后 24h ticker
                           Check(force)：拉 api.github.com/repos/<repo>/releases/latest
                                          1h 缓存（force=true 旁路）；semver.Compare；
                                          prerelease 跳过；按 GOOS/GOARCH 选 asset +
                                          SHA256SUMS/SHA256SUMS.sig
                           Download(ctx)：流式下载到 ${UserCacheDir}/atterm/updates/
                                          <ver>-<asset>.partial → 校验 size →
                                          Ed25519 验签 SHA256SUMS → 校验 asset hash →
                                          atomic rename
                           InstallAndQuit()：取出 go:embed 的 install helper 到 cache，
                                              spawn detached（bash on POSIX，
                                              powershell on Windows），传 PID 让它等待
                                              我们退出，然后 wailsruntime.Quit
                           dev/empty Version 时整个 path 短路
desktop/scripts/           install-darwin.sh / install-linux.sh / install-windows.ps1
                           等 PID 退出（最多 30s, 0.5s 轮询）→ 解压到 tmp →
                           原子替换 → 平台特异收尾（macOS `xattr -dr quarantine`，
                           Linux `chmod +x`，Windows 重试 Move-Item 应对 file-lock 滞后）
                           → 重启新副本 → 清理临时文件
desktop/config.go          ~/.config/atterm/config.json 持久化，atomic write-temp-rename
                           包含 RelayURL/RelayToken/AllowInsecureRelay +
                           AutoCheckUpdates *bool（nil = default true）+
                           LastCheckAt + SkipVersion（v0 reserved）
```

## Relay 启动安全

`cmd/atterm-relay` 是生产入口，默认 fail-closed：

- `ATTERM_TOKEN` 为空时自动生成 32 字节随机 token，并在日志打印一次访问 URL；
- 公网监听时拒绝弱 token（`dev` 或长度 <16），除非显式 `--dev-insecure`；
- 公网监听未设置 `--origins` / `ATTERM_ORIGINS` 时拒绝启动，除非显式 `--dev-insecure`；
- 公网监听启用 `/admin/` 时拒绝弱 admin token（`admin`、`dev` 或长度 <16）；
- 默认返回 CSP/security headers，`web/` 只允许同源 script/style；xterm 静态资源放在 `web/vendor/`；
- 对 HTTP 请求和 WS upgrade 先按远端 IP 限流，鉴权成功后再按远端 IP + token hash 限流，并限制同一 key 的活跃 WS 连接数；
- 支持只读 token（`ATTERM_READ_ONLY_TOKENS` / `--read-only-tokens`）：可 list/attach/看输出，但不能输入、resize、粘贴图片，也不能注册 agent/uplink；
- 支持 owner 发布的 `remote_permission`（view/control/full），relay 和 desktop uplink 双重强制执行；
- 可选 `--config` + `--admin-token` 启用持久化 admin 配置和 `/admin/`，但主 write token 永远不写入该配置；
- `--dev-insecure` 只用于开发/可信内网，会打印明文传输/弱鉴权警告。

`internal/relay.NewServer(relay.Config{Token:""})` 作为库仍保留“不鉴权”
语义，供本地 mini relay 或测试使用；不要把它等同于 `cmd/atterm-relay`
的生产默认行为。

## 远程权限与 admin 配置

远程权限由拥有 PTY 的 desktop app 决定。Settings 中的默认权限会写入
`desktop/config.go`，`desktop/uplink.go` 在 `ANNOUNCE` 的每个 `SessionInfo`
里发布 `remote_permission`。远端 relay 计算 token scope 与 owner 权限的交集：
只读 token 始终只能 view；write token 也不能超过 owner 发布的 view/control/full。

relay admin 配置只服务运维场景：调整 rate limit、连接数和持久化只读 token。
admin-created read-only token 只显示一次，配置文件中仅保存 `sha256:<base64url>`
hash。主 write token 仍由 `ATTERM_TOKEN` / flag / 启动自动生成提供，不能通过
admin 页面读取、写入或轮换。

## 前端架构细节

```
desktop/frontend/src/
├── main.ts                Vue 入口
├── App.vue                双源轮询：local + remote /api/sessions；
                           tabs 模型：每 tab 持 layout (single/vertical/horizontal/grid2x2)
                           + panes[]；boot 时挂 measure probe（FitAddon proposeDimensions）
                           预测 PTY cols/rows，避免开局 SIGWINCH；⚙ 按钮 dot badge
                           跟着 update state.available || ready
├── components/
│   ├── TabBar.vue         tab 编号 + layout icon + active pane session 标题
│   ├── TerminalView.vue   xterm.js + FitAddon；多实例 v-show 切换；
│                          rAF 兜底 fit 处理 layout 时序竞态；focused prop 描边；
│                          expectedCols/Rows = SessionInfo.cols/rows，相同时跳过
│                          初始 RESIZE（远端 attach 不打扰）
│   ├── PaneGrid.vue       CSS Grid 渲染 1/2/4 cell；远程 pane 右上角 cast badge
│                          + close-pane × 同行 flex 布局
│   ├── SessionPickerDialog.vue ⌘⌥N 触发：local + remote 已有 session 选一个进 pane
│   ├── SettingsDialog.vue relay 配置 + Updates 区（current / latest / 进度 /
│                          autocheck toggle / release notes / 三个按钮）
│   ├── ConfirmInstallDialog.vue force install & restart 确认弹窗，列出会被
│                                 终止的本地 session 数 + 远端 detach 数
│   └── RemoteSessionsDialog.vue cast 面板：以 tab 形式打开远端 session
├── composables/
│   └── useTerminalShortcuts.ts document 级 capture-phase keydown router
│                                匹配 e.code（KeyN/KeyW/KeyT/Bracket{Left,Right}）
│                                避开 macOS Option-letter dead key 的 e.key 陷阱
└── lib/
    ├── types.ts           LayoutKind/Pane/Tab/SplitDir/FocusDir
    ├── layout.ts          transitionLayout / closePane / focusNeighbor 纯函数
                           （27 单测，TDD）
    ├── proto.ts           帧编解码（移植自 web/app.js）
    ├── connection.ts      SessionConnection：WS attach + 重连 + 续传 +
                           sendResize 队列（WS 还在 CONNECTING 时缓存，ws.onopen
                           跟在 ATTACH 帧后 flush）
    └── api.ts             Wails bindings 包装（不依赖 generated 文件，走
                           window.go.main.App.*；含 UpdateState 镜像）
```

## 跨进程时序细节

ANNOUNCE 全量快照：每次发都覆盖 relay 的 mirror manifest，无 diff 协议。心跳 30s + 事件驱动（`relayHost.notifyChange()`）。relay 端连接断开时清空该 host 全部 mirror sessions（registry.Remove → broadcast CLOSE 给当前 attachers）。

OUT 帧 `seq` u64：从 1 开始单调递增，不在 reattach/reconnect 时重置。客户端 ATTACH(since_seq=N) 时 relay 从 ringbuf 取 `seq > N` 的帧补发；如果 ringbuf 已驱逐 N，relay 发 `OUT(seq=0, "\x1b[2J\x1b[H")` 表示截断后重放当前所有 history。

写入 WebSocket 必须串行：`nhooyr.io/websocket` 的 Write 不允许并发。每个连接持有一个 `out chan proto.Frame`，单 writer goroutine drain 之；ANNOUNCE/STREAM_REQUEST/STREAM_STOP/OUT/META 等所有帧都要走这个 channel。

uplink 注册表清理：relay 端 `internal/relay/uplink_conn.go` 的 writer goroutine 跑 25s ping。Ping 失败 → writer return → `defer cancelConn()` 触发 reader 上下文取消 → reader 解阻塞 → handleUplink 的 deferred `cleanup()` 删掉该连接对应的全部 mirror sessions。35s 内孤儿不再泄漏（替代 OS-level TCP keepalive 的分钟级超时）。

## 自动更新

```
boot →
  Updater.Start(ctx)（受 cfg.AutoCheckUpdatesOrDefault() 门控）
    ├─ 2s 后 Check(force=false)
    └─ 24h ticker

用户在 Settings → Updates 点 "Check now" →
  CheckUpdate() → Check(force=true)（绕过 1h 缓存）

发现新版（state.Available=true）→
  ⚙ badge dot 亮（App.vue 5s poll GetUpdateState 维持）
  Settings 显示 "v0.2.0 available · [Download]"

用户点 Download →
  StartDownload() → fetch asset → ${UserCacheDir}/atterm/updates/<ver>-<asset>.partial
  → size 校验 → 验证 SHA256SUMS.sig → 校验 asset SHA256 → atomic rename
  → state.Ready=true → 按钮变 "Force install & restart"

用户点 Force install & restart →
  ConfirmInstallDialog（列出受影响 session 数）
  确认 → InstallUpdate()
    ├─ Updater.InstallAndQuit()：写 helper 到 cache，spawn detached
    │     bash <helper> <pid> <archive> <bundle/binary>
    └─ 200ms 后 wailsruntime.Quit(ctx)

helper 在子进程跑：
    while kill -0 $pid; do sleep 0.5; done   # 等我们退出
    解压 archive → 替换原位 → 平台特异收尾 → 启动新副本 → 清理
```

release CI：

```
push tag v* →
  build jobs 使用 prod environment secret
    ATTERM_UPDATE_VERIFY_PUBLIC_KEY → ldflags 注入 main.UpdateVerifyPublicKey
  release job 下载所有 artifacts
    ATTERM_UPDATE_SIGNING_PRIVATE_KEY → .github/scripts/sign-release-checksums.go
    → 生成 SHA256SUMS + SHA256SUMS.sig
    → softprops/action-gh-release 上传 artifacts + 校验文件
```

公钥可以公开；私钥只放 GitHub prod environment secret，不进仓库。

dev/empty Version 的整个路径短路（不查 GitHub、UI 显示 "development build"）。
跨实例 attach 的远程 session 在 fit 后的 cols/rows 与 SessionInfo 一致时跳过初始
RESIZE，避免对端 zsh 的 `PROMPT_EOL_MARK` 噪声。
