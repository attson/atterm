import { ref, type Ref } from "vue";
import { usePlatform } from "../platform";

// Module-scoped so state is shared across all consumers within the app
// lifetime (matches useCollapsedGroups / useTaskGroupBy pattern).
const pinnedIds = ref<Set<string>>(new Set());
let loaded = false;
let loadPromise: Promise<void> | null = null;
let flushHandle: ReturnType<typeof setTimeout> | null = null;
let eventsBound = false;

const PERSIST_DEBOUNCE_MS = 300;

async function readFromPlatform(): Promise<void> {
  try {
    const list = await usePlatform().sessions.getPins();
    pinnedIds.value = new Set(list);
  } catch {
    /* best-effort — leave whatever value we had */
  }
}

async function loadOnce(): Promise<void> {
  if (loaded) return;
  if (loadPromise) return loadPromise;
  loadPromise = (async () => {
    try {
      await readFromPlatform();
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
    void usePlatform().sessions.setPins(Array.from(pinnedIds.value)).catch((e) => {
      /* best-effort */
      console.warn("[pins] schedulePersist failed", e);
    });
  }, PERSIST_DEBOUNCE_MS);
}

// Re-reads from the store whenever the Go side (or another device via
// prefsSync) reconciles the pin list, so open windows/tabs stay in sync
// without requiring a manual reload() call.
function bindEventsOnce(): void {
  if (eventsBound) return;
  eventsBound = true;
  usePlatform().events.on("prefs:changed", () => {
    void readFromPlatform();
  });
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

function renameFn(oldId: string, newId: string): void {
  if (!oldId || !newId || oldId === newId) return;
  if (!pinnedIds.value.has(oldId)) return;
  const next = new Set(pinnedIds.value);
  next.delete(oldId);
  next.add(newId);
  pinnedIds.value = next;
  schedulePersist();
}

async function flushNowFn(): Promise<void> {
  if (flushHandle) {
    clearTimeout(flushHandle);
    flushHandle = null;
  }
  try {
    await usePlatform().sessions.setPins(Array.from(pinnedIds.value));
  } catch (e) {
    // Still best-effort (same policy as schedulePersist), but recovery is
    // the one synchronous window where a silent failure would be costly —
    // log it so the caller has a signal even though flushNow keeps resolving.
    console.warn("[pins] flushNow persist failed", e);
  }
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
  rename: (oldId: string, newId: string) => void;
  flushNow: () => Promise<void>;
  // Resolves once the initial platform.sessions.getPins() load has settled
  // (loaded or failed-to-empty — see loadOnce). Callers that need pinnedIds
  // to be authoritative before reading it synchronously (e.g. recovery's pin
  // migration) should `await` this first instead of racing the fire-and-
  // forget load that useSessionPins() kicks off on every call.
  ready: () => Promise<void>;
  /** Force a re-read from the store, ignoring the internal loaded guard.
   *  Bound to prefs:changed event; also callable directly. */
  reload: () => Promise<void>;
}

export function useSessionPins(): UseSessionPins {
  void loadOnce();
  bindEventsOnce();
  return {
    pinnedIds,
    isPinned: (id) => pinnedIds.value.has(id),
    pin: pinFn,
    unpin: unpinFn,
    toggle: (id) => (pinnedIds.value.has(id) ? unpinFn(id) : pinFn(id)),
    rename: renameFn,
    flushNow: flushNowFn,
    ready: () => loadOnce(),
    reload: () => readFromPlatform(),
  };
}

export function __resetForTests(): void {
  pinnedIds.value = new Set();
  loaded = false;
  loadPromise = null;
  eventsBound = false;
  if (flushHandle) {
    clearTimeout(flushHandle);
    flushHandle = null;
  }
}
