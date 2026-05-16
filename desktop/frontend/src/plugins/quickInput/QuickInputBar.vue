<script lang="ts" setup>
import { computed, onMounted } from "vue";
import { usePluginConfigStore } from "../configStore";
import { useQuickInputHotkeys } from "./useQuickInputHotkeys";
import type { PluginContext } from "../types";
import type { QuickInputButton } from "../configStore";

const props = defineProps<{ context: PluginContext }>();
const store = usePluginConfigStore();

onMounted(async () => {
  if (!store.cfg) await store.load();
});

const buttons = computed<QuickInputButton[]>(() => store.cfg?.quickInput.buttons ?? []);

function fire(b: QuickInputButton) {
  const text = b.appendNewline ? b.send + "\n" : b.send;
  props.context.send(text);
}

function tooltipFor(send: string, newline: boolean, hotkey?: string): string {
  const shown = newline ? send + "\\n" : send;
  return hotkey ? `${shown} (${hotkey})` : shown;
}

useQuickInputHotkeys(buttons, fire);
</script>

<template>
  <div class="quick-input-bar">
    <button
      v-for="b in buttons"
      :key="b.id"
      class="quick-input-btn"
      :title="tooltipFor(b.send, b.appendNewline, b.hotkey)"
      @click="fire(b)"
    >{{ b.label }}</button>
  </div>
</template>

<style scoped>
.quick-input-bar {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 3px;
  padding: 0 6px;
  overflow-x: auto;
  white-space: nowrap;
  width: 100%;
  height: 100%;
  box-sizing: border-box;
}
.quick-input-btn {
  padding: 1px 8px;
  border-radius: 3px;
  border: 1px solid var(--ed-border, #2d333b);
  background: var(--ed-shell-bg, #21262d);
  color: var(--ed-row-fg, #c9d1d9);
  font-size: 11px;
  line-height: 16px;
  cursor: pointer;
}
.quick-input-btn:hover {
  background: var(--ed-row-hover, #30363d);
  color: var(--ed-tab-hover-fg, var(--ed-row-fg, #ffffff));
}
</style>
