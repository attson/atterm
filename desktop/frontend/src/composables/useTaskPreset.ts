import { computed, type ComputedRef } from "vue";
import { getTaskPreset, setTaskPreset } from "../lib/api";
import { presets, type PresetId, type TaskStatePreset } from "../lib/taskState";
import { definePersistedSingletonRef } from "./persistedSingletonRef";

function isPresetId(s: unknown): s is PresetId {
  return s === "iconOnly" || s === "iconLabel";
}

const store = definePersistedSingletonRef<PresetId>({
  defaultValue: "iconOnly",
  storageKey: "taskPreset",
  // Wrap in closures so vi.spyOn(api, "...") replacements in tests reach
  // the factory (bound references captured at module init would not).
  load: () => getTaskPreset(),
  save: (v) => setTaskPreset(v),
  isValid: isPresetId,
});

export interface UseTaskPreset {
  activeId: typeof store.activeId;
  active: ComputedRef<TaskStatePreset>;
  setPreset(id: PresetId): Promise<void>;
}

export function useTaskPreset(): UseTaskPreset {
  store.ensureLoaded();
  const active = computed(() => presets[store.activeId.value]);
  return { activeId: store.activeId, active, setPreset: store.set };
}

// Test-only reset for the singleton.
export function __resetForTests(): void {
  store.__resetForTests();
}
