# AGENTS.md

> **Audience**: 在 atterm 仓库里工作的 AI 编码 agent
> **Last updated**: 2026-06-19
> **Status**: stable
> **See also**: [README.md](./README.md) · [docs/spec/architecture.md](./docs/spec/architecture.md) · [docs/spec/auth.md](./docs/spec/auth.md) · [docs/spec/protocol.md](./docs/spec/protocol.md)

atterm = 跨平台终端模拟器 + 内建会话云同步。所有从桌面 app 启动的会话默认可被任意设备的 web/桌面客户端 attach、查看历史、继续输入。核心场景：本机跑 codex/claude 的长 AI 任务，离开工位后用手机/另一台机器接管。

阅读这份文件以快速上手；详细规范见 `docs/spec/`。最近版本和 PR 列表用 `git log --oneline -30` / `gh pr list --state merged --limit 30` 取真相，文档里不再维护流水账。

历史规范（已完成的大初始化系列）：用户账号 + OPAQUE E2EE 套件（M1–M6，至 v0.2.110，spec 见 [docs/superpowers/specs/2026-06-15-relay-e2ee-design.md](./docs/superpowers/specs/2026-06-15-relay-e2ee-design.md)）、移动端任务首页 + Capacitor 8 plugin 三件套（至 v0.2.39）、桌面 UI 抛光与 boot stage 错误隔离（v0.2.71、#150）。各 phase 完成度见 [docs/spec/architecture.md](./docs/spec/architecture.md) §phase 完成度。

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
├── mobile/                 Capacitor iOS WebView wrapper（同步 web/ 静态资源）
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
11. **远程权限由 owner 决定、relay/host 强制执行**：桌面端通过 `remote_permission` 发布 view/control/full；relay 先拦截越权 `IN`/`RESIZE`/`PASTE_IMAGE`，desktop uplink 写本机 PTY 前再拦一次。relay 不再有共享只读 token；如需限制某用户只读，在桌面端将该 session 的 `remote_permission` 设为 `view`。
12. **大历史 attach 要可感知**：relay 初始 scrollback 回放必须发 `REPLAY_PROGRESS`，并在 `/client` writer 侧做轻量 pacing，避免桌面/web 客户端长时间只显示 connecting 或卡住。不要移除该帧，wire 变更同步更新 `docs/spec/protocol.md`。
13. **iOS 26 字体栈三件套**：(a) 终端 / UI 字体栈把 `PingFang SC` 等 CJK family 放在 `-apple-system` 之前——iOS 26 WebKit 把 `-apple-system` 声明为含 CJK 覆盖但实际不渲染，导致 `[?]` 方框；(b) 终端字体常量统一在 `desktop/frontend/src/lib/terminalFont.ts`，desktop 与 mobile 共用；(c) UI emoji（⚠ 等）用内联 lucide SVG，不要依赖 U+FE0F 变体选择器。改其中一个就回归，详见 `docs/spec/conventions.md` §移动端字体栈。
14. **OSC 133 D 事件单点抓 summary，sticky non-shell**：`internal/session/applyOSC133Locked` 是 type 分类 + summary 抓取的唯一入口；C 事件按 `ClassifyCommand` 算 type，sticky 规则保证返回 `shell` 时不覆盖已有 non-shell；D 事件 `computeSummary(scroll, now, exitCode != 0)` 生成 `SessionSummary` 并写进 `s.meta`。MetaPayload 必须同时携带 `type` 和 `summary` 字段（前者是 P2.11 漏配的 carry-over）。前端不要再做"sticky type" patch；只在一处实现。
15. **Capacitor 8 plugin 注册三件套**：(a) 自定义 plugin 的 Swift 类必须 conform `CAPBridgedPlugin`（`identifier` / `jsName` / `pluginMethods`）；旧式 `.m` 文件里的 `CAP_PLUGIN` 宏在 Capacitor 8 是 no-op；(b) app-local 自定义 plugin 不走 auto-discovery，要在 `MainViewController.capacitorDidLoad()` 里 `bridge?.registerPluginInstance(...)`，并把 `Main.storyboard` 根 VC 指到 `MainViewController`；(c) 第三方 plugin（`@capacitor/camera` / `@capacitor-mlkit/barcode-scanning` / `@capacitor/keyboard` 等）**必须装到 `mobile/package.json`** —— `cap sync` 只扫 mobile/ 的依赖。装在 `desktop/frontend/` 一边的 JS 能 import 但 native 不会注册，跑起来 `PLUGIN_NOT_AVAILABLE`。验证步骤详见 `docs/spec/conventions.md` §Capacitor 8 plugin 注册。
16. **移动端 IME 标点/数字/空格 capture-phase 接管**：iOS 中文九宫格的 `，。？！` / 数字 / 空格走 `input` 事件（`inputType='insertText'`、`isComposing=false`），xterm 自己的 handler 不 forward。MobileTerminal 在 `term.textarea` 的 capture 阶段监听 `input`，对 `insertText && !isComposing` 自己 `sendInput(data)` 并 `stopImmediatePropagation`。**不要碰 composition 路径**（pinyin→Hanzi 走 `insertCompositionText`），那是 xterm 的领地，碰了会让中文字双发。
17. **移动端 fit 走 ResizeObserver + viewer 锁尺寸**：MobileTerminal 首屏半屏的根因是 `fit.fit()` 跑在 `.term` 容器还没 settle 之前。改用 `ResizeObserver` 监听容器尺寸变化、driver 才 fit；viewer 模式下 `onMeta` 收到 `meta.cols/rows` 时 `term.resize(meta.cols, meta.rows)` 锁到 PTY 尺寸（不跑 FitAddon，匹配 protocol.md §Driver/Viewer）。两条都不要再去掉。
18. **`new WebSocket()` 同步抛出必须被隔离**：`SessionListConnection.openWS()` / `SessionConnection.openWS()` 在调用 `new WebSocket(url, protocols)` 时必须用 try/catch 包裹，并把异常路由到 `handleOpenFailure` —— 它 `console.error` 出 url/protocols/error.name，触发 `onStatus("error")`，按指数退避排重连。WebKit 对非法 URL scheme / 非法 subprotocol 字符抛 `SyntaxError: The string did not match the expected pattern.` 是**同步**的，不像普通连接失败走异步 `onclose`；若漏接，调用者（`App.vue` onMounted boot 链）的 `await` chain 会被击穿，整个启动卡在「正在启动第一个会话…」+ titlebar 红字。详见 `docs/spec/conventions.md` §WebSocket 同步异常隔离。
19. **桌面启动 try/catch 按 bootStage 分阶段**：`App.vue` `onMounted` 里 `refreshTerminalTheme` / `getEndpoint` / `getHostInfo` / `connectLocalSessionList` / `refreshRelayConfig` 顺序执行，必须用单个 `let bootStage = ""` 在每一步前重新赋值，catch 时把 `${bootStage}: ${e.name}: ${e.message}` 写进 `errorMsg`，同时 `console.error('[boot] step "${bootStage}" failed', { name, message, stack })`。否则一锅端的 catch 把五个调用塌成同一条不可读错误，下次再出 DOMException 又得猜。详见 `docs/spec/conventions.md` §启动 try/catch 分阶段。
20. **OPAQUE 套件 = P-256-SHA256 + Scrypt**：Go 端 `internal/relay/opaque_server.go` 的 `opaqueSuiteTag = "p256-scrypt-v1"` 和 TS 端 `@cloudflare/opaque-ts` 的 `OpaqueID.OPAQUE_P256 + ScryptMemHardFn` 绑死。bytemare 默认是 ristretto255-SHA512，但 Cloudflare TS 不支持，所以**不要**给"安全升级"为别的曲线——会让浏览器/桌面之间的握手直接失败。`SERVER_IDENTITY = "atterm-relay"` 也是两端硬编码。详见 [docs/spec/auth.md](./docs/spec/auth.md) §OPAQUE。
21. **account_key 永远不出主线程 / 不进 URL / 不进日志**：浏览器把 32B `account_key` 存 `sessionStorage`（tab 关掉就丢），桌面用 `zalando/go-keyring`，iOS 用 `AttermSecureStorage` Keychain plugin。SW 解密 push body 只能通过 `MessageChannel` 向可见 client 请求（M6-sw 的 `web/src/shared/sw-bridge.ts`），**不要**把 key 写进 IndexedDB / postMessage 广播 / GET query。同理 `password` / wrap 中间产物也不允许 log。
22. **AAD 鉴别字节是 cross-type replay 的唯一防线**：每个 sealed 信封的 AAD = `uuid(16B) || frame_type(1B)`。当前分配：`OUT=0x03` / `META=0x05` / `LIST_RESP/SessionInfo=0x12` / `COMMAND_EVENT=0x35`。加新 sealed 帧类型时**必须**给新 frame_type 一个唯一字节，并在 [docs/spec/protocol.md](./docs/spec/protocol.md) §E2EE 信封 加一行，否则别处的信封能被替换重放。
23. **agent seal 成功后必须 strip plaintext**：`desktop/uplink.go` 的 `SendCommandEvent` / `uplink_seal_fields.go` 的 SessionInfo/META 封装都遵守同一模式——seal 成功就把对应明文字段（`Title/Cwd/Command/CurrentCommand/Label/ExitCode/ElapsedMS`）写回零值。**不要**留一份明文给 relay 做 fallback——那会让 webhook/push 路径走回明文分支，绕过 E2EE。Seal 失败（短 key、cipher 出错）才走 fallback 路径。
24. **不要给 OPAQUE 加密码恢复 / step-up 之外的特权门**：spec 明确不做密码找回，admin reset 等于丢密钥；任何"备用问题/邮件链接"都会把整个 E2EE 模型降级回服务端可读。唯一的特权操作（DELETE /api/me 等）通过 step-up token（60s 单次有效，由再走一次 OPAQUE login 换取，见 `internal/relay/opaque_stepup.go`）。
25. **proto.Frame 拓展遵守"opaque field 可加, 含义不可改"**：`SessionInfo.Sealed []byte` / `MetaPayload.Sealed []byte` / `CommandEventPayload.SealedBody []byte` 当前已落地；新增 sealed payload 时遵循同一三件套（payload struct 加 `Sealed`/`SealedBody` 字节段、agent seal helper、客户端 open helper）。`Sealed` 字段为空 = 未加密；非空时客户端**必须**在拿到 `account_key` 后才信任内容，旧版客户端可以忽略并 fall back 到外层明文字段。
26. **relay 运行时配置以 `relay.json`（`AdminConfig`）为准、能热生效就不重启**：origins / 限流 / 连接数 / debug / 飞书都在 `internal/relay/admin_config.go` 的 `AdminConfig` 里，经 `/admin/api/config` 与 `/admin/api/feishu`（`admin_http.go`）读写并**热应用**——origins/debug 走 `Server` 上的 `atomic.Pointer`/`atomic.Bool`，飞书走 `Server.ApplyFeishuConfig`（建/拆 secret cipher + handler）。唯一需重启的是 VAPID subject（`webpush.Open` 一次性消费）。三条配套铁律：(a) **DB 必须能在无 secret cipher 下打开**（`userstore.Open` 不强制传 cipher，飞书未启用时 cipher 为 nil，仅飞书 CRUD 报错）——`ATTERM_FEISHU_ENCRYPT_KEY` 不再是启动必填，否则又会回到崩溃重启循环；(b) 飞书路由（`/v1/feishu/*`）在有 SQLite store 时**无条件注册一次**、闭包内按 `feishuRuntime.handler.Load()` 门控（`http.ServeMux` 不支持注销路由，禁用时绑定路由返 503、事件路由返 404）；(c) 飞书主加密密钥**有意持久化在 `relay.json`（0600）**，是「secret 不进 admin config」(见“不要做”) 的明确例外——GET 只回显末 4 位、绝不返回明文、绝不写日志；换 key 会让旧飞书绑定无法解密，故 PUT 默认拒绝轮换、需 `force:true`。env 仅作首启一次性播种（config 优先）。
27. **OPAQUE 需要「安全上下文」，所以 relay 默认自签 HTTPS**：浏览器只在 HTTPS 或 `localhost` 暴露 `crypto.subtle`（`@cloudflare/opaque-ts` 用它做哈希/HMAC），明文 HTTP 公网 IP 上 `crypto.subtle===undefined`，注册/登录在浏览器本地直接抛错、**连请求都发不出**（Network 为空）。因此 `cmd/atterm-relay` 同时起两个监听：HTTP（`--addr :8080`，给反代后端）和 **HTTPS（`--https-addr :8443`，默认开，自签证书走 `relay.LoadOrCreateSelfSigned`，持久化到 `<config-dir>/tls/` 复用**——别每次重启重生成，否则浏览器一次性信任失效）。自带证书用 `ATTERM_TLS_CERT/KEY`，SAN 用 `ATTERM_TLS_HOST` 注入。**不要**为了「能在明文 HTTP 跑」去掉 WebCrypto/换纯 JS OPAQUE 库——既改动 E2EE 核心（红线 #20 两端套件写死），又因 token/wrap 明文过网而并不安全。

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

