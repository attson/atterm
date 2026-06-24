import type { SessionInfo } from "./connection";

export interface MergeLocalSessionsResult {
  localList: SessionInfo[];
  pending: Set<string>;
}

// mergeLocalSessions reconciles a freshly-arrived LIST_RESP push from Go with
// the frontend's optimistic seeds (sessions we spawned and haven't yet seen
// echoed in a Go push). Without this:
//
//   1. executeRestore spawns A, B, C and seeds each into localList.
//   2. Go broadcasts LIST_RESP frames one per spawn: [A], [A,B], [A,B,C].
//   3. The first [A] frame overwrites localList wholesale, dropping the seeds
//      for B and C.
//   4. sweepMissingSessions then nulls every pane whose sessionId is no
//      longer in localList — i.e. tabs 2 and 3.
//   5. Later frames repopulate localList but the panes are already null; the
//      sweep doesn't reverse itself.
//
// The merge keeps seeds visible until Go confirms (or never confirms) them:
//
//   - Anything in `arrived` is canonical truth from Go. It is preserved as-is.
//   - For each id still in `pending`: if `arrived` contains it, Go has now
//     acknowledged the seed → drop from pending. Otherwise keep its seeded
//     SessionInfo from `currentLocal` in the output so sweep / lookups still
//     find it.
//   - A pending id absent from `currentLocal` (pane closed early, etc.) is
//     dropped silently rather than resurrected.
export function mergeLocalSessions(
  arrived: SessionInfo[],
  currentLocal: SessionInfo[],
  pending: Set<string>,
): MergeLocalSessionsResult {
  const arrivedIds = new Set(arrived.map((s) => s.id));
  const nextPending = new Set<string>();
  const preserved: SessionInfo[] = [];
  for (const id of pending) {
    if (arrivedIds.has(id)) continue; // Go confirmed → leave pending
    const seeded = currentLocal.find((s) => s.id === id);
    if (!seeded) continue; // pane was closed before Go ever caught up
    nextPending.add(id);
    preserved.push(seeded);
  }
  return {
    localList: [...arrived, ...preserved],
    pending: nextPending,
  };
}
