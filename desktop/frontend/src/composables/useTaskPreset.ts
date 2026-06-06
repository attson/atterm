import { computed, type ComputedRef, ref, watch } from "vue";
import { getTaskPreset, setTaskPreset } from "../lib/api";
import { presets, type PresetId, type TaskStatePreset } from "../lib/taskState";

const STORAGE_KEY = "taskPreset";

function isPresetId(s: string | null | undefined): s is PresetId {
  return s === "vivid" || s === "quiet";
}

// Module-level singleton — multiple call sites share one source of truth.
const activeId = ref<PresetId>("vivid");
let initialized = false;

function applyDataset(id: PresetId) {
  document.documentElement.dataset.taskPreset = id;
}

async function loadInitial() {
  try {
    const v = await getTaskPreset();
    if (isPresetId(v)) {
      activeId.value = v;
      applyDataset(v);
      return;
    }
  } catch {
    /* fall through */
  }
  if (typeof localStorage !== "undefined") {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (isPresetId(stored)) {
      activeId.value = stored;
      applyDataset(stored);
      return;
    }
  }
  applyDataset("vivid");
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
    watch(activeId, (v) => applyDataset(v));
  }
  const active = computed(() => presets[activeId.value]);
  async function setPreset(id: PresetId) {
    activeId.value = id;
    applyDataset(id);
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
  activeId.value = "vivid";
  if (typeof document !== "undefined") {
    delete document.documentElement.dataset.taskPreset;
  }
}
