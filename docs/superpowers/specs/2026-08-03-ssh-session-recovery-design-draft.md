# SSH 会话恢复设计

> **Status**: draft
> **Date**: 2026-08-03
> **Depends on**: 切片 1(连接内核)、切片 2(主机清单)、密钥库
> **See also**: `desktop/recovery_types.go` · `desktop/frontend/src/composables/useRecoverySnapshot.ts` · `desktop/frontend/src/App.vue::executeRestore` · `internal/proto/frame.go`

## 背景

atterm 崩溃/重启后能从 `recovery.json` 恢复会话。但 SSH 会话(切片 1 经 `AdoptSession` 接入,`SessionInfo.HostID = 本机 host_id`,前端 pane `remote: false`)在恢复机制里被**误当本地 shell**:

- 快照生成(`useRecoverySnapshot`):`persistAsRemote = p.remote && info.host_id !== localHostID` → SSH 会话 `remote===false` → 存成本地 shell,`shell: info.command.split(" ")[0]` = `"ssh"`
- 恢复重建(`executeRestore`):走本地 spawn → `newSession({ command: "ssh" })` → **fork 一个裸 `ssh` 进程**(无参数,打印 usage 退出),而非重建 SSH 连接

结果:SSH 会话没被真正恢复,连接信息全丢。

## 目标(已与用户确认)

- **从主机清单连的 SSH 会话**(`NewSshSessionByID`,有对应 SSHHost.ID):恢复时按 host_id `NewSshSessionByID` **重连**(会重走 TOFU / 取凭据)
- **即席连接的 SSH 会话**(`NewSshDialog` 粘密码/私钥,无保存主机):无主机可重连 → 恢复时 **pane 置空并提示"需重新连接"**
- **标识方式**:给 `proto.SessionInfo` 加可选 `ssh_host_id` 字段(omitempty,向后兼容,不破现有帧),快照据此区分

## 非目标

- ❌ 恢复即席会话的连接(凭据用完即弃,无法重连)
- ❌ 自动处理 TOFU/2FA(重连会正常走 TOFU 弹框,和首次连一样)

## 数据流

```
连接: NewSshSessionByID(hostID) → OpenSSHSession(req, cb, sshHostID=host.ID)
        → SessionInfo.SSHHostID = host.ID  (即席连接 sshHostID="")
快照: useRecoverySnapshot 读 info.ssh_host_id → PaneSnapshot.ssh_host_id
恢复: executeRestore 遇到 pane:
        ssh_host_id 非空 → NewSshSessionByID(ssh_host_id) 重连
        ssh_host_id 为空 但 是 SSH 会话 → 置空 pane + 提示
        否则(普通本地 shell)→ 现有 spawn 路径不变
```

## 组件改动(4 层)

### ① `internal/proto/frame.go` — SessionInfo 加字段

```go
// SSHHostID, when non-empty, marks this session as an SSH remote shell
// connected from a saved host (SSHHost.ID). Empty for local shells and for
// ad-hoc SSH connections. Used by recovery to reconnect by host id.
SSHHostID string `json:"ssh_host_id,omitempty"`
```

- omitempty → 旧 publisher 不发、旧 client 忽略,向后兼容(红线 4:不改现有字段结构,只加可选字段)。

### ② `desktop/ssh_host.go` — OpenSSHSession 收 host id 并设上

```go
func (h *relayHost) OpenSSHSession(ctx, req SSHConnectReq, hostKeyCb ssh.HostKeyCallback, sshHostID string) (uuid.UUID, error)
// info.SSHHostID = sshHostID
```

- `NewSshSessionByID(id)`:调用时传 `sshHostID = id`(该会话来自保存的主机)。
- `NewSshSession`(即席):传 `sshHostID = ""`(无保存主机)。
- 需要一个"标记这是 SSH 会话但无 host id"的方式,区分即席 SSH 与本地 shell。用 SSHHostID 无法区分(都空)。**方案**:即席 SSH 也要能被识别为 SSH → 见下"即席 SSH 识别"。

