<script lang="ts" setup>
import { computed, watchEffect } from "vue";
import { useTranslatePanelStore } from "./panelStore";
import { usePluginConfigStore } from "../configStore";
import { createOpenAIProvider } from "./providers/openai";
import TranslatePanel from "./TranslatePanel.vue";

const panel = useTranslatePanelStore();
const cfgStore = usePluginConfigStore();

const translateCfg = computed(() => cfgStore.cfg?.translate ?? null);

// Re-configure the panel store whenever the plugin config changes.
watchEffect(() => {
  const t = translateCfg.value;
  if (!t || !t.apiKey || !t.baseUrl || !t.model) {
    panel.configure({
      provider: { translate: async () => { throw new Error("Translate plugin not configured"); } },
      defaultTargetLang: t?.defaultTargetLang || "zh-CN",
    });
    return;
  }
  const provider = createOpenAIProvider({
    baseUrl: t.baseUrl,
    apiKey: t.apiKey,
    model: t.model,
  });
  panel.configure({ provider, defaultTargetLang: t.defaultTargetLang });
});
</script>

<template>
  <TranslatePanel />
</template>
