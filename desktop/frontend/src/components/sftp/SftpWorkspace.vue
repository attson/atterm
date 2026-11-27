<script setup lang="ts">
import { ArrowLeft, ArrowRight } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from "vue";
import {
  cancelFileTransfer,
  getUserHomeDir,
  listFileTransfers,
  listSFTPHosts,
  retryFileTransfer,
  startFileTransfer,
  type FileEndpoint,
  type FileTransferConflict,
  type FileTransferJob,
  type SSHHost,
  type StartFileTransferReq,
} from "../../lib/api";
import { useI18n } from "../../i18n/useI18n";
import type { HostKeyPrompt } from "../../lib/sshHostKey";
import { usePlatform } from "../../platform";
import { createLocalFSBridge } from "../../plugins/fileExplorer/fsBridge";
import SftpPane from "./SftpPane.vue";
import SftpTransferQueue from "./SftpTransferQueue.vue";
import {
  buildTransferRequest,
  createPaneState,
  type SftpPaneState,
  type SftpSource,
} from "./sftpWorkspaceModel";

type Side = "left" | "right";
type PaneHandle = { refresh(): Promise<void>; retrySource(accepted?: { host: string; fingerprint: string }): Promise<void> };
type PendingTransfer = { request: StartFileTransferReq; destination: Side };
type HostKeyRequest =
  | { kind: "transfer"; jobID: string; prompt: HostKeyPrompt }
  | { kind: "source"; side: Side; prompt: HostKeyPrompt };

const INTERNAL_DRAG_MIME = "application/x-atterm-sftp-paths";
const props = withDefaults(defineProps<{ active?: boolean }>(), { active: false });
const emit = defineEmits<{ (event: "edit-host", hostId: string): void }>();
const $platform = usePlatform();
const { t } = useI18n();
if (!$platform.pluginHost) throw new Error("SFTP workspace requires the desktop file-system bridge");

const localFS = createLocalFSBridge($platform.pluginHost, $platform.events);
const hosts = ref<SSHHost[]>([]);
const localHome = ref("/");
const left = ref(createPaneState({ kind: "local" }, "/"));
const right = ref(createPaneState({ kind: "local" }, "/"));
const leftPane = shallowRef<PaneHandle | null>(null);
const rightPane = shallowRef<PaneHandle | null>(null);
const jobs = ref<FileTransferJob[]>([]);
const workspaceError = ref("");
const transferError = ref("");
const pendingTransfer = ref<PendingTransfer | null>(null);
const conflict = ref<FileTransferConflict>("skip");
const hostKey = ref<HostKeyRequest | null>(null);
const jobDestinations = new Map<string, Side>();
const refreshedJobs = new Set<string>();
let stopProgress: (() => void) | undefined;
let stopNativeDrop: (() => void) | undefined;

const canTransferRight = computed(() => left.value.selectedPaths.size > 0);
const canTransferLeft = computed(() => right.value.selectedPaths.size > 0);

function pane(side: Side): SftpPaneState {
  return side === "left" ? left.value : right.value;
}

function setPane(side: Side, state: SftpPaneState) {
  if (side === "left") left.value = state;
  else right.value = state;
}

function beginSelectionTransfer(source: Side) {
  const destination: Side = source === "left" ? "right" : "left";
  try {
    openConflict(buildTransferRequest(pane(source), pane(destination), "skip"), destination);
  } catch (error) {
    transferError.value = t("sftp.transferError", { error: errorText(error) });
  }
}

function openConflict(request: StartFileTransferReq, destination: Side) {
  transferError.value = "";
  conflict.value = "skip";
  pendingTransfer.value = { request, destination };
}

async function confirmTransfer() {
  const pending = pendingTransfer.value;
  if (!pending) return;
  pendingTransfer.value = null;
  await runTransfer({
    request: { ...pending.request, conflict: conflict.value },
    destination: pending.destination,
  });
}

async function runTransfer(pending: PendingTransfer) {
  transferError.value = "";
  try {
    const id = await startFileTransfer(pending.request);
    jobDestinations.set(id, pending.destination);
    const current = jobs.value.find((job) => job.id === id);
    if (current?.state === "completed") refreshDestination(id);
    else if (current) promptForJobHostKey(current);
  } catch (error) {
    transferError.value = t("sftp.transferError", { error: errorText(error) });
  }
}

