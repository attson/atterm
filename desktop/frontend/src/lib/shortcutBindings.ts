// Pure, framework-agnostic helpers for terminal shortcut bindings.
//
// A binding string has the form "Mod+Alt+Shift+<code>" where:
//   - "Mod" is a platform-agnostic modifier (Meta on mac, Control elsewhere).
//   - Token order is fixed: Mod, Alt, Shift, code — only the modifiers
//     actually pressed appear.
//   - <code> is a KeyboardEvent.code from a known whitelist.
//   - The empty string means "disabled" — this action will not be routed.

import type { MessageKey } from "../i18n";

export type ShortcutGroup = "pane" | "tab" | "sidebar";

export interface ShortcutAction {
  id: string;
  group: ShortcutGroup;
  labelKey: MessageKey;
  defaultBinding: string;
}

export const ACTIONS: readonly ShortcutAction[] = [
  { id: "pane.split-vertical-new",    group: "pane", labelKey: "settings.shortcuts.splitPaneVertically",                defaultBinding: "Mod+KeyN" },
  { id: "pane.split-horizontal-new",  group: "pane", labelKey: "settings.shortcuts.splitPaneHorizontally",              defaultBinding: "Mod+Shift+KeyN" },
  { id: "pane.close",                 group: "pane", labelKey: "settings.shortcuts.closePane",                            defaultBinding: "Mod+KeyW" },
  { id: "pane.focus-left",            group: "pane", labelKey: "settings.shortcuts.focusPaneLeft",                       defaultBinding: "Mod+Alt+ArrowLeft" },
  { id: "pane.focus-right",           group: "pane", labelKey: "settings.shortcuts.focusPaneRight",                      defaultBinding: "Mod+Alt+ArrowRight" },
  { id: "pane.focus-up",              group: "pane", labelKey: "settings.shortcuts.focusPaneUp",                         defaultBinding: "Mod+Alt+ArrowUp" },
  { id: "pane.focus-down",            group: "pane", labelKey: "settings.shortcuts.focusPaneDown",                       defaultBinding: "Mod+Alt+ArrowDown" },
  { id: "terminal.search",            group: "pane", labelKey: "settings.shortcuts.terminalSearch",                      defaultBinding: "Mod+KeyF" },
  { id: "tab.new",                    group: "tab",  labelKey: "settings.shortcuts.newTab",                               defaultBinding: "Mod+KeyT" },
  { id: "tab.prev",                   group: "tab",  labelKey: "settings.shortcuts.previousTab",                          defaultBinding: "Mod+Shift+BracketLeft" },
  { id: "tab.next",                   group: "tab",  labelKey: "settings.shortcuts.nextTab",                              defaultBinding: "Mod+Shift+BracketRight" },
  { id: "toggleTaskSidebar",          group: "sidebar", labelKey: "tasks.sidebar.collapse",                               defaultBinding: "Mod+KeyB" },
  { id: "sidebar.focus-search",       group: "sidebar", labelKey: "settings.shortcuts.focusSidebarSearch",                defaultBinding: "Mod+Shift+KeyF" },
] as const;

export const ACTION_BY_ID: Record<string, ShortcutAction | undefined> = Object.fromEntries(
  ACTIONS.map((a) => [a.id, a]),
);

// Reverse map of defaults: binding -> actionId. Used by buildRoutingTable as
// the starting layer beneath user overrides.
export const DEFAULT_BINDINGS: Record<string, string> = Object.fromEntries(
  ACTIONS.map((a) => [a.defaultBinding, a.id]),
);

export type Mod = "Meta" | "Control";

const CODE_WHITELIST: ReadonlySet<string> = new Set([
  "KeyA","KeyB","KeyC","KeyD","KeyE","KeyF","KeyG","KeyH","KeyI","KeyJ","KeyK","KeyL","KeyM",
  "KeyN","KeyO","KeyP","KeyQ","KeyR","KeyS","KeyT","KeyU","KeyV","KeyW","KeyX","KeyY","KeyZ",
  "Digit0","Digit1","Digit2","Digit3","Digit4","Digit5","Digit6","Digit7","Digit8","Digit9",
  "ArrowLeft","ArrowRight","ArrowUp","ArrowDown",
  "BracketLeft","BracketRight",
  "Minus","Equal","Backquote","Comma","Period","Slash","Semicolon","Quote","Backslash",
]);

export interface ParsedBinding {
  mod: boolean;
  alt: boolean;
  shift: boolean;
  code: string | null;
}

// serialize converts a KeyboardEvent into a binding string. Returns null if
//   - the event's "wrong modifier" is pressed (Meta on Control platforms,
//     Control on Meta platforms),
//   - the code is not in the whitelist,
//   - no modifier is held (we never bind a bare key — would intercept typing).
export function serialize(e: KeyboardEvent, mod: Mod): string | null {
  const isMod = mod === "Meta" ? e.metaKey : e.ctrlKey;
  const wrongMod = mod === "Meta" ? e.ctrlKey : e.metaKey;
  if (wrongMod) return null;
  if (!CODE_WHITELIST.has(e.code)) return null;
  if (!isMod && !e.altKey && !e.shiftKey) return null;
  const parts: string[] = [];
  if (isMod) parts.push("Mod");
  if (e.altKey) parts.push("Alt");
  if (e.shiftKey) parts.push("Shift");
  parts.push(e.code);
  return parts.join("+");
}

