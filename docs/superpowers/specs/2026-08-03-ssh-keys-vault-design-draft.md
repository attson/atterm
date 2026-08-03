# SSH 密钥库(Keys Vault)设计

> **Status**: draft
> **Date**: 2026-08-03
> **Depends on**: 切片 1(连接内核)、切片 2(主机清单+凭据)、切片 3(E2EE 同步)
> **See also**: `desktop/ssh_hosts_store.go` · `desktop/ssh_sync.go` · `desktop/prefssync_adapter.go` · `desktop/frontend/src/components/SshHostsPanel.vue`

## 背景

当前(切片 2)私钥是**内嵌在主机上**的:`sshCredential.PrivateKey` 直接存该主机的 keyring。参照 Termius,把密钥提升为**独立实体(密钥库)**:一个 Key 可被多个主机引用,统一管理。UI 上在现有 SSH 面板顶部加 Hosts / Keys 两个 tab 切换。

**本地无迁移负担**(功能未发布,无真实用户数据):切片 2 的内嵌私钥路径直接删除,不做兼容。

## 范围

### 做

- **独立 Key 实体 + 密钥库 CRUD**:name + 私钥 PEM + 可选 passphrase;非敏感(id/name/key_type)入 config.json,私钥/passphrase 入 keyring(account = key ID)
- **主机认证改造**:`AuthKind` 从 `"password" | "privateKey"` 改为 **`"password" | "key"`**;`"key"` 时主机存 `KeyID` 引用某个 Key,**主机不再内嵌私钥**
- **连接**:auth_kind=="key" 时按 KeyID 取私钥连接
- **E2EE 同步**:Key(含私钥)与主机清单打包进**同一个** `ssh_hosts_encrypted` blob 同步(方案 A,原子一致),relay 不可读
- **删除保护**:删除被主机引用的 Key 时拒绝,返回引用它的主机名
- **私钥校验**:添加 Key 时解析私钥,失败则拒绝
- **UI**:SSH 面板顶部 Hosts / Keys tab;Keys 卡片网格 + 滑出表单;主机表单认证方式改为"密码 / 选择 Key 下拉"

### 不做(后置)

- ❌ Certificate / FIDO2(截图里 Termius 的其它认证类型)
- ❌ Identities(Termius 的"用户名+认证"组合实体)
- ❌ 从 ~/.ssh 导入现有密钥
- ❌ 切片 2 内嵌私钥的迁移(本地无数据)

### 关键边界决策(已与用户确认)

1. **主机只能引用 Key,不再内嵌私钥**:私钥必须先建 Key。密码认证保留为独立路径。
2. **Key 与主机清单同一 blob E2EE 同步**(方案 A):hosts + keys 强关联(主机靠 key_id 引用),放同一原子 blob 根除"引用了未同步的 key"裂缝。
3. **删除被引用的 Key 拒绝** + **添加时校验私钥有效性**。

## 数据模型

```go
// SSHKey is one key in the vault. Non-secret fields (id/name/key_type) live in
// config.json; the private key + passphrase live in the keyring keyed by ID.
type SSHKey struct {
	ID      string `json:"id"`                 // stable ULID
	Name    string `json:"name"`               // display name, e.g. "aws"
	KeyType string `json:"key_type,omitempty"` // parsed from PEM: "RSA"/"ED25519"/... (read-only)
}

// sshKeySecret is JSON-encoded into a single keyring entry keyed by key ID.
type sshKeySecret struct {
	PrivateKey string `json:"private_key"` // PEM
	Passphrase string `json:"passphrase,omitempty"`
}
```

**SSHHost 改造**:
```go
type SSHHost struct {
	ID       string `json:"id"`
	Alias    string `json:"alias,omitempty"`
	Host     string `json:"host"`
	Port     string `json:"port,omitempty"`
	User     string `json:"user"`
	AuthKind string `json:"auth_kind"`        // "password" | "key"
	KeyID    string `json:"key_id,omitempty"` // when auth_kind=="key"
	Group    string `json:"group,omitempty"`
	Note     string `json:"note,omitempty"`
}
```

`sshCredential` 保留 `Password`,删除 `PrivateKey`/`Passphrase`(私钥移到 Key 实体):
```go
type sshCredential struct {
	Password string `json:"password,omitempty"`
}
```

**config.json 新增** `SSHKeys []SSHKey`(仿 SSHHosts)。

keyring service:`com.atterm.ssh-key.v1` + `appdir.KeychainSuffix()`。

## 组件划分

### ① `desktop/ssh_keys_store.go`(新建)— 密钥库 CRUD

```go
func (a *App) ListSSHKeys() []SSHKey
func (a *App) AddSSHKey(name, privateKeyPEM, passphrase string) (SSHKey, error)
func (a *App) UpdateSSHKey(id, name, privateKeyPEM, passphrase string) error // PEM 空=保留原私钥
func (a *App) DeleteSSHKey(id string) error
```

