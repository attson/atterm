<script lang="ts" setup>
import type { Tab } from "./tabsModel";

const props = defineProps<{
  tabs: Tab[];
  activeIdx: number;
}>();

const emit = defineEmits<{
  (e: "select", idx: number): void;
  (e: "close", idx: number): void;
}>();

function basename(p: string): string {
  const i = p.lastIndexOf("/");
  return i === -1 ? p : p.slice(i + 1);
}
</script>

<template>
  <div class="file-tabs">
    <div
      v-for="(t, i) in tabs"
      :key="t.path"
      class="tab"
      :class="{ active: i === activeIdx, preview: !t.persistent }"
      @click="emit('select', i)"
    >
      <span class="name">{{ basename(t.path) }}</span>
      <span class="close" @click.stop="emit('close', i)">×</span>
    </div>
  </div>
</template>

<style scoped>
.file-tabs { display: flex; flex-direction: row; overflow-x: auto; border-bottom: 1px solid #2d333b; }
.tab { display: flex; align-items: center; gap: 6px; padding: 4px 8px; font-size: 11px; border-right: 1px solid #2d333b; cursor: pointer; color: #c9d1d9; white-space: nowrap; }
.tab.active { background: #161b22; }
.tab.preview .name { font-style: italic; }
.close { opacity: 0.5; }
.close:hover { opacity: 1; }
</style>
