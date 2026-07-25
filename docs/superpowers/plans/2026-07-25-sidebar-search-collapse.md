# Sidebar 搜索框收起为图标 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `TaskSidebar` 展开态头部搜索区默认只占一个放大镜图标位；点击图标 / 触发 `Cmd+F` 才展开成输入框。

**Architecture:** 在 `TaskSidebar.vue` 内加一个本地 `searchOpen` ref 控制 header 里"图标 vs 输入"两态；`focusSearch()` 打开并聚焦；`Esc` 空 + `blur` 空自动收起；`query` 非空时保持展开。单文件改动，无跨组件影响。

**Tech Stack:** Vue 3 `<script setup>` + Vitest + `@vue/test-utils`。

## Global Constraints

- 只改 `desktop/frontend/src/components/TaskSidebar.vue` 与 `desktop/frontend/src/components/TaskSidebar.test.ts`。
- 不动 i18n key（复用 `tasks.sidebar.searchPlaceholder`）。
- 不动 collapsed 侧栏 rail 分支。
- 不引入外部图标库；用 inline SVG。
- 保留 `data-test="sidebar-search"` 语义（输入框的选择器）；新增 `data-test="sidebar-search-toggle"` 给图标按钮。
- `onSearchEsc` 沿用现有语义：**query 非空 → 清空 query 并保持展开；query 为空 → 收起**（单次按键只做一件事）。
- 不动 `TaskSidebar` 的 props、emits 表面。

---

### Task 1: Sidebar 搜索区收起为图标（含 focusSearch 打开 + esc/blur 收起）

**Files:**
- Modify: `desktop/frontend/src/components/TaskSidebar.vue`
- Modify: `desktop/frontend/src/components/TaskSidebar.test.ts`

**Interfaces:**
- Consumes: 现有 `focusSearch()` exposed 方法（App.vue 侧的 `Cmd+F` 走这个）。
- Produces: 组件行为不变，`focusSearch()` 签名不变；新增 DOM 选择器 `[data-test="sidebar-search-toggle"]`。

- [ ] **Step 1: 更新现有测试 + 写新失败测试**

打开 `desktop/frontend/src/components/TaskSidebar.test.ts`。

改现有测试（第 139 行 `renders a search input in the expanded header and hides it when collapsed`）为：

```ts
test("expanded header defaults to search icon, not input; collapsed shows neither", async () => {
  const w = mount(TaskSidebar, {
    props: {
      collapsed: false,
      byHost: {},
      primaryStateForHost: () => "idle" as const,
      completedSeen: [],
      totalUnread: 0,
    },
  });
  await flushPromises();
  expect(w.find('[data-test="sidebar-search-toggle"]').exists()).toBe(true);
  expect(w.find('[data-test="sidebar-search"]').exists()).toBe(false);

  await w.setProps({ collapsed: true });
  await nextTick();
  expect(w.find('[data-test="sidebar-search-toggle"]').exists()).toBe(false);
  expect(w.find('[data-test="sidebar-search"]').exists()).toBe(false);
});
```

改现有测试（第 157 行 `passes the typed query down to TaskGroupedList`）为先点开再输入：

```ts
test("passes the typed query down to TaskGroupedList", async () => {
  const w = mount(TaskSidebar, {
    props: {
      collapsed: false,
      byHost: {},
      primaryStateForHost: () => "idle" as const,
      completedSeen: [],
      totalUnread: 0,
    },
  });
  await flushPromises();
  await w.find('[data-test="sidebar-search-toggle"]').trigger("click");
  await nextTick();
  const input = w.find('[data-test="sidebar-search"]');
  await input.setValue("feishu");
  await nextTick();
  const list = w.findComponent({ name: "TaskGroupedList" });
  expect(list.props("searchQuery")).toBe("feishu");
});
```

改现有测试（第 175 行 `Esc inside the search input clears the query`）为先点开再打字，再 Esc：

```ts
test("Esc inside the search input clears the query first, then collapses on empty", async () => {
  const w = mount(TaskSidebar, {
    props: {
      collapsed: false,
      byHost: {},
      primaryStateForHost: () => "idle" as const,
      completedSeen: [],
      totalUnread: 0,
    },
  });
  await flushPromises();
  await w.find('[data-test="sidebar-search-toggle"]').trigger("click");
  await nextTick();
  const input = w.find<HTMLInputElement>('[data-test="sidebar-search"]');
  await input.setValue("proj");
  expect(input.element.value).toBe("proj");

  // First Esc: non-empty → clears, stays open.
  await input.trigger("keydown", { key: "Escape" });
  await nextTick();
  const inputAfterFirstEsc = w.find<HTMLInputElement>('[data-test="sidebar-search"]');
  expect(inputAfterFirstEsc.exists()).toBe(true);
  expect(inputAfterFirstEsc.element.value).toBe("");

  // Second Esc: empty → collapses back to icon.
  await inputAfterFirstEsc.trigger("keydown", { key: "Escape" });
  await nextTick();
  expect(w.find('[data-test="sidebar-search"]').exists()).toBe(false);
  expect(w.find('[data-test="sidebar-search-toggle"]').exists()).toBe(true);
});
```

