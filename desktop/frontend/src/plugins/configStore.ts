import { defineStore } from "pinia";
import { ref } from "vue";
import { usePlatform, type PluginModels } from '../platform'
import type { PluginID } from "./types";

// Re-export types for convenience
export type FileExplorerConfig = PluginModels.FileExplorerConfig;
export type TranslateConfig = PluginModels.TranslateConfig;
export type ShortcutsConfig = PluginModels.ShortcutsConfig;
export type PluginConfig = PluginModels.PluginConfig;

let unsubscribe: (() => void) | null = null;

export const usePluginConfigStore = defineStore("pluginConfig", () => {
  const cfg = ref<PluginConfig | null>(null);

  async function load() {
    const platform = usePlatform();
    if (!platform.pluginHost) throw new Error('plugin host unavailable');
    cfg.value = await platform.pluginHost.getPluginConfig();
    if (!unsubscribe) {
      unsubscribe = platform.events.on("plugin-config-changed", (data) => {
        cfg.value = data as PluginConfig;
      });
    }
  }

  async function save(next: PluginConfig) {
    const platform = usePlatform();
    if (!platform.pluginHost) throw new Error('plugin host unavailable');
    await platform.pluginHost.setPluginConfig(next);
    cfg.value = next;
  }

  function isPluginEnabled(id: PluginID): boolean {
    if (!cfg.value) return false;
    if (id === "file-explorer") return cfg.value.fileExplorer.enabled;
    if (id === "translate") return cfg.value.translate.enabled;
    // The widget block was added after the others, so a config.json written by an
    // older build has no `widget` key at all — guard rather than throw during the
    // boot chain (red line #19 wants boot failures attributable, not silent).
    if (id === "desk-widget") return cfg.value.widget?.enabled ?? false;
    return false;
  }

  async function setEnabled(id: PluginID, enabled: boolean) {
    if (!cfg.value) return;
    const next: PluginConfig = JSON.parse(JSON.stringify(cfg.value));
    if (id === "file-explorer") next.fileExplorer.enabled = enabled;
    if (id === "translate") next.translate.enabled = enabled;
    if (id === "desk-widget" && next.widget) next.widget.enabled = enabled;
    await save(next);
  }

  return { cfg, load, save, isPluginEnabled, setEnabled };
});
