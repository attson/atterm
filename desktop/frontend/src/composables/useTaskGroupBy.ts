import { ref } from "vue";
import { getTaskGroupBy, setTaskGroupBy, type TaskGroupBy } from "../lib/api";

const STORAGE_KEY = "taskGroupBy";

function isGroupBy(s: string | null | undefined): s is TaskGroupBy {
  return s === "host" || s === "state";
}

const activeId = ref<TaskGroupBy>("host");
let initialized = false;

async function loadInitial() {
  try {
    const v = await getTaskGroupBy();
    if (isGroupBy(v)) {
      activeId.value = v;
      return;
    }
  } catch {
    /* fall through */
  }
  if (typeof localStorage !== "undefined") {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (isGroupBy(stored)) {
      activeId.value = stored;
      return;
    }
  }
}

export interface UseTaskGroupBy {
  activeId: typeof activeId;
  setGroupBy(id: TaskGroupBy): Promise<void>;
}

export function useTaskGroupBy(): UseTaskGroupBy {
  if (!initialized) {
    initialized = true;
    void loadInitial();
  }
  async function setGroupByFn(id: TaskGroupBy) {
    activeId.value = id;
    try {
      await setTaskGroupBy(id);
    } catch {
      if (typeof localStorage !== "undefined") {
        localStorage.setItem(STORAGE_KEY, id);
      }
    }
  }
  return { activeId, setGroupBy: setGroupByFn };
}

// Test-only reset for the singleton.
export function __resetForTests(): void {
  initialized = false;
  activeId.value = "host";
}
