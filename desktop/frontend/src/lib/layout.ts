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
