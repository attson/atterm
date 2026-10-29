<script lang="ts" setup>
import { errText, logWarn } from "../../lib/log";
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick } from "vue";
import FileTreeNode from "./FileTreeNode.vue";
import ConfirmDialog from "./ConfirmDialog.vue";
import type { FileSystemBridge } from "./fsBridge";
import { searchFileNames, type FileNameSearchResult } from "./fileNameSearch";
import { searchFileContents, type FileContentSearchResult } from "./contentSearch";
import { fallbackCopyText } from "../../lib/terminalCopy";
import { quoteForShell } from "./shellQuote";
import type { PluginContext } from "../types";
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
  size?: number;
  modTime?: number;
}

const props = defineProps<{
  fs: FileSystemBridge;
  root: string;
  showHidden: boolean;
  /** Editor-side view option, surfaced here because the context menu is
   *  where both view toggles live. The tree itself doesn't read it. */
  showLineNumbers?: boolean;
  readOnly?: boolean;
  searchQuery?: string;
  searchMode?: "name" | "content";
  searchCaseSensitive?: boolean;
  searchRegex?: boolean;
  sortMode?: "name" | "modified" | "size";
  context?: PluginContext;
}>();

const emit = defineEmits<{
  (e: "file-clicked", path: string): void;
  (e: "file-double-clicked", path: string): void;
  (e: "dir-toggled", path: string, expanded: boolean): void;
  (e: "toggle-show-hidden", value: boolean): void;
  (e: "toggle-show-line-numbers", value: boolean): void;
}>();

type ContextMenuAnchor = { x: number; y: number; node: TreeNode; level: number; shift: boolean };
const menu = ref<ContextMenuAnchor | null>(null);
const menuEl = ref<HTMLElement | null>(null);
const copyBuffer = ref<{ path: string; isDir: boolean; fsIdentity: string } | null>(null);
const canPaste = computed(() => !!copyBuffer.value && copyBuffer.value.fsIdentity === props.fs.identity);
const pasteState = ref<{ current: string; cancelled: boolean } | null>(null);

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
  void nextTick(() => {
    const el = menuEl.value;
    if (!el || !menu.value) return;
    const rect = el.getBoundingClientRect();
    const pad = 8;
    const x = Math.max(pad, Math.min(menu.value.x, window.innerWidth - rect.width - pad));
    const y = Math.max(pad, Math.min(menu.value.y, window.innerHeight - rect.height - pad));
    menu.value = { ...menu.value, x, y };
  });
}

function closeMenu() { menu.value = null; }
function cancelPaste() {
  if (pasteState.value) pasteState.value = { ...pasteState.value, cancelled: true };
}

// The two view toggles are panel-wide, not node-specific, but the context
// menu is the only always-reachable surface inside the panel. The config
// write stays with the host so this component keeps taking its state purely
// from props.
function toggleShowHidden() {
  menu.value = null;
  emit("toggle-show-hidden", !props.showHidden);
}

function toggleShowLineNumbers() {
  menu.value = null;
  emit("toggle-show-line-numbers", !props.showLineNumbers);
}

async function copyPathToClipboard(path: string) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(path);
    } else if (!fallbackCopyText(path)) {
      return;
    }
    props.context?.showToast?.(t("plugins.fileExplorer.pathCopied"));
  } catch (err) {
    logWarn("file-explorer", "copy path failed", { error: errText(err) });
  }
}

function decodeBytes(data: unknown): Uint8Array {
  if (data instanceof Uint8Array) return data;
  if (Array.isArray(data)) return new Uint8Array(data as number[]);
  if (typeof data === "string") {
    const raw = atob(data);
    const bytes = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
    return bytes;
  }
  throw new Error(t("plugins.fileExplorer.unexpectedContentType"));
}

async function pathExists(path: string): Promise<boolean> {
  try { await props.fs.fileMeta(path); return true; } catch { return false; }
}

