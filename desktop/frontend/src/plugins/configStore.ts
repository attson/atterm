import { defineStore } from "pinia";
import { ref } from "vue";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { GetPluginConfig, SetPluginConfig } from "../../wailsjs/go/main/App";
import type { PluginID } from "./types";
import type { main } from "../../wailsjs/go/models";

// Re-export Wails types for convenience
export type QuickInputButton = main.QuickInputButton;
export type QuickInputConfig = main.QuickInputConfig;
export type FileExplorerConfig = main.FileExplorerConfig;
export type PluginConfig = main.PluginConfig;

let unsubscribe: (() => void) | null = null;

export const usePluginConfigStore = defineStore("pluginConfig", () => {
  const cfg = ref<PluginConfig | null>(null);

  async function load() {
    cfg.value = (await GetPluginConfig()) as PluginConfig;
    if (!unsubscribe) {
      unsubscribe = EventsOn("plugin-config-changed", (next: PluginConfig) => {
        cfg.value = next;
      });
    }
  }

  async function save(next: PluginConfig) {
    await SetPluginConfig(next);
    cfg.value = next;
  }

  function isPluginEnabled(id: PluginID): boolean {
    if (!cfg.value) return false;
    if (id === "quick-input") return cfg.value.quickInput.enabled;
    if (id === "file-explorer") return cfg.value.fileExplorer.enabled;
    return false;
  }

  async function setEnabled(id: PluginID, enabled: boolean) {
    if (!cfg.value) return;
    const next: PluginConfig = JSON.parse(JSON.stringify(cfg.value));
    if (id === "quick-input") next.quickInput.enabled = enabled;
    if (id === "file-explorer") next.fileExplorer.enabled = enabled;
    await save(next);
  }

  return { cfg, load, save, isPluginEnabled, setEnabled };
});
