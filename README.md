# AT Term

AT Term 是一个带远程接管能力的跨平台终端。你在桌面端启动的 shell、Codex、Claude 等长任务，可以在离开电脑后从手机、浏览器或另一台电脑继续查看和输入。

> 一句话：本地先好用；需要远程时，再把会话安全地同步到自己的 relay。

## 适合谁

- 经常跑长时间 AI / 构建 / 运维任务，希望离开工位后还能接着看的用户。
- 想用一个轻量桌面终端，同时保留浏览器远程 attach 能力的开发者。
- 偏好自托管，不想把终端输入输出交给第三方平台的人。

## 现在能做什么

| 能力 | 状态 |
|------|------|
| 桌面终端 | 支持 macOS / Linux / Windows，多 tab、本地 PTY、cwd 跟踪 |
| 分屏 | 每个 tab 支持 1 / 2 / 4 pane，macOS 用 `⌘N` / `⌘⇧N`，Linux/Windows 用 `Ctrl` |
| 远程接管 | 桌面端连上 relay 后，其他浏览器或桌面端可 attach 同一会话 |
| 手机浏览器 / iOS App | 支持 PWA、Capacitor iOS WebView MVP、会话列表、触控终端、常用快捷键 |
| Lazy 同步 | 没有远程用户观看时不上传 PTY 字节，本地体验不依赖 relay |
| 自动更新 | 桌面端可手动检查、下载、确认重启安装；release 包先验签再安装 |
| 公网 relay 安全默认值 | 强 bootstrap 管理员密码、Origin 白名单、CSRF、限流、安全响应头 |
| Shell 集成（OSC 133） | macOS / Linux 自动注入 zsh / bash / fish hook；Windows 自动注入 PowerShell；命令完成 ≥10s 且窗口未聚焦时发系统通知 |
| Web Push 通知 | 浏览器和 PWA 订阅后，命令完成事件通过 self-hosted Web Push 推送，即使页面没打开也能收到（依赖 shell 集成 + 已连远端 relay） |
| 用户系统 | ✓ 支持（v2+）：邀请码注册、per-user API token、用户独立的会话列表与 Web Push；admin 后台管理用户与邀请 |

还在路线图中的能力：TLS 自动化、移动端原生体验增强、主题/字体配置。详见 [`docs/spec/architecture.md`](docs/spec/architecture.md)。

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

1. 启动 relay（设置 bootstrap 管理员邮箱 + 密码，密码至少 16 字符、含 3 类字符，不能在弱密码黑名单内）：

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
ATTERM_ORIGINS='https://relay.example.com' \
docker compose up -d atterm-relay
```

2. 在浏览器打开 `https://relay.example.com/login.html`，用 bootstrap 邮箱 + 密码登录；登录后顶部导航会出现 **Admin** 入口。登录成功后请把 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` 从环境/systemd unit 中删除并重启，避免明文密码长期留在进程环境里。
3. 在 “Invitations” 页面创建一个邀请码（`inv_…`），把它发给要使用的人（包括你自己）。
4. 用户在 `https://relay.example.com/signup.html` 用邀请码 + 邮箱 + 密码完成注册。
5. 登录后在 `/settings.html` 生成 API token（`atk_…`，**只显示一次**），复制后粘贴到桌面端 Settings → API token 字段。
6. 桌面端连上 relay 后，会显示 `connected as <email>`。手机或另一台电脑用同一账号登录 `https://relay.example.com` 即可看到 session 列表。

### 方式 C：源码启动调试

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH

# 终端 1：启动 relay + web 客户端（--dev-insecure 跳过 Origin/密码强度校验）
# loopback 调试时 bootstrap envs 是可选的：不设就不会自动创建 admin 用户
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web --dev-insecure

# 终端 2：启动桌面端
cd desktop
wails dev -tags webkit2_41   # Linux 需要 tag；macOS/Windows 可省略
```

浏览器访问 `http://127.0.0.1:8080/signup.html`，用邀请码注册账号；之后在 `/settings.html` 生成 API token，填入桌面端 Settings → API token 字段。

### 方式 D：iOS WebView MVP

`mobile/` 提供一个 Capacitor iOS 壳，把现有 `web/` 客户端打包进 `WKWebView`。手机端仍然只连接远程 relay，不在 iOS 本地运行 PTY。

```bash
cd mobile
npm install
npm run ios:add   # 首次创建 Xcode 工程；已存在时不用重复执行
npm run ios:open  # 同步 web/ 静态资源并打开 Xcode
```

iOS App 首次启动后，在 Settings 里填写：

```text
relay URL: https://relay.example.com
API token: atk_...（在 relay /settings.html 页面生成）
```

如果只是用公网 IP:port 做内测，也可以在手机端勾选 `allow insecure HTTP relay` 后填写：

```text
relay URL: http://121.43.40.128:23301
API token: atk_...
```

这个 insecure mode 只适合可信测试环境；正式使用仍建议配置 HTTPS/WSS 域名。

公网 relay 需要允许 Capacitor WebView 的 Origin：

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
ATTERM_ORIGINS='https://relay.example.com,capacitor://localhost' \
go run ./cmd/atterm-relay --addr :8080 --web web
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
docker compose up -d atterm-relay
docker compose logs atterm-relay
```

常用环境变量：

| 变量 | 用途 |
|------|------|
| `ATTERM_BOOTSTRAP_ADMIN_EMAIL` | 启动时将该邮箱对应的用户标记为 admin；用户不存在时配合 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` 创建新用户。公网监听必须设置（除非 `--dev-insecure`） |
| `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` | 首次启动用来创建 admin 用户的明文密码；须满足 ≥16 字符、≥3 类字符、不在弱密码黑名单内。若用户已存在则忽略。**首次登录后请从环境中删除**，避免明文密码长期留在进程状态里 |
| `ATTERM_ORIGINS` | 浏览器 Origin 白名单；公网部署必须设成真实域名 |
| `ATTERM_RELAY_PORT` | 宿主机端口，默认 `8080` |
| `ATTERM_RELAY_CONFIG_DIR` | relay 持久化配置目录，默认 `./data/atterm-relay` |
| `ATTERM_RATE_LIMIT_PER_MINUTE` | 每个 IP 的请求与 WS upgrade 分钟限额 |
| `ATTERM_MAX_CONNECTIONS_PER_KEY` | 每个 IP 的活跃 WebSocket 连接上限 |

