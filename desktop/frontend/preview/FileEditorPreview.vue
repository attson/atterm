<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView, lineNumbers } from "@codemirror/view";
import { languageForPath } from "../src/plugins/fileExplorer/languageMap";
import { highlightExtensionFor } from "../src/plugins/fileExplorer/highlight";
import type { MockEntry } from "./mockData";

const props = defineProps<{
  path: string | null;
  entry: MockEntry | null;
  showLineNumbers: boolean;
  theme: "dimmed" | "light";
}>();

const host = ref<HTMLDivElement | null>(null);
const state = ref<"empty" | "loading" | "tooLarge" | "binary" | "ok" | "error">("empty");
const errorMsg = ref<string>("");

let view: EditorView | null = null;

function cssVar(name: string, fallback: string): string {
  if (typeof window === "undefined") return fallback;
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
}

function makeThemeExt(): Extension {
  return EditorView.theme(
    {
      "&": {
        backgroundColor: cssVar("--ed-editor-bg", "#1e1e1e"),
        color: cssVar("--ed-editor-fg", "#cccccc"),
        height: "100%",
      },
      ".cm-gutters": {
        backgroundColor: cssVar("--ed-gutter-bg", "#1e1e1e"),
        color: cssVar("--ed-gutter-fg", "#5a5a5a"),
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
  view?.destroy();
  view = null;
  if (!props.path || !props.entry) {
    state.value = "empty";
    return;
  }
  state.value = "loading";
  try {
    if (props.entry.isBinary) { state.value = "binary"; return; }
    if (props.entry.tooLarge) { state.value = "tooLarge"; return; }
    const text = props.entry.content ?? "";
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

onMounted(() => { void load(); });
watch(
  () => [props.path, props.entry, props.showLineNumbers, props.theme],
  () => { void load(); },
);
onBeforeUnmount(() => { view?.destroy(); view = null; });
</script>

<template>
  <div class="file-editor">
    <div v-if="state === 'empty'" class="banner muted">Select a file from the tree.</div>
    <div v-else-if="state === 'tooLarge'" class="banner muted">File too large to preview. Open externally.</div>
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
  background: var(--ed-editor-bg, #1e1e1e);
}
.cm-host {
  flex: 1 1 auto;
  overflow: auto;
}
.banner {
  padding: 18px 20px;
  font-size: 13px;
}
.muted { color: var(--ed-muted, rgba(204, 204, 204, 0.5)); }
.err { color: var(--ed-error, #f48771); }
</style>
