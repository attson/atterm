<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView, lineNumbers } from "@codemirror/view";
import { ReadFile, FileMeta } from "../../../wailsjs/go/main/PluginFS";
import { languageForPath } from "./languageMap";

const MAX_BYTES_FRONTEND = 2 * 1024 * 1024;

const props = defineProps<{
  path: string;
}>();

const host = ref<HTMLDivElement | null>(null);
const state = ref<"loading" | "tooLarge" | "binary" | "ok" | "error">("loading");
const errorMsg = ref<string>("");

let view: EditorView | null = null;

async function load() {
  state.value = "loading";
  view?.destroy();
  view = null;
  try {
    const meta = (await FileMeta(props.path)) as any;
    if (meta.isBinary) {
      state.value = "binary";
      return;
    }
    if (meta.size > MAX_BYTES_FRONTEND) {
      state.value = "tooLarge";
      return;
    }
    const result = (await ReadFile(props.path, MAX_BYTES_FRONTEND)) as any;
    const text = new TextDecoder().decode(result.data);
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
});

watch(() => props.path, () => {
  void load();
});

onBeforeUnmount(() => {
  view?.destroy();
  view = null;
});
</script>

<template>
  <div class="file-editor">
    <div v-if="state === 'tooLarge'" class="banner">File too large to preview. Open externally.</div>
    <div v-if="state === 'binary'" class="banner">Binary file.</div>
    <div v-if="state === 'error'" class="banner err">Error: {{ errorMsg }}</div>
    <div v-show="state === 'ok'" ref="host" class="cm-host" />
    <div v-if="state === 'loading'" class="banner">Loading…</div>
  </div>
</template>

<style scoped>
.file-editor { height: 100%; display: flex; flex-direction: column; }
.cm-host { flex: 1; overflow: auto; }
.banner { padding: 10px 12px; font-size: 12px; opacity: 0.7; }
.banner.err { color: #f85149; opacity: 1; }
</style>
