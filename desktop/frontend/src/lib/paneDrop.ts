import type { Pane } from "./types";

/**
 * Drag payload type for "a session row was dragged onto a pane".
 *
 * Deliberately NOT text/plain: a pane is an xterm.js instance whose hidden
 * textarea accepts native text drops, so a text/plain payload that slipped past
 * our handler would be typed into the terminal. A private MIME type is inert
 * everywhere except the one handler that looks for it.
 */
export const SESSION_DND_MIME = "application/x-atterm-session";

/**
 * True when a drag carries a session of ours. Panes must ignore everything
 * else — files dragged from the OS, selected text — rather than swallow drops
 * they have no meaning for.
 */
export function carriesSessionDrag(types: readonly string[] | undefined): boolean {
  return !!types && Array.prototype.includes.call(types, SESSION_DND_MIME);
}

/**
 * Session id of the drag in flight, or "" when none.
 *
 * The dataTransfer payload alone is not dependable: WebKit — the engine behind
 * the desktop app's webview — keeps `types` readable (so the drop target still
 * lights up) while returning nothing from getData for a custom MIME type, which
 * made every drop a silent no-op. A drag never leaves this document, so a
 * module-level copy set at dragstart is the authoritative payload and
 * dataTransfer is left to do only what it does reliably: announce the type.
 */
let dragging = "";

export function setDraggingSession(sessionId: string): void {
  dragging = sessionId;
}

export function draggingSession(): string {
  return dragging;
}

/** Called on dragend so a later, unrelated drop cannot place a stale session. */
export function clearDraggingSession(): void {
  dragging = "";
}

/** The subset of Tab this module reads. */
export interface DropTabLike {
  id: string;
  panes: readonly Pane[];
}

export interface DroppedSession {
  /**
   * The pane to place. When the session is already on screen this is a copy of
   * its current pane rather than a fresh one: `remote` decides which endpoint
   * the pane attaches to and which list resolves its title, so re-minting a
   * locally-spawned shell as remote would leave it resolvable in neither and
   * the pane would render empty.
   */
  pane: Pane;
  /**
   * Where the session is showing right now, if anywhere. The caller detaches it
   * there before placing — a session may occupy exactly one pane, the same
   * invariant `openRemoteAsTab` keeps by refusing to duplicate.
   */
  from?: { tabId: string; paneIdx: number };
}

/**
 * Resolves a dragged session id into the pane to place and the pane to vacate.
 * Returns null for an empty id.
 */
export function resolveDroppedSession(
  tabs: readonly DropTabLike[],
  sessionId: string,
): DroppedSession | null {
  if (!sessionId) return null;
  for (const t of tabs) {
    const idx = t.panes.findIndex((p) => p.sessionId === sessionId);
    if (idx !== -1) {
      return { pane: { ...t.panes[idx] }, from: { tabId: t.id, paneIdx: idx } };
    }
  }
  // Not on screen: it comes straight from the sidebar, which reaches sessions
  // through the relay endpoint — the convention openRemoteAsTab uses.
  return { pane: { sessionId, remote: true } };
}

export interface DetachPlan {
  tabId: string;
  paneIdx: number;
  /** The pane as it stands, so the new tab keeps its endpoint and title source. */
  pane: Pane;
}

/**
 * Works out how to pull a session out of a split and into a tab of its own.
 *
 * Returns null when there is nothing to do — including the case that reads as a
 * bug if you let it through: a session that is the only pane in its tab is
 * already independent, and "detaching" it would close that tab and immediately
 * open another, losing its place in the strip for no visible gain.
 */
export function planDetachToTab(
  tabs: readonly DropTabLike[],
  sessionId: string,
): DetachPlan | null {
  if (!sessionId) return null;
  for (const t of tabs) {
    const idx = t.panes.findIndex((p) => p.sessionId === sessionId);
    if (idx === -1) continue;
    if (t.panes.length < 2) return null;
    return { tabId: t.id, paneIdx: idx, pane: { ...t.panes[idx] } };
  }
  return null;
}

/**
 * Trades the contents of two panes in one tab, returning a new array.
 *
 * Dragging a pane's grip onto a sibling means "these two should change places"
 * — the split itself is not being rebuilt, so both panes keep their own
 * `remote` flag and cached info and simply move.
 */
export function swapPanes(panes: readonly Pane[], a: number, b: number): Pane[] {
  const next = panes.map((p) => ({ ...p }));
  if (a === b) return next;
  if (a < 0 || b < 0 || a >= next.length || b >= next.length) return next;
  const tmp = next[a];
  next[a] = next[b];
  next[b] = tmp;
  return next;
}
