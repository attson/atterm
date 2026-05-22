// Pure, framework-agnostic helpers for terminal shortcut bindings.
//
// A binding string has the form "Mod+Alt+Shift+<code>" where:
//   - "Mod" is a platform-agnostic modifier (Meta on mac, Control elsewhere).
//   - Token order is fixed: Mod, Alt, Shift, code — only the modifiers
//     actually pressed appear.
//   - <code> is a KeyboardEvent.code from a known whitelist.
//   - The empty string means "disabled" — this action will not be routed.

export type ShortcutGroup = "pane" | "tab";

export interface ShortcutAction {
  id: string;
  group: ShortcutGroup;
  label: string;
  defaultBinding: string;
}

export const ACTIONS: readonly ShortcutAction[] = [
  { id: "pane.split-vertical-new",    group: "pane", label: "Split pane vertically",                defaultBinding: "Mod+KeyN" },
  { id: "pane.split-vertical-pick",   group: "pane", label: "Split pane vertically (pick target)",  defaultBinding: "Mod+Alt+KeyN" },
  { id: "pane.split-horizontal-new",  group: "pane", label: "Split pane horizontally",              defaultBinding: "Mod+Shift+KeyN" },
  { id: "pane.split-horizontal-pick", group: "pane", label: "Split pane horizontally (pick target)", defaultBinding: "Mod+Alt+Shift+KeyN" },
  { id: "pane.close",                 group: "pane", label: "Close pane",                            defaultBinding: "Mod+KeyW" },
  { id: "pane.focus-left",            group: "pane", label: "Focus pane left",                       defaultBinding: "Mod+Alt+ArrowLeft" },
  { id: "pane.focus-right",           group: "pane", label: "Focus pane right",                      defaultBinding: "Mod+Alt+ArrowRight" },
  { id: "pane.focus-up",              group: "pane", label: "Focus pane up",                         defaultBinding: "Mod+Alt+ArrowUp" },
  { id: "pane.focus-down",            group: "pane", label: "Focus pane down",                       defaultBinding: "Mod+Alt+ArrowDown" },
  { id: "tab.new",                    group: "tab",  label: "New tab",                               defaultBinding: "Mod+KeyT" },
  { id: "tab.prev",                   group: "tab",  label: "Previous tab",                          defaultBinding: "Mod+Shift+BracketLeft" },
  { id: "tab.next",                   group: "tab",  label: "Next tab",                              defaultBinding: "Mod+Shift+BracketRight" },
] as const;

export const ACTION_BY_ID: Record<string, ShortcutAction | undefined> = Object.fromEntries(
  ACTIONS.map((a) => [a.id, a]),
);

// Reverse map of defaults: binding -> actionId. Used by buildRoutingTable as
// the starting layer beneath user overrides.
export const DEFAULT_BINDINGS: Record<string, string> = Object.fromEntries(
  ACTIONS.map((a) => [a.defaultBinding, a.id]),
);
