# ProxyJump / 跳板机链（P6 第 27 项）— design

Date: 2026-08-17
Status: Drafted — awaiting user review before plan.
Parent: [2026-08-16 sync-layer roadmap](./2026-08-16-sync-layer-roadmap-design.md) §5 P6 第 27 项 · roadmap 第 27 项

## 0. Summary

让写了 `ProxyJump` 的主机能真正连上——单跳与多跳链路，终端会话与端口转发共用同一套。

本项**解锁**第 25 / 26 项里被明确拒绝的那批主机。那两项当时的选择是「宁可拒绝，不要瞎连」（第 25 项 §5.3）；现在补上正确的连法。

两条贯穿全篇的决定：

- **每一跳都必须是 atterm 里已保存的主机。** 理由见 §5.1——这是「凭据从哪来」唯一诚实的答案。
- **每一跳都独立校验 host key，且 TOFU 提示必须说清是哪一跳。** 理由见 §5.2。这是本设计里唯一一条安全红线。

`ProxyCommand` 仍然永不执行（第 25 项 §5.3），本项不改这一点。

## 1. 现状

- `hostNeedsJump`（`desktop/ssh_host.go:33-45`）是**唯一**的判定点，终端路径（`NewSshSessionByID`）与隧道路径（`StartForward`）共用。第 26 项刻意做成这样，就是为了本项只改一处。
- `SSHHost.ProxyJump` / `ProxyCommand` 字段已存在（第 25 项），随 `ssh_hosts_encrypted` sealed blob 同步，第 26 项的往返测试已证明新字段能过这条链路。**所以本项不需要任何新的同步工作**——母 spec 说的「链路配置存进 host 记录随 vault 同步」已经成立。
- `sshclient.DialConn`（`sshclient.go:95-125`）用 `ssh.Dial("tcp", addr, cfg)`，它自己建 TCP。跳板需要的是在**已有 net.Conn 上**握手。
- `Conn.DialRemote`（第 26 项）已经能从一跳穿出去拿到 `net.Conn`——正是下一跳需要的传输层。
- `KnownHostsCallback`（`internal/sshclient/knownhosts.go:18`）已有 TOFU 回调，但签名只带 `host` 与 `fingerprint`，不带「这是链上第几跳」。

## 2. Goals

- 单跳与多跳（`ProxyJump a,b,c`，从左到右依次穿过）都能连。
- 终端会话与端口转发走同一条链路构建逻辑。
- 每一跳独立认证、独立校验 host key。

## 3. Non-Goals

- 不实现 `ProxyCommand`（任意命令，RCE 面）。
- 不做 ssh_config 里 `ProxyJump` 的全部语法（`user@host:port` 里的 user/port 覆盖见 §5.1 的处理）。
- 不做跳板链上的 agent forwarding。

## 4. 机制

`sshclient` 加一个「在已有传输上握手」的入口：

```go
// Via, when non-nil, is the connection this dial rides on: the TCP connection
// is opened *from that host* instead of from this machine. Nil means dial
// directly, which is what every existing caller does.
type Config struct {
    // …existing fields…
    Via *Conn
}
```

`DialConn` 分支：`Via == nil` 时保持现有 `ssh.Dial`；否则 `Via.DialRemote("tcp", addr)` 拿到 `net.Conn`，再 `ssh.NewClientConn` + `ssh.NewClient`。

链路就是折叠：第一跳直连，第 N 跳 `Via` 前一跳，目标 `Via` 最后一跳。

**关闭顺序**：关目标连接不会自动关掉沿途的跳板连接。链路构建器返回一个句柄，`Close` 时**从目标往回**逐跳关闭。中途某跳建立失败时，必须把已经建好的前几跳全部关掉再返回错误——否则一次失败的连接尝试会在跳板上留下挂着的会话。

## 5. 四个需要决断的地方

### 5.1 每一跳必须是已保存的主机

`ProxyJump bastion` 里的 `bastion` **按 Alias 在 atterm 的主机清单里查**。查不到就拒绝，并说清楚「请先把 bastion 添加为主机」。

理由：跳板机自己需要认证。可选项只有三个，另外两个都更差——

- **拿目标主机的凭据去连跳板**：错的，而且是危险的错。它会把目标机的密码或密钥送给一台不同的机器。
- **连接时弹框问跳板凭据**：凭据不落盘就每次都问，落盘就等于偷偷造了一条主机记录。