async function acceptHostKey() {
  const pending = hostKey.value;
  if (!pending) return;
  hostKey.value = null;
  const accepted = {
    host: pending.prompt.host,
    fingerprint: pending.prompt.fingerprint,
  };
  if (pending.kind === "source") {
    void (pending.side === "left" ? leftPane.value : rightPane.value)?.retrySource(accepted);
    return;
  }
  await retryJob(pending.jobID, accepted);
}

function rejectHostKey() {
  hostKey.value = null;
}

function requestSourceHostKey(side: Side, payload: { hostId: string; prompt: HostKeyPrompt }) {
  hostKey.value = { kind: "source", side, prompt: payload.prompt };
}

function onInternalDrop(event: DragEvent, destination: Side) {
  event.preventDefault();
  const raw = event.dataTransfer?.getData(INTERNAL_DRAG_MIME) ?? "";
  try {
    const payload = JSON.parse(raw) as { endpoint?: FileEndpoint; paths?: unknown };
    if (!validEndpoint(payload.endpoint) || !Array.isArray(payload.paths) || !payload.paths.every((path) => typeof path === "string")) {
      throw new Error("invalid payload");
    }
    openConflict(requestFromDrop(payload.endpoint, payload.paths, destination), destination);
  } catch {
    transferError.value = t("sftp.dropError");
  }
}

function requestFromDrop(endpoint: FileEndpoint, paths: string[], destination: Side): StartFileTransferReq {
  const source = createPaneState(sourceFromEndpoint(endpoint), "/");
  source.selectedPaths = new Set(paths);
  return buildTransferRequest(source, pane(destination), "skip");
}

function sourceFromEndpoint(endpoint: FileEndpoint): SftpSource {
  return endpoint.kind === "local"
    ? { kind: "local" }
    : { kind: "ssh", hostId: endpoint.host_id ?? "" };
}

function validEndpoint(value: FileEndpoint | undefined): value is FileEndpoint {
  return !!value && (value.kind === "local" || (value.kind === "ssh" && typeof value.host_id === "string" && value.host_id !== ""));
}

function onNativeDrop(paths: string[], point?: { x: number; y: number }) {
  if (!point || paths.length === 0) return;
  const target = document.elementFromPoint(point.x, point.y)?.closest<HTMLElement>("[data-sftp-side]");
  const destination = target?.dataset.sftpSide;
  if (destination !== "left" && destination !== "right") return;
  try {
    openConflict(requestFromDrop({ kind: "local" }, paths, destination), destination);
  } catch (error) {
    transferError.value = t("sftp.transferError", { error: errorText(error) });
  }
}

function onProgress(value: unknown) {
  if (!isTransferJob(value)) return;
  mergeJob(value);
  if (value.state === "completed") refreshDestination(value.id);
  else promptForJobHostKey(value);
}

function promptForJobHostKey(job: FileTransferJob) {
  const prompt = jobHostKeyPrompt(job);
  if (!prompt) return;
  hostKey.value = { kind: "transfer", jobID: job.id, prompt };
}

function jobHostKeyPrompt(job: FileTransferJob): HostKeyPrompt | null {
  const prompt = job.host_key;
  if (!prompt?.host || !prompt.fingerprint) return null;
  return {
    host: prompt.host,
    fingerprint: prompt.fingerprint,
    hopIndex: prompt.hop_index > 0 ? prompt.hop_index : 0,
    hopName: prompt.hop_name ?? "",
  };
}

function mergeJob(incoming: FileTransferJob) {
  const index = jobs.value.findIndex((job) => job.id === incoming.id);
  if (index < 0) {
    jobs.value = [...jobs.value, normalizedJob(incoming)];
    return;
  }
  const current = jobs.value[index];
  if (!isNewerJob(incoming, current)) return;
  const next = [...jobs.value];
  next[index] = normalizedJob(incoming);
  jobs.value = next;
}

function isNewerJob(incoming: FileTransferJob, current: FileTransferJob): boolean {
  const incomingPhase = jobPhase(incoming.state);
  const currentPhase = jobPhase(current.state);
  if (incomingPhase !== currentPhase) return incomingPhase > currentPhase;
  return incoming.bytes_done >= current.bytes_done && incoming.entries_done >= current.entries_done;
}

function jobPhase(state: FileTransferJob["state"]): number {
  if (state === "queued") return 0;
  if (state === "running") return 1;
  return 2;
}

function normalizedJob(job: FileTransferJob): FileTransferJob {
  return {
    ...job,
    warnings: Array.isArray(job.warnings) ? [...job.warnings] : [],
    host_key: job.host_key ? { ...job.host_key } : undefined,
  };
}

