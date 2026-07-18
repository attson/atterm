<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView, keymap, lineNumbers } from "@codemirror/view";
import { history, defaultKeymap, historyKeymap } from "@codemirror/commands";
import { languageForPath } from "./languageMap";
import { highlightExtensionFor } from "./highlight";
import { useI18n } from "../../i18n/useI18n";
import type { FileSystemBridge } from "./fsBridge";

const { t } = useI18n();

const MAX_BYTES_FRONTEND = 2 * 1024 * 1024;

const props = defineProps<{
  fs: FileSystemBridge;
  path: string;
  showLineNumbers: boolean;
  theme: "dimmed" | "light";
}>();

const emit = defineEmits<{
  (e: "dirty-change", dirty: boolean): void;
}>();

const host = ref<HTMLDivElement | null>(null);
const state = ref<"loading" | "tooLarge" | "binary" | "ok" | "error">("loading");
const errorMsg = ref<string>("");
const reloadPending = ref(false);
const loadedAt = ref<number | null>(null);
const dirty = ref(false);
const conflict = ref<{ currentModTime: number } | null>(null);
const saveError = ref<string>("");

let view: EditorView | null = null;
let off: (() => void) | null = null;
let disposed = false;
let loadGeneration = 0;
let originalText = "";

function isCurrent(
  fs: FileSystemBridge,
  path: string,
  showLineNumbers: boolean,
  theme: "dimmed" | "light",
  request: number,
): boolean {
  return !disposed
    && loadGeneration === request
    && props.fs === fs
    && props.path === path
    && props.showLineNumbers === showLineNumbers
    && props.theme === theme;
}

function cssVar(name: string, fallback: string): string {
  if (typeof window === "undefined" || !host.value) return fallback;
  return getComputedStyle(host.value).getPropertyValue(name).trim() || fallback;
}

function decodeFileBytes(data: unknown): string {
  let bytes: Uint8Array;
  if (typeof data === "string") {
    const bin = atob(data);
    bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  } else if (data instanceof Uint8Array) {
    bytes = data;
  } else if (Array.isArray(data)) {
    bytes = new Uint8Array(data as number[]);
  } else {
    throw new Error(t("plugins.fileExplorer.unexpectedContentType"));
  }
  return new TextDecoder().decode(bytes);
}

function makeThemeExt(theme: "dimmed" | "light"): Extension {
  return EditorView.theme(
    {
      "&": {
        backgroundColor: cssVar("--ed-editor-bg", "#22272e"),
        color: cssVar("--ed-editor-fg", "#adbac7"),
        height: "100%",
      },
      ".cm-gutters": {
        backgroundColor: cssVar("--ed-gutter-bg", "#22272e"),
        color: cssVar("--ed-gutter-fg", "#545d68"),
        border: "none",
      },
      ".cm-activeLine": { backgroundColor: "transparent" },
      ".cm-activeLineGutter": { backgroundColor: "transparent" },
      ".cm-content": {
        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
        fontSize: "13px",
        padding: "8px 0",
      },
      ".cm-lineNumbers .cm-gutterElement": { padding: "0 8px 0 14px" },
    },
    { dark: theme !== "light" },
  );
}

function setDirty(next: boolean) {
  if (dirty.value === next) return;
  dirty.value = next;
  emit("dirty-change", next);
}

