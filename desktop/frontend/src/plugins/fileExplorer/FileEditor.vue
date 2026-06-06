<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import { usePlatform } from "../../platform";
import { previewKind, type PreviewKind } from "./previewKind";
import CodeViewer from "./CodeViewer.vue";
import ImagePreview from "./ImagePreview.vue";
import MediaPreview from "./MediaPreview.vue";
import PdfPreview from "./PdfPreview.vue";
import BinaryBanner from "./BinaryBanner.vue";
import { useI18n } from "../../i18n/useI18n";

const props = defineProps<{
  path: string;
  showLineNumbers: boolean;
  theme: "dimmed" | "light";
  /** SVG dual-mode toggle: "code" → highlight, "render" → ImagePreview. */
  viewMode: "code" | "render";
}>();

const platform = usePlatform();
const fs = platform.pluginHost!.fs;
const { t } = useI18n();

const kind = ref<PreviewKind | null>(null);
const error = ref<string>("");

let off: (() => void) | null = null;

async function resolveKind() {
  kind.value = null;
  error.value = "";
  try {
    const meta = (await fs.fileMeta(props.path)) as { isBinary: boolean };
    kind.value = previewKind(props.path, meta.isBinary);
  } catch (e) {
    error.value = (e as Error).message;
  }
}

onMounted(() => {
  void resolveKind();
  off = platform.events.on("plugin-fs:dir-changed", () => {
    // The dispatch decision depends only on the file path + binary-ness, both
    // of which are recomputed by CodeViewer on the dir-changed event already.
    // No re-dispatch is needed here.
  });
});

watch(() => props.path, () => { void resolveKind(); });

onBeforeUnmount(() => { if (off) off(); });
</script>

<template>
  <div class="file-editor-host">
    <div v-if="error" class="banner err">
      {{ t("plugins.fileExplorer.errorPrefix", { message: error }) }}
    </div>
    <template v-else-if="kind === 'code' || (kind === 'svg' && viewMode === 'code')">
      <CodeViewer
        :path="path"
        :show-line-numbers="showLineNumbers"
        :theme="theme"
      />
    </template>
    <template v-else-if="kind === 'image' || (kind === 'svg' && viewMode === 'render')">
      <ImagePreview :path="path" :theme="theme" />
    </template>
    <template v-else-if="kind === 'video' || kind === 'audio'">
      <MediaPreview :path="path" :kind="kind" />
    </template>
    <template v-else-if="kind === 'pdf'">
      <PdfPreview :path="path" />
    </template>
    <template v-else-if="kind === 'binary-unknown'">
      <BinaryBanner :path="path" />
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
