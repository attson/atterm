# Pane Split Layouts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-tab pane splits (single / left-right / top-bottom / 2×2) to the desktop app, with iTerm-style keyboard shortcuts. Each pane is an independent local PTY or attached existing session (local or remote).

**Architecture:** Refactor `App.vue` from a flat `sessions: AggSession[]` to `tabs: Tab[]` where each `Tab` owns a layout and 1–4 panes. Layout transitions, close reductions, and pane-focus navigation live in pure functions in `lib/layout.ts` (vitest-tested). A new `PaneGrid.vue` renders the layout via CSS Grid, hosting one `TerminalView` per pane. Shortcuts live in a `useTerminalShortcuts` composable that listens at document-capture phase to beat xterm.js. Backend (Go / protocol) is untouched.

**Tech Stack:** Vue 3 Composition API + TypeScript, xterm.js, Wails v2 bindings (`window.go.main.App`), CSS Grid, vitest (new dev dep).

**Reference spec:** `docs/superpowers/specs/2026-05-10-pane-split-layouts-design.md`

---

## Pre-flight

- [ ] **Step 0: Verify clean working tree on `main`**

```bash
cd /Users/attson/code/github.com.attson/atterm
git status
git rev-parse --abbrev-ref HEAD
```

Expected: `nothing to commit, working tree clean`, branch `main`.

- [ ] **Step 0.1: Verify env / toolchain**

```bash
export PATH=$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go version          # 1.23+
node --version      # 20+
cd desktop/frontend && npm install
```

Expected: install succeeds with no errors.

- [ ] **Step 0.2: Baseline build passes**

```bash
cd /Users/attson/code/github.com.attson/atterm
go vet -tags webkit2_41 ./...
cd desktop/frontend && npm run build
```

Expected: both clean. If not, stop and fix root cause before proceeding.

---

## Task 1: Add vitest dev dependency and config

**Files:**
- Modify: `desktop/frontend/package.json`
- Create: `desktop/frontend/vitest.config.ts`
- Modify: `desktop/frontend/tsconfig.json` (only if vitest globals need typing — see step 4)

- [ ] **Step 1.1: Install vitest + jsdom + Vue Test Utils**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm install --save-dev vitest@^1.6.0 jsdom@^24.0.0 @vue/test-utils@^2.4.0
```

Expected: `package.json` updated; lockfile updated; install completes.

- [ ] **Step 1.2: Add `test` script to `package.json`**

Open `desktop/frontend/package.json`. Replace the `"scripts"` block with:

```json
"scripts": {
  "dev": "vite",
  "build": "vue-tsc --noEmit && vite build",
  "preview": "vite preview",
  "test": "vitest run",
  "test:watch": "vitest"
}
```

- [ ] **Step 1.3: Create `vitest.config.ts`**

Write `desktop/frontend/vitest.config.ts`:

```ts
import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";

export default defineConfig({
  plugins: [vue()],
  test: {
    environment: "jsdom",
    globals: false,
    include: ["src/**/*.test.ts"],
  },
});
```

- [ ] **Step 1.4: Smoke-test the runner with a trivial spec**

Write `desktop/frontend/src/lib/_smoke.test.ts`:

```ts
import { describe, it, expect } from "vitest";

describe("vitest smoke", () => {
  it("runs", () => {
    expect(1 + 1).toBe(2);
  });
});
```

Run: `cd desktop/frontend && npm test`

Expected: `1 passed`.

- [ ] **Step 1.5: Delete the smoke spec, type-check still clean**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
rm src/lib/_smoke.test.ts
npm run build
```

Expected: build clean.

- [ ] **Step 1.6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/package.json desktop/frontend/package-lock.json desktop/frontend/vitest.config.ts
git commit -m "chore: add vitest for frontend unit tests"
```

---

## Task 2: Create shared types in `lib/types.ts`

**Files:**
- Create: `desktop/frontend/src/lib/types.ts`

- [ ] **Step 2.1: Write the file**

Create `desktop/frontend/src/lib/types.ts`:

```ts
// Shared types for the per-tab pane-split layout model. See
// docs/superpowers/specs/2026-05-10-pane-split-layouts-design.md.

export type LayoutKind = "single" | "vertical" | "horizontal" | "grid2x2";

// Direction the user requests when invoking a split shortcut. Only meaningful
// from the `single` layout — both directions promote a 2-pane layout to
// `grid2x2` because there is only one geometrically valid place to put the
// new pane in the fixed 2x2 grid.
export type SplitDir = "vertical" | "horizontal";

export type FocusDir = "left" | "right" | "up" | "down";

export interface Pane {
  // null = empty slot. Empty slots happen after a session ends, after a
  // picker is canceled, and in 3-filled grid2x2 (one quadrant left empty
  // after a close).
  sessionId: string | null;
  remote: boolean;
}

export interface Tab {
  id: string;            // frontend-generated uuid; Vue key only
  layout: LayoutKind;
  panes: Pane[];         // length matches layout: 1 / 2 / 2 / 4
  activePaneIdx: number; // index in panes[] of the keyboard-focused pane
}

export const PANE_COUNT: Record<LayoutKind, number> = {
  single: 1,
  vertical: 2,
  horizontal: 2,
  grid2x2: 4,
};

export const EMPTY_PANE: Readonly<Pane> = Object.freeze({
  sessionId: null,
  remote: false,
});
```

- [ ] **Step 2.2: Type-check**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npx vue-tsc --noEmit
```

Expected: no errors.

- [ ] **Step 2.3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/types.ts
git commit -m "feat(frontend): add Tab/Pane/LayoutKind types"
```

---

## Task 3: TDD `transitionLayout` in `lib/layout.ts`

**Files:**
- Create: `desktop/frontend/src/lib/layout.ts`
- Create: `desktop/frontend/src/lib/layout.test.ts`

- [ ] **Step 3.1: Write the failing tests for `transitionLayout`**

Create `desktop/frontend/src/lib/layout.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { transitionLayout } from "./layout";
import type { Pane } from "./types";

const P = (id: string): Pane => ({ sessionId: id, remote: false });
const E: Pane = { sessionId: null, remote: false };

describe("transitionLayout", () => {
  describe("from single", () => {
    it("vertical dir → vertical layout, new pane on right", () => {
      const r = transitionLayout("single", [P("a")], 0, "vertical");
      expect(r.layout).toBe("vertical");
      expect(r.panes).toHaveLength(2);
      expect(r.panes[0].sessionId).toBe("a");
      expect(r.panes[1].sessionId).toBeNull();
      expect(r.newPaneIdx).toBe(1);
      expect(r.activePaneIdx).toBe(1);
      expect(r.noop).toBeFalsy();
    });

    it("horizontal dir → horizontal layout, new pane on bottom", () => {
      const r = transitionLayout("single", [P("a")], 0, "horizontal");
      expect(r.layout).toBe("horizontal");
      expect(r.panes).toHaveLength(2);
      expect(r.panes[0].sessionId).toBe("a");
      expect(r.panes[1].sessionId).toBeNull();
      expect(r.newPaneIdx).toBe(1);
      expect(r.activePaneIdx).toBe(1);
    });
  });

  describe("from vertical (direction-agnostic)", () => {
    it("active=left + vertical → grid2x2, new at idx 2 (BL)", () => {
      const r = transitionLayout("vertical", [P("a"), P("b")], 0, "vertical");
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", null, null]);
      expect(r.newPaneIdx).toBe(2);
      expect(r.activePaneIdx).toBe(2);
    });

    it("active=left + horizontal → same as vertical (direction ignored)", () => {
      const r = transitionLayout("vertical", [P("a"), P("b")], 0, "horizontal");
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", null, null]);
      expect(r.newPaneIdx).toBe(2);
    });

    it("active=right → new at idx 3 (BR)", () => {
      const r = transitionLayout("vertical", [P("a"), P("b")], 1, "vertical");
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", null, null]);
      expect(r.newPaneIdx).toBe(3);
      expect(r.activePaneIdx).toBe(3);
    });
  });

  describe("from horizontal (direction-agnostic)", () => {
    it("active=top → grid2x2 [TL=top, BL=bottom], new at TR=1", () => {
      const r = transitionLayout("horizontal", [P("a"), P("b")], 0, "vertical");
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", null, "b", null]);
      expect(r.newPaneIdx).toBe(1);
      expect(r.activePaneIdx).toBe(1);
    });

    it("active=top + horizontal dir → same as vertical dir", () => {
      const r = transitionLayout("horizontal", [P("a"), P("b")], 0, "horizontal");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", null, "b", null]);
      expect(r.newPaneIdx).toBe(1);
    });

    it("active=bottom → new at BR=3", () => {
      const r = transitionLayout("horizontal", [P("a"), P("b")], 1, "vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", null, "b", null]);
      expect(r.newPaneIdx).toBe(3);
      expect(r.activePaneIdx).toBe(3);
    });
  });

  describe("from grid2x2", () => {
    it("all 4 filled → noop", () => {
      const r = transitionLayout(
        "grid2x2",
        [P("a"), P("b"), P("c"), P("d")],
        0,
        "vertical",
      );
      expect(r.noop).toBe(true);
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", "c", "d"]);
      expect(r.newPaneIdx).toBe(-1);
    });

    it("with empty slot → fill lowest-idx empty, set active to it", () => {
      const r = transitionLayout(
        "grid2x2",
        [P("a"), P("b"), E, P("d")],
        0,
        "vertical",
      );
      expect(r.noop).toBeFalsy();
      expect(r.newPaneIdx).toBe(2);
      expect(r.activePaneIdx).toBe(2);
      expect(r.panes[2].sessionId).toBeNull(); // still empty, caller fills
    });

    it("with multiple empty slots → fill the lowest", () => {
      const r = transitionLayout(
        "grid2x2",
        [P("a"), E, E, P("d")],
        3,
        "horizontal",
      );
      expect(r.newPaneIdx).toBe(1);
    });
  });
});
```

- [ ] **Step 3.2: Run tests, verify they fail**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
```

