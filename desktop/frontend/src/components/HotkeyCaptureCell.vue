<script lang="ts" setup>
import { onBeforeUnmount, ref } from "vue";
import { serialize, type Mod } from "../lib/shortcutBindings";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  value: string;
  mod: Mod;
}>();

const emit = defineEmits<{
  (e: "update", value: string): void;
  (e: "cancel"): void;
}>();

const capturing = ref(false);
const { t } = useI18n();
let listener: ((e: KeyboardEvent) => void) | null = null;

function startCapture() {
  if (capturing.value) return;
  capturing.value = true;
  listener = (e: KeyboardEvent) => {
    // Eat navigation/confirm keys while capturing so the surrounding dialog
    // doesn't act on them.
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      stopCapture();
      emit("cancel");
      return;
    }
    if (e.key === "Backspace") {
      e.preventDefault();
      e.stopPropagation();
      stopCapture();
      emit("update", "");
      return;
    }
    if (e.key === "Tab" || e.key === "Enter") {
      e.preventDefault();
      e.stopPropagation();
      return;
    }
    const binding = serialize(e, props.mod);
    if (binding === null) return; // modifier-only, bare letter, or non-whitelisted code
    e.preventDefault();
    e.stopPropagation();
    stopCapture();
    emit("update", binding);
  };
  document.addEventListener("keydown", listener, { capture: true });
}

function stopCapture() {
  capturing.value = false;
  if (listener) {
    document.removeEventListener("keydown", listener, { capture: true } as EventListenerOptions);
    listener = null;
  }
}

onBeforeUnmount(stopCapture);
</script>

<template>
  <button
    type="button"
    class="hotkey-cell"
    :class="{ capturing, empty: !capturing && value === '' }"
    @click="startCapture"
  >
    <template v-if="capturing">{{ t("settings.shortcuts.pressKey") }}</template>
    <template v-else-if="value === ''">{{ t("common.disabled") }}</template>
    <template v-else>{{ value }}</template>
  </button>
</template>

<style scoped>
.hotkey-cell {
  font-family: "SF Mono", Menlo, monospace;
  font-size: 12px;
  background: #0d1117;
  border: 1px solid #2d333b;
  color: #c9d1d9;
  padding: 3px 8px;
  border-radius: 4px;
  cursor: pointer;
  min-width: 160px;
  text-align: left;
}
.hotkey-cell:hover {
  background: #161b22;
}
.hotkey-cell.capturing {
  background: rgba(88, 166, 255, 0.12);
  border-color: var(--accent);
  color: var(--accent);
}
.hotkey-cell.empty {
  color: #6e7681;
  font-style: italic;
}
</style>
