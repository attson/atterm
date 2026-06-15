# Multi-pane width/height adjustment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let desktop users drag pane separators in `vertical` / `horizontal` / `grid2x2` layouts to adjust column / row ratios; double-click resets to 50/50.

**Architecture:** Add two `Tab` fields (`colRatio`, `rowRatio`) threaded through `layout.ts` transitions. `PaneGrid` switches from hard-coded `grid-template: 1fr 1fr` to computed `${ratio}fr ${1-ratio}fr` and renders a `PaneSplitter` overlay per separator. The splitter uses Pointer Events with `setPointerCapture`. During drag, `PaneGrid` flips a `dragging` flag → `TerminalView`'s `resize-suspended` prop short-circuits the `term.onResize → conn.sendResize` call so only one PTY RESIZE fires on mouseup (red-line #6 preserved). State lives only in the Vue Tab object; no `config.json`, no mobile, no nesting.

**Tech Stack:** Vue 3 + TS, vitest (with `@vue/test-utils`), xterm.js + FitAddon, CSS Grid, PointerEvents API.

**Spec:** [docs/superpowers/specs/2026-06-15-multi-pane-resize-design.md](../specs/2026-06-15-multi-pane-resize-design.md)

---

## Task 1: Thread `colRatio` / `rowRatio` through `layout.ts` and `Tab` type

**Files:**
- Modify: `desktop/frontend/src/lib/types.ts`
- Modify: `desktop/frontend/src/lib/layout.ts`
- Modify: `desktop/frontend/src/lib/layout.test.ts`

This task only changes pure types + pure functions + their tests. App.vue still won't type-check after this task — fixed in Task 2.

- [ ] **Step 1: Write failing tests for ratio threading**

Append to `desktop/frontend/src/lib/layout.test.ts`:

```ts
import { RATIO_DEFAULT, RATIO_MIN, RATIO_MAX, closePane, transitionLayout } from "./layout";

describe("ratio threading", () => {
  it("transitionLayout passes through colRatio/rowRatio", () => {
    const r = transitionLayout("single", [P("a")], 0, "vertical", 0.3, 0.7);
    expect(r.colRatio).toBe(0.3);
    expect(r.rowRatio).toBe(0.7);
  });

  it("transitionLayout single→horizontal keeps both ratios", () => {
    const r = transitionLayout("single", [P("a")], 0, "horizontal", 0.4, 0.6);
    expect(r.colRatio).toBe(0.4);
    expect(r.rowRatio).toBe(0.6);
  });

  it("transitionLayout vertical→grid2x2 keeps both ratios", () => {
    const r = transitionLayout("vertical", [P("a"), P("b")], 0, "vertical", 0.25, 0.75);
    expect(r.layout).toBe("grid2x2");
    expect(r.colRatio).toBe(0.25);
    expect(r.rowRatio).toBe(0.75);
  });

  it("closePane grid2x2→vertical keeps both ratios", () => {
    const r = closePane(
      "grid2x2",
      [P("a"), P("b"), E, E],
      2,
      0.3,
      0.7,
    );
    expect(r.layout).toBe("vertical");
    expect(r.colRatio).toBe(0.3);
    expect(r.rowRatio).toBe(0.7);
  });

  it("closePane vertical→single keeps both ratios", () => {
    const r = closePane("vertical", [P("a"), P("b")], 1, 0.2, 0.8);
    expect(r.layout).toBe("single");
    expect(r.colRatio).toBe(0.2);
    expect(r.rowRatio).toBe(0.8);
  });

  it("ratio constants are exported", () => {
    expect(RATIO_DEFAULT).toBe(0.5);
    expect(RATIO_MIN).toBe(0.1);
    expect(RATIO_MAX).toBe(0.9);
  });
});
```

- [ ] **Step 2: Run tests and verify they fail with type errors**

Run: `cd desktop/frontend && npx vitest run src/lib/layout.test.ts`

Expected: TS error — `RATIO_DEFAULT` / `colRatio` not exported; `transitionLayout` / `closePane` signature mismatch.

- [ ] **Step 3: Add ratio fields to `Tab`**

Replace the entire `Tab` interface in `desktop/frontend/src/lib/types.ts`:

```ts
export interface Tab {
  id: string;            // frontend-generated uuid; Vue key only
  layout: LayoutKind;
  panes: Pane[];         // length matches layout: 1 / 2 / 2 / 4
  activePaneIdx: number; // index in panes[] of the keyboard-focused pane
  // 0.1..0.9, left column share for vertical/grid2x2. Ignored when layout
  // is single/horizontal but always present so callers don't branch.
  colRatio: number;
  // 0.1..0.9, top row share for horizontal/grid2x2. Same caveat as colRatio.
  rowRatio: number;
}
```

- [ ] **Step 4: Update `layout.ts` — constants + signatures + threading**

Replace the file `desktop/frontend/src/lib/layout.ts` with:

