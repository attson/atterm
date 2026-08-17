# SSH 端口转发（P6 第 26 项）— design

Date: 2026-08-17
Status: Drafted — awaiting user review before plan.
Parent: [2026-08-16 sync-layer roadmap](./2026-08-16-sync-layer-roadmap-design.md) §5 P6 第 26 项 · roadmap 第 26 项

## 0. Summary

给已保存的 SSH 主机配置端口转发规则（本地 `-L` / 远程 `-R` / 动态 SOCKS `-D`），规则随 sealed vault 同步，运行中的隧道在面板里可见、可开可停。

两条贯穿全篇的决定：

- **隧道走自己的 SSH 连接，不开 shell**，因此它永远不会变成 relay session，也就不可能进订阅计数（红线 #2 由**构造**保证，不靠纪律）。
- **监听默认只绑 `127.0.0.1`**，把本地转发暴露到整个局域网必须是用户显式的选择。理由见 §5.1。

## 1. 现状

- `internal/sshclient.Dial` **总是**要 PTY 并起 shell（`sshclient.go:72-133`）。用它开隧道会在远端留下一个登录会话、占一个 PTY，纯属浪费且会出现在 `who` 里。
- `Session` 持有 `*ssh.Client` 但不导出——转发需要 `client.Dial`（本地）与 `client.Listen`（远程）。
- `SSHHost`（第 25 项之后）有 `IdentityFile / ProxyJump / ProxyCommand`，随 `ssh_hosts_encrypted` 整块 sealed JSON 同步（AAD tag `0xF0`）。第 25 项已**证明**新增字段能过这条链路。
- 第 25 项的直连闸门在 `NewSshSessionByID`。隧道是另一条入口，**必须自己再挡一次**。

## 2. Goals

- 在主机上配置本地 / 远程 / 动态三类转发规则，随 vault 同步。
- 手动启停，运行状态可见（监听地址、目标、已接受连接数、错误）。
- 隧道生命周期与终端会话、与 uplink 订阅计数**完全无关**。

## 3. Non-Goals

- 不做开机自启 / 连接即自动起隧道。见 §5.3。
- 不做 Unix domain socket 转发（`-L /path:...`）。
- 不实现 SOCKS5 的 UDP ASSOCIATE 与 BIND，只做 CONNECT。见 §5.4。
- 不做跳板机上的转发（依赖第 27 项）。

## 4. 结构

### 4.1 `sshclient` 增加「只连不开 shell」

拆出 `DialConn(ctx, Config) (*Conn, error)`，只做拨号 + 认证 + keepalive，不 `RequestPty`、不 `Shell()`。`Dial` 改为在 `DialConn` 之上加 PTY 与 shell，保持现有签名与行为不变。

`Conn` 导出转发需要的两个能力：

```go
func (c *Conn) DialRemote(network, addr string) (net.Conn, error) // 本地转发用
func (c *Conn) ListenRemote(network, addr string) (net.Listener, error) // 远程转发用
func (c *Conn) Close() error
```

这样 `internal/sshclient` 仍然不知道 GUI 的存在（红线 #5 依赖方向不变）。

### 4.2 规则模型

```go
type ForwardRule struct {
    ID         string `json:"id"`
    Kind       string `json:"kind"`         // "local" | "remote" | "dynamic"
    BindAddr   string `json:"bind_addr"`    // 缺省 "127.0.0.1"
    BindPort   string `json:"bind_port"`
    TargetHost string `json:"target_host,omitempty"` // dynamic 不用
    TargetPort string `json:"target_port,omitempty"`
    Note       string `json:"note,omitempty"`
}
```

挂在 `SSHHost.Forwards []ForwardRule`，随既有 sealed blob 同步。**不新建同步机制**（母 spec 的原话）。

### 4.3 隧道管理器

`desktop/ssh_tunnels.go`：一个 `tunnelManager`，key 是 `hostID + ruleID`，值是运行中的隧道（listener + 计数 + cancel）。它**不碰** `relayHost`、不调 `AdoptSession`、不出现在 `a.host.sessions` 里。

一台主机上起多条规则**共用一个 `*Conn`**，引用计数归零时关连接。理由：每条规则一条 TCP 连接会让远端看到 N 个登录，且 keepalive 成倍。

## 5. 需要决断的地方

### 5.1 监听默认绑 loopback，绑 0.0.0.0 必须显式且带警告

`ssh -L 8080:db:5432` 默认只监听 `127.0.0.1`。我们照做，并且**这不是可以「为了方便」翻转的默认值**。

绑 `0.0.0.0` 意味着：同一 Wi-Fi 下的任何人都能通过你的机器访问那台数据库，**不需要任何 SSH 凭据**，而且这条隧道随 vault 同步到你所有设备、在每台上都这么监听。

处理：`BindAddr` 允许改，但 UI 里非 loopback 值必须有明确的警示文案说明「同网段任何人可用，无需凭据」。不做「一键允许局域网访问」这种入口。

### 5.2 隧道也要过第 27 项的跳板闸门

