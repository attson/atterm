import type { PluginDescriptor } from "../types";

export const fileExplorerDescriptor: PluginDescriptor = {
  id: "file-explorer",
  slot: "right-panel",
  title: "File Explorer",
  description:
    "Side panel with file tree and read-only syntax-highlighted preview. Follows the active pane's cwd.",
  load: () => import("./FileExplorer.vue"),
  defaultEnabled: false,
};