```ts
// Pure layout state transitions. No DOM, no Vue, no IO. Tested via vitest.
// See docs/superpowers/specs/2026-06-15-multi-pane-resize-design.md.

import type {
  FocusDir,
  LayoutKind,
  Pane,
  SplitDir,
  Tab,
} from "./types";
import { EMPTY_PANE, PANE_COUNT } from "./types";

export const RATIO_MIN = 0.1;
export const RATIO_MAX = 0.9;
export const RATIO_DEFAULT = 0.5;

export function clampRatio(r: number): number {
  if (!Number.isFinite(r)) return RATIO_DEFAULT;
  if (r < RATIO_MIN) return RATIO_MIN;
  if (r > RATIO_MAX) return RATIO_MAX;
  return r;
}

const empty = (): Pane => ({ ...EMPTY_PANE });

export interface TransitionResult {
  layout: LayoutKind;
  panes: Pane[];
  activePaneIdx: number;
  newPaneIdx: number;
  noop?: boolean;
  colRatio: number;
  rowRatio: number;
}

export function transitionLayout(
  current: LayoutKind,
  panes: Pane[],
  activeIdx: number,
  dir: SplitDir,
  colRatio: number,
  rowRatio: number,
): TransitionResult {
  if (current === "single") {
    const nextLayout: LayoutKind = dir === "vertical" ? "vertical" : "horizontal";
    return {
      layout: nextLayout,
      panes: [{ ...panes[0] }, empty()],
      activePaneIdx: 1,
      newPaneIdx: 1,
      colRatio,
      rowRatio,
    };
  }

  if (current === "vertical") {
    const newIdx = activeIdx === 0 ? 2 : 3;
    const next: Pane[] = [{ ...panes[0] }, { ...panes[1] }, empty(), empty()];
    return {
      layout: "grid2x2",
      panes: next,
      activePaneIdx: newIdx,
      newPaneIdx: newIdx,
      colRatio,
      rowRatio,
    };
  }

  if (current === "horizontal") {
    const newIdx = activeIdx === 0 ? 1 : 3;
    const next: Pane[] = [{ ...panes[0] }, empty(), { ...panes[1] }, empty()];
    return {
      layout: "grid2x2",
      panes: next,
      activePaneIdx: newIdx,
      newPaneIdx: newIdx,
      colRatio,
      rowRatio,
    };
  }

  const emptyIdx = panes.findIndex((p) => p.sessionId === null);
  if (emptyIdx === -1) {
    return {
      layout: "grid2x2",
      panes: panes.map((p) => ({ ...p })),
      activePaneIdx: activeIdx,
      newPaneIdx: -1,
      noop: true,
      colRatio,
      rowRatio,
    };
  }
  return {
    layout: "grid2x2",
    panes: panes.map((p) => ({ ...p })),
    activePaneIdx: emptyIdx,
    newPaneIdx: emptyIdx,
    colRatio,
    rowRatio,
  };
}

export interface CloseResult {
  layout: LayoutKind;
  panes: Pane[];
  activePaneIdx: number;
  closeTab?: boolean;
  colRatio: number;
  rowRatio: number;
}

export function closePane(
  layout: LayoutKind,
  panes: Pane[],
  closeIdx: number,
  colRatio: number,
  rowRatio: number,
): CloseResult {
  if (layout === "single") {
    return {
      layout: "single",
      panes: panes.map((p) => ({ ...p })),
      activePaneIdx: 0,
      closeTab: true,
      colRatio,
      rowRatio,
    };
  }

  if (layout === "vertical" || layout === "horizontal") {
    const survivorIdx = closeIdx === 0 ? 1 : 0;
    return {
      layout: "single",
      panes: [{ ...panes[survivorIdx] }],
      activePaneIdx: 0,
      colRatio,
      rowRatio,
    };
  }

  const next = panes.map((p, i) => (i === closeIdx ? empty() : { ...p }));
  const filledIndices = next
    .map((p, i) => (p.sessionId !== null ? i : -1))
    .filter((i) => i >= 0);
  const filled = filledIndices.length;

  if (filled >= 3) {
    return {
      layout: "grid2x2",
      panes: next,
      activePaneIdx: filledIndices[0],
      colRatio,
      rowRatio,
    };
  }

  if (filled === 2) {
    return reduceTwoFilled(next, filledIndices, colRatio, rowRatio);
  }

  if (filled === 1) {
    return {
      layout: "single",
      panes: [{ ...next[filledIndices[0]] }],
      activePaneIdx: 0,
      colRatio,
      rowRatio,
    };
  }

  return {
    layout: "single",
    panes: [empty()],
    activePaneIdx: 0,
    closeTab: true,
    colRatio,
    rowRatio,
  };
}

function reduceTwoFilled(
  panes: Pane[],
  idx: number[],
  colRatio: number,
  rowRatio: number,
): CloseResult {
  const [a, b] = idx;
  const sameCol = (a === 0 && b === 2) || (a === 1 && b === 3);
  const layout: LayoutKind = sameCol ? "horizontal" : "vertical";
  return {
    layout,
    panes: [{ ...panes[a] }, { ...panes[b] }],
    activePaneIdx: 0,
    colRatio,
    rowRatio,
  };
}

type NeighborMap = Record<number, Partial<Record<FocusDir, number>>>;

const NEIGHBORS: Record<LayoutKind, NeighborMap> = {
  single: { 0: {} },
  vertical: {
    0: { right: 1 },
    1: { left: 0 },
  },
  horizontal: {
    0: { down: 1 },
    1: { up: 0 },
  },
  grid2x2: {
    0: { right: 1, down: 2 },
    1: { left: 0, down: 3 },
    2: { up: 0, right: 3 },
    3: { up: 1, left: 2 },
  },
};

export function focusNeighbor(
  layout: LayoutKind,
  activeIdx: number,
  dir: FocusDir,
): number | null {
  const next = NEIGHBORS[layout]?.[activeIdx]?.[dir];
  return typeof next === "number" ? next : null;
}

export type { Pane, Tab, LayoutKind, SplitDir, FocusDir };
export { PANE_COUNT, EMPTY_PANE };
```

- [ ] **Step 5: Update existing layout tests that hit the new signatures**

The pre-existing tests in `layout.test.ts` call `transitionLayout(layout, panes, idx, dir)` (4 args) and `closePane(layout, panes, idx)` (3 args). Add `RATIO_DEFAULT, RATIO_DEFAULT` to every call so they keep compiling. Search and append in `layout.test.ts`:

```bash
cd desktop/frontend
# Visually: every transitionLayout(...) call gets `, RATIO_DEFAULT, RATIO_DEFAULT` appended
# and every closePane(...) call too. Import RATIO_DEFAULT at top.
```

