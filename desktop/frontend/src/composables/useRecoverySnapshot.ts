import { watch, onScopeDispose } from "vue";
import type { Ref } from "vue";
import type { Tab } from "../lib/types";
import type { SessionInfo } from "../lib/connection";
import {
  saveRecoverySnapshot,
  type RecoverySnapshot,
  type RecoveryTabSnapshot,
  type RecoveryPaneSnapshot,
  type RecoveryAIInfo,
} from "../lib/api";
import { usePlatform } from "../platform";

// AI session id captures keyed by atterm session id. Lives outside the
// reactive store so non-structural changes don't trigger watcher re-runs.
type AIState = {
  kind: "claude" | "codex" | "aider";
  session_id: string;
  captured_at_unix: number;
};

// Default debounce intervals. Structural changes (tabs/panes/AI capture)
// flush at 500ms; cwd/title heartbeat at 5s.
const STRUCTURAL_DEBOUNCE_MS = 500;
const HEARTBEAT_DEBOUNCE_MS = 5000;
// Periodic safety flush: if anything is dirty, persist at least this often so
// sleep / force-quit loses at most a few seconds of recovery state.
const SAFETY_FLUSH_MS = 10000;

export interface UseRecoverySnapshotArgs {
  tabs: Ref<Tab[]>;
  currentTabId: Ref<string | null>;
  sessionInfoFor: (sid: string) => SessionInfo | undefined;
  // Used to look through a `remote: true` viewer pane whose session actually
  // lives on this machine (sidebar-opened local session). Persisting such a
  // pane as remote bakes in a sessionID that dies on every restart, and
  // executeRestore's rebind-only branch then strands the pane on the dead sid.
  // When info.host_id matches, the pane is saved as plain-local so restore
  // re-spawns a fresh shell at last_cwd.
  localHostID: Ref<string>;
  // Test seam — production code goes through platform.events.on (backed by
  // Wails EventsOn / the Capacitor bridge / the web BroadcastChannel bus
  // depending on platform). Returns an off() function the composable calls
  // on scope dispose.
  onEvent?: (name: string, cb: (payload: any) => void) => () => void;
}

