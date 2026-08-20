<script setup lang="ts">
// Search bar for the terminal scrollback. Deliberately knows nothing about
// xterm: it owns the query string and the key semantics, and reports what to
// look for through `find`. The parent drives the search addon and feeds the
// result position back in via `resultIndex` / `resultCount`.
import { nextTick, ref, watch } from "vue";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  open: boolean;
  // Bumped by the parent every time the open shortcut fires, so pressing it
  // again while the bar is already open re-focuses and selects the query.
  focusSeq: number;
  // 0-based index of the active match; -1 when there is none.
  resultIndex: number;
  resultCount: number;
}>();

const emit = defineEmits<{
  (e: "find", query: string, dir: "next" | "prev", incremental: boolean): void;
  (e: "close"): void;
}>();

const { t } = useI18n();
const query = ref("");
const inputEl = ref<HTMLInputElement | null>(null);

function focusInput() {
  void nextTick(() => {
    inputEl.value?.focus();
    inputEl.value?.select();
  });
}

// Reopening (or re-focusing) with a retained query must re-run the search
// immediately — the query ref survives a close (v-if only tears down the
// DOM, not the component instance), but the parent resets its result
// counters on close. Without this, the bar shows a false "no results" until
// the user types again. Matches VS Code / iTerm2: retained query re-highlights
// on reopen.
function refreshIfQuery() {
  if (query.value) emit("find", query.value, "next", true);
}

watch(() => props.open, (open) => { if (open) { focusInput(); refreshIfQuery(); } });
watch(() => props.focusSeq, () => { if (props.open) { focusInput(); refreshIfQuery(); } });

function onInput() {
  emit("find", query.value, "next", true);
}

function step(dir: "next" | "prev") {
  emit("find", query.value, dir, false);
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    emit("close");
    return;
  }
  if (e.key === "Enter") {
    e.preventDefault();
    step(e.shiftKey ? "prev" : "next");
  }
}
</script>

<template>
  <div v-if="open" class="term-search" data-test="terminal-search">
    <input
      ref="inputEl"
      v-model="query"
      class="term-search-input"
      data-test="terminal-search-input"
      type="text"
      spellcheck="false"
      autocomplete="off"
      :placeholder="t('terminal.search.placeholder')"
      @input="onInput"
      @keydown="onKeydown"
    />
    <span class="term-search-count" data-test="terminal-search-count">
      {{ resultCount > 0 ? (resultIndex >= 0 ? `${resultIndex + 1}/${resultCount}` : `${resultCount}+`) : (query ? t("terminal.search.noResults") : "") }}
    </span>
    <button
      class="term-search-btn"
      data-test="terminal-search-prev"
      :title="t('terminal.search.prev')"
      :aria-label="t('terminal.search.prev')"
      :disabled="resultCount === 0"
      @click="step('prev')"
    >↑</button>
    <button
      class="term-search-btn"
      data-test="terminal-search-next"
      :title="t('terminal.search.next')"
      :aria-label="t('terminal.search.next')"
      :disabled="resultCount === 0"
      @click="step('next')"
    >↓</button>
    <button
      class="term-search-btn"
      data-test="terminal-search-close"
      :title="t('terminal.search.close')"
      :aria-label="t('terminal.search.close')"
      @click="emit('close')"
    >×</button>
  </div>
</template>

<style scoped>
/* Anchored top-LEFT on purpose: TerminalView's .overlay (attach progress /
   replay) and the remote badge both live at top-right, so the right side is
   already spoken for. The parent .term-view is `position: absolute; inset: 0`,
   which is the positioning context this absolute box resolves against. */
.term-search {
  position: absolute;
  top: 8px;
  left: 12px;
  z-index: 6;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 6px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.35);
}
.term-search-input {
  width: 180px;
  padding: 2px 6px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg);
  color: var(--fg);
  font-size: 12px;
  outline: none;
}
.term-search-input:focus { border-color: var(--accent); }
.term-search-count {
  min-width: 52px;
  color: var(--fg-dim);
  font-size: 11px;
  text-align: center;
  white-space: nowrap;
}
.term-search-btn {
  padding: 1px 6px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: #21262d;
  color: var(--fg);
  font-size: 11px;
  line-height: 16px;
  cursor: pointer;
}
.term-search-btn:hover:not(:disabled) { background: var(--border); }
.term-search-btn:disabled { opacity: 0.4; cursor: default; }
</style>
