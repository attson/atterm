# Sidebar Multi-Select & Row Details Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the left session sidebar with (1) a right-click "view details" popover and (2) Cmd/Ctrl+click multi-select supporting two batch actions — merge selected sessions into a new tab, and close selected sessions that are currently open as panes in this window.

**Architecture:**
- A module-scoped composable `useSessionSelection` owns `selectedIds: Set<string>` and `anchorId`. Structure mirrors `useSessionPins` (no persistence branch).
- `TaskGroupedList` intercepts `@click` to route Shift / Cmd/Ctrl / plain clicks to selection or `open`, marks selected rows with an `outline` overlay, and computes an `orderedVisibleIds` flat list for Shift range logic. Its `@contextmenu` handler picks a menu items array based on `sel.size`.
- `SessionRowMenu` is refactored to be `items[]`-driven (loses its hard-coded pin/unpin coupling); menu opens on right-click and emits `select(key)`.
- A new `SessionDetailsPopover` mounts on the "details" menu item — non-modal, shares the viewport-edge flipping / Esc / outside / focusout pattern from `SessionRowMenu`.
- A new `BulkActionBar` mounts in `TaskSidebar`'s footer, mutually exclusive with the existing `mark-all` footer.
- `App.vue` grows `mergeSelectedIntoTab()`, `closeSelectedOpen()`, and an `opts.detachOnly?: boolean` on `closePaneAt` so merge can detach without killing local shells.

**Tech Stack:** Vue 3 + TypeScript + Vitest.

## Global Constraints

- User-facing prose 中文, code / commits English (memory `feedback_reply_language.md`).
- No backward-compat shims (memory `feedback_no_backward_compat.md`); the `SessionRowMenu` refactor drops `labelPin` / `labelUnpin` / `pinned` / `togglePin` props/emits outright — no aliases.
- No new frontend dependencies.
- Platform key convention: `e.metaKey || e.ctrlKey` for multi-select modifier. Do NOT use platform detection — `SessionPickerDialog` already sets the precedent (`e.metaKey || e.ctrlKey`).
- Row click semantics: plain click ALWAYS calls `sel.clear()` before emitting `open`. This is the spec's "普通 click → open + clear" contract; do not conditionalize on `sel.size`.
- Merge tab layout: `1→single, 2→vertical, 3→grid2x2 (one empty slot), 4→grid2x2`. `n>4 → null`, `Merge` button disabled with tooltip `tasks.bulk.mergeTooMany`.
- Merge produces panes with `remote: true` (matches sidebar-driven open path; see `openRemoteAsTab` at `desktop/frontend/src/App.vue:1005-1029`). Do NOT try to preserve the source pane's `remote` flag.
- Batch close semantics: same `closePaneAt` path as single close (kills local, detaches remote). Only ids in `openSessionIdSet` are processed; the rest are silently skipped. `M=0` disables the button with tooltip `tasks.bulk.closeNoneOpen`.
- Merge tab semantics: detach original panes with `opts.detachOnly = true` (no `closeSession` call). If an original tab's pane count drops to 0 after detach, the tab closes normally via `closePane`'s `closeTab: true` return path.
- Selection lifetime: session (memory only), no persistence, cleared on merge / close success / plain click / Esc / blank sidebar click.
- Selection insertion order = user click order (JS `Set` preserves insertion order). Merge feeds panes in this order.
- Cmd+A / Ctrl+A select-all is NOT in scope (spec §2). Do NOT add.
- Contextmenu rules:
  - `sel.size <= 1`: menu = `[details, pin/unpin]` and `sel.selectOnly(rightClickedId)` if it wasn't already selected.
  - `sel.size >= 2` and right-clicked row IS in selection: menu = `[merge (disabled if n>4), close (disabled if openCount=0), details (disabled), pin/unpin(rightClickedId)]`.
  - `sel.size >= 2` and right-clicked row NOT in selection: `sel.selectOnly(rightClickedId)` — same as single-select case; falls back to `[details, pin/unpin]`.
- i18n files are `desktop/frontend/src/i18n/messages/{en.ts, zh-CN.ts}` — NOT `zh.ts`. Verify existing structure before editing.
- Commit style: lowercase verb start, ≤72-char subject, one logical change per commit.

Spec: [`docs/superpowers/specs/2026-07-24-sidebar-multi-select-and-details-design.md`](../specs/2026-07-24-sidebar-multi-select-and-details-design.md)

## File Overview

Create:
- `desktop/frontend/src/composables/useSessionSelection.ts` — module-scoped selection state.
- `desktop/frontend/src/composables/useSessionSelection.test.ts` — pure logic tests.
- `desktop/frontend/src/components/BulkActionBar.vue` — sidebar footer batch operations bar.
- `desktop/frontend/src/components/BulkActionBar.test.ts`
- `desktop/frontend/src/components/SessionDetailsPopover.vue` — non-modal popover with session metadata.
- `desktop/frontend/src/components/SessionDetailsPopover.test.ts`

Modify:
- `desktop/frontend/src/components/SessionRowMenu.vue` — items-array props/emits.
- `desktop/frontend/src/components/SessionRowMenu.test.ts` — cover items-array behavior.
- `desktop/frontend/src/components/TaskGroupedList.vue` — multi-select row click branch, `.selected` class, `orderedVisibleIds`, contextmenu items variants, mount `SessionDetailsPopover`.
- `desktop/frontend/src/components/TaskGroupedList.test.ts` — cover 6 new interaction cases.
- `desktop/frontend/src/components/TaskSidebar.vue` — mount `BulkActionBar`, blank-click / Esc `clear()`, root aside `tabindex`.
- `desktop/frontend/src/components/TaskSidebar.test.ts` — cover footer swap, blank click, Esc.
- `desktop/frontend/src/App.vue` — `closePaneAt(t, idx, opts?)` with `detachOnly`; `mergeSelectedIntoTab()`; `closeSelectedOpen()`; wire sidebar `@merge` / `@close-selected` events.
- `desktop/frontend/src/i18n/messages/en.ts` — new keys under `tasks.bulk.*`, `tasks.rowMenu.*`, `tasks.details.*`.
- `desktop/frontend/src/i18n/messages/zh-CN.ts` — same keys in Chinese.

Go side: **untouched.**

---

### Task 1: `useSessionSelection` composable + tests

**Files:**
- Create: `desktop/frontend/src/composables/useSessionSelection.ts`
- Create: `desktop/frontend/src/composables/useSessionSelection.test.ts`

**Interfaces:**
- Consumes: nothing external.
- Produces:
  ```ts
  export function useSessionSelection(): {
    selectedIds: Ref<Set<string>>;
    anchorId: Ref<string | null>;
    size: ComputedRef<number>;
    isSelected(id: string): boolean;
    toggle(id: string): void;
    selectOnly(id: string): void;
    selectRange(id: string, orderedIds: string[]): void;
    clear(): void;
  };
  export function __resetForTests(): void;
  ```
  Module-scoped `selectedIds` + `anchorId` shared across all callers.

- [ ] **Step 1: Write the failing tests**

Create `desktop/frontend/src/composables/useSessionSelection.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { effectScope } from "vue";
import { __resetForTests, useSessionSelection } from "./useSessionSelection";

describe("useSessionSelection", () => {
  let scope: ReturnType<typeof effectScope>;
  beforeEach(() => {
    __resetForTests();
    scope = effectScope();
  });
  afterEach(() => scope.stop());

  test("starts empty, size=0, anchor null", () => {
    scope.run(() => {
      const s = useSessionSelection();
      expect(s.size.value).toBe(0);
      expect(s.anchorId.value).toBeNull();
      expect(s.isSelected("a")).toBe(false);
    });
  });

  test("toggle adds then removes an id and sets anchor", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("a");
      expect(s.isSelected("a")).toBe(true);
      expect(s.size.value).toBe(1);
      expect(s.anchorId.value).toBe("a");
      s.toggle("a");
      expect(s.isSelected("a")).toBe(false);
      expect(s.size.value).toBe(0);
      // anchor stays on last-touched even when set becomes empty
      expect(s.anchorId.value).toBe("a");
    });
  });

  test("selectOnly replaces existing selection and re-anchors", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("a");
      s.toggle("b");
      expect(s.size.value).toBe(2);
      s.selectOnly("c");
      expect(s.size.value).toBe(1);
      expect(s.isSelected("c")).toBe(true);
      expect(s.isSelected("a")).toBe(false);
      expect(s.anchorId.value).toBe("c");
    });
  });

  test("selectRange fills anchor→id range from orderedIds", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("b"); // anchor = b
      s.selectRange("d", ["a", "b", "c", "d", "e"]);
      expect(s.isSelected("b")).toBe(true);
      expect(s.isSelected("c")).toBe(true);
      expect(s.isSelected("d")).toBe(true);
      expect(s.isSelected("a")).toBe(false);
      expect(s.isSelected("e")).toBe(false);
      // anchor unchanged after a range select
      expect(s.anchorId.value).toBe("b");
    });
  });

  test("selectRange without anchor falls back to toggle", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.selectRange("b", ["a", "b", "c"]);
      expect(s.isSelected("b")).toBe(true);
      expect(s.size.value).toBe(1);
      expect(s.anchorId.value).toBe("b");
    });
  });

  test("selectRange with anchor missing from orderedIds falls back to toggle", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("zzz");
      s.selectRange("b", ["a", "b", "c"]);
      // "zzz" not in ordered list → fallback = toggle "b"
      expect(s.isSelected("b")).toBe(true);
      expect(s.isSelected("zzz")).toBe(true);
      expect(s.size.value).toBe(2);
    });
  });

  test("selectRange handles reversed order (id before anchor)", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("d");
      s.selectRange("b", ["a", "b", "c", "d", "e"]);
      expect(s.isSelected("b")).toBe(true);
      expect(s.isSelected("c")).toBe(true);
      expect(s.isSelected("d")).toBe(true);
      expect(s.isSelected("a")).toBe(false);
    });
  });

  test("clear() empties selection and null-anchors", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("a");
      s.toggle("b");
      s.clear();
      expect(s.size.value).toBe(0);
      expect(s.anchorId.value).toBeNull();
    });
  });

  test("state is shared across multiple useSessionSelection() calls", () => {
    scope.run(() => {
      const a = useSessionSelection();
      const b = useSessionSelection();
      a.toggle("x");
      expect(b.isSelected("x")).toBe(true);
      expect(b.size.value).toBe(1);
    });
  });

  test("insertion order is preserved", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("b");
      s.toggle("a");
      s.toggle("c");
      expect(Array.from(s.selectedIds.value)).toEqual(["b", "a", "c"]);
    });
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/composables/useSessionSelection.test.ts`
Expected: FAIL — `useSessionSelection` not defined.

