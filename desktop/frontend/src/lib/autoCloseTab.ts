import type { Tab } from "./types";

// tabsToAutoCloseOnExit decides which tabs should be auto-closed after a
// session-sweep. A terminal (local shell or adopted SSH session — both
// remote:false) that exits has its pane nulled by sweepMissingSessions. If
// that leaves a tab with no live session in ANY pane, the tab should close
// instead of stranding an "[empty pane]" placeholder.
//
// Only tabs that just lost a session THIS sweep (clearedTabIds) are eligible,
// so freshly-opened empty tabs and mid-restore tabs (whose sessions haven't
// been confirmed by Go yet) are never swept away.
//
// A pane counts as holding a live session when it has a sessionId AND either
// it is remote (managed by the remote-list path, still considered live here)
// or its local session id is still present in localIds.
export function tabsToAutoCloseOnExit(
  tabs: Tab[],
  localIds: Set<string>,
  clearedTabIds: string[],
): string[] {
  const cleared = new Set(clearedTabIds);
  const out: string[] = [];
  for (const t of tabs) {
    if (!cleared.has(t.id)) continue;
    const hasLiveSession = t.panes.some(
      (p) => p.sessionId && (p.remote || localIds.has(p.sessionId)),
    );
    if (!hasLiveSession) out.push(t.id);
  }
  return out;
}