改现有测试（第 194 行 `focusSearch() expose focuses the input`）为：

```ts
test("focusSearch() expands sidebar if collapsed and opens the search input", async () => {
  const w = mount(TaskSidebar, {
    props: {
      collapsed: true,
      byHost: {},
      primaryStateForHost: () => "idle" as const,
      completedSeen: [],
      totalUnread: 0,
    },
    attachTo: document.body,
  });
  await flushPromises();
  // From collapsed: emits update:collapsed=false (parent expands the sidebar);
  // internally focusSearch also flips searchOpen=true so once the parent
  // applies the prop, the input (not the icon) is what shows up.
  await (w.vm as any).focusSearch();
  await nextTick();
  expect(w.emitted("update:collapsed")?.[0]).toEqual([false]);

  // Parent responds by setting collapsed=false — expanded branch renders
  // with searchOpen already true, so the input is visible (no toggle).
  await w.setProps({ collapsed: false });
  await nextTick();
  const input = w.find<HTMLInputElement>('[data-test="sidebar-search"]');
  expect(input.exists()).toBe(true);
  expect(w.find('[data-test="sidebar-search-toggle"]').exists()).toBe(false);

  // Second focusSearch (after parent applied prop) focuses the input.
  await (w.vm as any).focusSearch();
  await nextTick();
  expect(document.activeElement).toBe(input.element);
  w.unmount();
});
```

再加一条覆盖"展开态+icon 态下 focusSearch 直接开输入并聚焦"：

```ts
test("focusSearch() from expanded + icon state opens the input and focuses", async () => {
  const w = mount(TaskSidebar, {
    props: {
      collapsed: false,
      byHost: {},
      primaryStateForHost: () => "idle" as const,
      completedSeen: [],
      totalUnread: 0,
    },
    attachTo: document.body,
  });
  await flushPromises();
  expect(w.find('[data-test="sidebar-search-toggle"]').exists()).toBe(true);
  await (w.vm as any).focusSearch();
  await nextTick();
  const input = w.find<HTMLInputElement>('[data-test="sidebar-search"]');
  expect(input.exists()).toBe(true);
  expect(document.activeElement).toBe(input.element);
  w.unmount();
});
```

新增测试（追加到 `describe("TaskSidebar", ...)` 块末尾，在结尾 `});` 之前）：

```ts
test("clicking the search-toggle opens the input and focuses it", async () => {
  const w = mount(TaskSidebar, {
    props: {
      collapsed: false,
      byHost: {},
      primaryStateForHost: () => "idle" as const,
      completedSeen: [],
      totalUnread: 0,
    },
    attachTo: document.body,
  });
  await flushPromises();
  await w.find('[data-test="sidebar-search-toggle"]').trigger("click");
  await nextTick();
  const input = w.find<HTMLInputElement>('[data-test="sidebar-search"]');
  expect(input.exists()).toBe(true);
  expect(document.activeElement).toBe(input.element);
  w.unmount();
});

test("blur with empty query collapses back to icon", async () => {
  const w = mount(TaskSidebar, {
    props: {
      collapsed: false,
      byHost: {},
      primaryStateForHost: () => "idle" as const,
      completedSeen: [],
      totalUnread: 0,
    },
    attachTo: document.body,
  });
  await flushPromises();
  await w.find('[data-test="sidebar-search-toggle"]').trigger("click");
  await nextTick();
  const input = w.find<HTMLInputElement>('[data-test="sidebar-search"]');
  expect(input.exists()).toBe(true);
  await input.trigger("blur");
  await nextTick();
  expect(w.find('[data-test="sidebar-search"]').exists()).toBe(false);
  expect(w.find('[data-test="sidebar-search-toggle"]').exists()).toBe(true);
  w.unmount();
});

test("blur with non-empty query keeps input visible", async () => {
  const w = mount(TaskSidebar, {
    props: {
      collapsed: false,
      byHost: {},
      primaryStateForHost: () => "idle" as const,
      completedSeen: [],
      totalUnread: 0,
    },
    attachTo: document.body,
  });
  await flushPromises();
  await w.find('[data-test="sidebar-search-toggle"]').trigger("click");
  await nextTick();
  const input = w.find<HTMLInputElement>('[data-test="sidebar-search"]');
  await input.setValue("keep");
  await input.trigger("blur");
  await nextTick();
  const stillInput = w.find<HTMLInputElement>('[data-test="sidebar-search"]');
  expect(stillInput.exists()).toBe(true);
  expect(stillInput.element.value).toBe("keep");
  w.unmount();
});
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `cd desktop/frontend && npm test -- --run src/components/TaskSidebar.test.ts`

Expected: 新加/改的 6 个测试全部 FAIL（旧的其他测试仍通过）。失败信息应指向 `sidebar-search-toggle` 不存在，或默认渲染仍是 input 而非 toggle。

- [ ] **Step 3: 修改 `TaskSidebar.vue` — script 部分**

打开 `desktop/frontend/src/components/TaskSidebar.vue`。

在第 92 行 `const searchEl = ref<HTMLInputElement | null>(null);` 后加：

```ts
const searchOpen = ref(false);

