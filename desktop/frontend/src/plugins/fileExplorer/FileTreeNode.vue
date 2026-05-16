<script lang="ts">
import { defineComponent } from "vue";
// Self-name for recursive template usage.
export default defineComponent({ name: "FileTreeNode" });
</script>

<script lang="ts" setup>
import { computed } from "vue";
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

interface TreeNode {
  path: string;
  name: string;
  isDir: boolean;
  expanded: boolean;
  children: TreeNode[] | null;
}

const props = defineProps<{
  node: TreeNode;
  level: number;
  selectedPath: string;
}>();

const emit = defineEmits<{
  (e: "toggle", n: TreeNode): void;
  (e: "click-file", n: TreeNode): void;
  (e: "dblclick-file", n: TreeNode): void;
}>();

const INDENT_PX = 8;
const ROW_HEIGHT = 22;

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
    const entry = fileIconMap[m[1].toLowerCase()];
    if (entry) return entry;
  }
  return { comp: File, color: "currentColor" };
}

function cssVar(name: string, fallback: string): string {
  if (typeof window === "undefined") return fallback;
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
}

const folderColor = computed(() => cssVar("--ed-folder", "#d4a14a"));
const chevronColor = computed(() => cssVar("--ed-chevron", "rgba(173,186,199,0.7)"));

const isSelected = computed(() => props.selectedPath === props.node.path);
const dirChevron = computed(() => (props.node.expanded ? ChevronDown : ChevronRight));
const dirIcon = computed(() => (props.node.expanded ? FolderOpen : Folder));
const fileIcon = computed(() => fileIconFor(props.node.name));

function onClick() {
  if (props.node.isDir) emit("toggle", props.node);
  else emit("click-file", props.node);
}

function onDblClick() {
  if (!props.node.isDir) emit("dblclick-file", props.node);
}
</script>

<template>
  <div class="node-wrap">
    <div
      class="node"
      :class="{ selected: isSelected, 'is-dir': node.isDir, 'is-file': !node.isDir }"
      :data-type="node.isDir ? 'dir' : 'file'"
      :style="{
        paddingLeft: `${level * INDENT_PX}px`,
        height: `${ROW_HEIGHT}px`,
        lineHeight: `${ROW_HEIGHT}px`,
      }"
      :title="node.path"
      @click="onClick"
      @dblclick="onDblClick"
    >
      <span class="twisty">
        <component
          v-if="node.isDir"
          :is="dirChevron"
          :size="14"
          :color="chevronColor"
          :stroke-width="2"
        />
      </span>
      <span class="icon">
        <component
          v-if="node.isDir"
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
        />
      </span>
      <span class="node-name">{{ node.name }}</span>
    </div>
    <ul
      v-if="node.isDir && node.expanded && node.children"
      class="tree-list"
      :style="{ '--indent-base': `${level * INDENT_PX + 12}px` }"
    >
      <li v-for="c in node.children" :key="c.path">
        <FileTreeNode
          :node="c"
          :level="level + 1"
          :selected-path="selectedPath"
          @toggle="(n: TreeNode) => emit('toggle', n)"
          @click-file="(n: TreeNode) => emit('click-file', n)"
          @dblclick-file="(n: TreeNode) => emit('dblclick-file', n)"
        />
      </li>
    </ul>
  </div>
</template>

<style scoped>
.node-wrap { display: block; }
.tree-list {
  list-style: none;
  margin: 0;
  padding: 0;
  position: relative;
}
.tree-list::before {
  content: "";
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: var(--ed-indent-guide, rgba(204, 204, 204, 0.1));
  left: var(--indent-base, 12px);
  pointer-events: none;
}
.tree-list > li { display: block; }
.node {
  display: flex;
  align-items: center;
  padding-right: 6px;
  cursor: pointer;
  font-size: 13px;
  white-space: nowrap;
  color: var(--ed-row-fg, #cccccc);
  user-select: none;
}
.node:hover { background: var(--ed-row-hover, rgba(255, 255, 255, 0.05)); }
.node.selected { background: var(--ed-row-selected, #37373d); }
.twisty {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 14px;
  width: 14px;
  height: 100%;
  color: var(--ed-chevron, rgba(204, 204, 204, 0.7));
}
.icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 20px;
  width: 20px;
  margin-right: 4px;
}
.node-name {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