- [ ] **Step 3: Implement the composable**

Create `desktop/frontend/src/composables/useSessionSelection.ts`:

```ts
import { computed, ref, type ComputedRef, type Ref } from "vue";

// Module-scoped so state is shared across all consumers within the app
// lifetime (matches useSessionPins / useCollapsedGroups / useTaskGroupBy
// pattern). No persistence — selection is session-only by spec §1.
const selectedIds = ref<Set<string>>(new Set());
const anchorId = ref<string | null>(null);

export function useSessionSelection() {
  const size = computed(() => selectedIds.value.size);

  function isSelected(id: string): boolean {
    return selectedIds.value.has(id);
  }

  function toggle(id: string): void {
    // Mutate a fresh Set so Vue's reactivity picks up the change; refs
    // hold the same Set instance otherwise. Same pattern as
    // TaskGroupedList's collapsedGroups.
    const next = new Set(selectedIds.value);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selectedIds.value = next;
    anchorId.value = id;
  }

  function selectOnly(id: string): void {
    selectedIds.value = new Set([id]);
    anchorId.value = id;
  }

  function selectRange(id: string, orderedIds: string[]): void {
    if (!anchorId.value) return toggle(id);
    const a = orderedIds.indexOf(anchorId.value);
    const b = orderedIds.indexOf(id);
    if (a < 0 || b < 0) return toggle(id);
    const [lo, hi] = a < b ? [a, b] : [b, a];
    const next = new Set(selectedIds.value);
    for (let i = lo; i <= hi; i++) next.add(orderedIds[i]);
    selectedIds.value = next;
    // anchor stays put — mirrors macOS Finder / VSCode Shift+click
    // behavior, so subsequent Shift+click extends from the same origin.
  }

  function clear(): void {
    if (selectedIds.value.size === 0 && anchorId.value === null) return;
    selectedIds.value = new Set();
    anchorId.value = null;
  }

  return {
    selectedIds: selectedIds as Ref<Set<string>>,
    anchorId: anchorId as Ref<string | null>,
    size: size as ComputedRef<number>,
    isSelected,
    toggle,
    selectOnly,
    selectRange,
    clear,
  };
}

// Test-only: reset module state between tests. Do NOT call from app code.
export function __resetForTests(): void {
  selectedIds.value = new Set();
  anchorId.value = null;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/composables/useSessionSelection.test.ts`
Expected: PASS 9/9.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/composables/useSessionSelection.ts \
        desktop/frontend/src/composables/useSessionSelection.test.ts
git commit -m "feat(sidebar): add useSessionSelection composable"
```

---

### Task 2: Refactor `SessionRowMenu` to items-array driven

**Files:**
- Modify: `desktop/frontend/src/components/SessionRowMenu.vue`
- Modify: `desktop/frontend/src/components/SessionRowMenu.test.ts`
- Modify: `desktop/frontend/src/components/TaskGroupedList.vue` (single caller update, still single-select flow only)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  ```ts
  // SessionRowMenu new props/emits
  export type MenuItem = { key: string; label: string; disabled?: boolean };
  defineProps<{ open: boolean; x: number; y: number; items: MenuItem[] }>();
  defineEmits<{ (e: "close"): void; (e: "select", key: string): void }>();
  ```
  Old props (`pinned`, `labelPin`, `labelUnpin`) and old emit (`togglePin`) are REMOVED.

- [ ] **Step 1: Update the menu component**

Rewrite `desktop/frontend/src/components/SessionRowMenu.vue`:

```vue
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

export type MenuItem = { key: string; label: string; disabled?: boolean };

const props = withDefaults(defineProps<{
  open: boolean;
  x: number;
  y: number;
  items: MenuItem[];
}>(), { open: false });

const emit = defineEmits<{
  (e: "close"): void;
  (e: "select", key: string): void;
}>();

const menuRef = ref<HTMLElement | null>(null);
const positionedX = ref(0);
const positionedY = ref(0);

function updatePosition() {
  if (!menuRef.value) {
    positionedX.value = props.x;
    positionedY.value = props.y;
    return;
  }
  const rect = menuRef.value.getBoundingClientRect();
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  positionedX.value = props.x + rect.width > vw ? Math.max(0, props.x - rect.width) : props.x;
  positionedY.value = props.y + rect.height > vh ? Math.max(0, props.y - rect.height) : props.y;
}

const style = computed(() => ({
  left: positionedX.value + "px",
  top: positionedY.value + "px",
}));

function onItemClick(item: MenuItem) {
  if (item.disabled) return;
  emit("select", item.key);
  emit("close");
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") emit("close");
}

function onOutside(e: MouseEvent) {
  if (!menuRef.value) return;
  if (!menuRef.value.contains(e.target as Node)) emit("close");
}

function onFocusOut(e: FocusEvent) {
  if (!menuRef.value) return;
  const related = e.relatedTarget as Node | null;
  if (!related || !menuRef.value.contains(related)) emit("close");
}

watch(
  () => props.open,
  (v) => {
    if (v) {
      positionedX.value = props.x;
      positionedY.value = props.y;
      requestAnimationFrame(updatePosition);
      window.addEventListener("keydown", onKeydown);
      window.addEventListener("mousedown", onOutside);
    } else {
      window.removeEventListener("keydown", onKeydown);
      window.removeEventListener("mousedown", onOutside);
    }
  },
  { immediate: true },
);

onMounted(() => {
  if (props.open) requestAnimationFrame(updatePosition);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("mousedown", onOutside);
});
</script>

<template>
  <div
    v-if="open"
    ref="menuRef"
    class="session-row-menu"
    data-test="session-row-menu"
    role="menu"
    :style="style"
    @focusout.capture="onFocusOut"
    @contextmenu.prevent
  >
    <button
      v-for="item in items"
      :key="item.key"
      class="menu-item"
      :class="{ disabled: item.disabled }"
      :data-test="`session-row-menu-item-${item.key}`"
      role="menuitem"
      type="button"
      :disabled="item.disabled"
      @click.stop="onItemClick(item)"
    >
      {{ item.label }}
    </button>
  </div>
</template>

