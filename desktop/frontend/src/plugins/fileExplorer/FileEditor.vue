<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView, lineNumbers } from "@codemirror/view";
import { usePlatform } from "../../platform";
import { languageForPath } from "./languageMap";
import { highlightExtensionFor } from "./highlight";

const platform = usePlatform();
const fs = platform.pluginHost!.fs;

const MAX_BYTES_FRONTEND = 2 * 1024 * 1024;

const props = defineProps<{
  path: string;
  showLineNumbers: boolean;
  theme: "dimmed" | "light";
}>();

const host = ref<HTMLDivElement | null>(null);
const state = ref<"loading" | "tooLarge" | "binary" | "ok" | "error">("loading");
const errorMsg = ref<string>("");
const reloadPending = ref(false);
const loadedAt = ref<number | null>(null);

let view: EditorView | null = null;
let off: (() => void) | null = null;

function cssVar(name: string, fallback: string): string {
  if (typeof window === "undefined" || !host.value) return fallback;
  return getComputedStyle(host.value).getPropertyValue(name).trim() || fallback;
}

// Wails serializes Go []byte as a base64 string over JSON. Older runtimes
// may also pass through a Uint8Array or number[]. Decode all three to text.
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
    throw new Error("Unexpected file content type");
  }
  return new TextDecoder().decode(bytes);
}

function makeThemeExt(): Extension {
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
    { dark: props.theme !== "light" },
  );
}

async function load() {
  state.value = "loading";
  view?.destroy();
  view = null;
  try {
    const meta = (await fs.fileMeta(props.path)) as any;
    loadedAt.value = meta.modTime;
    reloadPending.value = false;
    if (meta.isBinary) { state.value = "binary"; return; }
    if (meta.size > MAX_BYTES_FRONTEND) { state.value = "tooLarge"; return; }
    const result = (await fs.readFile(props.path, MAX_BYTES_FRONTEND)) as any;
    const text = decodeFileBytes(result.data);
    state.value = "ok";

    const exts: Extension[] = [
      EditorView.editable.of(false),
      EditorState.readOnly.of(true),
      makeThemeExt(),
      highlightExtensionFor(props.theme),
    ];
    if (props.showLineNumbers) exts.push(lineNumbers());
    const langExt = await languageForPath(props.path);
    if (langExt) exts.push(langExt);

    if (!host.value) return;
    view = new EditorView({
      state: EditorState.create({ doc: text, extensions: exts }),
      parent: host.value,
    });
  } catch (err) {
    state.value = "error";
    errorMsg.value = (err as Error).message;
  }
}

onMounted(() => {
  void load();
  off = platform.events.on("plugin-fs:dir-changed", async (data: unknown) => {
    const dir = data as string;
    if (!props.path.startsWith(dir + "/") && props.path !== dir) return;
    try {
      const meta = (await fs.fileMeta(props.path)) as any;
      if (loadedAt.value && meta.modTime > loadedAt.value) {
        reloadPending.value = true;
      }
    } catch { /* ignore */ }
  });
});

watch(
  () => [props.path, props.showLineNumbers, props.theme],
  () => { void load(); },
);

onBeforeUnmount(() => {
  view?.destroy();
  view = null;
  if (off) off();
});
</script>

<template>
  <div class="file-editor">
    <div v-if="reloadPending" class="reload-badge">
      File changed on disk
      <button @click="load">Reload</button>
    </div>
    <div v-if="state === 'tooLarge'" class="banner muted">File too large to preview. Open externally.</div>
    <div v-else-if="state === 'binary'" class="banner muted">Binary file — no preview.</div>
    <div v-else-if="state === 'error'" class="banner err">Error: {{ errorMsg }}</div>
    <div v-else-if="state === 'loading'" class="banner muted">Loading…</div>
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
/* CodeMirror's internal scrollable element (.cm-scroller) and the host's own
   scrollbar both need to track the theme — otherwise macOS paints a default
   light bar on the dark editor (and vice versa). */
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
.banner { padding: 18px 20px; font-size: 13px; }
.muted { color: var(--ed-muted, rgba(173, 186, 199, 0.5)); }
.err { color: var(--ed-error, #f47067); }
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
