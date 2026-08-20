import { bindings } from "./_bindings";
import type { SyncStatus } from "./_bindings";

export type { SyncStatus, PullResult } from "./_bindings";

// getSyncStatus reads the current cross-device preference sync status --
// use it for first paint; after that, listen for the "sync:status" event
// (payload: SyncStatus) instead of polling this.
export function getSyncStatus(): Promise<SyncStatus> {
  return bindings().GetSyncStatus();
}

// syncNow enqueues an immediate pull-then-push and resolves as soon as it
// is queued, not once it finishes -- the outcome arrives later via the
// "sync:status" and "sync:pulled" events. It only rejects for "cannot
// start" (no relay configured / paused / logged out).
export function syncNow(): Promise<void> {
  return bindings().SyncNow();
}
