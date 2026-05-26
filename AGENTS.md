# AGENTS.md

atterm = 跨平台终端模拟器 + 内建会话云同步。所有从桌面 app 启动的会话默认可被任意设备的 web/桌面客户端 attach、查看历史、继续输入。核心场景：本机跑 codex/claude 的长 AI 任务，离开工位后用手机/另一台机器接管。

阅读这份文件以快速上手；详细规范见 `docs/spec/`。

- 当前 release：`v0.2.25`。Web 客户端已迁移为 Vue 3 + TypeScript + Naive UI 多页应用（login/signup/main/settings/admin/setup），并支持中英双语；spec 见 `docs/superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md` 和 `docs/superpowers/specs/2026-05-26-i18n-english-chinese-design.md`。

## 仓库布局

```
atterm/
├── cmd/
│   ├── atterm-relay/       中央 relay 服务的入口（生产部署的 server）
│   └── atterm-agent/       命令行 PTY wrapper（Phase 0 调试遗留，CLI 形态）
├── internal/
│   ├── proto/              帧协议定义 + 二进制编解码（跨所有组件复用）
│   ├── ringbuf/            scrollback 环形缓冲（按字节预算丢最老）
│   ├── session/            relay 端 session 模型 + Subscriber fan-out + lifecycle 钩子
│   ├── relay/              relay 服务实现：/agent · /uplink · /client · /api/sessions
│   ├── ptyhost/            纯 PTY 包装（Open/Read/Write/Resize/Wait/Close + Cwd via /proc）
│   ├── agent/              CLI agent 业务层（PTY + 本地 TTY 集成 + WS 客户端）
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
9. **公网 relay 默认安全**：`cmd/atterm-relay` 公网监听时必须提供 `ATTERM_BOOTSTRAP_ADMIN_EMAIL`（启动时该 user 不存在则需同时提供 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` ≥16 字符 + ≥3 类字符 + 不在弱密码黑名单内），否则启动拒绝；缺失 `--origins`/`ATTERM_ORIGINS` 同样拒绝，除非显式 `--dev-insecure`。relay 默认加 CSP/security headers，并按 IP 做 HTTP/WS rate limit 与连接数限制。服务端所有鉴权接口都不接受 `?token=`；桌面端 API token（`atk_…`）通过 `Sec-WebSocket-Protocol` 传递。桌面端默认拒绝非 loopback `ws://`，只有用户在 Settings 打开 insecure mode 才允许。
10. **Web 客户端不依赖 CDN**：`web/` 必须只加载同源构建产物；Vue/xterm/Naive UI 等 npm 依赖由 Vite 打包进同源 assets，并由 service worker 预缓存。不要重新引入外部 CDN script/style，否则 CSP/PWA 离线能力会回归。
11. **远程权限由 owner 决定、relay/host 强制执行**：桌面端通过 `remote_permission` 发布 view/control/full；relay 先拦截越权 `IN`/`RESIZE`/`PASTE_IMAGE`，desktop uplink 写本机 PTY 前再拦一次。relay 不再有共享只读 token；如需限制某用户只读，在桌面端将该 session 的 `remote_permission` 设为 `view`。
12. **大历史 attach 要可感知**：relay 初始 scrollback 回放必须发 `REPLAY_PROGRESS`，并在 `/client` writer 侧做轻量 pacing，避免桌面/web 客户端长时间只显示 connecting 或卡住。不要移除该帧，wire 变更同步更新 `docs/spec/protocol.md`。

## 开发命令

