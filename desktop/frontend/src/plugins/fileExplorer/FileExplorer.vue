<script lang="ts" setup>
import { computed, ref, watch } from "vue";
import { Pin, PinOff } from "lucide-vue-next";
import { usePluginConfigStore } from "../configStore";
import { useResizer } from "../useResizer";
import { isLightTerminalTheme } from "../../lib/terminalThemes";
import FileTree from "./FileTree.vue";
import FileTabs from "./FileTabs.vue";
import FileEditor from "./FileEditor.vue";
import { openPath, closeTab, type TabsState } from "./tabsModel";
import type { PluginContext } from "../types";
import "./theme.css";

const props = defineProps<{ context: PluginContext }>();
const store = usePluginConfigStore();

// In-memory pinned root. Resets to "follow active pane" on app restart per spec.
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

// lastCwd retains the last non-empty cwd we've seen so the panel keeps a
// stable root during the brief window when a freshly-spawned session's
// optimistic SessionInfo entry has been overwritten by a stale server
// listing push, or when the active pane is empty (pre-fill split).
const lastCwd = ref<string>("");
watch(
  () => props.context.activeCwd.value,
  (val) => {
    if (val) lastCwd.value = val;
  },
  { immediate: true },
);

const root = computed<string | null>(() => {
  if (pinned.value) return pinned.value;
  const cur = props.context.activeCwd.value;
  if (cur) return cur;
  return lastCwd.value || null;
});

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
const showLineNumbers = computed(() => store.cfg?.fileExplorer.showLineNumbers ?? false);

// Derive the explorer skin from the terminal theme. Light terminal → light;
// any dark terminal → dimmed.
const explorerTheme = computed<"dimmed" | "light">(() =>
  isLightTerminalTheme(props.context.terminalThemeId.value) ? "light" : "dimmed",
);
</script>

<template>
  <div class="file-explorer" :class="`fe-theme-${explorerTheme}`">
    <header class="fe-header">
      <span class="root-path" :title="root ?? ''">{{ root ?? "(no active pane)" }}</span>
      <button
        class="pin"
        :class="{ pinned: pinned !== null }"
        :title="pinned !== null ? 'Unpin (follow active pane)' : 'Pin current cwd'"
        @click="togglePin"
      >
        <component :is="pinned !== null ? Pin : PinOff" :size="14" :stroke-width="1.5" />
      </button>
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
      <div class="editor-pane">
        <FileTabs
          :tabs="tabsState.tabs"
          :active-idx="tabsState.activeIdx"
          @select="selectTab"
          @close="closeTabAt"
        />
        <div class="editor-area">
          <FileEditor
            v-if="activePath"
            :path="activePath"
            :show-line-numbers="showLineNumbers"
            :theme="explorerTheme"
          />
          <div v-else class="placeholder">Select a file.</div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.file-explorer {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--ed-shell-bg, #22272e);
  color: var(--ed-row-fg, #adbac7);
}
.fe-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-bottom: 1px solid var(--ed-border, #444c56);
  background: var(--ed-tree-header-bg, #22272e);
  font-size: 11px;
  color: var(--ed-tree-header-fg, rgba(173, 186, 199, 0.7));
  text-transform: uppercase;
  letter-spacing: 0.4px;
}
.root-path { flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pin {
  background: none;
  border: none;
  cursor: pointer;
  padding: 0 2px;
  opacity: 0.55;
  color: inherit;
  display: inline-flex;
  align-items: center;
}
.pin:hover { opacity: 1; }
.pin.pinned { opacity: 1; color: var(--ed-tab-active-bar, #539bf5); }

.fe-body { flex: 1 1 auto; display: flex; min-height: 0; }
.tree-pane {
  min-width: 60px;
  overflow: auto;
  background: var(--ed-tree-bg, #2d333b);
  border-right: 1px solid var(--ed-border, #444c56);
  flex: 0 0 auto;
  padding: 4px 0;
}
.tree-pane::-webkit-scrollbar { width: 10px; height: 10px; }
.tree-pane::-webkit-scrollbar-track { background: transparent; }
.tree-pane::-webkit-scrollbar-thumb {
  background: var(--ed-indent-guide, rgba(173, 186, 199, 0.18));
  border-radius: 5px;
}
.tree-pane::-webkit-scrollbar-thumb:hover { background: var(--ed-chevron, rgba(173, 186, 199, 0.3)); }

.divider {
  width: 4px;
  cursor: col-resize;
  background: transparent;
  flex: 0 0 4px;
}
.divider:hover { background: var(--ed-border, #444c56); }

.editor-pane { flex: 1 1 auto; display: flex; flex-direction: column; min-width: 0; }
.editor-area { flex: 1 1 auto; overflow: auto; }
.placeholder {
  color: var(--ed-muted, rgba(173, 186, 199, 0.55));
  font-size: 12px;
  padding: 12px;
}
</style>
