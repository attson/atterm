# 会话栏多实例区分 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在桌面会话栏里，给同一台机器上多个 atterm 实例的 host 组追加 `#1 / #2 / …` 后缀，按 host_id 字典序统一编号，使 dev + 正式版（或未来多 profile）从 relay 同时露面时可以区分。

**Architecture:** 纯前端实现。`sessionLabel.ts` 新增两个纯函数：`coResidentIndex(byHost, localHost)` 计算每个本机 host_id 的编号，`hostNameWithIndex(...)` 把编号叠到现有显示名上。`TaskGroupedList.vue` 接受新 prop `localHost`、用 computed 调用上述函数、改 `groupHeader()`。`TaskSidebar.vue` 透传 prop。`App.vue` 在已有 `getHostInfo()` 调用后多存一个 `localHost = info.host` 并向下传递。

**Tech Stack:** Vue 3 + TypeScript + Vitest + @vue/test-utils（已有）。无后端/协议改动。

**Spec:** `docs/superpowers/specs/2026-06-26-session-bar-multi-instance-distinction-design.md`

---

## File Map

| File | 作用 | 改动 |
|---|---|---|
| `desktop/frontend/src/lib/sessionLabel.ts` | 共享 session 标签辅助函数 | 新增 `coResidentIndex` + `hostNameWithIndex` |
| `desktop/frontend/src/lib/sessionLabel.test.ts` | 上述辅助的单测 | 追加两个 describe 块 |
| `desktop/frontend/src/components/TaskGroupedList.vue` | 按 host/state 分组渲染会话栏 | 新增 `localHost` prop + `coResidentMap` computed + 改 `groupHeader()` 调用 |
| `desktop/frontend/src/components/TaskGroupedList.test.ts` | 组件测试 | 追加多实例编号断言 |
| `desktop/frontend/src/components/TaskSidebar.vue` | 会话栏外壳 | 透传 `localHost` |
| `desktop/frontend/src/App.vue` | 顶层装配 | 保存 `info.host` 到 `localHost` ref + 向下传 |

---

### Task 1: 实现 `coResidentIndex` 纯函数

**Files:**
- Modify: `desktop/frontend/src/lib/sessionLabel.ts` (末尾追加)
- Modify: `desktop/frontend/src/lib/sessionLabel.test.ts` (追加 describe 块)

- [ ] **Step 1: 写失败的单测**

把下面这段追加到 `desktop/frontend/src/lib/sessionLabel.test.ts` 文件末尾（同时把 `coResidentIndex` 加入顶部的 import）：

```ts
describe('sessionLabel.coResidentIndex', () => {
  it('returns an empty map when localHost is empty', () => {
    const byHost = { 'h-a': [{ host: 'mac' }], 'h-b': [{ host: 'mac' }] }
    expect(coResidentIndex(byHost, '').size).toBe(0)
  })
  it('returns an empty map when only one local host_id is present', () => {
    const byHost = { 'h-a': [{ host: 'mac' }], 'remote': [{ host: 'other' }] }
    const out = coResidentIndex(byHost, 'mac')
    expect(out.size).toBe(0)
  })
  it('numbers two local host_ids in lexicographic order', () => {
    const byHost = {
      'h-b': [{ host: 'mac' }],
      'h-a': [{ host: 'mac' }],
    }
    const out = coResidentIndex(byHost, 'mac')
    expect(out.get('h-a')).toBe(1)
    expect(out.get('h-b')).toBe(2)
  })
  it('numbers three local host_ids and excludes remote ones', () => {
    const byHost = {
      'h-c': [{ host: 'mac' }],
      'h-a': [{ host: 'mac' }],
      'h-b': [{ host: 'mac' }],
      'remote-1': [{ host: 'other' }],
    }
    const out = coResidentIndex(byHost, 'mac')
    expect(out.size).toBe(3)
    expect(out.get('h-a')).toBe(1)
    expect(out.get('h-b')).toBe(2)
    expect(out.get('h-c')).toBe(3)
    expect(out.has('remote-1')).toBe(false)
  })
  it('ignores entries with empty session list', () => {
    const byHost: Record<string, { host?: string }[]> = {
      'h-a': [],
      'h-b': [{ host: 'mac' }],
    }
    // h-a has no session → unknown host → not counted as local
    expect(coResidentIndex(byHost, 'mac').size).toBe(0)
  })
})
```

