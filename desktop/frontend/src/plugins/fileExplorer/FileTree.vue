<script lang="ts" setup>
import { ref, watch, onMounted, onBeforeUnmount } from "vue";
import { ListDir, WatchDir, UnwatchDir } from "../../../wailsjs/go/main/PluginFS";
import { EventsOn } from "../../../wailsjs/runtime/runtime";

interface DirEntry {
  name: string;
  isDir: boolean;
  size?: number;
  modTime?: number;
}

interface TreeNode {
  path: string;
  name: string;
  isDir: boolean;
  expanded: boolean;
  children: TreeNode[] | null;
}

const props = defineProps<{
  root: string;
  showHidden: boolean;
}>();

const emit = defineEmits<{
  (e: "file-clicked", path: string): void;
  (e: "file-double-clicked", path: string): void;
  (e: "dir-toggled", path: string, expanded: boolean): void;
}>();

const rootNodes = ref<TreeNode[]>([]);
const selectedPath = ref<string>("");
const watchHandles = new Map<string, number>();

async function loadDir(path: string): Promise<TreeNode[]> {
  const entries = (await ListDir(path)) as DirEntry[];
  return entries
    .filter((e) => props.showHidden || !e.name.startsWith("."))
    .map((e) => ({
      path: joinPath(path, e.name),
      name: e.name,
      isDir: e.isDir,
      expanded: false,
      children: null,
    }))
    .sort((a, b) => (a.isDir === b.isDir ? a.name.localeCompare(b.name) : a.isDir ? -1 : 1));
}

function joinPath(parent: string, name: string): string {
  return parent.endsWith("/") ? parent + name : parent + "/" + name;
}

async function refreshRoot() {
  rootNodes.value = await loadDir(props.root);
}

watch(
  () => [props.root, props.showHidden],
  () => {
    void refreshRoot();
  },
);

onMounted(() => {
  void refreshRoot();
});

async function toggle(n: TreeNode) {
  if (!n.isDir) return;
  selectedPath.value = n.path;
  if (!n.expanded) {
    if (n.children === null) n.children = await loadDir(n.path);
    n.expanded = true;
    try {
      const id = (await WatchDir(n.path)) as number;
      watchHandles.set(n.path, id);
    } catch (err) {
      console.warn("plugin-fs: watcher unavailable or cap reached for", n.path, err);
    }
  } else {
    const id = watchHandles.get(n.path);
    if (id) {
      await UnwatchDir(id);
      watchHandles.delete(n.path);
    }
    n.expanded = false;
  }
  emit("dir-toggled", n.path, n.expanded);
}

function findNode(nodes: TreeNode[], path: string): TreeNode | null {
  for (const n of nodes) {
    if (n.path === path) return n;
    if (n.children) {
      const sub = findNode(n.children, path);
      if (sub) return sub;
    }
  }
  return null;
}

const off = EventsOn("plugin-fs:dir-changed", async (dir: string) => {
  if (dir === props.root) {
    rootNodes.value = await loadDir(props.root);
    return;
  }
  const node = findNode(rootNodes.value, dir);
  if (node && node.expanded) {
    node.children = await loadDir(node.path);
  }
});

onBeforeUnmount(async () => {
  for (const id of watchHandles.values()) {
    try { await UnwatchDir(id); } catch { /* ignore */ }
  }
  watchHandles.clear();
  off();
});

function clickFile(n: TreeNode) {
  if (n.isDir) return;
  selectedPath.value = n.path;
  emit("file-clicked", n.path);
}

function dblClickFile(n: TreeNode) {
  if (n.isDir) return;
  selectedPath.value = n.path;
  emit("file-double-clicked", n.path);
}

defineExpose({ refresh: refreshRoot });
</script>

