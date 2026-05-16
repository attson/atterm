export interface Tab {
  path: string;
  persistent: boolean;
  // Activation order ts, larger = more recent. Used for LRU eviction.
  lastActiveAt: number;
}

export interface TabsState {
  tabs: Tab[];
  activeIdx: number;
}

const MAX_TABS = 8;

export type OpenKind = "preview" | "persistent";

export function openPath(state: TabsState, path: string, kind: OpenKind): TabsState {
  const now = Date.now();
  const existingIdx = state.tabs.findIndex((t) => t.path === path);
  if (existingIdx >= 0) {
    const next = clone(state);
    next.tabs[existingIdx].lastActiveAt = now;
    if (kind === "persistent") next.tabs[existingIdx].persistent = true;
    next.activeIdx = existingIdx;
    return next;
  }

  if (kind === "preview") {
    // Replace the existing preview tab if any.
    const previewIdx = state.tabs.findIndex((t) => !t.persistent);
    if (previewIdx >= 0) {
      const next = clone(state);
      next.tabs[previewIdx] = { path, persistent: false, lastActiveAt: now };
      next.activeIdx = previewIdx;
      return next;
    }
  }

  // Append; may need eviction.
  let next = clone(state);
  next.tabs.push({ path, persistent: kind === "persistent", lastActiveAt: now });
  next.activeIdx = next.tabs.length - 1;
  if (next.tabs.length > MAX_TABS) {
    next = evictOldest(next);
  }
  return next;
}

export function closeTab(state: TabsState, idx: number): TabsState {
  if (idx < 0 || idx >= state.tabs.length) return state;
  const next = clone(state);
  next.tabs.splice(idx, 1);
  if (next.tabs.length === 0) {
    next.activeIdx = -1;
    return next;
  }
  next.activeIdx = Math.min(idx, next.tabs.length - 1);
  return next;
}

function evictOldest(state: TabsState): TabsState {
  // Prefer evicting non-persistent (but not the active tab). If none, fall back to oldest overall.
  const activePath = state.activeIdx >= 0 ? state.tabs[state.activeIdx].path : null;
  const candidatePred = (t: Tab) => !t.persistent && t.path !== activePath;
  const candidates = state.tabs.filter(candidatePred);
  const pool = candidates.length > 0 ? candidates : state.tabs;
  let oldest = pool[0];
  for (const t of pool) {
    if (t.lastActiveAt < oldest.lastActiveAt) oldest = t;
  }
  const evictIdx = state.tabs.indexOf(oldest);

  // Close the tab but preserve activeIdx if we're evicting a non-active tab.
  const next = clone(state);
  next.tabs.splice(evictIdx, 1);

  // Adjust activeIdx based on what we evicted.
  if (evictIdx < state.activeIdx) {
    // We evicted before the active tab, so it shifted left by 1.
    next.activeIdx = state.activeIdx - 1;
  } else if (evictIdx === state.activeIdx) {
    // We evicted the active tab; refocus on neighbor.
    next.activeIdx = Math.min(evictIdx, next.tabs.length - 1);
  } else {
    // We evicted after the active tab; activeIdx stays the same.
    next.activeIdx = state.activeIdx;
  }

  if (next.tabs.length === 0) {
    next.activeIdx = -1;
  }

  return next;
}

function clone(s: TabsState): TabsState {
  return { tabs: s.tabs.map((t) => ({ ...t })), activeIdx: s.activeIdx };
}
