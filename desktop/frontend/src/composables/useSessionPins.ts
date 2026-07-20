import { ref, type Ref } from "vue";
import { getPinnedSessionIds, setPinnedSessionIds } from "../lib/api";

// Module-scoped so state is shared across all consumers within the app
// lifetime (matches useCollapsedGroups / useTaskGroupBy pattern).
const pinnedIds = ref<Set<string>>(new Set());
let loaded = false;
let loadPromise: Promise<void> | null = null;
let flushHandle: ReturnType<typeof setTimeout> | null = null;

const PERSIST_DEBOUNCE_MS = 300;

async function loadOnce(): Promise<void> {
  if (loaded) return;
  if (loadPromise) return loadPromise;
  loadPromise = (async () => {
    try {
      const list = await getPinnedSessionIds();
      pinnedIds.value = new Set(list);
    } catch {
      /* best-effort — leave the empty default in place */
    } finally {
      loaded = true;
      loadPromise = null;
    }
  })();
  return loadPromise;
}

function schedulePersist(): void {
  if (flushHandle) clearTimeout(flushHandle);
  flushHandle = setTimeout(() => {
    flushHandle = null;
    void setPinnedSessionIds(Array.from(pinnedIds.value)).catch(() => {
      /* best-effort */
    });
  }, PERSIST_DEBOUNCE_MS);
}

function pinFn(id: string): void {
  if (!id || pinnedIds.value.has(id)) return;
  const next = new Set(pinnedIds.value);
  next.add(id);
  pinnedIds.value = next;
  schedulePersist();
}

function unpinFn(id: string): void {
  if (!pinnedIds.value.has(id)) return;
  const next = new Set(pinnedIds.value);
  next.delete(id);
  pinnedIds.value = next;
  schedulePersist();
}

export interface UseSessionPins {
  // Read-only from the outside — the only safe mutation entry points are
  // pin/unpin/toggle, which always create a fresh Set so Vue's reactivity
  // picks up the change. Exposing a mutable Set/Ref would let a consumer
  // call .add()/.delete() directly, which is a same-instance mutation that
  // Vue's shallow-ref change detection silently misses.
  pinnedIds: Readonly<Ref<ReadonlySet<string>>>;
  isPinned: (id: string) => boolean;
  pin: (id: string) => void;
  unpin: (id: string) => void;
  toggle: (id: string) => void;
}

export function useSessionPins(): UseSessionPins {
  void loadOnce();
  return {
    pinnedIds,
    isPinned: (id) => pinnedIds.value.has(id),
    pin: pinFn,
    unpin: unpinFn,
    toggle: (id) => (pinnedIds.value.has(id) ? unpinFn(id) : pinFn(id)),
  };
}

export function __resetForTests(): void {
  pinnedIds.value = new Set();
  loaded = false;
  loadPromise = null;
  if (flushHandle) {
    clearTimeout(flushHandle);
    flushHandle = null;
  }
}
