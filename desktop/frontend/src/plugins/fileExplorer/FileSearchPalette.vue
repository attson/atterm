<script lang="ts" setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Search, X } from "lucide-vue-next";
import { errText, logWarn } from "../../lib/log";
import { useI18n } from "../../i18n/useI18n";
import type { FileSystemBridge } from "./fsBridge";
import { searchFileNames, type FileNameSearchResult } from "./fileNameSearch";
import { searchFileContents, type FileContentSearchResult } from "./contentSearch";

type SearchResult = FileNameSearchResult | FileContentSearchResult;

const props = defineProps<{
  fs: FileSystemBridge;
  root: string;
  showHidden: boolean;
  query: string;
  mode: "name" | "content";
  caseSensitive: boolean;
  useRegex: boolean;
}>();

const emit = defineEmits<{
  (e: "update:query", value: string): void;
  (e: "update:mode", value: "name" | "content"): void;
  (e: "update:case-sensitive", value: boolean): void;
  (e: "update:use-regex", value: boolean): void;
  (e: "open", path: string): void;
  (e: "close"): void;
}>();

const { t } = useI18n();
const inputRef = ref<HTMLInputElement | null>(null);
const results = ref<SearchResult[]>([]);
const loading = ref(false);
const truncated = ref(false);
let generation = 0;
let disposed = false;

function relativePath(path: string): string {
  const prefix = props.root.endsWith("/") ? props.root : `${props.root}/`;
  return path.startsWith(prefix) ? path.slice(prefix.length) : path;
}

function hasLine(result: SearchResult): result is FileContentSearchResult {
  return "line" in result;
}

async function runSearch() {
  const request = ++generation;
  const query = props.query.trim();
  results.value = [];
  truncated.value = false;
  if (!query) {
    loading.value = false;
    return;
  }
  loading.value = true;
  try {
    const response = props.mode === "content"
      ? await searchFileContents(props.fs, props.root, query, {
        showHidden: props.showHidden,
        caseSensitive: props.caseSensitive,
        useRegex: props.useRegex,
        maxDirs: props.fs.searchMaxDirs,
        isCancelled: () => disposed || generation !== request,
      })
      : await searchFileNames(props.fs, props.root, query, {
        showHidden: props.showHidden,
        maxDirs: props.fs.searchMaxDirs,
        isCancelled: () => disposed || generation !== request,
      });
    if (disposed || generation !== request) return;
    results.value = response.results;
    truncated.value = response.truncated;
  } catch (err) {
    if (disposed || generation !== request) return;
    logWarn("file-explorer", "file search failed", { error: errText(err) });
    results.value = [];
    truncated.value = false;
  } finally {
    if (!disposed && generation === request) loading.value = false;
  }
}

watch(
  () => [
    props.fs,
    props.root,
    props.showHidden,
    props.query,
    props.mode,
    props.caseSensitive,
    props.useRegex,
  ] as const,
  () => { void runSearch(); },
  { immediate: true },
);

onMounted(() => {
  void nextTick(() => inputRef.value?.focus());
});

onBeforeUnmount(() => {
  disposed = true;
  generation++;
});
</script>

