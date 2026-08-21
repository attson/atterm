<script lang="ts" setup>
// Settings -> Config export & import (roadmap item config-export-import,
// task 4). Desktop-only: ExportConfig opens a native save dialog and
// Preview/ApplyConfigImport are plain Wails bindings, none of which exist on
// web/Capacitor -- web/vite.config.ts aliases the web build's `@` to this
// same desktop/frontend/src tree, and Capacitor mounts the identical shell,
// so this component is reachable from both unless it guards itself. `enabled`
// short-circuits before any binding is ever called and the template's root
// v-if renders nothing at all when it's false -- see SyncStatusIndicator.vue
// for the same shape of gate (and why "renders nothing" beats a
// confidently-broken UI: that component's own comment cites two prior fix
// rounds for exactly this class of mistake). SettingsDialog.vue's
// diag-merged block also only mounts this component when
// `caps.wailsBindings` is true (the whole block, alongside Logging and
// Diagnostics) as defense in depth; that external gate is not the only one.
import { computed, ref } from "vue";
import { useI18n } from "../i18n/useI18n";
import { usePlatform } from "../platform";
import {
  exportConfig,
  previewConfigImport,
  applyConfigImport,
  type ImportChange,
  type ImportPreview,
  type ImportReport,
} from "../lib/api";

const { t } = useI18n();
const platform = usePlatform();
const enabled = platform.caps.wailsBindings;

// ---- export ----

// Defaults OFF: env vars frequently hold API tokens, and a profile with
// sync_env === false has already told atterm once that its env should stay
// on this machine -- exporting it anyway by default would go behind that
// choice. See desktop/config_export.go's BuildConfigExport doc comment.
const includeLocalEnv = ref(false);
const exporting = ref(false);
const exportStatus = ref("");
const exportError = ref("");

async function doExport(): Promise<void> {
  if (!enabled || exporting.value) return;
  exporting.value = true;
  exportStatus.value = "";
  exportError.value = "";
  try {
    const path = await exportConfig(includeLocalEnv.value);
    // ExportConfig mirrors ExportDiagnostics's cancel semantics: an empty
    // path with no thrown error means the user dismissed the save dialog,
    // not a failure worth reporting (see SettingsDiagnostics.vue's
    // exportToFile for the same convention).
    if (path) {
      exportStatus.value = t("settings.configio.export.success", { path });
    }
  } catch (e) {
    exportError.value = e instanceof Error ? e.message : String(e);
  } finally {
    exporting.value = false;
  }
}

// ---- import ----

const fileInput = ref<HTMLInputElement | null>(null);
const pickedFileName = ref("");
const importText = ref("");
const previewLoading = ref(false);
const preview = ref<ImportPreview | null>(null);
const previewError = ref("");
const applying = ref(false);
const applyError = ref("");
const report = ref<ImportReport | null>(null);

const addedChanges = computed<ImportChange[]>(() => preview.value?.changes.filter((c) => c.action === "add") ?? []);
const replacedChanges = computed<ImportChange[]>(() => preview.value?.changes.filter((c) => c.action === "replace") ?? []);
const unchangedChanges = computed<ImportChange[]>(() => preview.value?.changes.filter((c) => c.action === "unchanged") ?? []);
const hasChanges = computed(() => (preview.value?.changes.length ?? 0) > 0);
// A preview with nothing to apply (e.g. re-importing a file onto its own
// source machine) is still a legitimate outcome -- Apply stays enabled for
// it rather than being blocked here; applyConfigImport is defined to no-op
// cleanly on an empty change set.
const canApply = computed(() => enabled && !!preview.value && !!importText.value && !applying.value);

function pickFile(): void {
  if (!enabled) return;
  fileInput.value?.click();
}

// Blob.prototype.text() is missing on older WebKit (and in jsdom); fall back
// to FileReader. Mirrors lib/sshKeyFile.ts's private readFileText -- same
// shape, kept local here since the two call sites want different error
// handling around the read (that one validates a PEM, this one hands the
// raw text straight to PreviewConfigImport).
function readFileAsText(file: File): Promise<string> {
  if (typeof file.text === "function") return file.text();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ""));
    reader.onerror = () => reject(reader.error ?? new Error(t("settings.configio.import.fileReadError")));
    reader.readAsText(file);
  });
}

