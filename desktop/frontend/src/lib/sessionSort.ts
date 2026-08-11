import type { TaskState } from "./taskState";

export interface SessionSortFields {
  session_id?: string;
  task_state?: TaskState | string;
  unread?: boolean;
  last_output_at?: number;
  command_ended_at?: number;
  command_started_at?: number;
  attention_at?: number;
  started_at?: number;
}

export const SESSION_URGENCY: readonly TaskState[] = [
  "waiting_input", "failed", "running", "completed", "idle", "disconnected", "closed",
];

export function sessionUrgencyIndex(state?: TaskState | string): number {
  if (!state) return SESSION_URGENCY.length;
  const index = SESSION_URGENCY.indexOf(state as TaskState);
  return index === -1 ? SESSION_URGENCY.length : index;
}

export function sessionActivityAt(session: SessionSortFields): number {
  return Math.max(
    session.last_output_at ?? 0,
    session.command_ended_at ?? 0,
    session.command_started_at ?? 0,
    session.attention_at ?? 0,
    session.started_at ?? 0,
  );
}

/** Sidebar's state-group ordering: unread first, then latest activity, then id. */
export function compareSessionsBySidebarOrder(a: SessionSortFields, b: SessionSortFields): number {
  const stateDelta = sessionUrgencyIndex(a.task_state) - sessionUrgencyIndex(b.task_state);
  if (stateDelta !== 0) return stateDelta;
  const unreadDelta = (a.unread ? 0 : 1) - (b.unread ? 0 : 1);
  if (unreadDelta !== 0) return unreadDelta;
  const activityDelta = sessionActivityAt(b) - sessionActivityAt(a);
  if (activityDelta !== 0) return activityDelta;
  return (a.session_id ?? "").localeCompare(b.session_id ?? "");
}

export function compareSessionsByLatestActivity(a: SessionSortFields, b: SessionSortFields): number {
  const activityDelta = sessionActivityAt(b) - sessionActivityAt(a);
  if (activityDelta !== 0) return activityDelta;
  return (a.session_id ?? "").localeCompare(b.session_id ?? "");
}