Concretely add to top of file:

```ts
import { RATIO_DEFAULT, RATIO_MIN, RATIO_MAX, closePane, focusNeighbor, transitionLayout } from "./layout";
```

(replace existing import).

Then `transitionLayout(arg1, arg2, arg3, arg4)` → `transitionLayout(arg1, arg2, arg3, arg4, RATIO_DEFAULT, RATIO_DEFAULT)` across the file. Similarly `closePane(arg1, arg2, arg3)` → `closePane(arg1, arg2, arg3, RATIO_DEFAULT, RATIO_DEFAULT)`. Use Find & Replace; verify no false positives (no nested calls).

- [ ] **Step 6: Run layout tests**

Run: `cd desktop/frontend && npx vitest run src/lib/layout.test.ts`
Expected: all PASS (existing + new ratio-threading suite).

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/lib/types.ts desktop/frontend/src/lib/layout.ts desktop/frontend/src/lib/layout.test.ts
git commit -m "feat(layout): thread colRatio/rowRatio through Tab and transitions"
```

---

## Task 2: Wire ratio defaults + transitions in `App.vue`

**Files:**
- Modify: `desktop/frontend/src/App.vue`

After Task 1 the project doesn't type-check yet (Tab missing ratios at the two construction sites, and transitionLayout / closePane calls are short two args). This task fixes both. No new tests — App.vue smoke is covered by existing `App.test.ts` plus the manual run.

- [ ] **Step 1: Import ratio constants**

Find the line:

```ts
import { closePane, focusNeighbor, transitionLayout } from "./lib/layout";
```

Replace with:

```ts
import { RATIO_DEFAULT, closePane, focusNeighbor, transitionLayout } from "./lib/layout";
```

- [ ] **Step 2: Add ratios to both `tabs.value.push(...)` Tab literals**

Find both occurrences (currently ~lines 593 and 741):

```ts
    tabs.value.push({
      id,
      layout: "single",
      panes: [{ sessionId: sid, remote: false }],
      activePaneIdx: 0,
    });
```

and:

```ts
  tabs.value.push({
    id,
    layout: "single",
    panes: [{ sessionId, remote: true }],
    activePaneIdx: 0,
  });
```

Add `colRatio: RATIO_DEFAULT,` and `rowRatio: RATIO_DEFAULT,` to each. After the change they read:

```ts
    tabs.value.push({
      id,
      layout: "single",
      panes: [{ sessionId: sid, remote: false }],
      activePaneIdx: 0,
      colRatio: RATIO_DEFAULT,
      rowRatio: RATIO_DEFAULT,
    });
```

and likewise the second site.

- [ ] **Step 3: Pass ratios into `transitionLayout` + write them back**

Find:

```ts
  const result = transitionLayout(t.layout, t.panes, t.activePaneIdx, dir);
```

Replace with:

```ts
  const result = transitionLayout(t.layout, t.panes, t.activePaneIdx, dir, t.colRatio, t.rowRatio);
```

And below, where `t.layout = result.layout; t.panes = result.panes; t.activePaneIdx = result.activePaneIdx;` is set, append:

```ts
  t.colRatio = result.colRatio;
  t.rowRatio = result.rowRatio;
```

- [ ] **Step 4: Pass ratios into `closePane` + write them back**

Find:

```ts
  const r = closePane(t.layout, t.panes, idx);
  t.layout = r.layout;
  t.panes = r.panes;
  t.activePaneIdx = r.activePaneIdx;
```

Replace with:

```ts
  const r = closePane(t.layout, t.panes, idx, t.colRatio, t.rowRatio);
  t.layout = r.layout;
  t.panes = r.panes;
  t.activePaneIdx = r.activePaneIdx;
  t.colRatio = r.colRatio;
  t.rowRatio = r.rowRatio;
```

- [ ] **Step 5: Type-check**

Run: `cd desktop/frontend && npx vue-tsc --noEmit`
Expected: PASS. If `App.test.ts` constructs Tabs without the new fields it will also fail — fix by adding `colRatio: 0.5, rowRatio: 0.5` to each mock Tab literal.

- [ ] **Step 6: Run App tests**

Run: `cd desktop/frontend && npx vitest run src/App.test.ts src/App.theme.test.ts`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/App.test.ts
git commit -m "feat(app): construct Tabs with default ratios and pass through transitions"
```

(Only stage `App.test.ts` if it actually changed.)

---

## Task 3: `PaneSplitter` component

**Files:**
- Create: `desktop/frontend/src/components/PaneSplitter.vue`
- Create: `desktop/frontend/src/components/PaneSplitter.test.ts`

- [ ] **Step 1: Write failing tests**

Create `desktop/frontend/src/components/PaneSplitter.test.ts`:

