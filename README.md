# atterm

跨平台终端模拟器 + 内建会话云同步。所有从 atterm 启动的会话默认可在任意设备的浏览器/客户端 attach、查看历史、继续输入。核心场景：在本机跑 codex/claude 之类的长 AI 任务，离开工位后用手机或另一台机器继续看进度、继续打字。

> **当前阶段：Phase 0 — 云同步链路骨架。** 命令行 wrapper agent + Go relay + 极简 web 客户端。桌面端（Wails）、用户系统、移动端等留给后续 phase。

## 组件

- **`cmd/atterm-relay`** — 中心 relay 服务，HTTP/WS，内存里维护会话与 scrollback。
- **`cmd/atterm-agent`** — 命令行 PTY wrapper（Phase 0 调试用），把任意命令的 PTY 接入 relay。
- **`web/`** — 浏览器客户端，xterm.js 渲染，fetch 会话列表、WS 连 relay 接管会话。

## 运行

启动 relay：

```bash
ATTERM_TOKEN=dev go run ./cmd/atterm-relay --addr :8080 --web web
```

或：

```bash
ATTERM_TOKEN=dev ./scripts/dev.sh
```

启动 agent（在另一终端）：

```bash
# 包装 bash
ATTERM_TOKEN=dev go run ./cmd/atterm-agent -- bash

# 或包装其他命令
ATTERM_TOKEN=dev go run ./cmd/atterm-agent --relay ws://localhost:8080 -- htop
```

打开浏览器：

```
http://localhost:8080/?token=dev
```

应当看到一条会话条目，点击进入即可看到滚动历史并继续输入。

## 协议

二进制 WebSocket，单消息单帧。每帧 6 字节 header + 16 字节 session id + payload。详见 `internal/proto/`。

帧类型：

| 类型 | 方向 | 说明 |
|---|---|---|
| `OPEN 0x01` | agent → relay | JSON：cols/rows/command/cwd/title |
| `IN 0x02` | client → agent | 用户键入字节 |
| `OUT 0x03` | agent → client | 8B seq u64 BE + PTY 字节 |
| `RESIZE 0x04` | client → agent | cols u16 \| rows u16 |
| `META 0x05` | agent → client | JSON：cwd/title |
| `CLOSE 0x06` | agent → client | JSON：exit_code |
| `ATTACH 0x10` | client → relay | JSON：session_id, since_seq? |
| `LIST 0x11` / `LIST_RESP 0x12` | client ↔ relay | 会话列表 |
| `PING/PONG 0x20/0x21` | 双向 | 心跳 |

## 鉴权

环境变量 `ATTERM_TOKEN` 设置共享 bearer token；agent 走 `Authorization` header，浏览器走 `?token=` query。空 token 表示无鉴权（仅 dev）。

## 当前限制（Phase 0 范围）

- 单租户，所有客户端看同一组会话
- scrollback 仅活动会话期间保留（4 MiB 上限/会话），PTY 退出即丢
- 无 TLS（自托管时套 caddy/nginx）
- Web 客户端是 vanilla HTML/CSS/JS，无构建链
- 无桌面端（Phase 1 才上 Wails）

## 路线图

- **Phase 0** ✓ 协议骨架，云链路验证
- **Phase 1** Wails 桌面端 MVP，本地终端 + 默认上云
- **Phase 2** Web 端 UI 升级 + 用户体系
- **Phase 3** 桌面端多 tab/split/主题/快捷键
- **Phase 4** 用户系统、TLS 自动化、安装包签名、PWA 移动端