async function onFilePicked(e: Event): Promise<void> {
  const input = e.target as HTMLInputElement;
  const file = input.files?.[0];
  // Clear so re-picking the same path fires change again (SshHostsPanel.vue
  // does the same after its key-file input).
  input.value = "";
  if (!file) return;
  await loadFile(file);
}

async function loadFile(file: File): Promise<void> {
  resetImportState();
  pickedFileName.value = file.name;
  previewLoading.value = true;
  try {
    const text = await readFileAsText(file);
    // PreviewConfigImport never touches the config store (see its Go doc
    // comment) -- this is the only binding call this function makes.
    // ApplyConfigImport is never called from here, only from confirmApply()
    // below, and only after the user clicks the Apply button explicitly.
    const p = await previewConfigImport(text);
    importText.value = text;
    preview.value = p;
  } catch (e) {
    previewError.value = e instanceof Error ? e.message : String(e);
  } finally {
    previewLoading.value = false;
  }
}

function resetImportState(): void {
  pickedFileName.value = "";
  importText.value = "";
  preview.value = null;
  previewError.value = "";
  applying.value = false;
  applyError.value = "";
  report.value = null;
}

async function confirmApply(): Promise<void> {
  if (!canApply.value) return;
  applying.value = true;
  applyError.value = "";
  try {
    // Re-uses the exact text PreviewConfigImport was already called with,
    // never a re-read of the file -- what the user was shown and what gets
    // applied must be the same bytes (mirrors ApplyConfigImport's own Go-side
    // guarantee: it re-parses jsonText independently of the cached preview).
    report.value = await applyConfigImport(importText.value);
    // The preview it was computed from is now stale/consumed -- show the
    // report instead of the preview again, so "what would happen" and "what
    // happened" are never rendered as the same screen.
    preview.value = null;
  } catch (e) {
    applyError.value = e instanceof Error ? e.message : String(e);
  } finally {
    applying.value = false;
  }
}

function cancelImport(): void {
  resetImportState();
}
</script>

