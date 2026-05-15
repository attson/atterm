import type { PluginDescriptor } from "./types";

// Adding a new plugin requires (1) a directory under plugins/<id>/ with a
// default Vue export, and (2) a PluginDescriptor entry here. Vite's static
// analysis of import() arguments performs the chunk split — keep the path
// literal (no dynamic strings).
export const PLUGINS: PluginDescriptor[] = [
  // Phase 2 fills in quick-input; Phase 4 fills in file-explorer. Skeleton
  // ships empty so PluginHost has a stable contract from day 1.
];

export function descriptorsForSlot(slot: PluginDescriptor["slot"]): PluginDescriptor[] {
  return PLUGINS.filter((p) => p.slot === slot);
}

export function findDescriptor(id: string): PluginDescriptor | undefined {
  return PLUGINS.find((p) => p.id === id);
}