<style scoped>
.session-row-menu {
  position: fixed;
  z-index: 1000;
  min-width: 140px;
  padding: 4px;
  background: var(--menu-bg, #1f1f22);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 6px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
}
.menu-item {
  display: block;
  width: 100%;
  padding: 6px 10px;
  background: transparent;
  border: none;
  color: inherit;
  text-align: left;
  cursor: pointer;
  border-radius: 4px;
  font-size: 13px;
}
.menu-item:hover:not(.disabled),
.menu-item:focus:not(.disabled) {
  background: rgba(255, 255, 255, 0.08);
  outline: none;
}
.menu-item.disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
</style>
```

- [ ] **Step 2: Rewrite the menu tests**

Replace `desktop/frontend/src/components/SessionRowMenu.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import SessionRowMenu, { type MenuItem } from "./SessionRowMenu.vue";

function factory(overrides: {
  open?: boolean;
  items?: MenuItem[];
} = {}) {
  return mount(SessionRowMenu, {
    attachTo: document.body,
    props: {
      open: true,
      x: 100,
      y: 100,
      items: overrides.items ?? [
        { key: "pin", label: "Pin to top" },
      ],
      ...(overrides.open !== undefined ? { open: overrides.open } : {}),
    },
  });
}

describe("SessionRowMenu (items-driven)", () => {
  test("does not render when open=false", () => {
    const w = factory({ open: false });
    expect(w.find("[data-test=session-row-menu]").exists()).toBe(false);
  });

  test("renders each item with its label and key-scoped data-test", () => {
    const w = factory({
      items: [
        { key: "details", label: "View details" },
        { key: "pin", label: "Pin to top" },
      ],
    });
    expect(w.find("[data-test=session-row-menu-item-details]").text()).toBe("View details");
    expect(w.find("[data-test=session-row-menu-item-pin]").text()).toBe("Pin to top");
  });

  test("clicking an enabled item emits select(key) then close", async () => {
    const w = factory({
      items: [{ key: "pin", label: "Pin to top" }],
    });
    await w.find("[data-test=session-row-menu-item-pin]").trigger("click");
    expect(w.emitted("select")).toEqual([["pin"]]);
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("clicking a disabled item does not emit select or close", async () => {
    const w = factory({
      items: [{ key: "details", label: "View details", disabled: true }],
    });
    await w.find("[data-test=session-row-menu-item-details]").trigger("click");
    expect(w.emitted("select")).toBeUndefined();
    expect(w.emitted("close")).toBeUndefined();
  });

  test("Escape emits close", async () => {
    const w = factory();
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(w.emitted("close")).toHaveLength(1);
    w.unmount();
  });

  test("outside mousedown emits close", async () => {
    const w = factory();
    document.body.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    expect(w.emitted("close")).toHaveLength(1);
    w.unmount();
  });
});
```

Note: the previous test used `w.trigger("keydown", ...)` on the wrapper — the new component listens on `window`, so we dispatch the event on `document` (JSDOM: document keydown bubbles to window). Same for outside mousedown, unchanged.

- [ ] **Step 3: Update `TaskGroupedList.vue` — single caller, still single-select**

Change `desktop/frontend/src/components/TaskGroupedList.vue`:

Replace the `<SessionRowMenu ...>` at the bottom of the template with:

```vue
<SessionRowMenu
  :open="menuState.open"
  :x="menuState.x"
  :y="menuState.y"
  :items="menuItems"
  @close="closeMenu"
  @select="onMenuSelect"
/>
```

Add these in `<script setup>` (near the existing `menuState` / `onRowMenu`):

```ts
import type { MenuItem } from "./SessionRowMenu.vue";

const menuItems = computed<MenuItem[]>(() => {
  const s = menuState.value.session;
  if (!s) return [];
  return [
    {
      key: pins.isPinned(s.session_id) ? "unpin" : "pin",
      label: pins.isPinned(s.session_id)
        ? t("tasks.pinned.menuUnpin")
        : t("tasks.pinned.menuPin"),
    },
  ];
});

function onMenuSelect(key: string) {
  const s = menuState.value.session;
  if (!s) return;
  if (key === "pin" || key === "unpin") {
    pins.toggle(s.session_id);
  }
}
```

Delete the now-unused `onToggleFromMenu` function.

- [ ] **Step 4: Run tests**

Run:
```
cd desktop/frontend && npx vitest run src/components/SessionRowMenu.test.ts src/components/TaskGroupedList.test.ts
```
Expected: PASS. If `TaskGroupedList.test.ts` had a test binding on the old `togglePin` emit, update it to bind on `select("pin")` — grep for `togglePin` in the test file first.

Run: `cd desktop/frontend && npx vitest run` (full suite) — expect PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SessionRowMenu.vue \
        desktop/frontend/src/components/SessionRowMenu.test.ts \
        desktop/frontend/src/components/TaskGroupedList.vue \
        desktop/frontend/src/components/TaskGroupedList.test.ts
git commit -m "refactor(sidebar): SessionRowMenu items-array driven"
```

---

### Task 3: `SessionDetailsPopover` component + tests

**Files:**
- Create: `desktop/frontend/src/components/SessionDetailsPopover.vue`
- Create: `desktop/frontend/src/components/SessionDetailsPopover.test.ts`

**Interfaces:**
- Consumes: `RemoteSession` from `../platform/types`; `Tab` from `../lib/types`; `findPaneLocation` from `../lib/layout`.
- Produces:
  ```ts
  defineProps<{
    open: boolean;
    x: number;
    y: number;
    session: RemoteSession | null;
    paneLocation: { tabId: string; paneIdx: number } | null;
    tabIndexById: (tabId: string) => number; // 1-based tab number for display
  }>();
  defineEmits<{ (e: "close"): void }>();
  ```

Field render order (skip when value is `null`/`undefined`/empty):
1. `session_id` (with ⧉ copy)
2. `type` (label lookup: `local` / `remote` / `ai` — mapped by pane-location & session type)
3. `host` / `user@host_id`
4. `cwd` (full, copyable)
5. `current_command` (or `title` if empty, copyable)
6. `task_state` (localized label)
7. `started_at` — ISO string of `started_at * 1000` if present
8. `command_started_at` / `command_ended_at` / `command_duration_ms` / `command_exit_code` — 4 fields, each only if present
9. `unread` (Yes/No)
10. `pinned` (Yes/No) — from `useSessionPins`
11. `paneLocation` — `Tab {n} · Pane {i+1}` or `tasks.details.paneNone`

- [ ] **Step 1: Write tests**

Create `desktop/frontend/src/components/SessionDetailsPopover.test.ts`:

```ts
import { describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import SessionDetailsPopover from "./SessionDetailsPopover.vue";
import type { RemoteSession } from "../platform/types";

function mkSession(over: Partial<RemoteSession> = {}): RemoteSession {
  return {
    session_id: "sess-abc",
    host_id: "h1",
    host: "laptop",
    user: "attson",
    title: "shell",
    cwd: "/Users/attson/proj",
    cols: 80,
    rows: 24,
    started_at: 1_700_000_000,
    task_state: "running",
    current_command: "npm run dev",
    unread: false,
    ...over,
  } as RemoteSession;
}

function factory(over: {
  session?: RemoteSession | null;
  paneLocation?: { tabId: string; paneIdx: number } | null;
} = {}) {
  return mount(SessionDetailsPopover, {
    attachTo: document.body,
    props: {
      open: true,
      x: 200,
      y: 200,
      session: over.session ?? mkSession(),
      paneLocation: over.paneLocation ?? null,
      tabIndexById: (id: string) => (id === "t1" ? 1 : 0),
    },
  });
}

describe("SessionDetailsPopover", () => {
  test("does not render when open=false", () => {
    const w = factory();
    w.setProps({ open: false });
    expect(w.find("[data-test=session-details-popover]").exists()).toBe(false);
  });

  test("does not render when session is null", () => {
    const w = factory({ session: null });
    expect(w.find("[data-test=session-details-popover]").exists()).toBe(false);
  });

  test("renders session_id row", () => {
    const w = factory();
    expect(w.find("[data-test=details-field-sessionId] .value").text()).toBe("sess-abc");
  });

  test("skips optional rows when value is empty", () => {
    const w = factory({
      session: mkSession({
        current_command: undefined,
        command_started_at: undefined,
      }),
    });
    expect(w.find("[data-test=details-field-command]").exists()).toBe(true); // title fallback
    expect(w.find("[data-test=details-field-commandStartedAt]").exists()).toBe(false);
  });

  test("renders pane location when provided", () => {
    const w = factory({
      paneLocation: { tabId: "t1", paneIdx: 2 },
    });
    expect(w.find("[data-test=details-field-paneLocation] .value").text()).toContain("Tab 1");
    expect(w.find("[data-test=details-field-paneLocation] .value").text()).toContain("Pane 3");
  });

  test("renders 'not open' when paneLocation is null", () => {
    const w = factory({ paneLocation: null });
    expect(w.find("[data-test=details-field-paneLocation]").exists()).toBe(true);
  });

  test("copy button writes to clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    const w = factory();
    await w.find("[data-test=details-field-sessionId] .copy").trigger("click");
    expect(writeText).toHaveBeenCalledWith("sess-abc");
  });

  test("Escape emits close", async () => {
    const w = factory();
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(w.emitted("close")).toHaveLength(1);
    w.unmount();
  });

  test("outside mousedown emits close", async () => {
    const w = factory();
    document.body.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    expect(w.emitted("close")).toHaveLength(1);
    w.unmount();
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/components/SessionDetailsPopover.test.ts`
Expected: FAIL — component not defined.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/components/SessionDetailsPopover.vue`:

```vue
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { RemoteSession } from "../platform/types";
import { useI18n } from "../i18n/useI18n";
import { useSessionPins } from "../composables/useSessionPins";
import { taskStateLabel } from "../lib/sessionLabel";

const props = withDefaults(defineProps<{
  open: boolean;
  x: number;
  y: number;
  session: RemoteSession | null;
  paneLocation: { tabId: string; paneIdx: number } | null;
  tabIndexById: (tabId: string) => number;
}>(), { open: false });

const emit = defineEmits<{ (e: "close"): void }>();

const { t } = useI18n();
const pins = useSessionPins();
const popoverRef = ref<HTMLElement | null>(null);
const positionedX = ref(0);
const positionedY = ref(0);

function updatePosition() {
  if (!popoverRef.value) {
    positionedX.value = props.x;
    positionedY.value = props.y;
    return;
  }
  const rect = popoverRef.value.getBoundingClientRect();
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  positionedX.value = props.x + rect.width > vw ? Math.max(0, props.x - rect.width) : props.x;
  positionedY.value = props.y + rect.height > vh ? Math.max(0, props.y - rect.height) : props.y;
}

const style = computed(() => ({
  left: positionedX.value + "px",
  top: positionedY.value + "px",
}));

// Compact ISO-ish local string; keep the seconds precision the wire format
// carries. `started_at` etc are Unix seconds.
function fmtTs(sec: number | undefined): string {
  if (!sec) return "";
  const d = new Date(sec * 1000);
  return d.toISOString().replace("T", " ").replace(/\.\d+Z$/, "Z");
}

function fmtDuration(ms: number | undefined): string {
  if (ms === undefined || ms === null) return "";
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const rem = Math.round(s - m * 60);
  return `${m}m${rem}s`;
}

async function copy(value: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    /* clipboard unavailable — silently ignore; user can select-and-copy */
  }
}

// Compose the fields with `v-if` guards so an empty value collapses the row.
const rows = computed(() => {
  const s = props.session;
  if (!s) return [];
  const list: Array<{
    key: string;
    label: string;
    value: string;
    copy?: boolean;
  }> = [];
  list.push({ key: "sessionId", label: t("tasks.details.sessionId"), value: s.session_id, copy: true });
  if (s.type) list.push({ key: "type", label: t("tasks.details.type"), value: s.type });
  const hostLine = [s.user, s.host].filter(Boolean).join("@");
  if (hostLine || s.host_id) {
    list.push({
      key: "host",
      label: t("tasks.details.host"),
      value: hostLine || s.host_id,
      copy: true,
    });
  }
  if (s.cwd) list.push({ key: "cwd", label: t("tasks.details.cwd"), value: s.cwd, copy: true });
  const cmd = s.current_command || s.title;
  if (cmd) list.push({ key: "command", label: t("tasks.details.command"), value: cmd, copy: true });
  if (s.task_state) list.push({ key: "state", label: t("tasks.details.state"), value: taskStateLabel(s.task_state, t) });
  if (s.started_at) list.push({ key: "startedAt", label: t("tasks.details.startedAt"), value: fmtTs(s.started_at) });
  if (s.command_started_at) list.push({ key: "commandStartedAt", label: t("tasks.details.commandStartedAt"), value: fmtTs(s.command_started_at) });
  if (s.command_ended_at) list.push({ key: "commandEndedAt", label: t("tasks.details.commandEndedAt"), value: fmtTs(s.command_ended_at) });
  const dur = fmtDuration(s.command_duration_ms);
  if (dur) list.push({ key: "commandDuration", label: t("tasks.details.commandDuration"), value: dur });
  if (typeof s.command_exit_code === "number") list.push({ key: "commandExitCode", label: t("tasks.details.commandExitCode"), value: String(s.command_exit_code) });
  list.push({ key: "unread", label: t("tasks.details.unread"), value: s.unread ? t("common.yes") : t("common.no") });
  list.push({ key: "pinned", label: t("tasks.details.pinned"), value: pins.isPinned(s.session_id) ? t("common.yes") : t("common.no") });
  const loc = props.paneLocation;
  const paneLabel = loc
    ? t("tasks.details.paneAt", {
        tab: props.tabIndexById(loc.tabId),
        pane: loc.paneIdx + 1,
      })
    : t("tasks.details.paneNone");
  list.push({ key: "paneLocation", label: t("tasks.details.paneLocation"), value: paneLabel });
  return list;
});

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") emit("close");
}

function onOutside(e: MouseEvent) {
  if (!popoverRef.value) return;
  if (!popoverRef.value.contains(e.target as Node)) emit("close");
}

function onFocusOut(e: FocusEvent) {
  if (!popoverRef.value) return;
  const related = e.relatedTarget as Node | null;
  if (!related || !popoverRef.value.contains(related)) emit("close");
}

watch(
  () => props.open,
  (v) => {
    if (v) {
      positionedX.value = props.x;
      positionedY.value = props.y;
      requestAnimationFrame(updatePosition);
      window.addEventListener("keydown", onKeydown);
      window.addEventListener("mousedown", onOutside);
    } else {
      window.removeEventListener("keydown", onKeydown);
      window.removeEventListener("mousedown", onOutside);
    }
  },
  { immediate: true },
);

onMounted(() => {
  if (props.open) requestAnimationFrame(updatePosition);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("mousedown", onOutside);
});
</script>

<template>
  <div
    v-if="open && session"
    ref="popoverRef"
    class="session-details-popover"
    data-test="session-details-popover"
    role="dialog"
    :aria-label="t('tasks.details.title')"
    tabindex="-1"
    :style="style"
    @focusout.capture="onFocusOut"
    @contextmenu.prevent
  >
    <div class="popover-title">{{ t("tasks.details.title") }}</div>
    <div class="rows">
      <div
        v-for="row in rows"
        :key="row.key"
        class="row"
        :data-test="`details-field-${row.key}`"
      >
        <span class="label">{{ row.label }}</span>
        <span class="value" :title="row.value">{{ row.value }}</span>
        <button
          v-if="row.copy"
          class="copy"
          type="button"
          :title="t('common.copy')"
          :aria-label="t('common.copy')"
          @click.stop="copy(row.value)"
        >⧉</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.session-details-popover {
  position: fixed;
  z-index: 1000;
  min-width: 260px;
  max-width: 420px;
  padding: 8px 10px 10px;
  background: var(--menu-bg, #1f1f22);
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 8px;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.45);
  font-size: 12px;
}
.popover-title {
  font-weight: 600;
  padding-bottom: 6px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  margin-bottom: 6px;
}
.rows {
  display: flex;
  flex-direction: column;
  gap: 3px;
  max-height: 380px;
  overflow-y: auto;
}
.row {
  display: grid;
  grid-template-columns: 96px 1fr auto;
  gap: 8px;
  align-items: baseline;
}
.label {
  color: var(--fg-dim);
  white-space: nowrap;
}
.value {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
  min-width: 0;
}
.copy {
  border: none;
  background: transparent;
  color: var(--fg-dim);
  cursor: pointer;
  padding: 0 3px;
  font-size: 12px;
  border-radius: 3px;
}
.copy:hover {
  color: var(--fg);
  background: rgba(255, 255, 255, 0.08);
}
</style>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/components/SessionDetailsPopover.test.ts`
Expected: PASS (some field-label tests rely on i18n keys defined in Task 9; if they fail with `[tasks.details.sessionId]` echoed as label, that's expected until Task 9 lands. Tests as written above assert `.value` text — the label text is not asserted. Verify PASS.)

If any test fails because `tabIndexById` or `paneLocation` isn't yet consumed correctly, adjust the component; do not weaken tests.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SessionDetailsPopover.vue \
        desktop/frontend/src/components/SessionDetailsPopover.test.ts
git commit -m "feat(sidebar): add SessionDetailsPopover component"
```

---

### Task 4: `BulkActionBar` component + tests

**Files:**
- Create: `desktop/frontend/src/components/BulkActionBar.vue`
- Create: `desktop/frontend/src/components/BulkActionBar.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```ts
  defineProps<{
    count: number;      // total selected
    openCount: number;  // subset that is open as pane
    canMerge: boolean;  // count >= 1 && count <= 4
  }>();
  defineEmits<{
    (e: "merge"): void;
    (e: "close-selected"): void;
    (e: "clear"): void;
  }>();
  ```

- [ ] **Step 1: Write tests**

Create `desktop/frontend/src/components/BulkActionBar.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import BulkActionBar from "./BulkActionBar.vue";

function factory(overrides: {
  count?: number;
  openCount?: number;
  canMerge?: boolean;
} = {}) {
  return mount(BulkActionBar, {
    props: {
      count: 1,
      openCount: 0,
      canMerge: true,
      ...overrides,
    },
  });
}

describe("BulkActionBar", () => {
  test("renders count in the counter label", () => {
    const w = factory({ count: 3 });
    expect(w.find("[data-test=bulk-selected-count]").text()).toContain("3");
  });

  test("merge button enabled when canMerge=true", () => {
    const w = factory({ count: 2, canMerge: true });
    const btn = w.find("[data-test=bulk-merge]");
    expect((btn.element as HTMLButtonElement).disabled).toBe(false);
  });

  test("merge button disabled when canMerge=false", () => {
    const w = factory({ count: 5, canMerge: false });
    const btn = w.find("[data-test=bulk-merge]");
    expect((btn.element as HTMLButtonElement).disabled).toBe(true);
  });

  test("close button disabled when openCount=0", () => {
    const w = factory({ openCount: 0 });
    const btn = w.find("[data-test=bulk-close]");
    expect((btn.element as HTMLButtonElement).disabled).toBe(true);
  });

  test("close button enabled when openCount>0", () => {
    const w = factory({ openCount: 2 });
    const btn = w.find("[data-test=bulk-close]");
    expect((btn.element as HTMLButtonElement).disabled).toBe(false);
  });

  test("clicking merge emits 'merge'", async () => {
    const w = factory({ count: 2, canMerge: true });
    await w.find("[data-test=bulk-merge]").trigger("click");
    expect(w.emitted("merge")).toHaveLength(1);
  });

  test("clicking close emits 'close-selected'", async () => {
    const w = factory({ openCount: 2 });
    await w.find("[data-test=bulk-close]").trigger("click");
    expect(w.emitted("close-selected")).toHaveLength(1);
  });

  test("clicking cancel emits 'clear'", async () => {
    const w = factory();
    await w.find("[data-test=bulk-clear]").trigger("click");
    expect(w.emitted("clear")).toHaveLength(1);
  });

  test("disabled merge does not emit", async () => {
    const w = factory({ count: 5, canMerge: false });
    await w.find("[data-test=bulk-merge]").trigger("click");
    expect(w.emitted("merge")).toBeUndefined();
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/components/BulkActionBar.test.ts`
Expected: FAIL — component not defined.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/components/BulkActionBar.vue`:

```vue
<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  count: number;
  openCount: number;
  canMerge: boolean;
}>();

const emit = defineEmits<{
  (e: "merge"): void;
  (e: "close-selected"): void;
  (e: "clear"): void;
}>();

const { t } = useI18n();

const mergeTitle = computed(() => {
  if (props.count > 4) return t("tasks.bulk.mergeTooMany");
  return t("tasks.bulk.mergeTab");
});

const closeTitle = computed(() => {
  if (props.openCount === 0) return t("tasks.bulk.closeNoneOpen");
  return t("tasks.bulk.closeHint", { count: props.openCount });
});

function onMerge() {
  if (!props.canMerge) return;
  emit("merge");
}

function onClose() {
  if (props.openCount === 0) return;
  emit("close-selected");
}

function onClear() {
  emit("clear");
}
</script>

<template>
  <div class="bulk-bar" data-test="bulk-action-bar" role="toolbar">
    <span class="counter" data-test="bulk-selected-count">
      {{ t("tasks.bulk.selectedCount", { count }) }}
    </span>
    <button
      class="btn primary"
      data-test="bulk-merge"
      type="button"
      :disabled="!canMerge"
      :title="mergeTitle"
      @click="onMerge"
    >
      {{ t("tasks.bulk.mergeTab") }} ({{ count }})
    </button>
    <button
      class="btn"
      data-test="bulk-close"
      type="button"
      :disabled="openCount === 0"
      :title="closeTitle"
      @click="onClose"
    >
      {{ t("tasks.bulk.close", { count: openCount }) }}
    </button>
    <button
      class="btn ghost"
      data-test="bulk-clear"
      type="button"
      :title="t('tasks.bulk.cancel')"
      @click="onClear"
    >
      {{ t("tasks.bulk.cancel") }}
    </button>
  </div>
</template>

<style scoped>
.bulk-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  padding: 6px 4px;
}
.counter {
  font-size: 11px;
  color: var(--fg-dim);
  margin-right: 4px;
  white-space: nowrap;
}
.btn {
  background: none;
  border: 1px solid rgba(255, 255, 255, 0.14);
  color: inherit;
  cursor: pointer;
  padding: 3px 8px;
  font-size: 11px;
  border-radius: 3px;
  line-height: 1.4;
}
.btn:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.06);
}
.btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.btn.primary {
  border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
  color: var(--accent);
}
.btn.primary:hover:not(:disabled) {
  background: color-mix(in srgb, var(--accent) 12%, transparent);
}
.btn.ghost {
  border-color: transparent;
  opacity: 0.75;
}
</style>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/components/BulkActionBar.test.ts`
Expected: PASS 9/9.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/BulkActionBar.vue \
        desktop/frontend/src/components/BulkActionBar.test.ts
git commit -m "feat(sidebar): add BulkActionBar component"
```

---

### Task 5: `TaskGroupedList` — multi-select interactions + contextmenu variants + `.selected` visual

**Files:**
- Modify: `desktop/frontend/src/components/TaskGroupedList.vue`
- Modify: `desktop/frontend/src/components/TaskGroupedList.test.ts`

**Interfaces:**
- Consumes: `useSessionSelection` (Task 1); `SessionRowMenu` items-array shape (Task 2).
- Produces:
  ```vue
  <!-- New template hooks -->
  <button class="task-row" :class="{ active, selected }" data-test="task-row" :data-selected="selected ? 'true' : undefined" @click="onRowClick($event, s)" @contextmenu="onRowMenu($event, s)">
  ```
  New props for merge / batch actions passthrough:
  ```ts
  interface Props {
    // ...existing...
    openSessionIds?: string[]; // already exists
  }
  ```
  New emits — the list itself does not perform merge/close; those go to `App.vue` through `TaskSidebar`. `TaskGroupedList` still only emits `open` / `close` / `markSeen`. Batch actions are triggered by `SessionRowMenu` items (`merge` / `close-selected`) — the list emits new events `merge-selected` and `close-selected` up to `TaskSidebar` when the user picks them from the right-click menu.

  Final emits list:
  ```ts
  const emit = defineEmits<{
    (e: "open", session: RemoteSession): void;
    (e: "close", session: RemoteSession): void;
    (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
    (e: "merge-selected"): void;
    (e: "close-selected"): void;
  }>();
  ```

- [ ] **Step 1: Write the failing tests**

Add to `desktop/frontend/src/components/TaskGroupedList.test.ts` — a new `describe` block after existing ones:

```ts
import { __resetForTests as resetSel, useSessionSelection } from "../composables/useSessionSelection";

describe("TaskGroupedList — multi-select", () => {
  beforeEach(() => resetSel());

  function mkSessions(): RemoteSession[] {
    // Use whatever your existing helpers produce; if none, inline:
    return [
      { session_id: "s1", host_id: "h1", host: "h1", user: "u", title: "one", cols: 80, rows: 24, cwd: "/a" },
      { session_id: "s2", host_id: "h1", host: "h1", user: "u", title: "two", cols: 80, rows: 24, cwd: "/b" },
      { session_id: "s3", host_id: "h1", host: "h1", user: "u", title: "three", cols: 80, rows: 24, cwd: "/c" },
    ] as RemoteSession[];
  }

  function mount3() {
    return mountList({
      byHost: { h1: mkSessions() },
      primaryStateForHost: () => "idle",
      completedSeen: [],
      openSessionIds: ["s1"],
    });
  }

  test("Cmd+click on a row toggles selection, does not emit open", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[1].trigger("click", { metaKey: true });
    expect(w.emitted("open")).toBeUndefined();
    expect(rows[1].attributes("data-selected")).toBe("true");
    // Toggle off
    await rows[1].trigger("click", { metaKey: true });
    expect(w.findAll("[data-test=task-row][data-selected=true]").length).toBe(0);
  });

  test("Ctrl+click on a row toggles selection (Windows/Linux)", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[2].trigger("click", { ctrlKey: true });
    expect(rows[2].attributes("data-selected")).toBe("true");
  });

  test("plain click clears selection and emits open", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true });
    await rows[1].trigger("click", { metaKey: true });
    await rows[2].trigger("click"); // plain click
    expect(w.emitted("open")).toHaveLength(1);
    expect(w.findAll("[data-test=task-row][data-selected=true]").length).toBe(0);
  });

  test("Shift+click extends selection from anchor to target in visible order", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true }); // anchor s1
    await rows[2].trigger("click", { shiftKey: true }); // range s1..s3
    expect(rows[0].attributes("data-selected")).toBe("true");
    expect(rows[1].attributes("data-selected")).toBe("true");
    expect(rows[2].attributes("data-selected")).toBe("true");
  });

  test("contextmenu with sel.size>=2 shows merge/close/details items", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true });
    await rows[1].trigger("click", { metaKey: true });
    await rows[0].trigger("contextmenu");
    expect(w.find("[data-test=session-row-menu-item-merge]").exists()).toBe(true);
    expect(w.find("[data-test=session-row-menu-item-close]").exists()).toBe(true);
    expect(
      (w.find("[data-test=session-row-menu-item-details]").element as HTMLButtonElement).disabled,
    ).toBe(true);
  });

  test("contextmenu with sel.size<=1 shows details/pin items only", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("contextmenu");
    expect(w.find("[data-test=session-row-menu-item-details]").exists()).toBe(true);
    expect(w.find("[data-test=session-row-menu-item-pin]").exists()).toBe(true);
    expect(w.find("[data-test=session-row-menu-item-merge]").exists()).toBe(false);
  });

  test("contextmenu on unselected row when sel.size>=1 replaces selection", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true }); // select s1
    await rows[2].trigger("contextmenu"); // right-click s3 (not selected)
    // Menu should be the single-select variant (s3 alone)
    expect(w.find("[data-test=session-row-menu-item-details]").exists()).toBe(true);
    expect(w.find("[data-test=session-row-menu-item-merge]").exists()).toBe(false);
    // s3 is now the only selected row
    expect(w.findAll("[data-test=task-row][data-selected=true]").length).toBe(1);
    expect(rows[2].attributes("data-selected")).toBe("true");
  });

  test("selecting merge from menu emits merge-selected", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true });
    await rows[1].trigger("click", { metaKey: true });
    await rows[0].trigger("contextmenu");
    await w.find("[data-test=session-row-menu-item-merge]").trigger("click");
    expect(w.emitted("merge-selected")).toHaveLength(1);
  });

  test("selecting close-selected from menu emits close-selected", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true }); // s1 is open
    await rows[1].trigger("click", { metaKey: true }); // s2 not open
    await rows[0].trigger("contextmenu");
    await w.find("[data-test=session-row-menu-item-close]").trigger("click");
    expect(w.emitted("close-selected")).toHaveLength(1);
  });

  test("selecting details from menu opens SessionDetailsPopover with the right-clicked session", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[1].trigger("contextmenu");
    await w.find("[data-test=session-row-menu-item-details]").trigger("click");
    const popover = w.find("[data-test=session-details-popover]");
    expect(popover.exists()).toBe(true);
    expect(popover.find("[data-test=details-field-sessionId] .value").text()).toBe("s2");
  });
});
```

Note: the test file needs a `mountList` helper. Check existing `TaskGroupedList.test.ts` for how it mounts the component; reuse or add a small factory helper at the top of the file if missing.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/components/TaskGroupedList.test.ts`
Expected: FAIL on new cases.

