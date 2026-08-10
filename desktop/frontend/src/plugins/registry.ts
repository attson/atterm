import type { PluginDescriptor } from "./types";
import { fileExplorerDescriptor } from "./fileExplorer";
import { translateDescriptor } from "./translate";
import { petDescriptor } from "./pet";

// Adding a new plugin requires (1) a directory under plugins/<id>/ with a
// default Vue export, and (2) a PluginDescriptor entry here. Vite's static
// analysis of import() arguments performs the chunk split — keep the path
// literal (no dynamic strings).
export const PLUGINS: PluginDescriptor[] = [
  fileExplorerDescriptor,
  translateDescriptor,
  petDescriptor,
];

export function descriptorsForSlot(slot: PluginDescriptor["slot"]): PluginDescriptor[] {
  return PLUGINS.filter((p) => p.slot === slot);
}
