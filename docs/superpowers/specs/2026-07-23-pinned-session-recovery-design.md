# 置顶会话跨重启保持 — design

Date: 2026-07-23
Status: Drafted — awaiting user review before plan.
See also: [2026-07-20 session-bar pin design](./2026-07-20-session-bar-pin-design.md)

## 0. Summary

`session-bar-pin` 已经支持右键把会话钉在侧栏顶部,pin 集(`PinnedSessionIDs`
`[]string`)按 session_id 存桌面 config。但 desktop 恢复流程里 **local pane
每次重启拿新 session_id**,老 pin id 指向的会话消失,新 sid 又没进 pin 集,
用户体感是"重启后置顶状态丢了"。remote pane 因为 session_id 由 relay 保
留、不变,所以那类会话置顶实际上是能保住的。

本设计只补 local pane 这一条链:让 recovery snapshot 记住上世代 pane 的
session_id,在 `executeRestore` spawn 出新 sid 后原子把 pin 集里的
`old_sid → new_sid` 换掉。不改协议、不改 relay、不改 pin UI。

## 1. Goals

- 用户在 v0.3-dev 里右键置顶的 local session:重启桌面后自动恢复的那个新
  pane 保持在置顶组里。
- Remote 置顶会话继续维持(现状已保,这次加测试锁死)。
- Recovery.json schema 向前兼容:老快照 / 缺失 `session_id` 的 pane 不崩、
  不误挂 pin。
- 实现限定在 `desktop/frontend/`(`useSessionPins` + `useRecoverySnapshot`
  + `App.vue::executeRestore`)三处,加一份 unit test。

## 2. Non-goals

- **不改 pin 主键语义**:仍然按 session_id 索引;不引入 (tab_id, slot)
  或内容签名(shell/cwd/ai_kind)作为 pin key(方案 B/C 已在 brainstorm
  被拒绝——见文档末 §7)。
- **不清理孤儿 pin id**:用户在旧版本 pin 过、这次没恢复(用户在 dialog
  里主动 discard 掉,或者上世代 pane 已经关闭)的 sid 保留在 config 里
  不动,沿用 `session-bar-pin-design.md` §4.7 的语义。
- **不做跨端 pin 同步**:mobile / web 不受影响,pin 仍只在桌面本地。
- **不改 recovery dialog UI**:不加"置顶"标签、不预勾选置顶 tab;pin 迁
  移完全由用户在 dialog 里点了恢复的 pane 触发。用户选择 discard 掉某个
  tab,那这些 pane 的 pin 就是孤儿(等下次再启动同一 session_id 时——不
  会发生,local sid 只用一次——它自然废掉)。
- **不做 pin metadata 缓存**:pin 只存 id,不缓存 shell/cwd/ai_kind
  副本用于兜底(那是方案 B)。

## 3. 现状

关键源:

- `desktop/frontend/src/composables/useRecoverySnapshot.ts:60-83` — 构造
  `RecoveryPaneSnapshot`。只有 remote 且 host 匹配远端时才把 `session_id`
  写进 snapshot(`persistAsRemote && p.sessionId`);local pane 一律
  `session_id: undefined`。
- `desktop/frontend/src/App.vue:834-915` — `executeRestore`。remote 分支
  用 `snap.session_id` 直接 rebind;local 分支走 `newSession(req)` 拿新
  `resp.session_id`,写进 `t.panes[i].sessionId`,seed 进 `localList` 让
  UI 立刻见。这里是唯一能拿到 `old_sid → new_sid` 映射的窗口。
- `desktop/frontend/src/composables/useSessionPins.ts`(已存在,由
  `session-bar-pin-design.md` 落地) — module-level `pinnedIds:
  Ref<Set<string>>` + `pin/unpin/toggle` + 300ms debounce persist 到
  `SetPinnedSessionIds`。目前**没有** `rename` 操作。
- `desktop/recovery_types.go` — `PaneSnapshot.SessionID string
  \`json:"session_id,omitempty"\`` 字段本来就有,只是被 buildSnapshot
  条件性置空,不需要改 Go 侧 schema。

Session_id 生成:local pane 通过 `App.NewSession` → 相当于走桌面内嵌 mini
relay(`desktop/relay_host.go`)发一个新 UUID。同一 pane 每次 spawn 都是
新 UUID,与上世代无关。

Remote pane:session_id 由外部 relay 拥有,只要 relay 在、session 未
close,重启桌面客户端后仍能拿到同一 sid 并 rebind → 该情形下 pin 无缝。

## 4. 设计

### 4.1 Snapshot:local pane 也写 session_id