```ts
import { describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import PaneSplitter from "./PaneSplitter.vue";

function makeRect(width = 1000, height = 800): DOMRect {
  return {
    x: 0, y: 0, top: 0, left: 0, right: width, bottom: height,
    width, height, toJSON: () => ({}),
  } as DOMRect;
}

describe("PaneSplitter", () => {
  test("renders col-orientation class", () => {
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.5, containerRect: () => makeRect() },
    });
    expect(w.find(".pane-splitter.col").exists()).toBe(true);
  });

  test("renders row-orientation class", () => {
    const w = mount(PaneSplitter, {
      props: { orientation: "row", ratio: 0.5, containerRect: () => makeRect() },
    });
    expect(w.find(".pane-splitter.row").exists()).toBe(true);
  });

  test("pointermove emits update:ratio with delta/width for col", async () => {
    const rect = makeRect(1000, 800);
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.5, containerRect: () => rect },
      attachTo: document.body,
    });
    const el = w.find(".pane-splitter").element as HTMLElement;
    // jsdom stubs setPointerCapture
    (el as any).setPointerCapture = vi.fn();
    (el as any).releasePointerCapture = vi.fn();
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerId: 1, clientX: 500, clientY: 0 }));
    el.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: 600, clientY: 0 }));
    const events = w.emitted("update:ratio");
    expect(events).toBeTruthy();
    // delta = 100, width = 1000 → next = 0.5 + 0.1 = 0.6
    expect(events![events!.length - 1][0]).toBeCloseTo(0.6, 5);
    w.unmount();
  });

  test("pointermove emits update:ratio with delta/height for row", async () => {
    const rect = makeRect(1000, 800);
    const w = mount(PaneSplitter, {
      props: { orientation: "row", ratio: 0.5, containerRect: () => rect },
      attachTo: document.body,
    });
    const el = w.find(".pane-splitter").element as HTMLElement;
    (el as any).setPointerCapture = vi.fn();
    (el as any).releasePointerCapture = vi.fn();
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerId: 1, clientX: 0, clientY: 400 }));
    el.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: 0, clientY: 480 }));
    const events = w.emitted("update:ratio");
    // delta = 80, height = 800 → next = 0.5 + 0.1 = 0.6
    expect(events![events!.length - 1][0]).toBeCloseTo(0.6, 5);
    w.unmount();
  });

  test("update:ratio is clamped to [0.1, 0.9]", async () => {
    const rect = makeRect(1000, 800);
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.5, containerRect: () => rect },
      attachTo: document.body,
    });
    const el = w.find(".pane-splitter").element as HTMLElement;
    (el as any).setPointerCapture = vi.fn();
    (el as any).releasePointerCapture = vi.fn();
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerId: 1, clientX: 500, clientY: 0 }));
    el.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: 5000, clientY: 0 }));
    const events = w.emitted("update:ratio")!;
    expect(events[events.length - 1][0]).toBeCloseTo(0.9, 5);
    el.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: -5000, clientY: 0 }));
    const events2 = w.emitted("update:ratio")!;
    expect(events2[events2.length - 1][0]).toBeCloseTo(0.1, 5);
    w.unmount();
  });

  test("pointerup emits commit", async () => {
    const rect = makeRect(1000, 800);
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.5, containerRect: () => rect },
      attachTo: document.body,
    });
    const el = w.find(".pane-splitter").element as HTMLElement;
    (el as any).setPointerCapture = vi.fn();
    (el as any).releasePointerCapture = vi.fn();
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerId: 1, clientX: 500, clientY: 0 }));
    el.dispatchEvent(new PointerEvent("pointerup", { pointerId: 1, clientX: 500, clientY: 0 }));
    expect(w.emitted("commit")).toBeTruthy();
    w.unmount();
  });

  test("dblclick emits reset", async () => {
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.7, containerRect: () => makeRect() },
    });
    await w.find(".pane-splitter").trigger("dblclick");
    expect(w.emitted("reset")).toBeTruthy();
  });
});
```

- [ ] **Step 2: Run tests and verify they fail with module-not-found**

Run: `cd desktop/frontend && npx vitest run src/components/PaneSplitter.test.ts`
Expected: FAIL — `Cannot find module './PaneSplitter.vue'`.

- [ ] **Step 3: Create `PaneSplitter.vue`**

Create `desktop/frontend/src/components/PaneSplitter.vue`:

```vue
<script lang="ts" setup>
import { computed, onBeforeUnmount, ref } from "vue";

const RATIO_MIN = 0.1;
const RATIO_MAX = 0.9;

const props = defineProps<{
  orientation: "col" | "row";
  ratio: number;
  containerRect: () => DOMRect | null;
}>();

const emit = defineEmits<{
  (e: "update:ratio", next: number): void;
  (e: "commit"): void;
  (e: "reset"): void;
}>();

const rootEl = ref<HTMLDivElement | null>(null);
let activePointerId: number | null = null;
let startCoord = 0;
let startRatio = 0;
let savedBodyCursor = "";

function clamp(r: number): number {
  if (!Number.isFinite(r)) return 0.5;
  if (r < RATIO_MIN) return RATIO_MIN;
  if (r > RATIO_MAX) return RATIO_MAX;
  return r;
}

function onPointerDown(e: PointerEvent) {
  if (e.button !== 0) return;
  const el = rootEl.value;
  if (!el) return;
  activePointerId = e.pointerId;
  startRatio = props.ratio;
  startCoord = props.orientation === "col" ? e.clientX : e.clientY;
  el.setPointerCapture(e.pointerId);
  if (typeof document !== "undefined") {
    savedBodyCursor = document.body.style.cursor;
    document.body.style.cursor = props.orientation === "col" ? "col-resize" : "row-resize";
  }
  e.preventDefault();
}

function onPointerMove(e: PointerEvent) {
  if (activePointerId !== e.pointerId) return;
  const rect = props.containerRect();
  if (!rect) return;
  const size = props.orientation === "col" ? rect.width : rect.height;
  if (size <= 0) return;
  const current = props.orientation === "col" ? e.clientX : e.clientY;
  const delta = (current - startCoord) / size;
  emit("update:ratio", clamp(startRatio + delta));
}

function endDrag(e: PointerEvent) {
  if (activePointerId !== e.pointerId) return;
  const el = rootEl.value;
  try { el?.releasePointerCapture(e.pointerId); } catch { /* ignore */ }
  activePointerId = null;
  if (typeof document !== "undefined") {
    document.body.style.cursor = savedBodyCursor;
    savedBodyCursor = "";
  }
  emit("commit");
}

function onDblclick() {
  emit("reset");
}

onBeforeUnmount(() => {
  if (activePointerId !== null && rootEl.value) {
    try { rootEl.value.releasePointerCapture(activePointerId); } catch { /* ignore */ }
    if (typeof document !== "undefined") {
      document.body.style.cursor = savedBodyCursor;
    }
  }
});

const style = computed(() => {
  const pct = clamp(props.ratio) * 100;
  if (props.orientation === "col") {
    return { left: `calc(${pct}% - 3px)` };
  }
  return { top: `calc(${pct}% - 3px)` };
});
</script>

<template>
  <div
    ref="rootEl"
    class="pane-splitter"
    :class="orientation"
    :style="style"
    role="separator"
    :aria-orientation="orientation === 'col' ? 'vertical' : 'horizontal'"
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerup="endDrag"
    @pointercancel="endDrag"
    @dblclick="onDblclick"
  />
</template>

<style scoped>
.pane-splitter {
  position: absolute;
  z-index: 5;
  background: transparent;
  user-select: none;
  touch-action: none;
}
.pane-splitter.col {
  top: 0;
  bottom: 0;
  width: 6px;
  cursor: col-resize;
}
.pane-splitter.row {
  left: 0;
  right: 0;
  height: 6px;
  cursor: row-resize;
}
.pane-splitter:hover,
.pane-splitter:active {
  background: rgba(255, 255, 255, 0.08);
}
</style>
```

