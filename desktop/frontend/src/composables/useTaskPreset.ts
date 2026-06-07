import { computed, type ComputedRef, ref } from "vue";
import { getTaskPreset, setTaskPreset } from "../lib/api";
import { presets, type PresetId, type TaskStatePreset } from "../lib/taskState";

const STORAGE_KEY = "taskPreset";

function isPresetId(s: string | null | undefined): s is PresetId {
  return s === "iconOnly" || s === "iconLabel";
}

// Module-level singleton — multiple call sites share one source of truth.
const activeId = ref<PresetId>("iconOnly");
let initialized = false;

async function loadInitial() {
  try {
    const v = await getTaskPreset();
    if (isPresetId(v)) {
      activeId.value = v;
      return;
    }
  } catch {
    /* fall through */
  }
  if (typeof localStorage !== "undefined") {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (isPresetId(stored)) {
      activeId.value = stored;
      return;
    }
  }
}

export interface UseTaskPreset {
  activeId: typeof activeId;
  active: ComputedRef<TaskStatePreset>;
  setPreset(id: PresetId): Promise<void>;
}

export function useTaskPreset(): UseTaskPreset {
  if (!initialized) {
    initialized = true;
    void loadInitial();
  }
  const active = computed(() => presets[activeId.value]);
  async function setPreset(id: PresetId) {
    activeId.value = id;
    try {
      await setTaskPreset(id);
    } catch {
      if (typeof localStorage !== "undefined") {
        localStorage.setItem(STORAGE_KEY, id);
      }
    }
  }
  return { activeId, active, setPreset };
}

// Test-only reset for the singleton.
export function __resetForTests(): void {
  initialized = false;
  activeId.value = "iconOnly";
}
