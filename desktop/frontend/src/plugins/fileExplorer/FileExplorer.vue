<script lang="ts" setup>
import { computed, ref, watch } from "vue";
import { usePluginConfigStore } from "../configStore";
import FileTree from "./FileTree.vue";
import FileTabs from "./FileTabs.vue";
import { openPath, closeTab, type TabsState } from "./tabsModel";
import type { PluginContext } from "../types";

const props = defineProps<{ context: PluginContext }>();
const store = usePluginConfigStore();

// Pinned root (in-memory only; resets on app restart per spec).
const pinned = ref<string | null>(null);

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
    <div class="fe-body">
      <div class="tree-pane">
        <FileTree
          v-if="root"
          :root="root"
          :show-hidden="showHidden"
          @file-clicked="onFileClick"
          @file-double-clicked="onFileDoubleClick"
        />
        <div v-else class="placeholder">No active pane.</div>
      </div>
      <div class="editor-pane">
        <FileTabs :tabs="tabsState.tabs" :active-idx="tabsState.activeIdx" @select="selectTab" @close="closeTabAt" />
        <div class="editor-area">
          <div v-if="!activePath" class="placeholder">Select a file.</div>
          <div v-else class="placeholder">Preview placeholder: {{ activePath }}</div>
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
.tree-pane { width: 30%; min-width: 120px; overflow: auto; border-right: 1px solid #2d333b; }
.editor-pane { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.editor-area { flex: 1; overflow: auto; padding: 8px; }
.placeholder { opacity: 0.5; font-size: 12px; padding: 12px; }
</style>
