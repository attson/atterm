# SSH 连接内核(切片 1)设计

> **Status**: draft · 待用户 review
> **Date**: 2026-08-02
> **Slice**: 1 / 3(SSH 特性总规划的第一片)
> **See also**: [AGENTS.md](../../../AGENTS.md) · [docs/spec/architecture.md](../../spec/architecture.md) · [internal/relay/adopt.go](../../../internal/relay/adopt.go)

## 背景与产品定位

atterm 的核心价值是「从桌面启动的会话可被任意设备接管 + E2EE」。当前所有会话都是**本地** shell(PTY)。但「跑 codex/claude 长任务的机器」很多时候本身就是台**远程服务器**——用户 SSH 上去跑任务,然后想用手机接管。没有 SSH,这个核心场景直接断掉。

因此引入内建 SSH,不是「把 atterm 做成通用 SSH 客户端 / 复刻 Termius」,而是**把「可接管会话」从本机扩展到远程主机,强化 atterm 的长板**。

### 总规划(3 切片,本文档只设计切片 1)

| 切片 | 内容 | 独立价值 |
|---|---|---|
| **1(本文档)** | SSH 连接内核:连上远程主机、开可接管的远程 shell | 用户已能连远程主机 + 手机接管 |
| 2 | 主机清单持久化 CRUD + 凭据进 keyring 加密存储 + known_hosts 管理 | 管理多台主机、安全存凭据 |
| 3 | 主机配置 + 凭据 E2EE 同步到多端(复用 account_key / prefssync) | 多设备共享主机(相对 Termius 的差异化卖点:relay 全程不可读) |

**端口转发**在 3 个切片之外,作为**后续独立插件切片**(挂现有 `plugins/` 框架,默认关闭,与「接管 + E2EE」核心无协同,故隔离),本文档不涉及。

## 切片 1 范围

### 做

- 单个 SSH 连接:输入 host / port / user / 认证方式 → 连上开远程 shell
- 认证:**密码** + **私钥**(文件路径或粘贴内容,含可选 passphrase);不含 ssh-agent
- **known_hosts 验证(TOFU)**:首次连接展示指纹让用户确认,接受则写入 `~/.ssh/known_hosts`;指纹不匹配则**坚决拒绝**(疑似 MITM)
- 连上后作为**可接管会话**出现在现有会话列表,与本地 shell 一视同仁(自动获得 relay 镜像 / E2EE / 多端接管)
- 连接超时 + keepalive(核心场景是长任务挂机,二者是必需的稳定性保障)
- 连接 / 认证 / 断线错误清晰回传前端,不静默吞掉
- 最小 UI:一个「新建 SSH 连接」入口 + 填表单连上即可

### 不做(明确后置)

- ❌ 主机清单持久化 CRUD → 切片 2
- ❌ 凭据进 keyring 加密存储 → 切片 2(**切片 1 凭据用完即弃,不落盘**)
- ❌ 跨端 E2EE 同步 → 切片 3
- ❌ 端口转发 → 后续插件切片
- ❌ 跳板机 / ProxyJump、2FA / 键盘交互认证、ssh-agent、SFTP、多标签同步

### 关键边界决策(已与用户确认)

1. **连接信息用完即弃,不落盘**——每次新建 SSH 连接都重填,凭据只在本次连接内存里用完即弃。「持久化主机清单」正是切片 2 的核心价值,切片 1 落盘会与切片 2 的数据结构提前耦合、且引入未加密存密码的安全问题。known_hosts 除外(它是安全机制,照常写 `~/.ssh/known_hosts`)。
2. **known_hosts 首次未知走 TOFU 弹框确认**;指纹不匹配坚决拒绝、**不提供「忽略」按钮**(符合 atterm「安全默认」红线)。服务器重装换 key 的场景,用户需手动删 `~/.ssh/known_hosts` 里的旧条目——切片 1 接受这个粗糙度。

## 架构

**核心决策:SSH 会话是「另一种 PTY 来源」,复用现有接管管线,不碰核心协议。**

