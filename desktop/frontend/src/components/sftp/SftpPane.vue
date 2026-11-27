<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  ArrowDownAZ,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  FolderPlus,
  Pencil,
  RefreshCw,
  Search,
  Trash2,
  X,
} from "lucide-vue-next";
import type { AcceptedHostKey, SFTPDirEntry, SSHHost } from "../../lib/api";
import { ACCEPT_NO_HOST_KEY, openSFTPSource } from "../../lib/api";
import { useI18n } from "../../i18n/useI18n";
import { parseHostKeyPrompt, type HostKeyPrompt } from "../../lib/sshHostKey";
import FileEditor from "../../plugins/fileExplorer/FileEditor.vue";
import { createSSHHostFS } from "../../plugins/fileExplorer/sshHostFS";
import type { DirListing, FileSystemBridge } from "../../plugins/fileExplorer/fsBridge";
import SftpSourcePicker from "./SftpSourcePicker.vue";
import {
  goBack,
  goForward,
  navigate,
  switchSource,
  visibleEntries,
  type SftpPaneState,
  type SftpSource,
} from "./sftpWorkspaceModel";

const props = defineProps<{
  state: SftpPaneState;
  hosts: SSHHost[];
  localFS: FileSystemBridge;
  localHome: string;
}>();

const emit = defineEmits<{
  (event: "update:state", state: SftpPaneState): void;
  (event: "transfer-selection"): void;
  (event: "toast", message: string): void;
  (event: "host-key-required", payload: { hostId: string; prompt: HostKeyPrompt }): void;
}>();

const { t } = useI18n();
const paneState = ref(cloneState(props.state));
const currentFS = ref<FileSystemBridge | null>(props.state.source.kind === "local" ? props.localFS : null);
const entries = ref<SFTPDirEntry[]>([]);
const loading = ref(false);
const loadError = ref("");
const truncated = ref(false);
const totalEntries = ref(0);
const pathDraft = ref(props.state.cwd);
const editorPath = ref("");
const loadGeneration = ref(0);

type ActionKind = "mkdir" | "rename" | "delete";
const action = ref<ActionKind | null>(null);
const actionInput = ref("");
const actionBusy = ref(false);
const actionError = ref("");

watch(() => props.state, (next) => {
  paneState.value = cloneState(next);
  pathDraft.value = next.cwd;
}, { deep: true });

const shownEntries = computed(() => visibleEntries(entries.value, paneState.value));
const selectedEntries = computed(() => entries.value.filter((entry) => paneState.value.selectedPaths.has(entryPath(entry))));
const canRename = computed(() => selectedEntries.value.length === 1);
const canDelete = computed(() => selectedEntries.value.length > 0);

function updateState(next: SftpPaneState) {
  paneState.value = cloneState(next);
  pathDraft.value = next.cwd;
  emit("update:state", cloneState(next));
}

async function loadDirectory() {
  const fs = currentFS.value;
  if (!fs) return;
  const request = ++loadGeneration.value;
  loading.value = true;
  loadError.value = "";
  try {
    const listing: DirListing = fs.listDirDetailed
      ? await fs.listDirDetailed(paneState.value.cwd)
      : { entries: await fs.listDir(paneState.value.cwd), truncated: false, total: 0 };
    if (request !== loadGeneration.value || fs !== currentFS.value) return;
    entries.value = listing.entries as SFTPDirEntry[];
    truncated.value = listing.truncated;
    totalEntries.value = listing.total || listing.entries.length;
  } catch (error) {
    if (request !== loadGeneration.value || fs !== currentFS.value) return;
    loadError.value = error instanceof Error ? error.message : String(error);
  } finally {
    if (request === loadGeneration.value) loading.value = false;
  }
}

async function chooseSource(source: SftpSource, accepted: AcceptedHostKey = ACCEPT_NO_HOST_KEY) {
  if (sameSource(source, paneState.value.source) && !loadError.value) return;
  editorPath.value = "";
  if (source.kind === "local") {
    disposeRemoteBridge();
    currentFS.value = props.localFS;
    updateState(switchSource(paneState.value, source, props.localHome));
    await loadDirectory();
    return;
  }

  updateState(switchSource(paneState.value, source, "/"));
  loadError.value = "";
  loading.value = true;
  try {
    const info = await openSFTPSource(source.hostId, accepted);
    disposeRemoteBridge();
    currentFS.value = createSSHHostFS(source.hostId);
    updateState(switchSource(paneState.value, source, info.initial_dir));
    await loadDirectory();
  } catch (error) {
    const prompt = parseHostKeyPrompt(error);
    if (prompt) emit("host-key-required", { hostId: source.hostId, prompt });
    loadError.value = prompt ? t("sftp.hostKeyRequired") : (error instanceof Error ? error.message : String(error));
    loading.value = false;
  }
}

