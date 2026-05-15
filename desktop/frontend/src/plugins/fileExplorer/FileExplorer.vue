<script lang="ts" setup>
import { computed, ref, watch } from "vue";
import { usePluginConfigStore } from "../configStore";
import { useResizer } from "../useResizer";
import FileTree from "./FileTree.vue";
import FileTabs from "./FileTabs.vue";
import FileEditor from "./FileEditor.vue";
import { openPath, closeTab, type TabsState } from "./tabsModel";
import type { PluginContext } from "../types";

const props = defineProps<{ context: PluginContext }>();
const store = usePluginConfigStore();

// Pinned root (in-memory only; resets on app restart per spec).
const pinned = ref<string | null>(null);

const persistedInnerRatio = computed(() => store.cfg?.fileExplorer.innerTreeRatio ?? 0.3);
const dragInnerRatio = ref<number | null>(null);
const innerRatio = computed(() => dragInnerRatio.value ?? persistedInnerRatio.value);

const bodyRef = ref<HTMLDivElement | null>(null);

const { onMouseDown: onDividerDown } = useResizer({
  onDrag: (deltaX) => {
    if (!bodyRef.value) return;
    const width = bodyRef.value.clientWidth;
    if (width <= 0) return;
    const cur = dragInnerRatio.value ?? persistedInnerRatio.value;
    const next = Math.max(0.15, Math.min(cur - deltaX / width, 0.5));
    dragInnerRatio.value = next;
  },
  onEnd: () => {
    if (dragInnerRatio.value === null || !store.cfg) {
      dragInnerRatio.value = null;
      return;
    }
    const next = JSON.parse(JSON.stringify(store.cfg));
    next.fileExplorer.innerTreeRatio = dragInnerRatio.value;
    void store.save(next);
    dragInnerRatio.value = null;
  },
});

const root = computed<string | null>(() => pinned.value ?? props.context.activeCwd.value);

const tabsState = ref<TabsState>({ tabs: [], activeIdx: -1 });

function onFileClick(path: string) {
  tabsState.value = openPath(tabsState.value, path, "preview");
}

function onFileDoubleClick(path: string) {
  tabsState.value = openPath(tabsState.value, path, "persistent");
}

function selectTab(idx: number) {
  tabsState.value = { ...tabsState.value, activeIdx: idx };
}

function closeTabAt(idx: number) {
  tabsState.value = closeTab(tabsState.value, idx);
}

function togglePin() {
  pinned.value = pinned.value === null ? props.context.activeCwd.value : null;
}

const activePath = computed(() =>
  tabsState.value.activeIdx >= 0 ? tabsState.value.tabs[tabsState.value.activeIdx].path : null,
);

const showHidden = computed(() => store.cfg?.fileExplorer.showHidden ?? false);
</script>

<template>
  <div class="file-explorer">
    <header class="fe-header">
      <span class="root-path" :title="root ?? ''">{{ root ?? "(no active pane)" }}</span>
      <button class="pin" :class="{ pinned: pinned !== null }" :title="pinned ? 'Pinned' : 'Pin root'" @click="togglePin">📌</button>
    </header>
    <div class="fe-body" ref="bodyRef">
      <div class="tree-pane" :style="{ width: (innerRatio * 100) + '%' }">
        <FileTree
          v-if="root"
          :root="root"
          :show-hidden="showHidden"
          @file-clicked="onFileClick"
          @file-double-clicked="onFileDoubleClick"
        />
        <div v-else class="placeholder">No active pane.</div>
      </div>
      <div class="divider" @mousedown="onDividerDown" />
      <div class="editor-pane" :style="{ flex: '1 1 auto' }">
        <FileTabs :tabs="tabsState.tabs" :active-idx="tabsState.activeIdx" @select="selectTab" @close="closeTabAt" />
        <div class="editor-area">
          <FileEditor v-if="activePath" :path="activePath" />
          <div v-else class="placeholder">Select a file.</div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.file-explorer { display: flex; flex-direction: column; height: 100%; color: #c9d1d9; }
.fe-header { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-bottom: 1px solid #2d333b; font-size: 11px; }
.root-path { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; opacity: 0.85; }
.pin { background: none; border: none; cursor: pointer; opacity: 0.5; }
.pin.pinned { opacity: 1; }
.fe-body { flex: 1; display: flex; min-height: 0; }
.tree-pane { min-width: 60px; overflow: auto; border-right: 1px solid #2d333b; flex: 0 0 auto; }
.divider { width: 4px; cursor: col-resize; background: transparent; flex: 0 0 4px; }
.divider:hover { background: #2d333b; }
.editor-pane { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.editor-area { flex: 1; overflow: auto; padding: 8px; }
.placeholder { opacity: 0.5; font-size: 12px; padding: 12px; }
</style>