<template>
  <div v-if="enabled" class="configio" data-testid="configio-panel">
    <section class="configio-section">
      <h4 class="section-title">{{ t("settings.configio.export.title") }}</h4>
      <p class="hint">{{ t("settings.configio.export.hint") }}</p>
      <label class="checkbox">
        <input type="checkbox" v-model="includeLocalEnv" data-testid="configio-env-checkbox" />
        {{ t("settings.configio.export.includeEnvLabel") }}
      </label>
      <div class="row">
        <button type="button" data-testid="configio-export-button" :disabled="exporting" @click="doExport">
          {{ exporting ? t("settings.configio.export.exporting") : t("settings.configio.export.button") }}
        </button>
        <span v-if="exportStatus" class="status" data-testid="configio-export-status">{{ exportStatus }}</span>
        <span v-if="exportError" class="status error" data-testid="configio-export-error">{{ exportError }}</span>
      </div>
    </section>

    <section class="configio-section">
      <h4 class="section-title">{{ t("settings.configio.import.title") }}</h4>
      <p class="hint">{{ t("settings.configio.import.hint") }}</p>
      <input
        ref="fileInput" class="file-input" type="file" accept="application/json,.json"
        data-testid="configio-import-file-input" @change="onFilePicked"
      />
      <div class="row">
        <button type="button" data-testid="configio-import-pick-button" @click="pickFile">
          {{ t("settings.configio.import.pickFile") }}
        </button>
        <span v-if="pickedFileName" class="status" data-testid="configio-import-filename">
          {{ t("settings.configio.import.fileLabel", { name: pickedFileName }) }}
        </span>
      </div>
      <p v-if="previewLoading" class="status" data-testid="configio-import-loading">
        {{ t("settings.configio.import.previewing") }}
      </p>
      <p v-if="previewError" class="status error" data-testid="configio-preview-error">
        {{ t("settings.configio.import.previewError", { error: previewError }) }}
      </p>

      <div v-if="preview" class="configio-preview" data-testid="configio-preview">
        <h5>{{ t("settings.configio.import.previewTitle") }}</h5>
        <p v-if="!hasChanges" class="hint" data-testid="configio-preview-empty">
          {{ t("settings.configio.import.noChanges") }}
        </p>
        <div v-if="addedChanges.length" data-testid="configio-changes-add">
          <h6>{{ t("settings.configio.import.changesAdd") }} ({{ addedChanges.length }})</h6>
          <ul>
            <li v-for="c in addedChanges" :key="c.key">{{ c.key }}<template v-if="c.detail"> — {{ c.detail }}</template></li>
          </ul>
        </div>
        <div v-if="replacedChanges.length" data-testid="configio-changes-replace">
          <h6>{{ t("settings.configio.import.changesReplace") }} ({{ replacedChanges.length }})</h6>
          <ul>
            <li v-for="c in replacedChanges" :key="c.key">{{ c.key }}<template v-if="c.detail"> — {{ c.detail }}</template></li>
          </ul>
        </div>
        <div v-if="unchangedChanges.length" data-testid="configio-changes-unchanged">
          <h6>{{ t("settings.configio.import.changesUnchanged") }} ({{ unchangedChanges.length }})</h6>
          <ul>
            <li v-for="c in unchangedChanges" :key="c.key">{{ c.key }}</li>
          </ul>
        </div>
        <div v-if="preview.skipped.length" data-testid="configio-skipped">
          <h6>{{ t("settings.configio.import.skippedTitle") }} ({{ preview.skipped.length }})</h6>
          <ul>
            <li v-for="(reason, i) in preview.skipped" :key="i">{{ reason }}</li>
          </ul>
        </div>
        <div class="row">
          <button type="button" data-testid="configio-apply-button" :disabled="!canApply" @click="confirmApply">
            {{ applying ? t("settings.configio.import.applying") : t("settings.configio.import.apply") }}
          </button>
          <button type="button" data-testid="configio-cancel-button" @click="cancelImport">{{ t("common.cancel") }}</button>
        </div>
        <p v-if="applyError" class="status error" data-testid="configio-apply-error">
          {{ t("settings.configio.import.applyError", { error: applyError }) }}
        </p>
      </div>

      <div v-if="report" class="configio-report" data-testid="configio-report">
        <h5>{{ t("settings.configio.import.appliedTitle") }}</h5>
        <p data-testid="configio-report-summary">
          {{ t("settings.configio.import.appliedSummary", { count: report.applied.length }) }}
        </p>
        <ul data-testid="configio-report-applied">
          <li v-for="c in report.applied" :key="c.key">{{ c.key }}<template v-if="c.detail"> — {{ c.detail }}</template></li>
        </ul>
        <div v-if="report.skipped.length" data-testid="configio-report-skipped">
          <h6>{{ t("settings.configio.import.skippedTitle") }} ({{ report.skipped.length }})</h6>
          <ul>
            <li v-for="(reason, i) in report.skipped" :key="i">{{ reason }}</li>
          </ul>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.configio { display: flex; flex-direction: column; gap: 16px; margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--border); }
.configio-section { display: flex; flex-direction: column; gap: 8px; }
.section-title { margin: 0; font-size: 13px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.5; }
.checkbox { display: flex; align-items: center; gap: 6px; font-size: 12px; }
.row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.status { font-size: 12px; color: var(--fg-dim); }
.status.error { color: var(--bad); }
.file-input { display: none; }
.configio-preview, .configio-report {
  border: 1px solid var(--border); border-radius: 6px; padding: 10px;
  background: var(--bg); display: flex; flex-direction: column; gap: 6px;
}
.configio-preview h5, .configio-report h5 { margin: 0; font-size: 12px; }
.configio-preview h6, .configio-report h6 { margin: 4px 0 2px; font-size: 11px; color: var(--fg-dim); }
.configio-preview ul, .configio-report ul { margin: 0; padding-left: 18px; font-size: 12px; }
button { height: 30px; border: 1px solid var(--accent); border-radius: 7px; background: var(--accent); color: var(--bg); padding: 0 12px; font-size: 12px; font-weight: 700; cursor: pointer; }
button:disabled { opacity: 0.5; cursor: default; }
</style>
