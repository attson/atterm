<script lang="ts" setup>
import { computed, watchEffect } from "vue";
import { useTranslatePanelStore } from "./panelStore";
import { usePluginConfigStore } from "../configStore";
import { createOpenAIProvider, type OpenAITransport } from "./providers/openai";
import TranslatePanel from "./TranslatePanel.vue";
import { t } from "../../i18n";
import { usePlatform } from "../../platform";
import { TranslateOpenAIChat } from "../../../wailsjs/go/main/App";
import { main } from "../../../wailsjs/go/models";

const panel = useTranslatePanelStore();
const cfgStore = usePluginConfigStore();
const platform = usePlatform();

const translateCfg = computed(() => cfgStore.cfg?.translate ?? null);

// wailsTransport proxies the OpenAI chat/completions POST through Go.
// WKWebView blocks direct fetch() to user-supplied third-party endpoints
// (no CORS, cert quirks) with a bare "Load failed"; routing through Go
// bypasses the webview network stack entirely. Body is passed opaquely so
// user's extraParams (stream, top_p, ...) survive unchanged; Go collapses
// SSE responses back to a non-stream envelope.
function wailsTransport(cfg: { baseUrl: string; apiKey: string }): OpenAITransport {
  return async (_url, init) => {
    const req = main.TranslateHTTPRequest.createFrom({
      baseUrl: cfg.baseUrl,
      apiKey: cfg.apiKey,
      body: init.body as string,
      timeoutSeconds: 30,
    });
    const resp = await TranslateOpenAIChat(req);
    return new Response(resp.body, {
      status: resp.status,
      headers: { "Content-Type": "application/json" },
    });
  };
}

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
  const provider = createOpenAIProvider(
    { baseUrl: cfg.baseUrl, apiKey: cfg.apiKey, model: cfg.model, extraParams: cfg.extraParams },
    platform.caps.wailsBindings
      ? { transport: wailsTransport({ baseUrl: cfg.baseUrl, apiKey: cfg.apiKey }) }
      : undefined,
  );
  panel.configure({ provider, defaultTargetLang: cfg.defaultTargetLang });
});
</script>

<template>
  <TranslatePanel />
</template>