`useRecoverySnapshot.ts::buildSnapshot` 里,把 `session_id` 的赋值改成
"只要 pane 有 sid 就写":

```ts
// before
session_id: persistAsRemote && p.sessionId ? p.sessionId : undefined,

// after
session_id: p.sessionId || undefined,
```

`remote` / `host_id` 的写入规则不变(仍受 `persistAsRemote` 门控)。
`executeRestore` 里的 `if (snap.remote && snap.session_id) {...}` 分支
判定 remote-rebind,不受影响——多写的 local sid 不会误触发 rebind
分支,因为 `remote` 是 false。

字段语义变化(需在 spec 注释里落):

- `remote: true` 的 pane,`session_id` = 用于 rebind 的权威 id。
- `remote: false` 的 pane,`session_id` = **上世代** local sid,仅用于
  pin 迁移;不做任何其它用途(尤其不要拿去 rebind——那 sid 已死)。

`recovery.json` 体积影响:每个 local pane 多 ~50 字节(UUID 字符串 +
JSON 引号 + 键名),量级可忽略。

### 4.2 `useSessionPins.rename(old, new)`

在 `useSessionPins.ts` 里新增一个 `rename` 方法:

```ts
function renameFn(oldId: string, newId: string): void {
  if (!oldId || !newId || oldId === newId) return;
  if (!pinnedIds.value.has(oldId)) return;
  const next = new Set(pinnedIds.value);
  next.delete(oldId);
  next.add(newId);
  pinnedIds.value = next;
  schedulePersist();
}
```

同时暴露一个 `flushNow()`,让 `executeRestore` 在收尾时能立即落盘,不
等 300ms debounce:

```ts
async function flushNowFn(): Promise<void> {
  if (flushHandle) {
    clearTimeout(flushHandle);
    flushHandle = null;
  }
  await setPinnedSessionIds(Array.from(pinnedIds.value));
}
```

`UseSessionPins` interface 加两个字段:

```ts
export interface UseSessionPins {
  pinnedIds: Ref<Set<string>>;
  isPinned: (id: string) => boolean;
  pin: (id: string) => void;
  unpin: (id: string) => void;
  toggle: (id: string) => void;
  rename: (oldId: string, newId: string) => void;     // NEW
  flushNow: () => Promise<void>;                      // NEW
}
```

`__resetForTests` 已有,不动。

### 4.3 `executeRestore` 里的迁移点

`App.vue::executeRestore` 里 local spawn 成功那一行(约 line 877)之后
插入 pin 迁移;remote 分支不动:

```ts
// existing
const pins = useSessionPins();

// ...

if (snap.remote && snap.session_id) {
  // existing remote rebind unchanged. sid 未变，pin 自动保住。
  ...
  continue;
}
try {
  const oldSid = snap.session_id || "";     // 新增: 上世代 id (§4.1)
  const dims = predictCellDims(tab.layout);
  const req = buildRestoreSessionReq(snap, dims.cols, dims.rows, defaultShell);
  const resp = await newSession(req);
  t.panes[i] = { sessionId: resp.session_id, remote: false };
  // ... 现有 pendingLocalIds + localList seed ...

  // 新增: pin 迁移
  if (oldSid && pins.isPinned(oldSid)) {
    pins.rename(oldSid, resp.session_id);
  }
} catch (e) { ... }
```

collateral:`executeRestore` 结尾在切完 `gotoTab` 之后 `await
pins.flushNow()`,确保用户第一次打开侧栏时 pin 集已经是新 sid,不用等
300ms。

remote 分支不需要 flush(sid 未变、pin 集本来就没被动过)。

### 4.4 UI / 别处

- `TaskSidebar` / `TaskGroupedList`:零改动。pin 集通过响应式 Set 变
  更传导,置顶组自动重新计算。
- `SessionRowMenu`:零改动。
- Go 侧 `PinnedSessionIDs` / `Get/SetPinnedSessionIds`:零改动。写入路
  径已经是 dedup + drop empty(见 pin-design §4.2)。
- 协议帧 / relay:零改动。

### 4.5 边界

