import type { PluginDescriptor } from "../types";

export const fileExplorerDescriptor: PluginDescriptor = {
  id: "file-explorer",
  slot: "right-panel",
  titleKey: "plugins.fileExplorer.title",
  descriptionKey: "plugins.fileExplorer.description",
  load: () => import("./FileExplorer.vue"),
  defaultEnabled: false,
};
