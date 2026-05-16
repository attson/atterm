<script lang="ts" setup>
import { ref } from "vue";
import FileTreeNode from "./FileTreeNode.vue";
import type { MockEntry } from "./mockData";

const props = defineProps<{
  root: string;
  entries: MockEntry[];
  selectedPath?: string;
}>();

const emit = defineEmits<{
  (e: "select", path: string): void;
  (e: "dblclick", path: string): void;
}>();

const internalSelected = ref<string>("");

function effectiveSelected() {
  return props.selectedPath ?? internalSelected.value;
}

function onSelect(path: string) {
  internalSelected.value = path;
  emit("select", path);
}

function onDblclick(path: string) {
  emit("dblclick", path);
}

function sortRoot(entries: MockEntry[]): MockEntry[] {
  return [...entries].sort((a, b) =>
    a.isDir === b.isDir ? a.name.localeCompare(b.name) : a.isDir ? -1 : 1,
  );
}
</script>

<template>
  <div class="file-tree">
    <header class="fe-header">
      <span class="root-label" :title="root">{{ root }}</span>
    </header>
    <div class="tree-scroll">
      <FileTreeNode
        v-for="(e, i) in sortRoot(entries)"
        :key="e.name + i"
        :entry="e"
        :level="0"
        :parent-path="root"
        :selected-path="effectiveSelected()"
        @select="onSelect"
        @dblclick="onDblclick"
      />
    </div>
  </div>
</template>

<style scoped>
.file-tree {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--ed-tree-bg, #252526);
  color: var(--ed-row-fg, #cccccc);
  border: 1px solid var(--ed-border, #1a1a1a);
  border-radius: 4px;
  overflow: hidden;
}
.fe-header {
  display: flex;
  align-items: center;
  padding: 6px 10px;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  color: var(--ed-tree-header-fg, rgba(204, 204, 204, 0.7));
  border-bottom: 1px solid var(--ed-border, #1a1a1a);
  background: var(--ed-tree-header-bg, #1f1f1f);
}
.root-label {
  flex: 1 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tree-scroll {
  flex: 1 1 auto;
  overflow: auto;
  padding: 4px 0;
}
.tree-scroll::-webkit-scrollbar { width: 10px; height: 10px; }
.tree-scroll::-webkit-scrollbar-track { background: transparent; }
.tree-scroll::-webkit-scrollbar-thumb {
  background: var(--ed-indent-guide, rgba(204, 204, 204, 0.18));
  border-radius: 5px;
}
.tree-scroll::-webkit-scrollbar-thumb:hover { background: var(--ed-chevron, rgba(204, 204, 204, 0.3)); }
</style>