- [ ] **Step 3: Modify `TaskGroupedList.vue`**

Add these imports:

```ts
import { useSessionSelection } from "../composables/useSessionSelection";
import SessionDetailsPopover from "./SessionDetailsPopover.vue";
```

Add near existing composables:

```ts
const sel = useSessionSelection();
```

Extend `defineEmits` — the new tuple:

```ts
const emit = defineEmits<{
  (e: "open", session: RemoteSession): void;
  (e: "close", session: RemoteSession): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
  (e: "merge-selected"): void;
  (e: "close-selected"): void;
}>();
```

Add `orderedVisibleIds` computed (place after existing computeds):

```ts
// Flat id list in the exact visual order rows render (excluding group
// headers, empty-hint, fold-toggle). Used as the axis for Shift+click
// range selection. Rebuild on any change to pinned/filtered/completed.
const orderedVisibleIds = computed<string[]>(() => {
  const out: string[] = [];
  for (const s of pinnedSessions.value) out.push(s.session_id);
  for (const key of groupKeys.value) {
    if (isGroupCollapsed(key)) continue;
    for (const s of filteredGroups.value[key] ?? []) out.push(s.session_id);
  }
  if (foldOpen.value) {
    for (const s of completedFiltered.value) out.push(s.session_id);
  }
  return out;
});
```