<template>
  <div class="search-overlay" data-test="file-search-overlay" @mousedown.self="emit('close')">
    <section class="search-palette" role="dialog" :aria-label="t('plugins.fileExplorer.searchFiles')" @keydown.esc.prevent.stop="emit('close')">
      <div class="palette-input-row">
        <Search :size="15" :stroke-width="1.5" />
        <input
          ref="inputRef"
          :value="query"
          :data-test="mode === 'name' ? 'file-name-search' : 'file-content-search'"
          type="search"
          :placeholder="mode === 'name'
            ? t('plugins.fileExplorer.searchFiles')
            : t('plugins.fileExplorer.searchContent')"
          @input="emit('update:query', ($event.target as HTMLInputElement).value)"
        >
        <button class="close-search" type="button" :title="t('plugins.fileExplorer.closeSearch')" @click="emit('close')">
          <X :size="16" :stroke-width="1.5" />
        </button>
      </div>

      <div class="palette-options">
        <div class="mode-control" role="group">
          <button type="button" :class="{ active: mode === 'name' }" @click="emit('update:mode', 'name')">
            {{ t("plugins.fileExplorer.searchByName") }}
          </button>
          <button type="button" :class="{ active: mode === 'content' }" @click="emit('update:mode', 'content')">
            {{ t("plugins.fileExplorer.searchByContent") }}
          </button>
        </div>
        <template v-if="mode === 'content'">
          <button
            type="button"
            class="search-option"
            :class="{ active: caseSensitive }"
            :title="t('plugins.fileExplorer.searchCaseSensitive')"
            @click="emit('update:case-sensitive', !caseSensitive)"
          >Aa</button>
          <button
            type="button"
            class="search-option"
            :class="{ active: useRegex }"
            :title="t('plugins.fileExplorer.searchRegex')"
            @click="emit('update:use-regex', !useRegex)"
          >.*</button>
        </template>
      </div>

      <div class="search-results" data-test="file-search-results">
        <div v-if="loading" class="search-status">{{ t("common.loading") }}</div>
        <div v-else-if="query.trim() && results.length === 0" class="search-status">
          {{ t("plugins.fileExplorer.noSearchResults") }}
        </div>
        <button
          v-for="result in results"
          :key="hasLine(result) ? `${result.path}:${result.line}` : result.path"
          class="search-result"
          type="button"
          data-test="file-search-result"
          :title="result.path"
          @click="emit('open', result.path)"
        >
          <span class="result-main">
            <span class="result-name">{{ result.name }}</span>
            <span v-if="hasLine(result)" class="result-line">:{{ result.line }}</span>
          </span>
          <span class="result-path">{{ relativePath(result.path) }}</span>
          <span v-if="hasLine(result)" class="result-preview">{{ result.preview }}</span>
        </button>
        <div v-if="truncated" class="search-status">{{ t("plugins.fileExplorer.searchTruncated") }}</div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.search-overlay {
  position: absolute;
  inset: 0;
  z-index: 20;
  padding: 10px;
  background: rgba(15, 18, 22, 0.22);
}
.search-palette {
  display: flex;
  flex-direction: column;
  max-height: min(520px, calc(100% - 8px));
  overflow: hidden;
  border: 1px solid var(--ed-border, #4a5563);
  border-radius: 6px;
  background: var(--ed-tree-bg, #2d333b);
  box-shadow: 0 14px 34px rgba(0, 0, 0, 0.46);
}
.palette-input-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 7px;
  min-height: 40px;
  padding: 6px 7px 6px 10px;
  border-bottom: 1px solid var(--ed-border, #444c56);
  color: var(--ed-muted, rgba(173, 186, 199, 0.7));
  background: var(--ed-tree-header-bg, #22272e);
}
.palette-input-row input {
  width: 100%;
  min-width: 0;
  height: 28px;
  padding: 0 7px;
  border: 1px solid transparent;
  border-radius: 4px;
  outline: none;
  background: transparent;
  color: var(--ed-row-fg, #adbac7);
  font: inherit;
  font-size: 12px;
}
.palette-input-row input:focus {
  border-color: var(--ed-tab-active-bar, #539bf5);
  background: var(--ed-editor-bg, #22272e);
}
.close-search {
  display: inline-grid;
  place-items: center;
  width: 27px;
  height: 27px;
  padding: 0;
  border: 1px solid transparent;
  border-radius: 4px;
  background: transparent;
  color: inherit;
  cursor: pointer;
}
.close-search:hover { border-color: var(--ed-border, #444c56); color: var(--ed-row-fg, #adbac7); }
.palette-options {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 9px;
  border-bottom: 1px solid var(--ed-border, #444c56);
}
.mode-control {
  display: flex;
  overflow: hidden;
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 4px;
}
.mode-control button {
  min-height: 27px;
  padding: 0 10px;
  border: 0;
  border-right: 1px solid var(--ed-border, #444c56);
  background: var(--ed-editor-bg, #22272e);
  color: var(--ed-muted, rgba(173, 186, 199, 0.75));
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}
.mode-control button:last-child { border-right: 0; }
.mode-control button.active {
  background: var(--ed-row-selected, #37373d);
  color: var(--ed-tab-active-fg, #cdd9e5);
  box-shadow: inset 0 -2px var(--ed-tab-active-bar, #539bf5);
}
.search-option {
  width: 30px;
  min-height: 27px;
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 4px;
  background: var(--ed-editor-bg, #22272e);
  color: var(--ed-muted, rgba(173, 186, 199, 0.7));
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}
.search-option.active {
  color: var(--ed-tab-active-fg, #cdd9e5);
  border-color: var(--ed-tab-active-bar, #539bf5);
  background: var(--ed-row-selected, #37373d);
}
.search-results { min-height: 96px; overflow: auto; padding: 4px 0; }
.search-result {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 2px 10px;
  width: 100%;
  min-height: 42px;
  padding: 6px 10px;
  border: 0;
  background: transparent;
  color: var(--ed-row-fg, #adbac7);
  text-align: left;
  cursor: pointer;
}
.search-result:hover, .search-result:focus-visible { background: var(--ed-row-hover, rgba(173, 186, 199, 0.1)); outline: none; }
.result-main { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.result-name { color: var(--ed-tab-active-fg, #cdd9e5); font-size: 12px; }
.result-line { color: var(--ed-muted, rgba(173, 186, 199, 0.7)); font-size: 11px; }
.result-path {
  max-width: 45vw;
  overflow: hidden;
  color: var(--ed-muted, rgba(173, 186, 199, 0.58));
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 10px;
}
.result-preview {
  grid-column: 1 / -1;
  overflow: hidden;
  color: var(--ed-muted, rgba(173, 186, 199, 0.72));
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 10px;
}
.search-status { padding: 13px 10px; color: var(--ed-muted, rgba(173, 186, 199, 0.6)); font-size: 11px; }
</style>
