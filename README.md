# AT Term

[![Release](https://img.shields.io/github/v/release/attson/atterm?style=flat-square)](https://github.com/attson/atterm/releases)
[![License](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](./LICENSE)
[![Platforms](https://img.shields.io/badge/platforms-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey?style=flat-square)](#)
[![Go](https://img.shields.io/badge/go-1.23-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)

> **Audience**: 第一次见到 atterm 的人 / 想跑起来或部署的开发者
> **Last updated**: 2026-07-10
> **Status**: stable
> **See also**: [AGENTS.md](./AGENTS.md) · [docs/spec/auth.md](./docs/spec/auth.md) · [docs/spec/protocol.md](./docs/spec/protocol.md) · [docs/spec/feishu.md](./docs/spec/feishu.md) · [docs/roadmap.md](./docs/roadmap.md)

AT Term 是一个带远程接管能力的跨平台终端。你在桌面端启动的 shell、Codex、Claude 等长任务，可以在离开电脑后从手机、浏览器或另一台电脑继续查看和输入。Relay 端启用 E2EE 后，命令输出 / 标题 / cwd / 任务摘要在传输和持久化路径上都对 relay 不可读，只在拥有 `account_key` 的客户端解开。

> 一句话：本地先好用；需要远程时，再把会话安全地同步到自己的 relay。

## 工作原理（5 行速览）

1. 桌面端跑本地 PTY，每个 tab 是一个独立会话；OSC 133 推导任务状态（running / waiting / done / failed）。
2. 登录 relay 后，桌面端按需把会话状态 + PTY 字节上行；远程端（手机 / 浏览器 / 另一台桌面）attach 后才上传字节，无人看时不上传。
3. 启用 E2EE：account_key 只在客户端持有；输出 / 标题 / cwd / 摘要在 relay 侧全程密文。
4. 任务关键节点（命令完成、AI 等输入）触发系统通知 / Web Push / 飞书卡片 / 出站 webhook，payload 带 session id 与摘要。
5. AI agent（Claude Code / Codex / Aider / Gemini）自动识别：命令分类、resume 注入、Notification hook 自动安装。

## 适合谁

- 经常跑长时间 AI / 构建 / 运维任务，希望离开工位后还能接着看的用户。
- 想用一个轻量桌面终端，同时保留浏览器远程 attach 能力的开发者。
- 偏好自托管 + 端到端加密，不想把终端输入输出交给第三方平台的人。

## 现在能做什么

按用途分组（每个条目下面用一句说清来源 / 边界）：

**核心终端体验**

- 桌面端 macOS / Linux / Windows 三平台，多 tab、本地 PTY、cwd 跟踪。
- 每 tab 1 / 2 / 4 pane 分屏，分割线可拖拽缩放；macOS `⌘N` / `⌘⇧N`，其他平台 `Ctrl`。
- 主题 / 快捷键设置、右侧插件面板、Quick Input、文件浏览器、翻译插件。

**远程接管（lazy 同步）**

- 桌面连上 relay 后，其他浏览器 / 桌面端 / iOS app 可 attach 同一会话；默认 viewer 模式，按空格 take over 才能写。
- 远程没人看时不上传 PTY 字节；权限由桌面端的 `remote_permission` 字段定义（`view` / `control` / `full`），relay 和 host 双重强制。
- 远程接管伴随 viewer 数量徽章（👁 N）与连接健康指示（RTT pill + 抽屉详情）。

**任务状态闭环**

- OSC 133 推导 running / waiting-input / completed / failed / disconnected 状态，三端同步。
- 命令完成触发系统通知、Web Push、出站 webhook（飞书 / generic JSON）；payload 带 session id / 任务类型 / summary。
- AI 任务控制台：自动给 `codex` / `claude` / `gemini` / `aider` / `go test` / `docker build` / `kubectl` 等命令打 type chip；失败卡片附 error 行；可配快捷模板（`yes / ok / continue / commit / push / release / 1 / 2 / 3` 默认，hotkey 可绑）。

**飞书作为远程终端（v0.2.171+）**

- **锚点卡片**在飞书 DM 里作为一张持续更新的 IM 卡片承载 AI 会话：body markdown 流式接收 assistant 输出，输入框和 `^C / ^D / Esc / Enter / 结束` 按钮直接把用户操作送回本地 PTY，不用打开 desktop。
- **AskUserQuestion 表单式回答**：`claude-code` 触发的 AskUserQuestion 会在锚点卡片上生成对应 form —— 每题一行下拉 + 自定义输入框（含 "Type something." 分支），支持单选 / 多选。用户在飞书表单上选好答案提交，系统自动按 claude TUI 需要的按键序列送到本地 pty，form 走完到 Submit fire，claude 收到答案。
- **local 与 relay 双模式**：local 走本机 LongConn（配置存钥匙串），relay 走中央中转（配置存 sqlite）。设置项、绑定回复、autoAttach 语义在两种模式下一致。
- 详情见 [docs/spec/feishu.md](./docs/spec/feishu.md) 和 shipped design record [docs/superpowers/specs/2026-07-10-askuserquestion-form-flow-spec.md](./docs/superpowers/specs/2026-07-10-askuserquestion-form-flow-spec.md)。

**移动端 / Web / PWA**

- Web 端 Vue 3 + TS + Naive UI 多页应用（login / signup / main / settings / admin / setup），中英双语。
- iOS Capacitor 壳：QR 扫码配对（5min 一次性 token）/ 手动登录 / Keychain 凭据持久化 / 防误触模式 / 中文输入法补获 / `@capacitor/camera` 图片菜单 / 隐藏键盘辅助条 / viewer 锁 PTY 尺寸。
- 移动任务首页按状态分组（需关注 / 运行中 / 完成 / 失败 / 断连）、列表分组可折叠。

**Relay 自托管**

- 邮箱 + 密码登录，邀请码注册，session token 走 `Authorization: Bearer` 或 WS subprotocol，不出现在 URL。
- 公网默认 fail-closed：OPAQUE 密码认证（relay 不见明文密码）+ claim-token 引导 admin、`ATTERM_ORIGINS` 白名单、CSP / 限流 / 连接数上限、Ed25519 签名的自动更新。
- Pairing QR、`/healthz` 公共健康端点 + `/admin/health` 管理页、admin 后台、出站 webhook。

**端到端加密（M1–M6，relay-e2ee-design）**

- OPAQUE aPAKE 注册 / 登录 / step-up（P-256-SHA256 + Scrypt）；服务器从不接触明文密码。
- 32 字节 `account_key` 随机生成、用 Argon2id 派生的 wrap key + XChaCha20-Poly1305 封装存 relay；客户端用密码当场解开后存 sessionStorage / Keychain / Keyring。
- 终端输出 / 标题 / cwd / 当前命令 / 任务摘要 / 命令完成 push body 在 agent 端用 HKDF-SHA256(account_key) 派生的 session key 封装；relay 转发不解开，只看 routing 必需的 session id / 时间戳。AAD 由 `uuid || frame_type` 鉴别帧类型，防止 cross-type 替换重放。
- 详见 [`docs/superpowers/specs/2026-06-15-relay-e2ee-design.md`](docs/superpowers/specs/2026-06-15-relay-e2ee-design.md)，wire 格式见 [`docs/spec/protocol.md`](docs/spec/protocol.md) §E2EE 信封。

**桌面诊断 + 启动稳定性**

- Settings → Diagnostics 一键导出脱敏的 app / OS / WebView / uplink / 配置摘要，方便贴 issue。
- Settings → Logging 配 log level + 文件路径，内置 viewer + 3s 实时尾部预览，不用切到 Finder 找日志文件。
- 启动链按 `bootStage` 分阶段，失败时 titlebar 直接显示 `connectLocalSessionList: SyntaxError: …`；`new WebSocket()` 同步异常被隔离重连，不会击穿 boot await chain。

路线图未完成：桌面安装包 codesign / notarization（P1.8）、单 session 分享 + presence + 审计日志（P3）、可选持久化历史 + 命令级回放（P4）、E2EE 外部加密评审（M7-audit）。详见 [`docs/roadmap.md`](docs/roadmap.md) 和 [`docs/spec/architecture.md`](docs/spec/architecture.md) §phase 完成度。

## AI agent 支持

`内置识别`：会话内启动的 AI CLI 会被自动归类并显示对应 type chip（来源：`desktop/ai_sid_sniff.go` + 前端 `lib/aiKind.ts`）。

| Agent | type chip | resume 注入 | session jsonl 嗅探 | Notification hook | 飞书 AskUserQuestion 远程回答 |
|---|---|---|---|---|---|
| **Claude Code** (`claude`) | ✅ | `claude --resume <sid>` | `~/.claude/projects/<cwd>/<sid>.jsonl` | ✅ atterm 自动安装 + 健康巡检（Feishu 路径） | ✅ 锚点卡上表单选择,提交后自动按键送 pty（v0.2.171） |
| **Codex** (`codex`) | ✅ | `codex resume <sid>` | `~/.codex/sessions/YYYY/MM/DD/rollout-*-<sid>.jsonl` | ⏳ 自动安装路径未做（hook 协议复用既有） | — |
| **Aider** (`aider`) | ✅ | 直接重放上一条 command line | —（无稳定 jsonl 协议） | — | — |
| **Gemini CLI** (`gemini`) | ✅ | —（暂无） | — | — | — |
| 其他（`go test` / `docker build` / `kubectl` …） | ✅ 命令分类 | — | — | — | — |

权限审批 / AskUserQuestion 等待这两条信号目前仅 Claude Code 走 Notification hook 路径；Codex 走 jsonl 监听是后续 spec。飞书 AskUserQuestion 表单式远程回答仅覆盖 Claude Code（依赖 PreToolUse hook + 反向工程的 TUI 按键模型,详见 [docs/spec/feishu.md](./docs/spec/feishu.md) §4）。

### 会话恢复

桌面端把 tab / pane 结构 + 已捕获的 AI sid 持续写入 `~/.config/atterm/recovery.json`（dev 模式走 `<repo>/desktop/.atterm-dev/recovery.json`）。下次启动弹恢复对话框，可挑选恢复哪些 tab：

- **本机 shell** — 用原 cwd 重 spawn 一个新 PTY。
- **远端 pane** — 用旧 `session_id` 直接 rebind 到 relay；relay 上 session 还活就接回去，挂了就显示 `disconnected` 占位、保留标题。
- **AI 会话** — Go 端在恢复 shell 第一次 prompt 时直接写 `claude --resume <sid>` / `codex resume <sid>`，不用你手敲；保留原启动命令的 flag（`--permission-mode` 等）。`aider` 无稳定 sid，改重放上次的整条 last command line。

AI sid 抓取在 session 启动时自动进行（OSC 133 D 事件触发分类 + claude/codex 的 session jsonl 文件 mtime 跟踪），抓不到就退化为普通 shell 恢复、不注入 resume —— "抓不到" 优于 "抓错的对话"。

对话框可在 Settings → General 关闭，关掉后下次启动直接走 startNewTab。

## 快速开始

### 方式 A：只用桌面端

1. 到 [Releases](https://github.com/attson/atterm/releases) 下载对应平台的包。
2. 解压或安装后启动 `AT Term`。
3. 像普通终端一样新建 tab、分屏、运行命令。

| 平台 | 推荐文件 | 备注 |
|------|----------|------|
| macOS Apple Silicon | `AT-Term_*_arm64.dmg` 或 `AT-Term-darwin-arm64.zip` | Intel 机器选 `amd64` |
| Linux x64 | `AT-Term_*_amd64.deb` 或 `AT-Term-linux-amd64.tar.gz` | 需要 `libwebkit2gtk-4.1-0` |
| Linux arm64 | `AT-Term_*_arm64.deb` 或 `AT-Term-linux-arm64.tar.gz` | 适合 ARM Linux 桌面 |
| Windows x64 | `AT-Term_*_amd64.exe` 或 `AT-Term-windows-amd64.zip` | Windows 11 自带 WebView2；Windows 10 可能需另装 |

### 方式 B：桌面端 + 自托管 relay（用户账号制）

这个模式适合「电脑上跑任务，手机/另一台机器接管」。

1. 启动 relay（只需 bootstrap 管理员邮箱 + Origin 白名单，**不需要密码 env**——密码由你后面在浏览器注册时本地设置，走 OPAQUE）：

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_ORIGINS='https://relay.example.com' \
docker compose up -d atterm-relay
```

2. 看启动日志里打印的一次性 **claim token**（`docker compose logs atterm-relay`，形如 `bootstrap-admin: claim token for you@example.com …`）。该 token 7 天有效、只显示一次。
3. 在浏览器打开 `https://relay.example.com/signup.html`，用 bootstrap 邮箱 + 自设密码注册，并把上一步的 **claim token 填进「邀请码 / claim token」输入框**。注册完成即获得 admin 角色（claim token 校验邮箱匹配后自动提权）。
4. 用 bootstrap 邮箱 + 刚设的密码在 `https://relay.example.com/login.html` 登录；顶部导航出现 **Admin** 入口。飞书、限流、Origin 等都在 Admin → Config / Feishu 里配置（见“部署 relay”）。
5. 在桌面端 Settings → Remote relay 填入 relay URL，用注册时的邮箱+密码登录；登录成功后 relay 会下发一份 session token（`ses_…`）并由桌面端持久化。
6. 桌面端连上 relay 后，会显示 `connected as <email>`。手机或另一台电脑用同一账号登录 `https://relay.example.com` 即可看到 session 列表。

### 方式 C：源码启动调试

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH

# 终端 1：启动 relay（--dev-insecure 跳过 Origin 校验与 admin 引导）
# --web 省略时使用 internal/relay/web-dist/ 的内嵌 web 构建产物。
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure

# 终端 2（可选）：调试 web 前端。Vite dev server 会把 /api、/client 等代理到 127.0.0.1:8080。
cd web
npm ci
npm run dev

# 终端 3：启动桌面端
cd desktop
wails dev -tags webkit2_41   # Linux 需要 tag；macOS/Windows 可省略
```

浏览器访问 `http://127.0.0.1:8080/signup.html`（或 Vite 的 `http://127.0.0.1:5173/signup.html`），用邀请码注册账号；之后在桌面端 Settings → Remote relay 填入 relay URL 并用邮箱+密码登录，relay 会下发 session token 由桌面端持久化。

### 方式 D：iOS WebView MVP

`mobile/` 提供一个 Capacitor iOS 壳，把现有 `web/` 客户端打包进 `WKWebView`。手机端仍然只连接远程 relay，不在 iOS 本地运行 PTY。

```bash
cd mobile
npm install
npm run ios:add   # 首次创建 Xcode 工程；已存在时不用重复执行
npm run ios:open  # 自动 npm install + 重新 build web 产物 + cap sync + 打开 Xcode
```

iOS App 首次启动后有两种配置方式：

**首选：QR 配对**

1. 桌面端连接好 relay 后，打开 Settings → Pairing，点 `Generate QR code`。
2. iOS App 点 Settings → Pair with desktop（或首次 setup 直接扫码），用相机扫码即可。
3. 移动端会自动调 `/api/pair/consume` 拿到 relay URL + 一份独立的 session token，并写入 iOS Keychain。
4. token 5 分钟内一次性有效；扫成功一次后立刻失效，没扫到自然过期，不需要手动撤销。

**fallback：手动填写**

```text
relay URL: https://relay.example.com
邮箱:      you@example.com
密码:      <注册时设置的密码>
```

移动端登录后 relay 下发的 session token（`ses_…`）写入 iOS Keychain，不需要手动复制粘贴。

如果只是用公网 IP:port 做内测，可以在手机端勾选 `allow insecure HTTP relay` 后填写：

```text
relay URL: http://121.43.40.128:23301
邮箱:      you@example.com
密码:      <注册时设置的密码>
```

insecure mode 只适合可信测试环境；正式使用建议配置 HTTPS/WSS 域名，iOS App Transport Security 默认禁止明文 HTTP。

公网 relay 需要允许 Capacitor WebView 的 Origin：

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_ORIGINS='https://relay.example.com,capacitor://localhost' \
go run ./cmd/atterm-relay --addr :8080
```

详见 [`mobile/README.md`](mobile/README.md)。

## 常见使用场景

### 在电脑上跑 AI 任务，手机继续看

1. 在 AT Term 新建会话，运行 `codex`、`claude`、构建命令或部署脚本。
2. 确认桌面端已连接远程 relay。
3. 手机打开 relay web 地址。
4. 点进对应 session，就能看历史输出；有权限时也能继续输入。

### 让同事查看会话

如需让同事 attach 查看，通过 admin 后台为其创建一个账号邀请（`inv_…`），对方注册后即可用自己的账号登录 relay 查看会话。relay 级别的共享只读 token 已在用户账号版本中移除；权限控制现在通过桌面端的 `remote_permission` 字段实现。

### Feishu — 远程终端 + AskUserQuestion 远程回答

`claude-code` 在 atterm session 内运行时，其 PreToolUse / Notification hook 触发 `atterm-hook` CLI（`ATTERM_SESSION_ID` 和 `ATTERM_HOOK_ENDPOINT` 环境变量已自动注入每个 PTY）。CLI 把 hook payload POST 到桌面进程，桌面进程按以下路径向飞书 DM 推送和接收：

- **anchor card**（锚点卡片）：一张持续更新的 IM 卡，body 流式接收 assistant 输出，输入框和 `^C / ^D / Esc / Enter / 结束` 按钮把用户操作直接送回本地 pty。
- **AskUserQuestion form**：`claude-code` 触发 AskUserQuestion 时，锚点卡片上生成对应表单（每题一行 select_static 下拉 + 自定义输入框，支持单选和多选）。用户在飞书上填完提交，桌面进程按反向工程的 TUI 按键模型送回本地 pty，claude 收到答案后 form 消失、锚点卡片恢复到普通输入 + 按钮态。
- **前置要求**：本机 `claude-code` 首次遇到 AskUserQuestion 权限对话框时，选 "Yes, and don't ask again" 一次；否则每次都要在飞书回答前先在本地授权（详见 memory `feedback_askform_permission_grant.md`）。
- **local vs relay**：`internal/feishu` 支持两种部署 —— local 走本机 LongConn 直连飞书（credentials 存钥匙串），relay 走中央 relay 服务（credentials 存 relay 的 `users.db` + `AdminConfig.Feishu`）。桌面 Settings → Feishu 里可切换。

完整设计与按键模型见 [docs/spec/feishu.md](./docs/spec/feishu.md) 和 shipped design record [docs/superpowers/specs/2026-07-10-askuserquestion-form-flow-spec.md](./docs/superpowers/specs/2026-07-10-askuserquestion-form-flow-spec.md)。手工端到端检查清单见 [scripts/feishu-hook-e2e-checklist.md](scripts/feishu-hook-e2e-checklist.md)。

### 启用端到端加密

新账号在注册时（`signup.html` / 桌面 Settings → Remote relay → Register）走 OPAQUE 流程，浏览器 / 桌面端在本地随机生成 32 字节 `account_key`，用 Argon2id 派生的 wrap key + XChaCha20-Poly1305 封装成 wrap blob 上传，relay 只看到 wrap 不看密码。登录时同样在本地 OPAQUE 后用密码解 wrap 拿回 `account_key`，存 sessionStorage / Keychain / Keyring。

只要 `account_key` 解锁，agent 就会自动开 E2EE：

- 终端 OUT 字节、会话标题 / cwd / 当前命令、任务摘要、命令完成 push body 都封装上链，relay 收到的就是密文。
- 浏览器 / 桌面 / 移动端在本地解密；service worker 命令完成通知会通过 `MessageChannel` 找到可见 tab 解密，渲染出富文本（无可见 tab 时退化到 "AT Term · Session command finished" 通用文案）。
- Webhook 接收端（飞书 / generic JSON）此时只看到 `Session command finished` + 一段 base64 `sealed_body`；要解开就拿 `account_key` 自己 AEAD-open。

特殊门控：硬删除账号 `DELETE /api/me` 走 step-up（再走一次 OPAQUE login 换 60s 一次性 token）。**没有密码找回**——忘记密码只能 admin reset，相当于换一把新 `account_key`，旧会话的 sealed ringbuf 内容永久不可解；这是单用户自托管定位下的设计选择。

详见 [`docs/spec/auth.md`](docs/spec/auth.md) §OPAQUE / `account_key` 与 [`docs/superpowers/specs/2026-06-15-relay-e2ee-design.md`](docs/superpowers/specs/2026-06-15-relay-e2ee-design.md)。

### 选择远程权限

桌面端 Settings 可以为本机 session 设置远程权限：

| 权限 | 远程用户可以做什么 |
|------|--------------------|
| `view` | 只能查看输出和历史 |
| `control` | 可以输入和 resize |
| `full` | 保留完整远程控制能力 |

权限由桌面端 `remote_permission` 设置决定；relay 和 desktop host 都强制执行。`view` 用户即使有完整账号，也只能查看输出，不能输入 / resize / 粘贴图片。

## 部署 relay

### Docker Compose

最简单的部署方式：

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
  docker compose up -d atterm-relay
docker compose logs atterm-relay
```

compose 默认只起明文 HTTP `:8080`，**前面要挂一个 TLS 终止反代**（Cloudflare/Caddy/nginx/Tailscale）。浏览器经反代的 `https://relay.<你的域名>` 访问，没有 admin 时会自动进入首次安装页创建管理员。

> **为什么必须 HTTPS**：OPAQUE 登录用浏览器 WebCrypto，而它只在「安全上下文」(HTTPS 或 `localhost`) 可用，明文 HTTP 在公网 IP 上无法登录。**relay 不再自带自签证书**——浏览器直连的 TLS 必须来自真证书或前置反代。
>
> - **前置反代（推荐）**：Caddy/nginx/Tailscale/Cloudflare 终止 TLS，反代到 `:8080` HTTP 端口。
> - **relay 直接跑 HTTPS**：提供真证书 `ATTERM_TLS_CERT`/`ATTERM_TLS_KEY`，并加 `--https-addr :8443`（缺证书会直接报错退出，没有自签回退）。
> - **`:8080` 是明文 HTTP 端口**：仅供反代后端或内网用，浏览器直连它登录不了。
> - 仅本机临时用：`ssh -L 8080:127.0.0.1:8080 <host>` 后开 `http://localhost:8080`（loopback 是安全上下文）。

大多数配置已下沉到管理后台（Admin → Config / Feishu），持久化在 `<config-dir>/relay.json`，运行时即可修改、无需重启（VAPID subject 除外）。**启动只需要核心 env**；其余 env 仍被支持，会在首次启动时一次性「播种」进 `relay.json`。

核心环境变量：

| 变量 | 用途 |
|------|------|
| `ATTERM_BOOTSTRAP_ADMIN_EMAIL` | 启动时为该邮箱打印一次性 claim token（见下「Bootstrap admin」）；用它注册一个**新**账号即获得 admin。公网监听必须设置（除非 `--dev-insecure`） |
| `ATTERM_ORIGINS` | 浏览器 Origin 白名单；公网部署必须设成真实域名（也可后台 Admin → Config 修改） |
| `ATTERM_RELAY_PORT` | 宿主机端口，默认 `8080` |
| `ATTERM_RELAY_CONFIG_DIR` | relay 持久化配置目录（含 `users.db`、`relay.json`），默认 `./data/atterm-relay` |

可选环境变量（不设也能启动；现在更推荐在管理后台配置）：

| 变量 | 用途 |
|------|------|
| `ATTERM_FEISHU_ENCRYPT_KEY` | 飞书应用凭据 AEAD 静态加密密钥（32 字节 base64）。**不再必填**——在 Admin → Feishu 里可一键「生成」并保存。仅当你想用 env 预置时设置（`openssl rand -base64 32`），首次启动会播种进 `relay.json` 并自动启用飞书 |
| `ATTERM_FEISHU_BASE_URL` | 飞书 Open Platform 基础 URL，默认 `https://open.feishu.cn`；私有化部署时覆盖（也可后台配置） |
| `ATTERM_VAPID_SUBJECT` | Web Push VAPID subject，默认 `mailto:noreply@atterm.local`（改动需重启 relay） |
| `ATTERM_RATE_LIMIT_PER_MINUTE` | 每个 IP 的请求与 WS upgrade 分钟限额（也可后台 Admin → Config 修改） |
| `ATTERM_MAX_CONNECTIONS_PER_KEY` | 每个 IP 的活跃 WebSocket 连接上限（同上） |

公网示例：

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_ORIGINS='https://relay.example.com' \
docker compose up -d atterm-relay
```

如果希望 Docker Hub `latest` 更新后自动拉取并重启 relay：

```bash
docker compose --profile auto-update up -d
```

该模式使用 watchtower，并需要挂载 Docker socket；不需要自动更新时，不要启用这个 profile。

### Go 直接运行

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_ORIGINS='https://relay.example.com' \
go run ./cmd/atterm-relay --addr :8080
```

本地开发可以临时跳过 Origin 校验（loopback 时 bootstrap env 可省略，relay 不会打印 claim token）：

```bash
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure
```

`--web` 省略时使用 `internal/relay/web-dist/` 的内嵌 web 构建产物；需要测试当前工作区的 web 改动时，先 `cd web && npm run build`，再从仓库根目录传 `--web web/dist`。

公网监听默认拒绝缺失 `ATTERM_BOOTSTRAP_ADMIN_EMAIL` 或缺失 Origin 白名单。只有明确传 `--dev-insecure` 才会放开这些限制；不要在公网生产环境使用。

### Bootstrap admin

密码认证已切换到 OPAQUE（密码只在客户端本地参与协议，**relay 永不接收明文密码**），因此 bootstrap 不再用密码 env，而是用一次性 **claim token**：

- **`ATTERM_BOOTSTRAP_ADMIN_EMAIL` 未设置**：relay 正常启动，不打印 claim token、也不创建 admin。可手动提权某用户（`UPDATE users SET is_admin=1 WHERE email='you@…';`）。
- **email 已设置**：relay 每次启动都为该 email 打印一条 7 天有效、**单次使用**的 claim token。在 `/signup.html` 用**该邮箱注册一个新账号**并把 token 填进「邀请码 / claim token」框，注册完成即创建用户并提为 admin（token 随即作废）。

claim token 是 OPAQUE 注册的「会合凭据」：`register/finalize` 校验 token 邮箱匹配 → 创建用户 → 消费 token → 提权。token 只在日志里出现一次、不可找回，过期或忘了就重启 relay 重新生成。注意它只能用于**注册新账号**——若该邮箱已注册过，token 无法消费（邮箱已占用），此时改用上面的 SQL 直接提权。

公网监听场景下 `ATTERM_BOOTSTRAP_ADMIN_EMAIL` 是必需的（缺失时 relay 拒绝启动，除非 `--dev-insecure`）。

### 管理后台（Admin）

以 admin 账号登录后，顶部导航出现 **Admin**，大部分运行时配置都在这里改、保存即生效、无需重启（持久化到 `<config-dir>/relay.json`，权限 0600）：

- **Config**：速率限制、每 key 连接上限、Origin 白名单、详细日志开关（`debug` / `debug_payload`，后者会记录终端输入输出，属敏感信息，仅排查时临时开）。
- **Feishu**：飞书集成开关 + 加密密钥（可一键「生成」32 字节安全码）+ Open Platform base URL。保存后 relay 即可热接入飞书，无需重启；关闭即拆除。该密钥用于加密 `users.db` 里的飞书凭据，存在 `relay.json`（GET 接口只回显末 4 位、绝不返回明文）。
- **Invitations / Users**：邀请记录与用户/角色管理。

> 唯一需要重启才生效的是 VAPID subject（Web Push 启动时一次性消费）。

## 安全模型

AT Term 的默认策略是 fail-closed：

- 公网 relay 必须提供 `ATTERM_BOOTSTRAP_ADMIN_EMAIL`（缺失则拒绝启动，除非 `--dev-insecure`）；admin 通过启动日志里的一次性 claim token 完成 OPAQUE 注册获得。密码走 OPAQUE，relay 永不接收明文密码。
- 公网 relay 必须使用明确的 `ATTERM_ORIGINS`。
- 服务端鉴权不接受 `?token=` 参数；session token 通过 `Authorization: Bearer` 或浏览器 / 桌面 WebSocket 的 `Sec-WebSocket-Protocol` 传递，避免写进 URL。
- session token（`ses_…`）以 sha256 哈希存储，明文只在登录 / 配对响应里返回给客户端一次，由客户端自行持久化。
- web 客户端只加载同源静态资源，不依赖 CDN。
- relay 默认启用 CSP、Referrer-Policy、nosniff、Permissions-Policy 等安全头。
- relay 按远端 IP 和认证后的 token hash 做限流与连接数限制。
- 桌面端默认拒绝非 loopback 的明文 `ws://` relay；可信内网需要在 Settings 手动开启 insecure mode。
- 自动更新必须通过 Ed25519 签名和 SHA256 校验；缺公钥、缺签名、签名不匹配或 hash 不匹配都会失败。

完整鉴权模型（含 bootstrap、pairing、错误码）见 [docs/spec/auth.md](./docs/spec/auth.md)。

## 开发环境

### 依赖

| 工具 | 版本 | 用途 |
|------|------|------|
| Go | 1.23+ | 后端、relay、PTY host |
| Node.js | 20+ | 桌面前端构建 |
| Wails CLI | v2.12.0 | 桌面应用开发与打包 |
| Xcode | 15+ | iOS WebView MVP 构建 |

安装 Wails：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
export PATH="/opt/homebrew/bin:$(go env GOPATH)/bin:$HOME/sdk/go1.23.12/bin:$PATH"
wails version
```

国内拉 Go 模块慢时：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

### Linux 依赖

Ubuntu 24.04 / Debian 12：

```bash
sudo apt update
sudo apt install -y build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
```

Ubuntu 22.04 可能需要额外启用 WebKitGTK 4.1 来源：

```bash
sudo add-apt-repository ppa:webkit-team/ppa
sudo apt install -y libwebkit2gtk-4.1-dev
```

### 常用命令

```bash
# 仓库根目录
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./desktop/
go build ./...

# 桌面前端
cd desktop/frontend
npm ci
npm test
npm run build
npm run dev

# Web / PWA 前端
cd web
npm ci
npm run build
npm test
npm run test:contract

# Capacitor mobile wrapper
cd mobile
npm test

# 桌面 app
cd desktop
wails dev -tags webkit2_41      # Linux
wails build -tags webkit2_41    # Linux
wails dev                       # macOS / Windows
wails build                     # macOS / Windows
```

macOS 上 Homebrew 的 `gh` 通常在 `/opt/homebrew/bin/gh`。非交互 shell 找不到时可以直接使用完整路径：

```bash
/opt/homebrew/bin/gh run list --repo attson/atterm --limit 10
```

## 仓库结构

```text
cmd/          atterm-relay 入口
internal/     proto、session、relay、ptyhost、hostid、ringbuf 等复用包
desktop/      Wails 桌面 app：Go 后端 + Vue 3 + xterm.js 前端
web/          Vue 3 + TypeScript + Naive UI 浏览器/PWA 客户端（MPA）
mobile/       Capacitor iOS WebView wrapper，打包 web/ 静态资源
docs/spec/    架构、协议、工程约定
.github/      CI、release、打包脚本
```

更多模块职责见 [`AGENTS.md`](AGENTS.md)。

## 文档

- [`docs/spec/architecture.md`](docs/spec/architecture.md)：架构、数据流、生命周期、路线图。
- [`docs/spec/protocol.md`](docs/spec/protocol.md)：二进制 WebSocket 帧协议、帧类型、E2EE 信封、重连语义。
- [`docs/spec/auth.md`](docs/spec/auth.md)：session token、OPAQUE、`account_key` wrap、step-up、错误码字典。
- [`docs/spec/conventions.md`](docs/spec/conventions.md)：Go / TypeScript 风格、测试组织、提交约定。
- [`docs/spec/component-style.md`](docs/spec/component-style.md)：前端组件视觉与控件规范。
- [`docs/spec/feishu.md`](docs/spec/feishu.md)：飞书子系统规范（模式、锚点卡片生命周期、AskUserQuestion form flow、已知限制）。
- [`docs/shell-integration.md`](docs/shell-integration.md)：OSC 133 shell 集成机制、各 shell 的注入方式、如何手动卸载。
- [`docs/web-push.md`](docs/web-push.md)：浏览器 / PWA 订阅 Web Push 命令完成通知的启用方式、iOS 限制、自托管关键管理。
- [`docs/superpowers/specs/2026-06-15-relay-e2ee-design.md`](docs/superpowers/specs/2026-06-15-relay-e2ee-design.md)：E2EE 完整设计（威胁模型、cipher suite、key 派生链、AAD 鉴别、实现状态表）。
- [`AGENTS.md`](AGENTS.md)：给开发代理和贡献者看的项目红线与修改指引。

## 贡献前检查

提交前建议至少跑：

```bash
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 ./...
cd desktop/frontend && npm run build
cd web && npm run build && npm test && npm run test:contract
cd mobile && npm test
```

如果改了协议或权限模型，请同步更新 [`docs/spec/protocol.md`](docs/spec/protocol.md) 和相关客户端实现。

## 许可证

本项目以 [Apache License 2.0](LICENSE) 发布。贡献代码即表示同意把贡献以同一协议授权出去。
