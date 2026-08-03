# AGENTS.md

> **Audience**: 在 atterm 仓库里工作的 AI 编码 agent
> **Last updated**: 2026-07-29
> **Status**: stable
> **See also**: [README.md](./README.md) · [docs/spec/architecture.md](./docs/spec/architecture.md) · [docs/spec/auth.md](./docs/spec/auth.md) · [docs/spec/protocol.md](./docs/spec/protocol.md) · [docs/spec/feishu.md](./docs/spec/feishu.md) · [docs/spec/conventions.md](./docs/spec/conventions.md) · [docs/spec/component-style.md](./docs/spec/component-style.md)

atterm = 跨平台终端模拟器 + 内建会话云同步。所有从桌面 app 启动的会话默认可被任意设备的 web/桌面客户端 attach、查看历史、继续输入。核心场景：本机跑 codex/claude 的长 AI 任务，离开工位后用手机/另一台机器接管。

阅读这份文件以快速上手；详细规范见 `docs/spec/`。最近版本和 PR 列表用 `git log --oneline -30` / `gh pr list --state merged --limit 30` 取真相，文档里不再维护流水账。

历史规范（已完成的大初始化系列）：用户账号 + OPAQUE E2EE 套件（M1–M6，至 v0.2.110，spec 见 [docs/superpowers/specs/2026-06-15-relay-e2ee-design.md](./docs/superpowers/specs/2026-06-15-relay-e2ee-design.md)）、移动端任务首页 + Capacitor 8 plugin 三件套（至 v0.2.39）、桌面 UI 抛光与 boot stage 错误隔离（v0.2.71、#150）。各 phase 完成度见 [docs/spec/architecture.md](./docs/spec/architecture.md) §phase 完成度。

v0.3.x 主线新增（本文档已并入相关红线 / 何时改哪里 / 不要做）：远程文件浏览器 + 编辑 + 回收站（#268/#269）、`PASTE_FILE 0x37` 远程文件通道（#266）、relay 多实例栈（realm identity + `relay_instances` 心跳 + `home_instance_url` 路由）+ DB-backed 运行时配置（`relay_config` 表，SQLite / Postgres 双后端，取代原 `relay.json`）、会话侧栏置顶（右键抽顶到虚拟 📌 组，`useSessionPins`）、置顶跨重启迁移（`useRecoverySnapshot` local pane 也写 `session_id` + `executeRestore` 里 `pins.rename` old→new）、侧栏内联搜索（header `Cmd/Ctrl+F` 聚焦，`matchesSession` 按 title/cwd/current_command 过滤，折叠组自动展开、完成折叠一并过滤）、Web/Capacitor 主入口复用桌面 `App.vue`（Settings / Admin 内嵌、右键菜单/多选/置顶复用桌面组件）、web/Capacitor 终端辅助键 + 选择图片/文件粘贴（复用 `TerminalView.vue` + `PASTE_IMAGE/PASTE_FILE`）、桌面启动致命错误非崩溃化（`desktop/app.go::setStartupFatalError` + 前端 `GetStartupError()` 展示可复制失败信息 + 日志路径）、通知点击路由（`desktop/notification_response.go` → 前端定位对应 tab）。相关设计 doc 见 `docs/superpowers/specs/2026-07-20-session-bar-pin-design.md` / `2026-07-23-pinned-session-recovery-design.md` / `2026-07-24-sidebar-search-design.md` / `2026-07-27-web-layout-align-desktop-design.md`。内建 SSH 连接内核（切片 1）：`internal/sshclient`（纯 SSH 客户端，密码/私钥认证 + `keepalive` + known_hosts TOFU）→ `desktop/ssh_host.go::OpenSSHSession` 把远程 shell 作为 `sessionPTY` 经 `AdoptSession` 接入现有接管 + E2EE 管线（**不新增 proto 帧**），binding `NewSshSession` 未知主机返回带指纹的 `HostKeyUnknownError` 供前端 `NewSshDialog.vue` TOFU 确认；凭据用完即弃、不落盘（主机清单持久化/加密属切片 2，E2EE 同步属切片 3，端口转发属后续插件切片）。设计 spec `docs/superpowers/specs/2026-08-02-ssh-connect-slice1-design-draft.md`。SSH 主机清单持久化 + 凭据加密（切片 2）：`desktop/ssh_hosts_store.go` 主机 CRUD（`SSHHost` 非敏感字段入 `config.json`，凭据 JSON blob 入 `com.atterm.ssh-credential.v1` keyring，account=主机 ULID；Add 回滚保护 / Update `cred==nil` 只改非敏感 / Delete 同步清凭据幂等）、`desktop/ssh_host.go::NewSshSessionByID` 按 ID 取凭据复用 `NewSshSession`（无凭据返回 `errCredentialMissing`）、`desktop/ssh_known_hosts.go` 的 `ListKnownHosts/RemoveKnownHost`（解决切片 1 遗留的服务器换 key 场景）、前端 `SshHostsPanel.vue`（TabBar SSH 按钮打开，一键连 + CRUD）。E2EE 跨端同步属切片 3。设计 spec `docs/superpowers/specs/2026-08-02-ssh-host-store-slice2-design-draft.md`。SSH 主机清单 E2EE 跨端同步（切片 3）：`desktop/ssh_sync.go` 用 account_key 派生的 sessionKey（固定虚拟 UUID `sshHostsSyncSessionID`）`SealUnsequenced` 把整个主机清单+凭据 seal 成密文，作为 prefssync value `ssh_hosts_encrypted`（白名单在 `internal/prefssync/sync.go`）同步——relay 只见密文；`prefssync_adapter.go` 注入 `App.accountKeyForSync`，account_key 空时 `ReadValue` 返回 ok=false（仅本地不上传）、`WriteValue` open 后写回 config.SSHHosts + keyring；CRUD 经 `markSSHHostsDirty` 触发 push；整列表 LWW。**seal 的 `sshSyncFrameType 0xF0` 只作 AAD，不上 relay wire（不违反不新增 proto 帧红线）**。spec `docs/superpowers/specs/2026-08-03-ssh-sync-slice3-design-draft.md`。SSH 密钥库(Keys Vault)：`desktop/ssh_keys_store.go` 独立 Key 实体 CRUD(name+私钥+passphrase,私钥入 `com.atterm.ssh-key.v1` keyring,`parseKeyType` 校验+`DeleteSSHKey` 引用保护返回占用主机名)；主机 `AuthKind` 改 `password|key`,key 时用 `KeyID` 引用 Key(**删内嵌私钥**,`sshCredential` 只剩 password)；`NewSshSessionByID` 按 auth_kind 取密码或按 KeyID 取私钥(失效返回 `errKeyMissing`)；Key 含私钥与主机清单打包进**同一** `ssh_hosts_encrypted` E2EE blob 同步(`sshSyncPayload.Keys`)；前端 `SshHostsPanel.vue` 加 Hosts/Keys tab、Keys 卡片网格+抽屉、主机表单认证 `Password|Key` 下拉选密钥库。即席对话框 `NewSshDialog` 仍可粘私钥(auth_kind=key)。spec `docs/superpowers/specs/2026-08-03-ssh-keys-vault-design-draft.md`。

## 仓库布局

```
atterm/
├── cmd/
│   └── atterm-relay/       中央 relay 服务的入口（生产部署的 server）
├── internal/
│   ├── proto/              帧协议定义 + 二进制编解码（跨所有组件复用）
│   ├── ringbuf/            scrollback 环形缓冲（按字节预算丢最老）
│   ├── session/            relay 端 session 模型 + Subscriber fan-out + lifecycle 钩子
│   ├── relay/              relay 服务实现：/agent · /uplink · /client · /api/sessions
│   ├── ptyhost/            纯 PTY 包装（Open/Read/Write/Resize/Wait/Close + Cwd via /proc）
│   └── hostid/             机器持久 UUID（~/.config/atterm/host_id）
├── desktop/                Wails v2 桌面 app（Go 后端 + Vue3+TS+xterm.js 前端）
│   ├── main.go             Wails 入口；ldflags 注入 Version
│   ├── app.go              暴露给前端的 bindings（Session/Relay/Update 三组）
│   ├── relay_host.go       内嵌 mini relay（127.0.0.1:<rand>）+ session helpers
│   ├── uplink.go           lazy 远程上传子系统（控制连 + STREAM_REQUEST 触发上传）
│   ├── updater.go          自动更新（GitHub Releases 拉取 / 下载 / install helper）
│   ├── scripts/            install-{darwin.sh,linux.sh,windows.ps1} (go:embed)
│   ├── config.go           ~/.config/atterm/config.json 持久化
│   ├── uplink_e2e_test.go  端到端协议测试（不依赖 webview）
│   ├── updater_test.go     updater 单测（semver / asset / cache / dev short-circuit）
│   └── frontend/           Vue 3 + Vite + TS（PaneGrid / Settings / plugins / i18n）
├── web/                    Vue 3 + TS + Naive UI 浏览器/PWA 客户端（MPA）
├── mobile/                 Capacitor iOS WebView wrapper（同步 desktop/frontend 的 capacitor 构建）
├── site/                   VitePress GitHub Pages 站点：文档 + 首页交互 demo（复用桌面前端 + mock 后端，零侵入）
├── docs/spec/               规范文档（细节见此目录）
└── .github/
    ├── workflows/          CI/release（desktop 多平台构建 + relay docker）
    └── scripts/            release 打包辅助（deb/dmg/version/checksum 签名）
```

## 关键设计原则（红线）