查已保存主机则三样东西一次到位：凭据、端口、用户名，而且都在用户已经审视过的地方。

`user@host:port` 形式的覆盖：解析出来后，**只用于匹配已保存主机**（比对 Alias 或 Host），不用于凭空构造一台没有凭据的主机。解析不出对应主机就走上面的拒绝路径。

### 5.2 每一跳独立校验 host key，且 TOFU 必须指名是哪一跳

这是本设计唯一的安全红线。

链路上每一跳都要走 `KnownHostsCallback`，**不能只校验最终目标**。只校验目标意味着一台被替换的跳板机可以在中间转发流量，而用户看到的是「目标主机指纹没变」。

更要紧的是 TOFU 弹框：现在的 `HostKeyUnknownError` 只带 host 和 fingerprint。在链路里，用户会看到一个不认识的指纹，却**不知道这是终点还是路上第二跳**。必须扩展成携带跳序与该跳的名字，弹框文案要明说「这是链路上的第 2 跳 bastion-b，不是你要连的 db-1」。

一个用户在不知道是谁的情况下点「接受」，等于把 TOFU 变成了摆设。

### 5.3 成环与深度上限

`a → b → a` 必须在**建立任何连接之前**被静态检出——按 Alias 记录已访问集合。深度上限 10 跳。

超限或成环都返回明确错误，不是静默截断，也不是连到一半才发现。理由和第 25 项解析器里的 `Include` 环检测一致：**在花掉真实副作用之前失败**。

### 5.4 闸门只放开 ProxyJump

`hostNeedsJump` 拆成两件事：

- `ProxyCommand != ""` → 仍然拒绝，文案不变。
- `ProxyJump != ""` → 不再拒绝，改为交给链路构建器。

函数名届时应改（它不再表示「需要跳板所以不能连」），但**两个调用点必须仍然只有两个**——第 26 项 review 专门确认过这一点，本项不能借机把判定散开。

## 6. 风险

1. **一跳的失败要说清是哪一跳失败的。** 「connection refused」在四跳链路上毫无用处。每层错误都要包上跳序与主机名。这是第 26 项远程转发那条教训的同一形态：**把用户指向错误的机器，比含糊更糟**。
2. **跳板连接的生命周期。** 多条目标共用同一台跳板时，是每个目标各建一条跳板连接，还是共享？第 26 项已经有 per-host 引用计数的 `*Conn` 可以借鉴，但**本设计倾向先不共享**：链路语义比端口转发复杂，过早共享会让「哪条链还活着」难以推理。先每条链独立，观察后再优化。
3. **known_hosts 里跳板的条目。** 经跳板连接目标时，目标的 host key 条目仍按目标的 host:port 记录，与直连一致——否则同一台机器直连与经跳板会产生两条不同条目，用户会被问两次。
4. **第 26 项的 `hostNeedsJump` 拆分要小心。** 那个函数现在同时挡着终端和隧道两条路。放开 ProxyJump 时若漏改一处，会出现「终端能连、隧道说不支持」的割裂。

## 7. 验证

- 单跳：经一台跳板连上目标，字节双向通。
- 多跳：`a,b,c` 三跳，断言**每一跳都建立了连接**（用测试 server 记录），而不只是最终连上。
- 成环：`a → b → a` 在**未发起任何 dial** 前返回错误。
- 深度：超过 10 跳返回错误。
- host key：链路中间一跳的 key 未知时，返回的错误**带跳序与该跳名字**；只有目标未知时同理。
- 失败清理：第 2 跳失败时，第 1 跳的连接已被关闭（用测试 server 的连接计数断言）。
- 未保存的跳板：`ProxyJump nosuchhost` 返回「请先添加为主机」而不是尝试连接。
- 隧道路径：带 ProxyJump 的主机现在能起隧道，且走的是同一条链路构建逻辑。
- 回归：`ProxyCommand` 的主机仍被拒绝，文案不变。

## 8. 与母 spec 的差异

- 母 spec 说「链路配置存进 host 记录随 vault 同步」。**这部分已经成立**（第 25 项加的字段，第 26 项证明了同步），本项不需要新增同步工作。
- 母 spec 未提跳板凭据从哪来。本设计明确：**每一跳必须是已保存主机**（§5.1）。
- 母 spec 未提 host key。本设计把「每跳独立校验 + TOFU 指名跳序」列为唯一安全红线（§5.2）。
