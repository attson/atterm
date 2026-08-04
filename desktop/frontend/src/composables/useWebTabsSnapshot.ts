import { onMounted, onUnmounted, watch, type ComputedRef, type Ref } from "vue";

import { RATIO_DEFAULT } from "../lib/layout";
import { synthSessionInfoFromSnapshot } from "../lib/recoveryRestore";
import { PANE_COUNT, type LayoutKind, type Tab } from "../lib/types";
import {
  formatHash,
  loadSnapshot,
  parseHashSid,
  saveSnapshot,
  type WebTabsSnapshot,
} from "../lib/webTabsSnapshot";

/**
 * useWebTabsSnapshot bundles the web-build-only workspace-persistence path:
 *
 *  - Reflects the active pane's session id as `#/session/<sid>` in the URL
 *    so the address bar is bookmarkable/shareable.
 *  - Debounces a `saveSnapshot()` on every tabs mutation so a reload / crash
 *    rebuilds the same tab & pane layout on next boot.
 *  - Handles `hashchange` deep-links (`#/session/<sid>` pasted into an
 *    already-open tab) by asking the caller to open that sid as a new tab.
 *  - Provides `tryRestoreOnBoot()` — called from the caller's onMounted —
 *    which restores from localStorage or a deep-link hash and returns true
 *    if it handled either.
 *
 * `enabled: false` (desktop / Capacitor via `caps.wailsBindings`) disables
 * every watcher and DOM listener: the hash-sync watcher would clobber
 * desktop's `#/t/<tabId>` scheme, the saveSnapshot debounce would burn a
 * timer for nothing (loadSnapshot always returns null on desktop per its
 * module comment), and the hashchange listener would double up on
 * App.vue's `syncRoute`. `tryRestoreOnBoot()` still runs — its two calls
 * (loadSnapshot + parseHashSid) are safe no-ops on desktop and preserve
 * the original guard structure.
 */
export interface UseWebTabsSnapshotOpts {
  /** Wire the watchers/listeners? False on desktop/Capacitor. */
  enabled: boolean;
  tabs: Ref<Tab[]>;
  currentTabId: Ref<string | null>;
  currentTab: ComputedRef<Tab | null>;
  gotoTab: (id: string) => void;
  openRemoteAsTab: (sid: string) => void;
  newId: () => string;
}

export interface UseWebTabsSnapshot {
  /** Restore tabs from the localStorage snapshot or a `#/session/<sid>`
   *  deep link. Returns true if either was handled — the caller should
   *  then skip its normal recovery / auto-start fallback. */
  tryRestoreOnBoot(): boolean;
}

export function useWebTabsSnapshot(opts: UseWebTabsSnapshotOpts): UseWebTabsSnapshot {
  const { enabled, tabs, currentTabId, currentTab, gotoTab, openRemoteAsTab, newId } = opts;

  let snapshotSaveHandle: ReturnType<typeof setTimeout> | null = null;

  function scheduleSnapshotSave() {
    if (snapshotSaveHandle) clearTimeout(snapshotSaveHandle);
    snapshotSaveHandle = setTimeout(() => {
      snapshotSaveHandle = null;
      saveSnapshot({
        tabs: tabs.value.map((t) => ({
          id: t.id,
          layout: t.layout,
          active_pane_idx: t.activePaneIdx,
          col_ratio: t.colRatio,
          row_ratio: t.rowRatio,
          panes: t.panes
            .map((p, slot) => ({
              slot,
              session_id: p.sessionId ?? "",
              host_id: p.lastSeenInfo?.host_id,
              sealed: p.lastSeenInfo?.sealed,
            }))
            .filter((p) => p.session_id),
        })),
        active_tab_id: currentTabId.value ?? "",
      });
    }, 300);
  }

  function onHashChange() {
    const { sid } = parseHashSid(location.hash);
    if (!sid) return;
    openRemoteAsTab(sid);
  }

  if (enabled) {
    // Uses history.replaceState (never `location.hash =`) so it never adds a
    // history entry and never fires a `hashchange` event back at onHashChange.
    watch(currentTab, (t) => {
      const activePane = t?.panes[t.activePaneIdx];
      const sid = activePane?.sessionId ?? "";
      const target = sid ? formatHash(sid) : "#/";
      if (location.hash !== target) history.replaceState({}, "", target);
    });

    watch(tabs, () => { scheduleSnapshotSave(); }, { deep: true });

    onMounted(() => {
      window.addEventListener("hashchange", onHashChange);
    });
    onUnmounted(() => {
      window.removeEventListener("hashchange", onHashChange);
      if (snapshotSaveHandle !== null) window.clearTimeout(snapshotSaveHandle);
    });
  }

  // Rebuilds tabs from the web-only localStorage snapshot at boot. Every pane
  // restores as remote (the web build has no local PTY — see caps.localPty
  // gating in App.vue), mirroring executeRestore's remote-pane branch:
  // re-bind sessionId + synth lastSeenInfo, no spawn.
  function restoreFromWebSnapshot(snap: WebTabsSnapshot) {
    const validLayouts: LayoutKind[] = ["single", "vertical", "horizontal", "grid2x2"];
    const newIds: string[] = [];
    let activeIdx = -1;
    for (const snapTab of snap.tabs) {
      const layout: LayoutKind = validLayouts.includes(snapTab.layout as LayoutKind)
        ? (snapTab.layout as LayoutKind)
        : "single";
      const t: Tab = {
        id: newId(),
        layout,
        activePaneIdx: snapTab.active_pane_idx,
        colRatio: snapTab.col_ratio ?? RATIO_DEFAULT,
        rowRatio: snapTab.row_ratio ?? RATIO_DEFAULT,
        panes: [],
      };
      const want = PANE_COUNT[layout];
      for (let i = 0; i < want; i++) {
        const p = snapTab.panes.find((pp) => pp.slot === i);
        if (!p || !p.session_id) {
          t.panes[i] = { sessionId: null, remote: false };
          continue;
        }
        t.panes[i] = {
          sessionId: p.session_id,
          remote: true,
          lastSeenInfo: synthSessionInfoFromSnapshot({
            slot: p.slot,
            remote: true,
            host_id: p.host_id,
            session_id: p.session_id,
            shell: "",
          }),
        };
      }
      if (snapTab.id === snap.active_tab_id) activeIdx = newIds.length;
      tabs.value.push(t);
      newIds.push(t.id);
    }
    if (activeIdx >= 0 && newIds[activeIdx]) gotoTab(newIds[activeIdx]);
    else if (newIds.length > 0) gotoTab(newIds[0]);
  }

  function tryRestoreOnBoot(): boolean {
    // loadSnapshot() is always null on desktop/capacitor, so this branch is
    // a no-op there and falls straight through to the deep-link check.
    const webSnap = loadSnapshot();
    if (webSnap && webSnap.tabs.length > 0) {
      restoreFromWebSnapshot(webSnap);
      return true;
    }
    // Deep link: `#/session/<sid>` opens that one remote session as a new
    // tab instead of falling through to recovery/auto-start.
    const { sid: hashSid } = parseHashSid(location.hash);
    if (hashSid) {
      openRemoteAsTab(hashSid);
      return true;
    }
    return false;
  }

  return { tryRestoreOnBoot };
}