### 即席 SSH 识别

即席 SSH 会话 `SSHHostID=""`,和本地 shell 一样。要让恢复知道"这是个即席 SSH(该置空提示)而非本地 shell(该 respawn)",需另一个信号。**复用现有 `Command` 字段**:SSH 会话的 `Command`/`Title` 是 `"ssh user@host"`,以 `"ssh "` 前缀开头。恢复时:

- `ssh_host_id` 非空 → 保存主机的 SSH → 重连
- `ssh_host_id` 空 且 `command` 以 `"ssh "` 开头 → 即席 SSH → 置空提示
- 否则 → 本地 shell → 现有 respawn

> 用 command 前缀判断即席 SSH 是启发式,但足够:本地 shell 的 command 是 shell 路径(bash/zsh/...),不会以 `"ssh "` 开头;即席 SSH 的 Title 恒为 `"ssh user@host"`(切片 1 固定格式)。

### ③ 前端 SessionInfo + PaneSnapshot 加字段

- `connection.ts::SessionInfo` 加 `ssh_host_id?: string`
- `recovery.ts`(RecoveryPaneSnapshot 类型)加 `ssh_host_id?: string`
- `useRecoverySnapshot.ts`:`ssh_host_id: info?.ssh_host_id ?? undefined`

### ④ `App.vue::executeRestore` — 识别 SSH pane

在本地 spawn 分支前插入 SSH 判断:

```
if (snap 是 SSH — ssh_host_id 非空 或 shell/command 以 "ssh" 开头) {
  if (snap.ssh_host_id) {
    // 保存主机 → 重连
    resp = await newSshSessionByID(snap.ssh_host_id)
    t.panes[i] = { sessionId: resp.session_id, remote: false }
    seed localList
  } else {
    // 即席 SSH → 无法重连,置空提示
    t.panes[i] = { sessionId: null, remote: false, lastSeenInfo: {需重新连接提示} }
  }
  continue
}
// 否则走现有本地 shell respawn
```

- 重连失败(host 已删 / errKeyMissing / TOFU 拒绝):catch → 置空 pane + 错误提示(不阻塞其它 pane 恢复)。

## 错误处理

| 场景 | 处理 |
|---|---|
| ssh_host_id 非空但主机已删 | NewSshSessionByID 返回 "no such host" → catch → 置空 + 提示 |
| ssh_host_id 非空但 key 已删 | errKeyMissing → catch → 置空 + 提示 |
| 重连触发 TOFU(未知主机) | 返回 HostKeyUnknownError → 恢复时视为失败置空(恢复不弹 TOFU,提示用户手动重连)|
| 即席 SSH | 置空 + "SSH 会话已断开,请重新连接" |
| 快照无 ssh_host_id(旧快照)| command 前缀判断即席 SSH,置空;非 ssh 前缀走本地 respawn(旧行为)|

## 测试策略(TDD)

- **desktop**:`OpenSSHSession` 设 SSHHostID;`NewSshSessionByID` 传入 host.ID → SessionInfo.SSHHostID == host.ID;即席 `NewSshSession` → SSHHostID 空。
- **前端 useRecoverySnapshot**:SSH 会话(有 ssh_host_id)→ 快照 pane 带 ssh_host_id;即席 SSH(command "ssh ...", 无 ssh_host_id)→ 快照记录可识别。
- **前端 executeRestore**(纯函数化恢复决策):
  - ssh_host_id 非空 → 调 newSshSessionByID
  - 即席 SSH → 置空提示,不调 newSession
  - 本地 shell → 走 newSession(现有)
  - 重连失败 → 置空
- **不测**:真实崩溃恢复(手动冒烟)。

## 未决 / 待 review 确认点

1. 即席 SSH 用 `command` 以 `"ssh "` 前缀识别 —— 依赖切片 1 的 Title 固定格式 `"ssh user@host"`。若未来改格式需同步。
2. 恢复重连不自动接受 TOFU(安全):未知主机指纹变化时恢复失败置空,提示手动重连,而非静默接受。