| 情形 | 行为 |
| --- | --- |
| Local pane 恢复,`snap.session_id ∈ pin` | rename → 新 sid pinned |
| Local pane 恢复,`snap.session_id ∉ pin` | 不映射,新 sid 未 pin |
| Remote pane 恢复(sid 不变) | remote 分支在 rename 之前 `continue`;不调用 rename,pin 集不变(sid 未变、天然仍在) |
| `snap.session_id` 缺失(旧 recovery.json / 异常快照) | 跳过映射,新 sid 未 pin;不崩 |
| 用户 discard 掉某 tab | 该 tab 的 pane 未 spawn,老 pin id 成孤儿(保留,兼容 pin-design §4.7 "不清理脏 id") |
| Spawn 失败(catch 分支) | `t.panes[i] = { sessionId: null }`,不做 rename;老 pin id 成孤儿 |
| Rapid restart(300ms debounce 未落盘时崩) | 最差丢一次 rename;老 sid 停留 pin 集、新 sid 未加入 → 用户可手动重 pin |
| `rename(old, new)`,`new` 已经在 pin 集里 | del old + add new(add 是幂等),Set 天然不重复 |
| 用户在 recovery 期间打开侧栏 | pins.flushNow() 前是"新 sid 已在 in-memory Set",UI 立刻反映;persist 延迟对 UI 不可见 |

## 5. Test plan

**新增/扩展 unit test:**

- `desktop/frontend/src/composables/useSessionPins.test.ts`
  - `rename(a, b)`:pin 集 `{a}` → `{b}`,触发一次 debounce persist
  - `rename(a, b)`,`a ∉ pin`:no-op,不触发 persist
  - `rename(a, a)`:no-op
  - `rename(a, b)`,`b ∈ pin`:`{a, b}` → `{b}`(不重复)
  - `flushNow()`:清空 debounce 定时器,立即调 `setPinnedSessionIds`;
    多次调 `pin` 后 `flushNow` 只有一次调用
- `desktop/frontend/src/composables/useRecoverySnapshot.test.ts`
  - Local pane(non-null sessionId、非 remote):snapshot 也写
    `session_id`,不写 `remote` / `host_id`
  - Remote pane:仍然写 `remote: true` + `session_id` + `host_id`(regression 锁死)
- `desktop/frontend/src/App.vue` 里 `executeRestore` 目前有没有 vitest
  覆盖需要确认;没有则新增 `App.recovery.test.ts` 或补进现有 App test:
  - Local pane 恢复:`snap.session_id ∈ pin` → 结束后 `pins.isPinned(newSid) === true`
  - Local pane 恢复:`snap.session_id ∉ pin` → 结束后 `pins.isPinned(newSid) === false`
  - Remote pane 恢复:sid 不变,pin 集不变
  - `snap.session_id` 缺失:不崩、不映射
  - Spawn 失败:不 rename、不误清 pin

**Go 侧:**

- 无新增 test。`recovery_store_test.go` 无 schema 变化(字段本来就有,
  只是从"有时候写空"变成"更常写非空")。可以补一条读写用例专门锁
  `Remote=false && SessionID != ""` 的合法性。

**手测清单(plan 阶段展开):**

- Pin 一个远程 session(比如另一台机器上的 claude)+ 一个本机 local
  shell + 一个本机 `claude` pane → 关闭桌面 app → 重开 → 走 recovery
  dialog "全选恢复" → 三个都出现在 📌 置顶组
- 只 pin 一个 local shell → 重启 → dialog 里 discard 该 tab → 侧栏无
  该 pane,config 里老 sid 依然存在(孤儿,不影响使用)
- 同一次 restart 中 pin 5 个 → 观察 recovery.json 是否有 5 个 pane 都
  带 `session_id`,SetPinnedSessionIds 只落一次盘(flushNow 收尾)
- 关掉 unclean(force quit):恢复流程走一遍,pin 迁移正常

## 6. Rollout notes

- 无迁移:schema 本身没变,只是 local pane 从"不写 session_id"变成"写
  session_id";老 recovery.json 反序列化下 local pane 的 session_id 是
  空,`executeRestore` 里 `oldSid || ""` fallback 到跳过迁移分支,等价
  于旧行为。
- 无向后兼容担忧(memory `feedback_no_backward_compat.md`):直接落。
- 不涉及 relay / mobile / web / 协议 / release CI 变更。

## 7. Open questions / 已拒绝的方案

_None open._

Brainstorm 阶段拒绝的替代方案(留档以防后来人再问):

- **方案 B: 内容签名匹配 (shell + cwd + ai_kind)** —— 不改 snapshot、
  用 pin metadata 缓存重新匹配。拒绝理由:同目录跑两个 shell 会命中错;
  pin 存储结构从 `[]string` 升级为对象数组,前端 model / Go 侧
  Get/Set 全体波及,收益不成比例。
- **方案 C: 改 pin 主键为 (tab_snapshot_id, slot)** —— 拒绝理由:sidebar
  里不在任何 pane 的 session 就没法 pin 了;跟红线 #3 "session_id 是权
  威"冲突;右键菜单 / rail / host header 上一堆逻辑要重写。
