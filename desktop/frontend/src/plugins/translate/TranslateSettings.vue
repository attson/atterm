<script lang="ts" setup>
import { computed } from "vue";
import { usePluginConfigStore, type PluginConfig } from "../configStore";

const store = usePluginConfigStore();

const TARGETS = [
  { code: "zh-CN", label: "中文 (Simplified)" },
  { code: "en", label: "English" },
  { code: "ja", label: "日本語" },
  { code: "ko", label: "한국어" },
  { code: "de", label: "Deutsch" },
  { code: "fr", label: "Français" },
  { code: "es", label: "Español" },
];

const t = computed(() => store.cfg?.translate);

async function update(patch: Partial<PluginConfig["translate"]>) {
  if (!store.cfg) return;
  const next = JSON.parse(JSON.stringify(store.cfg)) as PluginConfig;
  next.translate = { ...next.translate, ...patch };
  try { await store.save(next); } catch (err) { console.error("save translate cfg", err); }
}
</script>

<template>
  <div v-if="t" class="translate-settings">
    <label>
      <span>Base URL</span>
      <input
        type="text"
        :value="t.baseUrl"
        @change="update({ baseUrl: ($event.target as HTMLInputElement).value })"
        placeholder="https://api.openai.com"
      />
    </label>
    <label>
      <span>API Key</span>
      <input
        type="password"
        :value="t.apiKey"
        @change="update({ apiKey: ($event.target as HTMLInputElement).value })"
        placeholder="sk-..."
      />
    </label>
    <label>
      <span>Model</span>
      <input
        type="text"
        :value="t.model"
        @change="update({ model: ($event.target as HTMLInputElement).value })"
        placeholder="gpt-4o-mini"
      />
    </label>
    <label>
      <span>Default target language</span>
      <select
        :value="t.defaultTargetLang"
        @change="update({ defaultTargetLang: ($event.target as HTMLSelectElement).value })"
      >
        <option v-for="opt in TARGETS" :key="opt.code" :value="opt.code">{{ opt.label }}</option>
      </select>
    </label>
    <p class="muted">API key is stored plaintext in <code>~/.config/atterm/config.json</code>.</p>
  </div>
</template>

<style scoped>
.translate-settings { margin-top: 8px; padding-top: 8px; border-top: 1px solid #2d333b; font-size: 12px; display: flex; flex-direction: column; gap: 8px; }
.translate-settings label { display: flex; flex-direction: column; gap: 3px; }
.translate-settings label span { opacity: 0.7; font-size: 11px; }
.translate-settings input, .translate-settings select {
  background: rgba(0, 0, 0, 0.25); color: inherit; border: 1px solid #2d333b; border-radius: 3px; padding: 4px 6px; font-size: 12px;
}
.muted { opacity: 0.55; font-size: 11px; margin: 4px 0 0; }
</style>
