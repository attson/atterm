# SSH 主机清单持久化 + 凭据加密(切片 2)设计

> **Status**: draft
> **Date**: 2026-08-02
> **Slice**: 2 / 3
> **Depends on**: 切片 1(SSH 连接内核,`docs/superpowers/specs/2026-08-02-ssh-connect-slice1-design-draft.md`)
> **See also**: [AGENTS.md](../../../AGENTS.md) · `desktop/config.go` · `desktop/account_key_store.go` · `internal/safekeyring`

## 背景

切片 1 让 atterm 能连远程主机并被接管,但连接信息用完即弃、每次重填。切片 2 把它升级为**可管理的主机清单 + 安全存凭据**:主机 CRUD、敏感凭据进系统钥匙串、known_hosts 条目管理。E2EE 跨端同步是切片 3,端口转发是后续插件切片,本切片不涉及。

## 范围

### 做

- **主机清单 CRUD**:增/删/改/查主机记录
- **主机数据模型**:别名 / host / port / user / 认证方式 / 分组 / 备注 + 稳定 ID(ULID)
- **凭据加密存储**:密码 / 私钥 / passphrase 存 `safekeyring`(系统钥匙串 + 文件回退),account = 主机 ID;非敏感字段存 `config.json`
- **从清单连接**:选中主机 → 按 ID 从 keyring 取凭据 → 复用切片 1 的 `OpenSSHSession` 直连;未存凭据则前端弹框补
- **known_hosts 管理**:列出 / 删除 `~/.ssh/known_hosts` 条目(解决切片 1 遗留的"服务器重装换 key"场景)
- **切片 1 即席连接路径保留**:临时连接与"从清单连"两条路径并存

### 不做(后置)

- ❌ 主机配置 / 凭据 E2EE 跨端同步 → 切片 3
- ❌ 端口转发 → 后续插件切片
- ❌ 跳板机 / 默认命令 / per-host env(Termius 丰富模型)
- ❌ 主机清单导入 ~/.ssh/config

### 关键边界决策(已与用户确认)

1. **凭据寻址用主机稳定 ID(ULID)**:config.json 存非敏感字段 + ID,keyring 用 ID 作 account。改 host/user 不丢凭据;删主机按 ID 清凭据;为切片 3 同步铺路。
2. **从清单连接默认从 keyring 取凭据直连**(未存则弹框补),不是每次弹框确认。符合"一键连"预期。
3. **敏感凭据永不写 config.json**,只进 keyring。config.json 里主机记录仅含非敏感字段。

## 数据模型

```go
// SSHHost is the non-secret part of a saved host, persisted in config.json.
// Credentials (password / private key / passphrase) live in the keyring
// keyed by ID, never here.
type SSHHost struct {
	ID       string `json:"id"`                 // stable ULID, generated on Add
	Alias    string `json:"alias,omitempty"`    // display name; falls back to user@host
	Host     string `json:"host"`
	Port     string `json:"port,omitempty"`     // default "22"
	User     string `json:"user"`
	AuthKind string `json:"auth_kind"`          // "password" | "privateKey"
	Group    string `json:"group,omitempty"`    // optional grouping label
	Note     string `json:"note,omitempty"`
}
```

`appConfig` 新增 `SSHHosts []SSHHost json:"ssh_hosts,omitempty"`(仿现有 `PinnedSessionIDs []string`)。

**凭据在 keyring 的形态**:一条主机的凭据序列化为一个 JSON blob,存单个 keyring 条目,避免多字段多条目。

