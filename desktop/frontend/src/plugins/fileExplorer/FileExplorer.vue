<script lang="ts" setup>
import { errText, logWarn } from "../../lib/log";
import { computed, nextTick, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { ChevronsDownUp, Pin, PinOff, RefreshCw, Upload } from "lucide-vue-next";
import { usePlatform } from "../../platform";
import { usePluginConfigStore } from "../configStore";
import { useResizer } from "../useResizer";
import { isLightTerminalTheme } from "../../lib/terminalThemes";
import FileTree from "./FileTree.vue";
import FileTabs from "./FileTabs.vue";
import FileEditor from "./FileEditor.vue";
import ConfirmDialog from "./ConfirmDialog.vue";
import { openPath, closeTab, setViewMode, setDirty, type TabsState } from "./tabsModel";
import { createLocalFSBridge, type FileSystemBridge } from "./fsBridge";
import { useFileRevealStore } from "./fileReveal";
import { createRemoteSessionFS } from "./remoteSessionFS";
import type { PluginContext } from "../types";
import { useI18n } from "../../i18n/useI18n";
// theme.css is loaded once from App.vue so its --ed-* vars are available
// even before this lazy chunk is fetched.

const props = defineProps<{ context: PluginContext }>();
const platform = usePlatform();
const store = usePluginConfigStore();
const { t } = useI18n();
const readOnlySession = computed(() =>
  props.context.activeIsRemote.value && (props.context.activeRemotePermission?.value ?? "full") !== "full",
);

// In-memory pinned root. Resets to "follow active pane" on app restart per spec.
const pinned = ref<string | null>(null);

const persistedInnerRatio = computed(() => store.cfg?.fileExplorer.innerTreeRatio ?? 0.3);
const dragInnerRatio = ref<number | null>(null);
const innerRatio = computed(() => dragInnerRatio.value ?? persistedInnerRatio.value);

const bodyRef = ref<HTMLDivElement | null>(null);

const { onMouseDown: onDividerDown } = useResizer({
  onDrag: (deltaX) => {
    if (!bodyRef.value) return;
    const width = bodyRef.value.clientWidth;
    if (width <= 0) return;
    const cur = dragInnerRatio.value ?? persistedInnerRatio.value;
    const next = Math.max(0.15, Math.min(cur - deltaX / width, 0.5));
    dragInnerRatio.value = next;
  },
  onEnd: () => {
    if (dragInnerRatio.value === null || !store.cfg) {
      dragInnerRatio.value = null;
      return;
    }
    const next = JSON.parse(JSON.stringify(store.cfg));
    next.fileExplorer.innerTreeRatio = dragInnerRatio.value;
    void store.save(next);
    dragInnerRatio.value = null;
  },
});

const bridgeOwner = computed(() => {
  if (!props.context.activeIsRemote.value) {
    return { identity: platform.pluginHost ? "local" : null, connection: null };
  }
  const sessionID = props.context.activeSessionId.value;
  const connection = props.context.activeSessionConnection.value;
  return { identity: sessionID && connection ? `remote:${sessionID}` : null, connection };
});

// Preserve an optimistic cwd only for the filesystem that reported it. A
// stale/null update from one bridge must never become another bridge's root.
const lastCwds = ref<Record<string, string>>({});
watch(
  () => [props.context.activeCwd.value, bridgeOwner.value.identity] as const,
  ([cwd, identity]) => {
    if (cwd && identity) {
      lastCwds.value = { ...lastCwds.value, [identity]: cwd };
    }
  },
  { immediate: true },
);

const root = computed<string | null>(() => {
  if (pinned.value) return pinned.value;
  const cur = props.context.activeCwd.value;
  if (cur) return cur;
  const identity = bridgeOwner.value.identity;
  return identity ? lastCwds.value[identity] ?? null : null;
});
const pathDraft = ref("");
watch(root, (value) => { if (value) pathDraft.value = value; }, { immediate: true });

const tabsState = ref<TabsState>({ tabs: [], activeIdx: -1 });
const fs = shallowRef<FileSystemBridge | null>(null);
const fsGeneration = ref(0);
const fileNameSearch = ref("");
const fileSearchMode = ref<"name" | "content">("name");
const searchCaseSensitive = ref(false);
const searchRegex = ref(false);
const sortMode = ref<"name" | "modified" | "size">("name");

watch(
  () => [bridgeOwner.value.identity, bridgeOwner.value.connection] as const,
  async ([identity, connection]) => {
    const next = identity === "local"
      ? platform.pluginHost
        ? createLocalFSBridge(platform.pluginHost, platform.events)
        : null
      : identity && connection
        ? createRemoteSessionFS(connection, identity)
        : null;
    const previous = fs.value;
    const identityChanged = previous?.identity !== next?.identity;

    fs.value = next;
    fsGeneration.value++;
    if (identityChanged) {
      pinned.value = null;
      tabsState.value = { tabs: [], activeIdx: -1 };
      fileNameSearch.value = "";
      fileSearchMode.value = "name";
      searchCaseSensitive.value = false;
      searchRegex.value = false;
      sortMode.value = "name";
    }
    await nextTick();
    previous?.dispose?.();
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  fs.value?.dispose?.();
  fs.value = null;
});

const fileRevealStore = useFileRevealStore();
const fileTreeRef = ref<{
  revealPath: (p: string) => Promise<boolean>;
  refresh: () => void;
  refreshDir: (dir: string) => Promise<void>;
  selectedDir: string;
  collapseAll: () => void;
} | null>(null);

const uploadInput = ref<HTMLInputElement | null>(null);
const uploading = ref(false);
const uploadTargetDir = computed(() => fileTreeRef.value?.selectedDir ?? root.value);

async function jumpToPath() {
  const path = pathDraft.value.trim();
  if (!path || !fs.value) return;
  try {
    const meta = await fs.value.fileMeta(path) as { isDir?: boolean };
    pinned.value = meta.isDir ? path : path.slice(0, Math.max(1, path.lastIndexOf("/")));
  } catch (err) {
    props.context.showToast(errText(err));
  }
}

function pickUpload() { uploadInput.value?.click(); }

async function onUploadPicked(ev: Event) {
  const input = ev.target as HTMLInputElement;
  const file = input.files?.[0] ?? null;
  input.value = "";
  const bridge = fs.value;
  const dir = uploadTargetDir.value;
  if (!file || !bridge || !dir) return;
  uploading.value = true;
  try {
    const target = dir.endsWith("/") ? dir + file.name : `${dir}/${file.name}`;
    await bridge.writeFile(target, new Uint8Array(await file.arrayBuffer()), null);
    props.context.showToast(t("plugins.fileExplorer.uploadDone", { name: file.name, dir }));
    await fileTreeRef.value?.refreshDir?.(dir);
  } catch (err) {
    props.context.showToast(errText(err));
    logWarn("file-explorer", "file upload failed", { error: errText(err) });
  } finally {
    uploading.value = false;
  }
}

// Consume a reveal request from the terminal: expand/select the path in the
// tree and, when it's a file, open a preview tab.
watch(
  () => fileRevealStore.pending,
  async (p) => {
    if (!p || !fs.value) return;
    await nextTick(); // let the tree mount if the panel just opened
    const isFile = (await fileTreeRef.value?.revealPath(p)) ?? false;
    if (isFile) {
      tabsState.value = openPath(tabsState.value, p, "persistent");
    }
    fileRevealStore.consume();
  },
  { immediate: true },
);

function onFileClick(path: string) {
  tabsState.value = openPath(tabsState.value, path, "persistent");
}

function onFileDoubleClick(path: string) {
  tabsState.value = openPath(tabsState.value, path, "persistent");
}

function selectTab(idx: number) {
  tabsState.value = { ...tabsState.value, activeIdx: idx };
}

function onExplorerKeydown(event: KeyboardEvent) {
  const mod = event.metaKey || event.ctrlKey;
  if (!mod) return;
  if (event.key.toLowerCase() === "w" && tabsState.value.activeIdx >= 0) {
    event.preventDefault();
    void onCloseRequest(tabsState.value.activeIdx);
    return;
  }
  if (event.key === "Tab" && tabsState.value.tabs.length > 1) {
    event.preventDefault();
    const step = event.shiftKey ? -1 : 1;
    const count = tabsState.value.tabs.length;
    tabsState.value = { ...tabsState.value, activeIdx: (tabsState.value.activeIdx + step + count) % count };
  }
}

function closeTabAt(idx: number) {
  tabsState.value = closeTab(tabsState.value, idx);
}

function bulkCloseTabs(action: "close-others" | "close-all" | "close-left" | "close-right", idx: number) {
  const target = action === "close-all"
    ? tabsState.value.tabs.map((_, i) => i)
    : action === "close-others"
      ? tabsState.value.tabs.map((_, i) => i).filter((i) => i !== idx)
      : action === "close-left"
        ? tabsState.value.tabs.map((_, i) => i).filter((i) => i < idx)
        : tabsState.value.tabs.map((_, i) => i).filter((i) => i > idx);
  const dirty = target.filter((i) => tabsState.value.tabs[i]?.dirty);
  if (dirty.length > 0) {
    props.context.showToast(t("plugins.fileExplorer.bulkCloseDirty"));
  }
  const keep = tabsState.value.tabs.filter((tab, i) => !target.includes(i) || tab.dirty);
  if (keep.length === tabsState.value.tabs.length) return;
  const activePath = tabsState.value.tabs[tabsState.value.activeIdx]?.path;
  const nextIdx = Math.max(0, keep.findIndex((tab) => tab.path === activePath));
  tabsState.value = { tabs: keep, activeIdx: keep.length ? nextIdx : -1 };
}

interface CloseConfirmSpec {
  idx: number;
  name: string;
  buttons: Array<{ id: string; label: string; kind?: "primary" | "danger" | "secondary" }>;
}
const confirmClose = ref<CloseConfirmSpec | null>(null);
const codeEditorRef = ref<{ save: () => Promise<boolean> } | null>(null);

function onDirtyChange(v: boolean) {
  const active = activePath.value;
  if (!active) return;
  tabsState.value = setDirty(tabsState.value, active, v);
}

async function onCloseRequest(idx: number) {
  const tab = tabsState.value.tabs[idx];
  if (!tab?.dirty) {
    closeTabAt(idx);
    return;
  }
  confirmClose.value = {
    idx,
    name: tab.path.split("/").pop() ?? tab.path,
    buttons: [
      { id: "save", label: t("plugins.fileExplorer.save"), kind: "primary" },
      { id: "dontSave", label: t("plugins.fileExplorer.dontSave"), kind: "danger" },
      { id: "cancel", label: t("plugins.fileExplorer.cancel"), kind: "secondary" },
    ],
  };
}

async function resolveConfirmClose(id: string) {
  const spec = confirmClose.value;
  confirmClose.value = null;
  if (!spec) return;
  if (id === "cancel") return;
  if (id === "dontSave") { closeTabAt(spec.idx); return; }
  // save
  const ok = (await codeEditorRef.value?.save?.()) ?? false;
  if (ok) closeTabAt(spec.idx);
}

function togglePin() {
  pinned.value = pinned.value === null ? props.context.activeCwd.value : null;
}

const activeViewMode = computed<"code" | "render">(() => {
  const i = tabsState.value.activeIdx;
  return i >= 0 ? tabsState.value.tabs[i].viewMode : "code";
});
function onToggleViewMode() {
  tabsState.value = setViewMode(tabsState.value, activeViewMode.value === "code" ? "render" : "code");
}

const activePath = computed(() =>
  tabsState.value.activeIdx >= 0 ? tabsState.value.tabs[tabsState.value.activeIdx].path : null,
);

const showHidden = computed(() => store.cfg?.fileExplorer.showHidden ?? false);
const showLineNumbers = computed(() => store.cfg?.fileExplorer.showLineNumbers ?? false);

// Both view toggles are driven from the file tree's context menu; the config
// write lives here so FileTree stays purely props-in / events-out.
async function saveViewOption(key: "showHidden" | "showLineNumbers", value: boolean) {
  if (!store.cfg) return;
  const next = JSON.parse(JSON.stringify(store.cfg));
  next.fileExplorer[key] = value;
  try {
    await store.save(next);
  } catch (err) {
    logWarn("file-explorer", "saving view option failed", { error: errText(err) });
  }
}

// Derive the explorer skin from the terminal theme. Light terminal → light;
// any dark terminal → dimmed.
const explorerTheme = computed<"dimmed" | "light">(() =>
  isLightTerminalTheme(props.context.terminalThemeId.value) ? "light" : "dimmed",
);
</script>

<template>
  <div class="file-explorer" tabindex="0" @keydown="onExplorerKeydown">
    <div class="fe-body" ref="bodyRef">
      <div class="tree-pane" :style="{ width: (innerRatio * 100) + '%' }">
        <header class="fe-header">
          <span class="root-path" :title="root ?? ''">{{ root ?? t("plugins.fileExplorer.noActivePaneShort") }}</span>
          <input
            v-if="root"
            v-model="pathDraft"
            class="path-jump"
            :placeholder="t('plugins.fileExplorer.pathPlaceholder')"
            :aria-label="t('plugins.fileExplorer.pathLabel')"
            @keydown.enter.prevent="jumpToPath"
          />
          <span v-if="readOnlySession" class="readonly-badge">{{ t("plugins.fileExplorer.readOnly") }}</span>
          <button
            v-if="fs && uploadTargetDir && !readOnlySession"
            class="upload-btn"
            data-test="file-upload"
            :disabled="uploading"
            :title="t('plugins.fileExplorer.uploadFile', { dir: uploadTargetDir })"
            @click="pickUpload"
          >
            <Upload :size="14" :stroke-width="1.5" />
          </button>
          <button
            v-if="fs"
            class="toolbar-btn"
            data-test="refresh-tree"
            :title="t('plugins.fileExplorer.refresh')"
            @click="fileTreeRef?.refresh()"
          ><RefreshCw :size="14" :stroke-width="1.5" /></button>
          <button
            v-if="fs"
            class="toolbar-btn"
            data-test="collapse-tree"
            :title="t('plugins.fileExplorer.collapseAll')"
            @click="fileTreeRef?.collapseAll()"
          ><ChevronsDownUp :size="14" :stroke-width="1.5" /></button>
          <button
            class="pin"
            :class="{ pinned: pinned !== null }"
            :title="pinned !== null ? t('plugins.fileExplorer.unpinFollow') : t('plugins.fileExplorer.pinCurrentCwd')"
            @click="togglePin"
          >
            <component :is="pinned !== null ? Pin : PinOff" :size="14" :stroke-width="1.5" />
          </button>
        </header>
        <input ref="uploadInput" class="upload-input" data-test="file-upload-input" type="file" @change="onUploadPicked">
        <div v-if="uploading" class="upload-status">{{ t("plugins.fileExplorer.uploading") }}</div>
        <div class="tree-search">
          <div class="search-row">
            <input
              v-model="fileNameSearch"
              :data-test="fileSearchMode === 'name' ? 'file-name-search' : 'file-content-search'"
              type="search"
              :placeholder="fileSearchMode === 'name'
                ? t('plugins.fileExplorer.searchFiles')
                : t('plugins.fileExplorer.searchContent')"
              :aria-label="fileSearchMode === 'name'
                ? t('plugins.fileExplorer.searchFiles')
                : t('plugins.fileExplorer.searchContent')"
            />
            <select v-model="fileSearchMode" class="search-mode" data-test="file-search-mode">
              <option value="name">{{ t('plugins.fileExplorer.searchByName') }}</option>
              <option value="content">{{ t('plugins.fileExplorer.searchByContent') }}</option>
            </select>
            <template v-if="fileSearchMode === 'content'">
              <button
                type="button"
                class="search-option"
                :class="{ active: searchCaseSensitive }"
                :title="t('plugins.fileExplorer.searchCaseSensitive')"
                @click="searchCaseSensitive = !searchCaseSensitive"
              >Aa</button>
              <button
                type="button"
                class="search-option"
                :class="{ active: searchRegex }"
                :title="t('plugins.fileExplorer.searchRegex')"
                @click="searchRegex = !searchRegex"
              >.*</button>
            </template>
          </div>
        </div>
        <div class="tree-toolbar">
          <span>{{ t("plugins.fileExplorer.sortBy") }}</span>
          <select v-model="sortMode" data-test="file-sort-mode">
            <option value="name">{{ t("plugins.fileExplorer.sortName") }}</option>
            <option value="modified">{{ t("plugins.fileExplorer.sortModified") }}</option>
            <option value="size">{{ t("plugins.fileExplorer.sortSize") }}</option>
          </select>
        </div>
        <div class="tree-scroll">
          <FileTree
            v-if="root && fs"
            ref="fileTreeRef"
            :key="fsGeneration"
            :fs="fs"
            :root="root"
            :show-hidden="showHidden"
            :show-line-numbers="showLineNumbers"
            :read-only="readOnlySession"
            :search-query="fileNameSearch"
            :search-mode="fileSearchMode"
            :search-case-sensitive="searchCaseSensitive"
            :search-regex="searchRegex"
            :sort-mode="sortMode"
            :context="context"
            @file-clicked="onFileClick"
            @file-double-clicked="onFileDoubleClick"
            @toggle-show-hidden="(v: boolean) => saveViewOption('showHidden', v)"
            @toggle-show-line-numbers="(v: boolean) => saveViewOption('showLineNumbers', v)"
          />
          <div v-else class="placeholder">{{ t("plugins.fileExplorer.noActivePane") }}</div>
        </div>
      </div>
      <div class="divider" @mousedown="onDividerDown" />
      <div class="editor-pane">
        <FileTabs
          :tabs="tabsState.tabs"
          :active-idx="tabsState.activeIdx"
          :view-mode="activeViewMode"
          @select="selectTab"
          @close-request="onCloseRequest"
          @toggle-view-mode="onToggleViewMode"
          @tab-action="bulkCloseTabs"
        />
        <div class="editor-area">
          <FileEditor
            v-if="activePath && fs"
            ref="codeEditorRef"
            :key="fsGeneration"
            :fs="fs"
            :path="activePath"
            :show-line-numbers="showLineNumbers"
            :read-only="readOnlySession"
            :theme="explorerTheme"
            :view-mode="activeViewMode"
            @dirty-change="onDirtyChange"
          />
          <div v-else class="placeholder">{{ t("plugins.fileExplorer.selectFile") }}</div>
        </div>
      </div>
    </div>
    <ConfirmDialog
      v-if="confirmClose"
      :title="t('plugins.fileExplorer.confirmCloseTitle', { name: confirmClose.name })"
      :buttons="confirmClose.buttons"
      @resolve="resolveConfirmClose"
    />
  </div>
</template>

<style scoped>
.file-explorer {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--ed-shell-bg, #22272e);
  color: var(--ed-row-fg, #adbac7);
}
.fe-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-bottom: 1px solid var(--ed-border, #444c56);
  background: var(--ed-tree-header-bg, #22272e);
  font-size: 11px;
  color: var(--ed-tree-header-fg, rgba(173, 186, 199, 0.7));
  text-transform: uppercase;
  letter-spacing: 0.4px;
}
.root-path { flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.path-jump { width: 86px; min-width: 0; height: 20px; border: 1px solid var(--ed-border, #444c56); border-radius: 3px; background: var(--ed-editor-bg, #22272e); color: var(--ed-row-fg, #adbac7); font: inherit; font-size: 10px; padding: 0 4px; }
.readonly-badge {
  flex: 0 0 auto;
  padding: 1px 5px;
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 3px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.7));
  font-size: 10px;
  text-transform: none;
  letter-spacing: 0;
}
.pin {
  background: none;
  border: none;
  cursor: pointer;
  padding: 0 2px;
  opacity: 0.55;
  color: inherit;
  display: inline-flex;
  align-items: center;
}
.upload-btn {
  background: none;
  border: none;
  cursor: pointer;
  padding: 0 2px;
  opacity: 0.55;
  color: inherit;
  display: inline-flex;
  align-items: center;
}
.upload-btn:hover { opacity: 1; }
.upload-btn:disabled { opacity: 0.35; cursor: default; }
.pin:hover { opacity: 1; }
.pin.pinned { opacity: 1; color: var(--ed-tab-active-bar, #539bf5); }
.toolbar-btn {
  background: none;
  border: none;
  color: inherit;
  padding: 0 2px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  opacity: 0.65;
}
.toolbar-btn:hover { opacity: 1; }
.upload-input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}

.tree-search {
  padding: 6px 8px;
  border-bottom: 1px solid var(--ed-border, #444c56);
  background: var(--ed-tree-bg, #2d333b);
}
.upload-status { padding: 4px 8px; color: var(--ed-muted, rgba(173, 186, 199, 0.75)); font-size: 11px; border-bottom: 1px solid var(--ed-border, #444c56); }
.search-row { display: flex; gap: 6px; }
.search-row input { flex: 1 1 auto; min-width: 0; }
.search-mode {
  flex: 0 0 auto;
  width: 68px;
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 4px;
  background: var(--ed-editor-bg, #22272e);
  color: var(--ed-row-fg, #adbac7);
  font: inherit;
  font-size: 11px;
}
.search-option {
  flex: 0 0 auto;
  width: 28px;
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 4px;
  background: var(--ed-editor-bg, #22272e);
  color: var(--ed-muted, rgba(173, 186, 199, 0.7));
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}
.search-option.active { color: var(--ed-tab-active-fg, #cdd9e5); border-color: var(--ed-tab-active-bar, #539bf5); background: var(--ed-row-selected, #37373d); }
.tree-search input {
  width: 100%;
  min-width: 0;
  height: 24px;
  box-sizing: border-box;
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 4px;
  background: var(--ed-editor-bg, #22272e);
  color: var(--ed-row-fg, #adbac7);
  padding: 0 8px;
  font: inherit;
  font-size: 12px;
  outline: none;
}
.tree-search input:focus {
  border-color: var(--ed-tab-active-bar, #539bf5);
}
.tree-toolbar {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 3px 8px;
  border-bottom: 1px solid var(--ed-border, #444c56);
  color: var(--ed-muted, rgba(173, 186, 199, 0.65));
  font-size: 10px;
}
.tree-toolbar span { flex: 1 1 auto; }
.tree-toolbar select { max-width: 100px; border: 1px solid var(--ed-border, #444c56); border-radius: 3px; background: var(--ed-editor-bg, #22272e); color: var(--ed-row-fg, #adbac7); font: inherit; }

.fe-body { flex: 1 1 auto; display: flex; min-height: 0; }
.tree-pane {
  min-width: 60px;
  background: var(--ed-tree-bg, #2d333b);
  border-right: 1px solid var(--ed-border, #444c56);
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.tree-scroll {
  flex: 1 1 auto;
  overflow: auto;
  padding: 4px 0;
}
.tree-scroll::-webkit-scrollbar { width: 10px; height: 10px; }
.tree-scroll::-webkit-scrollbar-track { background: transparent; }
.tree-scroll::-webkit-scrollbar-thumb {
  background: var(--ed-indent-guide, rgba(173, 186, 199, 0.18));
  border-radius: 5px;
  border: 2px solid var(--ed-tree-bg, #2d333b);
}
.tree-scroll::-webkit-scrollbar-thumb:hover { background: var(--ed-chevron, rgba(173, 186, 199, 0.3)); }

.divider {
  width: 4px;
  cursor: col-resize;
  background: transparent;
  flex: 0 0 4px;
}
.divider:hover { background: var(--ed-border, #444c56); }

.editor-pane { flex: 1 1 auto; display: flex; flex-direction: column; min-width: 0; }
.editor-area { flex: 1 1 auto; overflow: auto; }
.placeholder {
  color: var(--ed-muted, rgba(173, 186, 199, 0.55));
  font-size: 12px;
  padding: 12px;
}
</style>
