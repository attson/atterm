import type { PresetId } from "../taskState";
import type { QuickTemplate } from "../templates";
import { bindings } from "./_bindings";
import type { MarkSessionsSeenOpts, TaskGroupBy } from "./_bindings";

export type { MarkSessionsSeenOpts, TaskGroupBy } from "./_bindings";

export function getQuickTemplates(): Promise<QuickTemplate[]> {
  return bindings().GetQuickTemplates();
}

export function setQuickTemplates(list: QuickTemplate[]): Promise<void> {
  return bindings().SetQuickTemplates(list);
}

export function getTaskPreset(): Promise<string> {
  return bindings().GetTaskPreset();
}

export function setTaskPreset(preset: PresetId): Promise<void> {
  return bindings().SetTaskPreset(preset);
}

export function getTaskGroupBy(): Promise<string> {
  return bindings().GetTaskGroupBy();
}

export function setTaskGroupBy(groupBy: TaskGroupBy): Promise<void> {
  return bindings().SetTaskGroupBy(groupBy);
}

export function getTaskSidebarCollapsed(): Promise<boolean> {
  return bindings().GetTaskSidebarCollapsed();
}

export function setTaskSidebarCollapsed(collapsed: boolean): Promise<void> {
  return bindings().SetTaskSidebarCollapsed(collapsed);
}

export function getTaskSidebarWidth(): Promise<number> {
  return bindings().GetTaskSidebarWidth();
}

export function setTaskSidebarWidth(px: number): Promise<void> {
  return bindings().SetTaskSidebarWidth(px);
}

export function getPinnedSessionIds(): Promise<string[]> {
  return bindings().GetPinnedSessionIds();
}

export function setPinnedSessionIds(ids: string[]): Promise<void> {
  return bindings().SetPinnedSessionIds(ids);
}

export function markSessionsSeen(opts: MarkSessionsSeenOpts): Promise<void> {
  if ("all" in opts && opts.all) {
    return bindings().MarkSessionsSeen([], true);
  }
  return bindings().MarkSessionsSeen((opts as { ids: string[] }).ids, false);
}

export function broadcastCommandFinished(
  sessionId: string,
  exitCode: number,
  elapsedMs: number,
  label: string,
): Promise<void> {
  return bindings().BroadcastCommandFinished(sessionId, exitCode, elapsedMs, label);
}
