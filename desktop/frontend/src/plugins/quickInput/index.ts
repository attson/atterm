import type { PluginDescriptor } from "../types";

export const quickInputDescriptor: PluginDescriptor = {
  id: "quick-input",
  slot: "bottom-toolbar",
  title: "Quick Input",
  description:
    "Bottom toolbar of user-defined buttons that send text to the active pane.",
  load: () => import("./QuickInputBar.vue"),
  defaultEnabled: true,
};