Expected: failures with "Cannot find module './layout'" or similar.

- [ ] **Step 3.3: Implement `transitionLayout`**

Create `desktop/frontend/src/lib/layout.ts`:

```ts
// Pure layout state transitions. No DOM, no Vue, no IO. Tested via vitest.
// See docs/superpowers/specs/2026-05-10-pane-split-layouts-design.md.

import type {
  FocusDir,
  LayoutKind,
  Pane,
  SplitDir,
  Tab,
} from "./types";
import { EMPTY_PANE, PANE_COUNT } from "./types";

const empty = (): Pane => ({ ...EMPTY_PANE });

export interface TransitionResult {
  layout: LayoutKind;
  panes: Pane[];
  activePaneIdx: number;
  // Index in `panes` that the caller must fill with a session id (new shell
  // or picker result). -1 when noop=true.
  newPaneIdx: number;
  // True when the layout is full (grid2x2 with no empty slot). Caller should
  // surface a "pane full" toast and not start any new session.
  noop?: boolean;
}

export function transitionLayout(
  current: LayoutKind,
  panes: Pane[],
  activeIdx: number,
  dir: SplitDir,
): TransitionResult {
  if (current === "single") {
    // Direction matters here: it picks vertical vs horizontal layout.
    const nextLayout: LayoutKind = dir === "vertical" ? "vertical" : "horizontal";
    return {
      layout: nextLayout,
      panes: [{ ...panes[0] }, empty()],
      activePaneIdx: 1,
      newPaneIdx: 1,
    };
  }

  if (current === "vertical") {
    // Existing 0=left → grid2x2 0 (TL), existing 1=right → grid2x2 1 (TR).
    // New pane goes below the active column. Direction is ignored: there is
    // no other geometrically valid slot for the third pane.
    const newIdx = activeIdx === 0 ? 2 : 3;
    const next: Pane[] = [{ ...panes[0] }, { ...panes[1] }, empty(), empty()];
    return {
      layout: "grid2x2",
      panes: next,
      activePaneIdx: newIdx,
      newPaneIdx: newIdx,
    };
  }

  if (current === "horizontal") {
    // Existing 0=top → grid2x2 0 (TL), existing 1=bottom → grid2x2 2 (BL).
    // New pane goes to the right of the active row. Direction ignored.
    const newIdx = activeIdx === 0 ? 1 : 3;
    const next: Pane[] = [{ ...panes[0] }, empty(), { ...panes[1] }, empty()];
    return {
      layout: "grid2x2",
      panes: next,
      activePaneIdx: newIdx,
      newPaneIdx: newIdx,
    };
  }

  // grid2x2: fill lowest-idx empty slot, or noop if full.
  const emptyIdx = panes.findIndex((p) => p.sessionId === null);
  if (emptyIdx === -1) {
    return {
      layout: "grid2x2",
      panes: panes.map((p) => ({ ...p })),
      activePaneIdx: activeIdx,
      newPaneIdx: -1,
      noop: true,
    };
  }
  return {
    layout: "grid2x2",
    panes: panes.map((p) => ({ ...p })),
    activePaneIdx: emptyIdx,
    newPaneIdx: emptyIdx,
  };
}

// Re-export so consumers only import from layout.ts.
export type { Pane, Tab, LayoutKind, SplitDir, FocusDir };
export { PANE_COUNT, EMPTY_PANE };
```

> `closePane` and `focusNeighbor` are added in Tasks 4 and 5 — they are
> intentionally not stubbed here so that vue-tsc doesn't have to type-check
> a `never`-returning placeholder against tests that assume the real shape.

- [ ] **Step 3.4: Run tests, verify they pass**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
```

Expected: `transitionLayout` block all green.

- [ ] **Step 3.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/layout.ts desktop/frontend/src/lib/layout.test.ts
git commit -m "feat(frontend): pure transitionLayout for pane splits"
```

---

## Task 4: TDD `closePane` in `lib/layout.ts`

**Files:**
- Modify: `desktop/frontend/src/lib/layout.ts`
- Modify: `desktop/frontend/src/lib/layout.test.ts`

- [ ] **Step 4.1: Append failing tests for `closePane`**

Append to `desktop/frontend/src/lib/layout.test.ts`:

```ts
import { closePane } from "./layout";

describe("closePane", () => {
  it("single → closeTab flag", () => {
    const r = closePane("single", [P("a")], 0);
    expect(r.closeTab).toBe(true);
  });

  it("vertical close left → single with right survivor", () => {
    const r = closePane("vertical", [P("a"), P("b")], 0);
    expect(r.layout).toBe("single");
    expect(r.panes).toHaveLength(1);
    expect(r.panes[0].sessionId).toBe("b");
    expect(r.activePaneIdx).toBe(0);
  });

  it("vertical close right → single with left survivor", () => {
    const r = closePane("vertical", [P("a"), P("b")], 1);
    expect(r.layout).toBe("single");
    expect(r.panes[0].sessionId).toBe("a");
  });

  it("horizontal close top → single with bottom survivor", () => {
    const r = closePane("horizontal", [P("a"), P("b")], 0);
    expect(r.layout).toBe("single");
    expect(r.panes[0].sessionId).toBe("b");
  });

  describe("grid2x2", () => {
    it("4 filled, close 1 → grid2x2 with 3 filled (one null)", () => {
      const r = closePane("grid2x2", [P("a"), P("b"), P("c"), P("d")], 0);
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual([null, "b", "c", "d"]);
      // active follows lowest filled idx
      expect(r.activePaneIdx).toBe(1);
    });

    it("3 filled (TL,TR,BL), close BL → 2 filled top row → vertical", () => {
      const r = closePane("grid2x2", [P("a"), P("b"), P("c"), E], 2);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b"]);
      expect(r.activePaneIdx).toBe(0); // first survivor
    });

    it("3 filled (TL,BL,BR), close TL → 2 filled bottom row → vertical", () => {
      const r = closePane("grid2x2", [P("a"), E, P("c"), P("d")], 0);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["c", "d"]);
    });

    it("3 filled, after close left column remains → horizontal", () => {
      // panes: TL=a, TR=null, BL=c, BR=d → close BR → {0,2} remain → horizontal
      const r = closePane("grid2x2", [P("a"), E, P("c"), P("d")], 3);
      expect(r.layout).toBe("horizontal");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "c"]);
    });

    it("3 filled, after close right column remains → horizontal", () => {
      // panes: TL=null, TR=b, BL=c, BR=d → close BL → {1,3} remain → horizontal
      const r = closePane("grid2x2", [E, P("b"), P("c"), P("d")], 2);
      expect(r.layout).toBe("horizontal");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["b", "d"]);
    });

    it("diagonal {0,3} → vertical sorted by index", () => {
      // panes after a sequence of closes: TL=a, TR=null, BL=null, BR=d
      // Now close one of the empties (no-op session-wise) — but exercise via
      // the path: simulate by starting from {a,_,_,d} and closing idx 1 (E).
      // Closing an empty slot still routes through closePane.
      const r = closePane("grid2x2", [P("a"), E, E, P("d")], 1);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "d"]);
    });

    it("diagonal {1,2} → vertical sorted by index", () => {
      const r = closePane("grid2x2", [E, P("b"), P("c"), E], 0);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["b", "c"]);
    });

    it("closing the last filled pane → closeTab signal", () => {
      const r = closePane("grid2x2", [P("a"), E, E, E], 0);
      expect(r.closeTab).toBe(true);
      expect(r.layout).toBe("single");
      expect(r.panes[0].sessionId).toBeNull();
    });

    it("closing the last filled (different slot) → still closeTab", () => {
      const r = closePane("grid2x2", [E, P("b"), E, E], 1);
      expect(r.closeTab).toBe(true);
    });
  });
});
```

