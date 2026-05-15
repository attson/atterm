<script lang="ts" setup>
import { ref, watch, onMounted } from "vue";
import { ListDir } from "../../../wailsjs/go/main/PluginFS";

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
  if (!n.expanded) {
    if (n.children === null) n.children = await loadDir(n.path);
    n.expanded = true;
  } else {
    n.expanded = false;
  }
  emit("dir-toggled", n.path, n.expanded);
}

function clickFile(n: Node) {
  if (n.isDir) return;
  emit("file-clicked", n.path);
}

function dblClickFile(n: Node) {
  if (n.isDir) return;
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
        @toggle="toggle"
        @click-file="clickFile"
        @dblclick-file="dblClickFile"
      />
    </li>
  </ul>
</template>

<script lang="ts">
import { defineComponent, h, PropType } from "vue";
export const NodeRow = defineComponent({
  name: "NodeRow",
  props: {
    node: { type: Object as PropType<any>, required: true },
    level: { type: Number, required: true },
  },
  emits: ["toggle", "click-file", "dblclick-file"],
  setup(props, { emit }) {
    return () =>
      h("div", { class: "node-wrap" }, [
        h(
          "div",
          {
            class: "node",
            "data-type": props.node.isDir ? "dir" : "file",
            style: { paddingLeft: `${props.level * 12}px` },
            onClick: () => (props.node.isDir ? emit("toggle", props.node) : emit("click-file", props.node)),
            onDblclick: () => (!props.node.isDir ? emit("dblclick-file", props.node) : null),
          },
          [
            h("span", { class: "twisty" }, props.node.isDir ? (props.node.expanded ? "▾" : "▸") : ""),
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
                    onToggle: (n: any) => emit("toggle", n),
                    "onClick-file": (n: any) => emit("click-file", n),
                    "onDblclick-file": (n: any) => emit("dblclick-file", n),
                  }),
                ),
              ),
            )
          : null,
      ]);
  },
});
</script>

<style scoped>
.tree-list { list-style: none; margin: 0; padding: 0; }
.node { display: flex; align-items: center; padding: 1px 4px; cursor: default; font-size: 12px; }
.node:hover { background: #21262d; }
.twisty { display: inline-block; width: 14px; color: #8b949e; }
.node-name { flex: 1; color: #c9d1d9; user-select: none; }
</style>
