import type { Component, ComputedRef, Ref } from "vue";
import type { Endpoint } from "../lib/api";
import type { Pane } from "../lib/types";

export type PluginSlot = "right-panel" | "bottom-toolbar";

export type PluginID = "quick-input" | "file-explorer";

export interface PluginContext {
  activePane: Ref<Pane | null>;
  activeSessionId: ComputedRef<string | null>;
  activeEndpoint: ComputedRef<Endpoint | null>;
  activeCwd: ComputedRef<string | null>;
  send: (text: string) => void;
  showToast: (msg: string) => void;
}

export interface PluginDescriptor {
  id: PluginID;
  slot: PluginSlot;
  title: string;
  description: string;
  load: () => Promise<{ default: Component }>;
  defaultEnabled: boolean;
}