- [ ] **Step 4.2: Run tests, verify they fail**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
```

Expected: `closePane` block fails (current stub throws).

- [ ] **Step 4.3: Implement `closePane`**

Append to `desktop/frontend/src/lib/layout.ts` (just before the final
`export type` block):

```ts
export interface CloseResult {
  layout: LayoutKind;
  panes: Pane[];
  activePaneIdx: number;
  // True when the calling tab should be closed entirely:
  //   - layout was `single`
  //   - layout becomes `single` with no survivor (no filled panes left)
  closeTab?: boolean;
}

export function closePane(
  layout: LayoutKind,
  panes: Pane[],
  closeIdx: number,
): CloseResult {
  if (layout === "single") {
    return {
      layout: "single",
      panes: panes.map((p) => ({ ...p })),
      activePaneIdx: 0,
      closeTab: true,
    };
  }

  if (layout === "vertical" || layout === "horizontal") {
    const survivorIdx = closeIdx === 0 ? 1 : 0;
    return {
      layout: "single",
      panes: [{ ...panes[survivorIdx] }],
      activePaneIdx: 0,
    };
  }

  // grid2x2
  const next = panes.map((p, i) => (i === closeIdx ? empty() : { ...p }));
  const filledIndices = next
    .map((p, i) => (p.sessionId !== null ? i : -1))
    .filter((i) => i >= 0);
  const filled = filledIndices.length;

  if (filled >= 3) {
    // Stay in grid2x2 with one slot null. New active = lowest filled idx.
    return {
      layout: "grid2x2",
      panes: next,
      activePaneIdx: filledIndices[0],
    };
  }

  if (filled === 2) {
    return reduceTwoFilled(next, filledIndices);
  }

  if (filled === 1) {
    // Single survivor; remaining state collapses to single.
    return {
      layout: "single",
      panes: [{ ...next[filledIndices[0]] }],
      activePaneIdx: 0,
    };
  }

  // filled === 0 — last session is gone, ask App.vue to close the tab.
  return {
    layout: "single",
    panes: [empty()],
    activePaneIdx: 0,
    closeTab: true,
  };
}

function reduceTwoFilled(panes: Pane[], idx: number[]): CloseResult {
  // panes is the post-close grid2x2 slice (length 4); idx are the two filled
  // indices in ascending order. Map back to vertical/horizontal:
  //   {0,1} top row    → vertical  [pane@0, pane@1]
  //   {2,3} bottom row → vertical  [pane@2, pane@3]
  //   {0,2} left col   → horizontal[pane@0, pane@2]
  //   {1,3} right col  → horizontal[pane@1, pane@3]
  //   {0,3} diagonal   → vertical  [pane@0, pane@3]   (sorted by idx)
  //   {1,2} diagonal   → vertical  [pane@1, pane@2]
  const [a, b] = idx;
  const sameCol = (a === 0 && b === 2) || (a === 1 && b === 3);
  const layout: LayoutKind = sameCol ? "horizontal" : "vertical"; // top-row, bottom-row, and diagonals all map to vertical
  return {
    layout,
    panes: [{ ...panes[a] }, { ...panes[b] }],
    activePaneIdx: 0,
  };
}
```

- [ ] **Step 4.4: Run tests**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
```

Expected: all `closePane` tests pass.

- [ ] **Step 4.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/layout.ts desktop/frontend/src/lib/layout.test.ts
git commit -m "feat(frontend): pure closePane reduction"
```

---

## Task 5: TDD `focusNeighbor` in `lib/layout.ts`

**Files:**
- Modify: `desktop/frontend/src/lib/layout.ts`
- Modify: `desktop/frontend/src/lib/layout.test.ts`

- [ ] **Step 5.1: Append failing tests**

Append to `desktop/frontend/src/lib/layout.test.ts`:

```ts
import { focusNeighbor } from "./layout";

describe("focusNeighbor", () => {
  it("single → all dirs return null", () => {
    expect(focusNeighbor("single", 0, "left")).toBeNull();
    expect(focusNeighbor("single", 0, "right")).toBeNull();
    expect(focusNeighbor("single", 0, "up")).toBeNull();
    expect(focusNeighbor("single", 0, "down")).toBeNull();
  });

  it("vertical: 0 ↔ 1 horizontal only", () => {
    expect(focusNeighbor("vertical", 0, "right")).toBe(1);
    expect(focusNeighbor("vertical", 1, "left")).toBe(0);
    expect(focusNeighbor("vertical", 0, "left")).toBeNull();
    expect(focusNeighbor("vertical", 0, "up")).toBeNull();
    expect(focusNeighbor("vertical", 0, "down")).toBeNull();
  });

  it("horizontal: 0 ↔ 1 vertical only", () => {
    expect(focusNeighbor("horizontal", 0, "down")).toBe(1);
    expect(focusNeighbor("horizontal", 1, "up")).toBe(0);
    expect(focusNeighbor("horizontal", 0, "left")).toBeNull();
    expect(focusNeighbor("horizontal", 0, "right")).toBeNull();
  });

  it("grid2x2: every quadrant has the right two neighbors", () => {
    // 0 (TL): right=1, down=2
    expect(focusNeighbor("grid2x2", 0, "right")).toBe(1);
    expect(focusNeighbor("grid2x2", 0, "down")).toBe(2);
    expect(focusNeighbor("grid2x2", 0, "left")).toBeNull();
    expect(focusNeighbor("grid2x2", 0, "up")).toBeNull();
    // 1 (TR): left=0, down=3
    expect(focusNeighbor("grid2x2", 1, "left")).toBe(0);
    expect(focusNeighbor("grid2x2", 1, "down")).toBe(3);
    // 2 (BL): up=0, right=3
    expect(focusNeighbor("grid2x2", 2, "up")).toBe(0);
    expect(focusNeighbor("grid2x2", 2, "right")).toBe(3);
    // 3 (BR): up=1, left=2
    expect(focusNeighbor("grid2x2", 3, "up")).toBe(1);
    expect(focusNeighbor("grid2x2", 3, "left")).toBe(2);
  });
});
```

- [ ] **Step 5.2: Run tests, verify they fail**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
```

Expected: failures from the stub `throw new Error("focusNeighbor not implemented yet")`.

- [ ] **Step 5.3: Implement `focusNeighbor`**

Append to `desktop/frontend/src/lib/layout.ts` (just before the final
`export type` block):

```ts
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
```

- [ ] **Step 5.4: Run tests**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
```

Expected: all green.

- [ ] **Step 5.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/layout.ts desktop/frontend/src/lib/layout.test.ts
git commit -m "feat(frontend): pure focusNeighbor for pane navigation"
```

---

## Task 6: Add `useTerminalShortcuts` composable

**Files:**
- Create: `desktop/frontend/src/composables/useTerminalShortcuts.ts`
- Create: `desktop/frontend/src/composables/useTerminalShortcuts.test.ts`

- [ ] **Step 6.1: Write the failing tests**