```bash
# macOS Homebrew 的 gh 在 /opt/homebrew/bin；Codex/CI 的非交互 shell
# 不一定加载你的 ~/.zshrc，所以本项目命令默认显式带上它。
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH

# 命令行 relay（本地调试；--dev-insecure 跳过强度与 Origin 校验，loopback 时 bootstrap envs 可省略）
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
  go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure
# --web flag omitted ⇒ uses the embedded FS at internal/relay/web-dist/.
# For production-like frontend dev: build once and serve from disk:
#   cd web && npm run build && cd .. && go run ./cmd/atterm-relay --web web/dist ...
# For live web frontend dev: run `cd web && npm run dev`; Vite at 5173 proxies
# /api, /admin/api, /agent, /uplink and /client to 127.0.0.1:8080.

# 命令行 agent（Phase 0 wrapper，调试用；需先在 relay 创建用户并生成 API token）
go run ./cmd/atterm-agent --relay ws://localhost:8080 --token atk_... -- bash

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

环境变量：
- `ATTERM_BOOTSTRAP_ADMIN_EMAIL`：启动时把该邮箱对应的 user 标记为 admin；公网监听时必填（否则启动拒绝），除非 `--dev-insecure`。User 已存在则只提权、忽略密码 env
- `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`：首次启动用来创建 admin 用户的明文密码；须满足 ≥16 字符、≥3 类字符、不在弱密码黑名单内（公网监听且 user 不存在时校验）。**首次登录后从 env / systemd unit 中删除并重启**，避免明文密码留在进程状态中
- `ATTERM_ORIGINS`：逗号分隔的浏览器 WebSocket Origin 白名单；公网 relay 必须配置（除非 `--dev-insecure`）
- `ATTERM_RATE_LIMIT_PER_MINUTE`：每个远端 IP 的 HTTP 请求与 WS upgrade 分钟限额；`0` 用默认值，负数禁用
- `ATTERM_MAX_CONNECTIONS_PER_KEY`：每个远端 IP 的活跃 WS 连接上限；`0` 用默认值，负数禁用
- `ATTERM_RELAY_CONFIG`：relay admin 持久化 JSON 配置路径
- `ATTERM_RELAY_URL` / `ATTERM_RELAY_TOKEN`：桌面 app 首次启动时若无配置文件，从这俩 env 读初始值
- `ATTERM_HOST_ID`：覆盖 host id 文件（容器场景）
- `ATTERM_UPDATE_VERIFY_PUBLIC_KEY`：GitHub prod environment secret；base64 Ed25519 公钥，release 构建时注入桌面 app
- `ATTERM_UPDATE_SIGNING_PRIVATE_KEY`：GitHub prod environment secret；base64 Ed25519 私钥，只在 release job 里签 `SHA256SUMS`

## 何时改哪里

| 修改场景 | 触动文件 |
|---------|----------|
| account / token / invitation 数据库操作 | `internal/userstore/`（users / apitokens / invitations / websessions）；Store 接口变更需同步更新所有实现和 `store_iface_test.go` |
| 新协议帧类型 | `internal/proto/frame.go` + 协议规范 + 接收方 |
| 改 relay session 行为 | `internal/session/`（local + mirror 都用同一个 Session） |
| 桌面新 binding | `desktop/app.go`（手写 + Wails 自动生成 `frontend/wailsjs/go/main/App.*`） |
| 新前端组件 | `desktop/frontend/src/components/` |
| 桌面端协议路径 | `desktop/uplink.go` 或 `desktop/relay_host.go`，**不要**碰 `internal/agent/` |
| CLI wrapper 行为 | `internal/agent/` + `cmd/atterm-agent/` |
| 改 pane 布局 / 分屏键 | `desktop/frontend/src/lib/layout.ts`（纯函数 + 单测） + `composables/useTerminalShortcuts.ts`（document capture）+ `components/PaneGrid.vue` |
| 改自动更新 | `desktop/updater.go`（state machine + Ed25519/SHA256 校验）+ `desktop/scripts/`（平台 helper）+ `.github/scripts/sign-release-checksums.go` + `.github/workflows/build.yml` + Settings UI |
| 改 relay 启动安全策略 | `cmd/atterm-relay/main.go` + `cmd/atterm-relay/main_test.go` + `internal/relay/*_test.go` + `docs/spec/protocol.md` |
| 改 web 安全头 / 静态资源 | `internal/relay/server.go` + `web/src/...` (Vue 3 + Naive UI) + `web/tests/contract/*.mjs` |
| 改 web 文案 / 多语言 | `web/src/shared/i18n/messages/*.ts` + `desktop/frontend/src/i18n/messages/*.ts`；新增用户可见文案时同步中英两套 |
| 改桌面远程 relay 配置 | `desktop/app.go` + `desktop/config.go` + `desktop/relay_security.go` + `desktop/frontend/src/components/SettingsDialog.vue` |
| 改远程权限模型 | `internal/proto/frame.go` + `internal/relay/permissions.go` + `desktop/uplink.go` + Settings UI + 协议规范 |
| 改 relay admin 配置 | `internal/relay/admin_config.go` + `internal/relay/admin_http.go` + `cmd/atterm-relay/main.go` + README/spec |
| relay 注册表清理 | `internal/relay/uplink_conn.go`（writer ping fail 触发 cancelConn → cleanup mirror sessions） |
| 改移动 app relay 配置 | `web/src/setup/` + `web/src/shared/api/relay-config.ts` + `web/src/shared/mobile-guard.ts` + `web/src/settings/tabs/Relay.vue`；`apiFetch`/`wsUrl` 的 mobile 分支在 `web/src/shared/api/client.ts` 和 `web/src/shared/ws/client-conn.ts` |
| 改桌面前端 ↔ Go IPC | `desktop/frontend/src/platform/wails.ts`（适配器）；新方法先在 `desktop/app.go` 或 `desktop/plugin_*.go` 定义，让 Wails 重生成 `wailsjs/`，再在 `platform/wails.ts` 包一层。**不要**在 `src/platform/` 之外的文件直接 import `wailsjs/*`。 |
| 改 relay 出站 webhook（命令结束通知） | `internal/webhook/`（render/transport/dispatch/service，**不依赖** userstore）+ `internal/userstore/webhooks.go`（+ migration `0003_webhooks.sql` + `Store` 接口）+ `internal/relay/auth_http.go`（`/api/me/webhooks`）+ `internal/relay/uplink_conn.go`（命令结束分发点，紧挨 WebPush）+ `cmd/atterm-relay/main.go`（构造 service + `webhookStoreAdapter` 映射）+ `web/src/settings/tabs/Webhooks.vue` |

## 风格摘要

- Go：`gofmt`，包注释 + 公共 API 注释（说"为什么"）；errors `%w` wrap；不引入日志框架，用标准库 `log`
- TS：strict mode；不写 `any` 除非 wails generated；不加新前端依赖（现有栈含 Vue / xterm / Naive UI / Pinia / lucide / CodeMirror）
- 注释：写 *why* 不写 *what*；commit-specific / "added for X" / "see ticket Y" 不要写进代码注释（应该在 commit msg 或 PR）
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
- ❌ 把主 write token 持久化到 relay admin config；只能来自 env/flag/启动自动生成
- ❌ 只在访问者客户端 UI 隐藏输入按钮而不在 relay/desktop host 强制拦截

## 文档导引

- `docs/spec/architecture.md` — 整体架构、组件职责、数据流、phase 路线
- `docs/spec/protocol.md` — wire 协议完整规范（所有帧类型、重连续传语义）
- `docs/spec/conventions.md` — Go/TS 代码约定、commit 风格、测试组织
- `docs/spec/component-style.md` — 前端组件视觉、控件复用、Settings / dialog / pane 样式规范
