# AT Term

> Local-first cross-platform terminal for SSH and AI coding agents, with E2EE remote session takeover.

```
   █████╗ ████████╗    ████████╗███████╗██████╗ ███╗   ███╗
  ██╔══██╗╚══██╔══╝    ╚══██╔══╝██╔════╝██╔══██╗████╗ ████║
  ███████║   ██║          ██║   █████╗  ██████╔╝██╔████╔██║
  ██╔══██║   ██║          ██║   ██╔══╝  ██╔══██╗██║╚██╔╝██║
  ██║  ██║   ██║          ██║   ███████╗██║  ██║██║ ╚═╝ ██║   █
  ╚═╝  ╚═╝   ╚═╝          ╚═╝   ╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝
```

**本地优先的跨平台终端、SSH 工作台与 AI Agent 控制台** · Go + Vue · Apache-2.0

AT Term 是本地优先的桌面终端。Shell、SSH、Codex、Claude 等任务始终在自己的
电脑上运行；需要离开工位时，可以从浏览器、iPhone 或另一台桌面客户端查看历史、
继续输入。远程 Relay 按观看者惰性上传终端流，启用 E2EE 后不能读取终端内容。

[在线体验](https://attson.github.io/atterm/) ·
[下载](https://github.com/attson/atterm/releases/latest) ·
[用户文档](https://attson.github.io/atterm/guide/) ·
[问题反馈](https://github.com/attson/atterm/issues)

## 能做什么

- **本地终端**：多标签、分屏、主题、滚动历史、链接识别、图片粘贴与恢复上次工作区。
- **SSH 工作台**：主机与密钥管理、输入历史、端口转发、ProxyJump、SFTP 文件浏览，
  以及从 OpenSSH config 或 Termius 导入。
- **远程接管**：桌面、Web/PWA 和 Capacitor iOS 共用同一套终端界面；支持
  driver/viewer、远程文件、用户输入历史、Web Preview 和后台恢复续传。
- **AI 任务控制台**：识别 Codex/Claude 等会话的运行、等待输入和完成状态，支持
  会话恢复、Quick/Snippet 命令、桌面挂件、通知和飞书远程交互。
- **两种远程路径**：账户 Relay 适合稳定的多设备访问；Peer Space/Rendezvous 提供
  无账户发现与直连。两者都不会改变“本地不可依赖远端”的基本边界。
- **安全默认值**：OPAQUE 登录、端到端内容密钥、Ed25519 更新验签、严格 Origin 与
  HTTPS 策略；Web 资源同源打包，不依赖 CDN。

## 快速开始

只需要本机终端时，直接从
[Releases](https://github.com/attson/atterm/releases/latest) 安装桌面应用即可，不需要
部署 Relay。远程接管的完整配置见[远程接管](https://attson.github.io/atterm/guide/remote-takeover)
和[部署 Relay](https://attson.github.io/atterm/guide/deploy-relay)。

源码开发需要 Go 1.23+、Node 20 和 Wails v2.12：

```bash
git config core.hooksPath .githooks

# 终端 1：本地开发 Relay；仅限 loopback 调试
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure

# 终端 2：桌面应用
cd desktop
wails dev -tags webkit2_41   # Linux
wails dev                    # macOS / Windows
```

Linux 还需要 `libgtk-3-dev libwebkit2gtk-4.1-dev libxext-dev`。Web 前端热更新使用
`cd web && npm run dev`，Vite 会把 API 和 WebSocket 代理到 `127.0.0.1:8080`。

## 仓库结构

| 路径 | 职责 |
| --- | --- |
| `desktop/` | Wails 桌面宿主、本地 PTY、SSH、uplink、更新器与插件桥接 |
| `desktop/frontend/` | 桌面、Web 主界面和 Capacitor 共用的 Vue/xterm 前端 |
| `internal/` | 协议、session、relay、E2EE、配置同步、Peer/Rendezvous 等共享 Go 包 |
| `cmd/` | Relay、Rendezvous、OPAQUE WASM 和辅助二进制入口 |
| `web/` | 浏览器登录/注册外壳、PWA 资源与共享 Web API；主界面复用桌面前端 |
| `mobile/` | Capacitor iOS 壳与原生插件，Web 资源由共享前端构建生成 |
| `site/` | VitePress 用户文档和交互式产品演示 |
| `docs/spec/` | 当前架构、协议、安全、样式和子系统的权威工程规范 |

贡献者和编码 agent 从 [AGENTS.md](./AGENTS.md) 开始。文档分层规则见
[docs/README.md](./docs/README.md)，架构入口见
[docs/spec/architecture.md](./docs/spec/architecture.md)。

## 验证

```bash
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./desktop/

cd desktop/frontend && npm test && npm run build
cd ../../web && npm run build && npm test && npm run test:contract
cd ../mobile && npm test
```

只改文档时至少运行 `git diff --check`，并检查新增的仓库内链接。

## 发布

发布统一走预检脚本，不手工创建和推送版本 tag：

```bash
./scripts/ship-release.sh             # 当前 HEAD 可达的最高 SemVer line 递增 patch
./scripts/ship-release.sh --line v0.5 # 仅在明确维护旧 line 时指定
```

脚本会检查 `main`、远端同步、干净工作区和内嵌 Web 产物，再创建并推送 tag。
`web/` 或共享前端影响 Web bundle 时，先运行 `./scripts/build-web.sh` 并提交生成结果。
