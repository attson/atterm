<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView, lineNumbers } from "@codemirror/view";
import { ReadFile, FileMeta } from "../../../wailsjs/go/main/PluginFS";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { languageForPath } from "./languageMap";

const MAX_BYTES_FRONTEND = 2 * 1024 * 1024;

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

const props = defineProps<{
  path: string;
}>();

const host = ref<HTMLDivElement | null>(null);
const state = ref<"loading" | "tooLarge" | "binary" | "ok" | "error">("loading");
const errorMsg = ref<string>("");
const reloadPending = ref(false);
const loadedAt = ref<number | null>(null);

let view: EditorView | null = null;
let off: (() => void) | null = null;

async function load() {
  state.value = "loading";
  view?.destroy();
  view = null;
  try {
    const meta = (await FileMeta(props.path)) as any;
    loadedAt.value = meta.modTime;
    reloadPending.value = false;
    if (meta.isBinary) {
      state.value = "binary";
      return;
    }
    if (meta.size > MAX_BYTES_FRONTEND) {
      state.value = "tooLarge";
      return;
    }
    const result = (await ReadFile(props.path, MAX_BYTES_FRONTEND)) as any;
    const text = decodeFileBytes(result.data);
    state.value = "ok";

    const exts: Extension[] = [
      lineNumbers(),
      EditorView.editable.of(false),
      EditorState.readOnly.of(true),
    ];
    const langExt = await languageForPath(props.path);
    if (langExt) exts.push(langExt);

    const newState = EditorState.create({ doc: text, extensions: exts });
    if (!host.value) return;
    view = new EditorView({ state: newState, parent: host.value });
  } catch (err) {
    state.value = "error";
    errorMsg.value = (err as Error).message;
  }
}

onMounted(() => {
  void load();
  off = EventsOn("plugin-fs:dir-changed", async (dir: string) => {
    if (!props.path.startsWith(dir + "/") && props.path !== dir) return;
    try {
      const meta = (await FileMeta(props.path)) as any;
      if (loadedAt.value && meta.modTime > loadedAt.value) {
        reloadPending.value = true;
      }
    } catch { /* ignore */ }
  });
});

watch(() => props.path, () => {
  void load();
});

onBeforeUnmount(() => {
  view?.destroy();
  view = null;
  if (off) off();
});
</script>

<template>
  <div class="file-editor">
    <div v-if="state === 'tooLarge'" class="banner">File too large to preview. Open externally.</div>
    <div v-if="state === 'binary'" class="banner">Binary file.</div>
    <div v-if="state === 'error'" class="banner err">Error: {{ errorMsg }}</div>
    <div v-if="reloadPending" class="reload-badge">
      File changed on disk
      <button @click="load">Reload</button>
    </div>
    <div v-show="state === 'ok'" ref="host" class="cm-host" />
    <div v-if="state === 'loading'" class="banner">Loading…</div>
  </div>
</template>

<style scoped>
.file-editor { height: 100%; display: flex; flex-direction: column; }
.cm-host { flex: 1; overflow: auto; }
.banner { padding: 10px 12px; font-size: 12px; opacity: 0.7; }
.banner.err { color: #f85149; opacity: 1; }
.reload-badge { display: flex; align-items: center; gap: 8px; padding: 4px 10px; background: #1f2937; border-bottom: 1px solid #2d333b; font-size: 11px; }
.reload-badge button { background: #21262d; border: 1px solid #2d333b; color: #c9d1d9; padding: 1px 8px; border-radius: 3px; cursor: pointer; }
</style>
