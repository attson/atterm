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

interface Node {
  path: string;
  name: string;
  isDir: boolean;
  expanded: boolean;
  children: Node[] | null; // null = not yet loaded
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

const rootNodes = ref<Node[]>([]);
const selectedPath = ref<string | null>(null);
const watchHandles = new Map<string, number>();

async function loadDir(path: string): Promise<Node[]> {
  const entries = (await ListDir(path)) as DirEntry[];
  const nodes: Node[] = entries
    .filter((e) => props.showHidden || !e.name.startsWith("."))
    .map((e) => ({
      path: joinPath(path, e.name),
      name: e.name,
      isDir: e.isDir,
      expanded: false,
      children: null,
    }))
    .sort((a, b) => (a.isDir === b.isDir ? a.name.localeCompare(b.name) : a.isDir ? -1 : 1));
  return nodes;
}

function joinPath(parent: string, name: string): string {
  return parent.endsWith("/") ? parent + name : parent + "/" + name;
}

async function refreshRoot() {
  rootNodes.value = await loadDir(props.root);
}

watch(() => [props.root, props.showHidden], () => {
  void refreshRoot();
});

onMounted(() => {
  void refreshRoot();
});

async function toggle(n: Node) {
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

function findNode(nodes: Node[], path: string): Node | null {
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

function clickFile(n: Node) {
  if (n.isDir) return;
  selectedPath.value = n.path;
  emit("file-clicked", n.path);
}

function dblClickFile(n: Node) {
  if (n.isDir) return;
  selectedPath.value = n.path;
  emit("file-double-clicked", n.path);
}

defineExpose({ refresh: refreshRoot });
</script>

<template>
  <ul class="tree-list">
    <li v-for="n in rootNodes" :key="n.path">
      <NodeRow
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
import { defineComponent, h, PropType } from "vue";

const INDENT_PX = 16;
const ROW_HEIGHT = 22;

export const NodeRow = defineComponent({
  name: "NodeRow",
  props: {
    node: { type: Object as PropType<any>, required: true },
    level: { type: Number, required: true },
    selectedPath: { type: String as PropType<string | null>, default: null },
  },
  emits: ["toggle", "click-file", "dblclick-file"],
  setup(props, { emit }) {
    return () => {
      const selected = props.selectedPath === props.node.path;
      const twistyChar = props.node.isDir ? (props.node.expanded ? "▾" : "▸") : "";
      const iconChar = props.node.isDir ? (props.node.expanded ? "📂" : "📁") : "📄";
      return h("div", { class: "node-wrap" }, [
        h(
          "div",
          {
            class: ["node", { selected, "is-dir": props.node.isDir, "is-file": !props.node.isDir }],
            "data-type": props.node.isDir ? "dir" : "file",
            style: { paddingLeft: `${props.level * INDENT_PX}px`, height: `${ROW_HEIGHT}px`, lineHeight: `${ROW_HEIGHT}px` },
            title: props.node.path,
            onClick: () => (props.node.isDir ? emit("toggle", props.node) : emit("click-file", props.node)),
            onDblclick: () => (!props.node.isDir ? emit("dblclick-file", props.node) : null),
          },
          [
            h("span", { class: "twisty" }, twistyChar),
            h("span", { class: "icon" }, iconChar),
            h("span", { class: "node-name" }, props.node.name),
          ],
        ),
        props.node.expanded && props.node.children
          ? h(
              "ul",
              { class: "tree-list" },
              props.node.children.map((c: any) =>
                h(
                  "li",
                  { key: c.path },
                  h(NodeRow, {
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
</script>

<style scoped>
.tree-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.tree-list > li {
  display: block;
}

.node {
  display: flex;
  align-items: center;
  padding-right: 6px;
  cursor: default;
  font-size: 12px;
  white-space: nowrap;
  color: #bcc0c4;
  user-select: none;
}
.node:hover {
  background: rgba(255, 255, 255, 0.06);
}
.node.selected {
  background: #2b4769;
  color: #f7f9fb;
}
.node.selected:hover {
  background: #2b4769;
}

.twisty {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 14px;
  width: 14px;
  height: 100%;
  color: #8b949e;
  font-size: 10px;
}
.icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 18px;
  width: 18px;
  margin-right: 4px;
  font-size: 11px;
  filter: grayscale(0.4);
}
.node-name {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
