import { getTaskGroupBy, setTaskGroupBy, type TaskGroupBy } from "../lib/api";
import { definePersistedSingletonRef } from "./persistedSingletonRef";

function isGroupBy(s: unknown): s is TaskGroupBy {
  return s === "host" || s === "state";
}

const store = definePersistedSingletonRef<TaskGroupBy>({
  defaultValue: "host",
  storageKey: "taskGroupBy",
  // Wrap in closures so vi.spyOn(api, "...") replacements in tests reach
  // the factory (bound references captured at module init would not).
  load: () => getTaskGroupBy(),
  save: (v) => setTaskGroupBy(v),
  isValid: isGroupBy,
});

export interface UseTaskGroupBy {
  activeId: typeof store.activeId;
  setGroupBy(id: TaskGroupBy): Promise<void>;
}

export function useTaskGroupBy(): UseTaskGroupBy {
  store.ensureLoaded();
  return { activeId: store.activeId, setGroupBy: store.set };
}

// Test-only reset for the singleton.
export function __resetForTests(): void {
  store.__resetForTests();
}
