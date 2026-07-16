<script lang="ts" setup>
import { ref, watch, onMounted, onBeforeUnmount } from "vue";
import FileTreeNode from "./FileTreeNode.vue";
import type { FileSystemBridge } from "./fsBridge";

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
  fs: FileSystemBridge;
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
const watchHandles = new Map<string, number | string>();

async function loadDir(path: string): Promise<TreeNode[]> {
  const entries = (await props.fs.listDir(path)) as DirEntry[];
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
      const id = await props.fs.watchDir(n.path);
      watchHandles.set(n.path, id);
    } catch (err) {
      console.warn("plugin-fs: watcher unavailable or cap reached for", n.path, err);
    }
  } else {
    const id = watchHandles.get(n.path);
    if (id !== undefined) {
      await props.fs.unwatchDir(id);
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

const off = props.fs.onDirChanged(async (dir) => {
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
    try { await props.fs.unwatchDir(id); } catch { /* ignore */ }
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
  <ul class="tree-root">
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

<style scoped>
.tree-root {
  list-style: none;
  margin: 0;
  padding: 0;
}
.tree-root > li { display: block; }
</style>
