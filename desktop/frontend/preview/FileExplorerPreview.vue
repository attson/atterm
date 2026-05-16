<script lang="ts" setup>
import { computed, ref } from "vue";
import FileTreePreview from "./FileTreePreview.vue";
import FileTabsPreview from "./FileTabsPreview.vue";
import FileEditorPreview from "./FileEditorPreview.vue";
import {
  openPath,
  closeTab,
  type TabsState,
} from "../src/plugins/fileExplorer/tabsModel";
import { flatten, type MockEntry } from "./mockData";

const props = defineProps<{
  root: string;
  entries: MockEntry[];
  showLineNumbers: boolean;
  theme: "dimmed" | "light";
}>();

const tabsState = ref<TabsState>({ tabs: [], activeIdx: -1 });
const selectedPath = ref<string>("");

const entryMap = computed(() => flatten(props.root, props.entries));

function onSelect(path: string) {
  selectedPath.value = path;
  const entry = entryMap.value.get(path);
  if (!entry || entry.isDir) return;
  tabsState.value = openPath(tabsState.value, path, "preview");
}

function onDblclick(path: string) {
  selectedPath.value = path;
  const entry = entryMap.value.get(path);
  if (!entry || entry.isDir) return;
  tabsState.value = openPath(tabsState.value, path, "persistent");
}

function selectTab(idx: number) {
  tabsState.value = { ...tabsState.value, activeIdx: idx };
  const t = tabsState.value.tabs[idx];
  if (t) selectedPath.value = t.path;
}

function closeAt(idx: number) {
  tabsState.value = closeTab(tabsState.value, idx);
  const t = tabsState.value.tabs[tabsState.value.activeIdx];
  selectedPath.value = t ? t.path : "";
}

const activeTab = computed(() =>
  tabsState.value.activeIdx >= 0
    ? tabsState.value.tabs[tabsState.value.activeIdx]
    : null,
);
const activeEntry = computed(() =>
  activeTab.value ? entryMap.value.get(activeTab.value.path) ?? null : null,
);
const activePath = computed(() => (activeTab.value ? activeTab.value.path : null));
</script>

<template>
  <div class="explorer">
    <div class="tree-pane">
      <FileTreePreview
        :root="root"
        :entries="entries"
        :selected-path="selectedPath"
        @select="onSelect"
        @dblclick="onDblclick"
      />
    </div>
    <div class="divider" />
    <div class="editor-pane">
      <FileTabsPreview
        :tabs="tabsState.tabs"
        :active-idx="tabsState.activeIdx"
        @select="selectTab"
        @close="closeAt"
      />
      <div class="editor-body">
        <FileEditorPreview
          :path="activePath"
          :entry="activeEntry"
          :show-line-numbers="showLineNumbers"
          :theme="theme"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.explorer {
  display: flex;
  height: 100%;
  background: var(--ed-shell-bg, #1e1e1e);
  border: 1px solid var(--ed-border, #1a1a1a);
  border-radius: 4px;
  overflow: hidden;
}
.tree-pane {
  flex: 0 0 240px;
  min-width: 180px;
  border-right: 1px solid var(--ed-border, #1a1a1a);
}
.divider {
  flex: 0 0 1px;
  background: var(--ed-border, #1a1a1a);
}
.editor-pane {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.editor-body {
  flex: 1 1 auto;
  min-height: 0;
}
</style>