function isTransferJob(value: unknown): value is FileTransferJob {
  if (!value || typeof value !== "object") return false;
  const job = value as Partial<FileTransferJob>;
  return typeof job.id === "string" && typeof job.state === "string";
}

function refreshDestination(id: string) {
  if (refreshedJobs.has(id)) return;
  const destination = jobDestinations.get(id);
  refreshedJobs.add(id);
  if (destination) {
    void (destination === "left" ? leftPane.value : rightPane.value)?.refresh();
    return;
  }
  void leftPane.value?.refresh();
  void rightPane.value?.refresh();
}

function cancelJob(id: string) {
  void cancelFileTransfer(id).catch((error) => {
    transferError.value = t("sftp.transferError", { error: errorText(error) });
  });
}

async function retryJob(id: string, accepted = { host: "", fingerprint: "" }) {
  transferError.value = "";
  try {
    const retryID = await retryFileTransfer(id, accepted);
    const destination = jobDestinations.get(id);
    if (destination) jobDestinations.set(retryID, destination);
    const current = jobs.value.find((job) => job.id === retryID);
    if (current?.state === "completed") refreshDestination(retryID);
    else if (current) promptForJobHostKey(current);
  } catch (error) {
    transferError.value = t("sftp.transferError", { error: errorText(error) });
  }
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

async function reloadHosts() {
  try {
    hosts.value = await listSFTPHosts();
  } catch (error) {
    workspaceError.value = t("sftp.loadError", { error: errorText(error) });
  }
}

watch(() => props.active, (active, previous) => {
  if (active && previous === false) void reloadHosts();
});

onMounted(async () => {
  stopProgress = $platform.events.on("sftp:transfer-progress", onProgress);
  const listPromise = listFileTransfers();
  stopNativeDrop = $platform.nativeFileDrop?.onFileDrop(onNativeDrop);
  try {
    const [loadedJobs, loadedHosts, home] = await Promise.all([
      listPromise,
      listSFTPHosts(),
      getUserHomeDir(),
    ]);
    for (const job of loadedJobs) mergeJob(job);
    hosts.value = loadedHosts;
    localHome.value = home || "/";
    if (left.value.source.kind === "local" && left.value.cwd === "/") left.value = createPaneState({ kind: "local" }, localHome.value);
    if (right.value.source.kind === "local" && right.value.cwd === "/") right.value = createPaneState({ kind: "local" }, localHome.value);
  } catch (error) {
    workspaceError.value = t("sftp.loadError", { error: errorText(error) });
  }
});

onBeforeUnmount(() => {
  stopProgress?.();
  stopNativeDrop?.();
  localFS.dispose?.();
});
</script>

<template>
  <section class="sftp-workspace" aria-label="SFTP">
    <p v-if="workspaceError" class="workspace-error">{{ workspaceError }}</p>
    <p v-if="transferError" class="workspace-error">{{ transferError }}</p>
    <div class="workspace-grid">
      <div data-test="drop-left" data-sftp-side="left" class="pane-drop" @dragover.prevent @drop="onInternalDrop($event, 'left')">
        <SftpPane
          ref="leftPane"
          side="left"
          :state="left"
          :hosts="hosts"
          :local-f-s="localFS"
          :local-home="localHome"
          @update:state="setPane('left', $event)"
          @transfer-selection="beginSelectionTransfer('left')"
          @host-key-required="requestSourceHostKey('left', $event)"
          @edit-host="emit('edit-host', $event)"
        />
      </div>
      <div class="transfer-controls">
        <button data-test="transfer-left" v-tooltip="t('sftp.transferLeft')" :disabled="!canTransferLeft" @click="beginSelectionTransfer('right')"><ArrowLeft :size="18" /></button>
        <button data-test="transfer-right" v-tooltip="t('sftp.transferRight')" :disabled="!canTransferRight" @click="beginSelectionTransfer('left')"><ArrowRight :size="18" /></button>
      </div>
      <div data-test="drop-right" data-sftp-side="right" class="pane-drop" @dragover.prevent @drop="onInternalDrop($event, 'right')">
        <SftpPane
          ref="rightPane"
          side="right"
          :state="right"
          :hosts="hosts"
          :local-f-s="localFS"
          :local-home="localHome"
          @update:state="setPane('right', $event)"
          @transfer-selection="beginSelectionTransfer('right')"
          @host-key-required="requestSourceHostKey('right', $event)"
          @edit-host="emit('edit-host', $event)"
        />
      </div>
    </div>

    <SftpTransferQueue
      :jobs="jobs"
      @cancel="cancelJob"
      @retry="retryJob"
      @edit-host="emit('edit-host', $event)"
    />

    <div v-if="pendingTransfer" class="modal-backdrop" role="dialog" :aria-label="t('sftp.conflictTitle')">
      <div class="modal">
        <h2>{{ t("sftp.conflictTitle") }}</h2>
        <p>{{ t("sftp.conflictPrompt") }}</p>
        <label><input data-test="conflict-skip" v-model="conflict" type="radio" value="skip" />{{ t("sftp.conflictSkip") }}</label>
        <label><input data-test="conflict-replace" v-model="conflict" type="radio" value="replace" />{{ t("sftp.conflictReplace") }}</label>
        <label><input data-test="conflict-rename" v-model="conflict" type="radio" value="rename" />{{ t("sftp.conflictRename") }}</label>
        <div class="modal-actions">
          <button @click="pendingTransfer = null">{{ t("common.cancel") }}</button>
          <button data-test="conflict-confirm" class="primary" @click="confirmTransfer">{{ t("sftp.startTransfer") }}</button>
        </div>
      </div>
    </div>

    <div v-if="hostKey" class="modal-backdrop" role="dialog" :aria-label="t('sftp.hostKeyTitle')" data-test="sftp-host-tofu">
      <div class="modal">
        <h2>{{ t("sftp.hostKeyTitle") }}</h2>
        <p>{{ t("sftp.hostKeyPrompt") }}</p>
        <code>{{ hostKey.prompt.host }}</code>
        <code>{{ hostKey.prompt.fingerprint }}</code>
        <div class="modal-actions">
          <button @click="rejectHostKey">{{ t("sftp.rejectHostKey") }}</button>
          <button data-test="sftp-accept-hostkey" class="primary" @click="acceptHostKey">{{ t("sftp.acceptHostKey") }}</button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.sftp-workspace { position: relative; min-width: 0; min-height: 0; height: 100%; display: flex; flex-direction: column; background: #0d1117; color: #c9d1d9; }
.workspace-grid { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(0, 1fr) 48px minmax(0, 1fr); gap: 8px; padding: 8px; }
.pane-drop { --wails-drop-target: drop; min-width: 0; min-height: 0; }
.pane-drop > :deep(.sftp-pane) { width: 100%; height: 100%; }
.transfer-controls { display: flex; flex-direction: column; justify-content: center; gap: 8px; }
.transfer-controls button { width: 40px; height: 36px; display: grid; place-content: center; border: 1px solid #30363d; border-radius: 5px; background: #21262d; color: #c9d1d9; cursor: pointer; }
.transfer-controls button:hover:not(:disabled) { background: #30363d; color: #f0f6fc; }
.transfer-controls button:disabled { opacity: .35; cursor: default; }
.workspace-error { flex: 0 0 auto; margin: 0; padding: 7px 12px; border-bottom: 1px solid #6e3630; background: rgba(248, 81, 73, .09); color: #ff7b72; font-size: 12px; }
.modal-backdrop { position: absolute; inset: 0; z-index: 20; display: grid; place-items: center; padding: 16px; background: rgba(1, 4, 9, .72); }
.modal { width: min(420px, 100%); display: grid; gap: 10px; padding: 18px; border: 1px solid #30363d; border-radius: 7px; background: #21262d; box-shadow: 0 16px 42px rgba(0,0,0,.48); }
.modal h2 { margin: 0; font-size: 16px; letter-spacing: 0; }
.modal p { margin: 0; color: #b1bac4; font-size: 13px; }
.modal label { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.modal code { display: block; overflow-wrap: anywhere; padding: 7px 9px; border: 1px solid #30363d; border-radius: 4px; background: #0d1117; color: #f0f6fc; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 6px; }
.modal-actions button { height: 30px; padding: 0 12px; border: 1px solid #30363d; border-radius: 5px; background: #21262d; color: #c9d1d9; cursor: pointer; }
.modal-actions .primary { border-color: #2f81f7; background: #1f6feb; color: white; }
@media (max-width: 760px) {
  .workspace-grid { grid-template-columns: minmax(0, 1fr); grid-template-rows: minmax(260px, 1fr) 44px minmax(260px, 1fr); }
  .transfer-controls { flex-direction: row; }
  .transfer-controls button:first-child { order: 2; transform: rotate(90deg); }
  .transfer-controls button:last-child { order: 1; transform: rotate(90deg); }
}
</style>
