# 架构规范

## 一句话总览

atterm 是 **本地桌面终端**（Wails app）+ **可选中央 relay**（独立 server）+ **任意 web/桌面客户端**。三者通过统一的二进制 WebSocket 帧协议通信。本地体验永远独立、可用；远程能力是叠加的，按需启动。

## 组件全图

```
┌─────────────── 桌面 app（atterm-desktop，单进程）──────────────┐
│                                                                │
│  ┌─ 前端 webview（Vue3 + xterm.js）─┐                          │
│  │  TabBar · TerminalView · 设置 · │                          │
│  │  cast 面板（远程发现）           │                          │
│  └─────────────────┬─────────────────┘                          │
│                    │ WS  ws://127.0.0.1:<rand>                  │
│                    ▼                                            │
│  ┌── 本地 mini relay（internal/relay.Server）─────┐             │
│  │  /agent /uplink /client /api/sessions          │             │
│  │  + AdoptSession(PtyHost) 同进程注册            │             │
│  └────────────────┬───────────────────────────────┘             │
│                   │ in-process channels                         │
│                   ▼                                             │
│  ┌── ptyhost.Host 集合 ─────────────┐                          │
│  │  本地 PTY 子进程（bash/zsh/...）  │                          │
│  └──────────────────────────────────┘                          │
│                                                                 │
│  ┌── desktop/uplink.go（可选）──────┐                          │
│  │  control conn → ws://relay/uplink│                          │
│  │  ANNOUNCE 元数据（30s 心跳 + 事件）│                         │
│  │  STREAM_REQ → SubscribeLocal     │                          │
│  │  STREAM_STOP → UnsubscribeLocal  │                          │
│  └──────────────────────────────────┘                          │
└─────────────────────────────────────────────────────────────────┘
                                │
                                │ WSS
                                ▼
┌──────────────── atterm-relay（独立 Go 服务）──────────────────┐
│                                                                │
│  /agent — 旧式 agent（CLI wrapper）注册 session                │
│  /uplink — 桌面 app 控制连，收 ANNOUNCE 维护 mirror sessions  │
│  /client — web/桌面 客户端 attach（ATTACH/IN/RESIZE/收 OUT）  │
│  /api/sessions — JSON 列表（local + mirror 全量）              │
│                                                                │
│  SessionRegistry：                                             │
│   - local sessions（来自 /agent，字节流持续）                   │
│   - mirror sessions（来自 /uplink，subscribe→上传，0 sub→停）  │
└────────────────────────────────────────────────────────────────┘
                                ▲
                                │ ws/http
                                │
       浏览器、其他桌面 app（attach 后浏览/操作任意会话）
```

## 组件职责矩阵

| 组件 | 路径 | 职责 | 不做 |
|------|------|------|------|
| `proto` | `internal/proto/` | 帧协议常量 + 编解码 | 任何业务逻辑 |
| `ringbuf` | `internal/ringbuf/` | 字节预算环形缓冲 | 不知道帧类型 |
| `session` | `internal/session/` | session 数据模型、订阅 fan-out、lifecycle 钩子 | 不开 WS 不读 PTY |
| `relay` | `internal/relay/` | HTTP/WS 服务，处理 4 种连接（agent/uplink/client/sessions） | 不写 PTY、不持久化 |
| `ptyhost` | `internal/ptyhost/` | 纯 PTY 包装，无本地 TTY 副作用 | 不知道 relay 协议 |
| `agent` | `internal/agent/` | CLI wrapper：`ptyhost` + 本地 stdin/stdout 桥接 + `relay` 客户端 | 不被桌面端用 |
| `hostid` | `internal/hostid/` | 机器持久 UUID | 不知道 session |
| `desktop/relay_host.go` | desktop | 启 mini relay，spawn PTY，AdoptSession | 不连远程 |
| `desktop/uplink.go` | desktop | 远程 relay 客户端（lazy 协议） | 不直接拥有 PTY |
| `desktop/app.go` | desktop | Wails bindings | 不实现协议 |

