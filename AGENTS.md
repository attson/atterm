# AGENTS.md

atterm = 跨平台终端模拟器 + 内建会话云同步。所有从桌面 app 启动的会话默认可被任意设备的 web/桌面客户端 attach、查看历史、继续输入。核心场景：本机跑 codex/claude 的长 AI 任务，离开工位后用手机/另一台机器接管。

阅读这份文件以快速上手；详细规范见 `docs/spec/`。

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
│   └── frontend/           Vue 3 + Vite + TS（含 PaneGrid + ConfirmInstallDialog）
├── web/                    极简 vanilla 浏览器客户端（直连 atterm-relay）
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
9. **公网 relay 默认安全**：`cmd/atterm-relay` 未设置 `ATTERM_TOKEN` 时自动生成高强度 token 并打印到日志。公网监听拒绝弱 token（如 `dev` 或长度 <16），除非显式 `--dev-insecure`。relay 默认加 CSP/security headers，并按 IP/token 做 HTTP/WS rate limit 与连接数限制。桌面端默认拒绝非 loopback `ws://`，只有用户在 Settings 打开 insecure mode 才允许。
10. **Web 客户端不依赖 CDN**：`web/` 必须只加载同源静态资源；xterm 资源放在 `web/vendor/` 并由 service worker 缓存。不要重新引入外部 CDN script/style，否则 CSP/PWA 离线能力会回归。
11. **远程权限由 owner 决定、relay/host 强制执行**：桌面端通过 `remote_permission` 发布 view/control/full；relay 先拦截越权 `IN`/`RESIZE`/`PASTE_IMAGE`，desktop uplink 写本机 PTY 前再拦一次。relay read-only token 是运维侧兜底限制，和 owner 权限取交集。

## 开发命令

```bash
# macOS Homebrew 的 gh 在 /opt/homebrew/bin；Codex/CI 的非交互 shell
# 不一定加载你的 ~/.zshrc，所以本项目命令默认显式带上它。
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH

# 命令行 relay（Phase 0）
ATTERM_TOKEN=dev go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web

# 命令行 agent（Phase 0 wrapper，调试用）
ATTERM_TOKEN=dev go run ./cmd/atterm-agent --relay ws://localhost:8080 -- bash

# 桌面 app
cd desktop
wails dev -tags webkit2_41               # Linux 必须加 -tags webkit2_41
wails build -tags webkit2_41             # 出 desktop/build/bin/AT Term

# 测试
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./desktop/   # 跑 lazy uplink e2e 协议测试
cd desktop/frontend && npm run build               # 前端 type-check + build
node --test web/*.test.mjs                         # vanilla web/PWA 安全与 helper 测试

# 查看 GitHub Actions / release 状态（如 PATH 未加载，可直接用 /opt/homebrew/bin/gh）
gh run list --repo attson/atterm --limit 10
```

环境变量：
- `ATTERM_TOKEN`：relay 共享 bearer token；`atterm-relay` 启动时未指定会自动生成并打印到日志
- `ATTERM_READ_ONLY_TOKENS`：逗号分隔的只读 bearer token；可 list/attach/看输出，但 relay 会丢弃 `IN`/`RESIZE`/`PASTE_IMAGE`，且不能连 `/agent`/`/uplink`
- `ATTERM_RATE_LIMIT_PER_MINUTE`：每个远端 IP/token 的 HTTP 请求与 WS upgrade 分钟限额；`0` 用默认值，负数禁用
- `ATTERM_MAX_CONNECTIONS_PER_KEY`：每个远端 IP/token 的活跃 WS 连接上限；`0` 用默认值，负数禁用
- `ATTERM_RELAY_CONFIG`：relay admin 持久化 JSON 配置路径；只保存运行参数和 hash 后的只读 token，不保存主 write token
- `ATTERM_ADMIN_TOKEN`：启用 `/admin/` 与 `/admin/api/*`；只接受 `Authorization: Bearer`，不要放 URL query
- `ATTERM_RELAY_URL` / `ATTERM_RELAY_TOKEN`：桌面 app 首次启动时若无配置文件，从这俩 env 读初始值
- `ATTERM_HOST_ID`：覆盖 host id 文件（容器场景）
- `ATTERM_UPDATE_VERIFY_PUBLIC_KEY`：GitHub prod environment secret；base64 Ed25519 公钥，release 构建时注入桌面 app
- `ATTERM_UPDATE_SIGNING_PRIVATE_KEY`：GitHub prod environment secret；base64 Ed25519 私钥，只在 release job 里签 `SHA256SUMS`

## 何时改哪里

| 修改场景 | 触动文件 |
|---------|----------|
| 新协议帧类型 | `internal/proto/frame.go` + 协议规范 + 接收方 |
| 改 relay session 行为 | `internal/session/`（local + mirror 都用同一个 Session） |
| 桌面新 binding | `desktop/app.go`（手写 + Wails 自动生成 `frontend/wailsjs/go/main/App.*`） |
| 新前端组件 | `desktop/frontend/src/components/` |
| 桌面端协议路径 | `desktop/uplink.go` 或 `desktop/relay_host.go`，**不要**碰 `internal/agent/` |
| CLI wrapper 行为 | `internal/agent/` + `cmd/atterm-agent/` |
| 改 pane 布局 / 分屏键 | `desktop/frontend/src/lib/layout.ts`（纯函数 + 单测） + `composables/useTerminalShortcuts.ts`（document capture）+ `components/PaneGrid.vue` |
| 改自动更新 | `desktop/updater.go`（state machine + Ed25519/SHA256 校验）+ `desktop/scripts/`（平台 helper）+ `.github/scripts/sign-release-checksums.go` + `.github/workflows/build.yml` + Settings UI |
| 改 relay 启动安全策略 | `cmd/atterm-relay/main.go` + `cmd/atterm-relay/main_test.go` + `internal/relay/*_test.go` + `docs/spec/protocol.md` |
| 改 web 安全头 / 静态资源 | `internal/relay/server.go` + `web/index.html` + `web/sw.js` + `web/*test.mjs` |
| 改桌面远程 relay 配置 | `desktop/app.go` + `desktop/config.go` + `desktop/relay_security.go` + `desktop/frontend/src/components/SettingsDialog.vue` |
| 改远程权限模型 | `internal/proto/frame.go` + `internal/relay/permissions.go` + `desktop/uplink.go` + Settings UI + 协议规范 |
| 改 relay admin 配置 | `internal/relay/admin_config.go` + `internal/relay/admin_http.go` + `cmd/atterm-relay/main.go` + README/spec |
| relay 注册表清理 | `internal/relay/uplink_conn.go`（writer ping fail 触发 cancelConn → cleanup mirror sessions） |

## 风格摘要

- Go：`gofmt`，包注释 + 公共 API 注释（说"为什么"）；errors `%w` wrap；不引入日志框架，用标准库 `log`
- TS：strict mode；不写 `any` 除非 wails generated；不加新前端依赖（除 xterm/Vue 已有）
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
- ❌ 把主 write token 持久化到 relay admin config；只能来自 env/flag/启动自动生成
- ❌ 只在访问者客户端 UI 隐藏输入按钮而不在 relay/desktop host 强制拦截

## 文档导引

- `docs/spec/architecture.md` — 整体架构、组件职责、数据流、phase 路线
- `docs/spec/protocol.md` — wire 协议完整规范（所有帧类型、重连续传语义）
- `docs/spec/conventions.md` — Go/TS 代码约定、commit 风格、测试组织
