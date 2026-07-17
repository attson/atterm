<script lang="ts" setup>
import { onBeforeUnmount, ref, watch, type Ref } from "vue";
import { previewKind, type PreviewKind } from "./previewKind";
import CodeEditor from "./CodeEditor.vue";
import ImagePreview from "./ImagePreview.vue";
import MediaPreview from "./MediaPreview.vue";
import PdfPreview from "./PdfPreview.vue";
import MarkdownPreview from "./MarkdownPreview.vue";
import BinaryBanner from "./BinaryBanner.vue";
import type { FileSystemBridge } from "./fsBridge";
import { useI18n } from "../../i18n/useI18n";

const props = defineProps<{
  fs: FileSystemBridge;
  path: string;
  showLineNumbers: boolean;
  theme: "dimmed" | "light";
  /** Dual-mode toggle for kinds with both source and rendered views
   *  (SVG: code↔image; markdown: code↔rendered HTML). Ignored for
   *  single-view kinds. */
  viewMode: "code" | "render";
}>();

const emit = defineEmits<{
  (e: "dirty-change", dirty: boolean): void;
}>();

const codeEditorRef = ref<{ save: () => Promise<boolean> } | null>(null) as Ref<{ save: () => Promise<boolean> } | null>;

async function save(): Promise<boolean> {
  return codeEditorRef.value?.save?.() ?? Promise.resolve(false);
}

defineExpose({ save });

const { t } = useI18n();

const kind = ref<PreviewKind | null>(null);
const error = ref<string>("");
let disposed = false;
let generation = 0;

function isCurrent(fs: FileSystemBridge, path: string, request: number): boolean {
  return !disposed && generation === request && props.fs === fs && props.path === path;
}

async function resolveKind() {
  const fs = props.fs;
  const path = props.path;
  const request = ++generation;
  kind.value = null;
  error.value = "";
  try {
    const meta = (await fs.fileMeta(path)) as { isBinary: boolean };
    if (!isCurrent(fs, path, request)) return;
    kind.value = previewKind(path, meta.isBinary);
  } catch (e) {
    if (!isCurrent(fs, path, request)) return;
    error.value = (e as Error).message;
  }
}

watch(() => [props.path, props.fs], () => { void resolveKind(); }, { immediate: true });
onBeforeUnmount(() => {
  disposed = true;
  generation++;
});
</script>

<template>
  <div class="file-editor-host">
    <div v-if="error" class="banner err">
      {{ t("plugins.fileExplorer.errorPrefix", { message: error }) }}
    </div>
    <template v-else-if="kind === 'code' || ((kind === 'svg' || kind === 'markdown') && viewMode === 'code')">
      <CodeEditor
        ref="codeEditorRef"
        :fs="fs"
        :path="path"
        :show-line-numbers="showLineNumbers"
        :theme="theme"
        @dirty-change="(v: boolean) => emit('dirty-change', v)"
      />
    </template>
    <template v-else-if="kind === 'image' || (kind === 'svg' && viewMode === 'render')">
      <ImagePreview :fs="fs" :path="path" :theme="theme" />
    </template>
    <template v-else-if="kind === 'markdown' && viewMode === 'render'">
      <MarkdownPreview :fs="fs" :path="path" :theme="theme" />
    </template>
    <template v-else-if="kind === 'video' || kind === 'audio'">
      <MediaPreview :fs="fs" :path="path" :kind="kind" />
    </template>
    <template v-else-if="kind === 'pdf'">
      <PdfPreview :fs="fs" :path="path" />
    </template>
    <template v-else-if="kind === 'binary-unknown'">
      <BinaryBanner :fs="fs" :path="path" />
    </template>
  </div>
</template>

<style scoped>
.file-editor-host {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--ed-editor-bg, #22272e);
}
.banner { padding: 18px 20px; font-size: 13px; }
.err { color: var(--ed-error, #f47067); }
</style>
