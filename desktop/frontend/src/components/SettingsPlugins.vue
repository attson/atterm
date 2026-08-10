<script lang="ts" setup>
import { errText, logError } from "../lib/log";
import { computed, onMounted } from "vue";
import { usePluginConfigStore } from "../plugins/configStore";
import { PLUGINS } from "../plugins/registry";
import type { PluginID } from "../plugins/types";
import TranslateSettings from "../plugins/translate/TranslateSettings.vue";
import { useI18n } from "../i18n/useI18n";
import { usePlatform } from "../platform";

const store = usePluginConfigStore();
const { t } = useI18n();
const platform = usePlatform();

// Desktop-only plugins (the AI pet needs a second always-on-top OS window)
// would be dead toggles in the browser and on iOS, which share this component.
const visiblePlugins = computed(() =>
  PLUGINS.filter((p) => !p.desktopOnly || platform.caps.wailsBindings),
);

onMounted(async () => {
  if (!store.cfg) await store.load();
});

async function toggle(id: PluginID, enabled: boolean) {
  try {
    await store.setEnabled(id, enabled);
  } catch (err) {
    logError("plugin", "setEnabled failed", { error: errText(err) });
  }
}

</script>

<template>
  <section class="settings-plugins">
    <p class="hint">
      {{ t("settings.plugins.hint") }}
    </p>
    <div v-if="!store.cfg" class="loading">{{ t("common.loading") }}</div>
    <ul v-else class="plugin-list">
      <li v-for="p in visiblePlugins" :key="p.id" class="plugin-row">
        <label class="row-head">
          <input
            type="checkbox"
            :checked="store.isPluginEnabled(p.id)"
            @change="toggle(p.id, ($event.target as HTMLInputElement).checked)"
          />
          <span class="title">{{ t(p.titleKey) }}</span>
        </label>
        <p class="desc">{{ t(p.descriptionKey) }}</p>
        <div v-if="p.id === 'file-explorer' && store.isPluginEnabled('file-explorer')" class="fe-settings">
          <p class="muted">{{ t("settings.plugins.panelDragHint") }}</p>
        </div>
        <TranslateSettings v-if="p.id === 'translate' && store.isPluginEnabled('translate')" />
      </li>
      <li v-if="visiblePlugins.length === 0" class="empty">{{ t("settings.plugins.noneRegistered") }}</li>
    </ul>
  </section>
</template>

<style scoped>
.settings-plugins {
  padding: 12px 16px;
  color: var(--settings-fg, #c9d1d9);
}
.hint {
  margin: 0 0 12px;
  font-size: 12px;
  opacity: 0.7;
}
.plugin-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.plugin-row {
  border: 1px solid #2d333b;
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 8px;
}
.row-head {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
}
.desc {
  margin: 6px 0 0 24px;
  font-size: 12px;
  opacity: 0.7;
}
.empty {
  font-size: 12px;
  opacity: 0.5;
}
.fe-settings { margin-top: 8px; padding-top: 8px; border-top: 1px solid #2d333b; font-size: 12px; display: flex; flex-direction: column; gap: 6px; }
.fe-settings label { display: inline-flex; align-items: center; gap: 6px; }
.fe-settings .muted { margin: 6px 0 0; opacity: 0.6; font-size: 11px; }
</style>
