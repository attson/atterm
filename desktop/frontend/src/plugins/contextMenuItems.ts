import type { ContextMenuPlugin, MenuItem, PluginContext } from "./types";
import { errText, logError } from "../lib/log";

// collectContextMenuItems calls each plugin's getMenuItems in registration
// order and concatenates the results. A plugin that throws is skipped with
// console.error so one buggy plugin can't suppress the entire menu.
export async function collectContextMenuItems(
  plugins: ContextMenuPlugin[],
  ctx: PluginContext,
  selection: string,
): Promise<MenuItem[]> {
  const out: MenuItem[] = [];
  for (const p of plugins) {
    try {
      const items = p.getMenuItems(ctx, selection);
      out.push(...items);
    } catch (e) {
      logError("plugin", "context-menu getMenuItems threw", { error: errText(e) });
    }
  }
  return out;
}
