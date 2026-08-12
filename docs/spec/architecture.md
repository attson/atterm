# 架构规范

> **Audience**: 理解 atterm 系统整体结构的工程师
> **Last updated**: 2026-07-31
> **Status**: stable
> **See also**: [auth.md](./auth.md) · [protocol.md](./protocol.md) · [feishu.md](./feishu.md) · [conventions.md](./conventions.md) · [component-style.md](./component-style.md) · [site.md](./site.md)

v0.3.x 主线新增（本文档已并入）：relay 多实例栈（realm identity + `relay_instances` 心跳 + `home_instance_url` 路由）+ DB-backed 运行时配置（`relay_config` 表，SQLite / Postgres 双后端，取代原 `relay.json`）、远程文件浏览器（`FS_REQUEST/RESPONSE/EVENT` + `PASTE_FILE`）、会话侧栏置顶 + 搜索（`useSessionPins` / `sessionMatch.ts`）及其恢复期 pin 迁移、桌面启动致命错误非崩溃化（`StartupError`）、**web 主入口改挂桌面 `App.vue`**（`platform/web.ts` 桥接，删除独立的 `web/src/main/` 会话列表页与 `settings.html` / `admin.html` 主界面）、web 终端辅助键 + 显式选择图片/文件粘贴（复用 `TerminalView.vue`，走 `PASTE_IMAGE` / `PASTE_FILE`，受 driver + `remote_permission=full` 门控）。相关设计 doc：[session-bar-pin-design](../superpowers/specs/2026-07-20-session-bar-pin-design.md) · [pinned-session-recovery-design](../superpowers/specs/2026-07-23-pinned-session-recovery-design.md) · [sidebar-search-design](../superpowers/specs/2026-07-24-sidebar-search-design.md) · [web-layout-align-desktop-design](../superpowers/specs/2026-07-27-web-layout-align-desktop-design.md)。

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
│  /agent — 直接连 relay 的 session 来源（保留给未来 wrapper）  │
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
| `userstore` | `internal/userstore/` | SQLite/Postgres 双后端持久化：users / invitations / sessions / pairing_tokens / webpush subscriptions / `relay_config`（运行时配置）/ `relay_realm_state`（realm identity）/ `relay_instances`（多实例心跳）；历史 `webhooks` 表已由 migration 删除 | 不知道 HTTP / 不依赖 relay |
| 多实例 | `internal/relay/node_home.go` + `config_refresh.go` + `internal/userstore/relay_instances.go` | 多实例心跳缓存（`relay_instances` 表）、`resolveHomeInstanceURL` 路由、`relay_config.version` 轮询（~10s TTL）向其它实例传播 admin 配置变更 | 不直连其它实例（gossip）；一切共享状态经 DB |
| `ptyhost` | `internal/ptyhost/` | 纯 PTY 包装，无本地 TTY 副作用 | 不知道 relay 协议 |
| `hostid` | `internal/hostid/` | 机器持久 UUID | 不知道 session |
| `desktop/relay_host.go` | desktop | 启 mini relay，spawn PTY，AdoptSession | 不连远程 |
| `desktop/uplink.go` | desktop | 远程 relay 客户端（lazy 协议） | 不直接拥有 PTY |
| `desktop/remote_fs.go` | desktop | 远程文件浏览器桥接：本机文件系统 CRUD + 回收站 + watch，响应 `FS_REQUEST`/发 `FS_EVENT`，受 `remote_permission=full` 门控（配合 `internal/relay/fs_router.go` 按 host 路由） | 不做前端 UI、不越权访问未授权 session 的 host |
| `desktop/updater.go` | desktop | GitHub Releases 自动更新 state machine（check / download / 调用 platform install helper） | 不动 PTY、不动 relay |
| `desktop/scripts/install-{darwin,linux,windows}` | desktop | 平台 install helper，等父 PID 退出后替换 binary 并重启 | 不发网络请求 |
| `desktop/diagnostics.go` | desktop | 收集 app/OS/relay 状态摘要 + 脱敏，写到用户选择的文件 | 不读 PTY 字节、不导出 token 明文 |
| `desktop/app.go` | desktop | Wails bindings (Session / Relay / Update / Pairing / Diagnostics / QuickTemplates) | 不实现协议 |
| `web/` | web | Vue 3 + TypeScript + Naive UI 浏览器/PWA client；除 login/signup/setup/firstrun 几个独立小页面外，主入口 `index.html` 现在通过 `main-web.ts` → `desktop/frontend/src/main.web.ts` 挂载**桌面同一份** `App.vue`（见「前端架构细节」节），不再是独立的 `web/src/main/` 会话列表页；`settings.html` 已删除（设置并入 `App.vue` 的 `SettingsDialog.vue`），admin 面板由主 App.vue 内嵌为 AdminPanel 视图（TabBar 按钮切换），不再是独立的 `admin.html` MPA entry；终端辅助键、右键菜单、置顶/多选、选择图片/文件粘贴都复用桌面组件，通过同源 API/WS 直连 relay | 不从 CDN 加载 script/style；localStorage 只保存 `session_token`（以及 web 专属的 pin/模板/aux-key 等 `platform/web.ts` 桥接的本地偏好） |
| `internal/feishu` | internal | 飞书 client SDK：Cardkit v2 anchor card 渲染 + IM 消息 API + Router.InjectKeystrokesBySession（stroke dispatch 到本地 pty） | 不知道 hook payload 结构（那是 `desktop/feishu` 层）|
| `desktop/feishu` | desktop | hook payload 解析、AskUserQuestion form 挂拆 lifecycle、stroke plan 构造、local 模式 LongConn subscriber | 不直接持有 pty；stroke 通过 `Router.InjectKeystrokesBySession` 送 |

