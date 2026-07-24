# 会话侧栏多选批量操作与右键详情 — design

Date: 2026-07-24
Status: Drafted — awaiting user review before plan.

## 0. Summary

左侧会话栏 (`TaskSidebar` → `TaskGroupedList`) 目前每行只支持单击 open、
右键 pin/unpin。用户希望：

1. 会话行右键新增 **"查看详细信息"** — 弹出非模态 popover，展示 session
   元数据（id / host / cwd / command / task_state / driver-viewers / 所在
   tab / pane 位置等）。
2. 按住 **Cmd (macOS) 或 Ctrl (其它平台)** 点击进入多选态；≥1 选中即处于
   多选，且允许对选中集合执行两类批量操作：**关闭会话** 与 **合并 tab**。

多选状态跟 `useSessionPins` 一样，用一个 module-scoped composable
(`useSessionSelection`) 承载，仅生存在会话生命周期内，不持久化。合并 tab
只在 N ≤ 4 时可点；N=2 → vertical，N=3~4 → grid2x2 (3 pane 时留一格空)。
批量关闭仅对"当前 window 已作为 pane 打开"的会话生效，未打开的静默跳过。

## 1. Goals

- 右键菜单动态化：单选时显示 "详情 / pin"；多选时显示 "合并 tab N ·
  关闭 M · 详情 (禁用)"。
- 详细信息 popover：非模态，Esc / 点空白 / focusout / 再次右键关闭；复用
  `SessionRowMenu` 已有的 viewport-edge flipping 定位思路。
- 多选交互：
  - 平台键判断：`e.metaKey || e.ctrlKey` — 与 `SessionPickerDialog` 已有
    做法一致，macOS 上 Cmd+click 可用而不与 Ctrl+click 右键冲突。
  - 普通 click → open 会话 + `clear()` 选中集（保留原行为）。
  - Cmd/Ctrl+click → `toggle(id)`；不 open。
  - Shift+click → 从锚点到当前行的**可见行范围**加入选中；锚点 = 上次
    Cmd/Ctrl+click 或 Shift+click 的目标 id。
  - 侧栏内空白点击 / Esc → `clear()`。
- 选中态视觉：行叠加 `outline: 2px solid var(--accent)`；`active` (当前
  pane) 的 inset shadow 保留，两者可同时显示。
- 底部批量操作条 (`BulkActionBar`)：`v-if="selected.size >= 1"`，与现有
  `mark-all` footer 互斥挂载。内容：`已选 N ｜ 合并 tab (N≤4) ｜ 关闭 M ｜
  取消`。M = 选中集合 ∩ `openSessionIds`。
- 合并 tab：
  - `layoutForCount(1)='single'`; `(2)='vertical'`; `(3)=(4)='grid2x2'`;
    `n>4 → null`（按钮禁用 + tooltip"最多 4"）。
  - 若选中会话有的已在其它 tab 里作为 pane 打开，先 detach 该 pane
    （若原 tab pane 数变 0，删除 tab），再放进新 tab；`currentTabId`
    切到新 tab。
  - 合并成功后 `clear()`。
- 批量关闭：只对 `openSessionIdSet` 里的 id 走单条 `onSidebarClose`
  路径；未打开的 id 忽略（按钮标签用 M 计数）。M=0 时按钮禁用 +
  tooltip "无已打开的会话"。
- 实现限定 `desktop/frontend/`；不改协议 / relay / mobile / web。

## 2. Non-goals

- **不做 Cmd/Ctrl+A 全选**：现实场景选中一般 ≤ 4，且全选需要额外定义"可见
  范围"（含 pinned / completed fold）；本轮 YAGNI。
- **不做拖拽合并**：目前不做拖拽 UI；批量条 + Cmd-click 已够。
- **不 kill 未打开会话**：`onSidebarClose` 现在的语义是"从 tab 里 detach
  pane"；批量关闭沿用相同语义，未打开的直接静默跳过。若之后想批量
  `killSession` 未打开的会话，另立需求。
- **不做移动端 / web 端**：与 sidebar-search 同样限定桌面。
- **不做多选状态跨会话持久化**：选中集仅存在于当前 window 生命周期。
- **不做合并后 layout 二次调整入口**：合并直接产出 vertical / grid2x2；
  用户如果想改成 horizontal，走现有 pane 布局切换（若未来加）。
