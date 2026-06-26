# 会话栏多实例区分 — design

Date: 2026-06-26
Status: Drafted — awaiting user review before plan.

## 0. Summary

同一台机器上跑多个 atterm 实例（今天主要是 dev + 正式版，未来可能扩展为多 profile）时，
两个实例都登录 relay 后，会话栏里会出现两个 host 组——但因为它们共用同一个 OS hostname
（如 `attson-mbp`），组标题撞车，用户无法分辨哪一组属于哪一个实例。

本设计在会话栏的本机相关组上叠加 `#1 / #2 / …` 编号后缀，规则简单且跨窗口一致：
**对本机 hostname 下的所有 host_id 按字典序赋号**。"本机"chip 仍只挂在当前进程那一组上，
编号后缀表达"物理同机但不同实例"。

## 1. Goals

- 本机 hostname 下出现 ≥2 个不同 host_id 时，给这些组标题加 `#N` 后缀。
- 编号规则：按 host_id 字典序，1-based。跨窗口一致：dev 和正式版两个窗口看到的同一实例显示相同编号。
- 当前进程那组仍是唯一带"本机"chip 的组；后缀负责区分同机其他实例。
- 单实例时维持现状（无 `#N` 后缀，无视觉变化）。
- 实现限定在桌面前端，不涉及协议、relay、后端 Go 改动（`GetHostInfo` 已返回 hostname）。

## 2. Non-goals

- 远端机器同样跑多实例时**不**做编号消歧（远端 hostname 重复继续按现状显示）。
- 移动端会话列表不变。
- 不为多实例引入手工命名 / profile 名 / 变体标签。"编号"是唯一显式区分手段。
- 不引入持久化的"槽位"概念：编号是 byHost 状态的纯函数，状态变化时编号可变。
- 不解决"两个进程共享同一数据目录"的误用场景（那需要单例锁，与本特性正交）。
- 不为未来 iTerm2 风格多窗口（一进程多 Wails 窗口）做任何特殊处理——那种场景下 host_id 仍是一个，自然不触发编号。

## 3. 现状

- `internal/hostid/hostid.go`：`host_id` 是按数据目录持久化的 UUID。dev 通过
  `appdir.UseDev()` 切到独立目录，自动生成不同 host_id；正式版用 `<UserConfigDir>/atterm/host_id`。
- `internal/proto/frame.go`：`AnnouncePayload` 和 `SessionInfo` 携带 `HostID`、`Host`（OS hostname）、`User`。
- `desktop/frontend/src/platform/types.ts:79`：`RemoteSession` 暴露 `host_id` 与 `host`。
- `desktop/frontend/src/components/TaskGroupedList.vue`：会话栏按 `host_id` 分组渲染。
  - `localHostId` prop（从 `GetHostInfo` 读取的本机 host_id）触发：
    - 该组排序顶到最上（line 67-73）
    - 该组渲染 "本机" chip（line 154-161）
  - `hostName(hostId)` → `sessionLabel.hostName()` 返回 `list[0].host || hostId || unknownHost`——
    多组撞同一个 `host` 时全部显示同一字符串，无法区分。
- `desktop/frontend/src/components/TaskSidebar.vue`：把 `localHostId` 透传到 `TaskGroupedList`。
- `desktop/frontend/src/lib/sessionLabel.ts`：导出 `hostName(hostId, list, unknownHost)`，纯函数，已有测试覆盖位置。
- `desktop/app.go:406` `GetHostInfo()` 返回 `HostInfo { HostID, Host, User }`——hostname 已经在结果里。
- `desktop/frontend/src/App.vue:1069-1071`：当前调用 `getHostInfo()` 只取了 `info.host_id`，`info.host` 被丢弃。本次要把它也存下来并向下透传。
- `desktop/frontend/src/components/TerminalView.vue:689-690` 已独立调用 `getHostInfo()` 取 hostname 用于 ClaimDriver。属于另一条调用链，本次不动。

## 4. 设计

### 4.1 触发条件

- 设本机 hostname 为 `H_local`（从 `GetHostInfo()` 拿到）。
- 对 `byHost: Record<hostId, RemoteSession[]>` 做：收集所有 `byHost[hid][0].host === H_local` 的 hostId。
- 当集合 size ≥ 2 → 触发编号；否则不触发，组标题维持现状。
- 远端机器（`host !== H_local`）即便多 host_id 撞名也不参与编号——保持组件分支简单。

### 4.2 编号算法

```ts
// 输入：byHost（已有），H_local（新增 prop）
// 输出：Map<hostId, number>，仅对本机 hostname 下的 hostId 赋值
function coResidentIndex(
  byHost: Record<string, { host?: string }[]>,
  localHost: string,
): Map<string, number> {
  if (!localHost) return new Map()
  const localHostIds = Object.keys(byHost).filter(
    (hid) => (byHost[hid][0]?.host ?? "") === localHost,
  )
  if (localHostIds.length < 2) return new Map()
  localHostIds.sort() // 字典序，1-based 编号
  const out = new Map<string, number>()
  localHostIds.forEach((hid, i) => out.set(hid, i + 1))
  return out
}
```

