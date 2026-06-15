// Shared types for the per-tab pane-split layout model. See
// docs/superpowers/specs/2026-06-15-multi-pane-resize-design.md.

import type { SessionInfo } from "./connection";

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
  // lastSeenInfo carries the most recent SessionInfo we saw for this pane
  // before sweepMissingSessions nulled sessionId. Used so the TabBar can
  // still show a meaningful title ("C:\\Users\\xianj — disconnected")
  // instead of "(空)" when a remote host briefly drops, and so the user
  // can tell which tab was which.
  lastSeenInfo?: SessionInfo;
}

export interface Tab {
  id: string;            // frontend-generated uuid; Vue key only
  layout: LayoutKind;
  panes: Pane[];         // length matches layout: 1 / 2 / 2 / 4
  activePaneIdx: number; // index in panes[] of the keyboard-focused pane
  // 0.1..0.9, left column share for vertical/grid2x2. Always present so
  // callers don't branch; single/horizontal layouts ignore it.
  colRatio: number;
  // 0.1..0.9, top row share for horizontal/grid2x2. Same caveat as colRatio.
  rowRatio: number;
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
