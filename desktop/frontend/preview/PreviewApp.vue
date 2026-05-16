<script lang="ts" setup>
import { onBeforeUnmount, ref, watch } from "vue";
import FileExplorerPreview from "./FileExplorerPreview.vue";
import FileTreePreview from "./FileTreePreview.vue";
import { datasets } from "./mockData";

type ThemeID = "dimmed" | "light";

const activeDatasetId = ref(datasets[0].id);
const activeTheme = ref<ThemeID>("dimmed");
const showLineNumbers = ref(false);

// Mirror the theme class onto <html> so getComputedStyle(document.documentElement)
// (used by FileEditor / FileTreeNode to read CSS custom properties) sees the
// switched palette. Without this, those reads would only see html's own
// undefined vars and silently fall back to dark defaults.
function applyHtmlTheme(theme: ThemeID) {
  const html = document.documentElement;
  html.classList.remove("theme-dimmed", "theme-light");
  html.classList.add(`theme-${theme}`);
}
watch(activeTheme, applyHtmlTheme, { immediate: true });
onBeforeUnmount(() => {
  document.documentElement.classList.remove("theme-dimmed", "theme-light");
});

const themes: { id: ThemeID; label: string; hint: string }[] = [
  { id: "dimmed", label: "GitHub Dimmed", hint: "Used when terminal is dark (classic / nord / solarized-dark)" },
  { id: "light", label: "GitHub Light", hint: "Used when terminal is daylight" },
];

function dataset(id: string) {
  return datasets.find((d) => d.id === id) ?? datasets[0];
}

const treeWidths = [180, 240, 320, 420];
</script>

<template>
  <div class="page" :class="`theme-${activeTheme}`">
    <header class="page-header">
      <h1>File Explorer — Style Preview</h1>
      <p class="hint">
        Auto-follows the terminal theme in atterm — light terminal → Light explorer; any dark terminal → Dimmed explorer. The picker below is only for visual comparison here.
      </p>

      <div class="picker">
        <span class="picker-label">Theme:</span>
        <button
          v-for="t in themes"
          :key="t.id"
          :class="{ active: activeTheme === t.id }"
          :title="t.hint"
          @click="activeTheme = t.id"
        >{{ t.label }}</button>
      </div>

      <div class="picker">
        <span class="picker-label">Dataset:</span>
        <button
          v-for="d in datasets"
          :key="d.id"
          :class="{ active: activeDatasetId === d.id }"
          @click="activeDatasetId = d.id"
        >{{ d.label }}</button>
      </div>

      <div class="picker">
        <span class="picker-label">Editor:</span>
        <label class="toggle">
          <input type="checkbox" v-model="showLineNumbers" />
          <span>Show line numbers</span>
        </label>
        <span class="toggle-hint">(defaults to off; exposed in Settings → Plugins → File Explorer)</span>
      </div>
    </header>

    <section class="section">
      <h2>Full explorer (tree + tabs + editor)</h2>
      <div class="explorer-frame">
        <FileExplorerPreview
          :root="dataset(activeDatasetId).root"
          :entries="dataset(activeDatasetId).entries"
          :show-line-numbers="showLineNumbers"
          :theme="activeTheme"
        />
      </div>
    </section>

    <section class="section">
      <h2>Tree at multiple widths (ellipsis &amp; indent guides)</h2>
      <div class="trees">
        <div
          v-for="w in treeWidths"
          :key="w"
          class="tree-cell"
          :style="{ width: `${w}px` }"
        >
          <div class="tree-meta">Width: {{ w }} px</div>
          <div class="tree-body">
            <FileTreePreview
              :root="dataset(activeDatasetId).root"
              :entries="dataset(activeDatasetId).entries"
            />
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.page {
  min-height: 100vh;
  padding: 24px 24px 48px;
  background: var(--ed-page-bg, #22272e);
  color: var(--ed-page-fg, #adbac7);
  transition: background 0.15s ease, color 0.15s ease;
}
.page-header { margin-bottom: 24px; }
.page-header h1 {
  margin: 0 0 4px;
  font-size: 18px;
  font-weight: 600;
}
.hint {
  margin: 0 0 16px;
  font-size: 12px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.7));
}
.picker {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  margin-bottom: 8px;
}
.picker-label {
  min-width: 64px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.8));
}
.picker button {
  background: var(--ed-control-bg, #2d333b);
  border: 1px solid var(--ed-control-border, #444c56);
  color: var(--ed-control-fg, #adbac7);
  padding: 4px 10px;
  border-radius: 3px;
  cursor: pointer;
  font-size: 12px;
}
.picker button:hover { filter: brightness(1.1); }
.picker button.active {
  background: var(--ed-control-active-bg, #316dca);
  border-color: var(--ed-control-active-border, #539bf5);
  color: var(--ed-control-active-fg, #ffffff);
}
.toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
.toggle input { margin: 0; }
.toggle-hint {
  font-size: 11px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.55));
}

.section { margin-top: 32px; }
.section h2 {
  margin: 0 0 12px;
  font-size: 13px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.7));
  font-weight: 500;
}
.explorer-frame {
  height: 560px;
  max-width: 1000px;
}
.trees {
  display: flex;
  flex-wrap: wrap;
  gap: 24px;
  align-items: flex-start;
}
.tree-cell {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.tree-meta {
  font-size: 11px;
  color: var(--ed-muted, rgba(173, 186, 199, 0.55));
  text-transform: uppercase;
  letter-spacing: 0.4px;
}
.tree-body { height: 440px; }
</style>