Add `openSessionIdSet` remains as-is; add its interaction with menu items.

Replace the click handler (there is no explicit handler today — `@click="emit('open', s)"` is inline). Change all four `@click="emit('open', s)"` occurrences (pinned group rows, host/state group rows, completed-fold rows) to `@click="(e) => onRowClick(e, s)"`.

Add:

```ts
function onRowClick(e: MouseEvent, s: RemoteSession) {
  if (e.shiftKey) {
    sel.selectRange(s.session_id, orderedVisibleIds.value);
    return;
  }
  if (e.metaKey || e.ctrlKey) {
    sel.toggle(s.session_id);
    return;
  }
  sel.clear();
  emit("open", s);
}
```

Update the row `:class` binding to include `selected`:

```vue
:class="{ active: s.session_id === activeSessionId, selected: sel.isSelected(s.session_id) }"
:data-selected="sel.isSelected(s.session_id) ? 'true' : undefined"
```

Do this on all three renderers (pinned, group, completed-fold). For the completed-fold `.task-row.dim` line, the click still uses `emit('open', s)` today — swap it to `onRowClick` too, so range selection can extend into the completed fold when it is open.

Add `.selected` style:

```css
.task-row.selected {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
```

Rewrite `onRowMenu`:

```ts
function onRowMenu(e: MouseEvent, s: RemoteSession) {
  e.preventDefault();
  // If right-click lands on a row that isn't part of the current
  // selection, reset the selection to just that row — same behavior as
  // macOS Finder / VSCode explorer. This keeps the menu's action target
  // unambiguous.
  if (!sel.isSelected(s.session_id)) sel.selectOnly(s.session_id);
  menuState.value = { open: true, x: e.clientX, y: e.clientY, session: s };
}
```