- [ ] **Step 4: Run tests**

Run: `cd desktop/frontend && npx vitest run src/components/PaneSplitter.test.ts`
Expected: PASS (all 7 tests).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/PaneSplitter.vue desktop/frontend/src/components/PaneSplitter.test.ts
git commit -m "feat(panesplitter): drag/dblclick component with pointer capture"
```

---

## Task 4: Switch `PaneGrid` to ratio-driven CSS + render splitters

**Files:**
- Modify: `desktop/frontend/src/components/PaneGrid.vue`
- Modify: `desktop/frontend/src/components/PaneGrid.test.ts`

- [ ] **Step 1: Update existing tests for new Tab shape + add new assertions**

In `desktop/frontend/src/components/PaneGrid.test.ts`:

Add `colRatio: 0.5, rowRatio: 0.5` to the inline Tab literal at line ~30-38 so the file type-checks.

Append a new describe block:

```ts
describe("PaneGrid ratio rendering", () => {
  test("vertical applies colRatio to grid-template", () => {
    const tab: Tab = {
      id: "t",
      layout: "vertical",
      activePaneIdx: 0,
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
      colRatio: 0.3,
      rowRatio: 0.5,
    };
    const w = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "t" }),
        sessionInfoFor: () => null,
        active: true,
        terminalTheme: {},
        commandNotifyThresholdSec: 10,
      },
    });
    const root = w.find(".pane-grid").element as HTMLElement;
    expect(root.style.gridTemplate).toMatch(/0\.3\d*fr 0\.7\d*fr/);
  });

  test("horizontal applies rowRatio to grid-template", () => {
    const tab: Tab = {
      id: "t",
      layout: "horizontal",
      activePaneIdx: 0,
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
      colRatio: 0.5,
      rowRatio: 0.25,
    };
    const w = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "t" }),
        sessionInfoFor: () => null,
        active: true,
        terminalTheme: {},
        commandNotifyThresholdSec: 10,
      },
    });
    const root = w.find(".pane-grid").element as HTMLElement;
    expect(root.style.gridTemplate).toMatch(/0\.25\d*fr/);
  });

  test("grid2x2 renders 2 splitters", () => {
    const tab: Tab = {
      id: "t",
      layout: "grid2x2",
      activePaneIdx: 0,
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
        { sessionId: "c", remote: false },
        { sessionId: "d", remote: false },
      ],
      colRatio: 0.5,
      rowRatio: 0.5,
    };
    const w = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "t" }),
        sessionInfoFor: () => null,
        active: true,
        terminalTheme: {},
        commandNotifyThresholdSec: 10,
      },
    });
    expect(w.findAll(".pane-splitter")).toHaveLength(2);
  });

  test("single renders no splitters", () => {
    const tab: Tab = {
      id: "t",
      layout: "single",
      activePaneIdx: 0,
      panes: [{ sessionId: "a", remote: false }],
      colRatio: 0.5,
      rowRatio: 0.5,
    };
    const w = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "t" }),
        sessionInfoFor: () => null,
        active: true,
        terminalTheme: {},
        commandNotifyThresholdSec: 10,
      },
    });
    expect(w.findAll(".pane-splitter")).toHaveLength(0);
  });

  test("emits update:col-ratio when col splitter emits", async () => {
    const tab: Tab = {
      id: "t",
      layout: "vertical",
      activePaneIdx: 0,
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
      colRatio: 0.5,
      rowRatio: 0.5,
    };
    const w = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "t" }),
        sessionInfoFor: () => null,
        active: true,
        terminalTheme: {},
        commandNotifyThresholdSec: 10,
      },
    });
    const splitter = w.findComponent({ name: "PaneSplitter" });
    splitter.vm.$emit("update:ratio", 0.42);
    expect(w.emitted("update:col-ratio")).toEqual([[0.42]]);
  });

  test("dblclick reset emits update:col-ratio with 0.5", async () => {
    const tab: Tab = {
      id: "t",
      layout: "vertical",
      activePaneIdx: 0,
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
      colRatio: 0.3,
      rowRatio: 0.5,
    };
    const w = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "t" }),
        sessionInfoFor: () => null,
        active: true,
        terminalTheme: {},
        commandNotifyThresholdSec: 10,
      },
    });
    const splitter = w.findComponent({ name: "PaneSplitter" });
    splitter.vm.$emit("reset");
    expect(w.emitted("update:col-ratio")).toEqual([[0.5]]);
  });
});
```

- [ ] **Step 2: Run tests and verify they fail**

Run: `cd desktop/frontend && npx vitest run src/components/PaneGrid.test.ts`
Expected: FAIL — `grid-template` still `1fr 1fr`, no `.pane-splitter` elements.

- [ ] **Step 3: Update `PaneGrid.vue`**

Replace the whole `<script lang="ts" setup>` block with:

```ts
import { computed, ref, type CSSProperties } from "vue";
import TerminalView from "./TerminalView.vue";
import PaneSplitter from "./PaneSplitter.vue";
import type { Endpoint } from "../lib/api";
import type { SessionInfo } from "../lib/connection";
import type { Pane, Tab } from "../lib/types";
import type { TerminalThemeDefinition } from "../lib/terminalThemes";
import { extractSessionLabel } from "../lib/terminalBell";
import { useI18n } from "../i18n/useI18n";
import { RATIO_DEFAULT, RATIO_MAX, RATIO_MIN, clampRatio } from "../lib/layout";