1. **本地优先**：桌面 app 即使远程 relay 不可达也必须能用。所有跨远程的逻辑放进 `desktop/uplink.go`，不要让本地路径依赖 uplink 状态。
2. **Lazy 上传**：远程 relay 静默时不传 PTY 字节。`session.SetSubscriberLifecycle` 在 0→1/N→0 时触发 STREAM_REQUEST/STOP。新增功能不要破坏这个语义。
3. **会话 id 是权威**：去重、attach、路由都按 `session_id` 走，**不要按 `host_id` 去重**——同一机器多 app 实例会共享 host_id 但 session id 独立。
4. **协议向后兼容**：`internal/proto.Version = 1`。新增帧类型只能用未占用的 `Type` 字节；不能改现有帧的 payload 结构。任何 wire 变更都要更新 `docs/spec/protocol.md`。
5. **internal 包不依赖 desktop/**：依赖方向永远是 `desktop/ → internal/*`。`internal/relay` 通过 `AdoptSession(PtyHost)` 接口被桌面端复用，但本身不能 import 桌面端代码。
6. **PTY winsize 必须在 fork 时设好**：前端的 `predictCellDims`（FitAddon 探针）→ `NewSession(cols/rows)` → `pty.StartWithSize`。子进程从一开始就是终态尺寸，避免开局 SIGWINCH 触发某些 zsh 主题的 `PROMPT_EOL_MARK`。`SessionConnection.sendResize` 在 WS 还 CONNECTING 时排队，TerminalView 比对 expectedCols/Rows 跳过无意义 RESIZE。三件耦合，单独动一个会回归。
7. **更新流程不打扰用户**：`updater.go` 永远手动触发——后台只检查、不静默重启。`InstallAndQuit` 必须由用户在 Settings 里点 "force install & restart" 走过 `ConfirmInstallDialog` 确认才执行。dev 构建（`Version == "dev"`）整个 update 子系统短路。
8. **自动更新必须验签**：release 构建通过 ldflags 注入 `main.UpdateVerifyPublicKey`。下载 asset 后必须先用 Ed25519 验证 `SHA256SUMS.sig`，再校验 asset SHA256；缺公钥、缺 `SHA256SUMS`/`.sig`、签名或 hash 不匹配都必须 fail-closed，不允许 install。
9. **公网 relay 默认安全**：`cmd/atterm-relay` 公网监听时必须提供 `ATTERM_BOOTSTRAP_ADMIN_EMAIL`，否则启动拒绝；缺失 `--origins`/`ATTERM_ORIGINS` 同样拒绝，除非显式 `--dev-insecure`。bootstrap 走 OPAQUE：relay 启动时为该 email 打印一次性 **claim token**（`cmd/atterm-relay/bootstrap_admin.go::CreateClaimToken`），操作员在 `/signup.html` 用它完成注册并自动提为 admin——**没有 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`**，relay 永不接收明文密码（密码只在客户端参与 OPAQUE）。relay 默认加 CSP/security headers，并按 IP 做 HTTP/WS rate limit 与连接数限制。服务端所有鉴权接口都不接受 `?token=`：HTTP 走 `Authorization: Bearer`，浏览器 WS 走 `Sec-WebSocket-Protocol: atterm-token.<token>` 子协议头。完整鉴权模型见 [docs/spec/auth.md](./docs/spec/auth.md)。桌面端默认拒绝非 loopback `ws://`，只有用户在 Settings 打开 insecure mode 才允许。
10. **Web 客户端不依赖 CDN**：`web/` 必须只加载同源构建产物；Vue/xterm/Naive UI 等 npm 依赖由 Vite 打包进同源 assets，并由 service worker 预缓存。不要重新引入外部 CDN script/style，否则 CSP/PWA 离线能力会回归。
11. **远程权限由 owner 决定、relay/host 强制执行**：桌面端通过 `remote_permission` 发布 view/control/full；relay 先拦截越权 `IN`/`RESIZE`/`PASTE_IMAGE`/`PASTE_FILE`/`FS_REQUEST`，desktop uplink 写本机 PTY 或访问本机文件系统前再拦一次。relay 不再有共享只读 token；如需限制某用户只读，在桌面端将该 session 的 `remote_permission` 设为 `view`。
12. **大历史 attach 要可感知**：relay 初始 scrollback 回放必须发 `REPLAY_PROGRESS`，并在 `/client` writer 侧做轻量 pacing，避免桌面/web 客户端长时间只显示 connecting 或卡住。不要移除该帧，wire 变更同步更新 `docs/spec/protocol.md`。
13. **iOS 26 字体栈三件套**：(a) 终端 / UI 字体栈把 `PingFang SC` 等 CJK family 放在 `-apple-system` 之前——iOS 26 WebKit 把 `-apple-system` 声明为含 CJK 覆盖但实际不渲染，导致 `[?]` 方框；(b) 终端字体常量统一在 `desktop/frontend/src/lib/terminalFont.ts`，desktop 与 mobile 共用；(c) UI emoji（⚠ 等）用内联 lucide SVG，不要依赖 U+FE0F 变体选择器。改其中一个就回归，详见 `docs/spec/conventions.md` §移动端字体栈。
14. **OSC 133 D 事件单点抓 summary，sticky non-shell**：`internal/session/applyOSC133Locked` 是 type 分类 + summary 抓取的唯一入口；C 事件按 `ClassifyCommand` 算 type，sticky 规则保证返回 `shell` 时不覆盖已有 non-shell；D 事件 `computeSummary(scroll, now, exitCode != 0)` 生成 `SessionSummary` 并写进 `s.meta`。MetaPayload 必须同时携带 `type` 和 `summary` 字段（前者是 P2.11 漏配的 carry-over）。前端不要再做"sticky type" patch；只在一处实现。
15. **Capacitor 8 plugin 注册三件套**：(a) 自定义 plugin 的 Swift 类必须 conform `CAPBridgedPlugin`（`identifier` / `jsName` / `pluginMethods`）；旧式 `.m` 文件里的 `CAP_PLUGIN` 宏在 Capacitor 8 是 no-op；(b) app-local 自定义 plugin 不走 auto-discovery，要在 `MainViewController.capacitorDidLoad()` 里 `bridge?.registerPluginInstance(...)`，并把 `Main.storyboard` 根 VC 指到 `MainViewController`；(c) 第三方 plugin（`@capacitor/camera` / `@capacitor-mlkit/barcode-scanning` / `@capacitor/keyboard` 等）**必须装到 `mobile/package.json`** —— `cap sync` 只扫 mobile/ 的依赖。装在 `desktop/frontend/` 一边的 JS 能 import 但 native 不会注册，跑起来 `PLUGIN_NOT_AVAILABLE`。验证步骤详见 `docs/spec/conventions.md` §Capacitor 8 plugin 注册。
16. **移动端 IME 标点/数字/空格 capture-phase 接管**：iOS 中文九宫格的 `，。？！` / 数字 / 空格走 `input` 事件（`inputType='insertText'`、`isComposing=false`），xterm 自己的 handler 不 forward。共享 `TerminalView.vue` 在 xterm textarea 的 capture 阶段监听 `input`，对 `insertText && !isComposing && data` 自己 `sendInput(data)` 并 `stopImmediatePropagation`。**不要碰 composition 路径**（pinyin→Hanzi 走 `insertCompositionText`），那是 xterm 的领地，碰了会让中文字双发。
17. **移动端 fit 走 ResizeObserver + viewer 锁尺寸**：旧 MobileTerminal 首屏半屏的根因是 `fit.fit()` 跑在 `.term` 容器还没 settle 之前；现在该逻辑已并入共享 `TerminalView.vue`。必须保留 `ResizeObserver` 监听容器尺寸变化、driver 才 fit；viewer 模式下 `onMeta` 收到 `meta.cols/rows` 时 `term.resize(meta.cols, meta.rows)` 锁到 PTY 尺寸（不跑 FitAddon，匹配 protocol.md §Driver/Viewer）。两条都不要再去掉。
18. **`new WebSocket()` 同步抛出必须被隔离**：`SessionListConnection.openWS()` / `SessionConnection.openWS()` 在调用 `new WebSocket(url, protocols)` 时必须用 try/catch 包裹，并把异常路由到 `handleOpenFailure` —— 它 `console.error` 出 url/protocols/error.name，触发 `onStatus("error")`，按指数退避排重连。WebKit 对非法 URL scheme / 非法 subprotocol 字符抛 `SyntaxError: The string did not match the expected pattern.` 是**同步**的，不像普通连接失败走异步 `onclose`；若漏接，调用者（`App.vue` onMounted boot 链）的 `await` chain 会被击穿，整个启动卡在「正在启动第一个会话…」+ titlebar 红字。详见 `docs/spec/conventions.md` §WebSocket 同步异常隔离。
19. **桌面启动 try/catch 按 bootStage 分阶段**：`App.vue` `onMounted` 里 `refreshTerminalTheme` / `getEndpoint` / `getHostInfo` / `connectLocalSessionList` / `refreshRelayConfig` 顺序执行，必须用单个 `let bootStage = ""` 在每一步前重新赋值，catch 时把 `${bootStage}: ${e.name}: ${e.message}` 写进 `errorMsg`，同时 `console.error('[boot] step "${bootStage}" failed', { name, message, stack })`。否则一锅端的 catch 把五个调用塌成同一条不可读错误，下次再出 DOMException 又得猜。详见 `docs/spec/conventions.md` §启动 try/catch 分阶段。
20. **OPAQUE = 全端统一 `github.com/bytemare/opaque`（P-256-SHA256 + Scrypt）**：套件 + server identity 由 **`internal/opaquesuite`** 一处定义（`Config()` + `ServerIdentity = "atterm-relay"`），三端共用——desktop SDK `internal/e2eeclient`、relay server `internal/relay/opaque_server.go`、以及 **web 浏览器端 `cmd/opaque-wasm`(编译成 WASM)**。**绝对不要让某一端换 OPAQUE 实现**:历史上 web 用过 `@cloudflare/opaque-ts`,它跟 bytemare **不能跨端互通**(客户端 AKE 密钥对派生方式不同——`hashToScalar(seed,…)` vs bytemare 的 `oprf.DeriveKey(seed,"OPAQUE-DeriveAuthKeyPair")`,库内部,改配置救不了),导致 web 注册的账号桌面登录报 `invalid credentials`。现已全部统一到 bytemare。`web/src/shared/lib/opaque.ts` 只保留 account-key 的 noble AEAD 包裹 + 会话内容解密(非 OPAQUE);OPAQUE 协议走 `web/src/shared/lib/opaqueWasm.ts` → WASM。**WASM 二进制入 `web-dist`,必须可复现**:wasm 构建抽到 `scripts/build-opaque-wasm.sh`(web 与 mobile 共用),`-trimpath -buildvcs=false` 两个都不能少——`-buildvcs=false` 关掉 Go 自动把 git HEAD SHA 戳进二进制(PR CI 检出的是临时 `refs/pull/N/merge` commit,SHA 每跑必变,会级联到所有 `?url` 引它的 JS chunk),`GO_VERSION` 在 CI 里**精确**钉死(否则 embed drift gate 红)。跨端互通由 `web/tests/unit/opaque-interop.test.ts`(WASM 客户端打真实 relay,需 `$ATTERM_RELAY_URL`)守护。`desktop/frontend`(Capacitor/iOS)**也已统一到同一 WASM**:`src/lib/opaqueWasm.ts`(web 同名文件的镜像副本)+ `platform/capacitor.ts` 用同一 `cmd/opaque-wasm` 二进制,由 `build-opaque-wasm.sh` 在 `prebuild:capacitor` 阶段构建进 `src/lib/`(gitignore,只有 vite 产出的 hash 副本随 `mobile/www` 走;mobile 不入 drift gate,本机 go 版本即可)。**web/desktop/iOS 三端账号自此完全通用。** 详见 [docs/spec/auth.md](./docs/spec/auth.md) §OPAQUE。
21. **account_key 永远不出主线程 / 不进 URL / 不进日志**：浏览器把 32B `account_key` 存 `sessionStorage`（tab 关掉就丢），桌面用 `zalando/go-keyring`，iOS 用 `AttermSecureStorage` Keychain plugin。SW 解密 push body 只能通过 `MessageChannel` 向可见 client 请求（M6-sw 的 `web/src/shared/sw-bridge.ts`），**不要**把 key 写进 IndexedDB / postMessage 广播 / GET query。同理 `password` / wrap 中间产物也不允许 log。
22. **AAD 鉴别字节是 cross-type replay 的唯一防线**：每个 sealed 信封的 AAD = `uuid(16B) || frame_type(1B)`。当前分配：`OUT=0x03` / `META=0x05` / `LIST_RESP/SessionInfo=0x12` / `COMMAND_EVENT=0x35`。加新 sealed 帧类型时**必须**给新 frame_type 一个唯一字节，并在 [docs/spec/protocol.md](./docs/spec/protocol.md) §E2EE 信封 加一行，否则别处的信封能被替换重放。
23. **agent seal 成功后必须 strip plaintext**：`desktop/uplink.go` 的 `SendCommandEvent` / `uplink_seal_fields.go` 的 SessionInfo/META 封装都遵守同一模式——seal 成功就把对应明文字段（`Title/Cwd/Command/CurrentCommand/Label/ExitCode/ElapsedMS`）写回零值。**不要**留一份明文给 relay 做 fallback——那会让 webhook/push 路径走回明文分支，绕过 E2EE。Seal 失败（短 key、cipher 出错）才走 fallback 路径。
24. **不要给 OPAQUE 加密码恢复 / step-up 之外的特权门**：spec 明确不做密码找回，admin reset 等于丢密钥；任何"备用问题/邮件链接"都会把整个 E2EE 模型降级回服务端可读。唯一的特权操作（DELETE /api/me 等）通过 step-up token（60s 单次有效，由再走一次 OPAQUE login 换取，见 `internal/relay/opaque_stepup.go`）。
25. **proto.Frame 拓展遵守"opaque field 可加, 含义不可改"**：`SessionInfo.Sealed []byte` / `MetaPayload.Sealed []byte` / `CommandEventPayload.SealedBody []byte` 当前已落地；新增 sealed payload 时遵循同一三件套（payload struct 加 `Sealed`/`SealedBody` 字节段、agent seal helper、客户端 open helper）。`Sealed` 字段为空 = 未加密；非空时客户端**必须**在拿到 `account_key` 后才信任内容，旧版客户端可以忽略并 fall back 到外层明文字段。
26. **relay 运行时配置以 DB `relay_config` 表（`AdminConfig`）为准、能热生效就不重启**：`relay.json` 已下线（`docker-compose.yml` 明确写"no longer written or read"、`grep -rn "relay.json" internal/ cmd/` 非测试文件零命中）。存储实现在 `internal/userstore/relay_config.go`（singleton row `id=1`，SQLite 或 Postgres 双后端，按 `ATTERM_RELAY_DB_DRIVER=sqlite|postgres` 选择；DSN 走 `ATTERM_RELAY_DB_DSN`）。多实例部署所有 relay 指向同一外部 Postgres，config 变更通过 `internal/relay/config_refresh.go` 的版本轮询（默认 ~10s TTL）向其它实例传播。origins / 限流 / 连接数 / debug / 飞书都在 `internal/relay/admin_config.go` 的 `AdminConfig` 里，经 `/admin/api/config` 与 `/admin/api/feishu`（`admin_http.go`）读写并**热应用**——origins/debug 走 `Server` 上的 `atomic.Pointer`/`atomic.Bool`，飞书走 `Server.ApplyFeishuConfig`（建/拆 secret cipher + handler）。唯一需重启的是 VAPID subject（`webpush.Open` 一次性消费）+ per-instance 的 rate_limit / max_connections（当前仍是进程内 map）。四条配套铁律：(a) **DB 必须能在无 secret cipher 下打开**（`userstore.Open` 不强制传 cipher，飞书未启用时 cipher 为 nil，仅飞书 CRUD 报错）——`ATTERM_FEISHU_ENCRYPT_KEY` 不再是启动必填，否则又会回到崩溃重启循环；(b) 飞书路由（`/v1/feishu/*`）在有 store 时**无条件注册一次**、闭包内按 `feishuRuntime.handler.Load()` 门控（`http.ServeMux` 不支持注销路由，禁用时绑定路由返 503、事件路由返 404）；(c) 飞书主加密密钥**持久化在 DB `relay_config.feishu_secret_key`**——GET 只回显末 4 位、绝不返回明文、绝不写日志；换 key 会让旧飞书绑定无法解密，故 PUT 默认拒绝轮换、需 `force:true`。env 仅作首启一次性播种（DB 值优先）；(d) 首启只需 `ATTERM_BOOTSTRAP_ADMIN_EMAIL` + `ATTERM_ORIGINS` + `ATTERM_RELAY_DB_DRIVER/DSN`，其它 env 变量在 admin UI 里改就行。
27. **OPAQUE 需要「安全上下文」，所以浏览器必须经 HTTPS 访问 relay**：浏览器只在 HTTPS 或 `localhost` 暴露 `crypto.subtle`（`@cloudflare/opaque-ts` 用它做哈希/HMAC），明文 HTTP 公网 IP 上 `crypto.subtle===undefined`，注册/登录在浏览器本地直接抛错、**连请求都发不出**（Network 为空）。`cmd/atterm-relay` 起两个监听：HTTP（`--addr :8080`，给反代后端 / loopback 开发）和 HTTPS（`--https-addr`，浏览器直连）。**没有自签回退**：开了 `--https-addr` 就必须提供真证书 `ATTERM_TLS_CERT`/`ATTERM_TLS_KEY`（缺失即 fatal，逻辑在 `buildTLSConfig`）；否则用 HTTP 端口在前面挂 TLS 终止反代（Cloudflare/Caddy/nginx/Tailscale）。生产单端口即 `--addr "" --https-addr :443` + 真证书。**不要**为了「能在明文 HTTP 跑」去掉 WebCrypto/换纯 JS OPAQUE 库——既改动 E2EE 核心（红线 #20 两端套件写死），又因 token/wrap 明文过网而并不安全。**也不要**重新引入自签证书：WebView 的 WebSocket 无法在代码层信任自签名证书（WKWebView 无 app 级钩子）。

28. **AI 会话恢复 "never resume the wrong conversation"**：`desktop/recovery_store.go` + `recovery_types.go` 的 `RecoverySnapshot` 记每个 pane 的 `ai.{kind,session_id,captured_at_unix}` / `last_command_line` / `last_cwd` / `title`；只有 `ai.session_id` 是真凭据，`aider` 无 sid 时改重放整条 `last_command_line`。sid 来自 `desktop/ai_sid_sniff.go`（session 首次 spawn 后由 OSC 133 D 触发分类 + 对应 jsonl 文件 mtime 监听）+ `desktop/ai_sid_resolve.go`（cwd 切换后按新 cwd 重试）；claude 走 `~/.claude/projects/<sanitized-cwd>/<sid>.jsonl`，codex 走 `~/.codex/sessions/YYYY/MM/DD/rollout-*-<sid>.jsonl`。**抓不到优于抓错**：抓不到就让 ai badge 留空、恢复时不注入 resume、用户得到普通 shell；抓错会把另一个对话的历史 leak 进恢复后的 agent。恢复时 resume 注入由 Go 侧 `desktop/relay_host.go::SetOnFirstPrompt` 直接写 PTY，保留原启动命令的 flag（`--permission-mode` 等），**不要**移回前端用 `sendInput` 一次发完整 `"<cmd>\r"` —— Codex 会把 CR 当 paste 解（同类教训 PR #63 → #110 → #129 来回三次）。前端 `useRecoverySnapshot` 写盘 + `App.vue::pendingLocalIds` + `lib/localListMerge.ts` 三件套保护刚 seed 的 sid 不被 stale `LIST_RESP` 推翻（PR #240）；恢复 attach 给远端 pane 默认 `isDriver = props.isLocalSession ?? true` —— 本机本地 driver，远端 viewer，避免乐观默认 driver 但 relay drop IN 帧的「看着能输入但不通」。

29. **发布 tag 路线分线管理**：仓库可能并行多条 release line（如 `v0.2.x` / `v0.3.x`），但**不要再用分支名硬编码版本线**；当前 `main` 已进入 `v0.3.x`。默认发布线由**当前 HEAD 可达的最新 SemVer tag line**决定，维护老线时必须显式指定目标 line。每条线的 patch 版本独立递增。**算下一个 tag 必须按"reachable from 当前 HEAD"过滤**：

    ```bash
    # 先确认当前分支/HEAD，再按目标 line 查 latest
    git rev-parse --abbrev-ref HEAD
    git tag --list 'v0.3.*' --merged HEAD --sort=-v:refname | head -1
    git tag --list 'v0.2.*' --merged HEAD --sort=-v:refname | head -1

    # 默认：选择可达的最高 major.minor line 递增 patch
    # 维护老线：显式选择对应 pattern，例如 v0.2.*
    ```

    **不要**用全局 `git tag --sort=-v:refname | head -1`，也不要只凭分支名猜版本线。`ship-release` skill 默认是单条主线假设，在这个仓库里**必须**手动校正：发布前先确认 HEAD 可达哪些 `vX.Y.*` tag，默认沿最高可达 line 递增；如果用户要求维护老线，才按用户指定 line 过滤算 next。release CI（`.github/workflows/build.yml` 的 release job）在 `v*` tag push 时触发，不区分版本线，所以 tag 错就立刻有 artifact 错。删 tag / release 是 destructive 操作，未经用户确认不得执行。

30. **飞书 anchor card 生命周期三件独立**：一张 anchor card 上有 `anchor_body_md` / `anchor_input` / `anchor_buttons` / `anchor_askform` 四个子元素，其中 input / buttons / form 三者的 **DELETE 与 CREATE 各自幂等**、各自由自己的 mounted 标记（`CardAnchor.CurrentInputID != ""` / `ButtonsMounted` / `FormMounted`）门控。挂 AskUserQuestion form 时会先 DELETE input 和 buttons（form 独占交互面）、拆 form 时按状态**独立**重建 input 和 buttons —— **不要**把三件事合并成"要么全挂要么全拆"，否则任一 CREATE 失败会短路后续 restore；也**不要**在 `swapAnchorButtons` 里对已删的 `anchor_buttons` 做 PATCH（会拿到飞书 `code=300313 not find elementID`），必须先看 `anchor.ButtonsMounted` 短路（见 `desktop/relay_host.go::swapAnchorButtons` + `updateAnchorAskForm` + `deleteAnchorForm`）。序列号所有 CREATE / DELETE / PATCH 都要在 `anchor.SendMu` 内分配，防止飞书 monotonic sequence 交叉（`code=300317`）。

33. **relay 多实例 = realm + instance registry + home_instance 路由**：`internal/userstore/realm.go` 里 `relay_realm_state`（singleton row）持有整套 relay 的 `realm_id`（首次启动生成，永不变），登录响应会带 `realm_id` + `home_instance_url` 一起下发（`internal/relay/opaque_auth.go`）。多实例场景下每个 relay 通过 `ATTERM_RELAY_INSTANCE_PUBLIC_URL` 声明自己的外部可达 URL，`internal/userstore/relay_instances.go` 定期心跳到 `relay_instances` 表；`internal/relay/node_home.go::resolveHomeInstanceURL` 按用户 `home_instance_url` 字段返回登录后应连的实例（未设则任选活跃实例）。三条铁律：(a) `realm_id` 只从 DB 读、不从 env 覆盖——同一物理集群必须共享同一 realm，否则 E2EE `account_key` 派生会跨实例失效；(b) `home_instance_url` 是**用户偏好**而非强绑定，客户端登录响应拿到该 URL 后自己 reconnect；(c) 加新实例只需指同一 Postgres 就行，不要在实例之间拉 gossip / 直连——所有共享状态都通过 DB 表（含 `relay_config` 版本轮询、`relay_instances` 心跳）。改 realm / instance registry 请同时更新 `docs/spec/architecture.md` §多实例 与 `docs/spec/auth.md` §登录响应字段。

34. **AI 会话恢复扩展：pin 迁移 + sidebar-viewer 三态**：延续红线 #28，`useRecoverySnapshot.buildSnapshot` 现在对**纯 local pane** 也写 `session_id`（"上世代 id"，仅用于 pin 迁移，不触发 remote-rebind 分支）；`executeRestore` spawn 出新 sid 后必须调 `useSessionPins.rename(oldSid, newSid)` + 收尾 `await pins.flushNow()`，把老 pin id 承接到新 sid 上，否则本机 pane 重启后 pin 全部漂成孤儿。**Sidebar-viewer on local host**（`p.remote===true` 但 `info.host_id===本机` → `persistAsRemote===false`）是**第三态**：snapshot 里 `session_id` 故意不写、`executeRestore` 里 `oldSid` 为空跳过 rename——因为该类会话的原 sid 属于另一实例的 relay，把它迁到新本机 sid 是跨实例语义漂移。三种 pane 判定见 `docs/superpowers/specs/2026-07-23-pinned-session-recovery-design.md` §4.1 三态表，改前必读。

35. **桌面启动 fatal 不再崩进程 = `setStartupFatalError` + StartupError UI**：延续红线 #19，`desktop/app.go` 在 relay host 启动 / 日志系统初始化失败时，**不再** `log.Fatalf`，而是 `setStartupFatalError(msg, logPath)` 记入 `startupFatal *StartupError` 字段，让 webview 还能起来。前端 `App.vue` 除现有 5 步 bootStage 外新增 `loadRecoverySnapshot` 步（在 `getHostInfo` 与 `connectLocalSessionList` 之间），并在 boot 末尾拉一次 `GetStartupError()`，展示 `startupFailureCopy` 按钮 + 日志路径，让用户能直接复制到 issue。**不要**再在 desktop/ 里加裸 `log.Fatalf`；除测试外，任何"没这个就跑不动"的初始化失败都要走 `setStartupFatalError` + return，而不是崩进程。

36. **Web 主入口复用桌面 App，不再维护第二套终端 UI**：`web/src/main-web.ts` 只桥到 `desktop/frontend/src/main.web.ts`，主界面的 tabs / panes / `TaskSidebar` / `TerminalView` / `SettingsDialog` / `AdminPanel` 都来自 `desktop/frontend/src/`。改 Web 端会话列表、右键菜单、置顶、多选、终端输入、文件/图片粘贴、Settings 或 Admin 时，优先改桌面组件和 `platform/web.ts` 桥接；不要重新创建 `web/src/main/`、`web/src/settings/`、`web/src/admin/` 的第二套主界面。浏览器端终端辅助键和文件/图片选择也在 `TerminalView.vue` 内按 `platform.caps` gated：只给 browser/web 显示，发送 `PASTE_IMAGE`/`PASTE_FILE` 必须要求 attached + driver + 非 view-only + `remote_permission=full`。

31. **AskUserQuestion form 按键模型是反向工程的，改前必读 memory**：`desktop/feishu/service.go::buildQuestionStrokes` 里 stroke plan 每题分 4 分支（单选 / 多选 / 单选+custom / 多选+custom），每种发的按键序列不一样（数字键单键 / 数字键 + Tab / 数字键 + Enter / 数字键 + ↓ 走位 + Enter / 数字键 + ↓ + Enter on Submit button）—— 这套模型来自反编译 claude-code 2.1.168 二进制找到的 `if (X && Y) { Y(D); return; }` 分支 + 用户手动实测。**改动这个函数前**必须先读 `~/.claude/projects/-Users-attson-code-github-com-attson-atterm/memory/feedback_askform_key_model.md`（血泪史 + 拒绝方案清单）和 `feedback_askform_permission_grant.md`（本机 claude 需先"Yes, and don't ask again" 授权 AskUserQuestion 才能远程回答，否则第一对 `1\r` 被 permission dialog 吃掉，最后一题空 → tool wedges）。**不要**再花时间试 delay tuning / Right-arrow pump / Ctrl+Return via LF —— 都测过不通。这两条见 memory index。

32. **飞书 form widget 状态按 element_id 缓存**：飞书 CardKit v2 客户端把 `select_static` / `input` 的用户选择、输入值按 `element_id` 缓存 —— DELETE + CREATE 用同一个 element_id **不清缓存**，第 2 次挂 form 会加载上次 submission 的下拉和 txt。修法在 `internal/feishu/anchor_card.go::RenderAskQuestionForm` 里给每个 widget 的 element_id 加 mount seq 后缀（`askform_q0_sel_<seq>` / `askform_q0_txt_<seq>`），`seq` 来自 `anchor.PatchSeq` 分配（`relay_host.updateAnchorAskForm` 已经在做）。改这块儿时**不要**把 seq 后缀去掉，也**不要**只旋转部分 widget id —— 部分同名部分不同名会让 form 上一半空一半有历史值。同理 claude AskUserQuestion Type-something 的 TUI 层稳定丢最后一个字符（用户手动键盘也丢，不是 stroke 问题）—— workaround 是给 `sl.txt` 末尾补一个空格作牺牲字符（`buildQuestionStrokes` 已做）。这是 upstream 的锅，不要拆掉 workaround。

## 开发命令

```bash
# macOS Homebrew 的 gh 在 /opt/homebrew/bin；Codex/CI 的非交互 shell
# 不一定加载你的 ~/.zshrc，所以本项目命令默认显式带上它。
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH

# 一次性：启用仓库内 git hooks（含 web-dist drift 守护，避免 web/ 改动
# 漏掉 internal/relay/web-dist/ 重建后被 CI 兜底打回）。
git config core.hooksPath .githooks

# 命令行 relay（本地调试；--dev-insecure 跳过 Origin 校验，loopback 时 bootstrap env 可省略）
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure
# 需要 admin：设 ATTERM_BOOTSTRAP_ADMIN_EMAIL，relay 启动会打印一次性 claim token，
# 在 /signup.html 用该 email + claim token 注册即提为 admin（无密码 env）。
# --web flag omitted ⇒ uses the embedded FS at internal/relay/web-dist/.
# For production-like frontend dev: build once and serve from disk:
#   cd web && npm run build && cd .. && go run ./cmd/atterm-relay --web web/dist ...
# For live web frontend dev: run `cd web && npm run dev`; Vite at 5173 proxies
# /api, /admin/api, /agent, /uplink and /client to 127.0.0.1:8080.

# 桌面 app
cd desktop
wails dev -tags webkit2_41               # Linux 必须加 -tags webkit2_41
wails build -tags webkit2_41             # 出 desktop/build/bin/AT Term

# 测试
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./desktop/   # 跑 lazy uplink e2e 协议测试
cd desktop/frontend && npm run build               # 前端 type-check + build
cd desktop/frontend && npm test                    # 桌面 Vue/Vitest 单测
cd web && npm run build && npm test && npm run test:contract
cd mobile && npm test                              # Capacitor wrapper sync 脚本测试

# 查看 GitHub Actions / release 状态（如 PATH 未加载，可直接用 /opt/homebrew/bin/gh）
gh run list --repo attson/atterm --limit 10
```

环境变量（relay 运行时配置全部下沉到 DB `relay_config` 表 + 管理后台，env 只是「核心」或「首启一次性播种」）：

核心：
- `ATTERM_BOOTSTRAP_ADMIN_EMAIL`：启动时为该 email 打印一次性 claim token（7 天 / 单次）；用它在 `/signup.html` 注册**新**账号即提为 admin（该 email 已注册则 token 无法消费，改用 SQL 提权）。公网监听必填，除非 `--dev-insecure`。**无 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`**——已随 OPAQUE 迁移移除
- `ATTERM_ORIGINS`：逗号分隔的浏览器 WS Origin 白名单；公网必填（除非 `--dev-insecure`）。首启播种进 `relay_config`，之后可在 Admin → Config 改
- `ATTERM_RELAY_DB_DRIVER`：`sqlite`（默认）或 `postgres`。多实例部署必须 `postgres`
- `ATTERM_RELAY_DB_DSN`：SQLite 模式下是文件路径（默认 `<config-dir>/users.db`），Postgres 模式下是 `postgres://user:pw@host:port/db?sslmode=...` DSN
- `ATTERM_RELAY_CONFIG_DIR`：SQLite 模式下的持久化目录（`users.db` 落这里，也是内部临时文件目录），默认 `./data/atterm-relay`。Postgres 模式下**不使用**这个目录做数据库存储，但仍是运行时临时文件的根目录
- `ATTERM_RELAY_INSTANCE_PUBLIC_URL`：本实例外部可达 URL（例如 `https://relay-a.example.com`）；多实例部署必填——每个实例声明自己的 URL，写入 `relay_instances` 心跳表并作为 `home_instance_url` 派发的一员。单实例部署可省

可选（不设也能启动；首启一次性播种进 `relay_config`，之后以管理后台为准）：
- `ATTERM_FEISHU_ENCRYPT_KEY`：飞书凭据字段加密密钥（base64 32B）。**不再必填**——Admin → Feishu 可生成；DB 在无 cipher 下也能开，仅启用飞书时才挂载
- `ATTERM_FEISHU_BASE_URL`：飞书 Open Platform base URL（默认 `https://open.feishu.cn`）
- `ATTERM_VAPID_SUBJECT`：Web Push VAPID subject（改动需重启）
- `ATTERM_RATE_LIMIT_PER_MINUTE` / `ATTERM_MAX_CONNECTIONS_PER_KEY`：限额；`0` 默认值、负数禁用。**per-instance**（不 DB-backed），改后需重启该实例
- `ATTERM_RELAY_DEBUG` / `ATTERM_RELAY_DEBUG_PAYLOAD`：详细日志（现在 Admin → Config 也能热开关）

其它：
- `ATTERM_RELAY_URL` / `ATTERM_RELAY_TOKEN`：桌面 app 首次启动时若无配置文件，从这俩 env 读初始值
- `ATTERM_HOST_ID`：覆盖 host id 文件（容器场景）
- `ATTERM_UPDATE_VERIFY_PUBLIC_KEY`：GitHub prod environment secret；base64 Ed25519 公钥，release 构建时注入桌面 app
- `ATTERM_UPDATE_SIGNING_PRIVATE_KEY`：GitHub prod environment secret；base64 Ed25519 私钥，只在 release job 里签 `SHA256SUMS`

## 何时改哪里

| 修改场景 | 触动文件 |
|---------|----------|
| account / token / invitation / pairing / webpush 数据库操作 | `internal/userstore/`（users / apitokens / invitations / websessions / pairing_tokens / webpush subscriptions）；Store 接口变更需同步更新所有实现和 `store_iface_test.go`。历史 `webhooks` 表已由 sqlite `0006_drop_webhooks.sql` 删除，不要再新增 `/api/me/webhooks` UI |
| 新协议帧类型 | `internal/proto/frame.go` + 协议规范 + 接收方 |
| 改 relay session 行为 | `internal/session/`（local + mirror 都用同一个 Session） |
| 桌面新 binding | `desktop/app.go`（手写 + Wails 自动生成 `frontend/wailsjs/go/main/App.*`） |
| 新前端组件 | `desktop/frontend/src/components/` |
| 桌面端协议路径 | `desktop/uplink.go` 或 `desktop/relay_host.go` |
| 改 pane 布局 / 分屏键 | `desktop/frontend/src/lib/layout.ts`（纯函数 + 单测） + `composables/useTerminalShortcuts.ts`（document capture）+ `components/PaneGrid.vue` |
| 改自动更新 | `desktop/updater.go`（state machine + Ed25519/SHA256 校验）+ `desktop/scripts/`（平台 helper）+ `.github/scripts/sign-release-checksums.go` + `.github/workflows/build.yml` + Settings UI |
| 改 relay 启动安全策略 | `cmd/atterm-relay/main.go` + `cmd/atterm-relay/main_test.go` + `internal/relay/*_test.go` + `docs/spec/protocol.md` |
| 改 web 安全头 / 静态资源 | `internal/relay/server.go` + `web/src/...` (Vue 3 + Naive UI) + `web/tests/contract/*.mjs` |
| 改 web 文案 / 多语言 | `web/src/shared/i18n/messages/*.ts` + `desktop/frontend/src/i18n/messages/*.ts`；新增用户可见文案时同步中英两套 |
| 改 web 主界面 / 终端 / 侧栏 / Settings / Admin | `desktop/frontend/src/App.vue` + `desktop/frontend/src/components/**` + `desktop/frontend/src/platform/web.ts` + `web/src/main-web.ts`；主 App 复用桌面组件，不新建第二套 `web/src/main`/`settings`/`admin` 主界面 |
| 改 web 端终端快捷键 / 选择图片 / 选择文件 | `desktop/frontend/src/components/TerminalView.vue`（browser-only aux row + hidden file inputs）+ `desktop/frontend/src/lib/auxKeys.ts` + `desktop/frontend/src/lib/connection.ts`（`sendPasteImage`/`sendPasteFile`）+ `desktop/frontend/src/platform/web.ts` + i18n；文件/图片按钮必须 gated 到 driver + `remote_permission=full` |
| 改桌面远程 relay 配置 | `desktop/app.go` + `desktop/config.go` + `desktop/relay_security.go` + `desktop/frontend/src/components/SettingsDialog.vue` |
| 改远程权限模型 | `internal/proto/frame.go` + `internal/relay/permissions.go` + `desktop/uplink.go` + Settings UI + 协议规范 |
| 改 relay admin 配置（限流 / origins / debug / 飞书 / VAPID） | `internal/userstore/relay_config.go`（DB schema + Get/Set + version bump）+ `internal/relay/admin_config.go`（`AdminConfig` 字段 + validate + `LoadFromDB`）+ `internal/relay/config_refresh.go`（多实例版本轮询）+ `internal/relay/admin_http.go`（`/admin/api/config`、`/admin/api/feishu`、generate-key）+ `internal/relay/server.go`（热应用：atomic origins/debug、`ApplyFeishuConfig`、飞书路由门控）+ `internal/userstore/store.go`（`SetSecretCipher`）+ `cmd/atterm-relay/main.go`（env→config 播种）+ `desktop/frontend/src/components/admin/{Config,FeishuConfig}.vue` + `web/src/shared/api/admin.ts` + i18n + README/spec。红线 #26 |
| 改 relay 多实例 / realm / home_instance 路由 | `internal/userstore/realm.go`（`relay_realm_state` singleton + `GetOrCreateRealm`）+ `internal/userstore/relay_instances.go`（`relay_instances` 心跳表 + `UpsertInstance`/`ListActiveInstances`）+ `internal/relay/config_refresh.go`（`relay_config.version` 轮询向其它实例传播 admin 配置）+ `internal/relay/node_home.go`（`resolveHomeInstanceURL` + 心跳/活跃列表）+ `internal/relay/opaque_auth.go`（登录响应携带 `realm_id` + `home_instance_url`）+ `internal/e2eeclient/client.go`（客户端解析 `realm_id`/`home_instance_url` 字段）+ `cmd/atterm-relay/main.go`（`ATTERM_RELAY_INSTANCE_PUBLIC_URL` 读入 + realm bootstrap）+ migrations `0007_relay_realm.sql` / `0008_node_selection.sql`（sqlite）与对应 postgres 版本 + `docs/spec/architecture.md` §多实例 + `docs/spec/auth.md` §登录响应字段。红线 #33 |
| 改会话置顶 / 侧栏搜索 | `desktop/config.go`（`PinnedSessionIDs []string`）+ `desktop/app.go`（`Get/SetPinnedSessionIds`）+ `desktop/frontend/src/composables/useSessionPins.ts`（module-level `pinnedIds: Ref<Set>` + `pin/unpin/toggle/rename/flushNow` + 300ms debounce persist）+ `desktop/frontend/src/lib/sessionMatch.ts`（`matchesSession(s, q)` 纯函数：title/cwd/current_command case-insensitive substring；`q` 必须由 caller pre-trim + pre-lower）+ `desktop/frontend/src/components/SessionRowMenu.vue`（fixed-positioned popover，Esc / focusout / 空白点自关，viewport 溢出翻转）+ `desktop/frontend/src/components/TaskGroupedList.vue`（虚拟 📌 pinned 组 + `filteredGroups` 叠 `matchesSession` + `groupKeys` 读 `filteredGroups.length` + `pinnedSessions` 内部条件叠 pin+match + `completedFiltered` + `isGroupCollapsed` 在 `q` 非空时短路 + 空态提示 `[data-test="search-empty"]`）+ `desktop/frontend/src/components/TaskSidebar.vue`（header 内联 `<input type="search">` + `defineExpose({ focusSearch })` + `.title` 使用 `flex: 0 1 auto` + `min-width: 0` + ellipsis 防窄侧栏溢出）+ `desktop/frontend/src/lib/shortcutBindings.ts`（`sidebar.focus-search` action，默认 `Mod+KeyF`）+ `desktop/frontend/src/composables/useTerminalShortcuts.ts`（`onFocusSidebarSearch?` handler）+ `desktop/frontend/src/App.vue`（`taskSidebarRef` template ref + handler）+ i18n `tasks.sidebar.searchPlaceholder` / `tasks.search.empty` / `settings.shortcuts.focusSidebarSearch`。红线 #34 |
| 改桌面通知点击路由到 session | `desktop/notification_response.go`（通知 payload → session_id 解析 + Wails event emit）+ `desktop/app.go`（`ShowNotification` 携带 `data`）+ `desktop/frontend/src/platform/types.ts`（`NotificationBridge`/`NotificationRouteData` 签名）+ `platform/wails.ts`（订阅 event）+ `App.vue`（收到 route data → 定位 tab 或开新 tab）+ `TerminalView.vue`（滚动/聚焦到点击来源）|
| 改远程文件浏览器 / 编辑 / CRUD / 回收站 | `internal/proto/frame.go`（`FS_REQUEST 0x38` / `FS_RESPONSE 0x39` / `FS_EVENT 0x3a` + `PASTE_FILE 0x37`）+ `internal/relay/fs_router.go`（按 host 路由 FS_REQUEST，session_id 权限门控）+ `internal/relay/permissions.go`（`remote_permission=full` 才允许 FS_REQUEST）+ `desktop/fs_host.go`（本机文件系统 CRUD + trash 实现 + `desktop/watcher/`）+ `desktop/uplink.go`（收 FS_REQUEST → 转 fs_host → 回 FS_RESPONSE）+ `desktop/frontend/src/plugins/fileExplorer/`（本地/远程双源 UI + editor）+ `web/src/plugins/fileExplorer/`（同镜像）+ `docs/spec/protocol.md` §FS_REQUEST/RESPONSE/EVENT |
| 改 AI sid 抓取 / codex subagent 跳过 | `desktop/ai_sid_sniff.go`（OSC 133 D 触发 + jsonl 监听）+ `desktop/ai_sid_resolve.go`（cwd 切换重试，同时跳过 Codex subagent restore session——避免把子代理误判成主会话 resume 目标）+ `desktop/relay_host.go::SetOnAIClassified`/`SetOnFirstPrompt`。红线 #28 系列 |
| relay 注册表清理 | `internal/relay/uplink_conn.go`（writer ping fail 触发 cancelConn → cleanup mirror sessions） |
| 改移动 app relay 配置 | Capacitor 入口是 `desktop/frontend/src/main.capacitor.ts` → 共享 `App.vue`。登录 / 退出 / 扫码配对 UI 在 `desktop/frontend/src/components/SettingsAccount.vue` + `SettingsPairingConsume.vue`，URL 校验在 `desktop/frontend/src/lib/relayUrl.ts`，持久化和 OPAQUE 登录在 `desktop/frontend/src/platform/capacitor.ts`。桌面/web 主 App 的 relay 设置仍在 `desktop/frontend/src/components/SettingsRelay.vue` + `desktop/frontend/src/platform/{web,capacitor}.ts` |
| 改桌面前端 ↔ Go IPC | `desktop/frontend/src/platform/wails.ts`（适配器）；新方法先在 `desktop/app.go` 或 `desktop/plugin_*.go` 定义，让 Wails 重生成 `wailsjs/`，再在 `platform/wails.ts` 包一层。**不要**在 `src/platform/` 之外的文件直接 import `wailsjs/*`。 |
| 改命令结束通知 / 外部通知 | 当前现役路径是 Web Push（`internal/webpush/` + `internal/relay/web_push_http.go` + `internal/relay/uplink_conn.go` + `web/src/shared/api/push*.ts`）和飞书（`internal/feishu/` + `desktop/feishu/` + `desktop/frontend/src/components/SettingsFeishu.vue` + `desktop/frontend/src/components/admin/FeishuConfig.vue`）。通用 outbound webhook 已下线：不要复活 `internal/webhook/`、`internal/userstore/webhooks.go`、`/api/me/webhooks` 或 `web/src/settings/tabs/Webhooks.vue` |
| 改 pairing QR 流程 | `internal/userstore/pairing.go`（+ `Store` 接口 + migration）+ `internal/relay/pair_http.go`（`/api/pair/create` + `/api/pair/consume`）+ `internal/relay/auth_http.go`（路由注册 + Limits 钩子）+ `desktop/app.go::CreatePairingToken` + `desktop/frontend/src/components/PairingPanel.vue` + `desktop/frontend/src/components/SettingsAccount.vue` / `SettingsPairingConsume.vue`（Capacitor 扫码 consume + 写 secure storage）+ `desktop/frontend/src/platform/qrScanner.ts` + `mobile/ios/App/App/Plugins/AttermQRScanner/` |
| 改 relay 健康检查 | `internal/relay/health_http.go`（`HealthPayload` + `/healthz` + `/admin/health` + `/admin/api/health`）+ `internal/relay/templates/health.gohtml` + `internal/relay/server.go`（路由注册）+ contract test |
| 改桌面诊断导出 | `desktop/diagnostics.go`（payload collector + redaction）+ `desktop/app.go::GetDiagnostics/ExportDiagnostics` + `desktop/frontend/src/components/SettingsDiagnostics.vue` |
| 改移动端安全存储 | `mobile/ios/App/App/plugins/SecureStorage/AttermSecureStorage.swift`（CAP_PLUGIN，SecItemAdd/CopyMatching/Delete + `kSecAttrAccessibleAfterFirstUnlock`）+ `web/src/shared/secure-storage.ts`（Capacitor bridge + localStorage 降级）+ `web/src/shared/api/relay-config.ts`（迁移路径）；改完一定要在 iOS simulator 验证 keychain 写入 |
| 改 session 类型分类 / 摘要 | `internal/session/classify.go`（关键字 + wrapper 剥离 + sticky）+ `internal/session/summary.go` + `internal/session/ansistrip.go` + `internal/ringbuf/ringbuf.go::TailBytes` + `internal/proto/frame.go`（`SessionInfo.Type` / `SessionSummary` / `MetaPayload.Type` / `MetaPayload.Summary`）+ `internal/session/session.go::applyOSC133Locked`（唯一调用点）+ TS 侧 `desktop/frontend/src/lib/connection.ts` + `web/src/shared/api/types.ts`（mirror SessionSummary）+ 前端任务卡片渲染 |
| 改 AI 快捷模板（含 hotkey / hide bar / 默认值） | `desktop/frontend/src/lib/templates.ts`（`QuickTemplate { id, label, text, hotkey? }` + `DEFAULT_TEMPLATES`：`yes / ok / continue / commit / push / release / 1 / 2 / 3` + `effectiveTemplates`）+ `web/src/shared/templates.ts`（镜像）+ `platform/types.ts`（`TemplateBridge` 含 `loadHidden/saveHidden`）+ `wails.ts` / `capacitor.ts`（hidden 都走 localStorage `atterm.templates.hidden`，desktop 模板列表走 `appConfig.QuickTemplates`，mobile/web 走 localStorage `atterm.templates`）+ `desktop/quick_templates.go`（含 `Hotkey` 字段）+ `wailsjs/go/models.ts`（同步）+ `SettingsTemplates.vue`（含 hotkey 列 + show-bar 开关，emit `quickTemplates:changed`）+ 共享 `TerminalView.vue` template-bar + 直接发送（无预览）+ document-capture keydown 匹配 `hotkey` 触发 |
| 改移动端 AUX 键 / 设置页 | `desktop/frontend/src/lib/auxKeys.ts`（`AuxKey { id, label, seq }` + `DEFAULT_AUX_KEYS` + `parseSeq/displaySeq` 转义解析）+ `platform/types.ts AuxKeyBridge`（capacitor/web localStorage `atterm.auxkeys`、wails no-op、fake 内存）+ `SettingsDialog.vue` / `SettingsTemplates.vue` / `SettingsAccount.vue`（共享设置页窄屏布局、登录/退出/扫码）+ `TerminalView.vue`（browser/Capacitor aux row，订阅 `mobile:shortcutsChanged` / `quickTemplates:changed`） |
| 改移动端 Capacitor plugin（Camera/QR/Keyboard 等） | **必须**装到 `mobile/package.json` 让 `cap sync` 发现；同时装到 `desktop/frontend/package.json` 让 TS import 解析。装完 `cd mobile && npm run ios:open`（已自动跑 `npm install` + `cap sync`）。自研 app-local plugin（如 `AttermQRScanner` / `AttermSecureStorage`）必须按红线 #15 显式注册，验证 `mobile/ios/App/App/MainViewController.swift` 里有 `registerPluginInstance(...)`。第三方 plugin 验证 `mobile/ios/App/App/capacitor.config.json` 的 `packageClassList` 包含新 plugin 名、`mobile/ios/App/CapApp-SPM/Package.swift` 有对应 SPM 依赖。权限串（如 `NSCameraUsageDescription` / `NSPhotoLibraryAddUsageDescription`）加在 `mobile/ios/App/App/Info.plist`。`@capacitor/keyboard` 的 `setAccessoryBarVisible(false)` 在 `desktop/frontend/src/main.capacitor.ts` 启动时调一次 |
| 改移动端 IME / xterm 文本接管 | `desktop/frontend/src/components/TerminalView.vue`：`onImeInput` 函数 + xterm textarea capture-phase 监听。**只**处理 `inputType === "insertText" && !isComposing && data`，权限/driver gate 后 `sendInput(data)` + `stopImmediatePropagation`，清空 textarea。composition 路径（`insertCompositionText`）一律不碰 |
| 改移动端 fit / viewer 锁尺寸 | `desktop/frontend/src/components/TerminalView.vue`：`resizeObserver = new ResizeObserver(() => safeFit())` + `safeFit()`（仅 isDriver 时 fit）+ `applyViewerSize()` / `onMeta` viewer 锁 `term.resize(meta.cols, meta.rows)`。`onBeforeUnmount` `resizeObserver?.disconnect()` |
| 改 titlebar 标题居中 / running indicator | `desktop/frontend/src/components/TitleBar.vue`：`.window-title` 用 `position: absolute; left: 50%; transform: translateX(-50%)`，`.status` 加 `margin-left: auto` 把右侧元素推到边缘；running indicator 走 `.titlebar::after`（伪元素永远存在，`opacity: 0` + `transition: opacity 0.5s`）+ `.titlebar.is-running::after { opacity: 1 }`，`background` 用 `repeating-linear-gradient(90deg, transparent, …, #bbf7d0, …, transparent)` 平铺 1120px 周期、`background-size: 1120px 100%`、`animation: titlebar-running-sweep 3.5s linear infinite` 把 background-position 推到 `1120px 0`。`prefers-reduced-motion` 退化成静态绿条。`App.vue` 把 `currentActiveSession.value?.task_state` 通过 `current-task-state` prop 传进来 |
| 改会话侧栏分组折叠 / 隐藏滚动条 | `desktop/frontend/src/components/TaskGroupedList.vue`：`collapsedGroups: ref<Set<string>>`（用替换 Set 触发响应式）+ `isGroupCollapsed/toggleGroupCollapsed`；header 加 `role="button" tabindex="0"` + `@click` / `@keydown.enter` / `@keydown.space` + `aria-expanded`，caret 在 `▼` / `▶` 之间切换。`v-for` 改成 `(isGroupCollapsed(key) ? [] : groups[key])`。mark-all 按钮加 `@click.stop`，避免点 ✓ 顺带把分组折叠。`desktop/frontend/src/components/TaskSidebar.vue`：`.expanded { flex: 1 1 auto; min-height: 0; display: flex; flex-direction: column }` 让里层 `.list-wrap` 的 `flex: 1; overflow-y: auto` 真正工作；`.list-wrap` 加 `scrollbar-width: none` + `::-webkit-scrollbar { display: none }` 隐藏 gutter |
| 改快捷模板栏滚轮 / 隐藏 gutter | `desktop/frontend/src/components/TerminalView.vue`：`.template-bar` 加 `scrollbar-width: none` + `::-webkit-scrollbar { display: none }`；`@wheel.passive="onTemplateBarWheel"` 把 `deltaY`（或 deltaX）折成 `scrollLeft`，让鼠标用户不按 Shift 也能横向 pan。passive listener 不 preventDefault，xterm 自己的 wheel 路径不受影响 |
| 改桌面启动错误隔离 / 阶段定位 | `desktop/frontend/src/lib/connection.ts`：`SessionListConnection.openWS()` / `SessionConnection.openWS()` 用 `try { ws = new WebSocket(...) } catch (e) { this.handleOpenFailure(e, auth); return; }` 包裹；`handleOpenFailure` 打印 url/protocols/error.name，调 `onStatus("error")`，按 `Math.min(8000, 500 * 2^attempts)` 退避排重连。`desktop/frontend/src/App.vue`：`onMounted` 用 `let bootStage = ""` 在每一步前赋值（`refreshTerminalTheme` / `getEndpoint` / `getHostInfo` / `loadRecoverySnapshot` / `connectLocalSessionList` / `refreshRelayConfig`；六步顺序固定，`loadRecoverySnapshot` 在 `getHostInfo` 与 `connectLocalSessionList` 之间），catch 时把 `${bootStage}: ${e.name}: ${e.message}` 写进 `errorMsg`，并 `console.error('[boot] step "..."', { name, message, stack })`。Boot 末尾还要拉一次 `GetStartupError()`（`desktop/app.go::setStartupFatalError` / `GetStartupError` 返回 `*StartupError{Fatal,Message,LogPath}`）显示 `startupFailureCopy` 按钮 + 日志路径，这两端合起来叫红线 #19 + #35。 |
| 改 OPAQUE 注册 / 登录 / step-up | `internal/relay/opaque_server.go`（套件 + persistence）+ `internal/relay/opaque_auth.go`（register/login HTTP handler）+ `internal/relay/opaque_stepup.go`（60s 一次性 token）+ `internal/e2eeclient/`（Go 桌面 SDK）+ `web/src/shared/lib/opaque.ts` + `desktop/frontend/src/lib/opaque.ts`（mobile mirror）。改套件**必须**两端一起改——见红线 #20 |
| 改 account_key 持久化 / 解锁路径 | 桌面：`zalando/go-keyring` via `desktop/account_key.go` + Wails 绑定 `GetAccountKey` / `setAccountKey` + `EventsEmit("account-key-changed")`。Web：`web/src/shared/api/account-key.ts`（sessionStorage）。iOS：`mobile/ios/.../AttermSecureStorage.swift` + `desktop/frontend/src/platform/capacitor.ts` + `desktop/frontend/src/lib/account-key.ts`（cache 注册表）。改写入/读取路径时**不要**加 IndexedDB / localStorage（红线 #21）|
| 改 sealed 字段 / E2EE 信封 | agent: `desktop/uplink.go::SendCommandEvent` / `desktop/uplink_seal_fields.go` / `desktop/uplink_seal_push.go`（seal + plaintext strip,红线 #23）。relay/通知: `internal/relay/uplink_conn.go::handleUplinkCommandEvent`（透传 SealedBody）+ `internal/webpush/dispatch.go`（sealed push 分支）+ `internal/feishu/card.go::RenderCommandFinishedCard`（sealed card 分支）。client 解密：`web/src/shared/lib/opaque.ts::openSessionFields/openMetaFields/openPushBodyFields` + `web/src/shared/sw-bridge.ts`（SW 解密桥）。`proto.CommandEventPayload.SealedBody` / `SessionInfo.Sealed` / `MetaPayload.Sealed` 已在 `internal/proto/frame.go` 落地；加新 sealed 帧请同时 bump [docs/spec/protocol.md](./docs/spec/protocol.md) §E2EE 信封 的 AAD 表（红线 #22）|
| 改 AI 会话恢复 / sid sniff / resume 注入 / pin 迁移 | `desktop/recovery_store.go`（snapshot schema + atomic write）+ `desktop/recovery_types.go`（`RecoverySnapshot` / `TabSnapshot` / `PaneSnapshot` / `AIInfo`；`PaneSnapshot.SessionID` 对 remote 用于 rebind、对 local 用于 pin 迁移）+ `desktop/ai_sid_sniff.go`（OSC 133 D 触发 + jsonl mtime 监听 + `aiSniffers` 注册表）+ `desktop/ai_sid_resolve.go`（cwd 切换重试、Codex subagent 跳过）+ `desktop/relay_host.go`（`SetOnAIClassified`、`SetOnFirstPrompt` 注入 resume 命令、`computeResumeArgs`）+ `desktop/frontend/src/composables/useRecoverySnapshot.ts`（debounce 500ms / 5s 写盘；`buildSnapshot` 对 remote 与纯 local pane 都写 session_id，sidebar-viewer on local host 故意不写——见红线 #34）+ `desktop/frontend/src/lib/localListMerge.ts`（`pendingLocalIds` 保护刚 seed 的 sid）+ `desktop/frontend/src/components/RecoveryDialog.vue` + `App.vue::executeRestore`（Case A new spawn 后 `pins.rename(oldSid, newSid)` 迁移置顶 + 收尾 `await pins.flushNow()` / Case B remote rebind）。新增 ai_kind 时：Go 加 `aiSniffSpec`（命令 token + jsonl glob）+ `computeResumeArgs` 分支；前端 `lib/aiKind.ts::classifyAIKind` 加 token；`buildRestoreSessionReq` 透传 `ai_kind` + `initial_ai_session_id`。改红线 #28 / #34 |
| 改飞书 anchor card 生命周期 / body streaming / archive | `desktop/relay_host.go`（`updateAnchorAskForm` 挂拆 form + 独立 restore input/buttons、`swapAnchorButtons` 门控 `ButtonsMounted`、`deleteAnchorForm` 独立幂等）+ `desktop/feishu/dispatcher.go`（`InsertAnchorFormWithSeq` / `DeleteAnchorFormWithSeq` / `CreateAnchorInputWithSeq` / `CreateAnchorButtonsWithSeq` typed helpers）+ `desktop/feishu/service.go`（`deleteAnchorForm` 也清 `PendingForm`、`clearAnchorInput` 旋转 element_id）+ `internal/feishu/cardindex.go`（`CardAnchor` 状态标记 `CurrentInputID` / `ButtonsMounted` / `FormMounted` / `PendingForm`）。红线 #30 |
| 改 AskUserQuestion form 按键 stroke plan | `desktop/feishu/service.go::buildQuestionStrokes`（4 分支：单选 / 多选 / 单选+custom / 多选+custom；trailing space 是牺牲字符不要去）+ `desktop/feishu/service.go::handleAskFormSubmit`（parse formValue → slots → 拼 stroke → `Router.InjectKeystrokesBySession`，350ms 间距是硬要求）+ `internal/feishu/router.go::InjectKeystrokesBySession`（每键独立 SendInput，首键 inline 后余键 goroutine）。**改前必读**：`memory/feedback_askform_key_model.md` + `feedback_askform_permission_grant.md`（红线 #31）|
| 改飞书 form 渲染 / widget 结构 | `internal/feishu/anchor_card.go::RenderAskQuestionForm`（signature 含 `mountSeq int64`；每题一行 column_set：select_static 或 multi_select_static + input；widget element_id 必须带 `_<mountSeq>` 后缀）+ `internal/feishu/anchor_card.go::AskFormQuestion`（`MultiSelect bool` + `Options []AskFormOpt`）+ `desktop/feishu/hook_adapter.go::extractAllAskUserQuestions`（`multiSelect` flag 传下去）+ `desktop/feishu/service.go::parseAskFormSlots`（`sel` 单选 string / `selMulti` 多选 []string / `txt` 都 populate）。红线 #32 |
| 改飞书本地模式 vs relay 模式配置分流 | `desktop/app.go`（模式路由 + `SetFeishuBinding` / `GetFeishuStatus` 分流）+ `desktop/feishu/binding_store_local.go`（local 存钥匙串）+ `internal/relay/admin_http.go::/admin/api/feishu`（relay 存 sqlite `users.db` + `AdminConfig.Feishu`）+ `desktop/frontend/src/components/SettingsFeishu.vue`（模式选择 + `SelectDropdown` 深色 UI）+ i18n。红线 #29 系列 |
| 改 demo 站点 / 首页交互 demo | `site/docs/.vitepress/theme/components/mock/*`（Platform / WebSocket 帧 / 文件系统三层拦截 + `fakeSessions` / `mockGoApp` / `replayScripts` / `fakeCommands`）+ `HomeDemo.vue`（挂载前注入 mock，再动态 import 真实 `App.vue`）+ `config.mjs`（vite alias：`@`→桌面前端、`wailsjs`/`opaqueWasm`/`webTabsSnapshot` 桩）+ 对应 `*.test.ts`。**零侵入**：不改 `desktop/frontend` / `web` 源码；改了这些前端接口（Platform / proto 帧 / caps 门控）时 mock 层要同步。细则见 `docs/spec/site.md` |

## 风格摘要

- Go：`gofmt`，包注释 + 公共 API 注释（说"为什么"）；errors `%w` wrap；不引入日志框架，用标准库 `log`
- TS：strict mode；不写 `any` 除非 wails generated；不加新前端依赖（现有栈含 Vue / xterm / Naive UI / Pinia / lucide / CodeMirror / qrcode-svg）
- 注释：写 *why* 不写 *what*；commit-specific / "added for X" / "see ticket Y" 不要写进代码注释（应该在 commit msg 或 PR）
- 移动端 / 共享字体栈：CJK family（`PingFang SC` 等）放在 `-apple-system` 之前；终端字体常量统一 import `desktop/frontend/src/lib/terminalFont.ts::TERMINAL_FONT_FAMILY`，desktop / mobile 共用
- 不要在 main 直接 push：CI 会跑，commit 要先在本地 `go vet -tags webkit2_41 ./...` + `npm run build` 全过
- commit msg 用小写动词起头，subject ≤ 72 字符（参考 git log 现有风格）

详见 `docs/spec/conventions.md`。

## 不要做

- ❌ 在 `internal/relay` 加桌面端特定逻辑（违反依赖方向）
- ❌ 按 host_id 去重 session（同机多实例会失效，详见红线 #3）
- ❌ 在 PTY 字节流路径加 JSON 解析或正则匹配（性能 + 正确性双输）
- ❌ 给 OUT 帧 seq 重置（重连必须保持单调递增，client 续传依赖此）
- ❌ 在 desktop/ 之外用 `os.UserConfigDir()` 做持久化（其他组件应当无状态）
- ❌ 直接 `conn.Write` from 多个 goroutine（nhooyr/websocket 不允许并发写；走单 writer goroutine + channel）
- ❌ 自动更新跳过 Ed25519/SHA256 校验，或在缺公钥时降级允许安装
- ❌ 公网 relay 使用弱 token/空鉴权，除非用户显式 `--dev-insecure`
- ❌ 桌面端默认允许非 loopback `ws://`；必须由用户打开 insecure mode
- ❌ `web/` 重新引入外部 CDN script/style，或让 token 长期留在浏览器地址栏
- ❌ 新增用户可见文案只改一种语言；desktop 和 web 的 i18n messages 必须保持中英覆盖
- ❌ 把主 write token（read-only token）持久化到 relay admin config；只能来自 env/flag/启动自动生成。**例外**：飞书字段加密密钥有意存进 DB `relay_config.feishu_secret_key`（红线 #26），GET 必须脱敏；别把这条例外推广到其它 secret
- ❌ 让 `userstore.Open` 强制要求 secret cipher、或把 `ATTERM_FEISHU_ENCRYPT_KEY` 设回启动必填——会让没配飞书的 relay 崩溃重启循环（红线 #26）
- ❌ 复活 `relay.json` / `web-push.json` 文件持久化路径；配置一律走 DB `relay_config` 表（红线 #26）。SQLite 单实例部署下 `relay.json` 甚至不会被读一眼
- ❌ 只在访问者客户端 UI 隐藏输入按钮而不在 relay/desktop host 强制拦截
- ❌ 把 `-apple-system` / `system-ui` 放在 CJK family 之前——iOS 26 WebKit 会触发 `[?]` 方框回归
- ❌ 把 pairing token 明文写进日志、URL query 或长期存储（只在 `qr_url` 与 owner 单次返回里出现，consumer 调用时通过 POST body 传输）
- ❌ 在 `/api/pair/consume` 路径上加任何鉴权头校验——pairing token 本身即凭据，加上 token-on-token 会把流程改回 OAuth code+secret 模式
- ❌ 在多处实现 session type sticky；只在 `internal/session/applyOSC133Locked` 一个入口写规则
- ❌ 自定义 Capacitor plugin 只写 `.m` 的 `CAP_PLUGIN` 宏 / 只在 `desktop/frontend` 装 plugin —— 必须走红线 #15 的三件套
- ❌ 让 `TerminalView.vue` 的移动端 IME 接管去碰 composition 路径（中文字会双发）
- ❌ 给已删的 `quickInput` 插件加 backward-compat shim；它已被 QuickTemplate 取代，旧 config.json 字段直接忽略（single-user project）
- ❌ 在 `connection.ts` 调 `new WebSocket(url, protocols)` 不包 try/catch；WebKit 对非法 url scheme / 非法 subprotocol 字符是**同步**抛 `SyntaxError` 的，会击穿 App.vue boot 的 await chain（红线 #18）
- ❌ 把 `App.vue` onMounted 的多步启动塞进一个统一 try/catch 又不带阶段标记；红线 #19 要求 `bootStage` 在每个调用前赋值，否则失败时只能看到一条不可读错误
- ❌ 在 titlebar 用 flex `flex: 1 1 0` 来"撑开"标题；左右两侧不对称时 flex 中点会偏。要绝对居中：`.window-title { position: absolute; left: 50%; transform: translateX(-50%) }` + `.status { margin-left: auto }`
- ❌ 给 OPAQUE 换套件 / 改 `SERVER_IDENTITY` 不同步两端；改 Go 一边 TS 会报 `unsupported KE2 / ke3 length`，握手当场断（红线 #20）
- ❌ 把 `account_key`、`password`、wrap intermediate 写进 URL query / log / IndexedDB / postMessage 广播（红线 #21）
- ❌ 给新 sealed 帧复用已分配的 AAD frame_type 字节（红线 #22），或忘记在 `docs/spec/protocol.md` §E2EE 信封 加一行
- ❌ agent seal 成功后还把同一份字段以明文继续 emit（红线 #23）；webhook/push 路径会优先走明文分支，等于 seal 没干活
- ❌ 给 E2EE 加"密码找回 / 备用问题 / 邮件链接"分支；这会让 relay 重新拿到 `account_key`，整套 E2EE 模型降级（红线 #24）。step-up 只是给 hard-delete 这种特权操作加一道再确认
- ❌ 在 AI sid 抓不准时硬猜一个塞进 `recovery.json`（红线 #28）；宁可让 ai 字段留空、恢复后是普通 shell 让用户自己重启 agent，也别把另一个对话的历史 leak 进恢复后的 session
- ❌ 把恢复 attach 的远端 pane 默认成 `isDriver: true`（红线 #28 / PR #240）；relay 那边可能 driverless 或别人是 driver，乐观默认 + META 不到 = UI 显 driver 但 IN 帧被丢，bug 极隐蔽
- ❌ 把 anchor card 的 input / buttons / form 合并成一次 DELETE + CREATE（红线 #30）；三件独立，各自 gate 在自己的 mounted 标记，否则任何一个 CREATE 失败会让 restore 短路
- ❌ 在 `swapAnchorButtons` 里 PATCH `anchor_buttons` 前不看 `anchor.ButtonsMounted`（红线 #30）；对已删元素 PATCH 会拿 `code=300313 not find elementID`
- ❌ 试图用 delay tuning / Right-arrow pump / Ctrl+Return via LF (0x0a) 修 AskUserQuestion 丢字（红线 #31）；都测过不通，问题在 claude TUI 里，workaround 已经是 trailing space（红线 #32）
- ❌ 给 `RenderAskQuestionForm` 里 widget element_id 去掉 `_<mountSeq>` 后缀（红线 #32）；同一 anchor 第 2 次挂 form 会加载上次答案，用户 confused
- ❌ 没有先本地"Yes, and don't ask again"授权 AskUserQuestion 就调 stroke（`feedback_askform_permission_grant.md`）；permission dialog 会吃掉序列头两个 stroke，form 走位全乱
- ❌ 手动删掉 `sl.txt + " "` 尾部空格 workaround（红线 #32）；claude TUI 稳定丢最后一字符，空格是被牺牲的那个
- ❌ 在 `desktop/` 里的初始化路径写裸 `log.Fatalf`——必须走 `setStartupFatalError(msg, logPath)` + return，webview 才能起来给用户显示可复制的失败信息（红线 #35）
- ❌ 只在 `useSessionPins.pin/unpin` 之外的地方直接改 `pinnedIds.value.add(...)` / `.delete(...)`——Vue shallow-ref 检测跳过同实例 mutation；必须 clone 出 `new Set(pinnedIds.value)` 再赋值（`useSessionPins.ts` 里有注释；红线 #34 也涉及）
- ❌ 在 recovery 里给 `sidebar-viewer on local host` pane 写 `session_id` 用于 pin 迁移——那 sid 属于另一实例的 relay，迁到新本机 sid 是跨实例语义漂移（红线 #34）
- ❌ 加新 relay 实例还去改 `realm_id` / 用 env 覆盖——`realm_id` 只从 DB 读，同一物理集群共享同一个（红线 #33）；实例之间也**不要**拉 gossip / 直连，共享状态全走 DB 表

## 文档导引

- `docs/spec/architecture.md` — 整体架构、组件职责、数据流、phase 路线
- `docs/spec/protocol.md` — wire 协议完整规范（所有帧类型、重连续传语义）
- `docs/spec/auth.md` — 鉴权层与 E2EE：OPAQUE 流程、account_key wrap、session token、step-up、错误码字典
- `docs/spec/feishu.md` — 飞书子系统正式 spec（模式、anchor card 生命周期、AskUserQuestion form flow、已知限制）
- `docs/spec/conventions.md` — Go/TS 代码约定、commit 风格、测试组织
- `docs/spec/component-style.md` — 前端组件视觉、控件复用、Settings / dialog / pane 样式规范
- `docs/spec/site.md` — Demo 站点子系统：mock 三层拦截、零侵入原则、caps/alias 关键决策、Pages 部署
