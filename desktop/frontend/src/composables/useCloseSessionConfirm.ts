import { computed, ref, type ComputedRef, type Ref } from "vue";

import type { RemoteSession } from "../platform/types";

/**
 * useCloseSessionConfirm holds the "are you sure you want to close this
 * session?" dialog state and the logic that decides when to skip the prompt.
 *
 * The dialog is opened from four unrelated call sites in App.vue (sidebar
 * close button, tab close, pane close, and multi-select close) with
 * different follow-up actions. Rather than repeat the state plumbing at
 * each site, callers hand `openCloseSessionConfirm` the sessions to warn
 * about and a thunk to run after "Confirm" — the composable stores the
 * thunk, drives the dialog, and calls it exactly when the user confirms.
 *
 * A session is "risky to close" if it is an AI session or has a running
 * task. Actively-open remote panes are exempt — the confirmation is
 * really about losing state you can't easily rebuild (an AI conversation,
 * an in-flight task), and a remote pane you're staring at doesn't
 * qualify. That "is this session currently open as a remote pane?" check
 * lives in App.vue (it reads the tabs tree) and is injected as
 * `isOpenRemoteSession`.
 */
export interface UseCloseSessionConfirmOpts {
  /** True when the given session id is mounted in one of the current
   *  tabs as a remote pane. Injected because it reads App.vue's `tabs`. */
  isOpenRemoteSession: (sessionId: string) => boolean;
}

export interface UseCloseSessionConfirm {
  // Dialog state — bound to <ConfirmCloseSessionDialog> in App.vue.
  pendingCloseSessions: Ref<RemoteSession[]>;
  pendingCloseTitle: ComputedRef<string>;
  pendingCloseIsAi: ComputedRef<boolean>;
  pendingCloseIsRunning: ComputedRef<boolean>;
  pendingCloseIsRemote: ComputedRef<boolean>;
  confirmCloseSession: () => void;
  cancelCloseSession: () => void;

  /** Pure predicate: AI session or currently-running task. */
  isCloseRiskySession: (s: RemoteSession) => boolean;
  /** True when a close attempt should pop the dialog first. */
  shouldConfirmCloseSession: (s: RemoteSession) => boolean;
  /** Open the dialog for the given sessions; on Confirm, run `action`. */
  openCloseSessionConfirm: (
    sessions: RemoteSession[],
    action: () => void | Promise<void>,
  ) => void;
}

function sessionCloseTitle(s: RemoteSession): string {
  return s.current_command || s.title || s.session_id.slice(0, 8);
}

export function useCloseSessionConfirm(
  opts: UseCloseSessionConfirmOpts,
): UseCloseSessionConfirm {
  const { isOpenRemoteSession } = opts;

  const pendingCloseSession = ref<RemoteSession | null>(null);
  const pendingCloseSessions = ref<RemoteSession[]>([]);
  let pendingCloseAction: (() => void | Promise<void>) | null = null;

  function isCloseRiskySession(s: RemoteSession): boolean {
    return s.type === "ai" || s.task_state === "running";
  }

  function shouldConfirmCloseSession(s: RemoteSession): boolean {
    return !isOpenRemoteSession(s.session_id) && isCloseRiskySession(s);
  }

  const pendingCloseTitle = computed(() => {
    if (pendingCloseSessions.value.length > 1) return "";
    const s = pendingCloseSession.value;
    return s ? sessionCloseTitle(s) : "";
  });

  const pendingCloseIsAi = computed(() =>
    pendingCloseSessions.value.some((s) => s.type === "ai"),
  );
  const pendingCloseIsRunning = computed(() =>
    pendingCloseSessions.value.some((s) => s.task_state === "running"),
  );
  const pendingCloseIsRemote = computed(
    () =>
      pendingCloseSessions.value.length > 0 &&
      pendingCloseSessions.value.every((s) => isOpenRemoteSession(s.session_id)),
  );

  function clearPending() {
    pendingCloseSession.value = null;
    pendingCloseSessions.value = [];
    pendingCloseAction = null;
  }

  function openCloseSessionConfirm(
    sessions: RemoteSession[],
    action: () => void | Promise<void>,
  ) {
    pendingCloseSessions.value = sessions;
    pendingCloseSession.value = sessions[0] ?? null;
    pendingCloseAction = action;
  }

  function confirmCloseSession() {
    const action = pendingCloseAction;
    clearPending();
    void action?.();
  }

  function cancelCloseSession() {
    clearPending();
  }

  return {
    pendingCloseSessions,
    pendingCloseTitle,
    pendingCloseIsAi,
    pendingCloseIsRunning,
    pendingCloseIsRemote,
    confirmCloseSession,
    cancelCloseSession,
    isCloseRiskySession,
    shouldConfirmCloseSession,
    openCloseSessionConfirm,
  };
}
