# SSH 主机清单 E2EE 跨端同步(切片 3)设计

> **Status**: draft
> **Date**: 2026-08-03
> **Slice**: 3 / 3(SSH 特性收官)
> **Depends on**: 切片 1(连接内核)、切片 2(主机清单持久化 + 凭据加密)
> **See also**: `internal/prefssync/sync.go` · `desktop/prefssync_adapter.go` · `internal/e2eecrypto/envelope.go` · `desktop/account_key_store.go`

## 背景

切片 2 让主机清单存在本机(非敏感入 config.json,凭据入 keyring)。切片 3 把它同步到用户的所有设备,且 **relay 全程不可读**——这是 atterm 相对 Termius 的差异化卖点(Termius 你得信任它的服务器)。

复用现有基建:`prefssync.Engine`(Pull/Push/last-write-wins)+ account_key E2EE(`e2eecrypto.SealUnsequenced/OpenUnsequenced`)。关键点:现有 synced 偏好是**明文**发给 relay 的(非敏感),而 SSH 凭据敏感,**必须先用 account_key seal 成密文**再走同步管线。

## 范围

### 做

- 主机清单(**非敏感字段 + 凭据整体**)打包为一个 JSON → 用 account_key `SealUnsequenced` 成密文 → 作为一个新的 prefssync value(key `ssh_hosts_encrypted`)同步
- relay 只看到密文;明文 SSHHost + 凭据永不离开本机未加密
- 多端 last-write-wins(复用 prefssync 现有冲突策略,按整个列表粒度)
- account_key 不可用(未登录 relay / 未启用 E2EE)时:**仅本地,不上传**(安全默认)
- 收到远端密文 → open → 写回本地 config.json(非敏感)+ keyring(凭据)
- 本地主机 CRUD(切片 2 的 Add/Update/Delete)触发 MarkDirty + Push

### 不做(明确后置)

- ❌ 单主机粒度冲突合并(整列表 LWW 已够用)
- ❌ 同步冲突的 UI 提示 / 手动合并(LWW 静默)
- ❌ known_hosts 同步(known_hosts 是本机安全边界,不同步)
- ❌ 端口转发 / 其它

### 关键边界决策(已与用户确认)

1. **整个主机清单整体 seal**:非敏感字段 + 凭据打包成一个 JSON,整体加密成一个 value。relay 只见密文,冲突按整列表 LWW。复用现有引擎,最直接。
2. **account_key 不可用时仅本地、不上传**:绝不明文外发凭据。启用 E2EE + 登录后自动开始同步。

## 加密方案

**seal/open 复用 `e2eecrypto`,但需要一个"虚拟 sessionID"**(现有 API 都以 session 为单位):

- 定义一个确定性常量 UUID `sshHostsSyncSessionID`(如从固定字符串派生的 namespace UUID,或硬编码一个固定 UUID 常量)。它不是真实会话,只用于给凭据同步派生独立的 sessionKey,并作为 AAD 绑定。
- `sessionKey = e2eecrypto.DeriveSessionKey(accountKey, sshHostsSyncSessionID)`
- 加密:`ciphertext = e2eecrypto.SealUnsequenced(sessionKey, sshHostsSyncSessionID, frameType, plaintextJSON)`
  - `frameType`:用一个不与真实帧冲突的常量(如 `0xF0`),仅作 AAD 绑定,不上 wire(不违反"不新增 proto 帧"——这不是 relay 协议帧,只是 seal 的 AAD 参数)。
- 解密:`plaintextJSON = e2eecrypto.OpenUnsequenced(sessionKey, sshHostsSyncSessionID, frameType, ciphertext)`

**同步 payload(seal 前的 plaintext JSON)**:

```go
// sshSyncPayload is the full host list (non-secret + credentials) serialized,
// sealed as one blob. Credentials travel inside the sealed blob only.
type sshSyncPayload struct {
	Hosts []sshSyncHost `json:"hosts"`
}
type sshSyncHost struct {
	Host SSHHost       `json:"host"`
	Cred sshCredential `json:"cred"`
}
```

**在 relay 上存的 value**:`ssh_hosts_encrypted` 的 value 是 `base64(ciphertext)` 的 JSON 字符串。relay 只见密文。

## 组件划分

### ① `internal/prefssync/sync.go` — 白名单加 key

`syncedKeys` 追加 `"ssh_hosts_encrypted"`。这是唯一需要动 internal 的地方(白名单),不改引擎逻辑。

### ② `desktop/ssh_sync.go`(新建)— seal/open 编解码