Create `desktop/frontend/src/composables/useTerminalShortcuts.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { effectScope } from "vue";
import { useTerminalShortcuts } from "./useTerminalShortcuts";

function fireKey(opts: KeyboardEventInit & { key: string }) {
  const ev = new KeyboardEvent("keydown", { ...opts, bubbles: true, cancelable: true });
  document.dispatchEvent(ev);
  return ev;
}

describe("useTerminalShortcuts", () => {
  let scope: ReturnType<typeof effectScope>;
  const handlers = {
    onSplitVertical: vi.fn(),
    onSplitHorizontal: vi.fn(),
    onClosePane: vi.fn(),
    onFocusPane: vi.fn(),
    onNewTab: vi.fn(),
    onSwitchTab: vi.fn(),
  };

  beforeEach(() => {
    Object.values(handlers).forEach((h) => h.mockReset());
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control" }));
  });

  afterEach(() => {
    scope.stop();
  });

  it("Ctrl+D → onSplitVertical('new')", () => {
    fireKey({ key: "d", ctrlKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledWith("new");
  });

  it("Ctrl+Shift+D → onSplitHorizontal('new')", () => {
    fireKey({ key: "D", ctrlKey: true, shiftKey: true });
    expect(handlers.onSplitHorizontal).toHaveBeenCalledWith("new");
  });

  it("Ctrl+Alt+D → onSplitVertical('pick')", () => {
    fireKey({ key: "d", ctrlKey: true, altKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledWith("pick");
  });

  it("Ctrl+Alt+Shift+D → onSplitHorizontal('pick')", () => {
    fireKey({ key: "D", ctrlKey: true, altKey: true, shiftKey: true });
    expect(handlers.onSplitHorizontal).toHaveBeenCalledWith("pick");
  });

  it("Ctrl+W → onClosePane", () => {
    fireKey({ key: "w", ctrlKey: true });
    expect(handlers.onClosePane).toHaveBeenCalled();
  });

  it("Ctrl+T → onNewTab", () => {
    fireKey({ key: "t", ctrlKey: true });
    expect(handlers.onNewTab).toHaveBeenCalled();
  });

  it("Ctrl+Alt+ArrowLeft → onFocusPane('left')", () => {
    fireKey({ key: "ArrowLeft", ctrlKey: true, altKey: true });
    expect(handlers.onFocusPane).toHaveBeenCalledWith("left");
  });

  it("Ctrl+Alt+ArrowRight → onFocusPane('right')", () => {
    fireKey({ key: "ArrowRight", ctrlKey: true, altKey: true });
    expect(handlers.onFocusPane).toHaveBeenCalledWith("right");
  });

  it("Ctrl+Shift+] → onSwitchTab(+1)", () => {
    fireKey({ key: "]", ctrlKey: true, shiftKey: true });
    expect(handlers.onSwitchTab).toHaveBeenCalledWith(1);
  });

  it("Ctrl+Shift+[ → onSwitchTab(-1)", () => {
    fireKey({ key: "[", ctrlKey: true, shiftKey: true });
    expect(handlers.onSwitchTab).toHaveBeenCalledWith(-1);
  });

  it("plain D (no modifier) → ignored", () => {
    fireKey({ key: "d" });
    expect(handlers.onSplitVertical).not.toHaveBeenCalled();
  });

  it("Ctrl+D preventDefault + stopPropagation", () => {
    const ev = fireKey({ key: "d", ctrlKey: true });
    expect(ev.defaultPrevented).toBe(true);
  });

  it("scope.stop unbinds the listener", () => {
    scope.stop();
    fireKey({ key: "d", ctrlKey: true });
    expect(handlers.onSplitVertical).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 6.2: Run tests, verify they fail**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
```

Expected: failures from missing module.

- [ ] **Step 6.3: Implement the composable**

Create `desktop/frontend/src/composables/useTerminalShortcuts.ts`:

```ts
// Document-level capture-phase keydown router. Listens before xterm.js so
// Ctrl/Cmd combos we care about never reach the terminal. See spec §"Shortcuts".

import { onScopeDispose } from "vue";
import type { FocusDir } from "../lib/types";

export type SplitMode = "new" | "pick";

export interface ShortcutHandlers {
  onSplitVertical: (mode: SplitMode) => void;
  onSplitHorizontal: (mode: SplitMode) => void;
  onClosePane: () => void;
  onFocusPane: (dir: FocusDir) => void;
  onNewTab: () => void;
  onSwitchTab: (delta: number) => void;
}

export interface ShortcutOptions {
  // Override the modifier-key detection. Default: "Meta" on Mac, "Control"
  // elsewhere. Tests use this to force "Control" for portability.
  mod?: "Meta" | "Control";
}

function detectMod(): "Meta" | "Control" {
  if (typeof navigator === "undefined") return "Control";
  return navigator.platform?.toLowerCase().includes("mac") ? "Meta" : "Control";
}

const ARROW_TO_DIR: Record<string, FocusDir> = {
  ArrowLeft: "left",
  ArrowRight: "right",
  ArrowUp: "up",
  ArrowDown: "down",
};

export function useTerminalShortcuts(
  h: ShortcutHandlers,
  opts: ShortcutOptions = {},
): void {
  const mod = opts.mod ?? detectMod();
  const isMod = (e: KeyboardEvent) => (mod === "Meta" ? e.metaKey : e.ctrlKey);
  // The "wrong" modifier — Cmd on Linux/Win shouldn't trigger us, and Ctrl
  // on Mac shouldn't either, to avoid double-binding.
  const wrongMod = (e: KeyboardEvent) => (mod === "Meta" ? e.ctrlKey : e.metaKey);

  function handler(e: KeyboardEvent) {
    if (!isMod(e) || wrongMod(e)) return;
    const key = e.key.length === 1 ? e.key.toLowerCase() : e.key;

    // Modifier groups for d / shift+d: alt presence flips mode to "pick".
    if (key === "d") {
      e.preventDefault();
      e.stopPropagation();
      const mode: SplitMode = e.altKey ? "pick" : "new";
      if (e.shiftKey) h.onSplitHorizontal(mode);
      else h.onSplitVertical(mode);
      return;
    }
    if (key === "w" && !e.altKey) {
      e.preventDefault();
      e.stopPropagation();
      h.onClosePane();
      return;
    }
    if (key === "t" && !e.altKey && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      h.onNewTab();
      return;
    }
    if (e.altKey && ARROW_TO_DIR[e.key]) {
      e.preventDefault();
      e.stopPropagation();
      h.onFocusPane(ARROW_TO_DIR[e.key]);
      return;
    }
    if (e.shiftKey && (key === "[" || key === "]")) {
      e.preventDefault();
      e.stopPropagation();
      h.onSwitchTab(key === "]" ? 1 : -1);
      return;
    }
  }

  document.addEventListener("keydown", handler, { capture: true });
  onScopeDispose(() => {
    document.removeEventListener("keydown", handler, { capture: true } as EventListenerOptions);
  });
}
```

- [ ] **Step 6.4: Run tests**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
```

Expected: all green.

- [ ] **Step 6.5: Type-check the project**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm run build
```

Expected: clean.

- [ ] **Step 6.6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/composables/useTerminalShortcuts.ts desktop/frontend/src/composables/useTerminalShortcuts.test.ts
git commit -m "feat(frontend): document-capture shortcut composable"
```

---

## Task 7: Add `focused` prop to `TerminalView.vue`

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`

- [ ] **Step 7.1: Add prop and visual border**

Edit `desktop/frontend/src/components/TerminalView.vue`. Replace the
`withDefaults(...)` block and the `<template>` + `<style scoped>` blocks as
follows.

Find:

```ts
const props = withDefaults(
  defineProps<{
    endpoint: Endpoint;
    sessionId: string;
    active?: boolean;
  }>(),
  { active: true }
);
```

Replace with:

```ts
const props = withDefaults(
  defineProps<{
    endpoint: Endpoint;
    sessionId: string;
    active?: boolean;
    focused?: boolean;
  }>(),
  { active: true, focused: false }
);
```

Find:

```vue
  <div class="term-view">
    <div ref="termContainer" class="term"></div>
```

Replace with:

```vue
  <div class="term-view" :class="{ focused }">
    <div ref="termContainer" class="term"></div>
```

In the `<style scoped>` block, append before the closing `</style>`:

```css
.term-view.focused {
  box-shadow: inset 0 0 0 1px var(--accent);
}
```

- [ ] **Step 7.2: Type-check**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm run build
```

Expected: clean. (The new prop has a default, so no consumer is broken.)

- [ ] **Step 7.3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/TerminalView.vue
git commit -m "feat(frontend): focused prop + accent border on TerminalView"
```

---

## Task 8: Add `PaneGrid.vue`

**Files:**
- Create: `desktop/frontend/src/components/PaneGrid.vue`

- [ ] **Step 8.1: Write the component**

Create `desktop/frontend/src/components/PaneGrid.vue`:

