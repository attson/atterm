<script lang="ts" setup>
import { onMounted, ref, watch } from "vue";
import { usePlatform } from "../../platform";
import BinaryBanner from "./BinaryBanner.vue";
import type { FileSystemBridge } from "./fsBridge";

const MAX_BYTES = 2 * 1024 * 1024;

const props = defineProps<{ fs: FileSystemBridge; path: string; theme: "dimmed" | "light" }>();
const platform = usePlatform();

const html = ref<string>("");
const state = ref<"loading" | "ok" | "error">("loading");

function decodeFileBytes(data: unknown): string {
  let bytes: Uint8Array;
  if (typeof data === "string") {
    const bin = atob(data);
    bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  } else if (ArrayBuffer.isView(data)) {
    // Use ArrayBuffer.isView so cross-realm Uint8Array (e.g. vitest test
    // env vs component module) still passes.
    const view = data as ArrayBufferView;
    bytes = new Uint8Array(view.buffer, view.byteOffset, view.byteLength);
  } else if (Array.isArray(data)) {
    bytes = new Uint8Array(data as number[]);
  } else {
    throw new Error("unexpected content type");
  }
  return new TextDecoder().decode(bytes);
}

async function load() {
  state.value = "loading";
  try {
    const meta = (await props.fs.fileMeta(props.path)) as { size: number; isBinary: boolean };
    if (meta.size > MAX_BYTES) { state.value = "error"; return; }
    const result = (await props.fs.readFile(props.path, MAX_BYTES)) as { data: unknown };
    const text = decodeFileBytes(result.data);
    // Lazy-load the parser so it joins this component's own chunk only when
    // the user actually opens a markdown file.
    const MarkdownIt = (await import("markdown-it")).default;
    // html: false disables raw HTML in the source. linkify: true turns bare
    // URLs into <a> tags. typographer: false keeps quotes and dashes literal.
    const md = new MarkdownIt({ html: false, linkify: true, breaks: false });
    html.value = md.render(text);
    state.value = "ok";
  } catch {
    state.value = "error";
  }
}

function onContainerClick(ev: MouseEvent) {
  const target = ev.target as HTMLElement | null;
  if (!target) return;
  const a = target.closest("a") as HTMLAnchorElement | null;
  if (!a) return;
  const href = a.getAttribute("href") ?? "";
  ev.preventDefault();
  if (/^(https?|mailto):/i.test(href)) {
    void platform.system.openExternalURL(href);
  }
  // Otherwise (relative links, fragments, javascript:) — no-op.
}

onMounted(() => { void load(); });
watch(() => [props.path, props.fs.identity], () => { void load(); });
</script>

<template>
  <BinaryBanner v-if="state === 'error'" :fs="fs" :path="path" />
  <div
    v-else-if="state === 'ok'"
    class="md-host"
    :class="theme"
    v-html="html"
    @click="onContainerClick"
  />
  <div v-else class="md-host placeholder">…</div>
</template>

<style scoped>
.md-host {
  flex: 1 1 auto;
  overflow: auto;
  padding: 20px 28px;
  background: var(--ed-editor-bg, #22272e);
  color: var(--ed-row-fg, #adbac7);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  font-size: 14px;
  line-height: 1.6;
}
.md-host.placeholder {
  color: var(--ed-muted, rgba(173, 186, 199, 0.5));
  font-size: 13px;
}
.md-host :deep(h1),
.md-host :deep(h2),
.md-host :deep(h3),
.md-host :deep(h4),
.md-host :deep(h5),
.md-host :deep(h6) {
  margin: 1.4em 0 0.6em;
  font-weight: 600;
  line-height: 1.25;
  color: var(--ed-tab-active-fg, #cdd9e5);
}
.md-host :deep(h1) { font-size: 1.8em; border-bottom: 1px solid var(--ed-border, #444c56); padding-bottom: 0.3em; }
.md-host :deep(h2) { font-size: 1.4em; border-bottom: 1px solid var(--ed-border, #444c56); padding-bottom: 0.2em; }
.md-host :deep(h3) { font-size: 1.2em; }
.md-host :deep(h4) { font-size: 1.05em; }
.md-host :deep(p) { margin: 0.6em 0; }
.md-host :deep(a) { color: var(--ed-tab-active-bar, #539bf5); text-decoration: none; }
.md-host :deep(a:hover) { text-decoration: underline; }
.md-host :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.88em;
  padding: 0.15em 0.35em;
  background: var(--ed-tab-bg, #2d333b);
  border-radius: 3px;
}
.md-host :deep(pre) {
  background: var(--ed-tab-bg, #2d333b);
  padding: 12px 14px;
  border-radius: 5px;
  overflow-x: auto;
  margin: 0.8em 0;
}
.md-host :deep(pre code) {
  background: transparent;
  padding: 0;
  font-size: 0.85em;
}
.md-host :deep(blockquote) {
  margin: 0.8em 0;
  padding: 0 1em;
  color: var(--ed-muted, rgba(173, 186, 199, 0.7));
  border-left: 3px solid var(--ed-border, #444c56);
}
.md-host :deep(ul),
.md-host :deep(ol) { margin: 0.6em 0; padding-left: 1.6em; }
.md-host :deep(li) { margin: 0.2em 0; }
.md-host :deep(li > p) { margin: 0.2em 0; }
.md-host :deep(hr) {
  border: none;
  border-top: 1px solid var(--ed-border, #444c56);
  margin: 1.4em 0;
}
.md-host :deep(table) {
  border-collapse: collapse;
  margin: 0.8em 0;
  font-size: 0.95em;
}
.md-host :deep(th),
.md-host :deep(td) {
  border: 1px solid var(--ed-border, #444c56);
  padding: 6px 10px;
  text-align: left;
}
.md-host :deep(th) { background: var(--ed-tab-bg, #2d333b); font-weight: 600; }
.md-host :deep(img) { max-width: 100%; height: auto; }
.md-host::-webkit-scrollbar { width: 12px; height: 12px; }
.md-host::-webkit-scrollbar-track { background: var(--ed-editor-bg, #22272e); }
.md-host::-webkit-scrollbar-thumb {
  background: var(--ed-row-hover, rgba(173, 186, 199, 0.2));
  border-radius: 6px;
  border: 3px solid var(--ed-editor-bg, #22272e);
}
.md-host::-webkit-scrollbar-thumb:hover { background: var(--ed-chevron, rgba(173, 186, 199, 0.4)); }
</style>