async function uniquePastePath(parent: string, name: string): Promise<string> {
  const dot = name.lastIndexOf(".");
  const stem = dot > 0 ? name.slice(0, dot) : name;
  const ext = dot > 0 ? name.slice(dot) : "";
  for (let i = 0; i < 1000; i++) {
    const suffix = i === 0 ? " copy" : ` copy ${i + 1}`;
    const candidate = joinPath(parent, `${stem}${suffix}${ext}`);
    if (!(await pathExists(candidate))) return candidate;
  }
  throw new Error(t("plugins.fileExplorer.pasteNameExhausted"));
}

async function copyEntry(source: string, destination: string, isDir: boolean): Promise<void> {
  if (pasteState.value?.cancelled) throw new Error(t("plugins.fileExplorer.operationCancelled"));
  pasteState.value = { current: source, cancelled: pasteState.value?.cancelled ?? false };
  if (isDir) {
    await props.fs.mkdir(destination);
    const entries = await props.fs.listDir(source);
    for (const entry of entries as Array<{ name: string; isDir: boolean }>) {
      await copyEntry(joinPath(source, entry.name), joinPath(destination, entry.name), entry.isDir);
    }
    return;
  }
  const content = await props.fs.readFile(source, 50 * 1024 * 1024) as { data: unknown };
  await props.fs.writeFile(destination, decodeBytes(content.data), null);
}

async function pasteEntry(anchor: ContextMenuAnchor): Promise<void> {
  const clip = copyBuffer.value;
  if (!clip || clip.fsIdentity !== props.fs.identity) return;
  const destinationDir = anchor.node.isDir ? anchor.node.path : parentDir(anchor.node.path);
  if (clip.isDir && (destinationDir === clip.path || destinationDir.startsWith(`${clip.path}/`))) {
    throw new Error(t("plugins.fileExplorer.pasteIntoSelf"));
  }
  const destination = await uniquePastePath(destinationDir, baseName(clip.path));
  pasteState.value = { current: clip.path, cancelled: false };
  try {
    await copyEntry(clip.path, destination, clip.isDir);
  } finally {
    pasteState.value = null;
  }
  props.context?.showToast?.(t("plugins.fileExplorer.pasted", { path: destination }));
}

function onTreeKeydown(e: KeyboardEvent) {
  const isCopyKey = e.key === "c" || e.key === "C";
  if (!isCopyKey || e.altKey || e.shiftKey) return;
  const mod = e.metaKey || e.ctrlKey;
  if (!mod) return;
  if (!selectedPath.value) return;
  e.preventDefault();
  void copyPathToClipboard(selectedPath.value);
}

