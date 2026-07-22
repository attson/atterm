import type { SessionInfo } from "./connection";
import type { Tab } from "./types";

export interface PruneStaleRemoteTabsInput {
  tabs: Tab[];
  remoteSessions: SessionInfo[];
  missingSince: Map<string, number>;
  nowMs: number;
  graceMs: number;
}

export interface PruneStaleRemoteTabsResult {
  tabs: Tab[];
  removedTabIds: string[];
}

function singleRemoteSessionId(tab: Tab): string | null {
  if (tab.layout !== "single" || tab.panes.length !== 1) return null;
  const pane = tab.panes[0];
  if (!pane?.remote || !pane.sessionId) return null;
  return pane.sessionId;
}

export function pruneStaleRemoteTabs(input: PruneStaleRemoteTabsInput): PruneStaleRemoteTabsResult {
  const presentRemoteIds = new Set(input.remoteSessions.map((s) => s.id));
  for (const id of presentRemoteIds) input.missingSince.delete(id);

  const candidateIds = new Set<string>();
  for (const tab of input.tabs) {
    const sessionId = singleRemoteSessionId(tab);
    if (sessionId) candidateIds.add(sessionId);
  }
  for (const id of input.missingSince.keys()) {
    if (!candidateIds.has(id)) input.missingSince.delete(id);
  }

  const removedTabIds: string[] = [];
  const nextTabs: Tab[] = [];

  for (const tab of input.tabs) {
    const sessionId = singleRemoteSessionId(tab);
    if (!sessionId) {
      nextTabs.push(tab);
      continue;
    }
    if (presentRemoteIds.has(sessionId)) {
      nextTabs.push(tab);
      continue;
    }

    const since = input.missingSince.get(sessionId) ?? input.nowMs;
    input.missingSince.set(sessionId, since);
    if (input.nowMs - since >= input.graceMs) {
      input.missingSince.delete(sessionId);
      removedTabIds.push(tab.id);
      continue;
    }
    nextTabs.push(tab);
  }

  return { tabs: nextTabs, removedTabIds };
}