环境变量（relay 已把大部分配置下沉到 `relay.json` + 管理后台，env 多为「核心」或「首启播种」）：

核心：
- `ATTERM_BOOTSTRAP_ADMIN_EMAIL`：启动时为该 email 打印一次性 claim token（7 天 / 单次）；用它在 `/signup.html` 注册**新**账号即提为 admin（该 email 已注册则 token 无法消费，改用 SQL 提权）。公网监听必填，除非 `--dev-insecure`。**无 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`**——已随 OPAQUE 迁移移除
- `ATTERM_ORIGINS`：逗号分隔的浏览器 WS Origin 白名单；公网必填（除非 `--dev-insecure`）。首启播种进 `relay.json`，之后可在 Admin → Config 改
- `ATTERM_RELAY_CONFIG_DIR`：持久化目录（`users.db` + `relay.json`），默认 `./data/atterm-relay`
- `ATTERM_RELAY_CONFIG`：admin 配置文件路径覆盖（默认 `<config-dir>/relay.json`）

可选（不设也能启动；首启一次性播种进 `relay.json`，之后以管理后台为准）：
- `ATTERM_FEISHU_ENCRYPT_KEY`：飞书凭据字段加密密钥（base64 32B）。**不再必填**——Admin → Feishu 可生成；DB 在无 cipher 下也能开，仅启用飞书时才挂载
- `ATTERM_FEISHU_BASE_URL`：飞书 Open Platform base URL（默认 `https://open.feishu.cn`）
- `ATTERM_VAPID_SUBJECT`：Web Push VAPID subject（改动需重启）
- `ATTERM_RATE_LIMIT_PER_MINUTE` / `ATTERM_MAX_CONNECTIONS_PER_KEY`：限额；`0` 默认值、负数禁用
- `ATTERM_RELAY_DEBUG` / `ATTERM_RELAY_DEBUG_PAYLOAD`：详细日志（现在 Admin → Config 也能热开关）

