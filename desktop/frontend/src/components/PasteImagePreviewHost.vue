<script setup lang="ts">
import { errText, logWarn } from "../lib/log";
import { onBeforeUnmount, onMounted, ref } from "vue";
import { pasteImageBus, type PasteImageEvent } from "../lib/pasteImageBus";

const DISMISS_MS = 5000;

interface Toast {
  id: string;
  file: Blob;
  url: string;
  name: string;
}

interface Lightbox {
  url: string;
  name: string;
}

const toasts = ref<Toast[]>([]);
const lightbox = ref<Lightbox | null>(null);
const timers = new Map<string, number>();

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

function handlePaste(event: PasteImageEvent) {
  let url: string;
  try {
    url = URL.createObjectURL(event.file);
  } catch (err) {
    logWarn("paste", "preview createObjectURL failed", { error: errText(err) });
    return;
  }
  toasts.value.push({
    id: event.id,
    file: event.file,
    url,
    name: event.name,
  });
  startTimer(event.id);
}

function dismiss(id: string) {
  clearTimer(id);
  const idx = toasts.value.findIndex((t) => t.id === id);
  if (idx === -1) return;
  const [removed] = toasts.value.splice(idx, 1);
  URL.revokeObjectURL(removed.url);
}

function onMouseEnter(id: string) {
  clearTimer(id);
}

function onMouseLeave(id: string) {
  if (!toasts.value.some((t) => t.id === id)) return;
  startTimer(id);
}

function openLightbox(toast: Toast) {
  if (lightbox.value) {
    URL.revokeObjectURL(lightbox.value.url);
  }
  try {
    lightbox.value = {
      url: URL.createObjectURL(toast.file),
      name: toast.name,
    };
  } catch (err) {
    logWarn("paste", "lightbox createObjectURL failed", { error: errText(err) });
    lightbox.value = null;
  }
}

function closeLightbox() {
  if (!lightbox.value) return;
  URL.revokeObjectURL(lightbox.value.url);
  lightbox.value = null;
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape" && lightbox.value) {
    closeLightbox();
  }
}

let unsubscribe: (() => void) | null = null;

onMounted(() => {
  unsubscribe = pasteImageBus.on(handlePaste);
  document.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
  unsubscribe?.();
  unsubscribe = null;
  document.removeEventListener("keydown", onKeydown);
  for (const handle of timers.values()) window.clearTimeout(handle);
  timers.clear();
  for (const t of toasts.value) URL.revokeObjectURL(t.url);
  toasts.value = [];
  if (lightbox.value) {
    URL.revokeObjectURL(lightbox.value.url);
    lightbox.value = null;
  }
});
</script>

<template>
  <div class="paste-preview-host">
    <div
      v-for="t in toasts"
      :key="t.id"
      class="paste-toast"
      data-testid="paste-toast"
      @mouseenter="onMouseEnter(t.id)"
      @mouseleave="onMouseLeave(t.id)"
    >
      <img :src="t.url" :alt="t.name" class="paste-toast-thumb" @click="openLightbox(t)" />
      <span class="paste-toast-name">{{ t.name }}</span>
      <button
        type="button"
        class="paste-toast-close"
        data-testid="paste-toast-close"
        :aria-label="'close ' + t.name"
        @click="dismiss(t.id)"
      >×</button>
    </div>
    <div
      v-if="lightbox"
      class="paste-lightbox"
      data-testid="paste-lightbox"
      @click="closeLightbox"
    >
      <img :src="lightbox.url" :alt="lightbox.name" @click.stop />
    </div>
  </div>
</template>

<style scoped>
.paste-preview-host {
  position: fixed;
  top: 88px;
  right: 0.75rem;
  z-index: 50;
  display: flex;
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
}
.paste-toast {
  pointer-events: auto;
  width: 200px;
  padding: 8px;
  background: var(--bg, #1f2024);
  color: var(--fg, #e6e6e6);
  border: 1px solid var(--border, #3a3b40);
  border-radius: 6px;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.35);
  display: flex;
  flex-direction: column;
  gap: 4px;
  position: relative;
}
.paste-toast-thumb {
  width: 100%;
  max-height: 120px;
  object-fit: contain;
  cursor: zoom-in;
  border-radius: 4px;
  background: #000;
}
.paste-toast-name {
  font-size: 12px;
  opacity: 0.8;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.paste-toast-close {
  position: absolute;
  top: 4px;
  right: 6px;
  background: transparent;
  border: none;
  color: inherit;
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
  opacity: 0.7;
}
.paste-toast-close:hover {
  opacity: 1;
}
.paste-lightbox {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.85);
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: zoom-out;
  pointer-events: auto;
}
.paste-lightbox img {
  max-width: 90vw;
  max-height: 90vh;
  cursor: default;
}
</style>