飞书子系统的模式分流（local vs relay）、anchor card 生命周期、AskUserQuestion 表单式远程回答的完整规范见 [feishu.md](./feishu.md)。

## User accounts and identity

所有客户端通过邮箱 + 密码登录 relay。登录成功后 relay 颁发 `session_token`（明文仅在响应 body 返回一次，DB 存 sha256 哈希），客户端在后续 HTTP / WS 请求中携带。`requireSession` 中间件统一在 mux 层拦截。

完整模型（含 bootstrap、pairing、状态机、错误码、客户端实现要点）见 [auth.md](./auth.md)。

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

### 流 4：Driver / Viewer 角色协商

同一 session 允许多端同时 attach（本机 desktop + 浏览器 + 另一台 desktop）。为避免多端写冲突，attach 默认进入 **viewer** —— relay 直接 drop 来自非 driver 的 `IN` / `RESIZE` 帧；某一端发 `CLAIM_DRIVER` 升 driver 后，relay 广播 META（`driver_client_id` + `driver_client_name`）让其他端落回 viewer。

```
client                       relay
─────────────────────────────────────────────
ATTACH(sid, client_id)  ───►
                        ◄── META(driver_client_id=X, name=...)
  isMe = (meta.driver_client_id === this.clientID)
  → 设置 / 隐藏 viewer overlay

用户在 viewer 模式按 Space：
  CLAIM_DRIVER(client_id) ───►
                        ◄── META(driver_client_id=this.clientID)
  onDriverChange(isMe=true) → 升 driver
  原 driver 收到同一帧 → 降 viewer
```

关键不变量：

- **Viewer 锁尺寸**：viewer 不发 RESIZE，`onMeta { cols, rows }` 直接 `term.resize(cols, rows)` 跟 PTY 走；FitAddon 只在 driver 模式跑（见 `desktop/frontend/src/components/TerminalView.vue::safeFit` / `applyViewerSize`）。
- **远端 pane 默认 viewer**：`TerminalView.isDriver = ref(props.isLocalSession ?? true)` —— 本机永远 driver，远端起始 viewer，等第一帧 META 校正。乐观默认 driver 会让 "看着能输入但 relay 在丢 IN 帧" 的状态难发现（PR #240）。
- **恢复 attach 不复用旧 client_id**：新建 SessionConnection 时用新的 `crypto.randomUUID()`，relay 视为新加入的 viewer；同一连接对象的内部重连保留 client_id，但不会在 `onopen` 立即重申旧 driver。它先等待权威 META：若已有其他 driver 就保持 viewer，只有 META 明确显示 driver 为空且断线前本端确为 driver 时，才补发一次 `CLAIM_DRIVER` 无感恢复。**uplink-proxy sub 不会自动 promote 自身**（见 `internal/relay/uplink_conn.go` 与 `desktop/frontend/src/lib/connection.ts::openWS`），避免桌面 mirror sub 或刚返回的后台 Tab 误抢真用户的 driver。