```vue
<script lang="ts" setup>
import { computed } from "vue";
import TerminalView from "./TerminalView.vue";
import type { Endpoint } from "../lib/api";
import type { Pane, Tab } from "../lib/types";

const props = defineProps<{
  tab: Tab;
  endpointFor: (pane: Pane) => Endpoint | null;
  active: boolean;     // owning tab is the currently visible one
}>();

const emit = defineEmits<{
  (e: "set-active-pane", paneIdx: number): void;
  (e: "close-pane", paneIdx: number): void;
}>();

// Map pane index → CSS grid-area letter for the current layout.
const AREA_FOR_LAYOUT = {
  single:     ["a"],
  vertical:   ["a", "b"],
  horizontal: ["a", "b"],
  grid2x2:    ["a", "b", "c", "d"],
} as const;

const areaFor = computed(() => AREA_FOR_LAYOUT[props.tab.layout]);

function onPaneClick(idx: number) {
  if (idx !== props.tab.activePaneIdx) emit("set-active-pane", idx);
}
</script>

<template>
  <div class="pane-grid" :class="tab.layout">
    <div
      v-for="(pane, idx) in tab.panes"
      :key="idx"
      class="cell"
      :style="{ gridArea: areaFor[idx] }"
      @mousedown="onPaneClick(idx)"
    >
      <TerminalView
        v-if="pane.sessionId && endpointFor(pane)"
        :endpoint="endpointFor(pane)!"
        :session-id="pane.sessionId"
        :active="active"
        :focused="active && idx === tab.activePaneIdx"
      />
      <div v-else class="empty">[empty pane — press ⌘D / Ctrl+D to fill]</div>
      <button
        v-if="tab.layout !== 'single'"
        class="close-pane"
        title="close pane (⌘W / Ctrl+W)"
        @click.stop="emit('close-pane', idx)"
      >×</button>
    </div>
  </div>
</template>

<style scoped>
.pane-grid {
  position: absolute;
  inset: 0;
  display: grid;
  gap: 2px;
  background: #11161d; /* divider color */
}
.pane-grid.single     { grid-template: "a"; }
.pane-grid.vertical   { grid-template: "a b" / 1fr 1fr; }
.pane-grid.horizontal { grid-template: "a" 1fr "b" 1fr; }
.pane-grid.grid2x2    { grid-template: "a b" 1fr "c d" 1fr / 1fr 1fr; }

.cell {
  position: relative;
  background: #000;
  overflow: hidden;
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
.close-pane {
  position: absolute;
  top: 4px;
  right: 4px;
  border: none;
  background: rgba(13, 17, 23, 0.7);
  color: var(--fg-dim);
  font-size: 14px;
  line-height: 1;
  padding: 2px 6px;
  border-radius: 4px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 120ms, background 120ms, color 120ms;
}
.cell:hover .close-pane { opacity: 1; }
.close-pane:hover {
  background: rgba(248, 81, 73, 0.18);
  color: var(--bad);
}
</style>
```

- [ ] **Step 8.2: Type-check**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm run build
```

Expected: clean. (Component has no consumer yet; this just verifies it compiles.)

- [ ] **Step 8.3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/PaneGrid.vue
git commit -m "feat(frontend): PaneGrid component for layout rendering"
```

---

## Task 9: Add `SessionPickerDialog.vue`

**Files:**
- Create: `desktop/frontend/src/components/SessionPickerDialog.vue`

- [ ] **Step 9.1: Write the component**

Create `desktop/frontend/src/components/SessionPickerDialog.vue`:

```vue
<script lang="ts" setup>
import { computed, onMounted, onBeforeUnmount } from "vue";
import type { SessionInfo } from "../lib/connection";

const props = defineProps<{
  excludeSessionIds: string[];
  localSessions: SessionInfo[];
  remoteSessions: SessionInfo[];
}>();

const emit = defineEmits<{
  (e: "pick", payload: { sessionId: string; remote: boolean }): void;
  (e: "close"): void;
}>();

const exclude = computed(() => new Set(props.excludeSessionIds));
const localOptions = computed(() =>
  props.localSessions.filter((s) => !exclude.value.has(s.id)),
);
const remoteOptions = computed(() =>
  props.remoteSessions.filter((s) => !exclude.value.has(s.id)),
);

function onEsc(e: KeyboardEvent) {
  if (e.key === "Escape") emit("close");
}
onMounted(() => document.addEventListener("keydown", onEsc));
onBeforeUnmount(() => document.removeEventListener("keydown", onEsc));

function shortTitle(s: SessionInfo): string {
  if (s.cwd) {
    const stripped = s.cwd.replace(/\/+$/, "");
    if (stripped !== "") return stripped.split("/").pop() || stripped;
  }
  const first = (s.command || "").split(/\s+/)[0] || "shell";
  return first.split("/").pop() || first;
}
</script>

<template>
  <div class="backdrop" @click.self="emit('close')">
    <div class="dialog">
      <h2>pick a session</h2>

      <div v-if="localOptions.length + remoteOptions.length === 0" class="empty">
        no sessions available — none running locally and no eligible remote.
      </div>

      <template v-else>
        <section v-if="localOptions.length > 0">
          <h3>local</h3>
          <div class="grid">
            <button
              v-for="s in localOptions"
              :key="s.id"
              class="card"
              @click="emit('pick', { sessionId: s.id, remote: false })"
            >
              <div class="title">{{ shortTitle(s) }}</div>
              <div class="meta">
                <span class="cmd">{{ s.command || "(unknown)" }}</span>
                <span class="cwd">{{ s.cwd }}</span>
              </div>
            </button>
          </div>
        </section>

        <section v-if="remoteOptions.length > 0">
          <h3>remote</h3>
          <div class="grid">
            <button
              v-for="s in remoteOptions"
              :key="s.id"
              class="card remote"
              @click="emit('pick', { sessionId: s.id, remote: true })"
            >
              <div class="title">{{ shortTitle(s) }}</div>
              <div class="meta">
                <span class="cmd">{{ s.command || "(unknown)" }}</span>
                <span class="cwd">{{ s.cwd }}</span>
                <span class="who">{{ (s.user || "") + "@" + (s.host || "") }}</span>
              </div>
            </button>
          </div>
        </section>
      </template>

      <div class="row">
        <button class="cancel" @click="emit('close')">cancel (esc)</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 100;
}
.dialog {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: 720px;
  max-width: calc(100vw - 32px); max-height: calc(100vh - 64px);
  display: flex; flex-direction: column; gap: 12px;
}
h2 {
  margin: 0; font-size: 14px; font-weight: 600; letter-spacing: 0.05em;
  text-transform: uppercase; color: var(--fg-dim);
}
h3 {
  margin: 12px 0 6px; font-size: 11px; letter-spacing: 0.08em;
  text-transform: uppercase; color: var(--fg-dim);
}
.empty {
  color: var(--fg-dim); font-size: 13px; text-align: center; padding: 40px 0;
}
.grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px; max-height: 30vh; overflow-y: auto;
}
.card {
  text-align: left; background: #0d1117; border: 1px solid var(--border);
  border-radius: 6px; padding: 10px 12px; cursor: pointer;
  transition: border-color 120ms; color: var(--fg);
  font-family: inherit;
}
.card:hover { border-color: var(--accent); }
.card.remote .title { color: #d29922; }
.card .title {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px; margin-bottom: 4px;
}
.card .meta {
  font-size: 11px; color: var(--fg-dim);
  display: flex; gap: 10px; flex-wrap: wrap;
}
.card .meta .cwd { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 100%; }
.row {
  display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px;
}
.cancel {
  padding: 6px 12px; background: transparent; border: 1px solid var(--border);
  color: var(--fg-dim); border-radius: 4px; cursor: pointer;
}
.cancel:hover { color: var(--fg); border-color: var(--accent); }
</style>
```

- [ ] **Step 9.2: Type-check**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm run build
```

Expected: clean.

- [ ] **Step 9.3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/SessionPickerDialog.vue
git commit -m "feat(frontend): SessionPickerDialog for split-pick flow"
```

---

## Task 10: Refactor `App.vue` to tab-with-panes model

This is the central wiring step. It changes:
- `sessions` (flat) → `tabs` (each owns a layout + panes)
- URL hash `#/s/<sid>` → `#/t/<tabId>`
- New shortcut wiring via `useTerminalShortcuts`
- Picker dialog state machine
- Pane sweep against poll results

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/components/TabBar.vue`

- [ ] **Step 10.1: Update `TabBar.vue` for the new tab model**

Replace `desktop/frontend/src/components/TabBar.vue` entirely with:

```vue
<script lang="ts" setup>
import type { SessionInfo } from "../lib/connection";
import type { Tab } from "../lib/types";

interface TabSummary {
  id: string;
  layout: Tab["layout"];
  // Session of the active pane in this tab (used for the title).
  activeSession: SessionInfo | null;
  // True if the active pane references a remote session.
  activeRemote: boolean;
  paneCount: number;
}

const props = defineProps<{
  tabs: TabSummary[];
  currentId: string | null;
  starting: boolean;
}>();

const emit = defineEmits<{
  (e: "activate", id: string): void;
  (e: "close", id: string): void;
  (e: "new"): void;
}>();

function shortTitle(s: SessionInfo | null): string {
  if (!s) return "(empty)";
  if (s.cwd) {
    const stripped = s.cwd.replace(/\/+$/, "");
    if (stripped === "") return "/";
    const base = stripped.split("/").pop();
    if (base) return base;
  }
  const first = (s.command || "").split(/\s+/)[0] || "shell";
  return first.split("/").pop() || first;
}

function layoutLabel(t: TabSummary): string {
  switch (t.layout) {
    case "single": return "";
    case "vertical": return "▮▮";
    case "horizontal": return "▬\n▬";
    case "grid2x2": return "▦";
  }
}

function onClose(e: MouseEvent, id: string) {
  e.stopPropagation();
  emit("close", id);
}
</script>

