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
│  /api/pair/{create,consume} — QR 配对：一次性 5min token       │
│  /healthz (public) + /admin/health (HTML/JSON) — 健康检查      │
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
| `relay` | `internal/relay/` | HTTP/WS 服务，处理 agent/uplink/client/sessions/pair/health 端点 | 不写 PTY、不持久化（除 `users.db` via userstore） |
| `userstore` | `internal/userstore/` | SQLite 持久化：users / invitations / api_tokens / web_sessions / pairing_tokens / webhooks | 不知道 HTTP / 不依赖 relay |
| `ptyhost` | `internal/ptyhost/` | 纯 PTY 包装，无本地 TTY 副作用 | 不知道 relay 协议 |
| `agent` | `internal/agent/` | CLI wrapper：`ptyhost` + 本地 stdin/stdout 桥接 + `relay` 客户端 | 不被桌面端用 |
| `hostid` | `internal/hostid/` | 机器持久 UUID | 不知道 session |
| `desktop/relay_host.go` | desktop | 启 mini relay，spawn PTY，AdoptSession | 不连远程 |
| `desktop/uplink.go` | desktop | 远程 relay 客户端（lazy 协议） | 不直接拥有 PTY |
| `desktop/updater.go` | desktop | GitHub Releases 自动更新 state machine（check / download / 调用 platform install helper） | 不动 PTY、不动 relay |
| `desktop/scripts/install-{darwin,linux,windows}` | desktop | 平台 install helper，等父 PID 退出后替换 binary 并重启 | 不发网络请求 |
| `desktop/diagnostics.go` | desktop | 收集 app/OS/relay 状态摘要 + 脱敏，写到用户选择的文件 | 不读 PTY 字节、不导出 token 明文 |
| `desktop/app.go` | desktop | Wails bindings (Session / Relay / Update / Pairing / Diagnostics / QuickTemplates) | 不实现协议 |
| `web/` | web | Vue 3 + TypeScript + Naive UI 多页浏览器/PWA client（login/signup/main/settings/admin/setup），通过同源 API/WS 直连 relay | 不从 CDN 加载 script/style，不持久化除 cookie/session/token bootstrap 以外的会话状态 |

## User accounts and identity

当前 atterm-relay 以 per-user accounts 为生产默认。All session data,
web push subscriptions, outbound webhooks, and API tokens are scoped to a
user.

### Storage

`internal/userstore` is the **only** package that opens the SQLite
database (`${ATTERM_RELAY_CONFIG_DIR}/users.db`, WAL mode by default).
Tables: `users`, `invitations`, `api_tokens`, `web_sessions`. All
credentials (passwords, invite codes, API tokens, cookie values) are
stored as `sha256` (or argon2id for passwords). Plaintext is returned to
the user exactly once at issue time.

### Principal kinds

`internal/relay/identity.go` resolves every incoming HTTP / WS-upgrade
request to a `Principal`:

| Kind | Source | Use |
|------|--------|-----|
| User | `atterm_session` cookie OR `Authorization: Bearer atk_…` (or `Sec-WebSocket-Protocol: atterm-token.atk_…`) | All user-scoped routes |
| Admin | `atterm_session` cookie where `user.is_admin=true` | Only `/admin/*` |
| None | (no valid credential) | Public routes only |

### Entry-point gates

| Entry | Allowed Principal |
|---|---|
| `GET /api/me/*` | User |
| `GET /api/sessions` | User (filtered to `OwnerUserID == UserID`) |
| `GET/WS /client?session=<id>` | User where `Session.OwnerUserID == Principal.UserID` |
| `WS /uplink` | User from API token (cookie rejected) |
| `WS /agent` | User from API token |
| `/admin/*` | Admin only |
| `POST /api/auth/{signup,login}` | None (public) |

### Bootstrap path

1. Operator starts relay with `ATTERM_BOOTSTRAP_ADMIN_EMAIL` and
   `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` (password must satisfy the strength check
   on public listen: ≥16 chars, ≥3 character classes, not in dev blacklist).
