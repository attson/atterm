import { computed, type ComputedRef, type Ref } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";

const URGENCY: TaskState[] = [
  "waiting_input",
  "failed",
  "running",
  "completed",
  "idle",
  "disconnected",
  "closed",
];

function urgencyIndex(s?: TaskState | string): number {
  if (!s) return URGENCY.length;
  const i = URGENCY.indexOf(s as TaskState);
  return i === -1 ? URGENCY.length : i;
}

export interface UseSessionsOptions {
  /** When set, sessions whose host_id equals this are considered "local"
   *  and excluded from remoteByHost. */
  localHostId?: string;
}

export interface UseSessionsReturn {
  all: ComputedRef<RemoteSession[]>;
  byHost: ComputedRef<Record<string, RemoteSession[]>>;
  remoteByHost: ComputedRef<Record<string, RemoteSession[]>>;
  unreadByHost: ComputedRef<Record<string, number>>;
  totalUnread: ComputedRef<number>;
  completedSeen: ComputedRef<RemoteSession[]>;
  /** byState groups sessions by their task_state. Keys are TaskState values
   *  in URGENCY order — waiting_input first, closed last. Empty groups are
   *  omitted so the sidebar doesn't render dead headers. Unread is NOT
   *  promoted here (unlike byHost's per-group sort), because the user
   *  asked to see "all running together", not "all attention-worthy
   *  together". */
  byState: ComputedRef<Record<string, RemoteSession[]>>;
  /** unreadByState mirrors unreadByHost but keyed by task_state. */
  unreadByState: ComputedRef<Record<string, number>>;
  /** Plain function (not a ComputedRef). Reads byHost.value when invoked.
   *  In Vue templates the call site re-evaluates on byHost change automatically.
   *  In <script setup> reactive contexts, wrap in `computed(() => primaryStateForHost(id))`. */
  primaryStateForHost(hostId: string): TaskState;
}

export function useSessions(
  localList: Ref<RemoteSession[]>,
  remoteList: Ref<RemoteSession[]>,
  options: UseSessionsOptions = {},
): UseSessionsReturn {
  const all = computed<RemoteSession[]>(() => {
    const byId = new Map<string, RemoteSession>();
    for (const s of localList.value) byId.set(s.session_id, s);
    for (const s of remoteList.value) byId.set(s.session_id, s); // relay wins
    return [...byId.values()];
  });

  const byHost = computed<Record<string, RemoteSession[]>>(() => {
    const out: Record<string, RemoteSession[]> = {};
    for (const s of all.value) {
      const k = s.host_id || "";
      (out[k] ||= []).push(s);
    }
    // Sort by started_at ascending (oldest first), with session_id as the
    // tiebreaker for determinism. Both keys are immutable for the lifetime
    // of a session, so rows stay put — no jumping when task_state changes
    // or the PTY writes output. Attention-worthy state still surfaces via
    // the row's color and the per-row unread dot.
    for (const k of Object.keys(out)) {
      out[k].sort((a, b) => {
        const da = (a.started_at ?? 0) - (b.started_at ?? 0);
        if (da !== 0) return da;
        return a.session_id.localeCompare(b.session_id);
      });
    }
    return out;
  });

  const remoteByHost = computed<Record<string, RemoteSession[]>>(() => {
    if (!options.localHostId) return byHost.value;
    const out: Record<string, RemoteSession[]> = {};
    for (const k of Object.keys(byHost.value)) {
      if (k !== options.localHostId) out[k] = byHost.value[k];
    }
    return out;
  });

  const unreadByHost = computed<Record<string, number>>(() => {
    const out: Record<string, number> = {};
    for (const [k, list] of Object.entries(byHost.value)) {
      out[k] = list.filter((s) => s.unread === true).length;
    }
    return out;
  });

  const totalUnread = computed<number>(() =>
    Object.values(unreadByHost.value).reduce((a, b) => a + b, 0),
  );

  const completedSeen = computed<RemoteSession[]>(() =>
    all.value.filter(
      (s) =>
        (s.task_state === "completed" || s.task_state === "failed") &&
        s.unread === false,
    ),
  );

  const byState = computed<Record<string, RemoteSession[]>>(() => {
    const out: Record<string, RemoteSession[]> = {};
    for (const s of all.value) {
      const k = (s.task_state as TaskState | undefined) ?? "idle";
      (out[k] ||= []).push(s);
    }
    // Sort within each state group: unread first, then most-recent activity.
    for (const k of Object.keys(out)) {
      out[k].sort((a, b) => {
        const au = a.unread ? 0 : 1;
        const bu = b.unread ? 0 : 1;
        if (au !== bu) return au - bu;
        return (b.last_output_at ?? 0) - (a.last_output_at ?? 0);
      });
    }
    return out;
  });

  const unreadByState = computed<Record<string, number>>(() => {
    const out: Record<string, number> = {};
    for (const [k, list] of Object.entries(byState.value)) {
      out[k] = list.filter((s) => s.unread === true).length;
    }
    return out;
  });

  function primaryStateForHost(hostId: string): TaskState {
    const list = byHost.value[hostId] ?? [];
    let best: TaskState = "idle";
    let bestIdx = urgencyIndex("idle");
    for (const s of list) {
      const st = (s.task_state as TaskState | undefined) ?? "idle";
      const idx = urgencyIndex(st);
      if (idx < bestIdx) {
        best = st;
        bestIdx = idx;
      }
    }
    return best;
  }

  return {
    all,
    byHost,
    remoteByHost,
    unreadByHost,
    totalUnread,
    completedSeen,
    byState,
    unreadByState,
    primaryStateForHost,
  };
}