- **不做"复制选中会话 id 列表"**：单条 popover 里已有 ⧉ 复制单个 id；批量
  复制不在本轮。
- **不做详细信息 popover 的自定义字段过滤 / 排序**：字段顺序固定。

## 3. 现状

- `desktop/frontend/src/components/TaskSidebar.vue:143-246` — sidebar
  容器；`footer v-if="totalUnread > 0"` 里当前挂 `mark-all` 按钮，本次多加
  的 `BulkActionBar` 挂在同一槽位，与之互斥。
- `desktop/frontend/src/components/TaskGroupedList.vue:262-431` — 分组渲
  染。每行 `<button class="task-row" @click @contextmenu>`。context menu
  当前只有一个 pin/unpin 项 (`SessionRowMenu`)；行的点击目前直接 emit
  `open`。
- `desktop/frontend/src/components/SessionRowMenu.vue` — 固定一个菜单项
  (`{pinned ? unpin : pin}`)。本次要改成 `items: MenuItem[]` 驱动 +
  `select(key)` 事件。
- `desktop/frontend/src/App.vue:153-159` — `onSidebarClose` = 找到 pane
  位置后调 `closePaneAt`。批量关闭复用它。
- `desktop/frontend/src/App.vue:487-495` — `openSessionIds` 已经是全 tab
  已打开 sessions 集合，直接下发给 sidebar 用作 `openSessionIdSet`。
- `desktop/frontend/src/composables/useSessionPins.ts` — module-scoped
  composable 模板，本次照抄结构写 `useSessionSelection` (不持久化，去掉
  loadOnce/schedulePersist)。
- `desktop/frontend/src/lib/paneLocation.ts`（若不存在则在 App.vue 内联）
  — `findPaneLocation(tabs, id)` 已有。合并 tab 需要新增
  `detachPaneAt(tabId, paneIdx)` — 从 tab.panes 里剥离该 pane（不 kill
  session），若 tab.panes 全空则删 tab。

## 4. 设计

### 4.1 useSessionSelection composable

新文件：`desktop/frontend/src/composables/useSessionSelection.ts`。

```ts
import { computed, ref, type ComputedRef, type Ref } from "vue";

const selectedIds = ref<Set<string>>(new Set());
const anchorId = ref<string | null>(null);

export function useSessionSelection() {
  const size = computed(() => selectedIds.value.size);
  const isSelected = (id: string) => selectedIds.value.has(id);
  const has = (id: string) => selectedIds.value.has(id);

  function toggle(id: string) {
    const next = new Set(selectedIds.value);
    if (next.has(id)) next.delete(id); else next.add(id);
    selectedIds.value = next;
    anchorId.value = id;
  }
  function selectOnly(id: string) {
    selectedIds.value = new Set([id]);
    anchorId.value = id;
  }
  function selectRange(id: string, orderedIds: string[]) {
    if (!anchorId.value) return toggle(id);
    const a = orderedIds.indexOf(anchorId.value);
    const b = orderedIds.indexOf(id);
    if (a < 0 || b < 0) return toggle(id);
    const [lo, hi] = a < b ? [a, b] : [b, a];
    const next = new Set(selectedIds.value);
    for (let i = lo; i <= hi; i++) next.add(orderedIds[i]);
    selectedIds.value = next;
    // anchor 保持在原锚点，符合桌面惯例（Shift 拉框以锚点为原点）
  }
  function clear() {
    if (selectedIds.value.size === 0) return;
    selectedIds.value = new Set();
    anchorId.value = null;
  }

  return {
    selectedIds: selectedIds as Ref<Set<string>>,
    anchorId: anchorId as Ref<string | null>,
    size,
    isSelected,
    has,
    toggle,
    selectOnly,
    selectRange,
    clear,
  };
}
```

Module-scoped 与 `useSessionPins` / `useTaskGroupBy` 一致，App.vue 与
TaskSidebar / TaskGroupedList 都直接 import 使用同一份状态。

### 4.2 TaskGroupedList — 行点击分支 & 视觉

`<button class="task-row" ...>` 的 `@click` 改为：