2. Operator logs in as the bootstrap admin and opens `/admin/` → creates an invitation.
3. End user signs up at `/signup.html?invite=inv_…`.
4. User generates an API token at `/settings.html`.
5. User pastes the API token into desktop client → uplink connects.

See `docs/superpowers/specs/2026-05-15-saas-user-accounts-design.md`
for the full design (data model, security invariants, threat model,
test strategy).

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

浏览器打开 http://relay/#token=… → ATTACH(sid)
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

- ✅ Phase 0：协议骨架，命令行 wrapper + relay + 浏览器 attach
- ✅ Phase 1：Wails 桌面壳，多 tab，自动建会话，cwd-driven 标题
- ✅ Phase 1.5：lazy 远程镜像（ANNOUNCE/STREAM_REQUEST/STOP），GUI 设置入口，cast 面板
- ✅ Phase 2：每 tab 1/2/4 pane 分屏（layout pure fns + iTerm-style ⌘N/⌘⇧N 快捷键）；自动更新（GitHub Releases，Ed25519/SHA256 验签，dev 短路，用户手动 force install）
- ✅ Phase 3：用户账号、邀请码、per-user API token、admin UI、Web Push、outbound webhook、Vue 3 + TypeScript + Naive UI Web/PWA、多语言
- ✅ Phase 4a：Capacitor iOS WebView MVP、移动 relay setup、host-grouped mobile session list、touch terminal
- ✅ Phase 4b：P0 任务状态模型 / 移动任务首页 / 通知深链 / 移动快捷控制 / relay setup wizard
- ✅ Phase 4c（v0.4 引导可信度）：P1.6 桌面端 QR 配对 + 移动端扫码消费 token（`/api/pair/*`）；P1.7 relay `/healthz` + admin `/admin/health` 健康检查页；P1.9 iOS Keychain 安全存储 + ATS；P1.10 桌面诊断信息导出 + 脱敏
- ✅ Phase 4d（v0.5 AI 任务控制台）：P2.11 session 类型分类（shell / ai / test / build / deploy，sticky non-shell）；P2.12 OSC 133 D 事件触发的 SessionSummary（ANSI-stripped tail + error lines），MetaPayload 携带 type + summary；P2.13 AI 快捷模板（QuickTemplate model + desktop Settings editor + 三端 bar）
- ✅ Phase 4e（v0.6 mobile UX 收口，至 v0.2.39 全部落地）：移动端独立设置页 + 模板/aux 键编辑器 + 退出登录保留配置（#105）；终端首屏全屏（ResizeObserver 替换一次性 fit）+ viewer 锁尺寸（onMeta term.resize）（#106）；中文输入法 capture-phase 补获 `insertText`（#107）；设置改动通过事件总线（`mobile:shortcutsChanged` / `quickTemplates:changed`）实时同步到已开 tab（#108）；Capacitor 8 plugin 正式落地（#104 Keychain + #109 Camera/barcode 注册到 mobile/package.json + #113 keyboard accessory bar 隐藏）；QuickTemplate v2（hotkey + 直接发送 + 显示/隐藏开关 + 新默认值）+ 删 legacy quickInput 插件（#110 + #111）；防误触模式 banner（#100）
- ⬜ Phase 5（未完成）：P1.8 桌面安装包 codesign + notarization；P3+ 单 session 分享 / presence / 审计日志 / 持久化历史 / 命令级回放

## 桌面端架构细节

