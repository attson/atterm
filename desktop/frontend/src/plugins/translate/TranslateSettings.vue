<script lang="ts" setup>
import { computed } from "vue";
import { usePluginConfigStore, type PluginConfig } from "../configStore";
import { useI18n } from "../../i18n/useI18n";
import type { MessageKey } from "../../i18n";

const store = usePluginConfigStore();
const { t: i18nT } = useI18n();

const TARGETS = [
  { code: "zh-CN", labelKey: "plugins.translate.simplifiedChinese" },
  { code: "en", labelKey: "plugins.translate.targetEnglish" },
  { code: "ja", labelKey: "plugins.translate.targetJapanese" },
  { code: "ko", labelKey: "plugins.translate.targetKorean" },
  { code: "de", labelKey: "plugins.translate.targetGerman" },
  { code: "fr", labelKey: "plugins.translate.targetFrench" },
  { code: "es", labelKey: "plugins.translate.targetSpanish" },
] satisfies { code: string; labelKey: MessageKey }[];

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
      <span>{{ i18nT("plugins.translate.baseUrl") }}</span>
      <input
        type="text"
        :value="t.baseUrl"
        @change="update({ baseUrl: ($event.target as HTMLInputElement).value })"
        placeholder="https://api.openai.com"
      />
    </label>
    <label>
      <span>{{ i18nT("plugins.translate.apiKey") }}</span>
      <input
        type="password"
        :value="t.apiKey"
        @change="update({ apiKey: ($event.target as HTMLInputElement).value })"
        placeholder="sk-..."
      />
    </label>
    <label>
      <span>{{ i18nT("plugins.translate.model") }}</span>
      <input
        type="text"
        :value="t.model"
        @change="update({ model: ($event.target as HTMLInputElement).value })"
        placeholder="gpt-4o-mini"
      />
    </label>
    <label>
      <span>{{ i18nT("plugins.translate.defaultTargetLanguage") }}</span>
      <select
        :value="t.defaultTargetLang"
        @change="update({ defaultTargetLang: ($event.target as HTMLSelectElement).value })"
      >
        <option v-for="opt in TARGETS" :key="opt.code" :value="opt.code">{{ i18nT(opt.labelKey) }}</option>
      </select>
    </label>
    <p class="muted">{{ i18nT("plugins.translate.keyStoredPlaintext", { path: "~/.config/atterm/config.json" }) }}</p>
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
