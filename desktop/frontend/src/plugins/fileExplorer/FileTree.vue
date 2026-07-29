<script lang="ts" setup>
import { computed, ref, watch, onMounted, onBeforeUnmount } from "vue";
import FileTreeNode from "./FileTreeNode.vue";
import ConfirmDialog from "./ConfirmDialog.vue";
import type { FileSystemBridge } from "./fsBridge";
import { searchFileNames, type FileNameSearchResult } from "./fileNameSearch";
import { useI18n } from "../../i18n/useI18n";

const { t } = useI18n();

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
  searchQuery?: string;
}>();

const emit = defineEmits<{
  (e: "file-clicked", path: string): void;
  (e: "file-double-clicked", path: string): void;
  (e: "dir-toggled", path: string, expanded: boolean): void;
}>();

type ContextMenuAnchor = { x: number; y: number; node: TreeNode; level: number; shift: boolean };
const menu = ref<ContextMenuAnchor | null>(null);

type InlineIntent =
  | { kind: "newFile"; parentPath: string; parentLevel: number }
  | { kind: "newFolder"; parentPath: string; parentLevel: number }
  | { kind: "rename"; node: TreeNode; level: number };
const inlineIntent = ref<InlineIntent | null>(null);

type DeleteConfirmSpec = { node: TreeNode; mode: "trash" | "hard" };
const deleteConfirm = ref<DeleteConfirmSpec | null>(null);

function parentDir(p: string): string {
  const i = p.lastIndexOf("/");
  return i <= 0 ? "/" : p.slice(0, i);
}

function baseName(p: string): string {
  const i = p.lastIndexOf("/");
  return i === -1 ? p : p.slice(i + 1);
}

function openMenuFromNode(ev: MouseEvent, node: TreeNode, level: number) {
  ev.preventDefault();
  menu.value = { x: ev.clientX, y: ev.clientY, node, level, shift: ev.shiftKey };
}

function closeMenu() { menu.value = null; }

async function onMenuAction(action: "newFile" | "newFolder" | "rename" | "delete") {
  const anchor = menu.value;
  menu.value = null;
  if (!anchor) return;
  const node = anchor.node;
  if (action === "newFile" || action === "newFolder") {
    const parentPath = node.isDir ? node.path : parentDir(node.path);
    const parentLevel = node.isDir ? anchor.level + 1 : anchor.level;
    inlineIntent.value = { kind: action, parentPath, parentLevel };
    return;
  }
  if (action === "rename") {
    inlineIntent.value = { kind: "rename", node, level: anchor.level };
    return;
  }
  if (action === "delete") {
    deleteConfirm.value = { node, mode: anchor.shift ? "hard" : "trash" };
  }
}

async function submitInline(name: string) {
  const intent = inlineIntent.value;
  inlineIntent.value = null;
  if (!intent) return;
  try {
    if (intent.kind === "newFile") await props.fs.createFile(joinPath(intent.parentPath, name));
    else if (intent.kind === "newFolder") await props.fs.mkdir(joinPath(intent.parentPath, name));
    else if (intent.kind === "rename") await props.fs.rename(intent.node.path, joinPath(parentDir(intent.node.path), name));
  } catch (err) {
    console.warn("file-explorer: inline action failed", err);
  }
}

function cancelInline() { inlineIntent.value = null; }

async function resolveDeleteConfirm(id: string) {
  const conf = deleteConfirm.value;
  deleteConfirm.value = null;
  if (!conf || id === "cancel") return;
  try {
    if (id === "hard") {
      await props.fs.remove(conf.node.path, conf.node.isDir);
    } else {
      try {
        await props.fs.trash(conf.node.path);
      } catch (err) {
        const msg = (err as Error).message ?? "";
        if (msg.includes("no platform trash command available")) {
          // Re-prompt with the hard-delete confirmation.
          deleteConfirm.value = { node: conf.node, mode: "hard" };
        } else {
          throw err;
        }
      }
    }
  } catch (err) {
    console.warn("file-explorer: delete failed", err);
  }
}

function deleteButtons(mode: "trash" | "hard") {
  const primary = mode === "hard"
    ? { id: "hard", label: t("plugins.fileExplorer.delete"), kind: "danger" as const }
    : { id: "trash", label: t("plugins.fileExplorer.moveToTrash"), kind: "primary" as const };
  return [
    primary,
    { id: "cancel", label: t("plugins.fileExplorer.cancel"), kind: "secondary" as const },
  ];
}

const rootNodes = ref<TreeNode[]>([]);
const selectedPath = ref<string>("");
const searchResults = ref<FileNameSearchResult[]>([]);
const searchLoading = ref(false);
const searchTruncated = ref(false);
const watchHandles = new Map<string, { fs: FileSystemBridge; id: number | string }>();
const pendingExpands = new Map<string, number>();
const watchGenerations = new Map<string, number>();
const refreshGenerations = new Map<string, number>();
let disposed = false;
let generation = 0;
let searchGeneration = 0;
let offDirChanged: () => void = () => {};

const normalizedSearchQuery = computed(() => (props.searchQuery ?? "").trim());

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