```
desktop/main.go            创建 *App，wails.Run；OnStartup→app.startup，OnShutdown→app.shutdown
                           macOS-only: 自定义 NSMenu（保留 App + Edit role，不要 Window
                           submenu，让 ⌘W 留给前端）
                           var Version / UpdateVerifyPublicKey (ldflags 注入)
desktop/app.go             A 持有 *relayHost、*uplink、*configStore、*Updater
                           暴露 ~48 个 binding，按用途分组：session/relay/update/
                           pairing/diagnostics/quicktemplates/plugin-fs；
                           CloseSession sync 注销（cleanup() 同步调，notifyChange 立即
                           推 ANNOUNCE，不等 pty.Wait）；
                           CreatePairingToken 用当前 RelayURL + API token 调
                           `/api/pair/create`，前端拿到 token 渲染 QR；
                           GetQuickTemplates / SetQuickTemplates 读写
                           appConfig.QuickTemplates；
                           GetDiagnostics / ExportDiagnostics 走 desktop/diagnostics.go
                           组装脱敏 payload + 平台 save dialog
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

- 用户账号和身份信息存储在 SQLite（`users.db`，路径由 `--config-dir` 或 `ATTERM_RELAY_CONFIG_DIR` 指定）；
- 公网监听时必须设置 `ATTERM_BOOTSTRAP_ADMIN_EMAIL` 和 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`（见协议规范 §鉴权），除非显式 `--dev-insecure`；
- 公网监听未设置 `--origins` / `ATTERM_ORIGINS` 时拒绝启动，除非显式 `--dev-insecure`；
- 默认返回 CSP/security headers，`web/` 项目代码只允许同源 script 和同源 stylesheet；CSP 额外允许 inline style 供 xterm.js 运行时布局样式，并预留 Cloudflare Web Analytics beacon 源（不允许应用代码引入 CDN 依赖）；Vue/xterm/Naive UI 等依赖由 Vite 打包为同源 assets，并由 PWA service worker 预缓存；
- 对 HTTP 请求和 WS upgrade 先按远端 IP 限流，鉴权成功后再按远端 IP + token hash 限流，并限制同一 key 的活跃 WS 连接数；
- 支持 owner 发布的 `remote_permission`（view/control/full），relay 和 desktop uplink 双重强制执行；
- 可选 `--config` 启用持久化 runtime 配置（rate limit、连接数）；`/admin/` API 需 bootstrap admin 用户（通过 env vars 初始化）；
- `--dev-insecure` 只用于开发/可信内网，会打印明文传输/弱鉴权警告。

鉴权详情见协议规范 §鉴权（browser cookie、desktop API token、admin Bearer token 三种来源）。

`internal/relay.NewServer(relay.Config{})` 作为库仍保留”不鉴权”语义（当 Resolver 和
Store 均为 nil 时），供本地 mini relay 或测试使用；不要把它等同于 `cmd/atterm-relay`
的生产默认行为。

## 远程权限与 admin 配置

远程权限由拥有 PTY 的 desktop app 决定。Settings 中的默认权限会写入
`desktop/config.go`，`desktop/uplink.go` 在 `ANNOUNCE` 的每个 `SessionInfo`
里发布 `remote_permission`。远端 relay 计算 principal scope 与 owner 权限的交集：
用户 API token 始终是 write scope；但不能超过 owner 发布的 view/control/full。