async function onMenuAction(
  action: "newFile" | "newFolder" | "rename" | "delete" | "copyPath" | "copy" | "paste" | "sendToTerminal",
) {
  const anchor = menu.value;
  menu.value = null;
  if (!anchor) return;
  const node = anchor.node;
  if (action === "copyPath") {
    await copyPathToClipboard(node.path);
    return;
  }
  if (action === "copy") {
    copyBuffer.value = { path: node.path, isDir: node.isDir, fsIdentity: props.fs.identity };
    props.context?.showToast?.(t("plugins.fileExplorer.copied", { name: node.name }));
    return;
  }
  if (action === "paste") {
    try { await pasteEntry(anchor); }
    catch (err) { props.context?.showToast?.(errText(err)); }
    return;
  }
  if (action === "sendToTerminal") {
    props.context?.send?.(quoteForShell(node.path));
    return;
  }
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
    // A source that will not delete a directory says so here, before the
    // confirmation appears. Asking the user to agree to a delete and only then
    // refusing it teaches them that the dialog means nothing.
    const refusal = node.isDir ? props.fs.dirRemovalRefusal?.(node.path) : undefined;
    if (refusal) {
      props.context?.showToast?.(refusal);
      return;
    }
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
    // A refused create/rename used to be a log line only, so on a source where
    // refusals are ordinary the row simply did not appear and nothing said why.
    logWarn("file-explorer", "inline action failed", { error: errText(err) });
    props.context?.showToast?.(errText(err));
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
    // A delete that failed halfway through is exactly the case the user must
    // hear about: the tree will not refresh itself on every source, so a
    // silent failure is indistinguishable from a stale row.
    logWarn("file-explorer", "delete failed", { error: errText(err) });
    props.context?.showToast?.(errText(err));
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
const searchResults = ref<Array<FileNameSearchResult | FileContentSearchResult>>([]);
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

// truncations records the directories whose listing came back capped, keyed by
// path. A source that caps (the SSH one does, at 2000 entries) has to say so
// where the user is looking: a tree that quietly shows 2000 of 30000 files is
// indistinguishable from a directory that really holds 2000, and every
// conclusion drawn from it is wrong.
const truncations = ref<Record<string, { shown: number; total: number }>>({});

const truncationNotices = computed(() =>
  Object.entries(truncations.value).map(([path, info]) => ({ path, ...info })),
);

function noteTruncation(path: string, truncated: boolean, shown: number, total: number) {
  const had = path in truncations.value;
  if (!truncated) {
    if (had) {
      const next = { ...truncations.value };
      delete next[path];
      truncations.value = next;
    }
    return;
  }
  truncations.value = { ...truncations.value, [path]: { shown, total } };
}

// A notice describes rows the user can see. Once a directory is collapsed (or
// its subtree reloaded) those rows are gone, and a notice still naming that
// path reads as a complaint about the listing currently on screen.
function clearTruncationsUnder(parent: string, includeSelf: boolean) {
  const next = { ...truncations.value };
  let changed = false;
  for (const path of Object.keys(next)) {
    // The self case is decided first: isDescendant("/", "/") is true, because
    // every path starts with "/", and a root reload must not wipe the notice
    // it has just recorded for the root itself.
    const keep = path === parent ? !includeSelf : !isDescendant(path, parent);
    if (keep) continue;
    delete next[path];
    changed = true;
  }
  if (changed) truncations.value = next;
}

async function loadDir(fs: FileSystemBridge, path: string, showHidden: boolean): Promise<TreeNode[]> {
  // A source that can cap a listing reports it through listDirDetailed;
  // listDir's plain array has nowhere to put the fact.
  const listing = fs.listDirDetailed
    ? await fs.listDirDetailed(path)
    : { entries: (await fs.listDir(path)) as DirEntry[], truncated: false, total: 0 };
  const entries = listing.entries as DirEntry[];
  noteTruncation(path, listing.truncated, entries.length, listing.total);
  return entries
    .filter((e) => showHidden || !e.name.startsWith("."))
    .map((e) => ({
      path: joinPath(path, e.name),
      name: e.name,
      isDir: e.isDir,
      size: e.size,
      modTime: e.modTime,
      expanded: false,
      children: null,
    }))
    .sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      if (props.sortMode === "modified") return (b.modTime ?? 0) - (a.modTime ?? 0) || a.name.localeCompare(b.name);
      if (props.sortMode === "size") return (b.size ?? 0) - (a.size ?? 0) || a.name.localeCompare(b.name);
      return a.name.localeCompare(b.name);
    });
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
  clearTruncationsUnder(parent, false);
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