## 三种核心数据流

### 流 1：本地新建会话

```
前端点 + → app.NewSession(req)
       → relayHost.NewSession(req)
       → ptyhost.Open(bash)
       → relay.Server.AdoptSession(id, info, pty)
              ├─ session.New(id, info) 注册到 registry
              ├─ goroutine A：read PTY → session.PushOut(seq, data)
              └─ goroutine B：select session.Inbound() → 写 PTY
       → 前端 hash → ATTACH WS → 收 scrollback + 实时 OUT
```

### 流 2：用户在浏览器 attach 桌面 app 的会话（lazy 路径）

```
桌面 app 启动时 uplink.Run 已建立控制连接
桌面 NewSession 后 → notifyChange → uplink 发新 ANNOUNCE → relay 建 mirror session

浏览器打开 http://relay/?token=… → ATTACH(sid)
       ↓
relay client_conn → mirror.Subscribe()  ← 触发 lifecycle.onFirstSubscribe
       ↓
uplink_conn 通过控制连发 STREAM_REQUEST(sid)
       ↓
桌面 uplink 收 STREAM_REQUEST → SubscribeLocal(sid) 在本地 mini relay 订阅
       ↓
本地 PTY 输出 → 本地 session.PushOut → fan-out 给 local subscriber + uplink subscriber
                                                              ↓
                                              uplink forwarder → WS → 远程 relay
                                                              ↓
                                              uplink_conn → mirror session.PushOut
                                                              ↓
                                                          fan-out → 浏览器
```

浏览器输入：`IN frame → relay client_conn → mirror.SendInbound → mirror.Inbound() chan → uplink_conn writer → WS → 桌面 uplink → host.SendLocalInbound → 本地 session.SendInbound → 本地 PTY 写入`。

最后一个浏览器 detach 时：`removeSubscriber → lifecycle.onLastUnsubscribe → uplink_conn 发 STREAM_STOP → 桌面 uplink 取消 forwarder`。

### 流 3：本机两个桌面 app 互相 attach

两个 app 都连同一 relay。各自 ANNOUNCE。一方点 cast 面板选另一方的 session：

```
app2 前端 → 拉 remote relay /api/sessions → 看到 app1 的 mirror session
       → 点击卡片 openRemote(sid) → 加 tab → TerminalView 用 remoteEndpoint attach
       → ATTACH 帧到 remote relay → 同流 2 后半段
```

注意：app1 的 host_id 和 app2 相同（同机器），但 session_id 独立，去重按 session_id（详见 protocol.md §去重）。

## 会话生命周期

```
创建：NewSession → ptyhost.Open → AdoptSession → registry.Add → 通知 uplink
活跃：PTY 输出 → session.PushOut → fan-out（含 ringbuf 持久化）
关闭：
  本地原因（用户关 tab）：CloseSession → ptyhost.Close → PTY EOF → AdoptSession
                            读循环收 EOF → broadcast CLOSE → registry.Remove
  PTY 自然退出（exit）：同上
  uplink 断线：远程 mirror session 被 uplink_conn cleanup 移除（远程 attacher 收 CLOSE）
  app 退出：relayHost.Stop → 清所有 PTY + cancel uplink ctx
```

session 保留期：**仅 PTY 进程活动期间**。退出即丢弃 ringbuf。**不持久化到磁盘**（个人自托管定位）。

## 会话标识

每个 session 有三个标识：

| 字段 | 来源 | 用途 |
|------|------|------|
| `session_id` (UUIDv4) | `uuid.New()` 创建时生成 | 唯一身份；attach、去重、路由 |
| `host_id` | `~/.config/atterm/host_id` 或 `ATTERM_HOST_ID` | 机器粒度的归属（人类视角） |
| `host` / `user` | `os.Hostname()` / `user.Current()` | 人类可读展示 |