帧格式与字节级语义见 [protocol.md](./protocol.md) §Driver / Viewer 模型。前端实现在 `desktop/frontend/src/components/TerminalView.vue`（`isDriver` ref + `onDriverChange` handler + Space 拦截调 `claimDriver()`）——web 现在挂载的就是这同一个组件（见「前端架构细节」节），不再有独立的 `web/src/main/` 实现。

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

**会话置顶跨重启迁移**：本机 pane 重启后 recovery 会拿到新 `session_id`；`desktop/frontend/src/composables/useRecoverySnapshot.ts::buildSnapshot` 现在对纯 local pane 也写 `session_id`（仅供 pin 迁移读，不触发 remote-rebind 分支），`App.vue::executeRestore` spawn 新 sid 后调 `useSessionPins.rename(oldSid, newSid)` 承接置顶状态。**sidebar-viewer on local host**（`p.remote===true` 但 `info.host_id===本机`）是第三种 pane 状态，其 `session_id` 属于另一实例的 relay，故意不参与该迁移——完整三态判定表见 [pinned-session-recovery-design.md](../superpowers/specs/2026-07-23-pinned-session-recovery-design.md) §4.1。

**同一 PTY 内 AI 恢复凭据 latest-generation-wins**：OSC 133 `C` 每报告一次顶层 `claude` / `codex` / `aider` 启动，desktop 就建立新的 resolver generation——包括 Claude→Codex、Codex→Claude 以及同 kind 新会话。新 generation 同步取消旧 resolver，并先通过 `recovery:ai-sid` 的空 `ai_session_id` 清掉旧恢复凭据；只有当前 generation 精确解析出的 SID 才能重新写入 recovery snapshot，旧 resolver 的迟到回调必须按 generation 丢弃。新 SID 抓不到时宁可恢复普通 shell，也不能回退到上一段 AI 对话。恢复注入产生的首次同 kind OSC 只确认已建立的恢复 generation，不重复清凭据/启动 resolver；该豁免只消费一次。

## phase 完成度（截至当前）

- ✅ Phase 0：协议骨架，命令行 wrapper + relay + 浏览器 attach
- ✅ Phase 1：Wails 桌面壳，多 tab，自动建会话，cwd-driven 标题
- ✅ Phase 1.5：lazy 远程镜像（ANNOUNCE/STREAM_REQUEST/STOP），GUI 设置入口，cast 面板
- ✅ Phase 2：每 tab 1/2/4 pane 分屏（layout pure fns + iTerm-style ⌘N/⌘⇧N 快捷键）；自动更新（GitHub Releases，Ed25519/SHA256 验签，dev 短路，用户手动 force install）
- ✅ Phase 3：用户账号、邀请码、session token 鉴权、admin UI、Web Push、outbound webhook、Vue 3 + TypeScript + Naive UI Web/PWA、多语言
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
                           pairing/diagnostics/quicktemplates/plugin-fs/pin；
                           CloseSession sync 注销（cleanup() 同步调，notifyChange 立即
                           推 ANNOUNCE，不等 pty.Wait）；
                           CreatePairingToken 用当前 RelayURL + session token 调
                           `/api/pair/create`，前端拿到 token 渲染 QR；
                           GetQuickTemplates / SetQuickTemplates 读写
                           appConfig.QuickTemplates；
                           GetDiagnostics / ExportDiagnostics 走 desktop/diagnostics.go
                           组装脱敏 payload + 平台 save dialog；
                           Get/SetPinnedSessionIds 读写 config.go::PinnedSessionIDs；
                           setStartupFatalError(msg, logPath) / GetStartupError() —
                           relay host 启动或日志初始化失败时不再 log.Fatalf 崩进程，
                           改记 startupFatal *StartupError{Fatal,Message,LogPath}
                           字段，webview 照常起来，前端 boot 末尾拉取展示可复制的
                           失败信息（红线 #19 + #35）
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

