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

/**
 * sessionInteractionAt is the sort key: when this session last changed in a way
 * the *user* would call an event — a command started or finished, it asked for
 * attention, or it was created.
 *
 * `last_output_at` is deliberately not in here. It ticks on every chunk of PTY
 * output, so two AI sessions streaming at once trade the lead several times a
 * second and the sidebar reorders under the pointer while you are trying to
 * click a row. Every field below is frozen for the whole of a run, so rows hold
 * still while the sessions work.
 */
export function sessionInteractionAt(session: SessionSortFields): number {
  return Math.max(
    session.command_ended_at ?? 0,
    session.command_started_at ?? 0,
    session.attention_at ?? 0,
    session.started_at ?? 0,
  );
}

/**
 * Output freshness, quantised to the minute, as a last-resort tiebreak.
 *
 * Sessions with no shell integration carry none of the interaction timestamps
 * beyond `started_at`, so without this they would order purely by creation and
 * a busy one could never surface. The bucket is what keeps that from
 * reintroducing the churn: rows can only change places when a session crosses a
 * minute boundary, not on every chunk.
 */
export const OUTPUT_BUCKET_SECONDS = 60;

export function sessionOutputBucket(session: SessionSortFields): number {
  const at = session.last_output_at ?? 0;
  return at > 0 ? Math.floor(at / OUTPUT_BUCKET_SECONDS) : 0;
}

/** Shared tail for both orderings: interaction, then coarse output, then id. */
function compareActivityTail(a: SessionSortFields, b: SessionSortFields): number {
  const interactionDelta = sessionInteractionAt(b) - sessionInteractionAt(a);
  if (interactionDelta !== 0) return interactionDelta;
  const bucketDelta = sessionOutputBucket(b) - sessionOutputBucket(a);
  if (bucketDelta !== 0) return bucketDelta;
  return (a.session_id ?? "").localeCompare(b.session_id ?? "");
}

/** Sidebar's state-group ordering: unread first, then latest activity, then id. */
export function compareSessionsBySidebarOrder(a: SessionSortFields, b: SessionSortFields): number {
  const stateDelta = sessionUrgencyIndex(a.task_state) - sessionUrgencyIndex(b.task_state);
  if (stateDelta !== 0) return stateDelta;
  const unreadDelta = (a.unread ? 0 : 1) - (b.unread ? 0 : 1);
  if (unreadDelta !== 0) return unreadDelta;
  return compareActivityTail(a, b);
}

export function compareSessionsByLatestActivity(a: SessionSortFields, b: SessionSortFields): number {
  return compareActivityTail(a, b);
}
