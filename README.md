# atterm

跨平台终端模拟器 + 内建会话云同步。所有从 atterm 启动的会话都可在任意设备 attach、查看历史、继续输入。核心场景：本机跑 codex/claude 类的长 AI 任务，离开工位后用手机或另一台机器接管。

**当前能力**

- ✅ Wails 桌面 app（Linux / macOS / Windows）：多 tab、每 tab 1/2/4 pane 分屏（⌘D / ⌘⇧D，Linux/Windows 用 Ctrl）、本地 PTY、cwd 跟踪、远程 cast 面板
- ✅ 中央 relay：本地或公网部署，统一 attach 入口
- ✅ Lazy 远程同步：远程 relay 静默时不传字节，按需上传，本地体验不受其可达性影响
- ⬜ 用户系统、TLS 自动化、移动端、shell 集成（路线图见 [`doc/spec/architecture.md`](doc/spec/architecture.md)）

## 三种使用方式

### 1. 桌面 app（多数用户用这个）

下载预编译产物：[Releases](https://github.com/attson/atterm/releases)。

| 平台 | 文件 |
|------|------|
| Linux x64 | `atterm-desktop-linux-amd64.tar.gz` |
| macOS Apple Silicon | `atterm-desktop-darwin-arm64.zip` |
| Windows x64 | `atterm-desktop-windows-amd64.zip` |

解压运行即可。Linux 需要系统已装 `libwebkit2gtk-4.1-0`；Windows 需要 WebView2 Runtime（Win11 自带）。

### 2. 自己跑 relay + 浏览器

不想装桌面端，只想在浏览器里看：

```bash
ATTERM_TOKEN=dev go run ./cmd/atterm-relay --addr :8080 --web web
ATTERM_TOKEN=dev go run ./cmd/atterm-agent --relay ws://localhost:8080 -- bash
# 浏览器: http://localhost:8080/?token=dev
```

### 3. 桌面 + 远程 relay 多设备同步

桌面 app 启动后点齿轮，填远程 relay URL/token。然后另一设备的浏览器或桌面 app 连同一 relay 即可互相 attach。详见 [`doc/spec/architecture.md`](doc/spec/architecture.md) §三种核心数据流。

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
export PATH="$(go env GOPATH)/bin:$PATH"
wails version  # 应当看到 v2.12.0
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
wails build -tags webkit2_41           # 产出 desktop/build/bin/atterm-desktop
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
wails build               # 产出 desktop/build/bin/atterm-desktop.app
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
wails build               # 产出 desktop\build\bin\atterm-desktop.exe
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
ATTERM_TOKEN=dev go run ./cmd/atterm-relay --addr :8080 --web web

# 终端 2：启 app A，配 relay
cd desktop && wails dev -tags webkit2_41
# 在 app 里点齿轮 → URL=ws://localhost:8080，token=dev

# 终端 3：启 app B，同样配置
cd desktop && wails dev -tags webkit2_41
```

两个 app 的 cast 面板（📡）里能互看对方的 session。详见 [`doc/spec/architecture.md`](doc/spec/architecture.md) §流 3。

## 仓库结构（简表）

```
cmd/         二进制入口（atterm-relay / atterm-agent）
internal/    可复用业务包（proto / session / relay / ptyhost / agent / hostid / ringbuf）
desktop/     Wails 桌面端（Go 后端 + Vue3 + xterm.js 前端）
web/         浏览器客户端（vanilla HTML/JS）
doc/spec/    协议、架构、约定规范
.github/     CI workflow
```

完整结构 + 各模块职责见 [`AGENTS.md`](AGENTS.md) §仓库布局。

## 文档

- [`AGENTS.md`](AGENTS.md) — 项目导览、设计红线、何时改哪里
- [`doc/spec/architecture.md`](doc/spec/architecture.md) — 整体架构、三条核心数据流、组件矩阵、phase 路线
- [`doc/spec/protocol.md`](doc/spec/protocol.md) — wire 协议规范（帧格式、12 个帧类型 schema、重连续传）
- [`doc/spec/conventions.md`](doc/spec/conventions.md) — Go/TS 代码约定、commit 风格、扩展 recipe

## 贡献

- 风格：见 [`doc/spec/conventions.md`](doc/spec/conventions.md)
- 提交前本地跑：`go vet -tags webkit2_41 ./...` + `go test -tags webkit2_41 ./desktop/` + `cd desktop/frontend && npm run build`
- CI 在 `.github/workflows/build.yml`：push main / PR / tag v* / manual 都会触发，三平台并行构建
- 协议变更：必须同步更新 [`doc/spec/protocol.md`](doc/spec/protocol.md) 与 TS 端 `desktop/frontend/src/lib/proto.ts`

## 许可证

未指定（默认 All Rights Reserved）。后续会加 license。