const props = defineProps<{
  tab: Tab;
  endpointFor: (pane: Pane) => Endpoint | null;
  sessionInfoFor: (pane: Pane) => SessionInfo | null;
  viewerCountFor?: (sessionId: string) => number;
  active: boolean;
  terminalTheme: TerminalThemeDefinition["xtermTheme"];
  commandNotifyThresholdSec: number;
}>();

const emit = defineEmits<{
  (e: "set-active-pane", paneIdx: number): void;
  (e: "close-pane", paneIdx: number): void;
  (e: "toast", message: string): void;
  (e: "update:col-ratio", ratio: number): void;
  (e: "update:row-ratio", ratio: number): void;
}>();

const { t } = useI18n();

const AREA_FOR_LAYOUT = {
  single:     ["a"],
  vertical:   ["a", "b"],
  horizontal: ["a", "b"],
  grid2x2:    ["a", "b", "c", "d"],
} as const;

const areaFor = computed(() => AREA_FOR_LAYOUT[props.tab.layout]);

const gridRoot = ref<HTMLDivElement | null>(null);
const dragging = ref(false);

function getContainerRect(): DOMRect | null {
  return gridRoot.value?.getBoundingClientRect() ?? null;
}

const gridStyle = computed<CSSProperties>(() => {
  const c = clampRatio(props.tab.colRatio);
  const r = clampRatio(props.tab.rowRatio);
  const cl = c.toFixed(4);
  const cr = (1 - c).toFixed(4);
  const rt = r.toFixed(4);
  const rb = (1 - r).toFixed(4);
  switch (props.tab.layout) {
    case "single":     return {};
    case "vertical":   return { gridTemplate: `"a b" / ${cl}fr ${cr}fr` };
    case "horizontal": return { gridTemplate: `"a" ${rt}fr "b" ${rb}fr / 1fr` };
    case "grid2x2":    return { gridTemplate: `"a b" ${rt}fr "c d" ${rb}fr / ${cl}fr ${cr}fr` };
  }
  return {};
});

const showColSplitter = computed(
  () => props.tab.layout === "vertical" || props.tab.layout === "grid2x2",
);
const showRowSplitter = computed(
  () => props.tab.layout === "horizontal" || props.tab.layout === "grid2x2",
);

function onColUpdate(next: number) {
  emit("update:col-ratio", next);
}
function onRowUpdate(next: number) {
  emit("update:row-ratio", next);
}
function onColReset() {
  emit("update:col-ratio", RATIO_DEFAULT);
}
function onRowReset() {
  emit("update:row-ratio", RATIO_DEFAULT);
}
function onCommitStart() {
  dragging.value = true;
}
function onCommitEnd() {
  dragging.value = false;
}

function onPaneClick(idx: number) {
  if (idx !== props.tab.activePaneIdx) emit("set-active-pane", idx);
}

function formatWho(info: SessionInfo | null): string {
  if (!info) return "";
  const u = info.user || "";
  const h = info.host || "";
  if (u && h) return `${u}@${h}`;
  return h || u || "";
}

// Suppress unused-warning for re-exports used in the script tag only.
void RATIO_MIN; void RATIO_MAX;
```

Replace the template root + add splitter elements + remove the hard-coded `grid-template` from CSS:

```vue
<template>
  <div
    ref="gridRoot"
    class="pane-grid"
    :class="tab.layout"
    :style="gridStyle"
  >
    <div
      v-for="(pane, idx) in tab.panes"
      :key="idx"
      class="cell"
      :style="{ gridArea: areaFor[idx] }"
      @mousedown="onPaneClick(idx)"
    >
      <div class="term-host">
        <TerminalView
          v-if="pane.sessionId && endpointFor(pane)"
          :endpoint="endpointFor(pane)!"
          :session-id="pane.sessionId"
          :active="active"
          :focused="active && idx === tab.activePaneIdx"
          :expected-cols="sessionInfoFor(pane)?.cols"
          :expected-rows="sessionInfoFor(pane)?.rows"
          :remote-permission="sessionInfoFor(pane)?.remote_permission"
          :session-label="extractSessionLabel(sessionInfoFor(pane))"
          :avoid-top-right-badge="pane.remote || (viewerCountFor?.(pane.sessionId) ?? 0) > 0"
          :theme="terminalTheme"
          :is-local-session="!pane.remote"
          :command-notify-threshold-sec="commandNotifyThresholdSec"
          :resize-suspended="dragging"
          @toast="emit('toast', $event)"
        />
        <div v-else class="empty">{{ t("terminal.emptyPaneHint") }}</div>
      </div>

      <div class="cell-controls">
        <div
          v-if="pane.sessionId && !pane.remote && (viewerCountFor?.(pane.sessionId) ?? 0) > 0"
          class="viewers-badge"
          :title="t('terminal.remoteViewerWatching', { count: viewerCountFor!(pane.sessionId) })"
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" />
            <circle cx="12" cy="12" r="3" />
          </svg>
          <span>{{ viewerCountFor!(pane.sessionId) }}</span>
        </div>

        <div
          v-if="pane.sessionId && pane.remote"
          class="remote-badge"
          :title="
            (sessionInfoFor(pane)?.host_id
              ? t('sessions.hostIdTitle', { hostId: sessionInfoFor(pane)!.host_id ?? '' }) + '\n'
              : '') + t('sessions.sessionTitle', { sessionId: pane.sessionId })
          "
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            width="11" height="11"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2.4"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M2 16.1A5 5 0 0 1 5.9 20" />
            <path d="M2 12.05A9 9 0 0 1 9.95 20" />
            <path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6" />
            <line x1="2" y1="20" x2="2.01" y2="20" />
          </svg>
          <span v-if="formatWho(sessionInfoFor(pane))" class="who">
            {{ formatWho(sessionInfoFor(pane)) }}
          </span>
          <span v-else class="who dim">{{ t("terminal.remote") }}</span>
          <span class="sid">{{ pane.sessionId.slice(0, 8) }}</span>
        </div>

        <button
          v-if="tab.layout !== 'single'"
          type="button"
          class="close-pane"
          :title="t('terminal.closePaneTitle')"
          @mousedown.stop
          @click.stop="emit('close-pane', idx)"
        >×</button>
      </div>
    </div>

    <PaneSplitter
      v-if="showColSplitter"
      orientation="col"
      :ratio="tab.colRatio"
      :container-rect="getContainerRect"
      @update:ratio="onColUpdate"
      @reset="onColReset"
      @pointerdown.passive="onCommitStart"
      @commit="onCommitEnd"
    />
    <PaneSplitter
      v-if="showRowSplitter"
      orientation="row"
      :ratio="tab.rowRatio"
      :container-rect="getContainerRect"
      @update:ratio="onRowUpdate"
      @reset="onRowReset"
      @pointerdown.passive="onCommitStart"
      @commit="onCommitEnd"
    />
  </div>