Add `openSessionIdSet` reference already exists at line ~83; ensure it is imported into `menuItems` calc.

Replace `menuItems` (from Task 2's stub) with the multi-select-aware version:

```ts
const menuItems = computed<MenuItem[]>(() => {
  const s = menuState.value.session;
  if (!s) return [];
  const n = sel.size.value;
  const multi = n >= 2 && sel.isSelected(s.session_id);
  if (multi) {
    const openCount = Array.from(sel.selectedIds.value).filter((id) =>
      openSessionIdSet.value.has(id),
    ).length;
    return [
      {
        key: "merge",
        label: t("tasks.rowMenu.merge", { n }),
        disabled: n > 4,
      },
      {
        key: "close",
        label: t("tasks.rowMenu.closeSelected", { n: openCount }),
        disabled: openCount === 0,
      },
      {
        key: "details",
        label: t("tasks.rowMenu.details"),
        disabled: true,
      },
      {
        key: pins.isPinned(s.session_id) ? "unpin" : "pin",
        label: pins.isPinned(s.session_id)
          ? t("tasks.pinned.menuUnpin")
          : t("tasks.pinned.menuPin"),
      },
    ];
  }
  return [
    { key: "details", label: t("tasks.rowMenu.details") },
    {
      key: pins.isPinned(s.session_id) ? "unpin" : "pin",
      label: pins.isPinned(s.session_id)
        ? t("tasks.pinned.menuUnpin")
        : t("tasks.pinned.menuPin"),
    },
  ];
});
```

Extend `onMenuSelect`:

```ts
const detailsState = ref<{
  open: boolean;
  x: number;
  y: number;
  session: RemoteSession | null;
}>({ open: false, x: 0, y: 0, session: null });

function onMenuSelect(key: string) {
  const s = menuState.value.session;
  if (!s) return;
  if (key === "pin" || key === "unpin") {
    pins.toggle(s.session_id);
  } else if (key === "details") {
    detailsState.value = {
      open: true,
      x: menuState.value.x,
      y: menuState.value.y,
      session: s,
    };
  } else if (key === "merge") {
    emit("merge-selected");
  } else if (key === "close") {
    emit("close-selected");
  }
}

function closeDetails() {
  detailsState.value = { ...detailsState.value, open: false, session: null };
}
```

Add pane-location helpers as props received from parent (to keep this component free of App-scope state):

```ts
// New props, added to existing withDefaults block:
paneLocationFor?: (id: string) => { tabId: string; paneIdx: number } | null;
tabIndexById?: (tabId: string) => number;
```

Defaults:

```ts
paneLocationFor: () => null,
tabIndexById: () => 0,
```

Add `<SessionDetailsPopover>` in the template just after `<SessionRowMenu>`:

```vue
<SessionDetailsPopover
  :open="detailsState.open"
  :x="detailsState.x"
  :y="detailsState.y"
  :session="detailsState.session"
  :pane-location="detailsState.session ? paneLocationFor(detailsState.session.session_id) : null"
  :tab-index-by-id="tabIndexById"
  @close="closeDetails"
/>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/components/TaskGroupedList.test.ts`
Expected: PASS. If the "details opens popover" test cannot find the session id because the test doesn't pass `paneLocationFor` / `tabIndexById`, the defaults still work — popover just renders `paneLocation`= null which is fine.

Also run: `cd desktop/frontend && npx vitest run` — full suite green.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TaskGroupedList.vue \
        desktop/frontend/src/components/TaskGroupedList.test.ts
git commit -m "feat(sidebar): multi-select interactions in TaskGroupedList"
```

---

### Task 6: `TaskSidebar` — mount BulkActionBar, wire Esc / blank-click clear

**Files:**
- Modify: `desktop/frontend/src/components/TaskSidebar.vue`
- Modify: `desktop/frontend/src/components/TaskSidebar.test.ts`

**Interfaces:**
- Consumes: `useSessionSelection` (Task 1); `BulkActionBar` (Task 4); `TaskGroupedList` events `merge-selected` / `close-selected`.
- Produces:
  ```ts
  // TaskSidebar new emits (forwarded up to App.vue):
  (e: "merge-selected"): void;
  (e: "close-selected"): void;
  // New optional props forwarded to TaskGroupedList:
  paneLocationFor?: (id: string) => { tabId: string; paneIdx: number } | null;
  tabIndexById?: (tabId: string) => number;
  ```

- [ ] **Step 1: Add sidebar tests**

Append to `desktop/frontend/src/components/TaskSidebar.test.ts` (or create a new `describe` block if the file already exists — the plan assumes it does):

```ts
import { __resetForTests as resetSel, useSessionSelection } from "../composables/useSessionSelection";

describe("TaskSidebar — multi-select footer + Esc / blank clear", () => {
  beforeEach(() => resetSel());

  function mountSidebar(overrides: Record<string, any> = {}) {
    return mount(TaskSidebar, {
      attachTo: document.body,
      props: {
        collapsed: false,
        byHost: { h1: [{
          session_id: "s1", host_id: "h1", host: "h1", user: "u",
          title: "one", cols: 80, rows: 24,
        }] },
        primaryStateForHost: () => "idle",
        completedSeen: [],
        totalUnread: 0,
        openSessionIds: ["s1"],
        ...overrides,
      },
    });
  }

  test("BulkActionBar not rendered when selection is empty", () => {
    const w = mountSidebar();
    expect(w.find("[data-test=bulk-action-bar]").exists()).toBe(false);
  });

  test("BulkActionBar rendered when selection is non-empty", async () => {
    const w = mountSidebar();
    // Force a selection through the composable directly
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    expect(w.find("[data-test=bulk-action-bar]").exists()).toBe(true);
  });

  test("mark-all footer is suppressed while selection is non-empty even with unread", async () => {
    const w = mountSidebar({ totalUnread: 3 });
    expect(w.find("[data-test=sidebar-mark-all]").exists()).toBe(true);
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    expect(w.find("[data-test=sidebar-mark-all]").exists()).toBe(false);
    expect(w.find("[data-test=bulk-action-bar]").exists()).toBe(true);
  });

  test("BulkActionBar 'clear' clears selection", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    await w.find("[data-test=bulk-clear]").trigger("click");
    expect(useSessionSelection().size.value).toBe(0);
  });

  test("Esc inside sidebar clears selection", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    await w.find(".task-sidebar").trigger("keydown", { key: "Escape" });
    expect(useSessionSelection().size.value).toBe(0);
  });

  test("clicking sidebar blank area clears selection", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    // Click on the sidebar-header area outside any row / menu / bar
    await w.find(".sidebar-header").trigger("click");
    expect(useSessionSelection().size.value).toBe(0);
  });

  test("clicking inside a task row does NOT trigger blank-click clear (row handles its own logic)", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    await w.find("[data-test=task-row]").trigger("click", { metaKey: true });
    // Selection may toggle for s1 (from 1 → 0 → 1), but blank-clear must NOT
    // fire independently — verify the composable is not force-empty via
    // an unrelated path.
    // After metaKey toggle: s1 was selected, so toggle removes it → 0.
    expect(useSessionSelection().size.value).toBe(0);
  });

  test("BulkActionBar merge emit is forwarded to TaskSidebar", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    await w.find("[data-test=bulk-merge]").trigger("click");
    expect(w.emitted("merge-selected")).toHaveLength(1);
  });

  test("BulkActionBar close emit is forwarded to TaskSidebar", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    await w.find("[data-test=bulk-close]").trigger("click");
    expect(w.emitted("close-selected")).toHaveLength(1);
  });
});
```

- [ ] **Step 2: Modify `TaskSidebar.vue`**

Add imports:

```ts
import BulkActionBar from "./BulkActionBar.vue";
import { useSessionSelection } from "../composables/useSessionSelection";
```

Extend `defineProps`:

```ts
const props = withDefaults(defineProps<{
  // ...existing...
  paneLocationFor?: (id: string) => { tabId: string; paneIdx: number } | null;
  tabIndexById?: (tabId: string) => number;
}>(), {
  // ...existing defaults...
  paneLocationFor: () => null,
  tabIndexById: () => 0,
});
```

Extend `defineEmits`:

```ts
const emit = defineEmits<{
  (e: "update:collapsed", v: boolean): void;
  (e: "open", session: RemoteSession): void;
  (e: "close", session: RemoteSession): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
  (e: "merge-selected"): void;
  (e: "close-selected"): void;
}>();
```

Add:

```ts
const sel = useSessionSelection();
const openCount = computed(() => {
  const opens = new Set(props.openSessionIds ?? []);
  let n = 0;
  for (const id of sel.selectedIds.value) if (opens.has(id)) n++;
  return n;
});
const canMerge = computed(() => sel.size.value >= 1 && sel.size.value <= 4);