并修改 import 头部：

```ts
import {
  aiTitleOrCommand,
  commandLabel,
  fullCommand,
  rowTitle,
  hostName,
  coResidentIndex,  // 新增
  taskStateLabel,
} from './sessionLabel'
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd desktop/frontend && pnpm vitest run src/lib/sessionLabel.test.ts`

Expected: 4 个新 case 全部失败（`coResidentIndex is not a function` 类错误，因为还没实现）。

- [ ] **Step 3: 实现 `coResidentIndex`**

在 `desktop/frontend/src/lib/sessionLabel.ts` 末尾追加：

```ts
// coResidentIndex assigns 1-based numbers to host_ids that physically live on
// the same machine as the caller (`localHost` = this machine's OS hostname).
// Returns an empty map when localHost is empty or when fewer than 2 local
// host_ids exist — callers render the bare host name in those cases.
// Order is lexicographic by host_id so every window sees the same numbering.
export function coResidentIndex(
  byHost: Record<string, { host?: string }[]>,
  localHost: string,
): Map<string, number> {
  if (!localHost) return new Map()
  const localHostIds: string[] = []
  for (const hid of Object.keys(byHost)) {
    const list = byHost[hid]
    if (list && list.length > 0 && (list[0]?.host ?? '') === localHost) {
      localHostIds.push(hid)
    }
  }
  if (localHostIds.length < 2) return new Map()
  localHostIds.sort()
  const out = new Map<string, number>()
  localHostIds.forEach((hid, i) => out.set(hid, i + 1))
  return out
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd desktop/frontend && pnpm vitest run src/lib/sessionLabel.test.ts`

Expected: 全绿，包括原有 case 和新 5 个 case。

- [ ] **Step 5: 提交**

```bash
git add desktop/frontend/src/lib/sessionLabel.ts desktop/frontend/src/lib/sessionLabel.test.ts
git commit -m "feat(sessionLabel): add coResidentIndex helper"
```

---

### Task 2: 实现 `hostNameWithIndex` 纯函数

**Files:**
- Modify: `desktop/frontend/src/lib/sessionLabel.ts` (末尾追加)
- Modify: `desktop/frontend/src/lib/sessionLabel.test.ts` (追加 describe 块)

- [ ] **Step 1: 写失败的单测**

把下面这段追加到 `desktop/frontend/src/lib/sessionLabel.test.ts`（同时把 `hostNameWithIndex` 加进顶部 import）：

```ts
describe('sessionLabel.hostNameWithIndex', () => {
  it('returns the base name when index is undefined', () => {
    expect(hostNameWithIndex('h-1', [{ host: 'mac' }], 'unknown', undefined))
      .toBe('mac')
  })
  it('returns the base name when index is 0 (treated as absent)', () => {
    // 0 is not a valid 1-based index; treat as no suffix.
    expect(hostNameWithIndex('h-1', [{ host: 'mac' }], 'unknown', 0))
      .toBe('mac')
  })
  it('appends "#N" suffix when index >= 1', () => {
    expect(hostNameWithIndex('h-1', [{ host: 'mac' }], 'unknown', 1))
      .toBe('mac #1')
    expect(hostNameWithIndex('h-2', [{ host: 'mac' }], 'unknown', 2))
      .toBe('mac #2')
  })
  it('appends suffix even when falling back to host_id', () => {
    expect(hostNameWithIndex('h-1', [], 'unknown', 1)).toBe('h-1 #1')
  })
})
```

并修改 import：

```ts
import {
  aiTitleOrCommand,
  commandLabel,
  fullCommand,
  rowTitle,
  hostName,
  hostNameWithIndex,  // 新增
  coResidentIndex,
  taskStateLabel,
} from './sessionLabel'
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd desktop/frontend && pnpm vitest run src/lib/sessionLabel.test.ts`

Expected: 新 4 个 case 失败（`hostNameWithIndex is not a function`）。

- [ ] **Step 3: 实现 `hostNameWithIndex`**