</template>
```

Replace the `<style scoped>` block — remove the four hardcoded `grid-template` rules:

```vue
<style scoped>
.pane-grid {
  position: absolute;
  inset: 0;
  display: grid;
  gap: 2px;
  background: var(--terminal-grid);
}
.pane-grid.single { grid-template: "a"; }
/* vertical / horizontal / grid2x2 templates set via :style from gridStyle */

.cell {
  position: relative;
  background: var(--terminal-bg);
  overflow: hidden;
}
.term-host {
  position: absolute;
  inset: 0;
}
.empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-dim);
  font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.cell-controls {
  position: absolute;
  top: 6px;
  right: 8px;
  display: flex;
  align-items: center;
  gap: 4px;
  pointer-events: none;
}
.remote-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: rgba(13, 17, 23, 0.85);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 2px 8px;
  font-size: 11px;
  line-height: 1.5;
  color: #d29922;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  user-select: none;
  pointer-events: none;
}
.remote-badge svg { display: block; }
.viewers-badge {
  position: absolute;
  top: 4px;
  right: 4px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 1px 6px;
  border-radius: 6px;
  background: rgba(0, 0, 0, 0.55);
  color: var(--fg);
  font-size: 11px;
  pointer-events: none;
}
.viewers-badge svg { display: block; }
.remote-badge .who { font-weight: 600; }
.remote-badge .who.dim { color: var(--fg-dim); font-weight: 400; }
.remote-badge .sid {
  color: var(--fg-dim);
  font-weight: 400;
}
.remote-badge .sid::before {
  content: "·";
  margin-right: 4px;
}
.close-pane {
  border: none;
  background: rgba(13, 17, 23, 0.7);
  color: var(--fg-dim);
  font-size: 14px;
  line-height: 1;
  padding: 2px 6px;
  border-radius: 4px;
  cursor: pointer;
  opacity: 0;
  pointer-events: auto;
  transition: opacity 120ms, background 120ms, color 120ms;
}
.cell:hover .close-pane { opacity: 1; }
.close-pane:hover {
  background: rgba(248, 81, 73, 0.18);
  color: var(--bad);
}
</style>
```

- [ ] **Step 4: Run PaneGrid tests**

Run: `cd desktop/frontend && npx vitest run src/components/PaneGrid.test.ts`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/PaneGrid.vue desktop/frontend/src/components/PaneGrid.test.ts
git commit -m "feat(panegrid): drive grid-template with colRatio/rowRatio and render splitters"
```

---

## Task 5: `TerminalView` honors `resize-suspended`

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Modify: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 1: Write failing tests**

Look at the top of `TerminalView.test.ts` for the existing mocking pattern. Append a new describe block (copy the harness pattern from existing tests; if mocking `SessionConnection` is non-trivial, focus on raw-source assertions like `PaneGrid.test.ts` does):

```ts
import sourceText from "./TerminalView.vue?raw";

describe("TerminalView resize-suspended", () => {
  test("declares resize-suspended prop", () => {
    expect(sourceText).toMatch(/resizeSuspended\?:\s*boolean/);
  });

  test("term.onResize gates conn.sendResize on resizeSuspended", () => {
    // The handler must early-return when props.resizeSuspended is true.
    expect(sourceText).toMatch(/term\.onResize\([\s\S]*?if \(props\.resizeSuspended\) return;[\s\S]*?conn\?\.sendResize/);
  });

  test("watches resize-suspended falling edge to emit final sendResize", () => {
    expect(sourceText).toMatch(/watch\(\s*\(\)\s*=>\s*props\.resizeSuspended,[\s\S]*?prev\s*&&\s*!next[\s\S]*?conn\?\.sendResize\(term\.cols,\s*term\.rows\)/);
  });
});
```

