<script lang="ts" setup>
import { X } from "lucide-vue-next";
import type { Tab } from "../src/plugins/fileExplorer/tabsModel";

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
      v-for="(t, i) in props.tabs"
      :key="t.path"
      class="tab"
      :class="{ active: i === props.activeIdx, preview: !t.persistent }"
      :title="t.path"
      @click="emit('select', i)"
    >
      <span class="name">{{ basename(t.path) }}</span>
      <span class="close" @click.stop="emit('close', i)">
        <X :size="12" :stroke-width="2" />
      </span>
    </div>
  </div>
</template>

<style scoped>
.file-tabs {
  display: flex;
  flex-direction: row;
  overflow-x: auto;
  border-bottom: 1px solid var(--ed-border, #1a1a1a);
  background: var(--ed-tab-bg, #252526);
  flex: 0 0 auto;
}
.tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  font-size: 12px;
  border-right: 1px solid var(--ed-border, #1a1a1a);
  cursor: pointer;
  color: var(--ed-tab-fg, #969696);
  white-space: nowrap;
  position: relative;
  user-select: none;
}
.tab:hover { color: var(--ed-tab-hover-fg, #cccccc); }
.tab.active {
  background: var(--ed-tab-active-bg, #1e1e1e);
  color: var(--ed-tab-active-fg, #ffffff);
}
.tab.active::after {
  content: "";
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 1px;
  background: var(--ed-tab-active-bar, #1177bb);
}
.tab.preview .name { font-style: italic; }
.close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  border-radius: 3px;
  opacity: 0.55;
}
.close:hover {
  opacity: 1;
  background: var(--ed-row-hover, rgba(255, 255, 255, 0.1));
}
</style>