公网示例：

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
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
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
ATTERM_ORIGINS='https://relay.example.com' \
go run ./cmd/atterm-relay --addr :8080 --web web
```

本地开发可以临时跳过强度与 Origin 校验（loopback 时 bootstrap envs 可省略，relay 不会自动创建 admin）：

```bash
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web --dev-insecure
```

公网监听默认拒绝缺失 `ATTERM_BOOTSTRAP_ADMIN_EMAIL`、弱 bootstrap 密码、缺失 Origin 白名单。只有明确传 `--dev-insecure` 才会放开这些限制；不要在公网生产环境使用。

### Bootstrap admin

启动时 relay 读取 `ATTERM_BOOTSTRAP_ADMIN_EMAIL` 和 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD`，分三种情况：

- **两个 env 都未设置**：relay 正常启动，但不会自动创建 admin。admin 接口在没有任何 admin 用户之前会返回 401，需要手动把某个用户提升为 admin（例如 `UPDATE users SET is_admin=1 WHERE email='you@…';`）。
- **email 已设置且用户已存在**：把该用户标记为 `is_admin=1`，忽略密码 env。日志会打一条 WARN，提示你登录后从环境中删除密码 env。
- **email 已设置且用户不存在**：使用提供的密码创建新用户并标记为 admin。日志会打一条 WARN，提示登录后从环境中删除密码 env。

**安全提醒**：bootstrap 用户创建/提权后，请把 `ATTERM_BOOTSTRAP_ADMIN_PASSWORD` 从 env 文件 / systemd unit 删除并重启 relay。明文密码长期留在进程环境里（`/proc/self/environ`、备份、同宿主其他服务）都可能被读出来。

公网监听场景下 `ATTERM_BOOTSTRAP_ADMIN_EMAIL` 是必需的（缺失时 relay 拒绝启动，除非 `--dev-insecure`）。

## 安全模型

AT Term 的默认策略是 fail-closed：

- 公网 relay 必须提供 `ATTERM_BOOTSTRAP_ADMIN_EMAIL`；首次启动若要自动创建该 admin 用户，`ATTERM_BOOTSTRAP_ADMIN_PASSWORD` 须满足 ≥16 字符、≥3 类字符、不在弱密码黑名单内，否则启动拒绝。Bootstrap 完成后应从环境中删除密码 env。
- 公网 relay 必须使用明确的 `ATTERM_ORIGINS`。
- 服务端鉴权不接受 `?token=` 参数；API token 通过 `Authorization: Bearer` 或桌面端 WebSocket 的 `Sec-WebSocket-Protocol` 传递，避免写进 URL。
- 用户 API token（`atk_…`）仅在创建时明文展示一次，此后 relay 只保存哈希。
- web 客户端只加载同源静态资源，不依赖 CDN。
- relay 默认启用 CSP、Referrer-Policy、nosniff、Permissions-Policy 等安全头。
- relay 按远端 IP 和认证后的 token hash 做限流与连接数限制。
- 桌面端默认拒绝非 loopback 的明文 `ws://` relay；可信内网需要在 Settings 手动开启 insecure mode。
- 自动更新必须通过 Ed25519 签名和 SHA256 校验；缺公钥、缺签名、签名不匹配或 hash 不匹配都会失败。

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
node --test web/*.test.mjs

# 桌面前端
cd desktop/frontend
npm ci
npm run build
npm run dev

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
cmd/          atterm-relay 和 atterm-agent 入口
internal/     proto、session、relay、ptyhost、agent、hostid、ringbuf 等复用包
desktop/      Wails 桌面 app：Go 后端 + Vue 3 + xterm.js 前端
web/          vanilla 浏览器/PWA 客户端
docs/spec/    架构、协议、工程约定
.github/      CI、release、打包脚本
```

更多模块职责见 [`AGENTS.md`](AGENTS.md)。

## 文档

- [`docs/spec/architecture.md`](docs/spec/architecture.md)：架构、数据流、生命周期、路线图。
- [`docs/spec/protocol.md`](docs/spec/protocol.md)：二进制 WebSocket 帧协议、帧类型、重连语义。
- [`docs/shell-integration.md`](docs/shell-integration.md)：OSC 133 shell 集成机制、各 shell 的注入方式、如何手动卸载。
- [`docs/web-push.md`](docs/web-push.md)：浏览器 / PWA 订阅 Web Push 命令完成通知的启用方式、iOS 限制、自托管关键管理。
- [`docs/spec/conventions.md`](docs/spec/conventions.md)：Go / TypeScript 风格、测试组织、提交约定。
- [`AGENTS.md`](AGENTS.md)：给开发代理和贡献者看的项目红线与修改指引。

## 贡献前检查

提交前建议至少跑：

```bash
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 ./...
node --test web/*.test.mjs
cd desktop/frontend && npm run build
```

如果改了协议或权限模型，请同步更新 [`docs/spec/protocol.md`](docs/spec/protocol.md) 和相关客户端实现。

## 许可证

暂未指定 license，默认 All Rights Reserved。