export function useRecoverySnapshot(args: UseRecoverySnapshotArgs) {
  const aiBySid = new Map<string, AIState>();
  let structuralTimer: ReturnType<typeof setTimeout> | null = null;
  let heartbeatTimer: ReturnType<typeof setTimeout> | null = null;
  let dirty = false;

  function buildSnapshot(): RecoverySnapshot {
    const tabs: RecoveryTabSnapshot[] = args.tabs.value
      .map((t): RecoveryTabSnapshot => ({
        id: t.id,
        layout: t.layout as RecoveryTabSnapshot["layout"],
        active_pane_idx: t.activePaneIdx,
        col_ratio: t.colRatio,
        row_ratio: t.rowRatio,
        panes: t.panes.map((p, idx): RecoveryPaneSnapshot => {
          const info = p.sessionId ? args.sessionInfoFor(p.sessionId) : undefined;
          const ai = p.sessionId ? aiBySid.get(p.sessionId) : undefined;
          // A sidebar-opened local session has pane.remote=true but lives on
          // this host; persist it as plain-local so restore re-spawns at the
          // saved cwd. Genuinely remote panes still save session_id+host_id so
          // executeRestore can re-bind without forking a fresh local shell.
          const persistAsRemote =
            !!p.remote && (!info || info.host_id !== args.localHostID.value);
          return {
            slot: idx,
            remote: persistAsRemote || undefined,
            host_id: persistAsRemote ? info?.host_id ?? "" : undefined,
            // Written for both remote (authoritative id used to rebind by
            // executeRestore) and local (previous-generation id, used only by
            // executeRestore's pin migration to keep pinned sessions pinned
            // after respawn). See §4.1 of
            // 2026-07-23-pinned-session-recovery-design.md.
            // Sidebar viewers (remote=true on local host) are treated as
            // plain-local, so session_id is omitted.
            session_id: (!p.remote || persistAsRemote) && p.sessionId ? p.sessionId : undefined,
            shell: info?.command?.split(" ")[0] ?? "",
            shell_args: [],
            last_cwd: info?.cwd ?? "",
            session_type: info?.type ?? "",
            last_command_line: info?.current_command ?? "",
            title: info?.title ?? "",
            ai: ai ? ({ ...ai } as RecoveryAIInfo) : undefined,
          };
        }),
      }))
      .filter((t) => t.panes.length > 0);

    return {
      version: 1,
      host_id: "",        // server overrides
      clean_shutdown: false,
      saved_at_unix: 0,   // server overrides
      active_tab_id: args.currentTabId.value ?? "",
      tabs,
    };
  }

  function flushNow() {
    if (structuralTimer) { clearTimeout(structuralTimer); structuralTimer = null; }
    if (heartbeatTimer)  { clearTimeout(heartbeatTimer);  heartbeatTimer = null; }
    dirty = false;
    void saveRecoverySnapshot(buildSnapshot());
  }

  function scheduleStructural() {
    dirty = true;
    if (structuralTimer) clearTimeout(structuralTimer);
    structuralTimer = setTimeout(() => {
      structuralTimer = null;
      flushNow();
    }, STRUCTURAL_DEBOUNCE_MS);
  }

  function scheduleHeartbeat() {
    dirty = true;
    if (heartbeatTimer) return; // first-write-wins
    heartbeatTimer = setTimeout(() => {
      heartbeatTimer = null;
      flushNow();
    }, HEARTBEAT_DEBOUNCE_MS);
  }

  // Structural watcher: deep watch on tab shape + pane slot identity.
  watch(
    () =>
      args.tabs.value.map((t) => ({
        id: t.id,
        layout: t.layout,
        active: t.activePaneIdx,
        col: t.colRatio,
        row: t.rowRatio,
        paneIds: t.panes.map((p) => p.sessionId ?? "").join("|"),
      })),
    () => {
      scheduleStructural();
    },
    { deep: true },
  );

  watch(args.currentTabId, () => {
    scheduleHeartbeat();
  });

  // Per-pane META watcher: persist when a pane's recovery-relevant metadata
  // changes — notably type shell→ai and the title (the key used to resolve the
  // AI session id). Running `claude` inside an existing pane changes no
  // structural field and does not alter sessionId, so without this watcher the
  // AI classification would not be saved until some unrelated structural/tab
  // event happened to flush. The getter returns a stable string so the watcher
  // only fires on real metadata changes (not on every task_state tick).
  //
  // current_command is deliberately NOT in this watcher: it is the live OSC 133
  // command line, which an AI session (Claude Code spinner/status) rewrites
  // every second. Routing it through the 500ms structural debounce made
  // recovery fsync the snapshot to disk once a second; it rides the 5s
  // heartbeat watcher below instead.
  watch(
    () =>
      args.tabs.value
        .map((t) =>
          t.panes
            .map((p) => {
              const i = p.sessionId ? args.sessionInfoFor(p.sessionId) : undefined;
              return [
                p.sessionId ?? "",
                i?.type ?? "",
                i?.title ?? "",
                i?.cwd ?? "",
              ].join("~");
            })
            .join("|"),
        )
        .join("||"),
    () => {
      scheduleStructural();
    },
  );

  // Per-pane current_command watcher: rides the 5s heartbeat, not the immediate
  // structural debounce, because this field churns every second on a running AI
  // session. The captured value is still useful on restore, just not worth an
  // fsync per spinner tick.
  watch(
    () =>
      args.tabs.value
        .map((t) =>
          t.panes
            .map((p) => {
              const i = p.sessionId ? args.sessionInfoFor(p.sessionId) : undefined;
              return i?.current_command ?? "";
            })
            .join("|"),
        )
        .join("||"),
    () => {
      scheduleHeartbeat();
    },
  );

  // AI sid capture event subscription.
  const evtOn = args.onEvent ?? ((name, cb) => usePlatform().events.on(name, cb));
  const off = evtOn("recovery:ai-sid", (payload: any) => {
    const sid: string = payload?.session_id ?? "";
    const kind = (payload?.kind ?? "") as "claude" | "codex" | "aider" | "";
    const aiSid: string = payload?.ai_session_id ?? "";
    if (!sid || !kind || !aiSid) return;
    if (kind !== "claude" && kind !== "codex" && kind !== "aider") return;
    aiBySid.set(sid, {
      kind,
      session_id: aiSid,
      captured_at_unix: Math.floor(Date.now() / 1000),
    });
    scheduleStructural();
  });

  // Periodic safety flush — backstop against sleep / force-quit. Only writes
  // when something is dirty, so an idle workspace stays quiet.
  const safetyTimer = setInterval(() => {
    if (dirty) flushNow();
  }, SAFETY_FLUSH_MS);

  onScopeDispose(() => {
    off?.();
    if (structuralTimer) clearTimeout(structuralTimer);
    if (heartbeatTimer) clearTimeout(heartbeatTimer);
    clearInterval(safetyTimer);
  });

  return {
    buildSnapshot,
    flushNow,
  };
}
