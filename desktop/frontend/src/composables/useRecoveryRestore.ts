import { ref, type Ref } from "vue";

import type { SessionInfo } from "../lib/connection";
import {
  discardRecoverySnapshot,
  listShells,
  newSession,
  newSshSessionByID,
  type RecoverySnapshot,
  type RecoveryTabSnapshot,
} from "../lib/api";
import {
  buildRestoreSessionReq,
  synthSessionInfoFromSnapshot,
} from "../lib/recoveryRestore";
import { classifySSHRestore } from "../lib/sshRestore";
import { PANE_COUNT, type LayoutKind, type Pane, type Tab } from "../lib/types";
import type { UseSessionPins } from "./useSessionPins";
import { errText, logWarn } from "../lib/log";

const RESTORE_SPAWN_CONCURRENCY = 4;

/**
 * useRecoveryRestore owns the recovery-dialog state + the executeRestore
 * pipeline. The dialog opens at boot when a non-empty snapshot exists AND
 * the "prompt on start" toggle is on; the user then picks a subset of
 * tabs to restore (Restore) or clears the snapshot (Discard).
 *
 * executeRestore is the load-bearing part: for each pane it fans out to
 * one of four spawn paths — remote re-bind (no fork), SSH reconnect
 * by-host-id, ad-hoc SSH placeholder, or a normal local fork via
 * buildRestoreSessionReq — with a pin-id migration hook so pinned local
 * sessions survive the sid change on respawn. The pipeline is serial per
 * pane on purpose: failure placement (empty pane) has to land in the
 * exact slot, and mutations against localList / pins must not race.
 */
export interface UseRecoveryRestoreOpts {
  // Reactive state owned by App.vue.
  tabs: Ref<Tab[]>;
  localList: Ref<SessionInfo[]>;
  localHostID: Ref<string>;

  /** Set of local session ids seeded into localList but not yet confirmed
   *  by a LIST_RESP push. Restore mirrors spawnLocalShell's seeding path. */
  pendingLocalIds: Set<string>;

  /** Setup-scope useSessionPins instance so the composable can await its
   *  initial load, migrate pin ids across the restart-sid boundary, and
   *  force-flush the migrated set before we return control to the caller. */
  pins: UseSessionPins;

  /** Web-build gate: skip local-fork branches when there is no local PTY. */
  hasLocalPty: boolean;

  /** Temporarily suppress recovery.json auto-save while multi-pane restore is
   *  mutating tabs/localList, then flush once at the end. */
  pauseRecoverySnapshot?: () => (() => void) | void;

  // Callables into App.vue's tab/session model.
  newId: () => string;
  gotoTab: (id: string) => void;
  startNewTab: () => Promise<void> | void;
  predictCellDims: (layout: LayoutKind) => { cols: number; rows: number };
}

export interface UseRecoveryRestore {
  /** Bound to <RecoveryDialog>. `snapshot: null` closes the dialog. */
  recoveryDialogState: Ref<{ open: boolean; snapshot: RecoverySnapshot | null }>;
  /** RecoveryDialog @restore handler. Empty picks → open a fresh tab. */
  onRecoveryRestore: (picks: RecoveryTabSnapshot[]) => Promise<void>;
  /** RecoveryDialog @discard handler. Clears saved snapshot + opens a fresh tab. */
  onRecoveryDiscard: () => Promise<void>;
}