<template>
  <div class="tabbar">
    <div class="tabs">
      <div
        v-for="(t, idx) in tabs"
        :key="t.id"
        class="tab"
        :class="{ active: t.id === currentId, remote: t.activeRemote }"
        :title="(t.activeRemote ? '[remote] ' : '') + (t.activeSession?.command ?? '')"
        @click="emit('activate', t.id)"
      >
        <span class="num">{{ idx + 1 }}:</span>
        <span v-if="t.layout !== 'single'" class="layout-icon" :title="t.layout">{{ layoutLabel(t) }}</span>
        <span v-else-if="t.activeRemote" class="dot remote-dot">●</span>
        <span v-else class="dot">●</span>
        <span class="title">{{ shortTitle(t.activeSession) }}</span>
        <button class="close" @click="onClose($event, t.id)">×</button>
      </div>
    </div>
    <button
      class="plus"
      :disabled="starting"
      :title="starting ? 'starting…' : 'new tab'"
      @click="emit('new')"
    >+</button>
  </div>
</template>

<style scoped>
.tabbar {
  display: flex; align-items: stretch; background: var(--panel);
  border-bottom: 1px solid var(--border); flex: 0 0 auto; height: 34px;
  overflow: hidden;
}
.tabs { display: flex; flex: 1 1 auto; overflow-x: auto; scrollbar-width: thin; }
.tabs::-webkit-scrollbar { height: 4px; }
.tabs::-webkit-scrollbar-thumb { background: var(--border); }

