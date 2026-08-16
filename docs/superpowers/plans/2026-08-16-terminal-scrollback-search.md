# 终端 scrollback 搜索 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给终端加一个可搜索 scrollback 的搜索栏——`Mod+F` 打开，输入即增量搜索，匹配高亮、上下跳转、显示 `当前/总数`，Esc 关闭。

**Architecture:** 搜索 UI 做成独立组件 `TerminalSearchBar.vue`（自包含、可 mount 测试），搜索能力由 `xterm-addon-search` 提供并挂在 `TerminalView.vue` 已有的 `Terminal` 实例上。快捷键走既有的注册表 + document capture 路由（`lib/shortcutBindings.ts` → `composables/useTerminalShortcuts.ts` → `App.vue`），App.vue 用一个自增序号 prop 透传到 `PaneGrid` 再到 `TerminalView`，由 `props.focused` 决定哪个 pane 响应。

**Tech Stack:** Vue 3 + TypeScript + Vite + Vitest + `xterm@5.3` / `xterm-addon-search@0.13`

**Spec:** [`docs/superpowers/specs/2026-08-16-sync-layer-roadmap-design.md`](../specs/2026-08-16-sync-layer-roadmap-design.md) §5 P5 第 19 项；roadmap 条目见 [`docs/roadmap.md`](../../roadmap.md) 第 19 项

## Global Constraints

- **xterm 是 v5 系列**：`package.json` 里是 `xterm@^5.3.0` / `xterm-addon-fit@^0.8.0` / `xterm-addon-webgl@^0.16.0`。搜索插件必须用**未加 scope 的 `xterm-addon-search@^0.13.0`**，不是 `@xterm/addon-search`（那是 xterm v6 的包名，装上会因 API 不兼容而运行时报错）。spec 正文里写的 `@xterm/addon-search` 是笔误，以本约束为准。
- **匹配装饰需要 `allowProposedApi: true`**。`TerminalView.vue` 的 `new Terminal({...})` 已经设了这一项，不要移除。
- **`Mod` 是平台无关修饰键**：mac 上是 Meta，其它平台是 Control（`lib/modKey.ts`）。所以 `Mod+KeyF` 在 mac 是 ⌘F、在 Linux/Windows 是 Ctrl+F。roadmap 文案里的 "Ctrl+Shift+F" 是照搬 Ghostty 的 GTK 约定，本实现不采用，统一走 `Mod`。
- **`sidebar.focus-search` 的默认绑定从 `Mod+KeyF` 改为 `Mod+Shift+KeyF`**（用户已确认）。终端搜索占用 `Mod+KeyF`，与 iTerm2 / Ghostty / VS Code 一致。
- **新 action 的 `group` 用 `"pane"`**。`SettingsShortcuts.vue` 按 `pane` / `tab` / `sidebar` 三个硬编码分组渲染（`src/components/SettingsShortcuts.vue:107-109`），新增第四个分组要连带改 UI 和分组标题 i18n，不在本次范围内。
- **用户可见文案必须中英双份**：`src/i18n/messages/en.ts` 与 `src/i18n/messages/zh-CN.ts` 同步新增，键名一致。
- **`desktop/frontend/` 的改动会改变 web 构建产物**。`.githooks/pre-commit` 只监听 `web/` 路径不会拦，但 CI 的 embed drift job 会红。**最后一个 task 必须重建 `internal/relay/web-dist/` 并一并提交**。
- 全部命令在 `desktop/frontend/` 目录下执行，除非另有说明。

---

### Task 1: 快捷键注册与路由

给动作注册表加 `terminal.search`，把 `sidebar.focus-search` 的默认绑定挪走，并在 document 路由里接上新动作。本任务不产生任何 UI，只让按键能被正确解析和分发。

