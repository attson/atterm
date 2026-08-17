# 介绍与快速上手

AT Term 是一个带远程接管能力的跨平台终端。你在桌面端启动的 shell、Codex、Claude 等长任务,可以在离开电脑后从手机、浏览器或另一台电脑继续查看和输入。Relay 端启用 E2EE 后,命令输出 / 标题 / cwd / 任务摘要在传输和持久化路径上都对 relay 不可读,只在拥有 `account_key` 的客户端解开。

> 一句话:本地先好用;需要远程时,再把会话安全地同步到自己的 relay。

## 工作原理(5 行速览)

1. 桌面端跑本地 PTY,每个 tab 是一个独立会话;OSC 133 推导任务状态(running / waiting / done / failed)。
2. 登录 relay 后,桌面端按需把会话状态 + PTY 字节上行;远程端(手机 / 浏览器 / 另一台桌面)attach 后才上传字节,无人看时不上传。
3. 启用 E2EE:account_key 只在客户端持有;输出 / 标题 / cwd / 摘要在 relay 侧全程密文。
4. 任务关键节点(命令完成、AI 等输入)触发系统通知 / Web Push / 飞书卡片 / 出站 webhook,payload 带 session id 与摘要。
5. AI agent(Claude Code / Codex / Aider / Gemini)自动识别:命令分类、resume 注入、Notification hook 自动安装。

## 适合谁

- 经常跑长时间 AI / 构建 / 运维任务,希望离开工位后还能接着看的用户。
- 想用一个轻量桌面终端,同时保留浏览器远程 attach 能力的开发者。
- 偏好自托管 + 端到端加密,不想把终端输入输出交给第三方平台的人。

## 方式 A:只用桌面端

1. 到 [Releases](https://github.com/attson/atterm/releases) 下载对应平台的包。
2. 解压或安装后启动 `AT Term`。
3. 像普通终端一样新建 tab、分屏、运行命令。

| 平台 | 推荐文件 | 备注 |
|------|----------|------|
| macOS Apple Silicon | `AT-Term_*_arm64.dmg` 或 `AT-Term-darwin-arm64.zip` | Intel 机器选 `amd64` |
| Linux x64 | `AT-Term_*_amd64.deb` 或 `AT-Term-linux-amd64.tar.gz` | 需要 `libwebkit2gtk-4.1-0` |
| Linux arm64 | `AT-Term_*_arm64.deb` 或 `AT-Term-linux-arm64.tar.gz` | 适合 ARM Linux 桌面 |
| Windows x64 | `AT-Term_*_amd64.exe` 或 `AT-Term-windows-amd64.zip` | Windows 11 自带 WebView2;Windows 10 可能需另装 |

> **macOS 用户注意**:目前的 macOS 构件**未签名 / 未公证**(签名工作见 roadmap 第 8 项,阻塞于 Apple Developer 证书),下载 dmg 直接双击会被 Gatekeeper 拦截,提示"无法打开,因为无法验证开发者"。解决办法见 [FAQ](/guide/faq)。
>
> Homebrew cask 分发(`brew install --cask attson/tap/atterm`)能绕开这一步,但承载 cask 的 tap 仓库还没建好,这条命令**目前还跑不通**——建好后会在这里换成可直接执行的命令。

## 方式 B:桌面端 + 自托管 relay(用户账号制)

这个模式适合「电脑上跑任务,手机/另一台机器接管」。

1. 启动 relay(只需 bootstrap 管理员邮箱 + Origin 白名单,**不需要密码 env**——密码由你后面在浏览器注册时本地设置,走 OPAQUE):

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_ORIGINS='https://relay.example.com' \
docker compose up -d atterm-relay
```

2. 看启动日志里打印的一次性 **claim token**(`docker compose logs atterm-relay`,形如 `bootstrap-admin: claim token for you@example.com …`)。该 token 7 天有效、只显示一次。
3. 在浏览器打开 `https://relay.example.com/signup.html`,用 bootstrap 邮箱 + 自设密码注册,并把上一步的 **claim token 填进「邀请码 / claim token」输入框**。注册完成即获得 admin 角色。
4. 用 bootstrap 邮箱 + 刚设的密码在 `https://relay.example.com/login.html` 登录;顶部导航出现 **Admin** 入口。
5. 在桌面端 Settings → Remote relay 填入 relay URL,用注册时的邮箱+密码登录;登录成功后 relay 会下发一份 session token(`ses_…`)并由桌面端持久化。
6. 桌面端连上 relay 后,会显示 `connected as <email>`。手机或另一台电脑用同一账号登录 `https://relay.example.com` 即可看到 session 列表。

完整部署说明见 [部署 Relay](/guide/deploy-relay)。

## 方式 C:源码启动调试

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH

# 终端 1:启动 relay(--dev-insecure 跳过 Origin 校验与 admin 引导)
# --web 省略时使用 internal/relay/web-dist/ 的内嵌 web 构建产物。
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure

# 终端 2(可选):调试 web 前端。Vite dev server 会把 /api、/client 等代理到 127.0.0.1:8080。
cd web
npm ci
npm run dev

# 终端 3:启动桌面端
cd desktop
wails dev -tags webkit2_41   # Linux 需要 tag;macOS/Windows 可省略
```

浏览器访问 `http://127.0.0.1:8080/signup.html`(或 Vite 的 `http://127.0.0.1:5173/signup.html`),用邀请码注册账号;之后在桌面端 Settings → Remote relay 填入 relay URL 并用邮箱+密码登录。

## 开发环境依赖

| 工具 | 版本 | 用途 |
|------|------|------|
| Go | 1.23+ | 后端、relay、PTY host |
| Node.js | 20+ | 桌面前端构建 |
| Wails CLI | v2.12.0 | 桌面应用开发与打包 |
| Xcode | 15+ | iOS WebView MVP 构建 |

Linux(Ubuntu 24.04 / Debian 12)额外依赖:

```bash
sudo apt update
sudo apt install -y build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
```

更多命令与仓库结构见项目 [README](https://github.com/attson/atterm) 与 [AGENTS.md](https://github.com/attson/atterm/blob/main/AGENTS.md)。
