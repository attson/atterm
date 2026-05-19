<script lang="ts" setup>
import { computed, ref, onUnmounted } from "vue";
import { useTranslatePanelStore } from "./panelStore";

const store = useTranslatePanelStore();

const TARGETS = [
  { code: "zh-CN", label: "中文 (Simplified)" },
  { code: "en", label: "English" },
  { code: "ja", label: "日本語" },
  { code: "ko", label: "한국어" },
  { code: "de", label: "Deutsch" },
  { code: "fr", label: "Français" },
  { code: "es", label: "Español" },
];

// Dragging: panel top-left position relative to viewport.
const pos = ref({ x: -1, y: 80 });  // -1 = "not placed yet, center on first mount"
const dragging = ref(false);
const dragOff = ref({ dx: 0, dy: 0 });

function onMouseDown(e: MouseEvent) {
  const t = e.target as HTMLElement;
  if (t.closest(".translate-panel__handle") == null) return;
  dragging.value = true;
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
  dragOff.value = { dx: e.clientX - rect.left, dy: e.clientY - rect.top };
  pos.value = { x: rect.left, y: rect.top };
  e.preventDefault();
}

function onMouseMove(e: MouseEvent) {
  if (!dragging.value) return;
  pos.value = clampPos(e.clientX - dragOff.value.dx, e.clientY - dragOff.value.dy);
}

function onMouseUp() { dragging.value = false; }

function clampPos(x: number, y: number) {
  const w = 480;
  const h = 320;  // approximate; clamp uses fixed panel size
  return {
    x: Math.max(8, Math.min(window.innerWidth - w - 8, x)),
    y: Math.max(8, Math.min(window.innerHeight - h - 8, y)),
  };
}

window.addEventListener("mousemove", onMouseMove);
window.addEventListener("mouseup", onMouseUp);
onUnmounted(() => {
  window.removeEventListener("mousemove", onMouseMove);
  window.removeEventListener("mouseup", onMouseUp);
});

const panelStyle = computed(() => {
  if (pos.value.x < 0) {
    // Initial placement: horizontally centered, 80px from top.
    return { left: `${Math.max(8, (window.innerWidth - 480) / 2)}px`, top: "80px" };
  }
  return { left: `${pos.value.x}px`, top: `${pos.value.y}px` };
});

function onTargetChange(e: Event) {
  const next = (e.target as HTMLSelectElement).value;
  void store.changeTarget(next);
}

function onRetry() { void store.retry(); }
</script>

<template>
  <Teleport to="body">
    <div
      v-if="store.visible"
      data-testid="translate-panel"
      class="translate-panel"
      :style="panelStyle"
      @mousedown="onMouseDown"
    >
      <header class="translate-panel__handle">
        <span class="translate-panel__title">Translate</span>
        <button
          type="button"
          class="translate-panel__close"
          data-testid="translate-close"
          aria-label="Close translate panel"
          @click="store.close"
        >×</button>
      </header>

      <section class="translate-panel__source">
        <div class="translate-panel__label">
          Source <span v-if="store.result?.detectedSrcLang && store.result.detectedSrcLang !== 'unknown'" class="translate-panel__detected">· detected {{ store.result.detectedSrcLang }}</span>
        </div>
        <pre class="translate-panel__pre">{{ store.source }}</pre>
      </section>

      <section class="translate-panel__target-row">
        <label class="translate-panel__label" for="translate-target">Target</label>
        <select
          id="translate-target"
          data-testid="translate-target"
          :value="store.targetLang"
          @change="onTargetChange"
        >
          <option v-for="t in TARGETS" :key="t.code" :value="t.code">{{ t.label }}</option>
        </select>
      </section>

      <section class="translate-panel__result">
        <div v-if="store.loading" class="translate-panel__loading">Translating…</div>
        <div v-else-if="store.error" class="translate-panel__error" role="alert">
          <div>{{ store.error.message }}</div>
          <button type="button" @click="onRetry">Retry</button>
        </div>
        <div v-else-if="store.result" class="translate-panel__text">{{ store.result.translated }}</div>
        <div v-else class="translate-panel__placeholder">No result yet.</div>
      </section>

      <details v-if="store.history.length > 0" class="translate-panel__history">
        <summary>Recent ({{ store.history.length }})</summary>
        <ul>
          <li v-for="(h, i) in store.history" :key="i">
            <button type="button" class="translate-panel__history-row" @click="store.restoreFromHistory(h)">
              <span class="translate-panel__history-source">{{ h.source.slice(0, 60) }}</span>
              <span class="translate-panel__history-arrow">→</span>
              <span class="translate-panel__history-translated">{{ h.translated.slice(0, 60) }}</span>
            </button>
          </li>
        </ul>
      </details>
    </div>
  </Teleport>
</template>

<style scoped>
.translate-panel {
  position: fixed;
  width: 480px;
  max-height: 70vh;
  overflow: auto;
  background: var(--ed-tab-bg, #21262d);
  border: 1px solid var(--ed-border, #2d333b);
  border-radius: 6px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  color: var(--ed-row-fg, #c9d1d9);
  z-index: 9999;
  display: flex;
  flex-direction: column;
}
.translate-panel__handle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  cursor: grab;
  background: var(--ed-row-hover, #30363d);
  border-bottom: 1px solid var(--ed-border, #2d333b);
  user-select: none;
}
.translate-panel__title { font-weight: 600; font-size: 13px; }
.translate-panel__close {
  background: transparent; border: none; color: inherit;
  font-size: 18px; cursor: pointer; padding: 0 4px;
}
.translate-panel__label { font-size: 11px; opacity: 0.7; text-transform: uppercase; letter-spacing: 0.04em; padding: 6px 12px 0; }
.translate-panel__detected { text-transform: none; opacity: 0.6; }
.translate-panel__pre {
  margin: 4px 12px 8px;
  padding: 6px;
  background: rgba(0, 0, 0, 0.25);
  border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  max-height: 200px;
  overflow: auto;
  white-space: pre-wrap;
}
.translate-panel__target-row { display: flex; align-items: center; gap: 8px; padding: 8px 12px; border-top: 1px solid var(--ed-border, #2d333b); }
.translate-panel__target-row select { flex: 1; }
.translate-panel__result { padding: 12px; border-top: 1px solid var(--ed-border, #2d333b); min-height: 60px; }
.translate-panel__loading { opacity: 0.7; }
.translate-panel__error { color: #f87171; font-size: 12px; display: flex; flex-direction: column; gap: 6px; }
.translate-panel__text { font-size: 13px; max-height: 240px; overflow: auto; white-space: pre-wrap; }
.translate-panel__placeholder { opacity: 0.5; font-size: 12px; }
.translate-panel__history { padding: 8px 12px; border-top: 1px solid var(--ed-border, #2d333b); font-size: 12px; }
.translate-panel__history summary { cursor: pointer; opacity: 0.8; }
.translate-panel__history ul { list-style: none; margin: 6px 0 0; padding: 0; }
.translate-panel__history-row { display: block; width: 100%; text-align: left; background: transparent; border: none; color: inherit; cursor: pointer; padding: 4px 0; font-size: 11px; }
.translate-panel__history-row:hover { opacity: 0.85; }
.translate-panel__history-source { opacity: 0.7; }
.translate-panel__history-arrow { margin: 0 6px; opacity: 0.5; }
</style>