**Files:**
- Modify: `desktop/frontend/src/lib/shortcutBindings.ts`（`ACTIONS` 表，约 21-34 行）
- Modify: `desktop/frontend/src/composables/useTerminalShortcuts.ts`（`ShortcutHandlers` 接口 + `dispatch`）
- Modify: `desktop/frontend/src/i18n/messages/en.ts`（`settings.shortcuts` 块，约 337 行附近）
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`（`settings.shortcuts` 块，约 339 行附近）
- Test: `desktop/frontend/src/lib/shortcutBindings.test.ts`
- Test: `desktop/frontend/src/composables/useTerminalShortcuts.test.ts`

**Interfaces:**
- Consumes: 无（本计划的第一个任务）
- Produces:
  - action id 字符串 `"terminal.search"`，默认绑定 `"Mod+KeyF"`，group `"pane"`，labelKey `"settings.shortcuts.terminalSearch"`
  - `ShortcutHandlers` 新增可选成员 `onTerminalSearch?: () => void`
  - i18n key `settings.shortcuts.terminalSearch`

- [ ] **Step 1: 写失败的测试 —— 注册表**

追加到 `src/lib/shortcutBindings.test.ts` 末尾：

```ts
describe("terminal search binding", () => {
  test("terminal.search is registered with Mod+KeyF in the pane group", () => {
    const action = ACTION_BY_ID["terminal.search"];
    expect(action).toBeDefined();
    expect(action!.defaultBinding).toBe("Mod+KeyF");
    expect(action!.group).toBe("pane");
    expect(action!.labelKey).toBe("settings.shortcuts.terminalSearch");
  });

  test("sidebar.focus-search moved off Mod+KeyF to Mod+Shift+KeyF", () => {
    expect(ACTION_BY_ID["sidebar.focus-search"]!.defaultBinding).toBe("Mod+Shift+KeyF");
  });

  test("no two actions share a default binding", () => {
    const seen = new Map<string, string>();
    for (const a of ACTIONS) {
      expect(seen.has(a.defaultBinding)).toBe(false);
      seen.set(a.defaultBinding, a.id);
    }
  });

  test("DEFAULT_BINDINGS routes Mod+KeyF to terminal.search", () => {
    expect(DEFAULT_BINDINGS["Mod+KeyF"]).toBe("terminal.search");
    expect(DEFAULT_BINDINGS["Mod+Shift+KeyF"]).toBe("sidebar.focus-search");
  });
});
```

确认该文件顶部的 import 含 `ACTIONS`、`ACTION_BY_ID`、`DEFAULT_BINDINGS`；缺哪个就补进去。

- [ ] **Step 2: 跑测试确认失败**

Run: `npx vitest run src/lib/shortcutBindings.test.ts`
Expected: FAIL —— `expect(action).toBeDefined()` 收到 `undefined`。

- [ ] **Step 3: 改注册表**

在 `src/lib/shortcutBindings.ts` 的 `ACTIONS` 数组里，`pane.focus-down` 那行之后插入一行：

```ts
  { id: "terminal.search",           group: "pane", labelKey: "settings.shortcuts.terminalSearch",                      defaultBinding: "Mod+KeyF" },
```

并把 `sidebar.focus-search` 那一行的默认绑定改掉：

```ts
  { id: "sidebar.focus-search",       group: "sidebar", labelKey: "settings.shortcuts.focusSidebarSearch",                defaultBinding: "Mod+Shift+KeyF" },
```

- [ ] **Step 4: 补 i18n 键**

`src/i18n/messages/en.ts` 的 `settings.shortcuts` 块里，`focusSidebarSearch` 那行之后加：

```ts
      terminalSearch: "Search terminal",
```

`src/i18n/messages/zh-CN.ts` 同一位置加：

```ts
      terminalSearch: "搜索终端",
```

- [ ] **Step 5: 跑测试确认通过**

Run: `npx vitest run src/lib/shortcutBindings.test.ts`
Expected: PASS

- [ ] **Step 6: 写失败的测试 —— 路由分发**

追加到 `src/composables/useTerminalShortcuts.test.ts` 末尾。该文件已有 `fireKey(opts)` 辅助（构造 `keydown` 并 `document.dispatchEvent`，返回事件对象），以及 `effectScope` + `beforeEach/afterEach` 的挂载模式；下面沿用同一套：

```ts
describe("useTerminalShortcuts (terminal search)", () => {
  let scope: ReturnType<typeof effectScope>;
  let bindings: Ref<Record<string, string>>;
  const handlers = {
    onSplitVertical: vi.fn(),
    onSplitHorizontal: vi.fn(),
    onClosePane: vi.fn(),
    onFocusPane: vi.fn(),
    onNewTab: vi.fn(),
    onSwitchTab: vi.fn(),
    onFocusSidebarSearch: vi.fn(),
    onTerminalSearch: vi.fn(),
  };

  beforeEach(() => {
    Object.values(handlers).forEach((h) => h.mockReset());
    bindings = ref({});
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
  });

  afterEach(() => {
    scope.stop();
  });

  it("Ctrl+F -> onTerminalSearch", () => {
    const ev = fireKey({ key: "f", code: "KeyF", ctrlKey: true });
    expect(handlers.onTerminalSearch).toHaveBeenCalledTimes(1);
    expect(handlers.onFocusSidebarSearch).not.toHaveBeenCalled();
    expect(ev.defaultPrevented).toBe(true);
  });

  it("Ctrl+Shift+F -> onFocusSidebarSearch", () => {
    fireKey({ key: "F", code: "KeyF", ctrlKey: true, shiftKey: true });
    expect(handlers.onFocusSidebarSearch).toHaveBeenCalledTimes(1);
    expect(handlers.onTerminalSearch).not.toHaveBeenCalled();
  });
});

