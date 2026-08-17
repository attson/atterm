# 会话配置档 profile（P5 第 22 项）— design

Date: 2026-08-17
Status: Drafted — awaiting user review before plan.
Parent: [2026-08-16 sync-layer roadmap](./2026-08-16-sync-layer-roadmap-design.md) §5 P5 第 22 项 · roadmap 第 22 项
See also: [2026-08-17 prefs-sync L1](./2026-08-17-prefs-sync-l1-design.md)（同步管线与首登语义）· [2026-08-17 terminal appearance](./2026-08-17-terminal-appearance-design.md)

## 0. Summary

引入 **profile**：一组命名的会话启动配置——shell、启动目录、环境变量、启动命令。
新建 tab / split 时可以选一个 profile，也可以设一个默认 profile。

这一项一次性关掉 roadmap Backlog 里的三条（「默认 shell 设置改进」「启动目录设置」
「环境变量设置」），因为这三者本来就是同一个东西的三个字段。

profile 是本阶段第一个 **L2 sealed** 内容物：环境变量可能含 token，所以整个 profile 走
`account_key` 信封，relay 读不到。这也是本项唯一需要新建机制的地方——L1 那条明文管线不适用。

## 1. Goals

- 定义、编辑、删除 profile；每个 profile 有名称、shell、启动目录、环境变量、启动命令。
- 新建 tab / split 时选 profile；可设默认 profile。
- profile 跨桌面设备同步，且 relay 在密码学上读不到内容。
- **环境变量默认不离开本机**，用户显式开启才同步。

## 2. Non-Goals

- 不做运行中 pane 的 profile 切换（改 profile 不影响已开会话，只影响新建的）。
- 不做 per-profile 外观覆盖（字体/主题仍是全局，第 20 项）。profile 只管「怎么起这个会话」。
- 不做 profile 级的 SSH 主机绑定——SSH 会话有自己的创建路径（`ssh_host.go`），第 25–29 项的范围。
- 不动 web / 移动端。同第 20/21 项：这些是桌面专属配置，relay 上有密文但 web 端不读。

## 3. 跨进程注册点清单

第 21 项最严重的缺陷（relay 白名单缺九个键，功能整条链路不通，四轮评审全过）根源是
**所有枚举都停在桌面进程边界内**。所以本设计先把注册点列全，再谈实现：

| # | 注册点 | 本项要做什么 | 有无自动守卫 |
|---|---|---|---|
| 1 | `internal/prefssync/syncedKeys` | 加 `profiles_encrypted` | — |
| 2 | relay `internal/userstore/allowedPreferenceKeys` | 加 `profiles_encrypted`（kind: string） | ✅ `preferences_synced_keys_test.go` 跨包断言（第 21 项补的） |
| 3 | `desktop/prefssync_adapter.go` `ReadValue`/`WriteValue` | 加 case，seal/open | — |
| 4 | `desktop/app.go` `isPrefCustomized` | 加判据（`len(Profiles) > 0`） | — |
| 5 | **AAD tag 分配** | 新 tag `0xF1`，登记进 `docs/spec/protocol.md` §E2EE 信封表 | ❌ **无守卫，见 §6.2** |
| 6 | `AGENTS.md` 红线 #22（AAD 字节分配表） | 补一行 | ❌ 无守卫 |
| 7 | 两份 TS 镜像（`desktop/frontend/src/lib/prefsSync.ts`、`web/src/shared/sync/prefsSync.ts`） | **不加**，理由同 `ssh_hosts_encrypted`：桌面专属键，web/Capacitor 的 `pull()` 按服务端条目写、不查 `SYNCED_KEYS`，且那两端无法把此键标脏 | ✅ drift 测试断言 TS ⊆ Go（第 21 项补的） |

第 2 项现在有守卫，是第 21 项那次事故的直接产物——本项如果漏了它，测试会红而不是静默 400。
第 5、6 项**没有守卫**，是本项新的同类风险，见 §6.2。

## 4. 数据模型