```ts
function onRowClick(e: MouseEvent, s: RemoteSession, orderedVisibleIds: string[]) {
  if (e.shiftKey) {
    sel.selectRange(s.session_id, orderedVisibleIds);
    return;
  }
  if (e.metaKey || e.ctrlKey) {
    sel.toggle(s.session_id);
    return;
  }
  // 普通 click：清空选中集，走原有 open 语义
  sel.clear();
  emit("open", s);
}
```

`orderedVisibleIds` = 按当前渲染顺序展开 pinned + group rows +
completedFiltered 得到的 flat id list（不含分组 header）。用 computed 缓
存，Shift+click 时按此列表算范围。

行 class 追加 `.selected`：

```css
.task-row.selected { outline: 2px solid var(--accent); outline-offset: -2px; }
```

`.active + .selected` 天然叠加（outline 不覆盖 inset shadow）。

Context menu 分支：

```ts
function onRowMenu(e: MouseEvent, s: RemoteSession) {
  e.preventDefault();
  // 若右键落点行不在选中集里 → 单选它（对齐大多数桌面文件管理器行为）
  if (sel.size.value > 0 && !sel.isSelected(s.session_id)) {
    sel.selectOnly(s.session_id);
  }
  menuState.value = { open: true, x: e.clientX, y: e.clientY, session: s };
}
```

`SessionRowMenu` 的 items 数组由父组件动态计算：

```ts
const menuItems = computed<MenuItem[]>(() => {
  if (sel.size.value <= 1) {
    return [
      { key: "details", label: t("tasks.rowMenu.details") },
      { key: pins.isPinned(...) ? "unpin" : "pin", label: ... },
    ];
  }
  const openCount = intersect(sel.selectedIds.value, openSessionIdSet.value).size;
  const n = sel.size.value;
  return [
    { key: "merge", label: t("tasks.bulk.mergeTab", { n }), disabled: n > 4 },
    { key: "close", label: t("tasks.bulk.close", { n: openCount }), disabled: openCount === 0 },
    { key: "details", label: t("tasks.rowMenu.details"), disabled: true }, // 详情多选无意义
    { key: pins.isPinned(...) ? "unpin" : "pin", label: ... },
  ];
});
```

### 4.3 SessionRowMenu — 由 items 数组驱动

改造：

```ts
type MenuItem = { key: string; label: string; disabled?: boolean };
defineProps<{ open: boolean; x: number; y: number; items: MenuItem[] }>();
const emit = defineEmits<{ (e: "close"): void; (e: "select", key: string): void }>();
```

原 `labelPin/labelUnpin/pinned/togglePin` 全部移除；父组件负责生成 items
+ 处理 `select(key)`。保留 focusout / Esc / outside-mousedown 逻辑。

### 4.4 BulkActionBar 组件

新文件：`desktop/frontend/src/components/BulkActionBar.vue`。

```
┌─────────────────────────────────────────────┐
│ 已选 N ｜ 合并 tab (N)   关闭 M   取消      │
└─────────────────────────────────────────────┘
```

Props：`count`, `openCount`, `canMerge`（= count ≥ 1 && count ≤ 4）。
Emits：`merge` / `close` / `clear`。

TaskSidebar 内挂在 footer 位置，替换 mark-all 显式判定：

```
<footer v-if="sel.size.value >= 1">
  <BulkActionBar ... @merge/@close/@clear />
</footer>
<footer v-else-if="totalUnread > 0">
  <button class="mark-all" ...>{{ t("tasks.markAllRead") }}</button>
</footer>
```

### 4.5 SessionDetailsPopover 组件

新文件：`desktop/frontend/src/components/SessionDetailsPopover.vue`。

复用 `SessionRowMenu` 的 viewport-edge flipping 定位、Esc / outside /
focusout 关闭逻辑（提炼一个 `usePopoverPosition(x, y, elRef)` composable
或就地拷贝一次 — 由 plan 阶段决定，两个组件都不大）。

字段渲染顺序（`v-if` 空值即跳过整行）：