关键依据:`internal/relay/adopt.go` 的 `PtyHost` 接口只要求 `Read/Write/Resize` 三个方法。SSH 会话天然满足——`golang.org/x/crypto/ssh` 的 session channel 有 stdin/stdout/window-change。因此 SSH 会话可通过现有 `AdoptSession` 无缝接入接管 + E2EE + 多端镜像管线。

```
前端 NewSshSession(host,user,auth,cols,rows)
        │
        ▼
desktop/ssh_host.go  ── OpenSSHSession()
   ├─ sshclient.Dial (golang.org/x/crypto/ssh)   建立 SSH 连接
   ├─ NewSession + RequestPty + Shell()           开远程 shell
   └─ 包装成 sshPtyHost{} —— 实现 relay.PtyHost
                             (Read=remote stdout / Write=remote stdin / Resize=WindowChange)
        │
        ▼
h.server.AdoptSession(id, info, sshPtyHost, ownerUserID)   ← 复用现有钩子
        │
        ▼
现有管线自动接管:xterm.js 渲染 · relay 镜像 · E2EE 封装 · 手机/浏览器接管
```

### 遵守的红线(AGENTS.md)

- **红线 1 本地优先**:SSH 是可选连接类型,relay 不可达时本地照样能连 SSH。
- **红线 3 session_id 权威**:SSH 会话独立生成 session_id,走现有去重/attach/路由。
- **红线 4 协议向后兼容**:**不新增任何 proto 帧类型,不动 wire 协议**。
- **红线 5 internal 不依赖 desktop**:`internal/sshclient` 不 import 任何 desktop 代码;依赖方向 `desktop/ssh_host.go → internal/sshclient`。
- **红线 6 PTY winsize 在开时设好**:`RequestPty` 带初始 cols/rows,和本地 `pty.StartWithSize` 对称。

## 组件划分与接口契约

按「单一职责、可独立测试」切分。

### ① `internal/sshclient/` — 纯 SSH 客户端(不依赖 desktop)

```go
type Config struct {
    Host, Port, User string
    Auth       AuthMethod          // 密码 or 私钥
    HostKeyCb  ssh.HostKeyCallback // known_hosts 验证,由调用方注入
    Cols, Rows uint16
    Timeout    time.Duration       // 连接超时
    Keepalive  time.Duration       // keepalive 间隔(0 = 默认值)
}

// AuthMethod 密封实现:PasswordAuth{pw} / PrivateKeyAuth{pem, passphrase}
type AuthMethod interface{ sshAuthMethods() ([]ssh.AuthMethod, error) }

// Session 是一个已开好远程 shell 的 SSH 会话,满足 relay.PtyHost 的 Read/Write/Resize
type Session struct { /* ssh.Client + ssh.Session + stdin/stdout pipe */ }

func Dial(ctx context.Context, cfg Config) (*Session, error)   // Dial→NewSession→RequestPty→Shell
func (s *Session) Read(p []byte) (int, error)                  // ← remote stdout
func (s *Session) Write(p []byte) (int, error)                 // → remote stdin
func (s *Session) Resize(cols, rows uint16) error              // WindowChange
func (s *Session) Wait() error                                 // 阻塞至远程 shell 退出 / 断线
func (s *Session) Close() error
```

- 职责单一:只管 SSH 协议,不知道 relay/session 的存在。
- `HostKeyCb` 由调用方注入 → known_hosts 逻辑可测、可替换(测试注入 mock)。
- keepalive:内部后台 goroutine 定期 `SendRequest("keepalive@openssh.com")`,失败判定断线,使 `Wait()` 返回。

### ② `internal/sshclient/knownhosts.go` — known_hosts 验证 + TOFU

```go
// 返回 HostKeyCallback:命中已知指纹→放行;未知→回调 onUnknown 询问用户(TOFU);
// 不匹配→返回错误(疑似 MITM),不放行。
func KnownHostsCallback(
    path string,
    onUnknown func(host, fingerprint string) (accept bool),
) ssh.HostKeyCallback
```

- 用 `golang.org/x/crypto/ssh/knownhosts` 解析,外包一层 TOFU 交互。
- 首次连接:onUnknown 返回 accept=true 时,把指纹追加写入 `~/.ssh/known_hosts`。
- 不匹配:直接返回 error(区分于「未知」),上层映射为 `ErrHostKeyMismatch`。