```go
// sshHostsSyncSessionID is the fixed virtual session UUID used to derive the
// key + bind the AAD for host-list sync. Not a real session.
var sshHostsSyncSessionID = uuid.MustParse("<fixed-uuid>")

const sshSyncFrameType = 0xF0 // AAD-only tag, never on the wire

// sealSSHHosts packs the host list + credentials and seals it. Returns
// (nil,nil) — signalling "skip sync" — when accountKey is empty.
func sealSSHHosts(accountKey []byte, hosts []SSHHost, creds map[string]sshCredential) (json.RawMessage, error)

// openSSHHosts decrypts a synced blob back into hosts + credentials.
func openSSHHosts(accountKey []byte, value json.RawMessage) ([]SSHHost, map[string]sshCredential, error)
```

### ③ `desktop/prefssync_adapter.go` — adapter 支持新 key

- adapter 需要访问 account_key。给 `appConfigAdapter` 注入一个 `accountKey func() []byte`(closure,读 `a.accountKey`,加锁)。App 构造 adapter 时传入。
- `ReadValue("ssh_hosts_encrypted")`:account_key 为空 → 返回 `(nil, false)`(不参与同步);否则读本地 SSHHosts + 从 keyring 取每主机凭据 → `sealSSHHosts` → 返回 base64 密文。
- `WriteValue("ssh_hosts_encrypted", value)`:account_key 为空 → 忽略(不写);否则 `openSSHHosts` → 写回 config.SSHHosts + 每主机凭据入 keyring。

### ④ `desktop/ssh_hosts_store.go`(切片 2 文件)— CRUD 触发同步

Add/Update/Delete 成功后调 `a.markPrefDirtyAndPush("ssh_hosts_encrypted")`(与 pinned_session_ids 一致),让改动 Push 到 relay。account_key 为空时 ReadValue 返回 false,Push 自动跳过(不上传)。

### ⑤ 前端

- 无需新 UI。切片 2 的 `SshHostsPanel` CRUD 已触发后端,同步透明发生。
- 可选:面板顶部显示"已同步 / 仅本地(未启用 E2EE)"状态提示(YAGNI:切片 3 不做,留给后续)。

## 数据流

```
本地 CRUD (切片2) → markPrefDirtyAndPush("ssh_hosts_encrypted")
   │
   ▼
prefssync.Push → adapter.ReadValue("ssh_hosts_encrypted")
   │  account_key 空? → 返回 false → 跳过(仅本地)
   │  否则: 读 config.SSHHosts + keyring 凭据 → sealSSHHosts → base64 密文
   ▼
relay.Put(密文)   ← relay 只见密文
   ...
其它设备 prefssync.Pull → adapter.WriteValue("ssh_hosts_encrypted", 密文)
   │  openSSHHosts → 写回 config.SSHHosts + keyring 凭据
   ▼
该设备 SshHostsPanel 显示同步来的主机
```

## 错误处理

| 场景 | 处理 |
|---|---|
| account_key 为空 | ReadValue 返回 (nil,false),Push 跳过该 key;WriteValue 忽略。不报错、不上传 |
| open 失败(密文损坏 / 密钥不匹配) | WriteValue 返回错误,该 key 本轮 Pull 失败并记录;不覆盖本地 |
| seal 失败 | ReadValue 返回 (nil,false),该轮不同步(不阻塞其它 key) |
| keyring 写凭据失败(Pull 落地时) | 返回错误,本地主机记录也不写(保持一致);下轮 Pull 重试 |

## 测试策略(TDD)

- **seal/open 往返**:`sealSSHHosts` → `openSSHHosts` 还原 hosts + creds;account_key 为空时 seal 返回 (nil,nil)。
- **AAD/密钥绑定**:错误 account_key open 失败;密文篡改 open 失败(复用 e2eecrypto 已有保证,这里只测集成)。
- **adapter 集成**:ReadValue 在有/无 account_key 下的行为;WriteValue open 后 config + keyring 都落地。
- **relay 只见密文**:断言 ReadValue 返回的 value 里不含明文 host/password 子串。
- **CRUD 触发 dirty**:Add/Update/Delete 后 `ssh_hosts_encrypted` 的 meta.Dirty=true。
- **不测**:真实多端 relay 往返(手动冒烟:两台设备验证同步)。

## 未决 / 待 review 确认点

1. `sshHostsSyncSessionID` 用硬编码固定 UUID(如 `ssh00000-...`)——需在实现时定一个确定值并注释来源。
2. LWW 粒度是整列表:两端同时改不同主机 → 后写覆盖先写(可能丢一端的改动)。切片 3 接受此限制(单主机粒度合并是明确后置项)。
3. 凭据 open 落地 keyring 时,若本地已有同 ID 凭据,直接覆盖(远端为准,符合 LWW)。