// resolvedBindings merges the user overrides with the registry defaults and
// returns the resulting action -> binding map (containing all 13 actions).
export function resolvedBindings(
  overrides: Record<string, string>,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const a of ACTIONS) {
    out[a.id] = a.defaultBinding;
  }
  for (const [id, binding] of Object.entries(overrides)) {
    if (id in ACTION_BY_ID) out[id] = binding;
    // Unknown action IDs are silently dropped (forward/backward compat).
  }
  return out;
}

// conflictsWith inspects a fully-resolved bindings map and returns the IDs
// of other actions that share the same non-empty binding as `actionId`.
export function conflictsWith(
  bindings: Record<string, string>,
  actionId: string,
): string[] {
  const target = bindings[actionId];
  if (!target) return [];
  const result: string[] = [];
  for (const [id, b] of Object.entries(bindings)) {
    if (id === actionId) continue;
    if (b === target) result.push(id);
  }
  return result;
}

// buildRoutingTable produces a binding-string -> actionId map used by the
// runtime keydown router. It seeds with registry defaults, then applies user
// overrides: each override first clears the action's previous slot, then
// (if non-empty) installs the new binding. Unknown action IDs are dropped.
export function buildRoutingTable(
  overrides: Record<string, string>,
): Record<string, string> {
  const table: Record<string, string> = { ...DEFAULT_BINDINGS };
  for (const [id, binding] of Object.entries(overrides)) {
    if (!(id in ACTION_BY_ID)) continue;
    // Find and remove the previous slot for this action (whether default or
    // a prior override) — there is at most one because table values are
    // unique per round of insertions.
    for (const key of Object.keys(table)) {
      if (table[key] === id) {
        delete table[key];
        break;
      }
    }
    if (binding !== "") table[binding] = id;
  }
  return table;
}

// Maps KeyboardEvent.code values to display characters. Mirrors CODE_WHITELIST
// (above). Keep the two in sync — adding a code to the whitelist requires
// adding it here too.
const CODE_DISPLAY: Record<string, string> = {
  KeyA: "A", KeyB: "B", KeyC: "C", KeyD: "D", KeyE: "E", KeyF: "F", KeyG: "G",
  KeyH: "H", KeyI: "I", KeyJ: "J", KeyK: "K", KeyL: "L", KeyM: "M", KeyN: "N",
  KeyO: "O", KeyP: "P", KeyQ: "Q", KeyR: "R", KeyS: "S", KeyT: "T", KeyU: "U",
  KeyV: "V", KeyW: "W", KeyX: "X", KeyY: "Y", KeyZ: "Z",
  Digit0: "0", Digit1: "1", Digit2: "2", Digit3: "3", Digit4: "4",
  Digit5: "5", Digit6: "6", Digit7: "7", Digit8: "8", Digit9: "9",
  ArrowLeft: "←", ArrowRight: "→", ArrowUp: "↑", ArrowDown: "↓",
  BracketLeft: "[", BracketRight: "]",
  Minus: "-", Equal: "=", Backquote: "`",
  Comma: ",", Period: ".", Slash: "/",
  Semicolon: ";", Quote: "'", Backslash: "\\",
};

// formatChord renders a binding string for human display. On mac (mod === "Meta")
// modifiers use Unicode symbols concatenated with no separator (⌘⌥⇧N). On
// other platforms (mod === "Control") modifiers are written as words joined
// with "+" (Ctrl+Alt+Shift+N). Empty string maps to empty string (the caller
// can render a disabled-state placeholder). Malformed bindings pass through
// unchanged — formatChord is purely cosmetic and never throws.
export function formatChord(binding: string, mod: Mod): string {
  if (binding === "") return "";
  const parsed = parse(binding);
  if (parsed === null || parsed.code === null) return binding;
  const display = CODE_DISPLAY[parsed.code] ?? parsed.code;
  if (mod === "Meta") {
    let out = "";
    if (parsed.mod) out += "⌘";
    if (parsed.alt) out += "⌥";
    if (parsed.shift) out += "⇧";
    return out + display;
  }
  const parts: string[] = [];
  if (parsed.mod) parts.push("Ctrl");
  if (parsed.alt) parts.push("Alt");
  if (parsed.shift) parts.push("Shift");
  parts.push(display);
  return parts.join("+");
}

// parse converts a binding string into a structured ParsedBinding, or returns
// null for malformed input. The empty string is treated as the sentinel
// "disabled" binding and parses successfully with all flags false and code=null.
export function parse(s: string): ParsedBinding | null {
  if (s === "") return { mod: false, alt: false, shift: false, code: null };
  const tokens = s.split("+");
  if (tokens.length < 2) return null;
  const code = tokens[tokens.length - 1];
  if (!CODE_WHITELIST.has(code)) return null;
  const modifiers = tokens.slice(0, -1);
  // Enforce fixed token order: Mod, Alt, Shift.
  const expected = ["Mod", "Alt", "Shift"];
  let i = 0;
  const flags = { mod: false, alt: false, shift: false };
  for (const tok of modifiers) {
    while (i < expected.length && tok !== expected[i]) i++;
    if (i === expected.length) return null;
    if (tok === "Mod") flags.mod = true;
    else if (tok === "Alt") flags.alt = true;
    else if (tok === "Shift") flags.shift = true;
    i++;
  }
  if (!flags.mod && !flags.alt && !flags.shift) return null;
  return { ...flags, code };
}