export function useRecoveryRestore(opts: UseRecoveryRestoreOpts): UseRecoveryRestore {
  const {
    tabs,
    localList,
    localHostID,
    pendingLocalIds,
    pins,
    hasLocalPty,
    newId,
    gotoTab,
    startNewTab,
    predictCellDims,
    pauseRecoverySnapshot,
  } = opts;

  const recoveryDialogState = ref<{ open: boolean; snapshot: RecoverySnapshot | null }>({
    open: false,
    snapshot: null,
  });

  async function onRecoveryRestore(picks: RecoveryTabSnapshot[]) {
    const savedActive = recoveryDialogState.value.snapshot?.active_tab_id ?? "";
    recoveryDialogState.value = { open: false, snapshot: null };
    if (picks.length === 0) {
      if (hasLocalPty) await startNewTab();
      return;
    }
    const resumeRecoverySnapshot = pauseRecoverySnapshot?.();
    try {
      await executeRestore(picks, savedActive);
    } finally {
      resumeRecoverySnapshot?.();
    }
  }

  async function onRecoveryDiscard() {
    recoveryDialogState.value = { open: false, snapshot: null };
    try {
      await discardRecoverySnapshot();
    } catch (e) {
      logWarn("recovery", "discard failed", { error: errText(e) });
    }
    if (hasLocalPty) await startNewTab();
  }

  type RestorePaneJob = {
    pickIdx: number;
    slot: number;
    tab: RecoveryTabSnapshot;
  };
  type RestorePaneResult = {
    pickIdx: number;
    slot: number;
    pane: Pane;
    seed?: SessionInfo;
    pinRename?: { oldSid: string; newSid: string };
  };

  async function runRestoreJobs(
    jobs: RestorePaneJob[],
    fn: (job: RestorePaneJob) => Promise<RestorePaneResult>,
  ): Promise<RestorePaneResult[]> {
    const results = new Array<RestorePaneResult>(jobs.length);
    let next = 0;
    const workers = Array.from({ length: Math.min(RESTORE_SPAWN_CONCURRENCY, jobs.length) }, async () => {
      while (next < jobs.length) {
        const idx = next++;
        results[idx] = await fn(jobs[idx]);
      }
    });
    await Promise.all(workers);
    return results;
  }

  // executeRestore rebuilds tabs/panes from a snapshot. Pane startup is bounded
  // parallel, but UI state is committed once at the end. That avoids two kinds
  // of boot jank: N shell startups no longer stack serially, and the main view
  // does not mount/attach/replay partially restored hidden tabs while restore is
  // still running.
  async function executeRestore(picks: RecoveryTabSnapshot[], savedActiveTabId: string) {
    const newIds = picks.map(() => newId());
    const restoredTabs: Tab[] = picks.map((tab, idx) => ({
      id: newIds[idx],
      layout: tab.layout,
      activePaneIdx: tab.active_pane_idx,
      colRatio: tab.col_ratio,
      rowRatio: tab.row_ratio,
      panes: [],
    }));
    // Pin migration: local panes get a fresh session_id on respawn, which
    // would strand the previous generation's pin ids in config. Capture the
    // old sid from the snapshot and hand it to pins.rename after each spawn.
    // Remote panes keep their sid across restarts, so remote entries in the
    // pin set are already correct — no rename needed on that branch.
    // See 2026-07-23-pinned-session-recovery-design.md §4.3.
    // pins is the caller's setup-scope instance; wait for its initial load
    // here so isPinned(oldSid) below can't race the fire-and-forget
    // getPinnedSessionIds() kicked off elsewhere.
    await pins.ready();
    let savedActiveIdx = -1;
    // Resolve the user's real default shell once. Panes whose snapshot has an
    // empty shell restore against this instead of /bin/sh — see
    // buildRestoreSessionReq.
    const defaultShell = (await listShells())[0] ?? "";
    const jobs: RestorePaneJob[] = [];
    for (let pickIdx = 0; pickIdx < picks.length; pickIdx++) {
      const tab = picks[pickIdx];
      if (tab.id === savedActiveTabId) savedActiveIdx = pickIdx;
      const want = PANE_COUNT[tab.layout];
      for (let i = 0; i < want; i++) {
        jobs.push({ pickIdx, slot: i, tab });
      }
    }

    const results = await runRestoreJobs(jobs, async (job): Promise<RestorePaneResult> => {
      const { pickIdx, slot: i, tab } = job;
      const snap = tab.panes.find((p) => p.slot === i);
      if (!snap) {
        return { pickIdx, slot: i, pane: { sessionId: null, remote: false } };
      }
      // Remote panes: do NOT fork a new local shell. The session is still
      // alive on the remote host (or will be when the relay reconnects);
      // re-bind the pane to the same session_id and let the remote list
      // push resolve SessionInfo. Until the relay catches up — or if the
      // session is gone — lastSeenInfo keeps the tab label meaningful
      // (matches the local-sweep "disconnected" display).
      if (snap.remote && snap.session_id) {
        return {
          pickIdx,
          slot: i,
          pane: {
            sessionId: snap.session_id,
            remote: true,
            lastSeenInfo: synthSessionInfoFromSnapshot(snap),
          },
        };
      }
      // Platforms without a local PTY (e.g. the web build) can't fork a
      // local shell at all — leave the pane empty instead of calling
      // newSession, matching how a snapshot-less boot renders there.
      if (!hasLocalPty) {
        return { pickIdx, slot: i, pane: { sessionId: null, remote: false } };
      }
      // SSH sessions must NOT be forked as a local shell (that would spawn a
      // bogus "ssh" process). Saved-host SSH reconnects by host id; ad-hoc SSH
      // can't reconnect (used-once creds) so it's left empty with a hint.
      const sshKind = classifySSHRestore(snap);
      if (sshKind.kind === "reconnect") {
        try {
          const resp = await newSshSessionByID(sshKind.hostId);
          const dims = predictCellDims(tab.layout);
          return {
            pickIdx,
            slot: i,
            pane: { sessionId: resp.session_id, remote: false },
            seed: {
              id: resp.session_id,
              command: snap.title || "ssh",
              cwd: "",
              title: snap.title || "ssh",
              type: "shell",
              cols: dims.cols,
              rows: dims.rows,
              started_at: Math.floor(Date.now() / 1000),
              host_id: localHostID.value,
            },
          };
        } catch (e) {
          // Host deleted / key missing / TOFU on reconnect → leave empty with a
          // hint instead of stranding a broken pane.
          return {
            pickIdx,
            slot: i,
            pane: {
              sessionId: null,
              remote: false,
              lastSeenInfo: synthSessionInfoFromSnapshot({
                ...snap,
                title: (snap.title || "ssh") + " — reconnect failed, open it again",
              }),
            },
          };
        }
      }
      if (sshKind.kind === "adhoc") {
        // Ad-hoc SSH: credentials were used-once; cannot reconnect.
        return {
          pickIdx,
          slot: i,
          pane: {
            sessionId: null,
            remote: false,
            lastSeenInfo: synthSessionInfoFromSnapshot({
              ...snap,
              title: (snap.title || "ssh") + " — SSH disconnected, reconnect to resume",
            }),
          },
        };
      }
      try {
        // oldSid: previous generation's session_id, saved by useRecoverySnapshot
        // for local panes (Task 2). Empty when the snapshot pre-dates that
        // change — skip pin migration in that case, matches old behavior.
        const oldSid = snap.session_id || "";
        const dims = predictCellDims(tab.layout);
        const req = buildRestoreSessionReq(snap, dims.cols, dims.rows, defaultShell);
        const resp = await newSession(req);
        // Seed localList immediately (mirrors spawnLocalShell) so the recovery
        // snapshot can resolve this pane's SessionInfo right away. Without this,
        // the window before the relay's session-list push arrives would persist
        // shell:"" / cwd:"" — corrupting recovery.json and making the NEXT
        // restore fall back to /bin/sh (sh-3.2$). pendingLocalIds protects the
        // seed from being dropped by the FIRST (still-stale) LIST_RESP frame
        // that arrives after a multi-spawn restore.
        return {
          pickIdx,
          slot: i,
          pane: { sessionId: resp.session_id, remote: false },
          seed: {
            id: resp.session_id,
            command: req.command,
            cwd: req.cwd || "",
            title: snap.title || req.command,
            type: snap.session_type || "",
            cols: dims.cols,
            rows: dims.rows,
            started_at: Math.floor(Date.now() / 1000),
            host_id: localHostID.value,
          },
          // Resume injection is handled Go-side on the shell's first prompt
          // (see relay_host SetOnFirstPrompt) — reliable, no task-state poll.
          pinRename: oldSid && pins.isPinned(oldSid)
            ? { oldSid, newSid: resp.session_id }
            : undefined,
        };
      } catch (e) {
        logWarn("recovery", "pane spawn failed", { error: errText(e) });
        return { pickIdx, slot: i, pane: { sessionId: null, remote: false } };
      }
    });

    const seeds: SessionInfo[] = [];
    const pinRenames: Array<{ oldSid: string; newSid: string }> = [];
    for (const r of results) {
      restoredTabs[r.pickIdx].panes[r.slot] = r.pane;
      if (r.seed) seeds.push(r.seed);
      if (r.pinRename) pinRenames.push(r.pinRename);
    }
    for (const seed of seeds) pendingLocalIds.add(seed.id);
    const existingLocalIds = new Set(localList.value.map((s) => s.id));
    const newSeeds = seeds.filter((s) => !existingLocalIds.has(s.id));
    if (newSeeds.length > 0) localList.value = [...localList.value, ...newSeeds];
    for (const r of pinRenames) pins.rename(r.oldSid, r.newSid);

    tabs.value = [...tabs.value, ...restoredTabs];
    if (savedActiveIdx >= 0 && newIds[savedActiveIdx]) {
      gotoTab(newIds[savedActiveIdx]);
    } else if (newIds.length > 0) {
      gotoTab(newIds[0]);
    }
    // Persist the migrated pin set now (rather than waiting for the 300ms
    // debounce). The sidebar's first repaint after recovery reads pinnedIds
    // from the reactive Set (already up to date), but a fast-follow force
    // quit before the debounce fires would strand the new sids out of
    // config. Idempotent no-op when no rename happened.
    await pins.flushNow();
  }

  return {
    recoveryDialogState,
    onRecoveryRestore,
    onRecoveryDiscard,
  };
}
