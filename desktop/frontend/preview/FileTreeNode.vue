<script lang="ts">
import { defineComponent } from "vue";
// Self-name so the template can recurse to itself.
export default defineComponent({ name: "FileTreeNode" });
</script>

<script lang="ts" setup>
import { computed, ref } from "vue";
import {
  ChevronDown,
  ChevronRight,
  Folder,
  FolderOpen,
  File,
  FileCode2,
  FileText,
  Braces,
  Image,
} from "lucide-vue-next";
import type { MockEntry } from "./mockData";

const props = defineProps<{
  entry: MockEntry;
  level: number;
  selectedPath: string;
  parentPath: string;
}>();

const emit = defineEmits<{
  (e: "select", path: string): void;
  (e: "dblclick", path: string): void;
}>();

const expanded = ref(props.level === 0 && props.entry.isDir);

const fullPath = computed(() => `${props.parentPath}/${props.entry.name}`);
const isSelected = computed(() => props.selectedPath === fullPath.value);

const fileIconMap: Record<string, { comp: any; color: string }> = {
  ts: { comp: FileCode2, color: "#3178c6" },
  tsx: { comp: FileCode2, color: "#3178c6" },
  js: { comp: FileCode2, color: "#f7df1e" },
  jsx: { comp: FileCode2, color: "#f7df1e" },
  go: { comp: FileCode2, color: "#00add8" },
  py: { comp: FileCode2, color: "#3776ab" },
  rs: { comp: FileCode2, color: "#dea584" },
  sh: { comp: FileCode2, color: "#89e051" },
  vue: { comp: FileCode2, color: "#41b883" },
  json: { comp: Braces, color: "#cbcb41" },
  md: { comp: FileText, color: "#519aba" },
  markdown: { comp: FileText, color: "#519aba" },
  txt: { comp: FileText, color: "#cccccc" },
  yaml: { comp: FileCode2, color: "#cb171e" },
  yml: { comp: FileCode2, color: "#cb171e" },
  toml: { comp: FileCode2, color: "#9c4221" },
  html: { comp: FileCode2, color: "#e44d26" },
  htm: { comp: FileCode2, color: "#e44d26" },
  css: { comp: FileCode2, color: "#264de4" },
  scss: { comp: FileCode2, color: "#c6538c" },
  sass: { comp: FileCode2, color: "#c6538c" },
  png: { comp: Image, color: "#a074c4" },
  jpg: { comp: Image, color: "#a074c4" },
  jpeg: { comp: Image, color: "#a074c4" },
  gif: { comp: Image, color: "#a074c4" },
  svg: { comp: Image, color: "#ffb13b" },
  ico: { comp: Image, color: "#a074c4" },
};

function fileIconFor(name: string) {
  const m = /\.([A-Za-z0-9]+)$/.exec(name);
  if (m) {
    const ext = m[1].toLowerCase();
    const entry = fileIconMap[ext];
    if (entry) return entry;
  }
  return { comp: File, color: "#cccccc" };
}

function sortedChildren(children: MockEntry[]): MockEntry[] {
  return [...children].sort((a, b) =>
    a.isDir === b.isDir ? a.name.localeCompare(b.name) : a.isDir ? -1 : 1,
  );
}

// Theme colors are read from CSS custom properties at render time so the
// node re-themes when the root class flips (see preview/styles.css).
// Switching theme triggers a Vue re-render (PreviewApp owns the class on a
// wrapper), so reading getComputedStyle here is acceptable.
function cssVar(name: string, fallback: string): string {
  if (typeof window === "undefined") return fallback;
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
}

const folderColor = computed(() => cssVar("--ed-folder", "#dcb67a"));
const chevronColor = computed(() => cssVar("--ed-chevron", "rgba(204, 204, 204, 0.7)"));

const dirChevron = computed(() => (expanded.value ? ChevronDown : ChevronRight));
const dirIcon = computed(() => (expanded.value ? FolderOpen : Folder));
const fileIcon = computed(() => fileIconFor(props.entry.name));

function onClick() {
  emit("select", fullPath.value);
  if (props.entry.isDir) expanded.value = !expanded.value;
}

function onDblClick() {
  if (props.entry.isDir) return;
  emit("dblclick", fullPath.value);
}

function onChildSelect(p: string) {
  emit("select", p);
}

function onChildDblclick(p: string) {
  emit("dblclick", p);
}
</script>

<template>
  <div class="tree-node">
    <div
      class="row"
      :class="{ selected: isSelected, dir: entry.isDir, file: !entry.isDir }"
      :title="fullPath"
      :style="{ paddingLeft: `${level * 8 + 4}px` }"
      @click="onClick"
      @dblclick="onDblClick"
    >
      <span class="twisty">
        <component
          v-if="entry.isDir"
          :is="dirChevron"
          :size="14"
          :color="chevronColor"
          :stroke-width="2"
        />
      </span>
      <span class="icon">
        <component
          v-if="entry.isDir"
          :is="dirIcon"
          :size="16"
          :color="folderColor"
          :stroke-width="1.5"
        />
        <component
          v-else
          :is="fileIcon.comp"
          :size="16"
          :color="fileIcon.color"
          :stroke-width="1.5"
          :key="`${entry.name}-${level}`"
        />
      </span>
      <span class="name">{{ entry.name }}</span>
    </div>
    <div
      v-if="entry.isDir && expanded && entry.children"
      class="children"
      :style="{ '--indent-base': `${level * 8 + 12}px` }"
    >
      <FileTreeNode
        v-for="(c, i) in sortedChildren(entry.children)"
        :key="c.name + i"
        :entry="c"
        :level="level + 1"
        :selected-path="selectedPath"
        :parent-path="fullPath"
        @select="onChildSelect"
        @dblclick="onChildDblclick"
      />
    </div>
  </div>
</template>

<style scoped>
.tree-node {
  display: block;
}
.row {
  display: flex;
  align-items: center;
  height: 22px;
  padding-right: 8px;
  cursor: pointer;
  font-size: 13px;
  line-height: 22px;
  white-space: nowrap;
  color: var(--ed-row-fg, #cccccc);
  user-select: none;
}
.row:hover {
  background: var(--ed-row-hover, #2a2d2e);
}
.row.selected {
  background: var(--ed-row-selected, #37373d);
}

.twisty {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 16px;
  width: 16px;
  height: 100%;
}
.icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 20px;
  width: 20px;
  margin-right: 4px;
}
.name {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.children {
  position: relative;
}
.children::before {
  content: "";
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: var(--ed-indent-guide, rgba(204, 204, 204, 0.1));
  left: var(--indent-base, 12px);
  pointer-events: none;
}
</style>
