import { computed, watch, type ComputedRef } from "vue";
import type { RemoteSession } from "../platform/types";

interface ActiveSessionSeenOptions {
  activeSessionId: ComputedRef<string | null>;
  sessions: ComputedRef<RemoteSession[]>;
  markSeen: (sessionId: string) => Promise<void>;
}

/** Marks the focused session read whenever focus or its attention version changes. */
export function useActiveSessionSeen(options: ActiveSessionSeenOptions): void {
  const unreadKey = computed(() => {
    const id = options.activeSessionId.value;
    if (!id) return null;
    const session = options.sessions.value.find((item) => item.session_id === id);
    if (!session || session.unread !== true || session.peer_direct === true) return null;
    return `${id}:${session.attention_at ?? 0}`;
  });

  watch(
    unreadKey,
    (key) => {
      if (!key) return;
      const id = options.activeSessionId.value;
      if (!id) return;
      // App.vue owns user-visible failure handling. Keep this watcher from
      // leaking a rejected Promise if a future caller uses a throwing bridge.
      void options.markSeen(id).catch(() => {});
    },
    { immediate: true },
  );
}