describe("useTerminalShortcuts (terminal search handler omitted)", () => {
  it("Ctrl+F does not throw when onTerminalSearch is absent", () => {
    const scope = effectScope();
    scope.run(() =>
      useTerminalShortcuts(
        {
          onSplitVertical: vi.fn(),
          onSplitHorizontal: vi.fn(),
          onClosePane: vi.fn(),
          onFocusPane: vi.fn(),
          onNewTab: vi.fn(),
          onSwitchTab: vi.fn(),
        },
        { mod: "Control" },
      ),
    );
    expect(() => fireKey({ key: "f", code: "KeyF", ctrlKey: true })).not.toThrow();
    scope.stop();
  });
});
```

文件顶部已 import `effectScope` / `ref` / `type Ref` / `vi` / `beforeEach` / `afterEach`，无需新增 import。

- [ ] **Step 7: 跑测试确认失败**

Run: `npx vitest run src/composables/useTerminalShortcuts.test.ts`
Expected: FAIL —— `onTerminalSearch` 未被调用（`dispatch` 里没有这个 case）。

- [ ] **Step 8: 接上路由**

`src/composables/useTerminalShortcuts.ts`，在 `ShortcutHandlers` 接口的 `onFocusSidebarSearch?` 之后加一行：

```ts
  onTerminalSearch?: () => void;
```

在 `dispatch` 的 switch 里，`sidebar.focus-search` 那个 case 之后加一行：

```ts
    case "terminal.search":            h.onTerminalSearch?.(); return true;
```

- [ ] **Step 9: 跑测试确认通过**

Run: `npx vitest run src/composables/useTerminalShortcuts.test.ts src/lib/shortcutBindings.test.ts`
Expected: PASS

- [ ] **Step 10: 类型检查**

Run: `npm run build`
Expected: 构建成功、无 TS 报错。

- [ ] **Step 11: 提交**

```bash
git add src/lib/shortcutBindings.ts src/lib/shortcutBindings.test.ts \
        src/composables/useTerminalShortcuts.ts src/composables/useTerminalShortcuts.test.ts \
        src/i18n/messages/en.ts src/i18n/messages/zh-CN.ts
git commit -m "feat(shortcuts): register terminal.search on Mod+F, move sidebar search to Mod+Shift+F"
```

---

### Task 2: TerminalSearchBar 组件

一个自包含的搜索栏组件。它不认识 xterm，只负责输入、按键语义和结果计数展示，通过事件把「查什么、往哪个方向查」告诉父组件。这样它能被真实 mount 测试——而 `TerminalView.vue` 因为依赖 xterm 与 Wails 绑定无法 mount，其测试只能做源码断言（见 Task 3）。

**Files:**
- Create: `desktop/frontend/src/components/TerminalSearchBar.vue`
- Create: `desktop/frontend/src/components/TerminalSearchBar.test.ts`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`（`terminal` 块，约 62-82 行）
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`（对应 `terminal` 块）

**Interfaces:**
- Consumes: Task 1 无直接产物被此任务使用（两者在 Task 3 汇合）
- Produces: 组件 `TerminalSearchBar.vue`，契约如下——
  - props：`open: boolean`、`focusSeq: number`、`resultIndex: number`（0 起；无结果时 `-1`）、`resultCount: number`
  - emits：`find(query: string, dir: "next" | "prev", incremental: boolean)`、`close()`
  - i18n keys：`terminal.search.placeholder` / `terminal.search.next` / `terminal.search.prev` / `terminal.search.close` / `terminal.search.noResults`

- [ ] **Step 1: 补 i18n 键**

`src/i18n/messages/en.ts` 的 `terminal:` 块内，`clearBuffer` 那行之后加：

```ts
    search: {
      placeholder: "Find",
      next: "Next match",
      prev: "Previous match",
      close: "Close search",
      noResults: "No results",
    },
