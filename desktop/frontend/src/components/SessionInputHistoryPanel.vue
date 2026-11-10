<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { History, LoaderCircle, LockKeyhole, RotateCcw, X } from "lucide-vue-next";
import type { SessionInputHistoryItem } from "../lib/connection";
import { formatPromptHistoryTime } from "../lib/promptHistoryBrowser";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  loading: boolean;
  items: readonly SessionInputHistoryItem[];
  truncated: boolean;
  error: "locked" | "failed" | null;
}>();

const emit = defineEmits<{
  (event: "close"): void;
  (event: "retry"): void;
}>();

const { t, resolvedLocale } = useI18n();
const nowMs = ref(Date.now());
const bodyRef = ref<HTMLElement | null>(null);
const chronologicalItems = computed(() => [...props.items].reverse());
let clock: number | null = null;

function relativeTime(unixMs: number): string {
  return formatPromptHistoryTime(unixMs / 1000, nowMs.value, resolvedLocale.value);
}

function exactTime(unixMs: number): string {
  return new Date(unixMs).toLocaleString();
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
            v-for="(item, index) in chronologicalItems"
            :key="`${item.submitted_at_unix_ms}-${index}`"
            data-test="session-input-history-item"
          >
            <time
              :datetime="new Date(item.submitted_at_unix_ms).toISOString()"
              v-tooltip="exactTime(item.submitted_at_unix_ms)"
              tabindex="0"
            >{{ relativeTime(item.submitted_at_unix_ms) }}</time>
            <div class="session-input-history-text">{{ item.text }}</div>
          </li>
        </ol>
      </template>
    </div>
  </section>
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