- 纯函数、无副作用、易测。
- 复杂度 O(n log n)（仅本机 hostId 子集），byHost 总长一般 < 20，无性能担忧。
- byHost 抖动（uplink 上下线）→ 编号跟着重算；用户已接受。

### 4.3 渲染改动

`sessionLabel.ts` 新增：

```ts
export function hostNameWithIndex(
  hostId: string,
  list: { host?: string }[] | undefined,
  unknownHost: string,
  coResidentIndex: number | undefined,
): string {
  const base = hostName(hostId, list, unknownHost)
  return coResidentIndex ? `${base} #${coResidentIndex}` : base
}
```

`TaskGroupedList.vue`：

- 新增 prop `localHost: string`（默认 `""`）。
- 新增 computed `coResidentMap`：调用 `coResidentIndex(byHost, localHost)`。
- 改 `groupHeader(key)`：当 `groupBy === 'host'` 时改用 `hostNameWithIndex(key, groups.value[key], t('sessions.unknownHost'), coResidentMap.value.get(key))`；`groupBy === 'state'` 路径不变。

`TaskSidebar.vue`：新增 `localHost: string` prop，原样透传给 `TaskGroupedList`。

`App.vue`：
- 新增 `localHost = ref<string>("")`，在调用 `getHostInfo()` 那处补一行 `localHost.value = info.host`（line ~1071）。
- 模板里把 `:local-host="localHost"` 一起传给 `<TaskSidebar>`（line ~1186）。

### 4.4 "本机" chip 行为

不变——仍然只挂在 `key === localHostId` 的组上。理由：
- chip 当前语义 = "当前 atterm 进程的会话"；扩到"任何同机进程"会让 dev 调试时
  chip 跳到正式版组上，反而误导。
- "本机 #N" 后缀已经表达了"物理同机"。

### 4.5 排序

不变。`groupKeys` 现有逻辑：本机组（localHostId）顶上，其余按字典序。
编号后缀不影响排序键。

## 5. 数据流

```
GetHostInfo (Go) → { hostId, host }
  ↓ 通过 lib/api 暴露
App.vue 持有 localHostId, localHost
  ↓ props
TaskSidebar { localHostId, localHost }
  ↓ props
TaskGroupedList { byHost, localHostId, localHost }
  ↓ computed
coResidentMap: Map<hostId, number>
  ↓ groupHeader(key)
hostNameWithIndex → "本机 #1" / "本机 #2" / "本机"
```

## 6. 渲染样例

场景 1：仅正式版（无 dev、无远端）
```
▼ 本机 [本机]    3
```

场景 2：dev + 正式版（同机），无远端
```
▼ 本机 #2 [本机]    3   ← 当前进程 = 正式版，host_id 字典序排第二
▼ 本机 #1           2   ← dev
```

场景 3：dev + 正式版 + 远端
```
▼ 本机 #2 [本机]    3
▼ 本机 #1           2
▼ another-mac       1
```

场景 4：远端机器自己也跑了两个实例（同 hostname、两个 host_id）——按 non-goal **不消歧**：
```
▼ 本机 [本机]       3
▼ another-mac       2   ← 两组同名，不加 #N，按现状显示
▼ another-mac       1
```

## 7. 测试要点

`sessionLabel.test.ts`（新增 `coResidentIndex` 单测）：
- 空 byHost → 空 Map
- 单本机 host_id → 空 Map（不触发）
- 双本机 host_id → 按字典序 1, 2
- 三本机 + 一远端 → 仅三个本机分到 1, 2, 3；远端不在 Map 中
- `localHost` 为空字符串 → 空 Map（早返回）
- host_id 顺序传入打乱 → 输出顺序仍按字典序

`TaskGroupedList.test.ts`（扩展）：
- 单实例：组标题不带 #N
- 双实例：两组分别 "本机 #1" / "本机 #2"，且只有 localHostId 那组有"本机"chip
- 远端多实例：标题维持现状，无 #N

## 8. 不动 / 显式排除

- **协议**：`AnnouncePayload` / `SessionInfo` / `MetaPayload` 完全不动。
- **relay**：完全不动。
- **桌面后端 Go**：完全不动。`GetHostInfo` 已经返回 hostname。
- **恢复路径**：`recovery.json` 按数据目录隔离，本特性不触及。
- **移动端**：不变。
- **iTerm2 风格多窗口**（一进程多窗口）：未来引入时仍是单 host_id，不会触发本特性的编号逻辑；二者正交。
- **持久化编号槽位**：不做。编号是当前 byHost 的纯函数。
- **手工命名 / profile 标签**：不做。仅用数字。
- **单例锁防止两个正式版进程共享数据目录**：不做（正交问题）。

## 9. 验收

- 同时开 dev 与正式版、两边都登录 relay：两个窗口的会话栏中，dev 组显示 "本机 #1"，正式版组显示 "本机 #2"（按 host_id 字典序）。
- "本机" chip 只挂在各自当前进程的那一组。
- 关掉 dev → 正式版那组在下次刷新后变回 "本机"（无后缀）。
- 远端机器若也跑多实例：组名维持现状，不出现 #N。
