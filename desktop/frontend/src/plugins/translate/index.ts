import type { ContextMenuPlugin, MenuItem, PluginContext, PluginDescriptor } from "../types";
import { useTranslatePanelStore } from "./panelStore";

const contextMenuImpl: ContextMenuPlugin = {
  getMenuItems(_ctx: PluginContext, selection: string): MenuItem[] {
    if (!selection || !selection.trim()) return [];
    return [
      {
        id: "translate-selection",
        label: "Translate selection",
        onClick: () => {
          const store = useTranslatePanelStore();
          void store.openWithSource(selection);
        },
      },
    ];
  },
};

export const translateDescriptor: PluginDescriptor = {
  id: "translate",
  slot: "context-menu",
  title: "Translate",
  description: "Translate the selected text via an OpenAI-compatible API; result shown in a floating panel.",
  load: async () => ({ default: contextMenuImpl }),
  defaultEnabled: false,
};
