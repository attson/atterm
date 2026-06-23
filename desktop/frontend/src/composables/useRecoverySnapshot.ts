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
import { EventsOn } from "../../wailsjs/runtime/runtime";

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
  // Test seam — production code calls Wails EventsOn. Returns an off()
  // function the composable calls on scope dispose.
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
          return {
            slot: idx,
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
                i?.current_command ?? "",
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

  // AI sid capture event subscription.
  const evtOn = args.onEvent ?? ((name, cb) => EventsOn(name, cb));
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