- 用户账号和身份信息存储在 `internal/userstore`（SQLite 或 Postgres 双后端，由 `ATTERM_RELAY_DB_DRIVER=sqlite|postgres` 选择，DSN 走 `ATTERM_RELAY_DB_DSN`；SQLite 默认路径由 `--config-dir` 或 `ATTERM_RELAY_CONFIG_DIR` 指定）；密码走 OPAQUE，relay 永不接收明文密码（见 auth.md §12）；
- 监听 HTTP（`--addr :8080`，给反代后端 / loopback 开发）与 HTTPS（`--https-addr`，浏览器直连）。OPAQUE 用浏览器 WebCrypto，只在安全上下文（HTTPS/localhost）可用，故浏览器必须经 HTTPS 访问。**无自签回退**：开 `--https-addr` 必须提供真证书 `ATTERM_TLS_CERT/KEY`（缺失即 fatal，见 `buildTLSConfig`），否则在 `:8080` 前面挂 TLS 终止反代（Cloudflare/Caddy/nginx）（红线 #27）；
- 公网监听时必须设置 `ATTERM_BOOTSTRAP_ADMIN_EMAIL`（relay 启动打印一次性 claim token，操作员用它完成 OPAQUE 注册即获得 admin；**无 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`**），除非显式 `--dev-insecure`；
- 公网监听未设置 `--origins` / `ATTERM_ORIGINS` 时拒绝启动，除非显式 `--dev-insecure`；
- 默认返回 CSP/security headers，`web/` 项目代码只允许同源 script 和同源 stylesheet；CSP 额外允许 inline style 供 xterm.js 运行时布局样式，并预留 Cloudflare Web Analytics beacon 源（不允许应用代码引入 CDN 依赖）；Vue/xterm/Naive UI 等依赖由 Vite 打包为同源 assets，并由 PWA service worker 预缓存；
- 对 HTTP 请求和 WS upgrade 先按远端 IP 限流，鉴权成功后再按远端 IP + token hash 限流，并限制同一 key 的活跃 WS 连接数；
- 支持 owner 发布的 `remote_permission`（view/control/full），relay 和 desktop uplink 双重强制执行；
- runtime 配置持久化在 DB `relay_config` 表（`internal/userstore/relay_config.go`，singleton row），`/admin/api/*` 可运行时读写并热生效，无需重启（见下「运行时配置」）；`/admin/` API 需 admin 用户；
- `--dev-insecure` 只用于开发/可信内网，会打印明文传输/弱鉴权警告。

### 运行时配置（DB `relay_config` + 管理后台）

`relay.json` 已完全下线：`docker-compose.yml` 明确注释「no longer written or read」，仓库内 `internal/`、`cmd/` 下已无非测试引用。启动所需 env 已收窄到「核心」（bootstrap email、origins、DB 后端选择）；飞书、限流、Origin 白名单、VAPID subject、详细日志等都下沉到 `internal/relay/admin_config.go` 的 `AdminConfig`，由 `/admin/api/config` 与 `/admin/api/feishu` 读写并热应用：

- **热生效（不重启）**：origins / debug 走 `Server` 上的 `atomic.Pointer` / `atomic.Bool`；限流走 `applyRuntimeLimits`（`ATTERM_RATE_LIMIT_PER_MINUTE` / `ATTERM_MAX_CONNECTIONS_PER_KEY` 例外，仍是 per-instance 进程内 map，改后需重启该实例）；飞书走 `Server.ApplyFeishuConfig`（运行时建/拆 secret cipher + handler，路由常注册后按原子 handler 门控，因为 `http.ServeMux` 不支持注销路由）。
- **唯一因架构限制需重启**：VAPID subject（`webpush.Open` 启动时一次性消费）。
- **DB 在无 secret cipher 下也能打开**：字段加密 cipher 只在启用飞书时挂载（`userstore.SetSecretCipher`），所以 `ATTERM_FEISHU_ENCRYPT_KEY` 不再是启动必填——没配飞书的 relay 不会因缺密钥崩溃。
- **env→config 播种**：上述每个 env 在首次启动时若 `relay_config` 对应字段为空则写入一次（DB 值优先），之后管理后台是唯一可信源。
- 飞书主加密密钥有意持久化在 DB `relay_config.feishu_secret_key` 以保护 `users.db` 里的飞书凭据；admin GET 只回显末 4 位、绝不返回明文、绝不写日志；换 key 会让旧飞书绑定无法解密，故 PUT 默认拒绝轮换、需 `force:true`。详见 AGENTS.md 红线 #26。
- **多实例传播**：`internal/relay/config_refresh.go` 轮询 `relay_config.version`（默认 ~10s TTL），任一实例的 admin 改动会在这个窗口内同步到所有实例。

鉴权详情见协议规范 §鉴权（统一为 Bearer session token，admin 由 `users.is_admin` 决定）。

`internal/relay.NewServer(relay.Config{})` 作为库仍保留”不鉴权”语义（当 Resolver 和
Store 均为 nil 时），供本地 mini relay 或测试使用；不要把它等同于 `cmd/atterm-relay`
的生产默认行为。

## Relay 多实例架构

跨机 HA / 就近节点路由通过 realm identity + instance registry 实现（v0.3.x，全部合入
`v0.3-dev`）：

- **realm identity**：`internal/userstore/realm.go` 的 `relay_realm_state`（singleton
  row）持有整个集群共享的 `realm_id`，首次启动生成、永不变，直接影响 E2EE
  `account_key` 派生——同一物理集群的所有实例必须共享同一 realm，`realm_id` 只从 DB
  读、不接受 env 覆盖。
- **instance heartbeat**：每个实例通过 `ATTERM_RELAY_INSTANCE_PUBLIC_URL` 声明自己的
  外部可达 URL，定期心跳写入 `internal/userstore/relay_instances.go` 的
  `relay_instances` 表；活跃实例列表直接从该表读（`ListActiveInstances`），由
  `internal/relay/node_home.go` 消费做路由。
- **home_instance 路由**：`internal/relay/node_home.go::resolveHomeInstanceURL` 按用户
  的 `home_instance_url` 偏好字段返回登录后应连的实例（未设置则任选一个活跃实例）。
  登录响应（`internal/relay/opaque_auth.go`）在 `AUTH_INFO` 之外额外下发 `realm_id` +
  `home_instance_url` 两个字段（走 HTTP 登录响应而非帧协议，详见 auth.md §登录）；
  客户端拿到 `home_instance_url` 后自行发起 reconnect。`PUT /api/me/home` 可改用户偏好，
  `GET /api/nodes` 返回活跃实例列表供节点切换 UI 使用。
- **配置传播**：多实例场景下所有实例指向同一外部 Postgres，`relay_config` 变更靠上文
  §运行时配置 的版本轮询扩散；除 realm/config 外没有实例间直连或 gossip。

改这部分代码时请同步更新本节与 AGENTS.md 红线 #33、auth.md §登录响应字段。

## 远程权限与 admin 配置

远程权限由拥有 PTY 的 desktop app 决定。Settings 中的默认权限会写入
`desktop/config.go`，`desktop/uplink.go` 在 `ANNOUNCE` 的每个 `SessionInfo`
里发布 `remote_permission`。远端 relay 计算 principal scope 与 owner 权限的交集：
session token 始终是 write scope；但不能超过 owner 发布的 view/control/full。

relay admin 配置（`AdminConfig` / DB `relay_config`）覆盖运维场景：rate limit、连接数、Origin 白名单、详细日志开关，以及飞书集成（开关 + 加密密钥 + base URL）。改动经 `/admin/api/config` 与 `/admin/api/feishu` 热生效（见上「运行时配置」）。用户账号管理（邀请、用户列表、提权）通过 `/admin/api/*` 端点操作，凭证为 admin user 的 session token（`user.is_admin=true`）。前端对应 `desktop/frontend/src/components/admin/{Config,FeishuConfig,Users,Invitations}.vue`：admin 面板由主 `App.vue` 内嵌为 `AdminPanel.vue` 主区域视图，经 TabBar admin 按钮切换渲染，不再是独立的 `web/src/admin/` MPA entry / `admin.html`。

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
│                          初始 RESIZE（远端 attach 不打扰）；browser/web 下额外
│                          渲染 aux key row（Enter/Esc/Tab/Ctrl-C/Ctrl-D/方向键）
│                          与图片/文件 hidden input，发送 PASTE_IMAGE/PASTE_FILE
│                          时要求 attached + driver + remote_permission=full
│   ├── PaneGrid.vue       CSS Grid 渲染 1/2/4 cell；远程 pane 右上角 cast badge
│                          + close-pane × 同行 flex 布局；close 按钮 mousedown
│                          不冒泡，避免先激活其它 pane 再关错目标
│   ├── SessionPickerDialog.vue ⌘⌥N 触发：local + remote 已有 session 选一个进 pane
│   ├── SettingsDialog.vue relay 配置 + Updates 区（current / latest / 进度 /
│                          autocheck toggle / release notes / 三个按钮）
│   ├── ConfirmInstallDialog.vue force install & restart 确认弹窗，列出会被
│                                 终止的本地 session 数 + 远端 detach 数
│   ├── RemoteSessionsDialog.vue cast 面板：以 tab 形式打开远端 session
│   ├── TaskSidebar.vue    会话侧栏容器：header 内联 `<input type="search">`
│                          （`Cmd/Ctrl+F` focus，`Esc` 清空）+ `defineExpose({
│                          focusSearch })`；`.title` ellipsis 应对窄侧栏
│   ├── TaskGroupedList.vue 虚拟 📌 pinned 分组渲染 + 折叠组（既有
│                          `useCollapsedGroups` 模式）+ `matchesSession`
│                          搜索过滤（含完成折叠一并过滤）+ 空态提示
│                          `[data-test="search-empty"]`
│   └── SessionRowMenu.vue 右键会话行菜单：fixed-positioned popover，
│                          Esc/focusout/空白点自关，viewport 溢出翻转
├── composables/
│   ├── useTerminalShortcuts.ts document 级 capture-phase keydown router
│   │                          匹配 e.code（KeyN/KeyW/KeyT/Bracket{Left,Right}）
│   │                          避开 macOS Option-letter dead key 的 e.key 陷阱
│   ├── useSessionPins.ts  module-level `pinnedIds: Ref<Set<string>>` 共享状态
│   │                      + `pin/unpin/toggle/rename/flushNow`（300ms debounce
│   │                      持久化到 `desktop/config.go::PinnedSessionIDs`）
│   ├── useCollapsedGroups.ts 侧栏分组折叠状态（`useSessionPins` 沿用同一
│   │                          module-level ref 共享模式）
│   └── useRecoverySnapshot.ts snapshot 写盘 + `buildSnapshot` 携带
│                              pin 迁移用的 `session_id`（见「会话标识」节）
├── lib/
│   ├── types.ts           LayoutKind/Pane/Tab/SplitDir/FocusDir
│   ├── layout.ts          transitionLayout / closePane / focusNeighbor 纯函数
│                          （27 单测，TDD）
│   ├── proto.ts           帧编解码（与 web/src/shared/ws/protocol.ts 保持协议常量同步）
│   ├── connection.ts      SessionConnection：WS attach + 重连 + 续传 +
│                          sendResize 队列（WS 还在 CONNECTING 时缓存，ws.onopen
│                          跟在 ATTACH 帧后 flush）
│   ├── sessionMatch.ts    `matchesSession(session, q)` 纯函数：title/cwd/
│                          current_command case-insensitive substring；`q`
│                          由调用方预先 trim + lowercase
│   ├── shortcutBindings.ts `sidebar.focus-search`（默认 `Mod+KeyF`）等
│                           快捷键 action 绑定表
│   └── api.ts             Wails bindings 包装（不依赖 generated 文件，走
│                          window.go.main.App.*；含 UpdateState 镜像）
├── platform/              Wails / Capacitor / browser adapter；App 只依赖
│                          `usePlatform()`，不要在其它目录直接 import wailsjs
├── plugins/               右侧插件槽：fileExplorer（本地/远程双源切换 +
│                          编辑 + CRUD + 回收站，受 `remote_permission=full`
│                          门控远程侧）/ translate（legacy quickInput 已删，
│                          由 QuickTemplate 取代）
└── i18n/                  desktop 前端中英 messages + useI18n()
```

`web/` 前端同样是 Vue 3 + TypeScript，Vite 多入口构建，但**主页面不再是独立实现**：`index.html`
挂载的是桌面同一份 `App.vue`（tabs/panes/侧栏/设置全部复用，见上一节），只有登录前后的少数独立
小页面还是各自的 MPA entry：

```
web/src/
├── main-web.ts            index.html 入口：仅 `import
│                          '../../desktop/frontend/src/main.web.ts'`
│                          （相对路径 import 而非 <script src>，因为 Vite/浏览器
│                          都会把 index.html script 标签的 "../.." 路径钳制在
│                          server root；同源模块 import 则会被 Vite 解析走
│                          `/@fs/` 机制，见 main-web.ts 顶部注释）
├── login/ signup/ setup/  auth 与移动 relay bootstrap（独立小页面，非 App.vue）
├── firstrun/              首次运行引导页
└── shared/                api clients、ws protocol、i18n、Naive theme、
                           `sync/prefsSync.ts`（web 侧 `PrefsSyncEngine`，见下）
```

`web/src/main/`、`web/src/settings/`、`web/src/admin/` 不再承载主体验；不要在这些路径复活第二套会话列表、设置页或管理后台。需要补 web-only 行为时，优先在 `desktop/frontend/src/platform/web.ts` 实现 `Platform` bridge，或在桌面组件里用 `platform.caps` 做能力分支。

`desktop/frontend/src/main.web.ts` + `desktop/frontend/src/platform/web.ts` 是桥接层：
`platform/web.ts` 用 `localStorage` + 同源 `apiFetch` 实现 `Platform` 接口（`getPins`/
`setPins` 等），让同一份 `App.vue` 在 web 下无需 Wails 绑定即可跑（Capacitor 走的是独立的
`main.capacitor.ts` + `mobile/MobileApp.vue`，不受影响）。已知 platform-capability gap（非本计划
引入、任务 6.1 已记录为预期行为，未修）：`App.vue` 里少数几处（`refreshTerminalTheme` 等 boot
step、`getStartupError`、`getCommandNotifyThresholdSeconds`）直接调 `lib/api.ts` 的 Wails-only
`bindings()` 而不是走 `platform` 抽象，在 web 下会各打一条**非致命** console error/warning
（`Wails 绑定尚未就绪`），不影响页面渲染或功能。

**pin 跨端同步**：`useSessionPins` 的 pin 变更（web 侧存 `localStorage`
key `atterm.pinned_session_ids.value`）触发 `platform/web.ts::sessions.setPins` →
`@webshared/sync/prefsSync.ts::notifyLocalChange('pinned_session_ids')` 标记该 key
dirty；`PrefsSyncEngine`（`web/src/shared/sync/prefsSync.ts`，逻辑镜像
`desktop/frontend/src/lib/prefsSync.ts`）按 per-key `updated_at` 做 LWW pull/push，
经 `PUT/GET /api/me/preferences` 与 relay 上的 `internal/prefssync` 交换；其它客户端
（桌面 `desktop/prefssync_adapter.go`、移动端）下一次 pull tick 拿到新值后触发
`prefs:changed` 事件，侧栏据此重新分组。

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

## Demo 站点（site/）

`site/` 是发布到 GitHub Pages（<https://attson.github.io/atterm/>）的 VitePress 站点：既是面向用户的文档站（`docs/guide/*`），也在首页嵌入**真实**的 `desktop/frontend/src/App.vue` 作为交互 demo。demo 复用桌面前端 + `web/src/shared` 源码（Vite alias 接入，**零侵入**），所有后端由 mock 层实现（Platform / WebSocket 帧 / 文件系统三处拦截），访客无需本地 relay 即可体验会话切换、终端回放、交互输入、文件浏览。demo 是生产系统之外的展示层，与 relay/desktop 运行态完全隔离。完整设计见 [site.md](./site.md)。

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