```go
type SessionProfile struct {
    ID        string            `json:"id"`         // uuid，创建时生成，同步与引用都按它
    Name      string            `json:"name"`
    Shell     string            `json:"shell,omitempty"`       // 空 = 沿用全局 default_shell
    Cwd       string            `json:"cwd,omitempty"`         // 空 = 沿用现有行为（HOME）
    StartupCmd string           `json:"startup_cmd,omitempty"` // 起 shell 后注入的第一条命令
    Env       map[string]string `json:"env,omitempty"`
    SyncEnv   bool              `json:"sync_env,omitempty"`    // 默认 false = env 不出本机
}
```

`appConfig` 加 `Profiles []SessionProfile` 与 `DefaultProfileID string`。

**优先级**：新建会话时，若指定了 profile 用它；否则若设了默认 profile 用默认的；
否则沿用今天的行为（`default_shell` + HOME）。`default_shell`（第 21 项接的同步键）保留不动，
它是「没有 profile 时」的回退，不是被 profile 取代。这一条要在 UI 上说清楚，
否则用户会同时看到两个 shell 设置而不知道谁生效。

## 5. 同步：sealed，且 env 默认留在本机

复用 `desktop/ssh_sync.go` 的形状——那是仓库里唯一跑通的 sealed 偏好同步：
固定虚拟 session UUID 派生 key，`SealUnsequenced` + 独立 AAD tag，`accountKey` 为空时
返回 `(nil, nil)` 让调用方跳过同步而不是发明文。

新 AAD tag：**`0xF1`**（`0xF0` 已被 SSH 同步占用）。同样只绑 AAD、不上 relay wire。

### 5.1 env 的合并规则——本设计最容易出错的一处

`SyncEnv` 默认 false，意味着 seal 之前要把该 profile 的 `Env` 抹掉。但这带来一个问题：
B 机 pull 到一个没有 env 的 profile 之后，如果直接整体替换本地 profile，
**B 机自己为这个 profile 配的本地 env 就没了**。

所以 pull 侧不能整体替换，必须按 profile id 合并：

- 入站 profile 的 `Env` 非空 → 采用入站的（说明源机开了 `SyncEnv`）。
- 入站 profile 的 `Env` 为空 **且** 本地同 id profile 有 env → **保留本地 env**。
- 本地没有该 id → 直接新增（env 为空）。
- 本地有、入站没有该 id → 删除（profile 确实被删了）。

这条规则的效果是：**env 只会被显式同步的 env 覆盖，永远不会被「没同步 env」这件事清空**。
代价是「用户在 A 机删掉了某个 profile 的 env」这个动作传不到 B 机——可接受，
因为 env 本来就被定义为本机私有；要清空 B 机的 env，在 B 机操作。

### 5.2 不做的事

不做「env 单独一个键」。那会让 profile 与其 env 分成两个 LWW 键，出现「profile 已删但 env 还在」
之类的不一致状态。合并规则复杂一点，好过两个键互相不一致。

同理，`DefaultProfileID` 也不单独开一个 sealed 键，而是跟 `Profiles` 一起塞进同一个
`profilesSyncPayload{ Profiles, DefaultProfileID }`，共用 `0xF1`（见
`desktop/profiles.go` `profilesSyncPayload` 的注释）。从 prefssync 的角度看，
「有哪些 profile」和「哪个是默认的」是同一条用户可见偏好的两个字段，不是两条偏好——
拆成两个键除了多一个 AAD tag 之外没有任何好处，反而引入一个新的时序问题：
两个键各自 LWW、各自可能先后到达，B 机就可能先 pull 到「默认 profile = X」，
却还没 pull 到 X 本身（或者 X 已经在 A 机被删了但删除事件还没同步过来）——
出现一个指向不存在 profile 的悬空默认值。

