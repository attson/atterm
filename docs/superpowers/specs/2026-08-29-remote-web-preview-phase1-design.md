# 远程 Web Preview 阶段 1 — design

Date: 2026-08-29
Status: Implemented
Parent: P8 v0.8 远程 Web Preview

## 0. Summary

阶段 1 让已 attach 的远程 driver 显式输入 owner desktop 上的 loopback
端口，并在当前 session 内打开一个临时 Preview。HTTP 字节不进入 PTY
uplink：控制帧沿现有 `/client` → mirror session → `/uplink` 路由，实际 TCP
字节走独立的 `/service-client` / `/service-host` WebSocket 对。

relay 只配对并转发密文。`account_key` 不交给原生代理；两端分别从
`account_key + service_id` 派生方向独立的 service keys，客户端只把这两把
临时派生 key 交给 Wails/Capacitor 的 loopback 代理。

## 1. 用户范围

- 入口只出现在 remote session、当前 driver、`remote_permission=full`、且平台
  有原生 Preview bridge 时。
- 用户手工输入 `1..65535` 端口；owner 始终只拨
  `127.0.0.1:<port>`，wire 上没有任意 host 字段。
- 成功后当前 TerminalView 出现 `Terminal / Preview :port` 两个内部 tab；
  Preview 由原生 loopback URL 驱动，关闭即撤销 lease。
- 阶段 1 支持 Desktop(Wails) 与 iOS(Capacitor)。纯 Web/PWA 不显示入口。

## 2. 控制协议

新增 additive frame，不改任何既有 payload：

- `SERVICE_OPEN 0x3d` client → relay → desktop uplink
- `SERVICE_OPENED 0x3e` desktop uplink → relay → requesting client
- `SERVICE_CLOSE 0x3f` client → relay/desktop lease teardown

`SERVICE_OPEN` 外层 JSON 只含路由所需的 `request_id/service_id` 与 relay
注入的一次性 `host_ticket`；`{port, scheme}` 放在 `sealed` 中，AAD tag
`0x3d`。relay 验证 attached + driver + `remote_permission=full`，desktop
再按自己的 raw permission fail-closed 验一次并解封 port。

relay 为每次请求生成单次 `host_ticket/client_ticket`。ticket 只在 WS 首帧
注册消息里出现，不进 URL、不进日志，配对后立即失效。relay 接受注册后先回
`{"ok":true}` ACK；host 收到 ACK 后才回 `SERVICE_OPENED`，client 收到 ACK
后才开放本机 loopback listener，避免“注册已写出但 ticket 实际被拒”被误报为成功。

## 3. 数据协议

每个 Preview 只有一对 service WebSocket，但在其中 multiplex 浏览器建立的
多条 TCP 连接。每个 binary message：

```text
magic "ATSP" (4) | version (1) | kind (1) | reserved (2)
conn_id (u32 BE) | seq (u64 BE) | ciphertext_len (u32 BE)
ciphertext (AES-256-GCM, tag included)
```

`kind = open/data/close`。方向 key 分离：

```text
HKDF-SHA256(account_key,
  info = "atterm-service-v1" || service_uuid,
  length = 64)
client_to_host = first 32B
host_to_client = last 32B
```

每方向 `seq` 从 1 单调递增；AES-GCM nonce = `0x00000000 || seq_be64`。
AAD = `service_uuid || 24-byte header`。relay 能校验 header/大小和统计流量，
但不能打开 TCP payload。

## 4. 生命周期与边界

- 每用户最多 4 个未结束 Preview；每 Preview 最多 16 条 TCP 连接。
- 单数据 message 明文最多 64 KiB；每 Preview 累计转发最多 512 MiB。
- 未配对 30 秒过期；配对后连续 10 分钟无数据自动关闭。
- requesting `/client`、owner `/uplink`、session、任一 service WS 断开都会
  撤销 lease，关闭另一侧 WS 与全部 loopback/target TCP 连接。
- service hub 不调用 `Subscribe`、`SetSubscriberLifecycle` 或
  `AdoptSession`，因此不会改变 lazy PTY 上传语义。
- 阶段 1 不做公网分享、通用 TCP/SOCKS、任意目标 host、自动端口扫描、
  自动暴露、纯 Web/PWA、SSH 临时 forward、HMR 兼容承诺。

## 5. 验证

- crypto cross-direction round trip、tamper/replay/跨 service 拒绝。
- relay：owner 隔离、driver/full 双门、ticket 单次、配对双向、TTL/额度、
  session/uplink/client cleanup。
- desktop：只拨 loopback、权限 fail-closed、target refusal 只关闭对应 conn、
  service teardown 不触碰 session subscriber lifecycle。
- frontend：入口 gate、端口校验、open timeout/late response、Preview/Terminal
  切换与 unmount stop。
- Capacitor：plugin 显式注册、App target source membership、Swift build。