async function retrySource(accepted: AcceptedHostKey = ACCEPT_NO_HOST_KEY) {
  if (paneState.value.source.kind === "ssh") {
    await chooseSource(paneState.value.source, accepted);
  } else {
    await loadDirectory();
  }
}

defineExpose({ retrySource });

async function moveTo(next: SftpPaneState) {
  updateState(next);
  editorPath.value = "";
  await loadDirectory();
}

function onPathSubmit() {
  const path = pathDraft.value.trim();
  if (path) void moveTo(navigate(paneState.value, path));
}

function upPath(value: string): string {
  const slash = value.includes("\\") && !value.includes("/") ? "\\" : "/";
  const normalized = value.replace(/[\\/]+$/, "");
  const index = Math.max(normalized.lastIndexOf("/"), normalized.lastIndexOf("\\"));
  if (index < 0) return value;
  if (index === 0) return slash;
  if (/^[A-Za-z]:$/.test(normalized.slice(0, index))) return `${normalized.slice(0, index)}${slash}`;
  return normalized.slice(0, index);
}

function joinPath(base: string, name: string): string {
  const slash = base.includes("\\") && !base.includes("/") ? "\\" : "/";
  const trimmed = base.replace(/[\\/]+$/, "");
  return `${trimmed || slash}${trimmed ? slash : ""}${name}`;
}

function entryPath(entry: SFTPDirEntry): string {
  return joinPath(paneState.value.cwd, entry.name);
}

function onRowDoubleClick(entry: SFTPDirEntry) {
  const path = entryPath(entry);
  if (entry.isDir) void moveTo(navigate(paneState.value, path));
  else editorPath.value = path;
}

function toggleSelection(path: string, selected: boolean) {
  const next = cloneState(paneState.value);
  if (selected) next.selectedPaths.add(path);
  else next.selectedPaths.delete(path);
  updateState(next);
}

function updateQuery(query: string) {
  updateState({ ...cloneState(paneState.value), query });
}

function cycleSort(sort: SftpPaneState["sort"]) {
  const direction = paneState.value.sort === sort && paneState.value.direction === "asc" ? "desc" : "asc";
  updateState({ ...cloneState(paneState.value), sort, direction });
}

function openAction(kind: ActionKind) {
  action.value = kind;
  actionError.value = "";
  if (kind === "mkdir") actionInput.value = "";
  if (kind === "rename") actionInput.value = selectedEntries.value[0]?.name ?? "";
}

async function confirmAction() {
  const fs = currentFS.value;
  if (!fs || !action.value) return;
  actionBusy.value = true;
  actionError.value = "";
  try {
    if (action.value === "mkdir") {
      const name = actionInput.value.trim();
      if (!name) throw new Error(t("sftp.nameRequired"));
      await fs.mkdir(joinPath(paneState.value.cwd, name));
    } else if (action.value === "rename") {
      const selected = selectedEntries.value[0];
      const name = actionInput.value.trim();
      if (!selected || !name) throw new Error(t("sftp.nameRequired"));
      await fs.rename(entryPath(selected), joinPath(paneState.value.cwd, name));
    } else {
      for (const selected of selectedEntries.value) {
        const path = entryPath(selected);
        if (paneState.value.source.kind === "local") await fs.trash(path);
        else await fs.remove(path, selected.isDir);
      }
    }
    action.value = null;
    const next = cloneState(paneState.value);
    next.selectedPaths.clear();
    updateState(next);
    await loadDirectory();
  } catch (error) {
    actionError.value = error instanceof Error ? error.message : String(error);
  } finally {
    actionBusy.value = false;
  }
}

function disposeRemoteBridge() {
  if (currentFS.value && currentFS.value !== props.localFS) currentFS.value.dispose?.();
}

function sameSource(left: SftpSource, right: SftpSource): boolean {
  return left.kind === right.kind && (left.kind === "local" || left.hostId === (right as { hostId: string }).hostId);
}

function cloneState(state: SftpPaneState): SftpPaneState {
  return {
    ...state,
    source: state.source.kind === "local" ? { kind: "local" } : { kind: "ssh", hostId: state.source.hostId },
    back: [...state.back],
    forward: [...state.forward],
    selectedPaths: new Set(state.selectedPaths),
  };
}

onMounted(async () => {
  if (paneState.value.source.kind === "ssh") await chooseSource(paneState.value.source);
  else await loadDirectory();
});

onBeforeUnmount(() => {
  loadGeneration.value++;
  disposeRemoteBridge();
});
</script>