在 `desktop/frontend/src/lib/sessionLabel.ts` 末尾（紧跟 `coResidentIndex` 之后）追加：

```ts
// hostNameWithIndex wraps hostName() and appends a "#N" co-residence suffix
// when an index is provided. Index 0 / undefined → no suffix (the helper
// stays no-op for the single-instance default).
export function hostNameWithIndex(
  hostId: string,
  list: { host?: string }[] | undefined,
  unknownHost: string,
  index: number | undefined,
): string {
  const base = hostName(hostId, list, unknownHost)
  return index && index >= 1 ? `${base} #${index}` : base
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd desktop/frontend && pnpm vitest run src/lib/sessionLabel.test.ts`

Expected: 全绿。

- [ ] **Step 5: 提交**

```bash
git add desktop/frontend/src/lib/sessionLabel.ts desktop/frontend/src/lib/sessionLabel.test.ts
git commit -m "feat(sessionLabel): add hostNameWithIndex helper"
```

---

### Task 3: TaskGroupedList 接入 `localHost` + 渲染编号

**Files:**
- Modify: `desktop/frontend/src/components/TaskGroupedList.vue`
- Modify: `desktop/frontend/src/components/TaskGroupedList.test.ts`

- [ ] **Step 1: 写失败的组件测试**

把下面追加到 `desktop/frontend/src/components/TaskGroupedList.test.ts` 文件末尾（在最后一个 `describe` 之外）：

```ts
describe("TaskGroupedList co-resident numbering", () => {
  test("appends #N to local-host groups when 2+ host_ids share the local hostname", () => {
    // Two atterm instances on the same Mac: host_id 'h-a' (dev) and 'h-b' (prod).
    // The current process is 'h-b' (prod). Expect 'mac #1' / 'mac #2' headers.
    const byHost = {
      "h-b": [mk({ session_id: "s1", host: "mac" })],
      "h-a": [mk({ session_id: "s2", host: "mac" })],
      "remote-x": [mk({ session_id: "s3", host: "other-mac" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        localHostId: "h-b",
        localHost: "mac",
      },
    });
    const headers = w.findAll('[data-test="host-header"]');
    // localHostId is pinned to top: h-b (#2) first.
    expect(headers[0].text()).toContain("mac #2");
    expect(headers[1].text()).toContain("mac #1");
    expect(headers[2].text()).toContain("other-mac");
    // Remote group does NOT get a #N suffix.
    expect(headers[2].text()).not.toContain("#");
  });

  test("no #N suffix when only one local host_id is present", () => {
    const byHost = {
      "h-a": [mk({ session_id: "s1", host: "mac" })],
      "remote": [mk({ session_id: "s2", host: "other" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        localHostId: "h-a",
        localHost: "mac",
      },
    });
    const headers = w.findAll('[data-test="host-header"]');
    expect(headers[0].text()).toContain("mac");
    expect(headers[0].text()).not.toContain("#");
  });

  test("'this machine' chip stays on current-process group only, even with #N suffix", () => {
    const byHost = {
      "h-a": [mk({ session_id: "s1", host: "mac" })],
      "h-b": [mk({ session_id: "s2", host: "mac" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        localHostId: "h-b",
        localHost: "mac",
      },
    });
    const chips = w.findAll('[data-test="local-chip"]');
    expect(chips.length).toBe(1);
  });

  test("remote machines with multiple host_ids are NOT renumbered", () => {
    const byHost = {
      "h-local": [mk({ session_id: "l", host: "my-mac" })],
      "h-r1": [mk({ session_id: "r1", host: "other-mac" })],
      "h-r2": [mk({ session_id: "r2", host: "other-mac" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        localHostId: "h-local",
        localHost: "my-mac",
      },
    });
    const headers = w.findAll('[data-test="host-header"]');
    // No headers should contain "#" — local has only 1 local host_id; remote
    // collisions are intentionally not disambiguated.
    for (const h of headers) {
      expect(h.text()).not.toContain("#");
    }
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd desktop/frontend && pnpm vitest run src/components/TaskGroupedList.test.ts`

Expected: 4 个新 case 失败 — 头部不含 `mac #1` / `mac #2`（组件还没接 `localHost` prop，也没改 `groupHeader()`）。

- [ ] **Step 3: 修改 TaskGroupedList.vue —— 加 prop**

在 `desktop/frontend/src/components/TaskGroupedList.vue` 的 props 声明里，把现有：

```vue
  localHostId?: string;
}>(), {
  groupBy: "host",
  byState: () => ({}),
  unreadByState: () => ({}),
  activeSessionId: null,
  localHostId: "",
});
```

改成：

```vue
  localHostId?: string;
  // Local OS hostname (from GetHostInfo). When ≥2 host_ids in byHost share
  // this hostname, those groups get a "#N" suffix (lex order by host_id) so
  // dev + prod (or future profiles) on the same machine become distinguishable.
  // Empty string disables the suffix entirely.
  localHost?: string;
}>(), {
  groupBy: "host",
  byState: () => ({}),
  unreadByState: () => ({}),
  activeSessionId: null,
  localHostId: "",
  localHost: "",
});
```

- [ ] **Step 4: 修改 TaskGroupedList.vue —— import 新增辅助 + 加 computed**

把现有 import：

```vue
import { aiTitleOrCommand, rowTitle, hostName as hostNameHelper, taskStateLabel } from "../lib/sessionLabel";
```

替换为：

```vue
import {
  aiTitleOrCommand,
  rowTitle,
  hostName as hostNameHelper,
  hostNameWithIndex,
  coResidentIndex,
  taskStateLabel,
} from "../lib/sessionLabel";
```

然后找到现有的 `hostName(hostId: string)` 函数定义（约 line 99）：

```vue
function hostName(hostId: string): string {
  return hostNameHelper(hostId, groups.value[hostId], t("sessions.unknownHost"));
}
```

在它前面添加 computed：

```vue
const coResidentMap = computed<Map<string, number>>(() =>
  coResidentIndex(props.byHost, props.localHost ?? ""),
);
```

然后把 `hostName` 改成：

```vue
function hostName(hostId: string): string {
  return hostNameWithIndex(
    hostId,
    groups.value[hostId],
    t("sessions.unknownHost"),
    coResidentMap.value.get(hostId),
  );
}
```

注意：`groupHeader()` 调用的是 `hostName()`（host 分组路径），所以改 `hostName` 一处就够；state 分组路径走 `stateLabel`，不受影响。

- [ ] **Step 5: 运行测试确认通过**

Run: `cd desktop/frontend && pnpm vitest run src/components/TaskGroupedList.test.ts`

Expected: 全绿（4 个新 case + 原有 case）。

- [ ] **Step 6: 提交**

```bash
git add desktop/frontend/src/components/TaskGroupedList.vue desktop/frontend/src/components/TaskGroupedList.test.ts
git commit -m "feat(task-sidebar): co-resident #N suffix in TaskGroupedList"
```

---

### Task 4: TaskSidebar 透传 `localHost`

**Files:**
- Modify: `desktop/frontend/src/components/TaskSidebar.vue`
- Modify: `desktop/frontend/src/components/TaskSidebar.test.ts`

- [ ] **Step 1: 写一个透传断言（失败）**

把下面追加到 `desktop/frontend/src/components/TaskSidebar.test.ts` 文件末尾（如果文件已有 describe，就在最后一个之外再起一个 describe）：

```ts
import { describe as describePassthrough, test as testPassthrough, expect as expectPassthrough } from "vitest";
import { mount as mountPassthrough } from "@vue/test-utils";
import TaskSidebarComp from "./TaskSidebar.vue";

describePassthrough("TaskSidebar localHost passthrough", () => {
  testPassthrough("renders #N suffix on co-resident headers when localHost set", () => {
    const w = mountPassthrough(TaskSidebarComp, {
      props: {
        collapsed: false,
        byHost: {
          "h-a": [{
            session_id: "s1", host_id: "h-a", host: "mac", user: "u",
            title: "", cols: 80, rows: 24,
          }],
          "h-b": [{
            session_id: "s2", host_id: "h-b", host: "mac", user: "u",
            title: "", cols: 80, rows: 24,
          }],
        },
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        totalUnread: 0,
        localHostId: "h-a",
        localHost: "mac",
      },
    });
    const headers = w.findAll('[data-test="host-header"]');
    const texts = headers.map((h) => h.text());
    expectPassthrough(texts.some((t) => t.includes("mac #1"))).toBe(true);
    expectPassthrough(texts.some((t) => t.includes("mac #2"))).toBe(true);
  });
});
```

> 这里用别名 import 是为了不和文件原有的 vitest/test-utils import 重复冲突；TS 不在意名字。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd desktop/frontend && pnpm vitest run src/components/TaskSidebar.test.ts`

Expected: 新 case 失败 —— TaskSidebar 还没接 `localHost` prop，往下传不到 TaskGroupedList。

- [ ] **Step 3: 修改 TaskSidebar.vue —— 加 prop**

把现有 props 块（约 line 14-32）：

```vue
const props = withDefaults(defineProps<{
  collapsed: boolean;
  byHost: Record<string, RemoteSession[]>;
  unreadByHost: Record<string, number>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
  totalUnread: number;
  byStateGroups?: Record<string, RemoteSession[]>;
  unreadByStateGroups?: Record<string, number>;
  activeSessionId?: string | null;
  // Pinned to the top of the host group list and tagged with a "本机"
  // chip in the header. Forwarded as-is to TaskGroupedList.
  localHostId?: string;
}>(), {
  byStateGroups: () => ({}),
  unreadByStateGroups: () => ({}),
  activeSessionId: null,
  localHostId: "",
});
```

改成：

```vue
const props = withDefaults(defineProps<{
  collapsed: boolean;
  byHost: Record<string, RemoteSession[]>;
  unreadByHost: Record<string, number>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
  totalUnread: number;
  byStateGroups?: Record<string, RemoteSession[]>;
  unreadByStateGroups?: Record<string, number>;
  activeSessionId?: string | null;
  // Pinned to the top of the host group list and tagged with a "本机"
  // chip in the header. Forwarded as-is to TaskGroupedList.
  localHostId?: string;
  // Local OS hostname; forwarded so TaskGroupedList can render "#N"
  // suffixes when multiple atterm instances on this machine share it.
  localHost?: string;
}>(), {
  byStateGroups: () => ({}),
  unreadByStateGroups: () => ({}),
  activeSessionId: null,
  localHostId: "",
  localHost: "",
});
```

- [ ] **Step 4: 修改 TaskSidebar.vue 模板 —— 透传给 TaskGroupedList**

找到 `<TaskGroupedList>` 标签（约 line 187-199），把现有的：

```vue
        <TaskGroupedList
          :by-host="byHost"
          :unread-by-host="unreadByHost"
          :primary-state-for-host="primaryStateForHost"
          :completed-seen="completedSeen"
          :group-by="groupByState.activeId.value"
          :by-state="byStateGroups"
          :unread-by-state="unreadByStateGroups"
          :active-session-id="activeSessionId"
          :local-host-id="localHostId"
          @open="(s) => emit('open', s)"
          @markSeen="(p) => emit('markSeen', p)"
        />
```

改成（多加一行 `:local-host="localHost"`）：

```vue
        <TaskGroupedList
          :by-host="byHost"
          :unread-by-host="unreadByHost"
          :primary-state-for-host="primaryStateForHost"
          :completed-seen="completedSeen"
          :group-by="groupByState.activeId.value"
          :by-state="byStateGroups"
          :unread-by-state="unreadByStateGroups"
          :active-session-id="activeSessionId"
          :local-host-id="localHostId"
          :local-host="localHost"
          @open="(s) => emit('open', s)"
          @markSeen="(p) => emit('markSeen', p)"
        />
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd desktop/frontend && pnpm vitest run src/components/TaskSidebar.test.ts`

Expected: 全绿。

- [ ] **Step 6: 提交**

```bash
git add desktop/frontend/src/components/TaskSidebar.vue desktop/frontend/src/components/TaskSidebar.test.ts
git commit -m "feat(task-sidebar): forward localHost to TaskGroupedList"
```

---

### Task 5: App.vue 捕获并向下传 `localHost`

**Files:**
- Modify: `desktop/frontend/src/App.vue`

- [ ] **Step 1: 加 ref**

找到 `localHostID` 的 ref 声明（约 line 103）：

```vue
const localHostID = ref<string>("");
```

紧随其后插入一行：

```vue
const localHost = ref<string>("");
```

- [ ] **Step 2: 在 bootStage="getHostInfo" 里保存 host**

找到 `getHostInfo()` 调用处（约 line 1069-1071）：

```ts
    bootStage = "getHostInfo";
    const info = await getHostInfo();
    localHostID.value = info.host_id;
```

改成（多加一行赋值）：

```ts
    bootStage = "getHostInfo";
    const info = await getHostInfo();
    localHostID.value = info.host_id;
    localHost.value = info.host;
```

- [ ] **Step 3: 模板里把 prop 传给 TaskSidebar**

找到 `<TaskSidebar>` 用法（约 line 1175-1190），定位到 `:local-host-id="localHostID"` 那一行（应为 line 1186），在它后面新增一行：

```vue
        :local-host-id="localHostID"
        :local-host="localHost"
```

- [ ] **Step 4: 运行整个前端测试套件确认无回归**

Run: `cd desktop/frontend && pnpm vitest run`

Expected: 全绿。如果有跟 App.vue 相关的快照测试失败，确认是新增 prop 引起的合法变化后更新快照（`pnpm vitest run -u`）。

- [ ] **Step 5: 类型检查**

Run: `cd desktop/frontend && pnpm tsc --noEmit`

Expected: 无类型错误。

- [ ] **Step 6: 提交**

```bash
git add desktop/frontend/src/App.vue
git commit -m "feat(app): wire localHost from getHostInfo to TaskSidebar"
```

---

### Task 6: 人肉验证 dev + prod 并存场景

**Files:** 无文件改动；这是行为验证 + 决定是否需要后续修复。

- [ ] **Step 1: 启动正式版**

按当前常规方式启动正式版 atterm（已编译的 .app 或 `make` 产物），并登录 relay。开一两个 session。

- [ ] **Step 2: 启动 dev 实例**

另开终端：

```bash
cd /Users/attson/code/github.com.attson/atterm
make dev   # 或当前项目的 dev 启动命令
```

dev 实例登录到同一个 relay 账户（账号密码相同）。

- [ ] **Step 3: 在正式版窗口检查会话栏**

- 自己进程那组应该带 "本机" chip + "#N" 后缀（N 取决于 host_id 字典序）。
- 另一组（dev）应该是同 hostname + 另一个 # 编号，**不**带 "本机" chip。
- 两组的总数（chip 旁边那个数字）应分别匹配各自实例真实开的 session 数。

- [ ] **Step 4: 在 dev 窗口检查会话栏**

- 应该看到与正式版窗口**完全一致**的两组编号（按 host_id 字典序，跨窗口一致）。
- "本机" chip 这次应在 dev 那组上。

- [ ] **Step 5: 关 dev → 验证编号回退**

关闭 dev 窗口。等待正式版会话栏刷新（10 秒内）。

- 正式版那组应回到 "本机"（无 #N 后缀）。

- [ ] **Step 6: 记录截图（可选但推荐）**

把验证结果（特别是 step 3/4 的两窗口对照）截图存到 `docs/superpowers/specs/2026-06-26-session-bar-multi-instance-distinction-design.md` 同目录或 PR 描述里。

- [ ] **Step 7: 若一切符合预期，无需新提交**

如果发现 bug：定位到具体 task 修复，按 TDD 补一个 reproduce 测试再修。

---

## Self-Review 已做

- **Spec coverage**：spec 的 §4.2（编号算法）→ Task 1；§4.3（hostNameWithIndex + 渲染）→ Task 2/3；§5（数据流）→ Task 4/5；§7（测试）→ Task 1/2/3/4 的 Step 1。
- **Placeholder scan**：所有 Step 都给了完整代码或具体命令，无 TBD/TODO。
- **Type consistency**：`coResidentIndex(byHost, localHost) → Map<string, number>` 和 `hostNameWithIndex(hostId, list, unknownHost, index)` 签名在 Task 1/2/3 间一致；prop 名 `localHost` 在 Task 3/4/5 一致。
- **不动**：spec §8 列出的"不动"清单（协议、relay、后端 Go、移动端、recovery）在所有 task 里都没被修改。
