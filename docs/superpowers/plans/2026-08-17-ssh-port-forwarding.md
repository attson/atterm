# SSH 端口转发 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给已保存的 SSH 主机配置本地 / 远程 / 动态 SOCKS 三类端口转发，规则随 sealed vault 同步，运行中的隧道可见可控。

**Architecture:** `internal/sshclient` 拆出「只连不开 shell」的 `Conn`；桌面端 `tunnelManager` 按 hostID+ruleID 管理运行中的隧道，一台主机的多条规则共用一个 `Conn`；隧道**不是 relay session**，因此天然不进订阅计数。

**Tech Stack:** Go + golang.org/x/crypto/ssh + 手写 SOCKS5 服务端 + Wails v2 bindings + Vue 3 + Vitest

**Spec:** [`docs/superpowers/specs/2026-08-17-ssh-port-forwarding-design.md`](../specs/2026-08-17-ssh-port-forwarding-design.md)

## Global Constraints

- **隧道走独立 SSH 连接，不开 shell，不调 `AdoptSession`，不进 `relayHost.sessions`。** 红线 #2（lazy 上传：`SetSubscriberLifecycle` 在 0→1/N→0 触发 STREAM_REQUEST/STOP）必须由**构造**保证而非纪律：隧道压根不是 session，就不可能是订阅者。若隧道复用终端会话连接并被算作订阅者，开着隧道就会让 PTY 字节永远上传，即使无人观看。
- **监听默认绑 `127.0.0.1`。** 绑 `0.0.0.0` 意味着同网段任何人**无需任何 SSH 凭据**即可访问被转发的服务，且规则随 vault 同步到所有设备、每台都这么监听。允许用户改，但必须有明确警示，且**不得提供「一键允许局域网访问」入口**。
- **动态转发在非 loopback 绑定下是一个开放代理**，警示文案必须涵盖这一点。
- **带 `ProxyJump` / `ProxyCommand` 的主机不能起隧道**，与不能开终端同因（第 25 项 §5.3）。必须**复用同一个判定函数**，不得在隧道路径上重抄条件——重抄意味着第 27 项落地时会漏改一处。
- **隧道手动启停，不随连接自动起**（design §5.3）：隧道占用本地端口，自动起会在用户没做任何隧道相关动作时抢占 5432 / 8080。
- **连接断开即关闭该连接下所有 listener 并标记 stopped + 原因，不自动重连**（design §7.1）：自动重连会在用户不知情时反复重试认证，可能触发远端失败锁定。
- **SOCKS5 只做 `NO AUTHENTICATION` + `CONNECT`**，地址类型 IPv4 / IPv6 / DOMAINNAME；`BIND` 与 `UDP ASSOCIATE` 按 RFC 1928 回 `X'07'`，不断链。
- `internal/` 不依赖 `desktop/`（红线 #5）。
- 新增 `SSHHost.Forwards` 随既有 `ssh_hosts_encrypted` sealed blob 同步，**不新建同步机制**。

---

### Task 1: `sshclient` 拆出只连不开 shell 的 `Conn`

**Files:**
- Modify: `internal/sshclient/sshclient.go`
- Modify: `internal/sshclient/sshclient_test.go`

**Interfaces:**
- Produces:
  ```go
  type Conn struct{ /* client + closeCh */ }
  func DialConn(ctx context.Context, cfg Config) (*Conn, error)
  func (c *Conn) DialRemote(network, addr string) (net.Conn, error)
  func (c *Conn) ListenRemote(network, addr string) (net.Listener, error)
  func (c *Conn) Close() error
  ```
  `Dial` 保持现有签名与行为，改为在 `DialConn` 之上加 `RequestPty` + `Shell`。

- [ ] **Step 1: 写失败的测试**

关键断言是**没有 PTY 请求**。`sshclient_test.go` 里已有测试用的 SSH server，先读它怎么搭的，沿用同一套，并让它记录收到的 channel request 类型。

```go
func TestDialConnRequestsNoPTYAndNoShell(t *testing.T) {
	srv := newTestSSHServer(t) // 沿用既有构造，若名字不同以实际为准
	c, err := DialConn(context.Background(), srv.clientConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, r := range srv.ChannelRequests() {
		if r == "pty-req" || r == "shell" {
			t.Fatalf("DialConn must not open a shell; saw %q", r)
		}
	}
}

func TestDialStillOpensShell(t *testing.T) {
	// 回归保护：Dial 的行为不能被这次重构改掉
	srv := newTestSSHServer(t)
	s, err := Dial(context.Background(), srv.clientConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var sawPty, sawShell bool
	for _, r := range srv.ChannelRequests() {
		sawPty = sawPty || r == "pty-req"
		sawShell = sawShell || r == "shell"
	}
	if !sawPty || !sawShell {
		t.Fatalf("Dial must still request a pty and a shell; pty=%v shell=%v", sawPty, sawShell)
	}
}
```

