<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "../i18n/useI18n";

interface PreviewTab {
  id: string;
  port: number;
  reconnecting: boolean;
}

const props = defineProps<{
  previews: PreviewTab[];
  activeId: string | null;
}>();

const emit = defineEmits<{
  (e: "show-terminal"): void;
  (e: "select", id: string): void;
  (e: "close", id: string): void;
  (e: "add"): void;
  (e: "open-browser"): void;
  (e: "copy"): void;
}>();

const { t } = useI18n();
const menuOpen = ref(false);
const rootRef = ref<HTMLElement | null>(null);

const active = computed(() => props.previews.find((p) => p.id === props.activeId) ?? null);
const onPreview = computed(() => props.activeId !== null && active.value !== null);

function toggleMenu(): void {
  menuOpen.value = !menuOpen.value;
}
function closeMenu(): void {
  menuOpen.value = false;
}
function select(id: string): void {
  emit("select", id);
  closeMenu();
}
function add(): void {
  closeMenu();
  emit("add");
}

function onDocClick(e: MouseEvent): void {
  if (menuOpen.value && !(e.target as HTMLElement)?.closest?.(".sp-switcher")) closeMenu();
}
onMounted(() => document.addEventListener("click", onDocClick, true));
onBeforeUnmount(() => document.removeEventListener("click", onDocClick, true));
</script>

<template>
  <div ref="rootRef" class="sp-switcher">
    <div class="sp-seg">
      <button type="button" :class="{ active: !onPreview }" @click="emit('show-terminal')">
        {{ t("terminal.preview.terminal") }}
      </button>
      <button
        type="button"
        class="sp-current"
        :class="{ active: onPreview, reconnecting: active?.reconnecting }"
        :title="active?.reconnecting ? t('terminal.preview.reconnecting') : t('terminal.preview.switch')"
        @click="toggleMenu"
      >
        <span class="sp-dot" :class="{ reconnecting: active?.reconnecting }"></span>
        {{ t("terminal.preview.previewPort", { port: active?.port ?? previews[0]?.port ?? "" }) }}
        <span class="sp-caret">▾</span>
      </button>
    </div>

    <div v-if="onPreview" class="sp-actions">
      <button type="button" :title="t('terminal.preview.openInBrowser')" :aria-label="t('terminal.preview.openInBrowser')" @click="emit('open-browser')">
        <span class="sp-ico"><svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M8 12h8M12 8l4 4-4 4"/></svg></span>
      </button>
      <button type="button" :title="t('terminal.preview.copyURL')" :aria-label="t('terminal.preview.copyURL')" @click="emit('copy')">
        <span class="sp-ico"><svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg></span>
      </button>
      <button type="button" class="close" :title="t('terminal.preview.closeThis')" :aria-label="t('terminal.preview.closeThis')" @click="active && emit('close', active.id)">✕</button>
    </div>

    <div v-if="menuOpen" class="sp-menu" role="menu">
      <button
        v-for="p in previews"
        :key="p.id"
        type="button"
        class="sp-item"
        :class="{ active: p.id === activeId }"
        @click="select(p.id)"
      >
        <span class="sp-dot" :class="{ reconnecting: p.reconnecting }"></span>
        <span class="sp-lbl">{{ t("terminal.preview.previewPort", { port: p.port }) }}</span>
        <span class="sp-mini" :title="t('terminal.preview.openInBrowser')" @click.stop="select(p.id); emit('open-browser')">
          <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M8 12h8M12 8l4 4-4 4"/></svg>
        </span>
        <span class="sp-mini" :title="t('terminal.preview.copyURL')" @click.stop="select(p.id); emit('copy')">
          <svg xmlns="http://www.w3.org/2000/svg" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
        </span>
        <span class="sp-x" :title="t('terminal.preview.closeThis')" @click.stop="emit('close', p.id)">✕</span>
      </button>
      <div class="sp-sep"></div>
      <button type="button" class="sp-item sp-add" @click="add">＋ {{ t("terminal.preview.newPreview") }}</button>
    </div>
  </div>