relay admin 配置只服务运维场景：调整 rate limit 和连接数。用户账号管理（邀请码、
用户列表、密码重置）通过 `/admin/api/*` 端点操作，凭证为 admin session cookie（`user.is_admin=true`）。

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
│                          + close-pane × 同行 flex 布局；close 按钮 mousedown
│                          不冒泡，避免先激活其它 pane 再关错目标
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
├── lib/
│   ├── types.ts           LayoutKind/Pane/Tab/SplitDir/FocusDir
│   ├── layout.ts          transitionLayout / closePane / focusNeighbor 纯函数
│                          （27 单测，TDD）
│   ├── proto.ts           帧编解码（与 web/src/shared/ws/protocol.ts 保持协议常量同步）
│   ├── connection.ts      SessionConnection：WS attach + 重连 + 续传 +
│                          sendResize 队列（WS 还在 CONNECTING 时缓存，ws.onopen
│                          跟在 ATTACH 帧后 flush）
│   └── api.ts             Wails bindings 包装（不依赖 generated 文件，走
│                          window.go.main.App.*；含 UpdateState 镜像）
├── platform/              Wails / Capacitor / browser adapter；App 只依赖
│                          `usePlatform()`，不要在其它目录直接 import wailsjs
├── plugins/               右侧插件槽：file explorer / translate（legacy quickInput 已删，由 QuickTemplate 取代）
└── i18n/                  desktop 前端中英 messages + useI18n()
```

`web/` 前端同样是 Vue 3 + TypeScript，但按浏览器页面拆成 MPA：

```
web/src/
├── main/                  session list + xterm attach + PWA install hint
├── login/ signup/ setup/  auth 与移动 relay bootstrap
├── settings/              tokens / sessions / push / webhooks / relay config
├── admin/                 users / invitations / relay config
└── shared/                api clients、ws protocol、i18n、Naive theme、Topbar
```

## 移动端架构（Capacitor）

```
mobile/                                   Capacitor 壳（capacitor.config.json + Xcode 工程）
├── package.json                          Capacitor plugin 依赖（cap sync 扫这里！）
├── capacitor.config.json                 appId / webDir = www
├── scripts/sync-web.mjs                  build desktop/frontend:capacitor + 复制到 www
└── ios/App/                              Xcode 工程
    ├── App/MainViewController.swift      CAPBridgeViewController 子类，
    │                                     capacitorDidLoad() 里
    │                                     bridge?.registerPluginInstance(...)
    ├── App/Plugins/AttermSecureStorage/  自定义 plugin（Keychain）
    │                                     CAPBridgedPlugin conformance
    ├── App/Info.plist                    NSCameraUsageDescription /
    │                                     NSPhotoLibraryUsageDescription /
    │                                     NSPhotoLibraryAddUsageDescription
    ├── App/capacitor.config.json         cap sync 生成的 packageClassList
    └── CapApp-SPM/Package.swift          cap sync 生成的 SPM 依赖列表

desktop/frontend/src/
├── main.capacitor.ts                     Capacitor 入口；启动调
│                                          Keyboard.setAccessoryBarVisible(false)
└── mobile/                               所有移动专属组件
    ├── MobileApp.vue                     view: 'setup' | 'list' | 'terminal' | 'settings'
    ├── MobileSetup.vue                   scheme dropdown + host input + token
    │                                     + 仅 http 时显示 insecure 开关
    ├── MobileSessionList.vue             gear emit openSettings（不再 editRelay）
    ├── MobileSettings.vue                语言 / 模板编辑器 / aux 键编辑器 / 退出登录
    │                                     emit 'mobile:shortcutsChanged' 同步开终端
    ├── MobileListEditor.vue              通用增删改/重排/重置组件
    └── MobileTerminal.vue                xterm + 防误触 banner + AUX bar + template bar
                                          + ResizeObserver（driver fit）
                                          + onMeta viewer 锁尺寸
                                          + onImeInput capture 阶段补获 insertText
                                          + Camera.getPhoto 图片上传
```

**Plugin 注册三件套（红线 #15）**：

1. **TS 端 import**：装到 `desktop/frontend/package.json`，import 解析。
2. **Native 端注册**：**同时**装到 `mobile/package.json`，`cap sync` 才扫得到，
   写进 `mobile/ios/App/App/capacitor.config.json` 的 `packageClassList` 与
   `mobile/ios/App/CapApp-SPM/Package.swift` 的 dependencies。
3. **自定义 plugin 还要**：Swift 类 conform `CAPBridgedPlugin`（设
   `identifier` / `jsName` / `pluginMethods`），并在 `MainViewController.capacitorDidLoad()`
   里 `bridge?.registerPluginInstance(...)`。

`mobile/package.json` 的 `ios:open` script 已自动串 `npm install + cap sync`。

**事件总线（同进程 pub/sub，platform.events）**：

- `mobile:shortcutsChanged`（mobile）：MobileSettings 改完后 emit，
  MobileTerminal 订阅重新 `effectiveTemplates / effectiveAuxKeys / loadHidden`。
  绕过 `active` prop 不变的死角（进设置页时 activeSessionId 不变）。
- `quickTemplates:changed`（desktop）：SettingsTemplates 改完后 emit，
  desktop TerminalView 订阅重新 reload。

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