**关键约束**：
- 多实例同机：`host_id` 相同，`session_id` 独立 → 去重必须按 `session_id`
- session 在迁移/重启间不持久（PTY 退出即销毁）

## phase 完成度（截至当前）

- ✅ Phase 0：协议骨架，命令行 wrapper + relay + vanilla web
- ✅ Phase 1：Wails 桌面壳，多 tab，自动建会话，cwd-driven 标题
- ✅ Phase 1.5：lazy 远程镜像（ANNOUNCE/STREAM_REQUEST/STOP），GUI 设置入口，cast 面板
- ⬜ Phase 2：web 端 UI 升级（按 host 分组、host alias、active 指示）
- ⬜ Phase 3：桌面端多 split、主题、字体、配置 DSL
- ⬜ Phase 4：用户系统、TLS 自动化、安装包签名、PWA 移动端

## 桌面端架构细节

```
desktop/main.go            创建 *App，wails.Run；OnStartup→app.startup，OnShutdown→app.shutdown
desktop/app.go             A 持有 *relayHost、*uplink、*configStore；暴露 7 个 binding
desktop/relay_host.go      启动 relay.NewServer + net.Listen("tcp","127.0.0.1:0")
                           NewSession 起 ptyhost、AdoptSession 到本地 server
                           watchCwd goroutine 每秒 readlink /proc/<pid>/cwd
                           Subscribe/UnsubscribeLocal/SendLocalInbound 给 uplink 用
desktop/uplink.go          连远程 /uplink，发 ANNOUNCE
                           收 STREAM_REQ → SubscribeLocal + 启 forwarder goroutine
                           收 STREAM_STOP → cancel forwarder
                           IN/RESIZE → host.SendLocalInbound
                           所有 conn.Write 通过单一 out channel + writer goroutine 串行化
desktop/config.go          ~/.config/atterm/config.json 持久化，atomic write-temp-rename
```

## 前端架构细节

```
desktop/frontend/src/
├── main.ts                Vue 入口
├── App.vue                双源轮询：local + remote /api/sessions；
                           sessions 是 computed（local + opened remote）
                           openedRemotes Set 控制远程哪些进 tab
├── components/
│   ├── TabBar.vue         tab 编号 + 圆点（绿=本地 / cast 图标=远程）
│   ├── TerminalView.vue   xterm.js + FitAddon；多实例 v-show 切换；
│                          active prop 触发 fit + focus
│   ├── SettingsDialog.vue relay url/token 编辑，SaveAndConnect / Disconnect
│   └── RemoteSessionsDialog.vue cast 面板，可发现的远程会话卡片列表
└── lib/
    ├── proto.ts           帧编解码（移植自 web/app.js）
    ├── connection.ts      SessionConnection：WS attach + 重连 + 续传
    └── api.ts             Wails bindings 包装（不依赖 generated 文件，走 window.go.main.App.*）
```

## 跨进程时序细节

ANNOUNCE 全量快照：每次发都覆盖 relay 的 mirror manifest，无 diff 协议。心跳 30s + 事件驱动（`relayHost.notifyChange()`）。relay 端连接断开时清空该 host 全部 mirror sessions（registry.Remove → broadcast CLOSE 给当前 attachers）。

OUT 帧 `seq` u64：从 1 开始单调递增，不在 reattach/reconnect 时重置。客户端 ATTACH(since_seq=N) 时 relay 从 ringbuf 取 `seq > N` 的帧补发；如果 ringbuf 已驱逐 N，relay 发 `OUT(seq=0, "\x1b[2J\x1b[H")` 表示截断后重放当前所有 history。

写入 WebSocket 必须串行：`nhooyr.io/websocket` 的 Write 不允许并发。每个连接持有一个 `out chan proto.Frame`，单 writer goroutine drain 之；ANNOUNCE/STREAM_REQUEST/STREAM_STOP/OUT/META 等所有帧都要走这个 channel。
