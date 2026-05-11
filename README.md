# atterm

跨平台终端模拟器 + 内建会话云同步。所有从 atterm 启动的会话都可在任意设备 attach、查看历史、继续输入。核心场景：本机跑 codex/claude 类的长 AI 任务，离开工位后用手机或另一台机器接管。

**当前能力**

- ✅ Wails 桌面 app（Linux / macOS / Windows）：多 tab、每 tab 1/2/4 pane 分屏（⌘N / ⌘⇧N，Linux/Windows 用 Ctrl）、本地 PTY、cwd 跟踪、远程 cast 面板
- ✅ 内置自动更新：Settings → Updates 检查/下载/用户主动重启升级（GitHub Releases，dev 构建禁用）
- ✅ 自动更新验签：release asset 通过 `SHA256SUMS` + Ed25519 签名校验后才允许安装
- ✅ 中央 relay：本地或公网部署，统一 attach 入口
- ✅ Lazy 远程同步：远程 relay 静默时不传字节，按需上传，本地体验不受其可达性影响
- ⬜ 用户系统、TLS 自动化、移动端、shell 集成（路线图见 [`docs/spec/architecture.md`](docs/spec/architecture.md)）

## 三种使用方式

### 1. 桌面 app（多数用户用这个）

下载预编译产物：[Releases](https://github.com/attson/atterm/releases)。

| 平台 | 文件 |
|------|------|
| Linux x64 | `AT-Term-linux-amd64.tar.gz` |
| macOS Apple Silicon | `AT-Term-darwin-arm64.zip` |
| Windows x64 | `AT-Term-windows-amd64.zip` |

解压运行即可。Linux 需要系统已装 `libwebkit2gtk-4.1-0`；Windows 需要 WebView2 Runtime（Win11 自带）。

### 2. 自己跑 relay + 浏览器

不想装桌面端，只想在浏览器里看：

```bash
ATTERM_TOKEN=dev go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web
ATTERM_TOKEN=dev go run ./cmd/atterm-agent --relay ws://localhost:8080 -- bash
# 浏览器: http://localhost:8080/?token=dev
```

手机接管：iPhone Safari 可以直接打开同一个 relay URL，例如
`https://relay.example.com/?token=...`。页面支持添加到主屏幕作为 PWA；手机端会显示触控优化的 session 列表、终端视图和 `Esc` / `Tab` / `Ctrl-C` / 方向键等快捷键。真实部署建议使用 HTTPS/WSS。

安全默认值：

- `atterm-relay` 未设置 `ATTERM_TOKEN` 时会自动生成高强度 token，并在启动日志打印访问 URL。
- 公网监听时拒绝弱 token（例如 `dev` 或长度 <16），除非显式传 `--dev-insecure`。
- relay 默认返回 CSP / Referrer-Policy / nosniff / Permissions-Policy 等安全头，并按远端 IP/token 限制请求速率与活跃 WebSocket 连接数。
- 浏览器客户端不再从 CDN 加载 xterm；所有 web 静态资源都走同源 `web/vendor/`，service worker 会缓存这些资源。
- URL 里的 `?token=...` 会在读取并保存到浏览器本地存储后从地址栏移除，降低分享/截图/历史记录泄漏风险。
- 可用 `ATTERM_READ_ONLY_TOKENS` 或 `--read-only-tokens` 配只读 token；只读用户可以列出/attach/看输出，但不能向 PTY 输入、resize 或粘贴图片。
- 桌面端 Settings 可设置本机 session 的远程权限：`view` / `control` / `full`。该权限由 owner 发布，relay 与 desktop host 双重强制执行；read-only token 会继续把有效权限压成只读。
- 可用 `ATTERM_RELAY_CONFIG`/`--config` + `ATTERM_ADMIN_TOKEN`/`--admin-token` 启用持久化 relay admin 配置和 `/admin/` 管理页。持久化配置不保存主 write token，只保存运行参数和 hash 后的只读 token。
- 桌面端默认拒绝非本机 `ws://` 明文 relay；如果确实在可信内网使用 `ws://`，需要在 Settings 勾选 insecure mode，并接受 token/输入/输出明文传输风险。

也可以直接用 Docker Compose 启动 relay：

```bash
docker compose up -d atterm-relay
docker compose logs atterm-relay   # 查看自动生成的 token
# 浏览器: http://localhost:8080/?token=<日志里的 token>
```

可选环境变量：

- `ATTERM_RELAY_IMAGE`：relay 镜像，默认 `attson/atterm-relay:latest`
- `ATTERM_RELAY_PORT`：宿主机端口，默认 `8080`
- `ATTERM_READ_ONLY_TOKENS`：逗号分隔的只读 token，适合临时分享“只能看不能输入”的会话入口
- `ATTERM_RATE_LIMIT_PER_MINUTE`：每个远端 IP/token 的请求/upgrade 分钟限额；`0` 用内置默认值，负数禁用
- `ATTERM_MAX_CONNECTIONS_PER_KEY`：每个远端 IP/token 的活跃 WS 连接上限；`0` 用内置默认值，负数禁用
- `ATTERM_RELAY_CONFIG`：持久化 admin 配置 JSON 路径，例如 `/etc/atterm/relay.json`
- `ATTERM_ADMIN_TOKEN`：启用 `/admin/` 管理页和 `/admin/api/*`；admin API 只接受 Authorization header
- `ATTERM_RELAY_DEBUG=1`：打印 relay 交互日志
- `ATTERM_RELAY_DEBUG_PAYLOAD=1`：额外打印 IN/OUT 字节内容（仅调试用）
- `ATTERM_WATCHTOWER_INTERVAL`：启用自动更新时的检查间隔秒数，默认 `300`
- `ATTERM_WATCHTOWER_DOCKER_API_VERSION`：watchtower 连接本机 Docker daemon
  使用的 API 版本，默认 `1.40`

如果希望 DockerHub `latest` 更新后自动拉取并重启 relay，可以启用可选的
`auto-update` profile：

```bash
docker compose --profile auto-update up -d
```

自动更新由 `watchtower` 容器完成，只会更新带有 watchtower label 的
`atterm-relay` 服务。该模式需要挂载 Docker socket；不需要自动更新时继续使用
普通 `docker compose up -d atterm-relay` 即可。

桌面端自动更新要求 GitHub Release 里同时存在 `SHA256SUMS` 和
`SHA256SUMS.sig`。CI 的 `build` workflow 会在 tag release 时用 prod
environment secret `ATTERM_UPDATE_VERIFY_PUBLIC_KEY` 注入桌面 app，并用
prod environment secret `ATTERM_UPDATE_SIGNING_PRIVATE_KEY` 生成
`SHA256SUMS` / `SHA256SUMS.sig` 后上传到 release。两个值均为 base64；
public key 是 32 字节 Ed25519 公钥，private key 是 64 字节 Ed25519 私钥。
私钥只放 GitHub environment secret，不要提交到仓库。

手动构建 release 版时也必须注入公钥：

```bash
cd desktop
VERSION=v0.1.14
wails build -ldflags "-X main.Version=$VERSION -X main.UpdateVerifyPublicKey=$ATTERM_UPDATE_VERIFY_PUBLIC_KEY"
```

如果修改了自动更新配置，建议重建 watchtower 容器：

```bash
docker compose --profile auto-update up -d --force-recreate watchtower
```

本地构建镜像用于调试：

```bash
docker build -f Dockerfile.relay -t atterm-relay:local .
ATTERM_RELAY_IMAGE=atterm-relay:local docker compose up -d atterm-relay
```

`main` 分支推送后 GitHub Actions 会构建并推送 `attson/atterm-relay:latest`。
仓库需要配置 Docker Hub secrets：`DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN`。

### 3. 桌面 + 远程 relay 多设备同步

桌面 app 启动后点齿轮，填远程 relay URL/token。然后另一设备的浏览器或桌面 app 连同一 relay 即可互相 attach。详见 [`docs/spec/architecture.md`](docs/spec/architecture.md) §三种核心数据流。

---

## 开发环境搭建

### 通用前置

| 工具 | 版本 | 用途 |
|------|------|------|
| Go | **1.23+** | 后端 |
| Node | **20+** | 前端构建 |
| Wails CLI | **v2.12.0** | 桌面端构建 |

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
# 加到 PATH
export PATH="/opt/homebrew/bin:$(go env GOPATH)/bin:$HOME/sdk/go1.23.12/bin:$PATH"
wails version  # 应当看到 v2.12.0
```

macOS 上 Homebrew 安装的 `gh` 通常在 `/opt/homebrew/bin/gh`。如果非交互
shell 找不到 `gh`，优先检查 PATH，或直接使用完整路径：

```bash
/opt/homebrew/bin/gh run list --repo attson/atterm --limit 10
```

国内拉模块慢可设置：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

### Linux（Ubuntu 24.04 / Debian 12）

```bash
sudo apt update
sudo apt install -y \
    build-essential pkg-config \
    libgtk-3-dev libwebkit2gtk-4.1-dev
```

Ubuntu 22.04 没有 `libwebkit2gtk-4.1-dev`，需要：

```bash
sudo add-apt-repository ppa:webkit-team/ppa  # 或自行编译 4.1
sudo apt install -y libwebkit2gtk-4.1-dev
```

校验环境：

```bash
wails doctor
# 关键行：libwebkit ... Installed
```

构建 / 开发：

```bash
cd desktop
wails dev   -tags webkit2_41           # 开发模式（Vite HMR）
wails build -tags webkit2_41           # 产出 desktop/build/bin/AT Term
```

**Linux 必须加 `-tags webkit2_41`**——Wails CLI 默认匹配旧的 4.0 SDK，新发行版只有 4.1。

### macOS（Apple Silicon / Intel）

```bash
xcode-select --install   # Xcode Command Line Tools
brew install go node     # 也可用 asdf / mise 管理版本
```

校验：

```bash
wails doctor
```

构建：

```bash
cd desktop
wails dev                 # 开发模式
wails build               # 产出 desktop/build/bin/AT Term.app
```

要打 universal 二进制（Intel + Apple Silicon 同 bundle）：

```bash
wails build -platform darwin/universal
```

### Windows（10 / 11，x64）

需要：

- Go：[官方安装包](https://go.dev/dl/) 或 `winget install GoLang.Go`
- Node：[nodejs.org](https://nodejs.org/) 或 `winget install OpenJS.NodeJS.LTS`
- WebView2 Runtime：Win11 自带；Win10 装 [Evergreen Bootstrapper](https://developer.microsoft.com/en-us/microsoft-edge/webview2/)
- C 编译器（cgo 用）：`winget install MartinStorsjo.LLVM-MinGW.UCRT` 或装 [TDM-GCC](https://jmeubank.github.io/tdm-gcc/)

PowerShell 里：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
$env:Path = "$(go env GOPATH)\bin;$env:Path"
wails version
wails doctor
```

构建：

```powershell
cd desktop
wails dev
wails build               # 产出 desktop\build\bin\AT Term.exe
```

带 NSIS 安装包：

```powershell
wails build -nsis
```

需要先装 NSIS：`winget install NSIS.NSIS`。

---

## 开发常用命令

```bash
# 在仓库根
go vet -tags webkit2_41 ./...                    # 静态检查（Linux 加 tag，其他平台可省）
go test -tags webkit2_41 -timeout 60s ./desktop/ # 跑 lazy uplink e2e 协议测试
go build ./...                                   # 编译所有非 desktop 包

# 在 desktop/frontend
npm ci                                           # 装前端依赖
npm run build                                    # vue-tsc + vite build → dist/
npm run dev                                      # Vite dev server（不通过 wails）

# 在 desktop/
wails dev   -tags webkit2_41                     # 推荐开发入口（Linux）
wails dev                                        # macOS / Windows
wails build -tags webkit2_41                     # 出可执行（Linux）
wails build                                      # 出可执行（macOS / Windows）
```

### 调试两台 atterm 互相接管

```bash
# 终端 1：启远程 relay
ATTERM_TOKEN=dev go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web

# 终端 2：启 app A，配 relay
cd desktop && wails dev -tags webkit2_41
# 在 app 里点齿轮 → URL=ws://localhost:8080，token=dev

# 终端 3：启 app B，同样配置
cd desktop && wails dev -tags webkit2_41
```

两个 app 的 cast 面板（📡）里能互看对方的 session。详见 [`docs/spec/architecture.md`](docs/spec/architecture.md) §流 3。

## 仓库结构（简表）

```
cmd/         二进制入口（atterm-relay / atterm-agent）
internal/    可复用业务包（proto / session / relay / ptyhost / agent / hostid / ringbuf）
desktop/     Wails 桌面端（Go 后端 + Vue3 + xterm.js 前端）
web/         浏览器客户端（vanilla HTML/JS）
docs/spec/    协议、架构、约定规范
.github/     CI workflow
```

完整结构 + 各模块职责见 [`AGENTS.md`](AGENTS.md) §仓库布局。

## 文档

- [`AGENTS.md`](AGENTS.md) — 项目导览、设计红线、何时改哪里
- [`docs/spec/architecture.md`](docs/spec/architecture.md) — 整体架构、三条核心数据流、组件矩阵、phase 路线
- [`docs/spec/protocol.md`](docs/spec/protocol.md) — wire 协议规范（帧格式、12 个帧类型 schema、重连续传）
- [`docs/spec/conventions.md`](docs/spec/conventions.md) — Go/TS 代码约定、commit 风格、扩展 recipe

## 贡献

- 风格：见 [`docs/spec/conventions.md`](docs/spec/conventions.md)
- 提交前本地跑：`go vet -tags webkit2_41 ./...` + `go test -tags webkit2_41 ./...` + `node --test web/*.test.mjs` + `cd desktop/frontend && npm run build`
- CI 在 `.github/workflows/build.yml`：push main / PR / tag v* / manual 都会触发，三平台并行构建
- 协议变更：必须同步更新 [`docs/spec/protocol.md`](docs/spec/protocol.md) 与 TS 端 `desktop/frontend/src/lib/proto.ts`

## 许可证

未指定（默认 All Rights Reserved）。后续会加 license。