> 实现者注意：既有测试 server 若不记录 request 类型，加一个记录钩子；**不要**为此新造一套 server。

- [ ] **Step 2: 跑测试确认失败** — `go test ./internal/sshclient/`

- [ ] **Step 3: 实现**

`DialConn` 做拨号 + 认证 + keepalive；`Dial` 调 `DialConn` 后再 `RequestPty`/`Shell`，失败时关掉 conn。`DialRemote` 包 `client.Dial`，`ListenRemote` 包 `client.Listen`。

- [ ] **Step 4: 跑测试确认通过并提交**

```bash
git add internal/sshclient/
git commit -m "feat(sshclient): add a shell-less Conn for port forwarding"
```

---

### Task 2: 规则模型、隧道管理器与本地转发

**Files:**
- Modify: `desktop/ssh_hosts_store.go`（`SSHHost` 加 `Forwards`）
- Create: `desktop/ssh_tunnels.go`
- Create: `desktop/ssh_tunnels_test.go`
- Modify: `desktop/ssh_host.go`（抽出共用的代理闸门判定）

**Interfaces:**
- Consumes: `sshclient.DialConn` / `Conn.DialRemote`
- Produces:
  ```go
  type ForwardRule struct {
      ID, Kind, BindAddr, BindPort, TargetHost, TargetPort, Note string
  }
  func (a *App) StartForward(hostID, ruleID string) error
  func (a *App) StopForward(hostID, ruleID string) error
  func (a *App) ListActiveForwards() []ActiveForward
  ```

- [ ] **Step 1: 抽出共用闸门**

第 25 项在 `NewSshSessionByID` 里内联了代理判定。抽成一个函数：

```go
// hostNeedsJump reports whether h must go through a jump host, and returns a
// user-facing reason. Both the terminal path and the tunnel path gate on this
// single function so roadmap item 27 has exactly one place to change.
func hostNeedsJump(h SSHHost) (bool, string)
```

`NewSshSessionByID` 改为调用它，**行为与文案不变**（第 25 项已有测试覆盖，必须仍然通过）。

- [ ] **Step 2: 写失败的测试**

```go
func TestStartForwardRefusesProxiedHost(t *testing.T) {
	// 带 ProxyJump 的主机 + 一条 local 规则 → StartForward 报错，
	// 且没有发起任何 dial（同第 25 项：用无凭据的主机做判别器，
	// 若闸门在读凭据之后才跑，报的会是 errCredentialMissing）
}

func TestStartForwardDefaultsBindToLoopback(t *testing.T) {
	// BindAddr 为空的规则，实际 listener 地址必须是 127.0.0.1
}

func TestLocalForwardCarriesBytesBothWays(t *testing.T) {
	// 起一个本地 echo 目标 + 测试 SSH server → StartForward →
	// 连本地端口 → 写入的字节原样返回
}

func TestStopForwardClosesListener(t *testing.T) {
	// StopForward 之后连本地端口必须被拒
}

func TestStartForwardDoesNotTouchSubscriberCounts(t *testing.T) {
	// 红线 #2：起隧道前后，目标 session 的订阅计数不变。
	// 断言方式以 internal/relay 既有测试怎么读订阅计数为准。
}
```

> 最后一条是本任务的核心。它断言的不是「我们没写那行代码」，而是**行为上**隧道不进订阅计数。

- [ ] **Step 3: 跑测试确认失败**

- [ ] **Step 4: 实现**

`tunnelManager` key 为 `hostID + "/" + ruleID`。同一主机多规则共用一个 `*Conn`，引用计数归零关连接。本地转发：`net.Listen(bind)` → 每个入站连接 `conn.DialRemote(target)` → 双向 `io.Copy`。

`net.Listen` 失败若为 `EADDRINUSE`，错误文案要说「本地端口 X 已被占用」，不要抛原始错误。

`*Conn` 断开时关闭该连接下所有 listener、标记 stopped 与原因，**不自动重连**。

