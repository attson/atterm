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
  const layout: LayoutKind = sameCol ? "horizontal" : "vertical";
  return {
    layout,
    panes: [{ ...panes[a] }, { ...panes[b] }],
    activePaneIdx: 0,
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

// Re-export so consumers only import from layout.ts.
export type { Pane, Tab, LayoutKind, SplitDir, FocusDir };
export { PANE_COUNT, EMPTY_PANE };
