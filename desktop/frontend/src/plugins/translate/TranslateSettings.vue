<script lang="ts" setup>
import { errText, logError } from "../../lib/log";
import { computed, ref, watch } from "vue";
import { usePluginConfigStore, type PluginConfig } from "../configStore";
import { useI18n } from "../../i18n/useI18n";
import type { MessageKey } from "../../i18n";
import SelectDropdown from "../../components/SelectDropdown.vue";

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

const targetOptions = computed(() =>
  TARGETS.map((o) => ({ value: o.code, label: i18nT(o.labelKey) })),
);
const defaultTargetModel = computed({
  get: () => t.value?.defaultTargetLang ?? "zh-CN",
  set: (v: string) => { void update({ defaultTargetLang: v }); },
});

// Local draft for extraParams so the textarea can hold invalid JSON while
// the user is editing without triggering a save. Persist only when it's
// empty or parses to a JSON object.
const extraParamsDraft = ref<string>(t.value?.extraParams ?? "");
const extraParamsError = ref<string>("");
watch(
  () => t.value?.extraParams ?? "",
  (v) => { if (v !== extraParamsDraft.value) extraParamsDraft.value = v; },
);

function onExtraParamsInput(e: Event) {
  const v = (e.target as HTMLTextAreaElement).value;
  extraParamsDraft.value = v;
  const trimmed = v.trim();
  if (trimmed === "") {
    extraParamsError.value = "";
    void update({ extraParams: "" });
    return;
  }
  let parsed: unknown;
  try { parsed = JSON.parse(trimmed); } catch (err) {
    extraParamsError.value = err instanceof Error ? err.message : String(err);
    return;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    extraParamsError.value = i18nT("plugins.translate.extraParamsInvalid");
    return;
  }
  extraParamsError.value = "";
  void update({ extraParams: trimmed });
}

async function update(patch: Partial<PluginConfig["translate"]>) {
  if (!store.cfg) return;
  const next = JSON.parse(JSON.stringify(store.cfg)) as PluginConfig;
  next.translate = { ...next.translate, ...patch };
  try { await store.save(next); } catch (err) { logError("plugin-translate", "save config failed", { error: errText(err) }); }
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
      <SelectDropdown
        v-model="defaultTargetModel"
        :options="targetOptions"
        :aria-label="i18nT('plugins.translate.defaultTargetLanguage')"
      />
    </label>
    <label>
      <span>{{ i18nT("plugins.translate.extraParams") }}</span>
      <textarea
        class="extra-params"
        rows="3"
        spellcheck="false"
        :value="extraParamsDraft"
        @input="onExtraParamsInput"
        placeholder='{"stream": true, "top_p": 0.9}'
      ></textarea>
      <span v-if="extraParamsError" class="extra-params-error">{{ extraParamsError }}</span>
      <span v-else class="hint">{{ i18nT("plugins.translate.extraParamsHint") }}</span>
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
.extra-params {
  background: rgba(0, 0, 0, 0.25); color: inherit; border: 1px solid #2d333b; border-radius: 3px;
  padding: 4px 6px; font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  resize: vertical; min-height: 56px;
}
.extra-params-error { color: #f87171; font-size: 11px; }
.hint { opacity: 0.55; font-size: 11px; }
.muted { opacity: 0.55; font-size: 11px; margin: 4px 0 0; }
</style>