```

`src/i18n/messages/zh-CN.ts` 的 `terminal:` 块同一位置加：

```ts
    search: {
      placeholder: "查找",
      next: "下一个匹配",
      prev: "上一个匹配",
      close: "关闭搜索",
      noResults: "无匹配",
    },
```

- [ ] **Step 2: 写失败的测试**

创建 `src/components/TerminalSearchBar.test.ts`：

```ts
import { describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import TerminalSearchBar from "./TerminalSearchBar.vue";

function factory(props: Partial<{
  open: boolean;
  focusSeq: number;
  resultIndex: number;
  resultCount: number;
}> = {}) {
  return mount(TerminalSearchBar, {
    attachTo: document.body,
    props: {
      open: true,
      focusSeq: 1,
      resultIndex: -1,
      resultCount: 0,
      ...props,
    },
  });
}

describe("TerminalSearchBar", () => {
  test("renders nothing when closed", () => {
    const w = factory({ open: false });
    expect(w.find('[data-test="terminal-search"]').exists()).toBe(false);
  });

  test("renders the input when open", () => {
    const w = factory();
    expect(w.find('[data-test="terminal-search-input"]').exists()).toBe(true);
  });

  test("typing emits an incremental next-direction find", async () => {
    const w = factory();
    const input = w.find('[data-test="terminal-search-input"]');
    await input.setValue("needle");

    const events = w.emitted("find");
    expect(events).toBeTruthy();
    expect(events!.at(-1)).toEqual(["needle", "next", true]);
  });

  test("Enter emits a non-incremental next find", async () => {
    const w = factory();
    await w.find('[data-test="terminal-search-input"]').setValue("needle");
    await w.find('[data-test="terminal-search-input"]').trigger("keydown", { key: "Enter" });

    expect(w.emitted("find")!.at(-1)).toEqual(["needle", "next", false]);
  });

  test("Shift+Enter emits a non-incremental prev find", async () => {
    const w = factory();
    await w.find('[data-test="terminal-search-input"]').setValue("needle");
    await w.find('[data-test="terminal-search-input"]').trigger("keydown", { key: "Enter", shiftKey: true });

    expect(w.emitted("find")!.at(-1)).toEqual(["needle", "prev", false]);
  });

  test("the next and prev buttons emit non-incremental finds", async () => {
    const w = factory({ resultIndex: 0, resultCount: 3 });
    await w.find('[data-test="terminal-search-input"]').setValue("needle");

    await w.find('[data-test="terminal-search-next"]').trigger("click");
    expect(w.emitted("find")!.at(-1)).toEqual(["needle", "next", false]);

    await w.find('[data-test="terminal-search-prev"]').trigger("click");
    expect(w.emitted("find")!.at(-1)).toEqual(["needle", "prev", false]);
  });

  test("Escape emits close", async () => {
    const w = factory();
    await w.find('[data-test="terminal-search-input"]').trigger("keydown", { key: "Escape" });
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("the close button emits close", async () => {
    const w = factory();
    await w.find('[data-test="terminal-search-close"]').trigger("click");
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("shows a one-based position over the total", () => {
    const w = factory({ resultIndex: 2, resultCount: 12 });
    expect(w.find('[data-test="terminal-search-count"]').text()).toBe("3/12");
  });

  test("shows the no-results label once a query has been typed", async () => {
    const w = factory({ resultIndex: -1, resultCount: 0 });
    await w.find('[data-test="terminal-search-input"]').setValue("nope");
    expect(w.find('[data-test="terminal-search-count"]').text()).toBe("No results");
  });

  test("shows no counter while the query is still empty", () => {
    const w = factory({ resultIndex: -1, resultCount: 0 });
    expect(w.find('[data-test="terminal-search-count"]').text()).toBe("");
  });

  test("disables navigation buttons when there are no matches", () => {
    const w = factory({ resultIndex: -1, resultCount: 0 });
    expect(w.find('[data-test="terminal-search-next"]').attributes("disabled")).toBeDefined();
    expect(w.find('[data-test="terminal-search-prev"]').attributes("disabled")).toBeDefined();
  });

  test("focuses and selects the input when focusSeq changes while open", async () => {
    const w = factory({ focusSeq: 1 });
    const el = w.find('[data-test="terminal-search-input"]').element as HTMLInputElement;
    el.value = "old";
    const focus = vi.spyOn(el, "focus");
    const select = vi.spyOn(el, "select");

    await w.setProps({ focusSeq: 2 });
    await w.vm.$nextTick();

    expect(focus).toHaveBeenCalled();
    expect(select).toHaveBeenCalled();
  });
});
```

- [ ] **Step 3: 跑测试确认失败**

Run: `npx vitest run src/components/TerminalSearchBar.test.ts`
Expected: FAIL —— 找不到模块 `./TerminalSearchBar.vue`。

- [ ] **Step 4: 写组件**

创建 `src/components/TerminalSearchBar.vue`：

```vue
<script setup lang="ts">
// Search bar for the terminal scrollback. Deliberately knows nothing about
// xterm: it owns the query string and the key semantics, and reports what to
// look for through `find`. The parent drives the search addon and feeds the
// result position back in via `resultIndex` / `resultCount`.
import { nextTick, ref, watch } from "vue";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  open: boolean;
  // Bumped by the parent every time the open shortcut fires, so pressing it
  // again while the bar is already open re-focuses and selects the query.
  focusSeq: number;
  // 0-based index of the active match; -1 when there is none.
  resultIndex: number;
  resultCount: number;
}>();

const emit = defineEmits<{
  (e: "find", query: string, dir: "next" | "prev", incremental: boolean): void;
  (e: "close"): void;
}>();

const { t } = useI18n();
const query = ref("");
const inputEl = ref<HTMLInputElement | null>(null);

function focusInput() {
  void nextTick(() => {
    inputEl.value?.focus();
    inputEl.value?.select();
  });
}

watch(() => props.open, (open) => { if (open) focusInput(); });
watch(() => props.focusSeq, () => { if (props.open) focusInput(); });

function onInput() {
  emit("find", query.value, "next", true);
}

function step(dir: "next" | "prev") {
  emit("find", query.value, dir, false);
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    emit("close");
    return;
  }
  if (e.key === "Enter") {
    e.preventDefault();
    step(e.shiftKey ? "prev" : "next");
  }
}
</script>

<template>
  <div v-if="open" class="term-search" data-test="terminal-search">
    <input
      ref="inputEl"
      v-model="query"
      class="term-search-input"
      data-test="terminal-search-input"
      type="text"
      spellcheck="false"
      autocomplete="off"
      :placeholder="t('terminal.search.placeholder')"
      @input="onInput"
      @keydown="onKeydown"
    />
    <span class="term-search-count" data-test="terminal-search-count">
      {{ resultCount > 0 ? `${resultIndex + 1}/${resultCount}` : (query ? t("terminal.search.noResults") : "") }}
    </span>
    <button
      class="term-search-btn"
      data-test="terminal-search-prev"
      :title="t('terminal.search.prev')"
      :disabled="resultCount === 0"
      @click="step('prev')"
    >↑</button>
    <button
      class="term-search-btn"
      data-test="terminal-search-next"
      :title="t('terminal.search.next')"
      :disabled="resultCount === 0"
      @click="step('next')"
    >↓</button>
    <button
      class="term-search-btn"
      data-test="terminal-search-close"
      :title="t('terminal.search.close')"
      @click="emit('close')"
    >✕</button>
  </div>
</template>

<style scoped>
/* Anchored top-LEFT on purpose: TerminalView's .overlay (attach progress /
   replay) and the remote badge both live at top-right, so the right side is
   already spoken for. The parent .term-view is `position: absolute; inset: 0`,
   which is the positioning context this absolute box resolves against. */
.term-search {
  position: absolute;
  top: 8px;
  left: 12px;
  z-index: 6;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 6px;
  border: 1px solid #2d333b;
  border-radius: 6px;
  background: #161b22;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.35);
}
.term-search-input {
  width: 180px;
  padding: 2px 6px;
  border: 1px solid #2d333b;
  border-radius: 4px;
  background: #0d1117;
  color: #c9d1d9;
  font-size: 12px;
  outline: none;
}
.term-search-input:focus { border-color: var(--accent); }
.term-search-count {
  min-width: 52px;
  color: #8b949e;
  font-size: 11px;
  text-align: center;
  white-space: nowrap;
}
.term-search-btn {
  padding: 1px 6px;
  border: 1px solid #2d333b;
  border-radius: 4px;
  background: #21262d;
  color: #c9d1d9;
  font-size: 11px;
  line-height: 16px;
  cursor: pointer;
}
.term-search-btn:hover:not(:disabled) { background: #2d333b; }
.term-search-btn:disabled { opacity: 0.4; cursor: default; }
</style>
```

- [ ] **Step 5: 跑测试确认通过**

Run: `npx vitest run src/components/TerminalSearchBar.test.ts`
Expected: PASS（13 个用例全绿）

- [ ] **Step 6: 类型检查**

Run: `npm run build`
Expected: 构建成功、无 TS 报错。

- [ ] **Step 7: 提交**

```bash
git add src/components/TerminalSearchBar.vue src/components/TerminalSearchBar.test.ts \
        src/i18n/messages/en.ts src/i18n/messages/zh-CN.ts
git commit -m "feat(terminal): add TerminalSearchBar component"
```

---

### Task 3: 接线 —— SearchAddon + 从 App.vue 透传到聚焦 pane

把 Task 1 的快捷键和 Task 2 的组件接到真实终端上：装依赖、在 `ensureTerm()` 里加载 `SearchAddon`、把搜索请求从 `App.vue` 透传经 `PaneGrid` 到 `TerminalView`，由 `props.focused` 决定谁响应。

`TerminalView.vue` / `PaneGrid.vue` / `App.vue` 都无法在 Vitest 里 mount（依赖 xterm、Wails 绑定和 WebSocket），所以本任务的测试沿用 `TerminalView.test.ts` 已有的做法：用 `?raw` 导入源码做正则断言。这是该文件既定的模式，不要改成 mount。

**Files:**
- Modify: `desktop/frontend/package.json`（新增依赖）
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Modify: `desktop/frontend/src/components/PaneGrid.vue`（`<TerminalView>` 调用处，约 192-211 行）
- Modify: `desktop/frontend/src/App.vue`（`useTerminalShortcuts(...)` 调用处，约 1301 行；`<PaneGrid>` 调用处）
- Modify: `desktop/frontend/src/components/TerminalView.test.ts`
- Modify: `docs/roadmap.md`（勾选第 19 项）
- Modify: `internal/relay/web-dist/`（重建产物）

**Interfaces:**
- Consumes:
  - Task 1 的 `ShortcutHandlers.onTerminalSearch?: () => void`
  - Task 2 的 `TerminalSearchBar.vue`（props `open` / `focusSeq` / `resultIndex` / `resultCount`；emits `find(query, dir, incremental)` / `close`）
- Produces: 无下游任务

- [ ] **Step 1: 装依赖**

Run:

```bash
npm install --save xterm-addon-search@^0.13.0
```

装完确认 `package.json` 的 `dependencies` 里出现 `"xterm-addon-search": "^0.13.0"`，且 `package-lock.json` 已更新。

- [ ] **Step 2: 写失败的测试**

先在 `src/components/TerminalView.test.ts` 顶部的 import 区（`import appSource from "../App.vue?raw";` 那一行之后）加一行：

```ts
import searchBarSource from "./TerminalSearchBar.vue?raw";
```

再把下面的 describe 追加到该文件末尾：

```ts
describe("TerminalView scrollback search wiring", () => {
  test("loads the search addon on the live terminal", () => {
    expect(source).toContain('import { SearchAddon } from "xterm-addon-search"');
    expect(source).toMatch(/searchAddon = new SearchAddon\(\);\s*term\.loadAddon\(searchAddon\)/);
  });

  test("keeps allowProposedApi on, which match decorations require", () => {
    expect(source).toContain("allowProposedApi: true");
  });

  test("mirrors addon result changes into the bar's counters", () => {
    expect(source).toMatch(
      /searchAddon\.onDidChangeResults\(\(\{ resultIndex, resultCount \}\) => \{[\s\S]*?searchResultIndex\.value = resultIndex;[\s\S]*?searchResultCount\.value = resultCount;/,
    );
  });

  test("only the focused pane opens on a search request", () => {
    expect(source).toContain("searchRequestSeq?: number");
    expect(source).toMatch(
      /watch\(\s*\(\)\s*=>\s*props\.searchRequestSeq,[\s\S]*?if \(!props\.focused\) return;[\s\S]*?searchOpen\.value = true/,
    );
  });

  test("closing the search clears decorations and returns focus to the terminal", () => {
    expect(source).toMatch(
      /function onSearchClose\(\) \{[\s\S]*?searchOpen\.value = false;[\s\S]*?searchAddon\?\.clearDecorations\(\);[\s\S]*?term\?\.focus\(\);/,
    );
  });

  test("renders the search bar bound to the addon-backed state", () => {
    expect(source).toContain("<TerminalSearchBar");
    expect(source).toContain(':open="searchOpen"');
    expect(source).toContain(':focus-seq="searchFocusSeq"');
    expect(source).toContain(':result-index="searchResultIndex"');
    expect(source).toContain(':result-count="searchResultCount"');
    expect(source).toContain('@find="onSearchFind"');
    expect(source).toContain('@close="onSearchClose"');
  });

  test("the search bar is positioned inside the terminal host", () => {
    expect(searchBarSource).toMatch(/position:\s*absolute/);
  });

  test("App drills a monotonic search request through PaneGrid", () => {
    expect(appSource).toMatch(/const terminalSearchSeq = ref\(0\)/);
    expect(appSource).toMatch(/onTerminalSearch: \(\) => \{\s*terminalSearchSeq\.value\+\+;/);
    expect(appSource).toContain(':search-request-seq="terminalSearchSeq"');
    expect(paneSource).toContain("searchRequestSeq?: number");
    expect(paneSource).toContain(':search-request-seq="searchRequestSeq"');
  });
});
```

- [ ] **Step 3: 跑测试确认失败**

Run: `npx vitest run src/components/TerminalView.test.ts`
Expected: FAIL —— 第一条就断在找不到 `import { SearchAddon }`。

- [ ] **Step 4: 在 TerminalView 里加载 addon 并管理搜索状态**

`src/components/TerminalView.vue`：

(a) 在 `import { WebglAddon } from "xterm-addon-webgl";`（第 7 行）之后加：

```ts
import { SearchAddon } from "xterm-addon-search";
```

并在其它组件 import 处加：

```ts
import TerminalSearchBar from "./TerminalSearchBar.vue";
```

(b) 在 `defineProps<{...}>()` 里，`resizeSuspended?: boolean;` 之后加：

```ts
    // Monotonic counter bumped by App.vue whenever the search shortcut fires.
    // Every pane sees the same value; only the focused one opens its bar.
    searchRequestSeq?: number;
```

并在同一个 `withDefaults` 的默认值对象里加 `searchRequestSeq: 0`。

(c) 在 `let fit: FitAddon | null = null;`（约 162 行）附近加模块级状态：

```ts
let searchAddon: SearchAddon | null = null;
const searchOpen = ref(false);
const searchFocusSeq = ref(0);
const searchResultIndex = ref(-1);
const searchResultCount = ref(0);
```

(d) 在 `ensureTerm()` 里，`term.loadAddon(fit);` 之后、`term.open(...)` 之前加：

```ts
  searchAddon = new SearchAddon();
  term.loadAddon(searchAddon);
  searchAddon.onDidChangeResults(({ resultIndex, resultCount }) => {
    searchResultIndex.value = resultIndex;
    searchResultCount.value = resultCount;
  });
```

(e) 在 script 的函数区加搜索处理逻辑：

```ts
// Match colours are fixed rather than theme-derived: the search overlay must
// stay legible on every terminal theme, and xterm needs concrete hex values
// for the overview ruler.
const SEARCH_DECORATIONS = {
  matchBackground: "#3a3f4b",
  matchBorder: "#5a6272",
  matchOverviewRuler: "#5a6272",
  activeMatchBackground: "#d19a66",
  activeMatchBorder: "#d19a66",
  activeMatchColorOverviewRuler: "#d19a66",
} as const;

function onSearchFind(query: string, dir: "next" | "prev", incremental: boolean) {
  if (!searchAddon) return;
  if (!query) {
    searchAddon.clearDecorations();
    searchResultIndex.value = -1;
    searchResultCount.value = 0;
    return;
  }
  const opts = { incremental, decorations: SEARCH_DECORATIONS };
  if (dir === "next") searchAddon.findNext(query, opts);
  else searchAddon.findPrevious(query, opts);
}

function onSearchClose() {
  searchOpen.value = false;
  searchAddon?.clearDecorations();
  searchResultIndex.value = -1;
  searchResultCount.value = 0;
  term?.focus();
}

watch(
  () => props.searchRequestSeq,
  () => {
    if (!props.focused) return;
    searchOpen.value = true;
    searchFocusSeq.value++;
  },
);
```

确认 `ref` 和 `watch` 已在文件顶部从 `vue` import；缺就补。

(f) 在模板里，根元素 `<div class="term-view" :class="{ focused }">` 内部、已有的 `class="overlay"` 那个 `<div>` 之后，加：

```vue
      <TerminalSearchBar
        :open="searchOpen"
        :focus-seq="searchFocusSeq"
        :result-index="searchResultIndex"
        :result-count="searchResultCount"
        @find="onSearchFind"
        @close="onSearchClose"
      />
```

`.term-view` 已经是 `position: absolute; inset: 0`（`TerminalView.vue` 样式块首条规则），是搜索栏 `position: absolute` 的定位上下文，**不需要**再改它的 CSS。

- [ ] **Step 5: PaneGrid 透传**

`src/components/PaneGrid.vue`：在 `defineProps` 里加

```ts
  searchRequestSeq?: number;
```

在 `<TerminalView ... />` 的属性列表里（`:resize-suspended="dragging"` 那行之后）加：

```vue
          :search-request-seq="searchRequestSeq"
```

- [ ] **Step 6: App.vue 产生请求**

`src/App.vue`：

(a) 在其它 `ref` 声明附近加：

```ts
// Bumped on every terminal-search shortcut press. Drilled down to every pane;
// only the focused one reacts (TerminalView checks props.focused).
const terminalSearchSeq = ref(0);
```

(b) 在 `useTerminalShortcuts({...})` 的 handlers 对象里，`onFocusSidebarSearch` 之后加：

```ts
    onTerminalSearch: () => {
      terminalSearchSeq.value++;
    },
```

(c) 在 `<PaneGrid ... />` 的属性列表里加：

```vue
        :search-request-seq="terminalSearchSeq"
```

- [ ] **Step 7: 跑测试确认通过**

Run: `npx vitest run src/components/TerminalView.test.ts`
Expected: PASS

- [ ] **Step 8: 跑全量前端测试 + 类型检查**

Run: `npm test && npm run build`
Expected: 全绿、构建成功。若 `SettingsShortcuts.test.ts` 因为 pane 分组多了一行而失败，更新该测试里的期望条数——这是 Task 1 引入的预期变化。

- [ ] **Step 9: 手动验证**

Run（在仓库根）：`cd desktop && wails dev -tags webkit2_41`（macOS/Windows 省略 `-tags`）

逐条确认：
1. `Cmd+F`（Linux/Windows `Ctrl+F`）打开搜索栏，输入框已聚焦
2. 输入一段 scrollback 里存在的文字 → 匹配高亮，计数显示 `1/N`
3. Enter 跳下一个、Shift+Enter 跳上一个，计数跟着变，活动匹配颜色与其它匹配不同
4. 输入不存在的字符串 → 显示「无匹配」，上下按钮禁用
5. Esc 关闭 → 高亮清除，焦点回到终端，可以直接继续打字
6. 搜索栏开着时再按一次 `Cmd+F` → 输入框重新聚焦且内容全选
7. 分屏后只有聚焦的那个 pane 弹出搜索栏
8. `Cmd+Shift+F` 打开侧栏搜索（原 `Cmd+F` 的功能已迁移到这里）

- [ ] **Step 10: 重建 web 嵌入产物**

`desktop/frontend/` 的改动会改变 web 构建输出，CI 的 embed drift job 会比对。

Run（在仓库根，需 Node 20）：

```bash
nvm use 20
./scripts/build-web.sh
git status --short internal/relay/web-dist | head
```

Expected: `internal/relay/web-dist/` 下有文件变更。

- [ ] **Step 11: 勾掉 roadmap**

`docs/roadmap.md` 第 19 项「scrollback 搜索」下的四个复选框全部改为 `- [x]`。

- [ ] **Step 12: 提交**

```bash
cd "$(git rev-parse --show-toplevel)"
git add desktop/frontend/package.json desktop/frontend/package-lock.json \
        desktop/frontend/src/components/TerminalView.vue \
        desktop/frontend/src/components/TerminalView.test.ts \
        desktop/frontend/src/components/PaneGrid.vue \
        desktop/frontend/src/App.vue \
        internal/relay/web-dist docs/roadmap.md
git commit -m "feat(terminal): searchable scrollback with match highlighting and counts"
```
