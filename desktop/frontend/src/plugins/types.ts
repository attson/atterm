import type { Component, ComputedRef, Ref } from "vue";
import type { Endpoint } from "../lib/api";
import type { Pane } from "../lib/types";
import type { MessageKey } from "../i18n";

export type PluginSlot = "right-panel" | "bottom-toolbar" | "context-menu";

export type PluginID = "quick-input" | "file-explorer" | "translate";

export interface MenuItem {
  id: string;
  label: string;
  disabled?: boolean;
  onClick: () => void;
}

export interface ContextMenuPlugin {
  getMenuItems(ctx: PluginContext, selection: string): MenuItem[];
}

export interface PluginContext {
  activePane: Ref<Pane | null>;
  activeSessionId: ComputedRef<string | null>;
  activeEndpoint: ComputedRef<Endpoint | null>;
  activeCwd: ComputedRef<string | null>;
  // Current terminal theme id (e.g. "classic" / "daylight"). Plugins use it
  // to pick a matching dark/light skin via isLightTerminalTheme().
  terminalThemeId: ComputedRef<string>;
  send: (text: string) => void;
  showToast: (msg: string) => void;
}

export interface PluginDescriptor {
  id: PluginID;
  slot: PluginSlot;
  titleKey: MessageKey;
  descriptionKey: MessageKey;
  // Union return type so context-menu plugins can load a headless module.
  load: () =>
    | Promise<{ default: Component }>
    | Promise<{ default: ContextMenuPlugin }>;
  defaultEnabled?: boolean;
}
