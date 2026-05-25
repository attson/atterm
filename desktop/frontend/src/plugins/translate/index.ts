import type { ContextMenuPlugin, MenuItem, PluginContext, PluginDescriptor } from "../types";
import { useTranslatePanelStore } from "./panelStore";
import { t } from "../../i18n";

const contextMenuImpl: ContextMenuPlugin = {
  getMenuItems(_ctx: PluginContext, selection: string): MenuItem[] {
    if (!selection || !selection.trim()) return [];
    return [
      {
        id: "translate-selection",
        label: t("plugins.translate.selection"),
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
  titleKey: "plugins.translate.title",
  descriptionKey: "plugins.translate.description",
  load: async () => ({ default: contextMenuImpl }),
  defaultEnabled: false,
};