- Add:生成 ULID;用 `ssh.ParsePrivateKey`/`ParsePrivateKeyWithPassphrase` 解析 → 取 `signer.PublicKey().Type()` 归一为 KeyType(如 "ssh-rsa"→"RSA");解析失败返回错误。私钥入 keyring。
- Update:name 必改;PEM 非空才覆盖 keyring 私钥(空=保留)。
- Delete:先扫 `cfg.SSHHosts` 找 `KeyID==id` 的主机,有则返回 `errKeyInUse` + 主机名列表;否则删 config + keyring。
- KeyType 解析辅助 `parseKeyType(pem, passphrase) (string, error)`。

### ② `desktop/ssh_hosts_store.go` — SSHHost 加 KeyID,sshCredential 删私钥字段

AuthKind 语义改为 password|key。AddSSHHost/UpdateSSHHost 的 cred 参数只承载 password。

### ③ `desktop/ssh_host.go::OpenSSHSession` / `NewSshSessionByID` — key 认证

`NewSshSessionByID`:
- auth_kind=="password" → 从 host keyring 取 password(不变)
- auth_kind=="key" → 按 `host.KeyID` 从 Key keyring 取 `sshKeySecret` → 构造 `sshclient.PrivateKeyAuth{PEM, Passphrase}`;Key 不存在 → 返回 `errKeyMissing`

`SSHConnectReq` 的 PrivateKey/Passphrase 字段保留(即席连接对话框仍可粘私钥),但"从清单连"走 Key 引用。

### ④ `desktop/ssh_sync.go` — payload 加 Keys(方案 A)

```go
type sshSyncPayload struct {
	Hosts []sshSyncHost `json:"hosts"`
	Keys  []sshSyncKey  `json:"keys"`
}
type sshSyncKey struct {
	Key        SSHKey `json:"key"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase,omitempty"`
}
```

`sealSSHHosts` 签名扩展为 `(accountKey, hosts, creds, keys, keySecrets)`;`openSSHHosts` 返回 keys + keySecrets。adapter ReadValue 组包 hosts+keys,WriteValue 落地 hosts+keys(私钥入 Key keyring)。

### ⑤ `desktop/prefssync_adapter.go` — ssh_hosts_encrypted 分支同时处理 keys

ReadValue:读 config.SSHKeys + 每 key 的 keyring secret,一起 seal。WriteValue:open 后写回 config.SSHHosts + config.SSHKeys + 两类 keyring。

### ⑥ 前端

- `SshHostsPanel.vue`:头部加 Hosts/Keys tab(`activeTab` ref);Hosts tab = 现有网格;Keys tab = 密钥卡片网格 + 滑出表单。
- 主机表单认证分段 `Password | Key`;Key 时下拉选 `listSSHKeys()`(空库提示去 Keys 添加)。
- `api.ts`:加 SSHKey 类型 + `listSSHKeys/addSSHKey/updateSSHKey/deleteSSHKey` 绑定;SSHHost 加 key_id 字段;SSHCredential 删私钥字段。

## 数据流

```
Keys tab CRUD → AddSSHKey → config.SSHKeys + keyring[ssh-key.v1/keyID] → markSSHHostsDirty
主机表单选 Key → host.auth_kind="key", host.key_id=<id>
从清单连 → NewSshSessionByID → auth_kind=="key" → 按 key_id 取私钥 → PrivateKeyAuth → 连接
同步 → seal(hosts + keys 同一 blob) → relay 只见密文 → 其它设备 open 落地 hosts+keys
```

## 错误处理

| 场景 | 处理 |
|---|---|
| 添加 Key 私钥无效 | 拒绝,"私钥解析失败" |
| 删除被引用的 Key | 拒绝,返回引用它的主机名(`errKeyInUse`) |
| 连接时 key_id 失效 | `errKeyMissing`,前端提示 |
| Key keyring 写失败 | 回滚 config |
| Update Key PEM 空 | 保留原私钥,只改 name |

## 测试策略(TDD)

- **ssh_keys_store**:Add 生成 ID+解析 KeyType+私钥入 keyring;无效 PEM 拒绝;Update(PEM 空保留);Delete 引用保护(被引用拒绝,含主机名);未引用可删。
- **连接**:auth_kind=="key" 按 key_id 取私钥连上(切片1 内存 ssh server + 私钥);key_id 失效→errKeyMissing。
- **同步**:hosts+keys 同一 blob seal/open 往返;密文无明文私钥泄漏(长 canary)。
- **前端**:Keys tab CRUD、tab 切换、主机表单 Key 下拉、删除引用报错。

## 未决 / 待 review 确认点

1. KeyType 归一映射:`ssh-rsa`→"RSA"、`ssh-ed25519`→"ED25519"、`ecdsa-*`→"ECDSA";未知类型显示原始 type 字符串。
2. 即席连接对话框(NewSshDialog)保留"粘私钥"路径,不强制建 Key(临时连接便利);只有"保存的主机"强制引用 Key。