| 字段 | 来源 | 备注 |
|------|------|------|
| Session ID | `s.session_id` | 可复制 |
| Type | `s.type` (`local` / `remote` / `ai`) | i18n label |
| Host | `hostNameWithIndex(...)` | 复用现有 helper |
| Driver / Viewers | `s.driver_client_id` / `s.viewer_count` | 仅远程会话 |
| CWD | `s.cwd` (full) + `shortenCwd(s.cwd, home)` | 两行显示 |
| Command / AI title | `aiTitleOrCommand(s)` + `s.current_command` | AI 会话额外显示 model 若可得 |
| Task state | `taskStateLabel(s.task_state, t)` | 复用 |
| PID | `s.pid` | `v-if` |
| Unread | boolean | 是/否 |
| Pinned | `pins.isPinned(s.session_id)` | 是/否 |
| Tab / Pane | `findPaneLocation(tabs, s.session_id)` | `Tab k · Pane j` 或 `未在本 window 打开` |

每字段行右侧 `⧉` 复制值（对 boolean 字段不显示）。使用
`navigator.clipboard.writeText`（现有 `useClipboard` 若存在则复用，否则
就地调用）。

### 4.6 App.vue — merge / close 集成

新增 helper（本文件内 or 抽 `lib/mergeSelected.ts`）：

```ts
function detachPaneForSession(sessionId: string) {
  const loc = findPaneLocation(tabs.value, sessionId);
  if (!loc) return;
  const t = tabs.value.find((tt) => tt.id === loc.tabId);
  if (!t) return;
  // detachPaneAt 不 kill 会话，只清 panes[i].sessionId（或若全空则删 tab）
  detachPaneAt(t, loc.paneIdx);
}

function layoutForCount(n: number): "single" | "vertical" | "grid2x2" | null {
  if (n === 1) return "single";
  if (n === 2) return "vertical";
  if (n === 3 || n === 4) return "grid2x2";
  return null;
}

async function mergeSelectedIntoTab() {
  const ids = Array.from(sel.selectedIds.value);
  const layout = layoutForCount(ids.length);
  if (!layout) return; // UI 已禁用，兜底
  for (const id of ids) detachPaneForSession(id);
  const newTab: Tab = {
    id: uuid(),
    layout,
    panes: buildPanes(layout, ids),
  };
  tabs.value.push(newTab);
  gotoTab(newTab.id);
  sel.clear();
}

async function closeSelectedOpen() {
  const openSet = openSessionIdSet.value;
  const ids = Array.from(sel.selectedIds.value).filter((id) => openSet.has(id));
  for (const id of ids) {
    const s = allKnownSessions.value.find((x) => x.session_id === id);
    if (s) onSidebarClose(s);
  }
  sel.clear();
}
```

`buildPanes(layout, ids)`：按 layout 生成 pane 数组，`ids.length <
capacity(layout)` 时补 empty pane `{sessionId: null}`。

`detachPaneAt` 若与现有 `closePaneAt` 逻辑重合大部分（只是不 kill
session），则复用后者，透传一个 `skipKill=true`；plan 阶段拆细。

### 4.7 键盘 & 空白点击

TaskSidebar 根 aside：

```ts
function onSidebarKeydown(e: KeyboardEvent) {
  if (e.key === "Escape" && sel.size.value > 0) {
    e.preventDefault();
    sel.clear();
  }
}
function onSidebarBlankClick(e: MouseEvent) {
  const target = e.target as HTMLElement;
  if (target.closest(".task-row, .host-header, .group-header, .bulk-bar, .row-menu, .details-popover")) return;
  sel.clear();
}
```

绑 `@click.capture` 到 `.task-sidebar` 外层，处理 blank click。keydown 走
`onMounted` 挂到 aside（tabindex="-1"）以便接 focus 后 Esc。

## 5. 数据流总图

```
App.vue
  ├── useSessionSelection (composable, module state)
  ├── mergeSelectedIntoTab(), closeSelectedOpen(), showDetailsFor(s)
  └── TaskSidebar
        │ (props: openSessionIds; emits: bulk-merge / bulk-close / clear)
        ├── TaskGroupedList
        │     │ (行点击分支; contextmenu → SessionRowMenu items)
        │     ├── SessionRowMenu (items, @select)
        │     └── SessionDetailsPopover (open, session)
        └── BulkActionBar (v-if size≥1; @merge/@close/@clear)
```

## 6. i18n keys

en / zh 各加：