```go
// sshCredential is what gets JSON-encoded into a single keyring entry keyed
// by host ID. Only the fields relevant to the host's AuthKind are populated.
type sshCredential struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"` // PEM
	Passphrase string `json:"passphrase,omitempty"`
}
```

keyring service:`com.atterm.ssh-credential.v1` + `appdir.KeychainSuffix()`(仿 `accountKeyService()` 的版本化命名)。

## 组件划分

### ① `desktop/ssh_hosts_store.go` — 主机 CRUD

```go
func (a *App) ListSSHHosts() []SSHHost
func (a *App) AddSSHHost(h SSHHost, cred sshCredential) (SSHHost, error)   // 生成 ID;非敏感→config,敏感→keyring
func (a *App) UpdateSSHHost(h SSHHost, cred *sshCredential) error          // cred==nil 表示只改非敏感字段,不动凭据
func (a *App) DeleteSSHHost(id string) error                              // 删 config 记录 + keyring 凭据
```

- 单一职责:主机记录与凭据的持久化。
- Add 生成 ULID(项目已用 `github.com/oklog/ulid/v2`)。
- Delete 同步删 keyring 凭据(`safekeyring.Delete`,ErrNotFound 视为已删)。

### ② `desktop/config.go` — appConfig 扩展

新增 `SSHHosts []SSHHost` 字段 + 读写它的 helper(仿 `GetPinnedSessionIds` / `SetPinnedSessionIds`)。

### ③ `desktop/ssh_host.go`(切片 1 文件)— 新增按 ID 连接

```go
// OpenSSHSessionByID looks up host + credential by ID and delegates to the
// slice-1 OpenSSHSession. Returns a distinct error if no credential is stored
// so the frontend can prompt for one.
func (a *App) NewSshSessionByID(id string) (NewSessionResp, error)
```

- 复用切片 1 的 `OpenSSHSession` + known_hosts 回调,不重写连接逻辑。
- 未存凭据 → 返回 `errCredentialMissing`,前端弹框补(补的凭据可选"记住"→ 走 UpdateSSHHost)。

### ④ `desktop/ssh_known_hosts.go` — known_hosts 管理

```go
type KnownHostEntry struct {
	Host        string `json:"host"`
	Fingerprint string `json:"fingerprint"`
}
func (a *App) ListKnownHosts() ([]KnownHostEntry, error)   // 解析 ~/.ssh/known_hosts
func (a *App) RemoveKnownHost(host string) error           // 删除匹配行,重写文件
```

- 解析用 `golang.org/x/crypto/ssh/knownhosts` / 逐行解析;删除按 host 匹配。
- 复用切片 1 的 `sshKnownHostsPath` 覆盖点做测试隔离。

### ⑤ 前端

- 主机清单面板(CRUD 表单 + 列表,按 group 分组展示),挂现有面板位置(参考 Settings / 侧栏)。
- 列表项"连接"按钮 → `NewSshSessionByID` → 开 tab(复用切片 1 的 `openSshTab`)。
- 未存凭据时弹凭据补充框(复用切片 1 的 `NewSshDialog` 的凭据字段部分,或轻量子表单)。
- known_hosts 管理小面板:列出条目 + 删除按钮。

## 数据流

```
前端 CRUD 表单
   │ AddSSHHost(host, cred)
   ▼
ssh_hosts_store:  host(非敏感) → appConfig.SSHHosts → config.json
                  cred(敏感)   → safekeyring[com.atterm.ssh-credential.v1 / id]
   │
从清单连接: NewSshSessionByID(id)
   │ 读 config 取 SSHHost + 读 keyring 取 cred(按 id)
   ▼
切片 1 OpenSSHSession(SSHConnectReq{...}, knownHostsCb)  → AdoptSession → 接管管线
```

## 错误处理

| 场景 | 处理 |
|---|---|
| Add/Update 时 keyring 写失败 | 回滚 config 改动,返回明确错误 |
| 连接时 keyring 无凭据 | 返回 `errCredentialMissing`,前端弹框补 |
| Delete 时 keyring 无凭据 | 视为已删(ErrNotFound 幂等) |
| known_hosts 文件不存在 | ListKnownHosts 返回空;RemoveKnownHost 幂等 |
| 主机 ID 不存在 | 返回明确 "no such host" |

## 测试策略(TDD)

- **store CRUD**:Add 生成 ID、非敏感入 config / 敏感入 keyring;Update(含 cred==nil 只改非敏感);Delete 同步清凭据。用 `safekeyring.UseFileStore()` + `SetFileDirForTest` 隔离。
- **凭据随主机生命周期**:Add→凭据可读回;Delete→凭据消失;改 host/user→凭据仍在(按 ID)。
- **NewSshSessionByID**:有凭据→连上(用切片 1 的内存 ssh server);无凭据→返回 errCredentialMissing。
- **known_hosts 管理**:List 解析多条;Remove 删指定 host 后 List 不再含它;文件不存在时幂等。
- **前端**:主机清单 CRUD 组件、连接触发、凭据补充框、known_hosts 面板(mock api 层)。
- **不测**:真实远程主机(手动冒烟)。

## 未决 / 待 review 确认点

1. 凭据 JSON blob 单条目 vs 每字段单条目:选单条目(减少 keyring 条目数,整机凭据原子读写)。
2. known_hosts 删除按 host 精确匹配;hashed known_hosts(`|1|...`)条目的匹配在切片 2 尽力而为,不保证全覆盖(记为已知限制)。
