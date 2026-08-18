<script lang="ts" setup>
import { errText, logWarn } from "../../lib/log";
import { computed, nextTick, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { Pin, PinOff, Upload } from "lucide-vue-next";
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
import { createSSHHostFS, SSH_HOST_ROOT } from "./sshHostFS";
import { listSFTPHosts, type SSHHost } from "../../lib/api";
import type { PluginContext } from "../types";
import { useI18n } from "../../i18n/useI18n";
// theme.css is loaded once from App.vue so its --ed-* vars are available
// even before this lazy chunk is fetched.

const props = defineProps<{ context: PluginContext }>();
const platform = usePlatform();
const store = usePluginConfigStore();
const { t } = useI18n();

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

// --- data sources -----------------------------------------------------------
//
// Two of the three follow the active pane: this machine, and the host a remote
// session runs on. The third does not — a saved SSH host is browsable because
// it is saved, not because a terminal happens to be open on it — so it is a
// deliberate selection that overrides the follow-the-pane default until the
// user picks the default back.

const sshHosts = ref<SSHHost[]>([]);
const selectedSSHHostID = ref<string>("");

// The source list is asked for rather than derived from the saved hosts: the
// Go side hides the hosts atterm will not dial (a ProxyCommand host cannot be
// connected at all), and it derives that from the same gate the browse path
// runs, so the list and the refusal cannot drift apart.
async function loadSSHSources() {
  // The SSH source exists only where the Wails bindings do. Asking on a web or
  // mobile build would throw on every panel mount and log a warning about a
  // feature that build never had.
  if (!platform.pluginHost) return;
  try {
    sshHosts.value = (await listSFTPHosts()) ?? [];
  } catch (err) {
    logWarn("file-explorer", "ssh sources unavailable", { error: errText(err) });
    sshHosts.value = [];
  }
}
void loadSSHSources();

function sshHostLabel(h: SSHHost): string {
  return h.alias || `${h.user}@${h.host}`;
}

const bridgeOwner = computed(() => {
  if (selectedSSHHostID.value) {
    return { identity: `ssh:${selectedSSHHostID.value}`, connection: null, sshHostID: selectedSSHHostID.value };
  }
  if (!props.context.activeIsRemote.value) {
    return { identity: platform.pluginHost ? "local" : null, connection: null, sshHostID: "" };
  }
  const sessionID = props.context.activeSessionId.value;
  const connection = props.context.activeSessionConnection.value;
  return { identity: sessionID && connection ? `remote:${sessionID}` : null, connection, sshHostID: "" };
});

// Preserve an optimistic cwd only for the filesystem that reported it. A
// stale/null update from one bridge must never become another bridge's root.
const lastCwds = ref<Record<string, string>>({});
watch(
  () => [props.context.activeCwd.value, bridgeOwner.value.identity, bridgeOwner.value.sshHostID] as const,
  ([cwd, identity, sshHostID]) => {
    // An SSH source has no cwd of its own, and the active pane's belongs to a
    // different machine entirely. Recording it here would make a local path
    // the root of a remote tree the moment the user switched sources.
    if (cwd && identity && !sshHostID) {
      lastCwds.value = { ...lastCwds.value, [identity]: cwd };
    }
  },
  { immediate: true },
);

const root = computed<string | null>(() => {
  if (bridgeOwner.value.sshHostID) return SSH_HOST_ROOT;
  if (pinned.value) return pinned.value;
  const cur = props.context.activeCwd.value;
  if (cur) return cur;
  const identity = bridgeOwner.value.identity;
  return identity ? lastCwds.value[identity] ?? null : null;
});

const tabsState = ref<TabsState>({ tabs: [], activeIdx: -1 });
const fs = shallowRef<FileSystemBridge | null>(null);
const fsGeneration = ref(0);
const fileNameSearch = ref("");

watch(
  () => [bridgeOwner.value.identity, bridgeOwner.value.connection, bridgeOwner.value.sshHostID] as const,
  async ([identity, connection, sshHostID]) => {
    const next = sshHostID
      ? createSSHHostFS(sshHostID)
      : identity === "local"
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
const fileTreeRef = ref<{ revealPath: (p: string) => Promise<boolean>; refresh: () => void } | null>(null);

// --- upload (SSH source only) -----------------------------------------------
//
// The panel's other write paths edit a file that is already there. This one
// puts a new file on a machine that has no trash and no versioning, which is
// why the executor refuses an upload onto an occupied path rather than
// overwriting: the cost of the refusal is one more click, the cost of the
// overwrite is unrecoverable. Everything this handler does with the refusal is
// show the sentence the bridge produced for it.
const uploadInput = ref<HTMLInputElement | null>(null);
const uploading = ref(false);

function pickUpload() {
  uploadInput.value?.click();
}

async function onUploadPicked(ev: Event) {
  const input = ev.target as HTMLInputElement;
  const file = input.files?.[0] ?? null;
  input.value = ""; // so picking the same file twice fires again
  const bridge = fs.value;
  const dir = root.value;
  if (!file || !bridge || !dir) return;
  uploading.value = true;
  try {
    const bytes = new Uint8Array(await file.arrayBuffer());
    const target = dir.endsWith("/") ? dir + file.name : `${dir}/${file.name}`;
    await bridge.writeFile(target, bytes, null);
    props.context.showToast(t("plugins.fileExplorer.sshUploadDone", { name: file.name }));
    fileTreeRef.value?.refresh?.();
  } catch (err) {
    // errText carries the bridge's actionable sentence for the default
    // refusal ("... already exists ... rename it or delete it first"), not the
    // raw already_exists token.
    props.context.showToast(errText(err));
    logWarn("file-explorer", "ssh upload failed", { error: errText(err) });
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
      tabsState.value = openPath(tabsState.value, p, "preview");
    }
    fileRevealStore.consume();
  },
  { immediate: true },
);

function onFileClick(path: string) {
  tabsState.value = openPath(tabsState.value, path, "preview");
}

function onFileDoubleClick(path: string) {
  tabsState.value = openPath(tabsState.value, path, "persistent");
}

function selectTab(idx: number) {
  tabsState.value = { ...tabsState.value, activeIdx: idx };
}

function closeTabAt(idx: number) {
  tabsState.value = closeTab(tabsState.value, idx);
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
  <div class="file-explorer">
    <div class="fe-body" ref="bodyRef">
      <div class="tree-pane" :style="{ width: (innerRatio * 100) + '%' }">
        <header class="fe-header">
          <span class="root-path" :title="root ?? ''">{{ root ?? t("plugins.fileExplorer.noActivePaneShort") }}</span>
          <button
            v-if="bridgeOwner.sshHostID"
            class="pin"
            data-test="ssh-upload"
            :disabled="uploading || !root"
            :title="t('plugins.fileExplorer.sshUpload')"
            @click="pickUpload"
          >
            <Upload :size="14" :stroke-width="1.5" />
          </button>
          <button
            v-else
            class="pin"
            :class="{ pinned: pinned !== null }"
            :title="pinned !== null ? t('plugins.fileExplorer.unpinFollow') : t('plugins.fileExplorer.pinCurrentCwd')"
            @click="togglePin"
          >
            <component :is="pinned !== null ? Pin : PinOff" :size="14" :stroke-width="1.5" />
          </button>
        </header>
        <div v-if="sshHosts.length > 0" class="source-picker">
          <select
            v-model="selectedSSHHostID"
            data-test="fs-source"
            :aria-label="t('plugins.fileExplorer.sourceLabel')"
          >
            <option value="">{{ t("plugins.fileExplorer.sourceActivePane") }}</option>
            <option v-for="h in sshHosts" :key="h.id" :value="h.id">
              {{ sshHostLabel(h) }}
            </option>
          </select>
        </div>
        <input
          ref="uploadInput"
          class="upload-input"
          data-test="ssh-upload-input"
          type="file"
          @change="onUploadPicked"
        >
        <div class="tree-search">
          <input
            v-model="fileNameSearch"
            data-test="file-name-search"
            type="search"
            :placeholder="t('plugins.fileExplorer.searchFiles')"
            :aria-label="t('plugins.fileExplorer.searchFiles')"
          />
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
            :search-query="fileNameSearch"
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
        />
        <div class="editor-area">
          <FileEditor
            v-if="activePath && fs"
            ref="codeEditorRef"
            :key="fsGeneration"
            :fs="fs"
            :path="activePath"
            :show-line-numbers="showLineNumbers"
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
.pin:hover { opacity: 1; }
.pin.pinned { opacity: 1; color: var(--ed-tab-active-bar, #539bf5); }

.source-picker {
  padding: 6px 8px 0;
  background: var(--ed-tree-bg, #2d333b);
}
.source-picker select {
  width: 100%;
  min-width: 0;
  height: 24px;
  box-sizing: border-box;
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 4px;
  background: var(--ed-editor-bg, #22272e);
  color: var(--ed-row-fg, #adbac7);
  padding: 0 6px;
  font: inherit;
  font-size: 12px;
  outline: none;
}
/* The file picker is opened by the toolbar button; a visible file input would
   take more room in this narrow panel than the tree it sits above. */
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