watch(
  () => [props.root, props.fs, props.showHidden, normalizedSearchQuery.value] as const,
  async ([root, fs, showHidden, query]) => {
    const request = ++searchGeneration;
    searchResults.value = [];
    searchTruncated.value = false;
    if (!query) {
      searchLoading.value = false;
      return;
    }
    searchLoading.value = true;
    try {
      const result = await searchFileNames(fs, root, query, { showHidden });
      if (disposed || searchGeneration !== request) return;
      searchResults.value = result.results;
      searchTruncated.value = result.truncated;
    } catch (err) {
      if (disposed || searchGeneration !== request) return;
      console.warn("file-explorer: filename search failed", err);
      searchResults.value = [];
      searchTruncated.value = false;
    } finally {
      if (!disposed && searchGeneration === request) searchLoading.value = false;
    }
  },
  { immediate: true },
);

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
  searchGeneration++;
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

function clickSearchResult(result: FileNameSearchResult) {
  selectedPath.value = result.path;
  emit("file-clicked", result.path);
}

function dblClickSearchResult(result: FileNameSearchResult) {
  selectedPath.value = result.path;
  emit("file-double-clicked", result.path);
}

defineExpose({ refresh: startGeneration });
</script>

<template>
  <div class="tree-wrap" @click="closeMenu">
    <div v-if="normalizedSearchQuery" class="search-results" data-test="file-search-results">
      <div v-if="searchLoading" class="search-status">{{ t("common.loading") }}</div>
      <div v-else-if="searchResults.length === 0" class="search-status">
        {{ t("plugins.fileExplorer.noSearchResults") }}
      </div>
      <button
        v-for="result in searchResults"
        :key="result.path"
        class="search-result"
        data-test="file-search-result"
        :title="result.path"
        @click.stop="clickSearchResult(result)"
        @dblclick.stop="dblClickSearchResult(result)"
      >
        <span class="search-name">{{ result.name }}</span>
        <span class="search-path">{{ result.path }}</span>
      </button>
      <div v-if="searchTruncated" class="search-status">
        {{ t("plugins.fileExplorer.searchTruncated") }}
      </div>
    </div>
    <ul v-else class="tree-root">
      <li v-for="n in rootNodes" :key="n.path">
        <FileTreeNode
          :node="n"
          :level="0"
          :selected-path="selectedPath"
          :inline-intent="inlineIntent"
          @toggle="toggle"
          @click-file="clickFile"
          @dblclick-file="dblClickFile"
          @context="openMenuFromNode"
          @inline-submit="submitInline"
          @inline-cancel="cancelInline"
        />
      </li>
    </ul>
    <div
      v-if="menu"
      class="ctx-menu"
      data-test="file-tree-menu"
      :style="{ top: menu.y + 'px', left: menu.x + 'px' }"
      @click.stop
    >
      <button data-test="menu-new-file" @click="onMenuAction('newFile')">{{ t("plugins.fileExplorer.newFile") }}</button>
      <button data-test="menu-new-folder" @click="onMenuAction('newFolder')">{{ t("plugins.fileExplorer.newFolder") }}</button>
      <button data-test="menu-rename" @click="onMenuAction('rename')">{{ t("plugins.fileExplorer.rename") }}</button>
      <button data-test="menu-delete" @click="onMenuAction('delete')">{{ t("plugins.fileExplorer.delete") }}</button>
    </div>
    <ConfirmDialog
      v-if="deleteConfirm"
      :title="deleteConfirm.mode === 'hard'
        ? t('plugins.fileExplorer.confirmHardDelete', { name: baseName(deleteConfirm.node.path) })
        : t('plugins.fileExplorer.confirmTrash', { name: baseName(deleteConfirm.node.path) })"
      :buttons="deleteButtons(deleteConfirm.mode)"
      @resolve="resolveDeleteConfirm"
    />
  </div>
</template>

<style scoped>
.tree-wrap { position: relative; height: 100%; }
.search-results {
  display: flex;
  flex-direction: column;
  gap: 1px;
  padding: 2px 0;
}
.search-result {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 1px;
  min-width: 0;
  padding: 5px 10px;
  border: 0;
  background: transparent;
  color: var(--ed-row-fg, #cccccc);
  text-align: left;
  cursor: pointer;
}
.search-result:hover { background: var(--ed-row-hover, rgba(255, 255, 255, 0.05)); }
.search-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}
.search-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--ed-muted, rgba(173, 186, 199, 0.55));
  font-size: 11px;
}
.search-status {
  padding: 8px 10px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.55));
  font-size: 12px;
}
.tree-root {
  list-style: none;
  margin: 0;
  padding: 0;
}
.tree-root > li { display: block; }
.ctx-menu {
  position: fixed;
  z-index: 60;
  background: var(--ed-shell-bg, #22272e);
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 4px;
  padding: 4px 0;
  min-width: 140px;
  box-shadow: 0 4px 12px rgba(0,0,0,0.3);
  display: flex;
  flex-direction: column;
}
.ctx-menu button {
  background: none;
  border: none;
  color: var(--ed-row-fg, #adbac7);
  font: inherit;
  font-size: 12px;
  padding: 6px 14px;
  text-align: left;
  cursor: pointer;
}
.ctx-menu button:hover { background: var(--ed-row-hover, rgba(255,255,255,0.06)); }
</style>
