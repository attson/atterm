<script lang="ts" setup>
import { useI18n } from "../../i18n/useI18n";
import type { FileSystemBridge } from "./fsBridge";

const props = defineProps<{ fs: FileSystemBridge; path: string; message?: string }>();
const { t } = useI18n();

async function openExternal() {
  try {
    await props.fs.openExternal(props.path);
  } catch {
    // The user already sees an error banner; nothing else useful to do here.
  }
}
</script>

<template>
  <div class="binary-banner">
    <span class="msg">{{ message ?? t("plugins.fileExplorer.unsupportedPreview") }}</span>
    <button class="open-btn" @click="openExternal">
      {{ t("plugins.fileExplorer.openInSystem") }}
    </button>
  </div>
</template>

<style scoped>
.binary-banner {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 18px 20px;
  font-size: 13px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.7));
}
.msg { flex: 0 1 auto; }
.open-btn {
  background: var(--ed-shell-bg, #22272e);
  border: 1px solid var(--ed-border, #444c56);
  color: var(--ed-row-fg, #adbac7);
  padding: 3px 10px;
  border-radius: 3px;
  font-size: 12px;
  cursor: pointer;
}
.open-btn:hover { background: var(--ed-row-hover, rgba(173, 186, 199, 0.1)); }
</style>
