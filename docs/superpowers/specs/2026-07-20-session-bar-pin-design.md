# 会话栏置顶 — design

Date: 2026-07-20
Status: Drafted — awaiting user review before plan.

## 0. Summary

左侧会话栏（`TaskSidebar` → `TaskGroupedList`）目前把所有 remote sessions 按 host 或
state 分组渲染，重要的长任务和琐碎会话混在一起，用户在 20+ 会话时容易找不到关心的那一个。

本设计新增「置顶」能力：用户在会话行右键菜单里选「置顶」后，该会话从原分组抽出，聚合到
列表最顶部的一个虚拟组「📌 置顶」里；再次右键选「取消置顶」即回到原分组。置顶集合按
`session_id` 存桌面本地 config（跟 `TaskSidebarWidth` 同源），不跨端同步，session_id 全局唯一
不会误伤。

## 1. Goals

- 单个会话粒度：右键 → 「置顶 / 取消置顶」切换。
- 置顶会话聚合到顶部一个虚拟组，**从原 host / state 分组中移除**，两种分组模式下都生效。
- 置顶组内部按现有 urgency 顺序（`waiting_input → failed → running → completed → idle → disconnected`）排序，跟其他组一致。
- 置顶组可折叠（沿用 `useCollapsedGroups`），空态（当前 `pinnedIds ∩ sessions = ∅`）不渲染。
- 持久化到桌面 config（`~/.config/atterm/config.json`），跟 `task_sidebar_width` 同存储。
- 实现限定在桌面前端 + `desktop/app.go` 两处；不涉及协议、relay、mobile、web。

## 2. Non-goals

- **不跨端同步**：手机 / 浏览器 web 端不显示置顶（不改 relay preferences）。
- **不做整个 host 组的置顶**：粒度就是单会话；host 组置顶留给后续需求，本次不设计。
- **不做数量上限**：置顶多少个都行。UI 靠用户自控。
- **不做键盘快捷键 / hover 图钉**：本次只做右键菜单入口。
- **不清理脏 id**：session 断连/删除后 pinned id 保留，不主动扫；session 回来自动重现，永久删除后 id 无副作用（不会渲染）。
- **不做置顶组自身的排序方案切换**（不给用户选"按置顶时间"）。
- **不动折叠 rail 的 railIcons**：折叠状态下 rail 图标按现有 urgency 序显示前 20 个，不特殊对待置顶。用户请求里没有"折叠时也要突出置顶"的诉求。

## 3. 现状

- `desktop/frontend/src/components/TaskSidebar.vue` — 左侧栏容器；处理宽度拖拽、折叠/展开、按 group-by 切换。透传 `byHost` / `byStateGroups` 等 props 给 `TaskGroupedList`。
- `desktop/frontend/src/components/TaskGroupedList.vue:200-235` — 渲染每个 `.task-row`。目前只有 `@click="emit('open', s)"` 和一个 `.row-mark-read` 按钮；**没有 `@contextmenu`**。
- `desktop/frontend/src/composables/useTaskGroupBy.ts` / `useTaskPreset.ts` / `useCollapsedGroups.ts` — module-level `ref` + Wails Getter/Setter 的组合式模式，本次沿用。
- `desktop/config.go:170-190` — `appConfig` struct 已有 `TaskSidebarWidth`, `TaskSidebarCollapsed`, `TaskPreset`, `TaskGroupBy` 等桌面本地字段，本次加一条 `PinnedSessionIDs []string`。
- `desktop/app.go:1290-1320` — `Get/SetTaskSidebarWidth` 是本次 Go 侧新方法的模板。
- `desktop/frontend/src/lib/api.ts:380-901` — Wails bindings + wrapper functions 的位置。
- `desktop/frontend/src/i18n/messages/{en,zh}.ts` — 加新 i18n key 的位置。

session 唯一 id 是 `RemoteSession.session_id`（UUID，全局唯一，session close 后不复用）。已经被 `useCollapsedGroups`、`mark-read`、`active session` 等多处当 stable 主键使用。

## 4. 设计

### 4.1 数据存储

`desktop/config.go` 里 `appConfig` 新增字段：

```go
// PinnedSessionIDs holds session_ids the user has pinned to the top of
// the session bar. Order in the slice is not meaningful — the frontend
// sorts pinned rows by task_state urgency, same as other groups. Stale
// ids (session gone) are kept in place: they render as nothing while
// absent and reappear if the session returns; the frontend never
// deletes ids the user did not explicitly unpin.
PinnedSessionIDs []string `json:"pinned_session_ids,omitempty"`
```

不加 helper（不像 `TaskSidebarWidthOrDefault` 那样，因为空 slice 就是正确默认）。

### 4.2 Go 侧 API

`desktop/app.go` 增加两个方法：