其它：
- `ATTERM_RELAY_URL` / `ATTERM_RELAY_TOKEN`：桌面 app 首次启动时若无配置文件，从这俩 env 读初始值
- `ATTERM_HOST_ID`：覆盖 host id 文件（容器场景）
- `ATTERM_UPDATE_VERIFY_PUBLIC_KEY`：GitHub prod environment secret；base64 Ed25519 公钥，release 构建时注入桌面 app
- `ATTERM_UPDATE_SIGNING_PRIVATE_KEY`：GitHub prod environment secret；base64 Ed25519 私钥，只在 release job 里签 `SHA256SUMS`

## 何时改哪里

| 修改场景 | 触动文件 |
|---------|----------|
| account / token / invitation / pairing 数据库操作 | `internal/userstore/`（users / apitokens / invitations / websessions / pairing_tokens / webhooks）；Store 接口变更需同步更新所有实现和 `store_iface_test.go` |
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
| 改桌面远程 relay 配置 | `desktop/app.go` + `desktop/config.go` + `desktop/relay_security.go` + `desktop/frontend/src/components/SettingsDialog.vue` |
| 改远程权限模型 | `internal/proto/frame.go` + `internal/relay/permissions.go` + `desktop/uplink.go` + Settings UI + 协议规范 |
| 改 relay admin 配置（限流 / origins / debug / 飞书 / VAPID） | `internal/relay/admin_config.go`（`AdminConfig` 字段 + validate）+ `internal/relay/admin_http.go`（`/admin/api/config`、`/admin/api/feishu`、generate-key）+ `internal/relay/server.go`（热应用：atomic origins/debug、`ApplyFeishuConfig`、飞书路由门控）+ `internal/userstore/store.go`（`SetSecretCipher`）+ `cmd/atterm-relay/main.go`（env→config 播种）+ `web/src/admin/tabs/{Config,FeishuConfig}.vue` + `web/src/shared/api/admin.ts` + i18n + README/spec。红线 #26 |
| relay 注册表清理 | `internal/relay/uplink_conn.go`（writer ping fail 触发 cancelConn → cleanup mirror sessions） |
| 改移动 app relay 配置 | `web/src/setup/` + `web/src/shared/api/relay-config.ts` + `web/src/shared/mobile-guard.ts` + `web/src/settings/tabs/Relay.vue`；`apiFetch`/`wsUrl` 的 mobile 分支在 `web/src/shared/api/client.ts` 和 `web/src/shared/ws/client-conn.ts` |
| 改桌面前端 ↔ Go IPC | `desktop/frontend/src/platform/wails.ts`（适配器）；新方法先在 `desktop/app.go` 或 `desktop/plugin_*.go` 定义，让 Wails 重生成 `wailsjs/`，再在 `platform/wails.ts` 包一层。**不要**在 `src/platform/` 之外的文件直接 import `wailsjs/*`。 |
| 改 relay 出站 webhook（命令结束通知） | `internal/webhook/`（render/transport/dispatch/service，**不依赖** userstore）+ `internal/userstore/webhooks.go`（+ migration `0003_webhooks.sql` + `Store` 接口）+ `internal/relay/auth_http.go`（`/api/me/webhooks`）+ `internal/relay/uplink_conn.go`（命令结束分发点，紧挨 WebPush）+ `cmd/atterm-relay/main.go`（构造 service + `webhookStoreAdapter` 映射）+ `web/src/settings/tabs/Webhooks.vue` |
| 改 pairing QR 流程 | `internal/userstore/pairing.go`（+ `Store` 接口 + migration）+ `internal/relay/pair_http.go`（`/api/pair/create` + `/api/pair/consume`）+ `internal/relay/auth_http.go`（路由注册 + Limits 钩子）+ `desktop/app.go::CreatePairingToken` + `desktop/frontend/src/components/PairingPanel.vue` + `web/src/setup/`（移动端 consume + 写 secure storage） |
| 改 relay 健康检查 | `internal/relay/health_http.go`（`HealthPayload` + `/healthz` + `/admin/health` + `/admin/api/health`）+ `internal/relay/templates/health.gohtml` + `internal/relay/server.go`（路由注册）+ contract test |
| 改桌面诊断导出 | `desktop/diagnostics.go`（payload collector + redaction）+ `desktop/app.go::GetDiagnostics/ExportDiagnostics` + `desktop/frontend/src/components/SettingsDiagnostics.vue` |
| 改移动端安全存储 | `mobile/ios/App/App/plugins/SecureStorage/AttermSecureStorage.swift`（CAP_PLUGIN，SecItemAdd/CopyMatching/Delete + `kSecAttrAccessibleAfterFirstUnlock`）+ `web/src/shared/secure-storage.ts`（Capacitor bridge + localStorage 降级）+ `web/src/shared/api/relay-config.ts`（迁移路径）；改完一定要在 iOS simulator 验证 keychain 写入 |
| 改 session 类型分类 / 摘要 | `internal/session/classify.go`（关键字 + wrapper 剥离 + sticky）+ `internal/session/summary.go` + `internal/session/ansistrip.go` + `internal/ringbuf/ringbuf.go::TailBytes` + `internal/proto/frame.go`（`SessionInfo.Type` / `SessionSummary` / `MetaPayload.Type` / `MetaPayload.Summary`）+ `internal/session/session.go::applyOSC133Locked`（唯一调用点）+ TS 侧 `desktop/frontend/src/lib/connection.ts` + `web/src/shared/api/types.ts`（mirror SessionSummary）+ 前端任务卡片渲染 |
| 改 AI 快捷模板（含 hotkey / hide bar / 默认值） | `desktop/frontend/src/lib/templates.ts`（`QuickTemplate { id, label, text, hotkey? }` + `DEFAULT_TEMPLATES`：`yes / ok / continue / commit / push / release / 1 / 2 / 3` + `effectiveTemplates`）+ `web/src/shared/templates.ts`（镜像）+ `platform/types.ts`（`TemplateBridge` 含 `loadHidden/saveHidden`）+ `wails.ts` / `capacitor.ts`（hidden 都走 localStorage `atterm.templates.hidden`，desktop 模板列表走 `appConfig.QuickTemplates`，mobile/web 走 localStorage `atterm.templates`）+ `desktop/quick_templates.go`（含 `Hotkey` 字段）+ `wailsjs/go/models.ts`（同步）+ `SettingsTemplates.vue`（含 hotkey 列 + show-bar 开关，emit `quickTemplates:changed`）+ `MobileSettings.vue`（show-bar 开关 emit `mobile:shortcutsChanged`）+ desktop TerminalView / web TerminalView / MobileTerminal 三端 template-bar + 直接发送（无预览） + desktop TerminalView document-capture keydown 匹配 `hotkey` 触发 |
| 改移动端 AUX 键 / 设置页 | `desktop/frontend/src/lib/auxKeys.ts`（`AuxKey { id, label, seq }` + `DEFAULT_AUX_KEYS` + `parseSeq/displaySeq` 转义解析）+ `platform/types.ts AuxKeyBridge`（capacitor localStorage `atterm.auxkeys`、wails no-op、fake 内存）+ `MobileSettings.vue`（语言 / 模板编辑器 / aux 键编辑器 / 退出登录）+ `MobileListEditor.vue`（通用增删改/重排/重置）+ `MobileApp.vue`（'settings' view + `onLogout` 保留 relay 配置）+ `MobileSessionList.vue`（gear emit `openSettings`） |
| 改移动端 Capacitor plugin（Camera/Barcode/Keyboard 等） | **必须**装到 `mobile/package.json` 让 `cap sync` 发现；同时装到 `desktop/frontend/package.json` 让 TS import 解析。装完 `cd mobile && npm run ios:open`（已自动跑 `npm install` + `cap sync`）。验证 `mobile/ios/App/App/capacitor.config.json` 的 `packageClassList` 包含新 plugin 名、`mobile/ios/App/CapApp-SPM/Package.swift` 有对应 SPM 依赖。权限串（如 `NSPhotoLibraryAddUsageDescription`）加在 `mobile/ios/App/App/Info.plist`。`@capacitor/keyboard` 的 `setAccessoryBarVisible(false)` 在 `desktop/frontend/src/main.capacitor.ts` 启动时调一次 |
| 改移动端 IME / xterm 文本接管 | `desktop/frontend/src/mobile/MobileTerminal.vue`：`onImeInput` 函数 + `term.textarea` capture-phase 监听。**只**处理 `inputType === 'insertText' && !isComposing && data`，sendRaw + `stopImmediatePropagation`，清空 textarea。composition 路径（`insertCompositionText`）一律不碰 |
| 改移动端 fit / viewer 锁尺寸 | `desktop/frontend/src/mobile/MobileTerminal.vue`：`ro = new ResizeObserver(fitIfDriver)` + `fitIfDriver()`（仅 isDriver 时 fit）+ `onMeta` viewer 锁 `term.resize(meta.cols, meta.rows)`。`onBeforeUnmount` `ro?.disconnect()` |
| 改防误触模式 banner | `desktop/frontend/src/mobile/MobileTerminal.vue`：`protectActive = canControl && isDriver && !controlMode` + `protectBump` 计数器 + `nudgeProtect()`。banner 用 `:key="protectBump"` 触发 shake 动画。`.term` 加 `pointerdown` listener 也 nudge |
| 改 titlebar 标题居中 / running indicator | `desktop/frontend/src/components/TitleBar.vue`：`.window-title` 用 `position: absolute; left: 50%; transform: translateX(-50%)`，`.status` 加 `margin-left: auto` 把右侧元素推到边缘；running indicator 走 `.titlebar::after`（伪元素永远存在，`opacity: 0` + `transition: opacity 0.5s`）+ `.titlebar.is-running::after { opacity: 1 }`，`background` 用 `repeating-linear-gradient(90deg, transparent, …, #bbf7d0, …, transparent)` 平铺 1120px 周期、`background-size: 1120px 100%`、`animation: titlebar-running-sweep 3.5s linear infinite` 把 background-position 推到 `1120px 0`。`prefers-reduced-motion` 退化成静态绿条。`App.vue` 把 `currentActiveSession.value?.task_state` 通过 `current-task-state` prop 传进来 |
| 改会话侧栏分组折叠 / 隐藏滚动条 | `desktop/frontend/src/components/TaskGroupedList.vue`：`collapsedGroups: ref<Set<string>>`（用替换 Set 触发响应式）+ `isGroupCollapsed/toggleGroupCollapsed`；header 加 `role="button" tabindex="0"` + `@click` / `@keydown.enter` / `@keydown.space` + `aria-expanded`，caret 在 `▼` / `▶` 之间切换。`v-for` 改成 `(isGroupCollapsed(key) ? [] : groups[key])`。mark-all 按钮加 `@click.stop`，避免点 ✓ 顺带把分组折叠。`desktop/frontend/src/components/TaskSidebar.vue`：`.expanded { flex: 1 1 auto; min-height: 0; display: flex; flex-direction: column }` 让里层 `.list-wrap` 的 `flex: 1; overflow-y: auto` 真正工作；`.list-wrap` 加 `scrollbar-width: none` + `::-webkit-scrollbar { display: none }` 隐藏 gutter |
| 改快捷模板栏滚轮 / 隐藏 gutter | `desktop/frontend/src/components/TerminalView.vue`：`.template-bar` 加 `scrollbar-width: none` + `::-webkit-scrollbar { display: none }`；`@wheel.passive="onTemplateBarWheel"` 把 `deltaY`（或 deltaX）折成 `scrollLeft`，让鼠标用户不按 Shift 也能横向 pan。passive listener 不 preventDefault，xterm 自己的 wheel 路径不受影响 |
| 改桌面启动错误隔离 / 阶段定位 | `desktop/frontend/src/lib/connection.ts`：`SessionListConnection.openWS()` / `SessionConnection.openWS()` 用 `try { ws = new WebSocket(...) } catch (e) { this.handleOpenFailure(e, auth); return; }` 包裹；`handleOpenFailure` 打印 url/protocols/error.name，调 `onStatus("error")`，按 `Math.min(8000, 500 * 2^attempts)` 退避排重连。`desktop/frontend/src/App.vue`：`onMounted` 用 `let bootStage = ""` 在每一步前赋值（`refreshTerminalTheme` / `getEndpoint` / `getHostInfo` / `connectLocalSessionList` / `refreshRelayConfig`），catch 时把 `${bootStage}: ${e.name}: ${e.message}` 写进 `errorMsg`，并 `console.error('[boot] step "..."', { name, message, stack })` |
| 改 OPAQUE 注册 / 登录 / step-up | `internal/relay/opaque_server.go`（套件 + persistence）+ `internal/relay/opaque_auth.go`（register/login HTTP handler）+ `internal/relay/opaque_stepup.go`（60s 一次性 token）+ `internal/e2eeclient/`（Go 桌面 SDK）+ `web/src/shared/lib/opaque.ts` + `desktop/frontend/src/lib/opaque.ts`（mobile mirror）。改套件**必须**两端一起改——见红线 #20 |
| 改 account_key 持久化 / 解锁路径 | 桌面：`zalando/go-keyring` via `desktop/account_key.go` + Wails 绑定 `GetAccountKey` / `setAccountKey` + `EventsEmit("account-key-changed")`。Web：`web/src/shared/api/account-key.ts`（sessionStorage）。iOS：`mobile/ios/.../AttermSecureStorage.swift` + `desktop/frontend/src/platform/capacitor.ts` + `desktop/frontend/src/lib/account-key.ts`（cache 注册表）。改写入/读取路径时**不要**加 IndexedDB / localStorage（红线 #21）|
| 改 sealed 字段 / E2EE 信封 | agent: `desktop/uplink.go::SendCommandEvent` / `desktop/uplink_seal_fields.go` / `desktop/uplink_seal_push.go`（seal + plaintext strip,红线 #23）。relay: `internal/relay/uplink_conn.go::handleUplinkCommandEvent`（透传 SealedBody）+ `internal/webpush/dispatch.go`（sealed 分支）+ `internal/webhook/render.go`（sealed 分支）。client 解密：`web/src/shared/lib/opaque.ts::openSessionFields/openMetaFields/openPushBodyFields` + `web/src/shared/sw-bridge.ts`（SW 解密桥）。`proto.CommandEventPayload.SealedBody` / `SessionInfo.Sealed` / `MetaPayload.Sealed` 已在 `internal/proto/frame.go` 落地；加新 sealed 帧请同时 bump [docs/spec/protocol.md](./docs/spec/protocol.md) §E2EE 信封 的 AAD 表（红线 #22）|

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
- ❌ 把主 write token（read-only token）持久化到 relay admin config；只能来自 env/flag/启动自动生成。**例外**：飞书字段加密密钥是有意存进 `relay.json`（0600）的（红线 #26），GET 必须脱敏；别把这条例外推广到其它 secret
- ❌ 让 `userstore.Open` 强制要求 secret cipher、或把 `ATTERM_FEISHU_ENCRYPT_KEY` 设回启动必填——会让没配飞书的 relay 崩溃重启循环（红线 #26）
- ❌ 只在访问者客户端 UI 隐藏输入按钮而不在 relay/desktop host 强制拦截
- ❌ 把 `-apple-system` / `system-ui` 放在 CJK family 之前——iOS 26 WebKit 会触发 `[?]` 方框回归
- ❌ 把 pairing token 明文写进日志、URL query 或长期存储（只在 `qr_url` 与 owner 单次返回里出现，consumer 调用时通过 POST body 传输）
- ❌ 在 `/api/pair/consume` 路径上加任何鉴权头校验——pairing token 本身即凭据，加上 token-on-token 会把流程改回 OAuth code+secret 模式
- ❌ 在多处实现 session type sticky；只在 `internal/session/applyOSC133Locked` 一个入口写规则
- ❌ 自定义 Capacitor plugin 只写 `.m` 的 `CAP_PLUGIN` 宏 / 只在 `desktop/frontend` 装 plugin —— 必须走红线 #15 的三件套
- ❌ 让 MobileTerminal IME 接管去碰 composition 路径（中文字会双发）
- ❌ 给已删的 `quickInput` 插件加 backward-compat shim；它已被 QuickTemplate 取代，旧 config.json 字段直接忽略（single-user project）
- ❌ 在 `connection.ts` 调 `new WebSocket(url, protocols)` 不包 try/catch；WebKit 对非法 url scheme / 非法 subprotocol 字符是**同步**抛 `SyntaxError` 的，会击穿 App.vue boot 的 await chain（红线 #18）
- ❌ 把 `App.vue` onMounted 的多步启动塞进一个统一 try/catch 又不带阶段标记；红线 #19 要求 `bootStage` 在每个调用前赋值，否则失败时只能看到一条不可读错误
- ❌ 在 titlebar 用 flex `flex: 1 1 0` 来"撑开"标题；左右两侧不对称时 flex 中点会偏。要绝对居中：`.window-title { position: absolute; left: 50%; transform: translateX(-50%) }` + `.status { margin-left: auto }`
- ❌ 给 OPAQUE 换套件 / 改 `SERVER_IDENTITY` 不同步两端；改 Go 一边 TS 会报 `unsupported KE2 / ke3 length`，握手当场断（红线 #20）
- ❌ 把 `account_key`、`password`、wrap intermediate 写进 URL query / log / IndexedDB / postMessage 广播（红线 #21）
- ❌ 给新 sealed 帧复用已分配的 AAD frame_type 字节（红线 #22），或忘记在 `docs/spec/protocol.md` §E2EE 信封 加一行
- ❌ agent seal 成功后还把同一份字段以明文继续 emit（红线 #23）；webhook/push 路径会优先走明文分支，等于 seal 没干活
- ❌ 给 E2EE 加"密码找回 / 备用问题 / 邮件链接"分支；这会让 relay 重新拿到 `account_key`，整套 E2EE 模型降级（红线 #24）。step-up 只是给 hard-delete 这种特权操作加一道再确认

## 文档导引

- `docs/spec/architecture.md` — 整体架构、组件职责、数据流、phase 路线
- `docs/spec/protocol.md` — wire 协议完整规范（所有帧类型、重连续传语义）
- `docs/spec/conventions.md` — Go/TS 代码约定、commit 风格、测试组织
- `docs/spec/component-style.md` — 前端组件视觉、控件复用、Settings / dialog / pane 样式规范
