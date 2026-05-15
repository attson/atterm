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
  gap: 4px;
  padding: 0 8px;
  overflow-x: auto;
  white-space: nowrap;
  width: 100%;
  height: 100%;
  box-sizing: border-box;
}
.quick-input-btn {
  padding: 2px 10px;
  border-radius: 4px;
  border: 1px solid #2d333b;
  background: #21262d;
  color: #c9d1d9;
  font-size: 12px;
  cursor: pointer;
}
.quick-input-btn:hover {
  background: #30363d;
}
</style>