<template>
  <section class="sftp-pane" :aria-busy="loading">
    <header class="pane-toolbar">
      <SftpSourcePicker
        :source="paneState.source"
        :hosts="hosts"
        :disabled="loading"
        @select="chooseSource"
      />
      <div class="nav-controls">
        <button data-test="nav-back" :title="t('sftp.back')" :disabled="!paneState.back.length" @click="moveTo(goBack(paneState))"><ArrowLeft :size="16" /></button>
        <button data-test="nav-forward" :title="t('sftp.forward')" :disabled="!paneState.forward.length" @click="moveTo(goForward(paneState))"><ArrowRight :size="16" /></button>
        <button data-test="nav-up" :title="t('sftp.up')" @click="moveTo(navigate(paneState, upPath(paneState.cwd)))"><ArrowUp :size="16" /></button>
        <button :title="t('common.refresh')" @click="loadDirectory"><RefreshCw :size="16" /></button>
      </div>
    </header>

    <div class="path-row">
      <input data-test="path-input" :value="pathDraft" :aria-label="t('sftp.path')" @input="pathDraft = ($event.target as HTMLInputElement).value" @keydown.enter="onPathSubmit" />
      <label class="search-box">
        <Search :size="14" />
        <input data-test="query-input" :value="paneState.query" :placeholder="t('sftp.filter')" @input="updateQuery(($event.target as HTMLInputElement).value)" />
      </label>
    </div>

    <div class="action-row">
      <button data-test="create-dir" :title="t('sftp.createDirectory')" @click="openAction('mkdir')"><FolderPlus :size="16" /></button>
      <button data-test="rename" :title="t('sftp.rename')" :disabled="!canRename" @click="openAction('rename')"><Pencil :size="16" /></button>
      <button data-test="delete" :title="t('common.delete')" :disabled="!canDelete" @click="openAction('delete')"><Trash2 :size="16" /></button>
      <span class="action-spacer"></span>
      <button data-test="sort-name" :class="{ active: paneState.sort === 'name' }" :title="t('sftp.sortName')" @click="cycleSort('name')"><ArrowDownAZ :size="15" />{{ t("sftp.name") }}</button>
      <button data-test="sort-size" :class="{ active: paneState.sort === 'size' }" @click="cycleSort('size')">{{ t("sftp.size") }}</button>
      <button data-test="sort-modified" :class="{ active: paneState.sort === 'modified' }" @click="cycleSort('modified')">{{ t("sftp.modified") }}</button>
    </div>

    <div v-if="loadError" class="state error" data-test="load-error">
      <span>{{ loadError }}</span>
      <button data-test="retry" @click="retrySource()">{{ t("common.retry") }}</button>
    </div>
    <div v-else-if="loading" class="state">{{ t("common.loading") }}</div>
    <div v-else class="file-list">
      <div v-if="truncated" class="truncated" data-test="truncated">{{ t("sftp.truncated", { shown: entries.length, total: totalEntries }) }}</div>
      <table>
        <thead><tr><th class="select-col"></th><th>{{ t("sftp.name") }}</th><th>{{ t("sftp.modified") }}</th><th>{{ t("sftp.size") }}</th><th>{{ t("sftp.kind") }}</th></tr></thead>
        <tbody>
          <tr
            v-for="entry in shownEntries"
            :key="entryPath(entry)"
            data-test="file-row"
            :data-path="entryPath(entry)"
            :class="{ selected: paneState.selectedPaths.has(entryPath(entry)) }"
            tabindex="0"
            @dblclick="onRowDoubleClick(entry)"
          >
            <td><input :data-test="`row-select-${entryPath(entry)}`" type="checkbox" :checked="paneState.selectedPaths.has(entryPath(entry))" @change="toggleSelection(entryPath(entry), ($event.target as HTMLInputElement).checked)" /></td>
            <td class="name-cell" :title="entry.name">{{ entry.name }}</td>
            <td>{{ entry.modTime ? new Date(entry.modTime).toLocaleString() : "—" }}</td>
            <td>{{ entry.isDir ? "—" : (entry.size ?? 0).toLocaleString() }}</td>
            <td>{{ entry.isDir ? t("sftp.directory") : t("sftp.file") }}</td>
          </tr>
          <tr v-if="shownEntries.length === 0"><td colspan="5" class="empty">{{ paneState.query ? t("sftp.noMatches") : t("sftp.emptyDirectory") }}</td></tr>
        </tbody>
      </table>
    </div>

    <div v-if="editorPath && currentFS" class="editor-overlay">
      <div class="editor-header"><span :title="editorPath">{{ editorPath }}</span><button :title="t('common.close')" @click="editorPath = ''"><X :size="16" /></button></div>
      <FileEditor :fs="currentFS" :path="editorPath" :show-line-numbers="true" theme="dimmed" view-mode="code" />
    </div>

    <div v-if="action" class="action-dialog" role="dialog" :aria-label="t(`sftp.${action}`)">
      <div class="dialog-body">
        <h3>{{ t(`sftp.${action}`) }}</h3>
        <p v-if="action === 'delete'">{{ t("sftp.deleteConfirm", { count: selectedEntries.length }) }}</p>
        <input v-else data-test="action-input" v-model="actionInput" :placeholder="t('sftp.name')" @keydown.enter="confirmAction" />
        <p v-if="actionError" class="dialog-error">{{ actionError }}</p>
        <div class="dialog-actions">
          <button :disabled="actionBusy" @click="action = null">{{ t("common.cancel") }}</button>
          <button data-test="action-confirm" class="primary" :disabled="actionBusy" @click="confirmAction">{{ action === "delete" ? t("common.delete") : t("common.save") }}</button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.sftp-pane { position: relative; min-width: 0; min-height: 0; display: flex; flex-direction: column; background: #161b22; border: 1px solid #30363d; color: #c9d1d9; overflow: hidden; }
.pane-toolbar, .path-row, .action-row { display: flex; align-items: center; gap: 6px; padding: 7px 8px; border-bottom: 1px solid #30363d; }
.pane-toolbar > :first-child { min-width: 150px; flex: 1; }
.nav-controls { display: flex; gap: 3px; }
button { min-width: 30px; height: 30px; display: inline-flex; align-items: center; justify-content: center; gap: 5px; border: 1px solid #30363d; border-radius: 5px; background: #21262d; color: #c9d1d9; font: inherit; cursor: pointer; }
button:hover:not(:disabled), button.active { background: #30363d; color: #f0f6fc; }
button:disabled { opacity: .42; cursor: default; }
input { box-sizing: border-box; min-width: 0; height: 30px; border: 1px solid #30363d; border-radius: 5px; background: #0d1117; color: #f0f6fc; padding: 0 8px; font: inherit; }
.path-row > input { flex: 1 1 58%; }
.search-box { flex: 1 1 42%; min-width: 100px; position: relative; display: flex; align-items: center; }
.search-box svg { position: absolute; left: 8px; color: #8b949e; }
.search-box input { width: 100%; padding-left: 28px; }
.action-row { min-height: 32px; }
.action-row button { width: auto; padding: 0 8px; font-size: 12px; }
.action-spacer { flex: 1; }
.file-list { flex: 1; min-height: 0; overflow: auto; }
table { width: 100%; border-collapse: collapse; table-layout: fixed; font-size: 12px; }
th { position: sticky; top: 0; z-index: 1; text-align: left; padding: 7px 8px; color: #8b949e; background: #161b22; border-bottom: 1px solid #30363d; font-weight: 600; }
th:nth-child(2) { width: 42%; } th:nth-child(3) { width: 25%; } th:nth-child(4) { width: 13%; } th:nth-child(5) { width: 12%; }
td { height: 32px; padding: 3px 8px; border-bottom: 1px solid #21262d; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.select-col, td:first-child { width: 28px; padding-left: 8px; padding-right: 0; }
td input[type="checkbox"] { width: 14px; height: 14px; accent-color: #2f81f7; }
tr.selected td { background: rgba(47, 129, 247, .14); }
tbody tr:hover td { background: #1f242c; }
.name-cell { color: #f0f6fc; }
.empty { text-align: center; color: #8b949e; padding: 28px 12px; }
.state { flex: 1; display: grid; place-content: center; color: #8b949e; gap: 10px; padding: 20px; text-align: center; }
.state.error { color: #ff7b72; }
.state.error button { margin: 0 auto; padding: 0 12px; }
.truncated { padding: 6px 10px; color: #d29922; background: rgba(187, 128, 9, .1); border-bottom: 1px solid #30363d; font-size: 12px; }
.editor-overlay { position: absolute; inset: 0; z-index: 5; display: flex; flex-direction: column; background: #161b22; }
.editor-header { height: 38px; display: flex; align-items: center; gap: 8px; padding: 0 8px 0 12px; border-bottom: 1px solid #30363d; }
.editor-header span { flex: 1; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.editor-overlay :deep(.file-editor-host) { flex: 1; min-height: 0; }
.action-dialog { position: absolute; inset: 0; z-index: 8; display: grid; place-items: center; background: rgba(1, 4, 9, .68); padding: 16px; }
.dialog-body { width: min(340px, 100%); padding: 16px; border: 1px solid #30363d; border-radius: 7px; background: #21262d; box-shadow: 0 16px 36px rgba(0,0,0,.42); }
.dialog-body h3 { margin: 0 0 12px; font-size: 15px; letter-spacing: 0; }
.dialog-body p { color: #b1bac4; font-size: 13px; }
.dialog-body > input { width: 100%; }
.dialog-error { color: #ff7b72 !important; }
.dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
.dialog-actions button { padding: 0 12px; }
.dialog-actions .primary { border-color: #2f81f7; background: #1f6feb; color: white; }
</style>