These are source-text assertions for the same reason the existing PaneGrid tests use them: simulating an xterm + ResizeObserver loop in jsdom is brittle. The source pattern matches are precise enough to catch regressions.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/components/TerminalView.test.ts`
Expected: FAIL on the three new tests.

- [ ] **Step 3: Add the prop**

In `desktop/frontend/src/components/TerminalView.vue` find:

```ts
    commandNotifyThresholdSec?: number;
    isLocalSession?: boolean;
  }>(),
  { active: true, focused: false, avoidTopRightBadge: false, commandNotifyThresholdSec: 10, isLocalSession: true }
);
```

Replace with:

```ts
    commandNotifyThresholdSec?: number;
    isLocalSession?: boolean;
    // True while the user is dragging a pane splitter. FitAddon still runs
    // (xterm stays visually correct) but we skip sendResize until the
    // drag ends, so the PTY child sees one SIGWINCH instead of dozens.
    resizeSuspended?: boolean;
  }>(),
  { active: true, focused: false, avoidTopRightBadge: false, commandNotifyThresholdSec: 10, isLocalSession: true, resizeSuspended: false }
);
```

- [ ] **Step 4: Gate sendResize in `term.onResize`**

Find:

```ts
  term.onResize(({ cols, rows }) => {
    if (!isDriver.value) return; // viewer's local resize is FitAddon-suppressed anyway
    conn?.sendResize(cols, rows);
  });
```

Replace with:

```ts
  term.onResize(({ cols, rows }) => {
    if (!isDriver.value) return; // viewer's local resize is FitAddon-suppressed anyway
    if (props.resizeSuspended) return; // mid-drag: keep xterm in sync locally but defer the PTY RESIZE until mouseup
    conn?.sendResize(cols, rows);
  });
```

- [ ] **Step 5: Add the falling-edge watcher**

The existing imports already include `watch`. Just after the `defineEmits<...>()` block (or near the other `watch(...)` calls — search for `watch(()` to find an idiomatic spot), add:

```ts
// Falling edge of resize-suspended: drag just ended. Emit one final
// RESIZE so the PTY learns the new cols/rows (during drag we suppressed
// every onResize call). nextTick ensures any post-drop layout settles
// before we read term.cols/rows.
watch(
  () => props.resizeSuspended,
  (next, prev) => {
    if (prev && !next) {
      nextTick(() => {
        if (term && conn) conn.sendResize(term.cols, term.rows);
      });
    }
  },
);
```

`nextTick` is already imported at line 2.

- [ ] **Step 6: Run TerminalView tests**

Run: `cd desktop/frontend && npx vitest run src/components/TerminalView.test.ts`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git commit -m "feat(terminal): suspend sendResize during pane drag, flush on release"
```

---

## Task 6: Wire ratio emits in `App.vue`

**Files:**
- Modify: `desktop/frontend/src/App.vue`

Find the `<PaneGrid ... />` invocation in App.vue's template (search for `<PaneGrid`):

- [ ] **Step 1: Add handlers**

Find the existing PaneGrid invocation and add two new event handlers `@update:col-ratio` and `@update:row-ratio` that write back into the active tab:

```html
        <PaneGrid
          :tab="tab"
          ...existing props...
          @set-active-pane="...existing..."
          @close-pane="...existing..."
          @toast="...existing..."
          @update:col-ratio="(r) => { tab.colRatio = r; }"
          @update:row-ratio="(r) => { tab.rowRatio = r; }"
        />
```

(Replace the literal `...existing...` with whatever is already present — do not delete unrelated bindings.)

- [ ] **Step 2: Type-check**

Run: `cd desktop/frontend && npx vue-tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/App.vue
git commit -m "feat(app): write back pane ratios from PaneGrid events"
```

---

## Task 7: Full verification

**Files:** none modified — verification only.

- [ ] **Step 1: Run all desktop frontend tests**

Run: `cd desktop/frontend && npm test`
Expected: ALL PASS.

- [ ] **Step 2: Type-check + build**

Run: `cd desktop/frontend && npm run build`
Expected: clean build, dist/ produced.

- [ ] **Step 3: Go vet (paranoia — Go side untouched but the build hook runs anyway)**

Run: `go vet -tags webkit2_41 ./...`
Expected: no errors.

- [ ] **Step 4: Manual smoke (briefly — see verify skill)**

Build the desktop app once and verify in the running app:

1. `cd desktop && wails dev -tags webkit2_41` (or `wails build -tags webkit2_41 && open build/bin/*.app`).
2. Open a tab. Split vertically. Drag the vertical splitter — left pane gets wider, xterm reflows live.
3. Release — terminal locks to new size. No stray `%` or duplicated prompt.
4. Double-click splitter — pane snaps back to 50/50.
5. Split again to make grid2x2. Confirm both vertical and horizontal splitters appear and work.
6. Close one pane. Confirm ratios survive the transition.
7. Reload the tab (open new tab) — new tab starts at 50/50, confirms in-memory only.

- [ ] **Step 5: Commit a no-op marker if needed**

(No commit if everything was already committed above.)

---

## Notes for the implementer

- `clampRatio` lives in `layout.ts`. Don't recreate it inside components — import.
- `PaneSplitter`'s `containerRect` is a getter (function returning DOMRect | null). The grid container may resize during drag (window resize), so reading fresh on each pointermove is intentional.
- Splitter position uses `calc(${pct}% - 3px)` to center on the CSS Grid gap (`gap: 2px`, splitter `width: 6px` → half-width 3px ≈ gap center). Visually acceptable.
- `dragging` ref stays in PaneGrid. It is NOT a Tab field — it has no business persisting across re-renders.
- Red line #6 from AGENTS.md: predict-fork + queue-not-drop + skip-on-match are untouched. The `resize-suspended` gate is a NEW fourth state that lives in TerminalView, independent of FitAddon. We still let FitAddon run during drag so xterm renders correctly; only the wire RESIZE is deferred.
- If the splitter feels visually misaligned with the gap, tweak the `- 3px` constant in `PaneSplitter.vue`'s `style` computed; it's a one-character fix.
