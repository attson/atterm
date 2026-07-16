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
const watchHandles = new Map<string, { fs: FileSystemBridge; id: number | string }>();
const pendingExpands = new Map<string, number>();
const watchGenerations = new Map<string, number>();
const refreshGenerations = new Map<string, number>();
let disposed = false;
let generation = 0;
let offDirChanged: () => void = () => {};

function isCurrent(fs: FileSystemBridge, root: string, showHidden: boolean, request: number): boolean {
  return !disposed
    && generation === request
    && props.fs === fs
    && props.root === root
    && props.showHidden === showHidden;
}

async function loadDir(fs: FileSystemBridge, path: string, showHidden: boolean): Promise<TreeNode[]> {
  const entries = (await fs.listDir(path)) as DirEntry[];
  return entries
    .filter((e) => showHidden || !e.name.startsWith("."))
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

async function unwatch(fs: FileSystemBridge, id: number | string): Promise<void> {
  try { await fs.unwatchDir(id); } catch { /* ignore */ }
}

function releaseWatches() {
  const handles = Array.from(watchHandles.values());
  watchHandles.clear();
  for (const { fs, id } of handles) void unwatch(fs, id);
}

function isDescendant(path: string, parent: string): boolean {
  return path.startsWith(parent.endsWith("/") ? parent : `${parent}/`);
}

function releaseDescendantState(parent: string) {
  for (const [path, handle] of Array.from(watchHandles)) {
    if (!isDescendant(path, parent)) continue;
    watchHandles.delete(path);
    void unwatch(handle.fs, handle.id);
  }
  for (const path of Array.from(pendingExpands.keys())) {
    if (isDescendant(path, parent)) pendingExpands.delete(path);
  }
  for (const path of Array.from(watchGenerations.keys())) {
    if (isDescendant(path, parent)) advanceWatchGeneration(path);
  }
  for (const path of Array.from(refreshGenerations.keys())) {
    if (isDescendant(path, parent)) advanceRefreshGeneration(path);
  }
}

function collapseDescendants(node: TreeNode) {
  for (const child of node.children ?? []) {
    child.expanded = false;
    collapseDescendants(child);
  }
}

function advanceWatchGeneration(path: string): number {
  const next = (watchGenerations.get(path) ?? 0) + 1;
  watchGenerations.set(path, next);
  return next;
}

function advanceRefreshGeneration(path: string): number {
  const next = (refreshGenerations.get(path) ?? 0) + 1;
  refreshGenerations.set(path, next);
  return next;
}

function stopCurrentGeneration() {
  generation++;
  offDirChanged();
  offDirChanged = () => {};
  pendingExpands.clear();
  watchGenerations.clear();
  refreshGenerations.clear();
  releaseWatches();
}

async function refreshRoot(fs: FileSystemBridge, root: string, showHidden: boolean, request: number) {
  const refreshRequest = advanceRefreshGeneration(root);
  const nodes = await loadDir(fs, root, showHidden);
  if (!isCurrent(fs, root, showHidden, request) || refreshGenerations.get(root) !== refreshRequest) return;
  releaseDescendantState(root);
  rootNodes.value = nodes;
}

async function refreshChanged(
  fs: FileSystemBridge,
  root: string,
  showHidden: boolean,
  request: number,
  dir: string,
) {
  if (!isCurrent(fs, root, showHidden, request)) return;
  if (dir === root) {
    await refreshRoot(fs, root, showHidden, request);
    return;
  }
  const node = findNode(rootNodes.value, dir);
  if (!node || !node.expanded) return;
  const refreshRequest = advanceRefreshGeneration(node.path);
  const children = await loadDir(fs, node.path, showHidden);
  if (
    !isCurrent(fs, root, showHidden, request)
    || !node.expanded
    || findNode(rootNodes.value, dir) !== node
    || refreshGenerations.get(node.path) !== refreshRequest
  ) return;
  releaseDescendantState(node.path);
  node.children = children;
}

function startGeneration() {
  stopCurrentGeneration();
  const fs = props.fs;
  const root = props.root;
  const showHidden = props.showHidden;
  const request = generation;
  rootNodes.value = [];
  offDirChanged = fs.onDirChanged((dir) => {
    void refreshChanged(fs, root, showHidden, request, dir).catch(() => {});
  });
  void refreshRoot(fs, root, showHidden, request).catch(() => {});
}

watch(() => [props.root, props.fs, props.showHidden], startGeneration);

onMounted(() => {
  startGeneration();
});

async function toggle(n: TreeNode) {
  if (!n.isDir) return;
  const fs = props.fs;
  const root = props.root;
  const showHidden = props.showHidden;
  const request = generation;
  if (!isCurrentNode(fs, root, showHidden, request, n)) return;
  selectedPath.value = n.path;
  if (!n.expanded) {
    if (pendingExpands.has(n.path)) {
      advanceWatchGeneration(n.path);
      advanceRefreshGeneration(n.path);
      pendingExpands.delete(n.path);
      return;
    }
    const watchRequest = advanceWatchGeneration(n.path);
    pendingExpands.set(n.path, watchRequest);
    try {
      if (n.children === null) {
        const refreshRequest = advanceRefreshGeneration(n.path);
        const children = await loadDir(fs, n.path, showHidden);
        if (
          !isCurrentNode(fs, root, showHidden, request, n)
          || watchGenerations.get(n.path) !== watchRequest
          || refreshGenerations.get(n.path) !== refreshRequest
        ) return;
        n.children = children;
      }
      if (!isCurrentNode(fs, root, showHidden, request, n) || watchGenerations.get(n.path) !== watchRequest) return;
      n.expanded = true;
      const id = await fs.watchDir(n.path);
      if (
        !isCurrentNode(fs, root, showHidden, request, n)
        || !n.expanded
        || watchGenerations.get(n.path) !== watchRequest
      ) {
        await unwatch(fs, id);
        return;
      }
      const previous = watchHandles.get(n.path);
      if (previous) {
        watchHandles.delete(n.path);
        await unwatch(previous.fs, previous.id);
        if (!isCurrentNode(fs, root, showHidden, request, n) || watchGenerations.get(n.path) !== watchRequest) {
          await unwatch(fs, id);
          return;
        }
      }
      watchHandles.set(n.path, { fs, id });
    } catch (err) {
      if (isCurrentNode(fs, root, showHidden, request, n)) {
        console.warn("plugin-fs: watcher unavailable or cap reached for", n.path, err);
      }
    } finally {
      if (pendingExpands.get(n.path) === watchRequest) pendingExpands.delete(n.path);
    }
  } else {
    advanceWatchGeneration(n.path);
    pendingExpands.delete(n.path);
    releaseDescendantState(n.path);
    collapseDescendants(n);
    n.expanded = false;
    const handle = watchHandles.get(n.path);
    if (handle) {
      watchHandles.delete(n.path);
      await unwatch(handle.fs, handle.id);
      if (!isCurrentNode(fs, root, showHidden, request, n)) return;
    }
  }
  if (isCurrentNode(fs, root, showHidden, request, n)) emit("dir-toggled", n.path, n.expanded);
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

function isCurrentNode(
  fs: FileSystemBridge,
  root: string,
  showHidden: boolean,
  request: number,
  node: TreeNode,
): boolean {
  return isCurrent(fs, root, showHidden, request) && findNode(rootNodes.value, node.path) === node;
}

onBeforeUnmount(() => {
  disposed = true;
  stopCurrentGeneration();
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

defineExpose({ refresh: startGeneration });
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