async function load() {
  const fs = props.fs;
  const path = props.path;
  const showLineNumbers = props.showLineNumbers;
  const theme = props.theme;
  const request = ++loadGeneration;
  if (disposed) return;
  state.value = "loading";
  view?.destroy();
  view = null;
  try {
    const meta = (await fs.fileMeta(path)) as { size: number; modTime: number; isBinary: boolean };
    if (!isCurrent(fs, path, showLineNumbers, theme, request)) return;
    loadedAt.value = meta.modTime;
    reloadPending.value = false;
    if (meta.isBinary) { state.value = "binary"; setDirty(false); return; }
    if (meta.size > MAX_BYTES_FRONTEND) { state.value = "tooLarge"; setDirty(false); return; }
    const result = (await fs.readFile(path, MAX_BYTES_FRONTEND)) as { data: unknown };
    if (!isCurrent(fs, path, showLineNumbers, theme, request)) return;
    const text = decodeFileBytes(result.data);

    originalText = text;
    setDirty(false);
    conflict.value = null;
    saveError.value = "";

    const dirtyListener = EditorView.updateListener.of((v) => {
      if (!v.docChanged) return;
      setDirty(v.state.doc.toString() !== originalText);
    });

    const saveKey = keymap.of([
      {
        key: "Mod-s",
        preventDefault: true,
        run: () => {
          void save();
          return true;
        },
      },
    ]);

    const exts: Extension[] = [
      makeThemeExt(theme),
      highlightExtensionFor(theme),
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      saveKey,
      dirtyListener,
    ];
    if (showLineNumbers) exts.push(lineNumbers());
    const langExt = await languageForPath(path);
    if (langExt) exts.push(langExt);

    if (!isCurrent(fs, path, showLineNumbers, theme, request) || !host.value) return;
    const nextView = new EditorView({
      state: EditorState.create({ doc: text, extensions: exts }),
      parent: host.value,
    });
    if (!isCurrent(fs, path, showLineNumbers, theme, request)) {
      nextView.destroy();
      return;
    }
    view = nextView;
    state.value = "ok";
  } catch (err) {
    if (!isCurrent(fs, path, showLineNumbers, theme, request)) return;
    state.value = "error";
    errorMsg.value = (err as Error).message;
  }
}

async function save(): Promise<boolean> {
  if (!view || state.value !== "ok") return false;
  const text = view.state.doc.toString();
  const bytes = new TextEncoder().encode(text);
  saveError.value = "";
  try {
    const meta = await props.fs.writeFile(props.path, bytes, loadedAt.value);
    originalText = text;
    loadedAt.value = meta.modTime;
    reloadPending.value = false;
    conflict.value = null;
    setDirty(false);
    return true;
  } catch (err) {
    const msg = (err as Error).message ?? "";
    const m = /stale_modtime: current=(\d+)/.exec(msg);
    if (m) {
      conflict.value = { currentModTime: Number(m[1]) };
    } else {
      saveError.value = msg;
    }
    return false;
  }
}

async function overwriteWithServerModTime() {
  if (!conflict.value) return;
  loadedAt.value = conflict.value.currentModTime;
  conflict.value = null;
  await save();
}

function reloadDiscardChanges() {
  conflict.value = null;
  void load();
}

async function checkDirChanged(fs: FileSystemBridge, dir: string) {
  const path = props.path;
  const request = loadGeneration;
  if (!path.startsWith(dir + "/") && path !== dir) return;
  try {
    const meta = (await fs.fileMeta(path)) as { modTime: number };
    if (disposed || props.fs !== fs || props.path !== path || loadGeneration !== request) return;
    if (loadedAt.value && meta.modTime > loadedAt.value) reloadPending.value = true;
  } catch { /* ignore */ }
}

function subscribeToDirChanges(fs: FileSystemBridge) {
  off?.();
  off = fs.onDirChanged((dir) => {
    void checkDirChanged(fs, dir);
  });
}

onMounted(() => {
  disposed = false;
  void load();
  subscribeToDirChanges(props.fs);
});

watch(
  () => [props.path, props.fs, props.showLineNumbers, props.theme],
  () => { void load(); },
);

watch(() => props.fs, (fs) => subscribeToDirChanges(fs));

onBeforeUnmount(() => {
  disposed = true;
  loadGeneration++;
  view?.destroy();
  view = null;
  if (off) off();
});

// testAppend is used by unit tests to simulate a doc edit without a live
// DOM keyboard event. It's a thin no-op wrapper in production.
defineExpose({
  save,
  testAppend: (text: string) => {
    if (!view) return;
    view.dispatch({ changes: { from: view.state.doc.length, insert: text } });
  },
});
</script>

