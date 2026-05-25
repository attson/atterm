import { defineStore } from "pinia";
import { ref, shallowRef } from "vue";
import { t } from "../../i18n";
import { computeAutoTargetLang } from "./detectLang";
import { TranslateError, type TranslateProvider, type TranslateResult } from "./providers/types";

interface HistoryEntry {
  source: string;
  target: string;
  translated: string;
  detectedSrcLang: string;
  at: number;
}

interface PanelError {
  code: TranslateError["code"];
  message: string;
}

interface Config {
  provider: TranslateProvider;
  defaultTargetLang: string;
}

const HISTORY_LIMIT = 5;

export const useTranslatePanelStore = defineStore("translatePanel", () => {
  const visible = ref(false);
  const source = ref("");
  const targetLang = ref("zh-CN");
  const loading = ref(false);
  const error = ref<PanelError | null>(null);
  const result = ref<TranslateResult | null>(null);
  const history = ref<HistoryEntry[]>([]);
  const cfg = shallowRef<Config | null>(null);
  let currentController: AbortController | null = null;

  function configure(next: Config) {
    cfg.value = next;
  }

  function close() {
    visible.value = false;
  }

  async function openWithSource(text: string): Promise<void> {
    if (!cfg.value) {
      visible.value = true;
      error.value = { code: "unknown", message: t("plugins.translate.notConfigured") };
      return;
    }
    visible.value = true;
    source.value = text;
    targetLang.value = computeAutoTargetLang(text, cfg.value.defaultTargetLang);
    void doTranslate();
  }

  async function changeTarget(next: string): Promise<void> {
    targetLang.value = next;
    await doTranslate();
  }

  async function retry(): Promise<void> {
    await doTranslate();
  }

  async function doTranslate() {
    if (!cfg.value) return;
    error.value = null;
    result.value = null;
    loading.value = true;
    currentController?.abort();
    const ctl = new AbortController();
    currentController = ctl;
    try {
      const r = await cfg.value.provider.translate(source.value, targetLang.value, { signal: ctl.signal });
      if (ctl.signal.aborted) return;
      result.value = r;
      history.value = [
        { source: source.value, target: targetLang.value, translated: r.translated, detectedSrcLang: r.detectedSrcLang, at: Date.now() },
        ...history.value,
      ].slice(0, HISTORY_LIMIT);
    } catch (e) {
      if (e instanceof TranslateError && e.code === "aborted") return;
      const code = e instanceof TranslateError ? e.code : "unknown";
      const msg = e instanceof Error ? e.message : String(e);
      error.value = { code, message: msg };
    } finally {
      if (currentController === ctl) loading.value = false;
    }
  }

  function restoreFromHistory(entry: HistoryEntry) {
    source.value = entry.source;
    targetLang.value = entry.target;
    result.value = { translated: entry.translated, detectedSrcLang: entry.detectedSrcLang };
    error.value = null;
    visible.value = true;
  }

  return {
    visible, source, targetLang, loading, error, result, history,
    configure, openWithSource, changeTarget, close, retry, restoreFromHistory,
  };
});
