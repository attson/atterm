# ProxyJump / 跳板机链 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让写了 `ProxyJump` 的主机真正能连——单跳与多跳，终端会话与端口转发共用同一套链路构建。

**Architecture:** `sshclient.Config` 加一个 `Via *Conn`，非空时在已有连接上握手而不是自己建 TCP；桌面端一个链路构建器把 `ProxyJump` 解析成已保存主机序列，逐跳建连，任一跳失败就把前面已建的全部关掉。

**Tech Stack:** Go + golang.org/x/crypto/ssh + Wails v2 bindings + Vue 3 + Vitest

**Spec:** [`docs/superpowers/specs/2026-08-17-ssh-proxyjump-design.md`](../specs/2026-08-17-ssh-proxyjump-design.md)

## Global Constraints

- **每一跳都必须是 atterm 里已保存的主机**（按 Alias 或 Host 匹配）。查不到就拒绝并说「请先把它添加为主机」。**绝不拿目标主机的凭据去连跳板**——那是把目标机的密码或密钥送给一台不同的机器（design §5.1）。
- **每一跳独立校验 host key。** 只校验最终目标意味着一台被替换的跳板机可以在中间转发流量，而用户看到的是「目标指纹没变」。这是本项唯一的安全红线（design §5.2）。
- **TOFU 错误必须携带跳序与该跳的名字。** 用户在不知道对方是谁的情况下点「接受」，等于让 TOFU 形同虚设。现有 `HostKeyUnknownError{Fingerprint, Host}`（`desktop/app.go:118`）要扩展，且 `Error()` 仍需返回 `errCodeHostKeyUnknown` 哨兵，前端靠它结构化识别（`NewSshDialog.vue:24-25`）。
- **成环与深度上限必须在发起任何 dial 之前静态检出**（按 Alias 记录已访问集合，上限 10 跳）。在花掉真实副作用之前失败——与第 25 项 `Include` 环检测同理。
- **任一跳失败时，已建立的前几跳必须全部关闭**，否则一次失败的连接尝试会在跳板上留下挂着的会话。
- **`ProxyCommand` 仍然永不执行、仍然拒绝**，文案不变。本项只放开 `ProxyJump`。
- **判定点仍然只能有两个调用点。** 第 26 项 review 专门确认过 `hostNeedsJump` 只被 `NewSshSessionByID`（`ssh_host.go:69`）与 `StartForward`（`ssh_tunnels.go:116`）调用。放开 ProxyJump 时**两处都要改**——漏一处就会出现「终端能连、隧道说不支持」的割裂。
- 每层错误都要包上跳序与主机名：「connection refused」在四跳链路上毫无用处（design §6.1）。
- `internal/` 不依赖 `desktop/`（红线 #5）。

---

### Task 1: `sshclient` 支持在已有连接上握手

**Files:**
- Modify: `internal/sshclient/sshclient.go`
- Modify: `internal/sshclient/sshclient_test.go`

**Interfaces:**
- Produces：`Config.Via *Conn`。`Via == nil` 时行为与今天完全一致。

- [ ] **Step 1: 写失败的测试**

沿用 `startTestServer` / `serveConn` 既有写法（第 26 项已给它加过 `direct-tcpip` 支持，读一遍现状）。

```go
func TestDialConnViaAnotherConn(t *testing.T) {
	// 起两台测试 server：bastion 与 target。
	// 直连 bastion 拿到 *Conn，再用 Via: bastionConn 连 target。
	// 断言：target 侧确实收到了一条连接，且这条连接是 bastion 转发过来的
	// （用 bastion server 记录它收到的 direct-tcpip 目标地址）。
}

func TestDialConnWithoutViaStillDialsDirectly(t *testing.T) {
	// 回归保护：Via 为 nil 时不得走新路径。
}
```

> 第二条是本任务的回归守卫，比第一条更重要——每一个既有 SSH 会话都走 `Via == nil` 这条路。

- [ ] **Step 2: 跑测试确认失败** — `go test ./internal/sshclient/`

- [ ] **Step 3: 实现**

`DialConn` 里分支：

```go
var client *ssh.Client
if cfg.Via == nil {
	client, err = ssh.Dial("tcp", addr, clientCfg)
} else {
	raw, derr := cfg.Via.DialRemote("tcp", addr)
	if derr != nil {
		return nil, fmt.Errorf("sshclient: reach %s through jump host: %w", addr, derr)
	}
	cc, chans, reqs, herr := ssh.NewClientConn(raw, addr, clientCfg)
	if herr != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("sshclient: handshake with %s through jump host: %w", addr, herr)
	}
	client = ssh.NewClient(cc, chans, reqs)
}
```

注意 `raw` 在握手失败时必须关掉，否则跳板上留一条挂着的 channel。

- [ ] **Step 4: 跑测试确认通过并提交**

```bash
git add internal/sshclient/
git commit -m "feat(sshclient): dial over an existing connection for jump hosts"
```

---

### Task 2: 链路构建器