```go
func (a *App) GetPinnedSessionIds() []string {
    if a.cfgStore == nil {
        return nil
    }
    ids := a.cfgStore.Get().PinnedSessionIDs
    if ids == nil {
        return []string{}   // 前端稳态：始终 array，不返回 null
    }
    return ids
}

func (a *App) SetPinnedSessionIds(ids []string) error {
    if a.cfgStore == nil {
        return nil
    }
    // Dedup + drop empties. Order preserved (frontend passes canonical order).
    seen := make(map[string]struct{}, len(ids))
    out := make([]string, 0, len(ids))
    for _, id := range ids {
        if id == "" {
            continue
        }
        if _, ok := seen[id]; ok {
            continue
        }
        seen[id] = struct{}{}
        out = append(out, id)
    }
    cfg := a.cfgStore.Get()
    cfg.PinnedSessionIDs = out
    return a.cfgStore.Set(cfg)
}
```

Wails 会自动把它俩绑进 `App` 接口；对应 `desktop/frontend/src/lib/api.ts` 的 `bindings()` interface + `getPinnedSessionIds()` / `setPinnedSessionIds()` wrapper。

### 4.3 前端 composable

`desktop/frontend/src/composables/useSessionPins.ts`（新文件）沿用 `useCollapsedGroups` 模式，
用 module-level `ref` 保证多组件共享同一状态：

```ts
import { ref } from "vue";
import { getPinnedSessionIds, setPinnedSessionIds } from "../lib/api";

const pinnedIds = ref<Set<string>>(new Set());
let loaded = false;
let flushHandle: ReturnType<typeof setTimeout> | null = null;

async function loadOnce() {
  if (loaded) return;
  loaded = true;
  try {
    const list = await getPinnedSessionIds();
    pinnedIds.value = new Set(list);
  } catch {
    /* best-effort — leave empty */
  }
}

function schedulePersist() {
  if (flushHandle) clearTimeout(flushHandle);
  flushHandle = setTimeout(() => {
    flushHandle = null;
    void setPinnedSessionIds(Array.from(pinnedIds.value));
  }, 300);
}

export interface UseSessionPins {
  pinnedIds: typeof pinnedIds;
  isPinned: (id: string) => boolean;
  pin: (id: string) => void;
  unpin: (id: string) => void;
  toggle: (id: string) => void;
}

function pinFn(id: string): void {
  if (!id || pinnedIds.value.has(id)) return;
  const next = new Set(pinnedIds.value);
  next.add(id);
  pinnedIds.value = next;
  schedulePersist();
}

function unpinFn(id: string): void {
  if (!pinnedIds.value.has(id)) return;
  const next = new Set(pinnedIds.value);
  next.delete(id);
  pinnedIds.value = next;
  schedulePersist();
}

export function useSessionPins(): UseSessionPins {
  void loadOnce();
  return {
    pinnedIds,
    isPinned: (id) => pinnedIds.value.has(id),
    pin: pinFn,
    unpin: unpinFn,
    toggle: (id) => (pinnedIds.value.has(id) ? unpinFn(id) : pinFn(id)),
  };
}

// Test helper: reset module state between tests.
export function __resetForTests(): void {
  pinnedIds.value = new Set();
  loaded = false;
  if (flushHandle) {
    clearTimeout(flushHandle);
    flushHandle = null;
  }
}
```

（真实实现里 `toggle` 直接调用同一闭包内的 `pin/unpin`；spec 里为可读性简化。）

**Debounce 300ms** 是为了防止用户 rapid-fire toggle 时对 config file 造成大量写。跟 `setTaskSidebarWidth` 的拖拽结束落盘不一样，这里 toggle 是离散事件但可能连打。

### 4.4 渲染改动

**`TaskGroupedList.vue`**：

1. 引入 `const pins = useSessionPins()`。
2. 计算 `const pinnedSessions = computed(() => allSessions().filter(s => pins.isPinned(s.session_id)))`，`allSessions()` 从 `props.byHost` 或 `props.byState`（取决于 groupBy）扁平化。
3. 现有的按组迭代（`groups.value[key]`）在遍历时用 `.filter(s => !pins.isPinned(s.session_id))` 过滤掉已置顶的。
4. 在模板顶部（groups 之前）加一段：

```vue
<template v-if="pinnedSessions.length > 0">
  <div
    class="group-header pinned-group"
    data-test="pinned-group-header"
    @click="toggleGroupCollapsed('__pinned__')"
  >
    <span class="pin-icon">📌</span>
    <span class="group-title">{{ t("tasks.pinned.title") }}</span>
    <span class="group-count">{{ pinnedSessions.length }}</span>
  </div>
  <template v-if="!isGroupCollapsed('__pinned__')">
    <button
      v-for="s in sortedPinned"
      :key="s.session_id"
      class="task-row"
      :class="{ active: s.session_id === activeSessionId }"
      data-test="task-row"
      @click="emit('open', s)"
      @contextmenu.prevent="onRowMenu($event, s)"
    >
      <!-- 复用现有 row 的插槽内容 -->
    </button>
  </template>
</template>
```

其中 `sortedPinned = computed(() => [...pinnedSessions.value].sort(byUrgency))`，用文件里已有的 `urgencyIndex`。

5. 现有的 `.task-row`（每个组内的）加 `@contextmenu.prevent="onRowMenu($event, s)"`。