function collapseAll() {
  for (const node of rootNodes.value) {
    node.expanded = false;
    collapseDescendants(node);
    releaseDescendantState(node.path);
    pendingExpands.delete(node.path);
    advanceWatchGeneration(node.path);
    advanceRefreshGeneration(node.path);
    clearTruncationsUnder(node.path, true);
    const handle = watchHandles.get(node.path);
    if (handle) {
      watchHandles.delete(node.path);
      void unwatch(handle.fs, handle.id);
    }
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
  truncations.value = {};
  rootError.value = "";
  pendingExpands.clear();
  watchGenerations.clear();
  refreshGenerations.clear();
  releaseWatches();
}

// rootError holds why the root listing failed, for the one source where that
// is an ordinary first-run outcome rather than a bug.
//
// On the local filesystem listing the root effectively never fails, so
// swallowing the rejection cost nothing. Over SFTP the failures are "no saved
// credential", "host key not in known_hosts", "the host is down", "sftp-server
// is not installed", "the connection dropped; try again to reconnect" — every
// one of them a sentence the Go side wrote for a user, and every one of them
// previously rendered as an empty tree.
const rootError = ref<string>("");

async function refreshRoot(fs: FileSystemBridge, root: string, showHidden: boolean, request: number) {
  const refreshRequest = advanceRefreshGeneration(root);
  let nodes: TreeNode[];
  try {
    nodes = await loadDir(fs, root, showHidden);
  } catch (err) {
    if (isCurrent(fs, root, showHidden, request) && refreshGenerations.get(root) === refreshRequest) {
      rootError.value = t("plugins.fileExplorer.treeLoadFailed", { path: root, message: errText(err) });
      rootNodes.value = [];
    }
    return;
  }
  if (!isCurrent(fs, root, showHidden, request) || refreshGenerations.get(root) !== refreshRequest) return;
  rootError.value = "";
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

watch(() => [props.root, props.fs, props.showHidden, props.sortMode], startGeneration);

watch(
  () => [props.root, props.fs, props.showHidden, normalizedSearchQuery.value, props.searchMode, props.searchCaseSensitive, props.searchRegex] as const,
  async ([root, fs, showHidden, query, mode, caseSensitive, useRegex]) => {
    const request = ++searchGeneration;
    searchResults.value = [];
    searchTruncated.value = false;
    if (!query) {
      searchLoading.value = false;
      return;
    }
    searchLoading.value = true;
    try {
      // isCancelled, not just discarding the result: the walk is one listing
      // per directory, and on a source where a listing is a network round trip
      // a superseded search that keeps going is traffic nobody asked for.
      // maxDirs comes from the source for the same reason — see
      // FileSystemBridge.searchMaxDirs.
      const result = mode === "content"
        ? await searchFileContents(fs, root, query, {
          showHidden,
          caseSensitive,
          useRegex,
          maxDirs: fs.searchMaxDirs,
          isCancelled: () => disposed || searchGeneration !== request,
        })
        : await searchFileNames(fs, root, query, {
          showHidden,
          maxDirs: fs.searchMaxDirs,
          isCancelled: () => disposed || searchGeneration !== request,
        });
      if (disposed || searchGeneration !== request) return;
      searchResults.value = result.results;
      searchTruncated.value = result.truncated;
    } catch (err) {
      if (disposed || searchGeneration !== request) return;
      logWarn("file-explorer", "file content search failed", { error: errText(err) });
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
        // The listing gets its own catch. Folded into the one below it was
        // reported as "watcher unavailable or cap reached", which names the
        // wrong failure — and on a remote source the listing is by far the
        // more likely of the two to fail, and the one the user can act on.
        let children: TreeNode[];
        try {
          children = await loadDir(fs, n.path, showHidden);
        } catch (err) {
          if (isCurrentNode(fs, root, showHidden, request, n)) {
            logWarn("file-explorer", "listing a directory failed", { path: n.path, error: errText(err) });
            props.context?.showToast?.(
              t("plugins.fileExplorer.treeLoadFailed", { path: n.path, message: errText(err) }),
            );
          }
          return;
        }
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
        logWarn("file-explorer", "watcher unavailable or cap reached", { path: n.path, error: errText(err) });
      }
    } finally {
      if (pendingExpands.get(n.path) === watchRequest) pendingExpands.delete(n.path);
    }
  } else {
    advanceWatchGeneration(n.path);
    pendingExpands.delete(n.path);
    releaseDescendantState(n.path);
    clearTruncationsUnder(n.path, true);
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

function clickSearchResult(result: FileNameSearchResult | FileContentSearchResult) {
  selectedPath.value = result.path;
  emit("file-clicked", result.path);
}

function dblClickSearchResult(result: FileNameSearchResult | FileContentSearchResult) {
  selectedPath.value = result.path;
  emit("file-double-clicked", result.path);
}

// Minimal CSS attribute-selector escaper for the title lookup in revealPath.
function cssEscape(s: string): string {
  return s.replace(/["\\]/g, "\\$&");
}

function ancestorChain(root: string, path: string): string[] {
  // Directory paths from just-below-root down to the parent of `path`, e.g.
  // root=/proj, path=/proj/a/b/c.ts -> ["/proj/a", "/proj/a/b"].
  const base = root.endsWith("/") ? root.slice(0, -1) : root;
  if (path === base || !path.startsWith(base + "/")) return [];
  const rest = path.slice(base.length + 1);
  const parts = rest.split("/");
  const dirs: string[] = [];
  let cur = base;
  for (let i = 0; i < parts.length - 1; i++) {
    cur = cur + "/" + parts[i];
    dirs.push(cur);
  }
  return dirs;
}

/**
 * Expand every ancestor directory of `path` (lazily loading children via
 * toggle) and select it. Returns true only when `path` is an existing *file*
 * inside the current root subtree; false for a directory, a missing entry, or
 * a path outside the root.
 */
async function revealPath(path: string): Promise<boolean> {
  const base = props.root.endsWith("/") ? props.root.slice(0, -1) : props.root;
  if (path !== base && !path.startsWith(base + "/")) return false;
  for (const dir of ancestorChain(props.root, path)) {
    const node = findNode(rootNodes.value, dir);
    if (!node) return false;
    if (!node.expanded) await toggle(node);
  }
  const target = findNode(rootNodes.value, path);
  if (!target) return false;
  selectedPath.value = path;
  await nextTick();
  const el = document.querySelector<HTMLElement>(`.node[title="${cssEscape(path)}"]`);
  el?.scrollIntoView?.({ block: "nearest" });
  return !target.isDir;
}

/**
 * refreshDir re-lists one directory in place, keeping the rest of the tree
 * expanded. startGeneration would also show a newly written file, but only by
 * throwing the whole tree away — which, now that a write can land anywhere in
 * it and not just at the root, means collapsing the user back to the top the
 * moment their upload succeeds.
 */
async function refreshDir(dir: string) {
  try {
    await refreshChanged(props.fs, props.root, props.showHidden, generation, dir);
  } catch (err) {
    logWarn("file-explorer", "refreshing a directory failed", { path: dir, error: errText(err) });
  }
}

async function onDrop(event: DragEvent) {
  if (props.readOnly) return;
  const files = Array.from(event.dataTransfer?.files ?? []);
  if (files.length === 0) return;
  const dir = selectedDir.value;
  try {
    for (const file of files) {
      await props.fs.writeFile(joinPath(dir, file.name), new Uint8Array(await file.arrayBuffer()), null);
    }
    props.context?.showToast?.(t("plugins.fileExplorer.dropUploaded", { count: files.length, dir }));
    await refreshDir(dir);
  } catch (err) {
    props.context?.showToast?.(errText(err));
  }
}

const selectedDir = computed<string>(() => {
  if (!selectedPath.value) return props.root;
  const node = findNode(rootNodes.value, selectedPath.value);
  if (!node) return props.root;
  return node.isDir ? node.path : parentDir(node.path);
});

defineExpose({ refresh: startGeneration, refreshDir, revealPath, selectedDir, collapseAll });
</script>

<template>
  <div class="tree-wrap" tabindex="0" @click="closeMenu" @keydown="onTreeKeydown" @dragover.prevent @drop.prevent="onDrop">
    <div v-if="pasteState" class="operation-bar" data-test="paste-progress">
      <span>{{ t("plugins.fileExplorer.pasting", { path: pasteState.current }) }}</span>
      <button type="button" @click.stop="cancelPaste">{{ t("plugins.fileExplorer.cancel") }}</button>
    </div>
    <div v-if="rootError" class="tree-error" data-test="tree-error">
      <span class="tree-error-text">{{ rootError }}</span>
      <button class="tree-error-retry" data-test="tree-error-retry" @click.stop="startGeneration">
        {{ t("plugins.fileExplorer.retry") }}
      </button>
    </div>
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
        <span class="search-path">
          {{ result.path }}<template v-if="'line' in result">:{{ result.line }}</template>
        </span>
        <span v-if="'preview' in result" class="search-preview">{{ result.preview }}</span>
      </button>
      <div v-if="searchTruncated" class="search-status">
        {{ t("plugins.fileExplorer.searchTruncated") }}
      </div>
    </div>
    <template v-else>
      <ul class="tree-root">
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
        v-for="notice in truncationNotices"
        :key="notice.path"
        class="search-status"
        data-test="listing-truncated"
        :title="notice.path"
      >
        {{ t("plugins.fileExplorer.listingTruncated", {
          path: notice.path, shown: notice.shown, total: notice.total,
        }) }}
      </div>
    </template>
    <div
      v-if="menu"
      ref="menuEl"
      class="ctx-menu"
      data-test="file-tree-menu"
      :style="{ top: menu.y + 'px', left: menu.x + 'px' }"
      @click.stop
    >
      <button v-if="!readOnly" data-test="menu-new-file" @click="onMenuAction('newFile')">{{ t("plugins.fileExplorer.newFile") }}</button>
      <button v-if="!readOnly" data-test="menu-new-folder" @click="onMenuAction('newFolder')">{{ t("plugins.fileExplorer.newFolder") }}</button>
      <button v-if="!readOnly" data-test="menu-rename" @click="onMenuAction('rename')">{{ t("plugins.fileExplorer.rename") }}</button>
      <button v-if="!readOnly" data-test="menu-copy" @click="onMenuAction('copy')">{{ t("plugins.fileExplorer.copy") }}</button>
      <button
        v-if="!readOnly"
        data-test="menu-paste"
        :disabled="!canPaste"
        @click="onMenuAction('paste')"
      >{{ t("plugins.fileExplorer.paste") }}</button>
      <button data-test="menu-copy-path" @click="onMenuAction('copyPath')">{{ t("plugins.fileExplorer.copyPath") }}</button>
      <button data-test="menu-send-to-terminal" @click="onMenuAction('sendToTerminal')">{{ t("plugins.fileExplorer.sendToTerminal") }}</button>
      <button v-if="!readOnly" data-test="menu-delete" @click="onMenuAction('delete')">{{ t("plugins.fileExplorer.delete") }}</button>
      <hr class="ctx-sep" />
      <button
        class="ctx-toggle"
        data-test="menu-toggle-hidden"
        role="menuitemcheckbox"
        :aria-checked="showHidden ? 'true' : 'false'"
        @click="toggleShowHidden"
      >
        <span class="ctx-check">{{ showHidden ? "✓" : "" }}</span>
        {{ t("plugins.fileExplorer.showHidden") }}
      </button>
      <button
        class="ctx-toggle"
        data-test="menu-toggle-line-numbers"
        role="menuitemcheckbox"
        :aria-checked="showLineNumbers ? 'true' : 'false'"
        @click="toggleShowLineNumbers"
      >
        <span class="ctx-check">{{ showLineNumbers ? "✓" : "" }}</span>
        {{ t("plugins.fileExplorer.showLineNumbers") }}
      </button>
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
.tree-wrap:focus { outline: none; }
.operation-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  border-bottom: 1px solid var(--ed-border, #444c56);
  background: var(--ed-tab-bg, #2d333b);
  color: var(--ed-muted, rgba(173, 186, 199, 0.8));
  font-size: 11px;
}
.operation-bar span { flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.operation-bar button { border: 1px solid var(--ed-border, #444c56); border-radius: 3px; background: none; color: inherit; cursor: pointer; font: inherit; padding: 1px 6px; }
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
.search-preview {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--ed-row-fg, #adbac7);
  opacity: 0.75;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
}
.search-status {
  padding: 8px 10px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.55));
  font-size: 12px;
}
.tree-error {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  margin: 8px;
  padding: 8px 10px;
  border: 1px solid var(--ed-error-border, rgba(248, 81, 73, 0.4));
  border-radius: 4px;
  background: var(--ed-error-bg, rgba(248, 81, 73, 0.08));
  font-size: 12px;
  line-height: 1.5;
}
.tree-error-text { color: var(--ed-row-fg, #adbac7); word-break: break-word; }
.tree-error-retry {
  background: none;
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 4px;
  color: var(--ed-row-fg, #adbac7);
  font: inherit;
  font-size: 11px;
  padding: 2px 8px;
  cursor: pointer;
}
.tree-error-retry:hover { background: var(--ed-row-hover, rgba(255, 255, 255, 0.06)); }
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
  max-height: calc(100vh - 16px);
  overflow-y: auto;
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
.ctx-menu button:disabled { opacity: 0.45; cursor: default; }
.ctx-sep {
  margin: 4px 0;
  border: none;
  border-top: 1px solid var(--ed-border, #444c56);
}
.ctx-menu button.ctx-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-left: 8px;
}
.ctx-check {
  display: inline-block;
  width: 10px;
  flex: 0 0 10px;
  color: var(--ed-tab-active-bar, #539bf5);
}
</style>