<template>
  <ul class="tree-list">
    <li v-for="n in rootNodes" :key="n.path">
      <FileTreeNode
        :node="n"
        :level="0"
        :selected-path="selectedPath"
        @toggle="toggle"
        @click-file="clickFile"
        @dblclick-file="dblClickFile"
      />
    </li>
  </ul>
</template>

<script lang="ts">
import { defineComponent, h, computed, type PropType } from "vue";
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

const INDENT_PX = 16;
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

export const FileTreeNode = defineComponent({
  name: "FileTreeNode",
  props: {
    node: { type: Object as PropType<any>, required: true },
    level: { type: Number, required: true },
    selectedPath: { type: String, default: "" },
  },
  emits: ["toggle", "click-file", "dblclick-file"],
  setup(props, { emit }) {
    const folderColor = computed(() => cssVar("--ed-folder", "#d4a14a"));
    const chevronColor = computed(() => cssVar("--ed-chevron", "rgba(204,204,204,0.7)"));

    return () => {
      const selected = props.selectedPath === props.node.path;
      const isDir = props.node.isDir as boolean;
      const expanded = props.node.expanded as boolean;
      const chevronComp = isDir ? (expanded ? ChevronDown : ChevronRight) : null;
      const dirComp = isDir ? (expanded ? FolderOpen : Folder) : null;
      const file = !isDir ? fileIconFor(props.node.name) : null;

      return h("div", { class: "node-wrap" }, [
        h(
          "div",
          {
            class: ["node", { selected, "is-dir": isDir, "is-file": !isDir }],
            "data-type": isDir ? "dir" : "file",
            style: {
              paddingLeft: `${props.level * INDENT_PX}px`,
              height: `${ROW_HEIGHT}px`,
              lineHeight: `${ROW_HEIGHT}px`,
            },
            title: props.node.path,
            onClick: () =>
              isDir ? emit("toggle", props.node) : emit("click-file", props.node),
            onDblclick: () => (!isDir ? emit("dblclick-file", props.node) : null),
          },
          [
            h(
              "span",
              { class: "twisty" },
              chevronComp
                ? [h(chevronComp, { size: 14, color: chevronColor.value, strokeWidth: 2 })]
                : [],
            ),
            h(
              "span",
              { class: "icon" },
              isDir
                ? [h(dirComp as any, { size: 16, color: folderColor.value, strokeWidth: 1.5 })]
                : [h((file as any).comp, { size: 16, color: (file as any).color, strokeWidth: 1.5 })],
            ),
            h("span", { class: "node-name" }, props.node.name),
          ],
        ),
        isDir && expanded && props.node.children
          ? h(
              "ul",
              {
                class: "tree-list",
                style: { "--indent-base": `${props.level * INDENT_PX + 12}px` },
              },
              props.node.children.map((c: any) =>
                h(
                  "li",
                  { key: c.path },
                  h(FileTreeNode, {
                    node: c,
                    level: props.level + 1,
                    selectedPath: props.selectedPath,
                    onToggle: (n: any) => emit("toggle", n),
                    "onClick-file": (n: any) => emit("click-file", n),
                    "onDblclick-file": (n: any) => emit("dblclick-file", n),
                  }),
                ),
              ),
            )
          : null,
      ]);
    };
  },
});

export default defineComponent({ name: "FileTree" });
</script>

<style scoped>
.tree-list {
  list-style: none;
  margin: 0;
  padding: 0;
  position: relative;
}
.tree-list ul.tree-list::before {
  content: "";
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: var(--ed-indent-guide, rgba(204, 204, 204, 0.1));
  left: var(--indent-base, 12px);
  pointer-events: none;
}
.tree-list > li {
  display: block;
}
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
.node:hover {
  background: var(--ed-row-hover, rgba(255, 255, 255, 0.05));
}
.node.selected {
  background: var(--ed-row-selected, #37373d);
}
.twisty {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 14px;
  width: 14px;
  height: 100%;
  color: var(--ed-chevron, rgba(204, 204, 204, 0.7));
  font-size: 10px;
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