async function openSearch(): Promise<void> {
  searchOpen.value = true;
  await nextTick();
  searchEl.value?.focus();
  searchEl.value?.select();
}

function onSearchBlur() {
  if (query.value === "") searchOpen.value = false;
}
```

修改现有 `onSearchEsc`（第 94 行）：

```ts
function onSearchEsc() {
  if (query.value !== "") {
    query.value = "";
    return;
  }
  searchOpen.value = false;
}
```

修改现有 `focusSearch`（第 98 行）：

```ts
async function focusSearch(): Promise<void> {
  if (props.collapsed) {
    emit("update:collapsed", false);
    await nextTick();
  }
  await openSearch();
}
```

- [ ] **Step 4: 修改 `TaskSidebar.vue` — template 部分**

替换第 231-241 行（`<span class="title">…</span>` 加 `<input …>`）为：

```html
<span v-if="!(searchOpen || query)" class="title">{{ t("tasks.sidebar.title") }}</span>
<button
  v-if="!(searchOpen || query)"
  class="search-icon-btn"
  data-test="sidebar-search-toggle"
  :title="t('tasks.sidebar.searchPlaceholder')"
  :aria-label="t('tasks.sidebar.searchPlaceholder')"
  @click="openSearch"
>
  <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
    <circle cx="7" cy="7" r="4.5" fill="none" stroke="currentColor" stroke-width="1.5" />
    <line x1="10.5" y1="10.5" x2="14" y2="14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
  </svg>
</button>
<input
  v-else
  ref="searchEl"
  v-model="query"
  type="search"
  class="sidebar-search"
  data-test="sidebar-search"
  :placeholder="t('tasks.sidebar.searchPlaceholder')"
  :aria-label="t('tasks.sidebar.searchPlaceholder')"
  @keydown.esc.prevent="onSearchEsc"
  @blur="onSearchBlur"
/>
```

注意：`title` 与 `search-icon-btn` 用同一个 `v-if` 条件，收起态时两者一起显示（title 占满释放空间，图标坐在右侧）；展开态两者都消失，让 input 独占。

- [ ] **Step 5: 修改 `TaskSidebar.vue` — style 部分**

修改第 344-352 行 `.title` 的 `flex`（`0 1 auto` → `1 1 auto`），让收起态 title 占满剩余空间：

```css
.title {
  flex: 1 1 auto;
  min-width: 0;
  font-weight: 500;
  margin-right: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
```

在 `.group-toggle` 定义（第 361 行）之前追加 `.search-icon-btn` 样式：

```css
.search-icon-btn {
  background: none;
  border: none;
  color: inherit;
  cursor: pointer;
  padding: 2px 4px;
  margin-right: 6px;
  opacity: 0.7;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 3px;
}
.search-icon-btn:hover { opacity: 1; background: rgba(255, 255, 255, 0.05); }
```

- [ ] **Step 6: 运行测试确认全绿**

Run: `cd desktop/frontend && npm test -- --run src/components/TaskSidebar.test.ts`

Expected: 该文件所有测试通过（原有的 + 改的 + 新增的）。

- [ ] **Step 7: 跑一次完整 frontend 测试确保没有远端 regression**

Run: `cd desktop/frontend && npm test -- --run`

Expected: 全部通过（baseline 1546；本 PR 新增 4 条测试 → 1550）。

- [ ] **Step 8: 手动烟测（可选，非阻断）**

若时间允许，`npm run dev` 打开侧栏检查：
- 默认头部：`会话` 标题 + 🔍 图标 + `按主机` + `«`
- 点 🔍 → 变成输入框获焦
- 打字 → 结果过滤
- 按 Esc（有内容）→ 清空但保持输入
- 按 Esc（空）→ 收起为图标
- `Cmd+F` → 从图标态直接展开并获焦

- [ ] **Step 9: Commit**

```bash
git add desktop/frontend/src/components/TaskSidebar.vue desktop/frontend/src/components/TaskSidebar.test.ts
git commit -m "feat(sidebar): collapse search input into icon by default

Default header shows a magnifier icon in place of the always-open search
input. Click / Cmd+F opens the input; Esc-on-empty and blur-on-empty
collapse back. Reclaims horizontal space for the sidebar title in the
common no-search state."
```
