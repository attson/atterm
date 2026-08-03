import { ref, type Ref } from "vue";

/**
 * Factory for a module-scoped preference ref that:
 *   1. Loads its initial value from a Go/Wails backend getter on first use.
 *   2. Falls back to `localStorage[storageKey]` if the backend rejects or
 *      returns an invalid value (browser preview, first-run, tests, ...).
 *   3. On every set, optimistically updates the ref, calls the backend
 *      setter, and on failure caches the value in `localStorage` so the
 *      choice survives a reload even though it never made it to Go.
 *   4. Exposes `__resetForTests()` so per-test state doesn't leak.
 *
 * Consumers: useTaskGroupBy, useTaskPreset. `useSessionPins` is deliberately
 * NOT built on this — it has a debounced persister, a prefs:changed listener,
 * a Set (not a primitive), and talks to `platform.sessions.*` instead of the
 * Wails API, so hoisting it would require enough optional knobs that the
 * factory turns into a leaky per-consumer config bag.
 *
 * `isValid` is used both to validate what the backend returns AND to validate
 * what localStorage returns; storing garbage in either source shouldn't
 * clobber a good default.
 */
export interface PersistedSingletonRefOptions<T> {
  /** Fallback used both as the ref's initial value and after `__resetForTests`. */
  defaultValue: T;
  /** localStorage key; also part of the debug story. */
  storageKey: string;
  /** Backend getter — return the persisted value or a Promise of it. */
  load: () => Promise<unknown>;
  /** Backend setter — Promise so the caller can await + fall back on reject. */
  save: (v: T) => Promise<void>;
  /** Type guard that decides whether a candidate string / unknown is a valid T. */
  isValid: (v: unknown) => v is T;
}

export interface PersistedSingletonRef<T> {
  /** Reactive ref. Read from templates; write via `set()` to trigger persistence. */
  activeId: Ref<T>;
  /** Ensures the initial load has been kicked off. Idempotent. */
  ensureLoaded(): void;
  /** Optimistically update + persist. */
  set(v: T): Promise<void>;
  /** Test-only: reset ref + reload gate. Never call from app code. */
  __resetForTests(): void;
}

export function definePersistedSingletonRef<T>(
  opts: PersistedSingletonRefOptions<T>,
): PersistedSingletonRef<T> {
  const activeId = ref<T>(opts.defaultValue) as Ref<T>;
  let initialized = false;

  async function loadInitial(): Promise<void> {
    try {
      const v = await opts.load();
      if (opts.isValid(v)) {
        activeId.value = v;
        return;
      }
    } catch {
      /* fall through to localStorage */
    }
    if (typeof localStorage !== "undefined") {
      const stored = localStorage.getItem(opts.storageKey);
      if (opts.isValid(stored)) activeId.value = stored;
    }
  }

  function ensureLoaded(): void {
    if (initialized) return;
    initialized = true;
    void loadInitial();
  }

  async function set(v: T): Promise<void> {
    activeId.value = v;
    try {
      await opts.save(v);
    } catch {
      if (typeof localStorage !== "undefined") {
        // localStorage requires a string; the two consumers today store a
        // primitive that stringifies cleanly. If a future consumer needs a
        // richer type, plumb an encode/decode pair through opts.
        localStorage.setItem(opts.storageKey, String(v));
      }
    }
  }

  function __resetForTests(): void {
    initialized = false;
    activeId.value = opts.defaultValue;
  }

  return { activeId, ensureLoaded, set, __resetForTests };
}