- [ ] **Step 5: 跑测试确认通过并提交**

```bash
git add desktop/
git commit -m "feat(ssh): forward rules, tunnel manager and local port forwarding"
```

---

### Task 3: 远程转发

**Files:**
- Modify: `desktop/ssh_tunnels.go`
- Modify: `desktop/ssh_tunnels_test.go`

- [ ] **Step 1: 写失败的测试**

- 远端 listener 建立后，从远端发起的连接回连到本地目标，字节双向通。
- 远端拒绝监听时（权限 / `GatewayPorts`），错误文案指向**远端配置**，不是本地端口占用。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现** — `conn.ListenRemote(bind)` → 每个入站连接 `net.Dial(target)` → 双向 `io.Copy`。

- [ ] **Step 4: 跑测试确认通过并提交**

---

### Task 4: 动态转发（SOCKS5）

**Files:**
- Create: `internal/socks5/socks5.go`
- Create: `internal/socks5/socks5_test.go`
- Modify: `desktop/ssh_tunnels.go`

**Interfaces:**
- Produces：一个最小 SOCKS5 服务端，把 CONNECT 目标交给注入的 dialer
  ```go
  type Dialer func(network, addr string) (net.Conn, error)
  func Serve(l net.Listener, dial Dialer) error
  ```
  放 `internal/` 且 dialer 注入，所以它可以脱离 SSH 单测。

- [ ] **Step 1: 写失败的测试**

必须覆盖：
- 正常 `CONNECT`：IPv4 / IPv6 / DOMAINNAME 三种地址类型各一条。
- **畸形握手不 panic**：版本字节错误、方法数为 0、地址长度声明超过实际字节、连接在握手中途断开。
- `BIND`（`0x02`）与 `UDP ASSOCIATE`（`0x03`）回 `X'07' Command not supported`，**且不断链**。
- 认证协商：只接受 `NO AUTHENTICATION`（`0x00`），其它回 `0xFF`。

> 自己写协议解析是最容易出 bug 的地方，这一组测试是本任务的主要产出。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现** — 按 RFC 1928，只做 §5.4 划定的范围。

- [ ] **Step 4: 接进隧道管理器** — `dynamic` 规则起一个本地 listener 跑 `socks5.Serve`，dialer 用 `conn.DialRemote`。

- [ ] **Step 5: 跑测试确认通过并提交**

---

### Task 5: 前端规则编辑与活跃隧道面板

**Files:**
- Modify: `desktop/frontend/src/components/SshHostsPanel.vue`
- Modify: `desktop/frontend/src/components/SshHostsPanel.test.ts`
- Modify: `desktop/frontend/src/lib/api/_bindings.ts`、`desktop/frontend/src/lib/api/ssh.ts`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`、`zh-CN.ts`

> **binding shim 是手工维护的**（`_bindings.ts` 自己的注释说明这点），Wails 不会自动更新它。第 25 项就是在这里卡住的——本任务**必须**包含 shim。
>
> 该面板已迁到 i18n，新文案走 `t()` 并同时补两个 locale 文件，**不要**硬编码字符串。

- [ ] **Step 1: 写失败的测试**

- 规则列表渲染；新建 / 删除规则。
- **`BindAddr` 非 loopback 时出现警示文案**，且文案提到「同网段任何人无需凭据即可访问」；动态转发额外提到「开放代理」。
- 带 `proxy_jump` 的主机，启动按钮不可用并说明原因。
- 活跃隧道显示监听地址、目标与状态；连接断开后显示 stopped 与原因。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现**

- [ ] **Step 4: 跑测试 + `npx vue-tsc --noEmit` 并提交**

---

### Task 6: 同步验证与 roadmap

**Files:**
- Modify: `desktop/ssh_hosts_sync_fields_test.go`
- Modify: `docs/roadmap.md`

- [ ] **Step 1: 同步往返测试**

第 25 项已证明 sealed blob 加字段能过，但**仍要为 `Forwards` 再验一次**——第 21 项的教训是这类假设必须验证而非假定。沿用 `ssh_hosts_sync_fields_test.go` 既有写法，用可辨识的非空值，并确认字段被丢弃时测试会红。

- [ ] **Step 2: roadmap**

第 26 项六条勾选，并如实标注：跳板机上的转发依赖第 27 项；SOCKS5 只做 CONNECT（不支持 UDP / BIND）；不做自动启动。

- [ ] **Step 3: 提交**
