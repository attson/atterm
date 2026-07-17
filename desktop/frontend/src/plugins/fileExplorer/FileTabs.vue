<script lang="ts" setup>
import { computed } from "vue";
import { X } from "lucide-vue-next";
import type { Tab } from "./tabsModel";
import { useI18n } from "../../i18n/useI18n";

const props = defineProps<{
  tabs: Tab[];
  activeIdx: number;
  viewMode: "code" | "render";
}>();

const emit = defineEmits<{
  (e: "select", idx: number): void;
  (e: "close-request", idx: number): void;
  (e: "toggle-view-mode"): void;
}>();

const { t } = useI18n();

function basename(p: string): string {
  const i = p.lastIndexOf("/");
  return i === -1 ? p : p.slice(i + 1);
}

const activeHasDualMode = computed(() => {
  const i = props.activeIdx;
  if (i < 0 || i >= props.tabs.length) return false;
  return /\.(svg|md|markdown)$/i.test(props.tabs[i].path);
});
</script>

<template>
  <div class="file-tabs">
    <div
      v-for="(t, i) in props.tabs"
      :key="t.path"
      class="tab"
      :class="{ active: i === props.activeIdx, preview: !t.persistent }"
      :title="t.path"
      @click="emit('select', i)"
    >
      <span class="name">
        {{ basename(t.path) }}<span v-if="t.dirty" class="dot" data-test="dirty-dot">•</span>
      </span>
      <span class="close" @click.stop="emit('close-request', i)">
        <X :size="12" :stroke-width="2" />
      </span>
    </div>
    <button
      v-if="activeHasDualMode"
      class="view-toggle"
      :title="viewMode === 'code'
        ? t('plugins.fileExplorer.showAsRender')
        : t('plugins.fileExplorer.showAsCode')"
      @click="emit('toggle-view-mode')"
    >
      {{ viewMode === 'code'
        ? t('plugins.fileExplorer.showAsRender')
        : t('plugins.fileExplorer.showAsCode') }}
    </button>
  </div>
</template>

<style scoped>
.file-tabs {
  display: flex;
  flex-direction: row;
  overflow-x: auto;
  background: var(--ed-tab-bg, #2d333b);
  border-bottom: 1px solid var(--ed-border, #444c56);
  flex: 0 0 auto;
}
.tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  font-size: 12px;
  border-right: 1px solid var(--ed-border, #444c56);
  cursor: pointer;
  color: var(--ed-tab-fg, #768390);
  white-space: nowrap;
  position: relative;
  user-select: none;
}
.tab:hover { color: var(--ed-tab-hover-fg, #adbac7); }
.tab.active {
  background: var(--ed-tab-active-bg, #22272e);
  color: var(--ed-tab-active-fg, #cdd9e5);
}
.tab.active::after {
  content: "";
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 1px;
  background: var(--ed-tab-active-bar, #539bf5);
}
.tab.preview .name { font-style: italic; }
.dot { margin-left: 3px; color: var(--ed-tab-active-bar, #539bf5); font-weight: bold; }
.close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  border-radius: 3px;
  opacity: 0.55;
}
.close:hover {
  opacity: 1;
  background: var(--ed-row-hover, rgba(255, 255, 255, 0.1));
}
.view-toggle {
  background: none;
  border: 1px solid var(--ed-border, #444c56);
  color: var(--ed-row-fg, #adbac7);
  font-size: 11px;
  padding: 1px 8px;
  border-radius: 3px;
  margin-left: auto;
  cursor: pointer;
}
.view-toggle:hover { background: var(--ed-row-hover, rgba(173, 186, 199, 0.1)); }
</style>
