<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { History, LoaderCircle, LockKeyhole, RotateCcw, X } from "lucide-vue-next";
import type { SessionInputHistoryItem } from "../lib/connection";
import {
  detectImagePlaceholders,
  type SessionHistoryImagePreview,
} from "../composables/useTerminalImagePreview";
import { formatPromptHistoryTime } from "../lib/promptHistoryBrowser";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  loading: boolean;
  items: readonly SessionInputHistoryItem[];
  truncated: boolean;
  error: "locked" | "failed" | null;
  resolveImage?: (
    text: string,
    submittedAtUnixMS: number,
    occurrence: number,
  ) => SessionHistoryImagePreview | null;
}>();

const emit = defineEmits<{
  (event: "close"): void;
  (event: "retry"): void;
}>();

const { t, resolvedLocale } = useI18n();
const nowMs = ref(Date.now());
const bodyRef = ref<HTMLElement | null>(null);
const chronologicalItems = computed(() => [...props.items].reverse());
type HistoryTextSegment = {
  text: string;
  occurrence: number;
  previewName: string;
};
const displayItems = computed(() => chronologicalItems.value.map((item) => ({
  item,
  segments: historyTextSegments(item),
})));
const imagePreview = ref<{
  url: string;
  name: string;
  left: number;
  top: number;
} | null>(null);
let clock: number | null = null;

function relativeTime(unixMs: number): string {
  return formatPromptHistoryTime(unixMs / 1000, nowMs.value, resolvedLocale.value);
}

function exactTime(unixMs: number): string {
  return new Date(unixMs).toLocaleString();
}

function historyTextSegments(item: SessionInputHistoryItem): HistoryTextSegment[] {
  const matches = detectImagePlaceholders(item.text);
  if (matches.length === 0) {
    return [{ text: item.text, occurrence: -1, previewName: "" }];
  }
  const segments: HistoryTextSegment[] = [];
  let cursor = 0;
  matches.forEach((match, occurrence) => {
    if (match.start > cursor) {
      segments.push({ text: item.text.slice(cursor, match.start), occurrence: -1, previewName: "" });
    }
    const preview = props.resolveImage?.(item.text, item.submitted_at_unix_ms, occurrence) ?? null;
    segments.push({
      text: match.text,
      occurrence,
      previewName: preview?.name ?? "",
    });
    cursor = match.end;
  });
  if (cursor < item.text.length) {
    segments.push({ text: item.text.slice(cursor), occurrence: -1, previewName: "" });
  }
  return segments;
}

function hideImagePreview(): void {
  if (!imagePreview.value) return;
  URL.revokeObjectURL(imagePreview.value.url);
  imagePreview.value = null;
}

function showImagePreview(item: SessionInputHistoryItem, occurrence: number, event: Event): void {
  hideImagePreview();
  const preview = props.resolveImage?.(item.text, item.submitted_at_unix_ms, occurrence) ?? null;
  if (!preview) return;
  let url: string;
  try {
    url = URL.createObjectURL(preview.file);
  } catch {
    return;
  }
  const target = event.currentTarget as HTMLElement | null;
  const rect = target?.getBoundingClientRect();
  const maxWidth = 320;
  const maxHeight = 260;
  const left = Math.max(8, Math.min((rect?.right ?? 8) + 8, window.innerWidth - maxWidth - 8));
  const top = Math.max(8, Math.min(rect?.top ?? 8, window.innerHeight - maxHeight - 8));
  imagePreview.value = { url, name: preview.name, left, top };
}

watch(
  () => props.items,
  async () => {
    await nextTick();
    if (bodyRef.value) bodyRef.value.scrollTop = bodyRef.value.scrollHeight;
  },
  { immediate: true },
);

onMounted(() => {
  clock = window.setInterval(() => {
    nowMs.value = Date.now();
  }, 30_000);
});

onBeforeUnmount(() => {
  if (clock !== null) window.clearInterval(clock);
  hideImagePreview();
});
</script>