### 4.5 右键菜单

新增文件 `desktop/frontend/src/components/SessionRowMenu.vue` —— 一个 ~120 行的轻量 fixed-positioned popover，不引第三方组件。API：

```ts
defineProps<{
  open: boolean;
  x: number;    // clientX
  y: number;    // clientY
  session: RemoteSession | null;
}>();
defineEmits<{
  (e: "close"): void;
  (e: "pin", s: RemoteSession): void;
  (e: "unpin", s: RemoteSession): void;
}>();
```

行为：
- `position: fixed; top: y; left: x` + 简单的溢出翻转（左/上边翻转，避免超出 viewport）。
- 点空白 / Esc / focusout → emit `close`。
- 菜单项：`置顶` 或 `取消置顶`（根据 `pins.isPinned(session.session_id)` 决定文案）。菜单只这一项。

`TaskGroupedList` 持有 menu state（open + x + y + session ref），响应 `@contextmenu`；调用 `pins.pin` / `pins.unpin`，关闭菜单。

### 4.6 i18n

`desktop/frontend/src/i18n/messages/en.ts`:
```ts
tasks: {
  ...
  pinned: {
    title: "Pinned",
    menuPin: "Pin to top",
    menuUnpin: "Unpin",
  },
}
```

`desktop/frontend/src/i18n/messages/zh.ts`:
```ts
tasks: {
  ...
  pinned: {
    title: "置顶",
    menuPin: "置顶",
    menuUnpin: "取消置顶",
  },
}
```

### 4.7 边界

- 用户在会话已经消失（uplink 掉线）时尝试右键 → 会话行不在 DOM 里，触发不到菜单，无操作。
- 用户在置顶组的行里再次右键 → 菜单显示「取消置顶」。
- 未登录 relay / 无 session 时置顶组不显示（`pinnedSessions.value.length === 0`）。
- Rapid toggle：debounce 300ms 兜底；进程崩溃丢失最近 <300ms 的 toggle 可接受。
- 前端 crash 恢复：下一次 `useSessionPins` 挂载重新 `getPinnedSessionIds()`，一致。

## 5. Test plan

**新增：**

- `desktop/frontend/src/composables/useSessionPins.test.ts`
  - 初次 `useSessionPins()` 会调 `getPinnedSessionIds` 且只调一次（多次调用共享）
  - `pin/unpin/toggle` 更新 `pinnedIds` 集合
  - 连续 pin 触发 debounce：300ms 内多次 pin 只落一次 `setPinnedSessionIds`
  - `__resetForTests` 清空 module state
- `desktop/frontend/src/components/SessionRowMenu.test.ts`
  - open=true 渲染菜单项文案取决于 `isPinned(session)`
  - Esc / 点空白 emit `close`
  - 点菜单项 emit `pin` / `unpin`
  - x + y 超出 viewport 时翻转

**扩展：**

- `desktop/frontend/src/components/TaskGroupedList.test.ts`
  - `pinnedSessions` 存在时，顶部渲染 `[data-test=pinned-group-header]`
  - 置顶会话不在原 host / state 组里重复出现（快照或 assert 具体 row 数量）
  - 置顶组内部按 urgency 排序
  - 全部 unpin 后置顶组从 DOM 消失
  - `@contextmenu` 触发菜单 open + 定位到 clientX/Y
- `desktop/frontend/src/components/TaskSidebar.test.ts`
  - 折叠 rail 状态下右键无效（不弹菜单）；这条视实现取舍——如果 rail 也支持右键就补对应用例
- `desktop/app_test.go`
  - `GetPinnedSessionIds` 空配 → `[]string{}` 而非 nil
  - `SetPinnedSessionIds` 去重、去空、保序
  - 往返：Set → Get 返回同一序列
  - `SetPinnedSessionIds(nil)` 清空持久化字段

**手测清单（`docs/superpowers/plans/…-testing.md` 里到时展开）：**

- 单实例：置顶 3 个 session，重启桌面端仍在
- 切换 group-by host ↔ state，置顶组恒在顶部
- 折叠置顶组，再置顶新 session，展开可见新 session
- 断连一个已置顶 session → 置顶组变短；恢复 → 回来
- 一次性置顶 20 个 session，观察 debounce 落盘（只有一次 SetPinnedSessionIds）

## 6. Rollout notes

- 无迁移：新字段是纯 additive，老 config 反序列化正常（`omitempty`）。
- 首次升级用户 config 里 `pinned_session_ids` 缺失 → 前端拿到 `[]`，与"从没置顶过"等价。
- 无跨端影响（不改 relay / protocol / mobile / web）。
- 无向后兼容担忧（memory `feedback_no_backward_compat.md`）：直接落。

## 7. Open questions

_None._ 关键决策已在 brainstorm 敲定：
- 粒度：单会话
- 分组关系：抽出到顶部虚拟组，从原组移除
- 存储：桌面本地
- UI：右键菜单
- 内部排序：urgency
- 数量上限：不设
- 脏 id：不清理