带 `ProxyJump` / `ProxyCommand` 的主机**不能起隧道**，和不能开终端是同一个理由（第 25 项 §5.3）：那台机器通常网络上不可直达，直连要么超时要么连上别的东西。

实现上必须是**共用一个判定函数**，不是在隧道路径上再抄一遍条件。抄一遍就意味着第 27 项落地时会漏改一处。

### 5.3 隧道手动启停，不随连接自动起

不做「连上主机就自动起隧道」。

理由：隧道会**占用本地端口**。自动起意味着打开一个终端就可能抢占 5432 或 8080，而用户没做任何与隧道有关的动作。端口冲突的报错会出现在一个他没在想这件事的时刻。

规则是配置，启动是动作。二者分开。

### 5.4 SOCKS5 自己写，只做 CONNECT

Go 标准库没有 SOCKS5 服务端，`golang.org/x/net/proxy` 只有客户端。所以动态转发要自己实现一个最小 SOCKS5 服务端。

范围：只支持 `NO AUTHENTICATION` + `CONNECT`，地址类型支持 IPv4 / IPv6 / DOMAINNAME。**不做 UDP ASSOCIATE 和 BIND**——前者要另开 UDP 通路且 SSH 转发本身不支持 UDP，后者几乎无人用。收到这两个命令时按 RFC 1928 回 `X'07' Command not supported`，而不是断链。

因为只绑 loopback（§5.1），不做认证是可接受的；一旦 `BindAddr` 非 loopback，这个 SOCKS 代理就是一个**开放代理**——这一点必须写进 §5.1 那条警示文案里。

### 5.5 端口占用要在启动时就说清楚

`net.Listen` 失败（`EADDRINUSE`）时给出「本地端口 8080 已被占用」而不是把 Go 的原始错误抛给用户。远程转发的失败不同：远端拒绝监听通常是 `GatewayPorts`/权限问题（<1024 需 root），错误文案要指向远端配置而不是本地。

## 6. 与红线 #2 的关系

红线 #2 是「远程 relay 静默时不传 PTY 字节，`SetSubscriberLifecycle` 在 0→1/N→0 时触发 STREAM_REQUEST/STOP」。

隧道**根本不进这套**：它不是 session，没有 subscriber，不调 `AdoptSession`。这是本设计把隧道连接与终端连接分开的直接收益——如果隧道复用终端会话的连接并被当成一个「订阅者」，那么开着隧道就会让 PTY 字节永远上传，即使没有人在看。

计划里要有一条测试**断言起一条隧道不改变任何 session 的订阅计数**，而不是靠「我们没写那行代码」。

## 7. 风险

1. **隧道连接断开后的行为。** 网络抖动会让 `*Conn` 掉线，此时隧道处于「listener 还在但转发必然失败」的状态。设计：连接断开即关闭该连接下所有 listener 并把规则标记为 stopped + 错误原因，**不自动重连**。自动重连会在用户不知情时反复重试认证，可能触发远端的失败锁定。
2. **同步带来的端口冲突。** 规则同步到另一台机器，那台上 8080 可能被别的东西占着。因为不自动启动（§5.3），冲突只在用户点启动时出现，可控。
3. **`SSHHost.Forwards` 又是一个新字段。** 第 25 项已证明 sealed blob 加字段能过，但**仍要再验一次**——第 21 项的教训是这类假设必须验证。成本是一条测试。
4. **SOCKS5 实现的正确性。** 自己写协议解析是出 bug 的地方。必须有针对畸形握手、截断地址、未知命令的测试。

## 8. 验证

- `DialConn` 不开 shell：断言远端没有 PTY 请求（用测试 SSH server 记录请求类型）。
- 本地转发：起隧道 → 连本地端口 → 数据到达目标 → 关隧道后连接被拒。
- 远程转发：远端 listener 建立、回连到本地目标。
- SOCKS5：正常 CONNECT；畸形握手不 panic；`BIND`/`UDP ASSOCIATE` 回 `X'07'`。
- **红线 #2**：起一条隧道，断言目标 session 的订阅计数不变、`SetSubscriberLifecycle` 未被触发。
- 跳板闸门：带 `ProxyJump` 的主机起隧道被拒，且**没有发起任何 dial**。
- 绑定地址：默认 `127.0.0.1`；显式设为 `0.0.0.0` 时 UI 出警示。
- 同步往返：`Forwards` 字段 seal→open 后值不丢。

## 9. 与母 spec 的差异

- 母 spec 只列了三种转发 + 状态面板 + 「不混进订阅计数」。本设计把「不混进」从**纪律**变成**构造**：隧道走独立连接，压根不是 session（§6）。
- 母 spec 未提绑定地址。本设计定为 loopback 默认，并说明为什么这不是一个可以为了方便翻转的默认值（§5.1）。
- 母 spec 未提自动启动。本设计明确**不做**（§5.3）。
- 母 spec 未提 SOCKS5 需要自己实现，也未限定范围（§5.4）。
