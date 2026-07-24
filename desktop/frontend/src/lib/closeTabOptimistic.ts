import type { Tab } from "./types";
import type { SessionInfo } from "./connection";

export interface CloseTabPlan {
  /** Local session IDs that should be killed via CloseSession RPC. */
  localIdsToClose: string[];
  /** tabs.value replacement — the target tab removed. */
  nextTabs: Tab[];
  /** localList.value replacement — the closed local sessions optimistically dropped. */
  nextLocalList: SessionInfo[];
  /** True iff the removed tab was the current tab; caller should reroute. */
  wasCurrent: boolean;
}

/**
 * Compute the optimistic UI state for closing a tab: remove the tab from tabs,
 * drop its local sessions from localList right away, and hand back the list
 * of local session ids the caller should still hit via CloseSession RPC.
 *
 * The tab and its local sessions vanish immediately regardless of RPC
 * latency; if the RPC ever fails, a follow-up LIST_RESP will resurface the
 * session, which is fine — better than blocking the UI on IPC roundtrips.
 *
 * detachOnly skips the RPC list: mergeSelectedIntoTab moves a session to
 * another tab and does NOT want its shell killed.
 */
export function computeCloseTabState(
  tabs: Tab[],
  localList: SessionInfo[],
  currentTabId: string | null,
  id: string,
  opts?: { detachOnly?: boolean },
): CloseTabPlan | null {
  const t = tabs.find((tt) => tt.id === id);
  if (!t) return null;
  const localIds: string[] = [];
  for (const p of t.panes) {
    if (p.sessionId && !p.remote) {
      localIds.push(p.sessionId);
    }
  }
  const localIdsToClose = opts?.detachOnly ? [] : localIds.slice();
  const droppedSet = new Set(localIdsToClose);
  return {
    localIdsToClose,
    nextTabs: tabs.filter((tt) => tt.id !== id),
    nextLocalList: droppedSet.size === 0
      ? localList
      : localList.filter((s) => !droppedSet.has(s.id)),
    wasCurrent: currentTabId === id,
  };
}