- `tasks.rowMenu.details` = `View details` / `查看详细信息`
- `tasks.rowMenu.merge` = `Merge into new tab ({n})` / `合并 tab ({n})`
- `tasks.rowMenu.closeSelected` = `Close selected ({n})` / `关闭已打开 ({n})`
- `tasks.bulk.selectedCount` = `{count} selected` / `已选 {count}`
- `tasks.bulk.mergeTab` = `Merge tab` / `合并 tab`
- `tasks.bulk.mergeTooMany` = `Merge up to 4` / `最多合并 4 个`
- `tasks.bulk.close` = `Close ({n})` / `关闭 ({n})`
- `tasks.bulk.closeNoneOpen` = `None open in this window` / `本窗口未打开`
- `tasks.bulk.cancel` = `Cancel` / `取消`
- `tasks.details.title` = `Session details` / `会话详细信息`
- `tasks.details.sessionId` / `type` / `host` / `driver` / `viewers` /
  `cwd` / `command` / `aiTitle` / `state` / `pid` / `unread` / `pinned` /
  `paneLocation` / `paneLocationNone`

## 7. 测试

- `useSessionSelection.test.ts` — 纯逻辑：toggle / selectOnly /
  selectRange (含无锚点 fallback) / clear / size / isSelected。
- `TaskGroupedList.test.ts` — 新增：
  - Cmd+click（metaKey）toggle 选中
  - Ctrl+click（ctrlKey）toggle 选中（模拟 Windows/Linux）
  - Shift+click 从锚点到目标的范围选择
  - 普通 click 后 `sel.size` = 0（清空）
  - 行 `.selected` class 视觉断言
  - 多选后右键：菜单 items = 合并/关闭/详情/pin
  - 单选右键：菜单 items = 详情/pin
- `BulkActionBar.test.ts`（新）— count/openCount 显示、按钮 disabled、
  emits。
- `SessionDetailsPopover.test.ts`（新）— 字段渲染（含 v-if 空值省略）、
  复制按钮 emits copy、Esc/outside 关闭。
- `App.vue` 集成（可能加到已有 `App.test.ts` 或新建
  `App.merge.test.ts`）：merge 后 tabs 变化 + 原 tab 若变空则删除。
- 无障碍：aria-selected 断言至少一条。

## 8. 实施顺序

1. `useSessionSelection` composable + 单测
2. `SessionRowMenu` items 化 + 现有单测更新（保持 pin 行为）
3. `BulkActionBar` 组件 + 单测
4. `SessionDetailsPopover` 组件 + 单测
5. `TaskGroupedList` 行点击分支、context menu 分支、可见 id 列表、
   `.selected` 样式；单测覆盖 5 种交互
6. `TaskSidebar` footer 挂 BulkActionBar；空白点击 / Esc 清空
7. `App.vue` `mergeSelectedIntoTab` / `closeSelectedOpen` /
   `detachPaneForSession`；与 `onSidebarClose` 复用
8. i18n en/zh key 添加
9. 手动回归：iOS Capacitor 端（sidebar 同源渲染但无 Cmd 键，Ctrl 不常见，
   预期多选态**不会**在 iOS 上被误触发；写一条测试断言 touch/no-key click
   仍走 open 分支）
10. 若 ship-release 走 v0.3-dev 分支 → 直接打 tag（按现有 memory
    `feedback_release_series_v02.md`）

## 9. Open Questions

- **OQ1**：Cmd/Ctrl+A 全选是否要加？本轮 **不做**。理由：可见集含
  pinned + completed fold + 若干折叠组，全选语义模糊；用户诉求还没到。
- **OQ2**：合并 tab 时会话顺序 = 用户点击顺序（Set 插入序）。若要用户
  显式排序，需要在 BulkActionBar 内加拖拽列表；本轮 **不做**，遗留。
- **OQ3**：详细信息 popover 里 `first_seen / last_activity` 时间戳字段
  是否可得？若 `RemoteSession` 类型上没有，本轮跳过；plan 阶段核对
  `platform/types.ts`。
- **OQ4**：iOS / mobile 端多选目前**不做交互**；侧栏在 mobile 上是
  overlay 抽屉，多选按键很难触发。plan 阶段确认 Capacitor 分支不引入
  regression。