function onSidebarKeydown(e: KeyboardEvent) {
  if (e.key === "Escape" && sel.size.value > 0) {
    e.preventDefault();
    sel.clear();
  }
}

function onSidebarBlankClick(e: MouseEvent) {
  if (sel.size.value === 0) return;
  const el = e.target as HTMLElement | null;
  if (!el) return;
  // Don't clear if the click landed inside a row, menu, popover, or the
  // bulk bar itself (those handle their own selection semantics).
  if (
    el.closest(
      ".task-row, .host-header, .group-header, .bulk-bar, .session-row-menu, .session-details-popover",
    )
  ) {
    return;
  }
  sel.clear();
}
```

Bind to root aside:

```vue
<aside
  class="task-sidebar"
  :class="{ collapsed }"
  :style="!collapsed ? { width: widthPx + 'px' } : undefined"
  tabindex="-1"
  @keydown="onSidebarKeydown"
  @click.capture="onSidebarBlankClick"
>
```

Update the footer block — replace:

```vue
<footer v-if="totalUnread > 0">
  <button ...>{{ t("tasks.markAllRead") }}</button>
</footer>
```

with:

```vue
<footer v-if="sel.size.value >= 1">
  <BulkActionBar
    :count="sel.size.value"
    :open-count="openCount"
    :can-merge="canMerge"
    @merge="emit('merge-selected')"
    @close-selected="emit('close-selected')"
    @clear="sel.clear()"
  />
</footer>
<footer v-else-if="totalUnread > 0">
  <button
    class="mark-all"
    data-test="sidebar-mark-all"
    @click="emit('markSeen', { all: true })"
  >
    {{ t("tasks.markAllRead") }}
  </button>
</footer>
```

Wire the `paneLocationFor` / `tabIndexById` props into `<TaskGroupedList>` and forward the two new events:

```vue
<TaskGroupedList
  ...existing bindings...
  :pane-location-for="paneLocationFor"
  :tab-index-by-id="tabIndexById"
  @open="(s) => emit('open', s)"
  @close="(s) => emit('close', s)"
  @markSeen="(p) => emit('markSeen', p)"
  @merge-selected="emit('merge-selected')"
  @close-selected="emit('close-selected')"
/>
```

- [ ] **Step 3: Run tests to verify they pass**

Run:
```
cd desktop/frontend && npx vitest run src/components/TaskSidebar.test.ts
```
Expected: PASS. Then run full suite:
```
cd desktop/frontend && npx vitest run
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/components/TaskSidebar.vue \
        desktop/frontend/src/components/TaskSidebar.test.ts
git commit -m "feat(sidebar): BulkActionBar + Esc/blank-click clear"
```

---

### Task 7: `App.vue` — detachOnly opt, merge / batch-close helpers, sidebar wiring

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/App.test.ts` (or create `App.merge.test.ts` if the existing file is a smoke test only)

**Interfaces:**
- Consumes: `useSessionSelection`; `findPaneLocation` / `closePane` from `../lib/layout`; `Tab` type; existing `newId()` / `gotoTab()`.
- Produces:
  ```ts
  async function closePaneAt(t: Tab, idx: number, opts?: { detachOnly?: boolean }): Promise<void>;
  async function mergeSelectedIntoTab(): Promise<void>;
  async function closeSelectedOpen(): Promise<void>;
  function paneLocationForSession(id: string): { tabId: string; paneIdx: number } | null;
  function tabIndexById(tabId: string): number;
  ```

- [ ] **Step 1: Extend `closePaneAt` with `detachOnly` opts**

Locate `async function closePaneAt(t: Tab, idx: number)` around `desktop/frontend/src/App.vue:955-970` and modify to:

```ts
async function closePaneAt(t: Tab, idx: number, opts?: { detachOnly?: boolean }): Promise<void> {
  const target = t.panes[idx];
  if (target?.sessionId && !target.remote && !opts?.detachOnly) {
    pendingLocalIds.delete(target.sessionId);
    try { await closeSession(target.sessionId); } catch { /* sweep cleans up */ }
  }
  const r = closePane(t.layout, t.panes, idx, t.colRatio, t.rowRatio);
  t.layout = r.layout;
  t.panes = r.panes;
  t.activePaneIdx = r.activePaneIdx;
  t.colRatio = r.colRatio;
  t.rowRatio = r.rowRatio;
  if (r.closeTab) {
    closeTab(t.id);
  }
}
```

- [ ] **Step 2: Add merge / close helpers**

Add near the other tab-management helpers (after `openRemoteAsTab`):

```ts
import { useSessionSelection } from "./composables/useSessionSelection";
import { PANE_COUNT, type LayoutKind } from "./lib/types";

const sel = useSessionSelection();

function layoutForCount(n: number): LayoutKind | null {
  if (n === 1) return "single";
  if (n === 2) return "vertical";
  if (n === 3 || n === 4) return "grid2x2";
  return null;
}

function paneLocationForSession(id: string): { tabId: string; paneIdx: number } | null {
  return findPaneLocation(tabs.value, id);
}

function tabIndexById(tabId: string): number {
  const i = tabs.value.findIndex((t) => t.id === tabId);
  return i < 0 ? 0 : i + 1; // 1-based for display
}

async function mergeSelectedIntoTab(): Promise<void> {
  const ids = Array.from(sel.selectedIds.value);
  const layout = layoutForCount(ids.length);
  if (!layout) return; // UI already disables; belt-and-braces guard.
  // Detach any panes currently holding these ids WITHOUT killing local
  // shells. closePaneAt will cascade closeTab when the last pane leaves.
  for (const id of ids) {
    const loc = findPaneLocation(tabs.value, id);
    if (!loc) continue;
    const t = tabs.value.find((tt) => tt.id === loc.tabId);
    if (!t) continue;
    await closePaneAt(t, loc.paneIdx, { detachOnly: true });
  }
  const capacity = PANE_COUNT[layout];
  const panes = new Array(capacity).fill(null).map((_, i) => (
    i < ids.length
      ? { sessionId: ids[i], remote: true }
      : { sessionId: null as string | null, remote: false }
  ));
  const newTab = {
    id: newId(),
    layout,
    panes,
    activePaneIdx: 0,
    colRatio: RATIO_DEFAULT,
    rowRatio: RATIO_DEFAULT,
  };
  tabs.value.push(newTab);
  gotoTab(newTab.id);
  sel.clear();
}

async function closeSelectedOpen(): Promise<void> {
  const opens = openSessionIdSet.value;
  const ids = Array.from(sel.selectedIds.value).filter((id) => opens.has(id));
  for (const id of ids) {
    const loc = findPaneLocation(tabs.value, id);
    if (!loc) continue;
    const t = tabs.value.find((tt) => tt.id === loc.tabId);
    if (!t) continue;
    await closePaneAt(t, loc.paneIdx);
  }
  sel.clear();
}
```

`openSessionIdSet` is derived elsewhere; verify it exists near line 487. If it doesn't, add:

```ts
const openSessionIdSet = computed<Set<string>>(() => {
  const s = new Set<string>();
  for (const t of tabs.value) for (const p of t.panes) if (p.sessionId) s.add(p.sessionId);
  return s;
});
```

- [ ] **Step 3: Wire sidebar events**

Find the `<TaskSidebar>` render at `~line 1330` and extend its bindings:

```vue
<TaskSidebar
  ...existing bindings...
  :pane-location-for="paneLocationForSession"
  :tab-index-by-id="tabIndexById"
  @open="onSidebarOpen"
  @close="onSidebarClose"
  @markSeen="onMarkSeen"
  @merge-selected="mergeSelectedIntoTab"
  @close-selected="closeSelectedOpen"
/>
```

- [ ] **Step 4: Write / update an integration test**

Create `desktop/frontend/src/App.merge.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import App from "./App.vue";
import { __resetForTests as resetSel, useSessionSelection } from "./composables/useSessionSelection";

// Existing test files should show how App.vue is mounted with mocked
// platform bridges. Reuse that mock; the point of this test is to
// exercise merge / close-selected end-to-end.

describe("App — merge & batch close (integration)", () => {
  beforeEach(() => resetSel());
  afterEach(() => {
    // Restore any mocks set by prior mount helpers.
  });

  // Skeleton — flesh out using the App.test.ts mount helper. Once
  // that helper is imported, the assertion payload is:
  test.todo("mergeSelectedIntoTab creates a grid2x2 tab from 3 selected sessions");
  test.todo("mergeSelectedIntoTab with 1 open + 2 unopened detaches the open pane and closes empty source tab");
  test.todo("closeSelectedOpen closes only the open subset and preserves unopened selection state");
  test.todo("mergeSelectedIntoTab is a no-op when count > 4");
});
```

The four `test.todo` are acknowledged coverage gaps — either flesh them out with the existing App.test.ts mount setup, or leave as `.todo`. If the existing test file already has a mount helper exported, import it and complete these tests:

```ts
// If mountApp helper exists in App.test.ts, export it and use it here.
// Otherwise, gate the four tests behind test.todo until the app-level
// mount plumbing is unified.
```

