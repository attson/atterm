<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { pasteFileBus, type PasteFileEvent } from "../lib/pasteFileBus";

const DISMISS_MS = 3000;

interface Toast {
  id: string;
  name: string;
  size: number;
}

const toasts = ref<Toast[]>([]);
const timers = new Map<string, number>();

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}

function startTimer(id: string) {
  const handle = window.setTimeout(() => dismiss(id), DISMISS_MS);
  timers.set(id, handle);
}

function clearTimer(id: string) {
  const handle = timers.get(id);
  if (handle !== undefined) {
    window.clearTimeout(handle);
    timers.delete(id);
  }
}

function handlePaste(event: PasteFileEvent) {
  toasts.value.push({ id: event.id, name: event.name, size: event.size });
  startTimer(event.id);
}

function dismiss(id: string) {
  clearTimer(id);
  const idx = toasts.value.findIndex((t) => t.id === id);
  if (idx !== -1) toasts.value.splice(idx, 1);
}

let unsubscribe: (() => void) | null = null;

onMounted(() => {
  unsubscribe = pasteFileBus.on(handlePaste);
});

onBeforeUnmount(() => {
  unsubscribe?.();
  unsubscribe = null;
  for (const handle of timers.values()) window.clearTimeout(handle);
  timers.clear();
  toasts.value = [];
});
</script>

<template>
  <div class="paste-file-host">
    <div
      v-for="t in toasts"
      :key="t.id"
      class="paste-file-toast"
      data-testid="paste-file-toast"
    >
      <span class="paste-file-icon">📎</span>
      <span class="paste-file-name">{{ t.name }}</span>
      <span class="paste-file-size">({{ formatSize(t.size) }})</span>
      <button
        type="button"
        class="paste-file-close"
        data-testid="paste-file-close"
        :aria-label="'close ' + t.name"
        @click="dismiss(t.id)"
      >×</button>
    </div>
  </div>
</template>

<style scoped>
.paste-file-host {
  position: fixed;
  top: 88px;
  right: 0.75rem;
  z-index: 50;
  display: flex;
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
}
.paste-file-toast {
  pointer-events: auto;
  padding: 8px 12px;
  background: var(--bg, #1f2024);
  color: var(--fg, #e6e6e6);
  border: 1px solid var(--border, #3a3b40);
  border-radius: 6px;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.35);
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  max-width: 260px;
}
.paste-file-icon { opacity: 0.7; }
.paste-file-name {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.paste-file-size { opacity: 0.7; }
.paste-file-close {
  margin-left: auto;
  background: transparent;
  border: none;
  color: inherit;
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
  opacity: 0.7;
}
.paste-file-close:hover { opacity: 1; }
</style>