.tab {
  display: flex; align-items: center; gap: 6px; padding: 0 8px 0 12px;
  border-right: 1px solid var(--border); font-size: 12px;
  color: var(--fg-dim); cursor: pointer; user-select: none;
  white-space: nowrap; min-width: 110px; max-width: 220px;
  transition: background 120ms;
}
.tab:hover { background: rgba(255, 255, 255, 0.04); }
.tab.active {
  background: var(--bg); color: var(--fg);
  box-shadow: inset 0 -2px 0 var(--accent);
}
.tab .num {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px; color: var(--fg-dim);
}
.tab.active .num { color: var(--accent); }
.tab .dot { font-size: 9px; color: var(--good); }
.tab .remote-dot { color: #d29922; }
.tab .layout-icon {
  font-size: 11px; color: var(--fg-dim);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  letter-spacing: -1px;
}
.tab .title {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis;
}
.tab .close {
  border: none; background: transparent; padding: 0 4px; font-size: 14px;
  line-height: 1; color: var(--fg-dim); border-radius: 4px; opacity: 0;
  transition: opacity 120ms, background 120ms, color 120ms;
}
.tab:hover .close, .tab.active .close { opacity: 1; }
.tab .close:hover { background: rgba(248, 81, 73, 0.18); color: var(--bad); }
.plus {
  border: none; background: transparent; color: var(--fg-dim);
  font-size: 18px; line-height: 1; padding: 0 14px; cursor: pointer;
  border-left: 1px solid var(--border); transition: color 120ms, background 120ms;
}
.plus:hover:not(:disabled) { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.plus:disabled { opacity: 0.4; cursor: not-allowed; }
</style>
```

- [ ] **Step 10.2: Replace `App.vue` script + template**

Replace `desktop/frontend/src/App.vue` entirely with:

```vue
<script lang="ts" setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import TabBar from "./components/TabBar.vue";
import PaneGrid from "./components/PaneGrid.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import RemoteSessionsDialog from "./components/RemoteSessionsDialog.vue";
import SessionPickerDialog from "./components/SessionPickerDialog.vue";
import {
  closeSession,
  getEndpoint,
  getHostInfo,
  getRelayConfig,
  listShells,
  newSession,
} from "./lib/api";
import type { Endpoint } from "./lib/api";
import { fetchSessions, type SessionInfo } from "./lib/connection";
import type { Pane, Tab, SplitDir } from "./lib/types";
import { closePane, focusNeighbor, transitionLayout } from "./lib/layout";
import { useTerminalShortcuts, type SplitMode } from "./composables/useTerminalShortcuts";

const localEndpoint = ref<Endpoint | null>(null);
const remoteEndpoint = ref<Endpoint | null>(null);
const localHostID = ref<string>("");

const localList = ref<SessionInfo[]>([]);
const remoteList = ref<SessionInfo[]>([]);

const tabs = ref<Tab[]>([]);
const currentTabId = ref<string | null>(null);

const status = ref<"loading" | "ready" | "error">("loading");
const errorMsg = ref<string>("");
const starting = ref(false);
const showSettings = ref(false);
const showRemote = ref(false);
const toast = ref<string>("");

// Picker state. When non-null, dialog is open and the resolved pick will go
// into tabs[*].panes[paneIdx] of the indicated tab (always the current tab).
const pickerCtx = ref<{ tabId: string; paneIdx: number } | null>(null);

let autoStarted = false;
let pollHandle: number | null = null;
let toastHandle: number | null = null;

const newId = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : "tab-" + Math.random().toString(36).slice(2);

const currentTab = computed(() => tabs.value.find((t) => t.id === currentTabId.value) ?? null);

// Sessions visible across all current tabs (used to decide which local panes
// reference live sessions, drives sweep on poll).
const allUsedSessionIds = computed(() => {
  const s = new Set<string>();
  for (const t of tabs.value) for (const p of t.panes) if (p.sessionId) s.add(p.sessionId);
  return s;
});

// Remote sessions still available to open as a fresh tab — i.e. not already
// referenced by any pane.
const availableRemote = computed<SessionInfo[]>(() =>
  remoteList.value.filter((r) => !allUsedSessionIds.value.has(r.id)),
);

function endpointFor(pane: Pane): Endpoint | null {
  return pane.remote ? remoteEndpoint.value : localEndpoint.value;
}

function showToast(msg: string) {
  toast.value = msg;
  if (toastHandle !== null) window.clearTimeout(toastHandle);
  toastHandle = window.setTimeout(() => {
    toast.value = "";
    toastHandle = null;
  }, 2000);
}

async function pollSessions() {
  if (!localEndpoint.value) return;
  let cfg = { url: "", token: "", connected: false };
  try {
    cfg = await getRelayConfig();
  } catch {
    /* keep last known */
  }
  remoteEndpoint.value = cfg.connected && cfg.url
    ? { url: cfg.url, token: cfg.token }
    : null;

  const local = await fetchSessions(localEndpoint.value).catch(() => [] as SessionInfo[]);
  const remote: SessionInfo[] = remoteEndpoint.value
    ? await fetchSessions(remoteEndpoint.value).catch(() => [] as SessionInfo[])
    : [];

  const localIds = new Set(local.map((s) => s.id));
  const filteredRemote = remote.filter((s) => !localIds.has(s.id));

  localList.value = local;
  remoteList.value = filteredRemote;

  // Sweep: if any pane references a session id no longer reported, null it.
  const remoteIds = new Set(filteredRemote.map((s) => s.id));
  for (const t of tabs.value) {
    for (let i = 0; i < t.panes.length; i++) {
      const p = t.panes[i];
      if (!p.sessionId) continue;
      if (p.remote ? !remoteIds.has(p.sessionId) : !localIds.has(p.sessionId)) {
        t.panes[i] = { sessionId: null, remote: p.remote };
      }
    }
  }

  if (status.value !== "ready") status.value = "ready";
}

function parseHash(): string | null {
  const m = location.hash.match(/^#\/t\/([\w-]+)$/);
  return m ? m[1] : null;
}
function syncRoute() {
  const id = parseHash();
  if (id && tabs.value.some((t) => t.id === id)) currentTabId.value = id;
}
function gotoTab(id: string) {
  if (location.hash !== "#/t/" + id) {
    location.hash = "#/t/" + id;
  } else {
    currentTabId.value = id;
  }
}

function findSessionInfo(sid: string, remote: boolean): SessionInfo | undefined {
  return (remote ? remoteList.value : localList.value).find((s) => s.id === sid);
}

async function spawnLocalShell(cwd: string): Promise<string> {
  const shells = await listShells();
  if (shells.length === 0) throw new Error("no shells found on this machine");
  const resp = await newSession({ command: shells[0], cwd });
  // Ensure local list reflects the new session immediately so PaneGrid finds
  // its endpoint without waiting for the next poll tick.
  localList.value = [
    ...localList.value,
    {
      id: resp.session_id,
      command: shells[0],
      cwd: cwd || "",
      title: shells[0],
      cols: 80,
      rows: 24,
      started_at: Math.floor(Date.now() / 1000),
      host_id: localHostID.value,
    },
  ];
  return resp.session_id;
}

async function startNewTab() {
  if (starting.value) return;
  starting.value = true;
  errorMsg.value = "";
  try {
    const sid = await spawnLocalShell("");
    const id = newId();
    tabs.value.push({
      id,
      layout: "single",
      panes: [{ sessionId: sid, remote: false }],
      activePaneIdx: 0,
    });
    gotoTab(id);
    pollSessions();
  } catch (e: any) {
    status.value = "error";
    errorMsg.value = e?.message ?? String(e);
  } finally {
    starting.value = false;
  }
}

async function onSplit(dir: SplitDir, mode: SplitMode) {
  const t = currentTab.value;
  if (!t) return;

  // Capture the active pane's cwd BEFORE mutation. After transitionLayout the
  // active idx points at the new (empty) slot, so we'd lose the parent.
  let parentCwd = "";
  const activePane: Pane | undefined = t.panes[t.activePaneIdx];
  if (activePane?.sessionId && !activePane.remote) {
    const info = findSessionInfo(activePane.sessionId, false);
    if (info?.cwd) parentCwd = info.cwd;
  }

  const result = transitionLayout(t.layout, t.panes, t.activePaneIdx, dir);
  if (result.noop) {
    showToast("pane full — close one first");
    return;
  }

  // Apply the new layout shape immediately so the user sees the empty pane.
  t.layout = result.layout;
  t.panes = result.panes;
  t.activePaneIdx = result.activePaneIdx;

  if (mode === "pick") {
    pickerCtx.value = { tabId: t.id, paneIdx: result.newPaneIdx };
    return;
  }

  // mode === "new": spawn a shell with the parent's cwd we captured above.
  try {
    const sid = await spawnLocalShell(parentCwd);
    t.panes[result.newPaneIdx] = { sessionId: sid, remote: false };
  } catch (e: any) {
    showToast("split failed: " + (e?.message ?? e));
  }
}

function onPickerPick(payload: { sessionId: string; remote: boolean }) {
  const ctx = pickerCtx.value;
  pickerCtx.value = null;
  if (!ctx) return;
  const t = tabs.value.find((tt) => tt.id === ctx.tabId);
  if (!t) return;
  // Reject same-session-twice in the same tab.
  if (t.panes.some((p, i) => i !== ctx.paneIdx && p.sessionId === payload.sessionId)) {
    showToast("that session is already in this tab");
    return;
  }
  t.panes[ctx.paneIdx] = { sessionId: payload.sessionId, remote: payload.remote };
}

function onPickerClose() {
  // Leave the new pane empty; user can fill via split shortcut later.
  pickerCtx.value = null;
}

async function onClosePane() {
  const t = currentTab.value;
  if (!t) return;
  closePaneAt(t, t.activePaneIdx);
}

async function closePaneAt(t: Tab, idx: number) {
  const target = t.panes[idx];
  // Free the underlying local session (remote panes only detach).
  if (target?.sessionId && !target.remote) {
    try { await closeSession(target.sessionId); } catch { /* sweep cleans up */ }
  }
  const r = closePane(t.layout, t.panes, idx);
  t.layout = r.layout;
  t.panes = r.panes;
  t.activePaneIdx = r.activePaneIdx;
  if (r.closeTab) {
    closeTab(t.id);
  }
}

async function closeTab(id: string) {
  const t = tabs.value.find((tt) => tt.id === id);
  if (!t) return;
  // Close every still-live local session inside the tab (concurrently).
  const closures: Promise<void>[] = [];
  for (const p of t.panes) {
    if (p.sessionId && !p.remote) {
      closures.push(closeSession(p.sessionId).catch(() => undefined));
    }
  }
  await Promise.all(closures);
  tabs.value = tabs.value.filter((tt) => tt.id !== id);
  if (currentTabId.value === id) {
    if (tabs.value.length > 0) gotoTab(tabs.value[0].id);
    else location.hash = "";
  }
}

function onFocusPane(dir: "left"|"right"|"up"|"down") {
  const t = currentTab.value;
  if (!t) return;
  const next = focusNeighbor(t.layout, t.activePaneIdx, dir);
  if (next !== null) t.activePaneIdx = next;
}

function onSwitchTab(delta: number) {
  if (tabs.value.length === 0) return;
  const idx = tabs.value.findIndex((t) => t.id === currentTabId.value);
  if (idx === -1) return;
  const next = (idx + delta + tabs.value.length) % tabs.value.length;
  gotoTab(tabs.value[next].id);
}

function openRemoteAsTab(sessionId: string) {
  // Existing UX for "discover panel"; opens the remote session in a fresh tab.
  const id = newId();
  tabs.value.push({
    id,
    layout: "single",
    panes: [{ sessionId, remote: true }],
    activePaneIdx: 0,
  });
  showRemote.value = false;
  gotoTab(id);
}

const tabSummaries = computed(() =>
  tabs.value.map((t) => {
    const active = t.panes[t.activePaneIdx];
    const info = active?.sessionId ? findSessionInfo(active.sessionId, active.remote) ?? null : null;
    return {
      id: t.id,
      layout: t.layout,
      activeSession: info,
      activeRemote: !!active?.remote,
      paneCount: t.panes.length,
    };
  }),
);

const sessionCount = computed(() => allUsedSessionIds.value.size);

useTerminalShortcuts({
  onSplitVertical: (mode) => onSplit("vertical", mode),
  onSplitHorizontal: (mode) => onSplit("horizontal", mode),
  onClosePane,
  onFocusPane,
  onNewTab: startNewTab,
  onSwitchTab,
});

// Keep current tab valid as tabs change.
watch([tabs, currentTabId], () => {
  if (tabs.value.length === 0) return;
  if (currentTabId.value && tabs.value.find((t) => t.id === currentTabId.value)) return;
  gotoTab(tabs.value[0].id);
});

onMounted(async () => {
  syncRoute();
  window.addEventListener("hashchange", syncRoute);
  try {
    localEndpoint.value = await getEndpoint();
    const info = await getHostInfo();
    localHostID.value = info.host_id;
  } catch (e: any) {
    status.value = "error";
    errorMsg.value = e?.message ?? "Wails bindings unavailable";
    return;
  }
  await pollSessions();
  pollHandle = window.setInterval(pollSessions, 2000);

  if (!autoStarted && tabs.value.length === 0) {
    autoStarted = true;
    startNewTab();
  }
});

onUnmounted(() => {
  window.removeEventListener("hashchange", syncRoute);
  if (pollHandle !== null) window.clearInterval(pollHandle);
  if (toastHandle !== null) window.clearTimeout(toastHandle);
});
</script>

<template>
  <div class="app">
    <header class="topbar">
      <div class="brand">atterm</div>
      <div class="status">
        <template v-if="status === 'loading'">starting…</template>
        <template v-else-if="status === 'error'">
          <span class="bad">{{ errorMsg }}</span>
        </template>
        <template v-else>
          {{ sessionCount }} session{{ sessionCount === 1 ? "" : "s" }}
          <span v-if="remoteEndpoint" class="dim"> · uplink on</span>
        </template>
      </div>
      <button
        class="icon-btn"
        :title="remoteEndpoint
          ? `${availableRemote.length} remote session(s) available`
          : 'connect to a relay to see remote sessions'"
        :disabled="!remoteEndpoint"
        @click="showRemote = true"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16" height="16"
          viewBox="0 0 24 24"
          fill="none" stroke="currentColor"
          stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M2 16.1A5 5 0 0 1 5.9 20" />
          <path d="M2 12.05A9 9 0 0 1 9.95 20" />
          <path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6" />
          <line x1="2" y1="20" x2="2.01" y2="20" />
        </svg>
        <span v-if="availableRemote.length > 0" class="badge">{{ availableRemote.length }}</span>
      </button>
      <button
        class="icon-btn"
        title="relay settings"
        @click="showSettings = true"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16" height="16"
          viewBox="0 0 24 24"
          fill="none" stroke="currentColor"
          stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
          <circle cx="12" cy="12" r="3" />
        </svg>
      </button>
    </header>

    <TabBar
      :tabs="tabSummaries"
      :current-id="currentTabId"
      :starting="starting"
      @activate="gotoTab"
      @close="closeTab"
      @new="startNewTab"
    />

    <main class="main">
      <template v-if="localEndpoint">
        <div v-if="tabs.length === 0" class="empty">
          starting first session…
        </div>
        <PaneGrid
          v-for="t in tabs"
          v-show="t.id === currentTabId"
          :key="t.id"
          :tab="t"
          :endpoint-for="endpointFor"
          :active="t.id === currentTabId"
          @set-active-pane="(idx) => (t.activePaneIdx = idx)"
          @close-pane="(idx) => closePaneAt(t, idx)"
        />
      </template>
      <div v-if="toast" class="toast">{{ toast }}</div>
    </main>

    <SettingsDialog
      v-if="showSettings"
      @close="showSettings = false"
    />
    <RemoteSessionsDialog
      v-if="showRemote"
      :sessions="availableRemote"
      @open="openRemoteAsTab"
      @close="showRemote = false"
    />
    <SessionPickerDialog
      v-if="pickerCtx"
      :exclude-session-ids="currentTab ? currentTab.panes.map((p) => p.sessionId).filter((id): id is string => !!id) : []"
      :local-sessions="localList"
      :remote-sessions="remoteList"
      @pick="onPickerPick"
      @close="onPickerClose"
    />
  </div>
</template>

<style scoped>
.app { display: flex; flex-direction: column; height: 100vh; }
.topbar {
  display: flex; align-items: center; gap: 12px; padding: 10px 16px;
  background: var(--panel); border-bottom: 1px solid var(--border); flex: 0 0 auto;
}
.brand { font-weight: 600; letter-spacing: 0.06em; }
.status { margin-left: auto; font-size: 12px; color: var(--fg-dim); }
.status .bad { color: var(--bad); }
.status .dim { color: var(--good); }

.icon-btn {
  position: relative; display: inline-flex; align-items: center; justify-content: center;
  border: none; background: transparent; color: var(--fg-dim); line-height: 1;
  padding: 6px 8px; border-radius: 6px; cursor: pointer;
  transition: color 120ms, background 120ms;
}
.icon-btn svg { display: block; }
.icon-btn:hover:not(:disabled) { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.icon-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.icon-btn .badge {
  position: absolute; top: -2px; right: -2px;
  background: #d29922; color: #0d1117; font-size: 9px; font-weight: 700;
  border-radius: 10px; padding: 1px 5px; line-height: 1.3;
  min-width: 16px; text-align: center;
}

.main { flex: 1 1 auto; position: relative; background: #000; overflow: hidden; }
.empty {
  position: absolute; inset: 0; display: flex; align-items: center;
  justify-content: center; color: var(--fg-dim); font-size: 13px;
}
.toast {
  position: absolute; bottom: 12px; left: 50%; transform: translateX(-50%);
  background: rgba(13, 17, 23, 0.92); border: 1px solid var(--border);
  color: var(--fg); padding: 6px 12px; border-radius: 6px; font-size: 12px;
  pointer-events: none;
}
</style>
```

- [ ] **Step 10.3: Type-check**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm run build
```

Expected: clean. If you see errors about `tab` property assignment in PaneGrid v-for, ensure `tab` is typed as `Tab` and `tabs.value` is `Tab[]`.

- [ ] **Step 10.4: Run unit tests**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
```

Expected: all green (layout, shortcuts).

- [ ] **Step 10.5: Run Go vet (no Go changes yet, but make sure embed assets still match)**

```bash
cd /Users/attson/code/github.com.attson/atterm
go vet -tags webkit2_41 ./...
```

Expected: clean.

- [ ] **Step 10.6: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/components/TabBar.vue
git commit -m "feat(frontend): tab-with-panes model + split shortcuts"
```

---

## Task 11: macOS — strip conflicting menu accelerators

**Files:**
- Modify: `desktop/main.go`

By default Wails v2 installs a Cocoa app menu containing an "App" submenu
(Hide / Quit / etc.), an "Edit" submenu (Cut / Copy / Paste / Select All),
and a "Window" submenu — and the **Window submenu is what binds `⌘W`** and
`⌘M`. Cocoa intercepts those before the webview, so our pane-close shortcut
never reaches the JS handler.

Wails exposes role-based menu helpers (`menu.AppMenu()`, `menu.EditMenu()`,
`menu.WindowMenu()`) that map to native Cocoa roles. Installing only
`AppMenu` and `EditMenu` (no `WindowMenu`) drops the `⌘W`/`⌘M` accelerators
while keeping native Quit, Hide, and the standard editing shortcuts that
`xterm.js` selection requires on macOS.

- [ ] **Step 11.1: Update `desktop/main.go`**

Replace `desktop/main.go` with:

```go
package main

import (
	"embed"
	stdruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	opts := &options.App{
		Title:  "atterm",
		Width:  1100,
		Height: 720,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 13, G: 17, B: 23, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	}

	// macOS only: install a custom menu that keeps native App + Edit
	// submenus (Hide / Quit / Cut / Copy / Paste / Select All) but omits
	// the Window submenu. The Window submenu is where Cocoa would bind
	// ⌘W / ⌘M, and we need ⌘W for "close pane" and don't want to claim
	// ⌘M either. Linux and Windows webviews don't have this problem.
	if stdruntime.GOOS == "darwin" {
		opts.Menu = darwinMenu()
	}

	if err := wails.Run(opts); err != nil {
		println("Error:", err.Error())
	}
}

func darwinMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())  // About / Hide / Quit (native roles)
	m.Append(menu.EditMenu()) // Cut / Copy / Paste / Select All (required for macOS webview text selection)
	return m
}
```

> The `keys` import is gone — role-based menus don't need explicit
> accelerators. If a future Wails release changes role semantics, fall back
> to a manual `appMenu.AddText` per item with `keys.CmdOrCtrl("q")` and
> `wruntime.Quit(app.ctx)`.

- [ ] **Step 11.2: `go vet` and full build**

```bash
cd /Users/attson/code/github.com.attson/atterm
go vet -tags webkit2_41 ./...
```

Expected: clean.

- [ ] **Step 11.3: Commit**

```bash
git add desktop/main.go
git commit -m "fix(desktop): strip macOS default ⌘W menu accelerator"
```

---

## Task 12: Final verification

- [ ] **Step 12.1: Full unit suite**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npm test
npm run build
```

Expected: tests green; build clean.

- [ ] **Step 12.2: Go vet + existing protocol e2e**

```bash
cd /Users/attson/code/github.com.attson/atterm
export PATH=$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./desktop/
```

Expected: vet clean; existing `desktop/uplink_e2e_test.go` still passes.

- [ ] **Step 12.3: Manual smoke (cannot be automated)**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop
wails dev -tags webkit2_41   # Linux; drop -tags on macOS / Windows
```

Step through each item in §"Manual verification" of the spec
(`docs/superpowers/specs/2026-05-10-pane-split-layouts-design.md`) and
confirm. If anything diverges, file the discrepancy as a follow-up task and
do **not** mark this task complete on a verbal "looks fine".

In particular, on macOS: confirm `⌘W` reaches the JS handler (closes a pane,
not the window) and `⌘Q` still quits. On Linux/Windows: confirm `Ctrl+W`
closes a pane and the shell's `Ctrl+D` (EOF) still works inside a focused
terminal.

- [ ] **Step 12.4: Update README's "current capabilities" section**

Open `README.md`. Find the line:

```
- ✅ Wails 桌面 app（Linux / macOS / Windows）：多 tab、本地 PTY、cwd 跟踪、远程 cast 面板
```

Replace with:

```
- ✅ Wails 桌面 app（Linux / macOS / Windows）：多 tab、每 tab 1/2/4 pane 分屏（⌘D / ⌘⇧D，Linux/Windows 用 Ctrl）、本地 PTY、cwd 跟踪、远程 cast 面板
```

- [ ] **Step 12.5: Commit**

```bash
git add README.md
git commit -m "docs: README mentions pane-split layouts"
```

- [ ] **Step 12.6: Final summary check**

```bash
git log --oneline main^..HEAD
git status
```

Expected: a series of focused commits, working tree clean. No
follow-up TODOs lingering in the diff.

---

## Self-review notes

- **Spec coverage**: Each spec section has a task — types (Task 2), pure
  layout (Tasks 3–5), shortcuts composable (Task 6), TerminalView focus prop
  (Task 7), PaneGrid (Task 8), SessionPicker (Task 9), App+TabBar refactor
  (Task 10), macOS menu (Task 11). cwd inheritance is in Task 10 step 10.2
  (`onSplit`). Toast on `noop` is in `onSplit`. Pane sweep on poll is in
  `pollSessions`. Hash route migration `#/s/` → `#/t/` is in Task 10. Same-
  session dedupe within a tab is checked in `onPickerPick`.
- **No placeholders**: every step contains the actual code or command. No
  "TBD"/"TODO"/"add error handling" prose. Each commit step has the exact
  message.
- **Type consistency**: `Tab`, `Pane`, `LayoutKind`, `SplitDir`, `FocusDir`
  are defined once in `lib/types.ts` (Task 2), re-exported from
  `lib/layout.ts` (Task 3). `TransitionResult` and `CloseResult` are defined
  next to their producers in `lib/layout.ts`. `ShortcutHandlers` is owned
  by `useTerminalShortcuts.ts` (Task 6); `SplitMode` is exported and
  imported into `App.vue` for `onSplit`. `TabSummary` is local to
  `TabBar.vue` (Task 10.1).
