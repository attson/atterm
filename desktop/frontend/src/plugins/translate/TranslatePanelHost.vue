<script lang="ts" setup>
import { computed, watchEffect } from "vue";
import { useTranslatePanelStore } from "./panelStore";
import { usePluginConfigStore } from "../configStore";
import { createOpenAIProvider } from "./providers/openai";
import TranslatePanel from "./TranslatePanel.vue";
import { t } from "../../i18n";

const panel = useTranslatePanelStore();
const cfgStore = usePluginConfigStore();

const translateCfg = computed(() => cfgStore.cfg?.translate ?? null);

// Re-configure the panel store whenever the plugin config changes.
watchEffect(() => {
  const cfg = translateCfg.value;
  if (!cfg || !cfg.apiKey || !cfg.baseUrl || !cfg.model) {
    panel.configure({
      provider: { translate: async () => { throw new Error(t("plugins.translate.notConfigured")); } },
      defaultTargetLang: cfg?.defaultTargetLang || "zh-CN",
    });
    return;
  }
  const provider = createOpenAIProvider({
    baseUrl: cfg.baseUrl,
    apiKey: cfg.apiKey,
    model: cfg.model,
  });
  panel.configure({ provider, defaultTargetLang: cfg.defaultTargetLang });
});
</script>

<template>
  <TranslatePanel />
</template>