<template>
  <section
    class="session-input-history-panel"
    data-test="session-input-history-panel"
    role="dialog"
    :aria-label="t('terminal.inputHistory.title')"
    @pointerdown.stop
    @click.stop
  >
    <header class="session-input-history-header">
      <History :size="16" aria-hidden="true" />
      <h2>{{ t("terminal.inputHistory.title") }}</h2>
      <button
        type="button"
        class="session-input-history-icon-button"
        data-test="session-input-history-close"
        v-tooltip="t('common.close')"
        :aria-label="t('common.close')"
        @click="emit('close')"
      >
        <X :size="16" aria-hidden="true" />
      </button>
    </header>

    <div ref="bodyRef" class="session-input-history-body">
      <div
        v-if="loading"
        class="session-input-history-state"
        data-test="session-input-history-loading"
        role="status"
      >
        <LoaderCircle class="session-input-history-spinner" :size="18" aria-hidden="true" />
        <span>{{ t("terminal.inputHistory.loading") }}</span>
      </div>

      <div
        v-else-if="error === 'locked'"
        class="session-input-history-state"
        data-test="session-input-history-locked"
      >
        <LockKeyhole :size="20" aria-hidden="true" />
        <span>{{ t("terminal.inputHistory.locked") }}</span>
      </div>

      <div
        v-else-if="error === 'failed'"
        class="session-input-history-state"
        data-test="session-input-history-error"
      >
        <span>{{ t("terminal.inputHistory.failed") }}</span>
        <button
          type="button"
          class="session-input-history-retry"
          data-test="session-input-history-retry"
          @click="emit('retry')"
        >
          <RotateCcw :size="14" aria-hidden="true" />
          {{ t("common.retry") }}
        </button>
      </div>

      <div
        v-else-if="items.length === 0"
        class="session-input-history-state"
        data-test="session-input-history-empty"
      >
        {{ t("terminal.inputHistory.empty") }}
      </div>

      <template v-else>
        <div
          v-if="truncated"
          class="session-input-history-truncated"
          data-test="session-input-history-truncated"
          role="note"
        >
          {{ t("terminal.inputHistory.truncated") }}
        </div>
        <ol class="session-input-history-list">
          <li
            v-for="({ item, segments }, index) in displayItems"
            :key="`${item.submitted_at_unix_ms}-${index}`"
            data-test="session-input-history-item"
          >
            <time
              :datetime="new Date(item.submitted_at_unix_ms).toISOString()"
              v-tooltip="exactTime(item.submitted_at_unix_ms)"
              tabindex="0"
            >{{ relativeTime(item.submitted_at_unix_ms) }}</time>
            <div class="session-input-history-text">
              <template v-for="(segment, segmentIndex) in segments" :key="segmentIndex">
                <button
                  v-if="segment.previewName"
                  type="button"
                  class="session-input-history-image"
                  data-test="session-input-history-image"
                  :aria-label="segment.previewName"
                  @mouseenter="showImagePreview(item, segment.occurrence, $event)"
                  @mouseleave="hideImagePreview"
                  @focus="showImagePreview(item, segment.occurrence, $event)"
                  @blur="hideImagePreview"
                >{{ segment.text }}</button>
                <span v-else>{{ segment.text }}</span>
              </template>
            </div>
          </li>
        </ol>
      </template>
    </div>
  </section>
  <Teleport to="body">
    <div
      v-if="imagePreview"
      class="session-input-history-image-preview"
      data-test="session-input-history-image-preview"
      role="tooltip"
      :style="{ left: `${imagePreview.left}px`, top: `${imagePreview.top}px` }"
    >
      <img :src="imagePreview.url" :alt="imagePreview.name" />
    </div>
  </Teleport>
</template>

<style scoped>
.session-input-history-panel {
  position: absolute;
  top: 38px;
  right: 8px;
  z-index: 12;
  width: min(420px, calc(100% - 16px));
  max-height: calc(100% - 48px);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  color: var(--fg);
  background: rgba(13, 17, 23, 0.97);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.42);
  font-family: "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", -apple-system, BlinkMacSystemFont, sans-serif;
}

.session-input-history-header {
  flex: 0 0 auto;
  min-height: 38px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 8px 0 12px;
  border-bottom: 1px solid var(--border);
}

.session-input-history-header h2 {
  flex: 1;
  min-width: 0;
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0;
}

.session-input-history-icon-button,
.session-input-history-retry {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 0;
  color: var(--fg-dim);
  background: transparent;
  cursor: pointer;
}

.session-input-history-icon-button {
  width: 28px;
  height: 28px;
  padding: 0;
  border-radius: 5px;
}

.session-input-history-icon-button:hover,
.session-input-history-icon-button:focus-visible,
.session-input-history-retry:hover,
.session-input-history-retry:focus-visible {
  color: var(--fg);
  background: rgba(255, 255, 255, 0.08);
  outline: none;
}

.session-input-history-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
  -webkit-overflow-scrolling: touch;
}

.session-input-history-state {
  min-height: 132px;
  padding: 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
  text-align: center;
}

.session-input-history-retry {
  gap: 6px;
  min-height: 30px;
  padding: 5px 10px;
  border: 1px solid var(--border);
  border-radius: 5px;
  font-size: 12px;
}

.session-input-history-truncated {
  padding: 8px 12px;
  color: #f0b429;
  background: rgba(240, 180, 41, 0.08);
  border-bottom: 1px solid var(--border);
  font-size: 11px;
  line-height: 1.45;
}

.session-input-history-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.session-input-history-list li {
  padding: 11px 12px 13px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.07);
}

.session-input-history-list li:last-child {
  border-bottom: 0;
}

.session-input-history-list time {
  display: inline-block;
  margin-bottom: 6px;
  color: var(--fg-dim);
  font-size: 10px;
  line-height: 1.3;
}

.session-input-history-text {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  user-select: text;
  color: var(--fg);
  font: 12px/1.55 var(--font-mono);
}

.session-input-history-image {
  display: inline;
  margin: 0;
  padding: 0;
  border: 0;
  color: var(--link, #58a6ff);
  background: transparent;
  font: inherit;
  text-decoration: underline;
  text-decoration-style: dotted;
  cursor: zoom-in;
}

.session-input-history-image:focus-visible {
  border-radius: 2px;
  outline: 1px solid currentColor;
  outline-offset: 2px;
}

.session-input-history-image-preview {
  position: fixed;
  z-index: 1000;
  max-width: min(320px, calc(100vw - 16px));
  max-height: 260px;
  padding: 6px;
  border: 1px solid rgba(255, 255, 255, 0.2);
  border-radius: 6px;
  background: rgba(18, 18, 20, 0.96);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
  pointer-events: none;
}

.session-input-history-image-preview img {
  display: block;
  max-width: 300px;
  max-height: 240px;
  object-fit: contain;
}

.session-input-history-spinner {
  animation: session-input-history-spin 0.8s linear infinite;
}

@keyframes session-input-history-spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 700px) {
  .session-input-history-panel {
    top: auto;
    right: 8px;
    bottom: calc(8px + env(safe-area-inset-bottom, 0px));
    left: 8px;
    width: auto;
    max-height: min(68dvh, calc(100% - 52px - env(safe-area-inset-bottom, 0px)));
  }
}

@media (prefers-reduced-motion: reduce) {
  .session-input-history-spinner { animation: none; }
}
</style>