</template>

<style scoped>
.sp-switcher { position: relative; display: inline-flex; align-items: center; gap: 6px; pointer-events: auto; }
.sp-seg {
  display: inline-flex; align-items: center; gap: 2px; background: var(--terminal-overlay, rgba(13, 17, 23, 0.92));
  border: 1px solid var(--border); border-radius: 8px; padding: 2px;
}
.sp-seg button {
  display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; flex: 0 0 auto;
  border: 0; border-radius: 5px; padding: 4px 8px; color: var(--fg-dim);
  background: transparent; font: 12px var(--font-mono); cursor: pointer;
}
.sp-seg button:hover { color: var(--fg); background: rgba(255, 255, 255, 0.06); }
.sp-seg button.active { color: var(--fg); background: rgba(255, 255, 255, 0.1); }
.sp-seg button.reconnecting { color: var(--warning, #d29922); }
.sp-caret { color: var(--fg-dim); font-size: 10px; }
.sp-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--good, #3fb950); flex: 0 0 auto; }
.sp-dot.reconnecting { background: var(--warning, #d29922); animation: sp-pulse 1s infinite; }
@keyframes sp-pulse { 50% { opacity: 0.3; } }

.sp-actions {
  display: inline-flex; align-items: center; gap: 2px; background: var(--terminal-overlay, rgba(13, 17, 23, 0.92));
  border: 1px solid var(--border); border-radius: 8px; padding: 2px;
}
.sp-actions button {
  width: 24px; height: 24px; display: inline-flex; align-items: center; justify-content: center;
  border: 0; border-radius: 5px; color: var(--fg-dim); background: transparent; cursor: pointer;
}
.sp-actions button:hover { color: var(--fg); background: rgba(255, 255, 255, 0.06); }
.sp-actions button.close:hover { color: var(--bad); background: rgba(248, 81, 73, 0.12); }
/* SVG must be wrapped in a span: WebKit collapses an <svg> that is a direct
   flex item of a <button>, but renders it fine inside a span. */
.sp-ico { display: inline-flex; align-items: center; justify-content: center; }
.sp-actions svg { width: 13px; height: 13px; display: block; }

.sp-menu {
  position: absolute; top: 32px; left: 0; z-index: 20; min-width: 210px;
  background: var(--terminal-overlay, rgba(13, 17, 23, 0.98)); border: 1px solid var(--border);
  border-radius: 8px; padding: 4px; box-shadow: 0 12px 30px rgba(0, 0, 0, 0.45);
}
.sp-item {
  display: flex; align-items: center; gap: 8px; width: 100%; padding: 6px 8px; border: 0;
  border-radius: 6px; background: transparent; color: var(--fg-dim); font: 12px var(--font-mono);
  cursor: pointer; white-space: nowrap; text-align: left;
}
.sp-item:hover { color: var(--fg); background: rgba(255, 255, 255, 0.06); }
.sp-item.active { color: var(--fg); background: rgba(255, 255, 255, 0.1); }
.sp-lbl { flex: 1; }
.sp-mini { width: 22px; height: 22px; display: inline-flex; align-items: center; justify-content: center; border-radius: 5px; color: var(--fg-dim); flex: 0 0 auto; }
.sp-mini:hover { color: var(--fg); background: rgba(255, 255, 255, 0.1); }
.sp-mini svg { width: 13px; height: 13px; display: block; }
.sp-x { width: 22px; height: 22px; display: inline-flex; align-items: center; justify-content: center; border-radius: 5px; color: var(--fg-dim); flex: 0 0 auto; }
.sp-x:hover { color: var(--bad); background: rgba(248, 81, 73, 0.12); }
.sp-sep { height: 1px; background: var(--border); margin: 4px 2px; }
.sp-add { color: var(--good, #3fb950); }
</style>