绑在同一个键里天然避免了「default 先于 profile 到达」这一半的问题（两者原子地一起到达），
但没有避免「引用了一个已被过滤掉的 profile」——`sealProfiles`/`openProfiles` 只负责整体
加解密，不校验 `DefaultProfileID` 是否确实命中 `Profiles` 里的某一项（`filterValidProfiles`
会丢弃畸形/重复 id 的条目，丢弃后原本合法的 `DefaultProfileID` 就可能变成悬空引用）。
这一步校验放在入站合并路径上：`resolveDefaultProfileID(id, profiles)` 在
`filterValidProfiles` 之后运行，`id` 不命中任何现存 profile 时一律返回 `""`，
而不是把悬空 id 存进本地 config。前端（`SettingsProfiles.vue` 的 `deleteProfile()`）
在本机删除当前默认 profile 时做了同样的事——本地立即清空默认值，
而不是等下一轮 pull 由 Go 侧纠正——这是同一条不变量在两个方向（出站 —— 本机编辑；
入站 —— 远端同步）上的对称实现，理由都是「默认值只能指向一个当下存在的 profile，
不存在就是没有默认值，而不是一个悬空指针」。

## 6. 风险

### 6.1 startup 命令注入的时机与形态

`StartupCmd` 要在 shell 起来、第一个提示符出现之后写进 PTY。仓库里已有这条路径——
AI 会话恢复用 `desktop/relay_host.go::SetOnFirstPrompt` 在首个提示符时直接写 PTY，
**并且明确不许走前端 `sendInput` 一次发完整的 `"<cmd>\r"`**：Codex 会把 CR 当 paste 解，
这个教训在 PR #63 → #110 → #129 来回踩了三次（红线 #28）。`StartupCmd` 必须复用同一条路径，
不要新开一个「前端发命令」的实现。

### 6.2 新 AAD tag 没有自动守卫

`0xF1` 的分配、`protocol.md` 的表、AGENTS.md 红线 #22 三处，靠人记。红线 #22 的原话是
「加新 sealed 帧类型时**必须**给新 frame_type 一个唯一字节，否则别处的信封能被替换重放」——
也就是说漏登记的后果是密码学的，不是文档的。

缓解：加一条 Go 测试，把「代码里所有 AAD tag 常量」与「protocol.md 表里列出的字节」比对。
本项要新建这条守卫（第 21 项的跨包测试是同一形状，已证明有效）。
如果实现时发现 tag 常量散落各处无法枚举，就先把它们集中到一个包再加守卫——
那本身也是值得做的清理。

### 6.3 环境变量会覆盖 atterm 自己设的终端变量

`relay_host.go:493` 现在用 `terminalEnvForXterm(os.Environ())`，它设了 `TERM` 等。
profile 的 env 若包含 `TERM`，会破坏终端渲染。缓解：profile env 与基础 env 合并时，
profile 优先，但 `TERM` 列入保护名单不允许覆盖；UI 上对被保护的键给出提示。

### 6.4 profile 数量增长导致 sealed blob 变大

单个键的 LWW，整块 profile 列表一起传。几十个 profile 仍在几十 KB 量级，
与 `ssh_hosts_encrypted` 同量级，不需要分片。

## 7. 验证

- `SessionProfile` 的 seal/open 往返测试，与 `ssh_sync_test.go` 同构；`accountKey` 为空时不产生网络写入。
- **env 合并规则四条分支各一条测试**（§5.1）——这是本设计最容易写错的地方。
- AAD tag 守卫测试（§6.2）：代码常量与 `protocol.md` 表一致。
- 优先级测试：指定 profile > 默认 profile > `default_shell` + HOME。
- `TERM` 保护：profile env 含 `TERM` 时不生效且有提示。
- 跨包白名单测试会自动覆盖注册点 #2（无需新写）。
- 手动：建一个 profile 指定 shell + cwd + startup 命令，新开 tab 验证三者都生效；
  两台机器验证 profile 同步、且未开 `SyncEnv` 的 env 没有离开本机。

## 8. 与母 spec 的差异

母 spec §5 第 22 项把 profile 描述为「sealed 同步」，与本设计一致。母 spec §6 风险 1 要求
「per-profile 不同步 env 的开关，默认不同步」，本设计实现为 `SyncEnv bool` 并补齐了
母 spec 未涉及的合并规则（§5.1）——没有合并规则的话，「默认不同步」会退化成「pull 一次就清空本地 env」。