<template>
  <div class="file-editor">
    <div v-if="conflict" class="banner err conflict" data-test="conflict-banner">
      <span class="msg">{{ t("plugins.fileExplorer.staleModTime") }}</span>
      <button data-test="conflict-overwrite" @click="overwriteWithServerModTime">
        {{ t("plugins.fileExplorer.overwrite") }}
      </button>
      <button data-test="conflict-reload" @click="reloadDiscardChanges">
        {{ t("plugins.fileExplorer.reloadDiscard") }}
      </button>
      <button data-test="conflict-cancel" @click="conflict = null">
        {{ t("plugins.fileExplorer.cancel") }}
      </button>
    </div>
    <div v-if="saveError" class="banner err" data-test="save-error">
      {{ t("plugins.fileExplorer.saveFailed", { message: saveError }) }}
    </div>
    <div v-if="reloadPending" class="reload-badge">
      {{ t("plugins.fileExplorer.fileChanged") }}
      <button @click="load">
        {{ dirty ? t("plugins.fileExplorer.reloadDiscard") : t("plugins.fileExplorer.reload") }}
      </button>
    </div>
    <div v-if="state === 'tooLarge'" class="banner muted">{{ t("plugins.fileExplorer.tooLarge") }}</div>
    <div v-else-if="state === 'binary'" class="banner muted">{{ t("plugins.fileExplorer.binary") }}</div>
    <div v-else-if="state === 'error'" class="banner err">{{ t("plugins.fileExplorer.errorPrefix", { message: errorMsg }) }}</div>
    <div v-else-if="state === 'loading'" class="banner muted">{{ t("common.loading") }}</div>
    <div v-show="state === 'ok'" ref="host" class="cm-host" />
  </div>
</template>

<style scoped>
.file-editor {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--ed-editor-bg, #22272e);
}
.cm-host { flex: 1 1 auto; overflow: auto; }
.cm-host::-webkit-scrollbar,
.cm-host :deep(.cm-scroller)::-webkit-scrollbar {
  width: 12px;
  height: 12px;
}
.cm-host::-webkit-scrollbar-track,
.cm-host :deep(.cm-scroller)::-webkit-scrollbar-track {
  background: var(--ed-editor-bg, #22272e);
}
.cm-host::-webkit-scrollbar-thumb,
.cm-host :deep(.cm-scroller)::-webkit-scrollbar-thumb {
  background: var(--ed-row-hover, rgba(173, 186, 199, 0.2));
  border-radius: 6px;
  border: 3px solid var(--ed-editor-bg, #22272e);
}
.cm-host::-webkit-scrollbar-thumb:hover,
.cm-host :deep(.cm-scroller)::-webkit-scrollbar-thumb:hover {
  background: var(--ed-chevron, rgba(173, 186, 199, 0.4));
}
.cm-host::-webkit-scrollbar-corner,
.cm-host :deep(.cm-scroller)::-webkit-scrollbar-corner {
  background: var(--ed-editor-bg, #22272e);
}
.banner { padding: 8px 12px; font-size: 13px; }
.muted { color: var(--ed-muted, rgba(173, 186, 199, 0.5)); }
.err { color: var(--ed-error, #f47067); }
.conflict {
  display: flex;
  align-items: center;
  gap: 8px;
  border-bottom: 1px solid var(--ed-border, #444c56);
}
.conflict .msg { flex: 1 1 auto; }
.conflict button {
  background: var(--ed-shell-bg, #22272e);
  border: 1px solid var(--ed-border, #444c56);
  color: var(--ed-row-fg, #adbac7);
  padding: 1px 8px;
  border-radius: 3px;
  cursor: pointer;
}
.conflict button:hover { background: var(--ed-row-hover, rgba(173, 186, 199, 0.1)); }
.reload-badge {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px;
  background: var(--ed-tab-bg, #2d333b);
  border-bottom: 1px solid var(--ed-border, #444c56);
  font-size: 11px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.7));
}
.reload-badge button {
  background: var(--ed-shell-bg, #22272e);
  border: 1px solid var(--ed-border, #444c56);
  color: var(--ed-row-fg, #adbac7);
  padding: 1px 8px;
  border-radius: 3px;
  cursor: pointer;
}
</style>