### ③ `desktop/ssh_host.go` — 接入层(与 `relay_host.go::NewSession` 对称)

```go
// sshPtyHost 把 sshclient.Session 适配成 relay.PtyHost
type sshPtyHost struct{ *sshclient.Session }

// OpenSSHSession: Dial → 构造 SessionInfo → AdoptSession → 返回 session_id;
// 断线(Session.Wait 返回)触发 cleanup(AdoptSession cleanup + 关闭 sshclient.Session)。
func (h *relayHost) OpenSSHSession(ctx context.Context, req SSHConnectReq) (uuid.UUID, error)
```

- `SessionInfo`:Title/Host/User 反映远程主机(如 `Title="ssh user@host"`),StartedAt=now,HostID 沿用本机 host_id(会话归属本 app 实例)。

### ④ `desktop/app.go` — 前端 binding

```go
func (a *App) NewSshSession(req SSHConnectReq) (NewSessionResp, error)
// SSHConnectReq{ Host, Port, User, AuthKind, Password, PrivateKey, Passphrase,
//                Cols, Rows, AcceptHostKey bool }
// known_hosts 未知指纹:返回 ErrHostKeyUnknown + 指纹;前端弹确认框;
// 用户接受后带 AcceptHostKey=true 重试(重试时 onUnknown 直接放行并写入)。
```

## 错误处理与生命周期

### 错误分类(清晰回传,不静默吞)

| 场景 | 处理 |
|---|---|
| 网络连不通 / 超时 | 明确错误码 + 消息,前端提示「无法连接 host:port」 |
| 认证失败 | 区分「密码错」/「私钥无效或 passphrase 错」,针对性提示 |
| known_hosts 未知 | 返回 `ErrHostKeyUnknown` + 指纹,前端弹 TOFU 确认框 |
| known_hosts 不匹配 | 返回 `ErrHostKeyMismatch`(疑似 MITM),**强提示 + 拒绝**,不给「忽略」 |
| 私钥文件读不到 / 解析失败 | 明确提示路径无效 / 私钥格式错误 |
| 连上后断线 | 走现有会话结束路径(与本地 PTY 退出对称),清理 AdoptSession |

### 生命周期

- `Dial` 成功 → `AdoptSession` 注册 → 返回 session_id
- SSH 会话断开(`Session.Wait()` 返回)→ 触发 cleanup:AdoptSession cleanup + 关闭 sshclient.Session
- app 关闭 → 所有 SSH 会话随之关闭
- keepalive:后台 goroutine 定期发 SSH keepalive,失败判定断线并走清理

## 测试策略(TDD)

- **`internal/sshclient` 单测**:用 `golang.org/x/crypto/ssh` 起内存测试 server,验证 Dial / 认证成功失败 / Resize / Read / Write / Close,注入 mock HostKeyCallback。
- **known_hosts 单测**:已知指纹放行 / 未知触发 onUnknown / 不匹配拒绝 / TOFU 写入后再连不再询问。
- **`desktop/ssh_host.go` 测试**:mock relayHost,验证 Dial 成功后正确调用 AdoptSession、SessionInfo 字段正确、断线触发 cleanup;认证 / known_hosts 各类错误正确回传前端错误码。
- **不测**:真实远程主机(集成测试留手动验证)、前端 UI 交互(切片 1 UI 最小)。

## 前端(最小)

- 会话新建入口旁增加「新建 SSH 连接」动作 → 打开一个最小表单(host/port/user/认证方式/密码或私钥)。
- 提交调用 `NewSshSession`;返回 `ErrHostKeyUnknown` 时弹指纹确认对话框,确认后带 `AcceptHostKey=true` 重试。
- 连上后的会话复用现有 `TerminalView` / 会话列表,无需新终端组件。

## 未决 / 待 review 确认点

1. **known_hosts 不匹配坚决拒绝、不给「忽略」按钮**——安全立场默认如此。若认为过严(服务器重装换 key 场景),可在切片 2 的 known_hosts 管理里提供「删除旧条目」入口,而非在连接时给「忽略」。
2. 私钥文件路径 vs 粘贴内容:切片 1 两者都支持,读文件路径时仅在本次连接内读取、不缓存路径。