**Files:**
- Create: `desktop/ssh_jump.go`
- Create: `desktop/ssh_jump_test.go`
- Modify: `desktop/app.go`（`HostKeyUnknownError` 加跳序字段）

**Interfaces:**
- Consumes: `sshclient.Config.Via`
- Produces:
  ```go
  // jumpChain holds every connection opened to reach a target, target last.
  type jumpChain struct{ /* conns, in dial order */ }
  func (c *jumpChain) Target() *sshclient.Conn
  func (c *jumpChain) Close() error // closes target-first, back down the chain

  func (a *App) dialThroughJumps(ctx context.Context, h SSHHost, acceptHostKey bool) (*jumpChain, error)
  ```

- [ ] **Step 1: 写失败的测试**

必须覆盖（每条都对应一条 Global Constraint）：

```go
func TestJumpHopMustBeASavedHost(t *testing.T)      // ProxyJump nosuchhost → 明确错误，且没有发起任何 dial
func TestJumpChainDialsEveryHopInOrder(t *testing.T) // a,b,c → 三跳都建连，顺序正确
func TestJumpCycleDetectedBeforeAnyDial(t *testing.T) // a→b→a → 报错，dial 计数为 0
func TestJumpDepthLimited(t *testing.T)               // >10 跳 → 报错
func TestFailedHopClosesEarlierHops(t *testing.T)     // 第 2 跳失败 → 第 1 跳已关闭（用 server 连接计数断言）
func TestUnknownHostKeyNamesTheHop(t *testing.T)      // 中间跳 key 未知 → 错误带跳序与该跳名字
func TestJumpNeverReusesTargetCredential(t *testing.T) // 跳板用的是它自己那条主机记录的凭据
```

最后一条是**安全断言**，不是功能断言：给跳板和目标配不同的凭据，断言跳板收到的是它自己的那份。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现**

解析 `ProxyJump`：逗号分隔，从左到右。每段按 Alias 匹配已保存主机；带 `user@host:port` 时解析出来只用于匹配，不用于构造新主机。

先做静态检查（成环、深度、每跳可解析），**全部通过后**才开始建连。逐跳 `DialConn`，第 N 跳的 `Via` 是第 N-1 跳。失败时倒序关闭已建的。

`HostKeyUnknownError` 加 `HopIndex int` 与 `HopName string`；`Error()` **仍返回 `errCodeHostKeyUnknown`**（前端靠这个哨兵识别）。

- [ ] **Step 4: 跑测试确认通过并提交**

---

### Task 3: 接进终端与隧道两条路径

**Files:**
- Modify: `desktop/ssh_host.go`（`hostNeedsJump` 拆分 + `NewSshSessionByID`）
- Modify: `desktop/ssh_tunnels.go`（`StartForward` + `dialTunnelConn`）
- Modify: `desktop/app_ssh_test.go`、`desktop/ssh_tunnels_test.go`

- [ ] **Step 1: 写失败的测试**

- 带 `ProxyJump` 的主机现在**能开终端会话**，且经过了跳板。
- 带 `ProxyJump` 的主机现在**能起隧道**，且经过了跳板。
- 带 `ProxyCommand` 的主机**两条路径都仍被拒绝**，文案不变（回归）。

> 第三条是回归守卫。第 26 项的既有测试断言了 ProxyJump 被拒，那些断言**需要更新**——更新时要确认改的是「期望值」而不是把断言删掉。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现**

`hostNeedsJump` 改名并拆成「`ProxyCommand` → 拒绝」与「`ProxyJump` → 走链路」。**两个调用点都要改**，且改完后仍然只有两个调用点。

- [ ] **Step 4: 跑全套并提交**

```bash
go test ./desktop/ -tags webkit2_41
```

---

### Task 4: 前端与 roadmap

**Files:**
- Modify: `desktop/frontend/src/components/NewSshDialog.vue`、`SshHostsPanel.vue`
- Modify: 对应 `.test.ts`
- Modify: `desktop/frontend/src/lib/api/_bindings.ts`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`、`zh-CN.ts`
- Modify: `docs/roadmap.md`

> binding shim 是手工维护的（第 25 项在这里 BLOCKED 过）；面板已迁到 i18n（新文案走 `t()` 并补两个 locale）。

- [ ] **Step 1: 写失败的测试**

- TOFU 弹框在链路场景下**显示是哪一跳**：文案含跳序与该跳名字，且与「这不是你要连的目标」区分得开。
- 主机行上的跳板标记从「不可连接」改为显示链路（第 26 项加的 `ssh-host-proxy-*` 标记要更新，不是删掉）。
- 隧道页里带 ProxyJump 的主机启动按钮**不再禁用**。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现**

- [ ] **Step 4: roadmap**

第 27 项三条勾选，并如实标注：跳板每一跳必须是已保存主机；`ProxyCommand` 仍不支持。读 24 / 25 / 26 项的写法对齐诚实度。

- [ ] **Step 5: 跑全套并提交**
