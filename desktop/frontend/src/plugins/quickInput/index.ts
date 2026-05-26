import type { PluginDescriptor } from "../types";

export const quickInputDescriptor: PluginDescriptor = {
  id: "quick-input",
  slot: "bottom-toolbar",
  titleKey: "plugins.quickInput.title",
  descriptionKey: "plugins.quickInput.description",
  load: () => import("./QuickInputBar.vue"),
  defaultEnabled: true,
};
