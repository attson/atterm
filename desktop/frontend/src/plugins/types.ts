import type { Component, ComputedRef, Ref } from "vue";
import type { Endpoint } from "../lib/api";
import type { SessionConnection } from "../lib/connection";
import type { Pane } from "../lib/types";
import type { MessageKey } from "../i18n";

// "companion-window" plugins render into a separate OS window owned by a
// child process rather than anywhere in this window's tree. PluginHost skips
// them (like the headless "context-menu" slot); their lifecycle is driven by
// composables/usePetCompanion.ts.
export type PluginSlot =
  | "right-panel"
  | "bottom-toolbar"
  | "context-menu"
  | "companion-window";

export type PluginID = "quick-input" | "file-explorer" | "translate" | "pet";

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
  activeIsRemote: ComputedRef<boolean>;
  activeSessionConnection: ComputedRef<SessionConnection | null>;
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
  // desktopOnly plugins are hidden from Settings on web and mobile. The pet
  // needs a second always-on-top OS window, which only the Wails build has.
  desktopOnly?: boolean;
}