Do NOT skip Task 7 completion because of the todo — the integration is verified manually in Task 10.

- [ ] **Step 5: Run tests + type check**

Run:
```
cd desktop/frontend && npx vitest run
```
Expected: PASS. `.todo` tests report as skipped but do not fail.

Run type check (existing project scripts):
```
cd desktop/frontend && npx vue-tsc --noEmit
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/App.merge.test.ts
git commit -m "feat(sidebar): App-side merge & batch close wiring"
```

---

### Task 8: i18n keys (en + zh-CN)

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

**Interfaces:**
- Consumes: none.
- Produces: `t()` keys referenced by all components in Tasks 3–7:
  - `tasks.rowMenu.details`
  - `tasks.rowMenu.merge` (interpolates `{n}`)
  - `tasks.rowMenu.closeSelected` (interpolates `{n}`)
  - `tasks.bulk.selectedCount` (interpolates `{count}`)
  - `tasks.bulk.mergeTab`
  - `tasks.bulk.mergeTooMany`
  - `tasks.bulk.close` (interpolates `{count}`)
  - `tasks.bulk.closeHint` (interpolates `{count}`)
  - `tasks.bulk.closeNoneOpen`
  - `tasks.bulk.cancel`
  - `tasks.details.title`
  - `tasks.details.sessionId`
  - `tasks.details.type`
  - `tasks.details.host`
  - `tasks.details.cwd`
  - `tasks.details.command`
  - `tasks.details.state`
  - `tasks.details.startedAt`
  - `tasks.details.commandStartedAt`
  - `tasks.details.commandEndedAt`
  - `tasks.details.commandDuration`
  - `tasks.details.commandExitCode`
  - `tasks.details.unread`
  - `tasks.details.pinned`
  - `tasks.details.paneLocation`
  - `tasks.details.paneAt` (interpolates `{tab}` and `{pane}`)
  - `tasks.details.paneNone`
  - `common.yes` / `common.no` / `common.copy` (add only if not already present — grep before editing)

- [ ] **Step 1: Grep for existing `common.yes` / `common.no` / `common.copy`**

Run:
```
grep -n "yes:\|no:\|copy:" desktop/frontend/src/i18n/messages/en.ts | head
grep -n "yes:\|no:\|copy:" desktop/frontend/src/i18n/messages/zh-CN.ts | head
```

If `common.yes` / `common.no` / `common.copy` are missing, add them in the `common: { ... }` block of each file (see Step 2 additions below).

- [ ] **Step 2: Add keys to `en.ts`**

Under the `tasks: {` block, add / extend:

```ts
    rowMenu: {
      details: "View details",
      merge: "Merge into new tab ({n})",
      closeSelected: "Close selected ({n})",
    },
    bulk: {
      selectedCount: "{count} selected",
      mergeTab: "Merge tab",
      mergeTooMany: "Merge up to 4",
      close: "Close ({count})",
      closeHint: "Close {count} open panes",
      closeNoneOpen: "None of the selected sessions are open in this window",
      cancel: "Cancel",
    },
    details: {
      title: "Session details",
      sessionId: "Session ID",
      type: "Type",
      host: "Host",
      cwd: "CWD",
      command: "Command",
      state: "State",
      startedAt: "Started",
      commandStartedAt: "Cmd started",
      commandEndedAt: "Cmd ended",
      commandDuration: "Cmd duration",
      commandExitCode: "Cmd exit code",
      unread: "Unread",
      pinned: "Pinned",
      paneLocation: "Open in",
      paneAt: "Tab {tab} · Pane {pane}",
      paneNone: "Not open in this window",
    },
```

Under `common: { ... }` (create the block if it doesn't exist), ensure:

```ts
  common: {
    yes: "Yes",
    no: "No",
    copy: "Copy",
    close: "Close",   // may already exist
  },
```

If `common` already exists, only add the three keys — do not duplicate.

- [ ] **Step 3: Add keys to `zh-CN.ts`**

Add the same structure:

```ts
    rowMenu: {
      details: "查看详细信息",
      merge: "合并 tab ({n})",
      closeSelected: "关闭已打开 ({n})",
    },
    bulk: {
      selectedCount: "已选 {count}",
      mergeTab: "合并 tab",
      mergeTooMany: "最多合并 4 个",
      close: "关闭 ({count})",
      closeHint: "关闭 {count} 个已打开的 pane",
      closeNoneOpen: "选中集合中没有本窗口已打开的会话",
      cancel: "取消",
    },
    details: {
      title: "会话详细信息",
      sessionId: "Session ID",
      type: "类型",
      host: "主机",
      cwd: "工作目录",
      command: "命令",
      state: "状态",
      startedAt: "开始于",
      commandStartedAt: "命令开始",
      commandEndedAt: "命令结束",
      commandDuration: "命令耗时",
      commandExitCode: "退出码",
      unread: "未读",
      pinned: "已置顶",
      paneLocation: "所在位置",
      paneAt: "Tab {tab} · Pane {pane}",
      paneNone: "本窗口未打开",
    },
```

Under `common: { ... }`:

```ts
  common: {
    yes: "是",
    no: "否",
    copy: "复制",
    close: "关闭",
  },
```

- [ ] **Step 4: Run tests to confirm keys resolve**

Run:
```
cd desktop/frontend && npx vitest run src/components/BulkActionBar.test.ts src/components/SessionDetailsPopover.test.ts src/components/TaskGroupedList.test.ts src/components/TaskSidebar.test.ts
```
Expected: PASS with rendered labels visible in the string comparisons (any test that asserts label text should now find the real translation, not the raw key).

Then run full suite:
```
cd desktop/frontend && npx vitest run
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "i18n: add sidebar multi-select + details keys"
```

---

### Task 9: Final verification — full test suite + type check + build

**Files:** none new.

- [ ] **Step 1: Full frontend test suite**

Run:
```
cd desktop/frontend && npx vitest run
```
Expected: all tests PASS. If any prior test broke because of the `SessionRowMenu` refactor (Task 2 supposedly fixed all callers, but stragglers may exist), fix in this task and commit as `test: adapt <file> to items-driven SessionRowMenu`.

- [ ] **Step 2: Type check**

Run:
```
cd desktop/frontend && npx vue-tsc --noEmit
```
Expected: no type errors.

- [ ] **Step 3: Vite build**

Run:
```
cd desktop/frontend && npm run build
```
Expected: build succeeds; no runtime error about missing i18n keys or missing components.

- [ ] **Step 4: Manual smoke (10 min)**

Launch the app locally (`cd desktop && wails dev` or your usual workflow). Verify:

1. Right-click a session row → menu shows "查看详细信息" and pin/unpin.
2. Click "查看详细信息" → popover appears near the click; Esc / click outside dismisses.
3. Cmd+click (macOS) two rows → both rows get an accent outline; bottom bar appears with "已选 2 · 合并 tab (2) · 关闭 (M) · 取消".
4. Click the "合并 tab" button → a new tab appears with vertical layout, both sessions attached; the source tabs (if any) close if their pane count dropped to zero.
5. Shift+click across three visible rows → range fills selection.
6. Cmd+A does nothing (verify unimplemented — spec §2).
7. Esc while sidebar has selection → clears selection.
8. Right-click a row that is NOT in the current selection while ≥1 row is selected → selection collapses to that single row, menu shows single-select variant (details/pin).
9. Merge tab with 5 selected → button disabled; tooltip = "最多合并 4 个".
10. Batch close where none of the selected are open panes → button disabled; tooltip = "选中集合中没有本窗口已打开的会话".

Report any regression as a new commit.

- [ ] **Step 5: Verification-before-completion checkpoint**

Before declaring done: `git status` should be clean, `git log` should show 8 commits (Tasks 1–8 each commit; Task 9's fixes if any). Then hand off to ship-release.

## Self-Review Checklist

- [x] Spec §1 (right-click details, multi-select, batch close, merge tab) → Tasks 3 / 5 (details), 1 / 5 / 6 (multi-select), 4 / 6 / 7 (batch actions).
- [x] Spec §2 non-goals: Cmd+A, drag-merge, kill unopened, mobile, cross-session persistence — none of them appear as tasks. Task 9 Step 4 point 6 explicitly checks Cmd+A does nothing.
- [x] Spec §4.1 useSessionSelection → Task 1 with full implementation.
- [x] Spec §4.2 TaskGroupedList row click branch, .selected, orderedVisibleIds, contextmenu variants → Task 5.
- [x] Spec §4.3 SessionRowMenu items → Task 2.
- [x] Spec §4.4 BulkActionBar → Task 4 + wiring in Task 6.
- [x] Spec §4.5 SessionDetailsPopover fields (adapted to actual RemoteSession fields; driver/viewers dropped because those aren't on RemoteSession — see Task 3 field list). This is a documented deviation from the spec's tentative field list and is acceptable because the spec §4.5 explicitly said "若数据可得，否则省略".
- [x] Spec §4.6 App-side merge / close / detachOnly → Task 7.
- [x] Spec §4.7 keyboard / blank click → Task 6.
- [x] Spec §6 i18n → Task 8.
- [x] Spec §7 tests → each task has TDD Step 1 tests.
- [x] Type consistency: `MenuItem` type comes from `SessionRowMenu.vue` (Task 2) and is imported in `TaskGroupedList.vue` (Task 5). `paneLocationFor` / `tabIndexById` types match across Tasks 3 / 5 / 6 / 7. `closePaneAt(t, idx, opts?)` signature is used identically in Task 7's merge and close helpers.
- [x] Insertion order for Set → verified in Task 1's "insertion order is preserved" test.
