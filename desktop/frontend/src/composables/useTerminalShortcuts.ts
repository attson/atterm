// Document-level capture-phase keydown router. Listens before xterm.js so
// Mod-combos we care about never reach the terminal.
//
// The composable accepts an optional `bindings` ref — a sparse map of
// actionId -> binding string (see lib/shortcutBindings.ts). Defaults from
// the action registry apply when the ref is omitted or an action is absent.

import { computed, onScopeDispose, type Ref } from "vue";
import type { FocusDir } from "../lib/types";
import { modKey } from "../lib/modKey";
import { buildRoutingTable, serialize, type Mod } from "../lib/shortcutBindings";


export interface ShortcutHandlers {
  onSplitVertical: () => void;
  onSplitHorizontal: () => void;
  onClosePane: () => void;
  onFocusPane: (dir: FocusDir) => void;
  onNewTab: () => void;
  onSwitchTab: (delta: number) => void;
  onToggleTaskSidebar?: () => void;
  onFocusSidebarSearch?: () => void;
  onToggleWindowMaximize?: () => void;
  onTerminalSearch?: () => void;
  /** Returns true only when a focused terminal opened local prompt history. */
  onPromptHistory?: () => boolean;
  /** Decreases the terminal font size, when the app can persist appearance. */
  onDecreaseFontSize?: () => void;
  /** Selects a tab by its one-based visible position. */
  onSwitchTabTo?: (index: number) => void;
}

export interface ShortcutOptions {
  // Override the modifier-key detection. Default: "Meta" on Mac, "Control"
  // elsewhere. Tests use this to force "Control" for portability.
  mod?: Mod;
  // Optional reactive bindings. Defaults are used for any unset action.
  bindings?: Ref<Record<string, string>>;
}

function detectMod(): Mod {
  return modKey();
}

function dispatch(actionId: string, h: ShortcutHandlers): boolean {
  switch (actionId) {
    case "pane.split-vertical-new":    h.onSplitVertical(); return true;
    case "pane.split-horizontal-new":  h.onSplitHorizontal(); return true;
    case "pane.close":                 h.onClosePane(); return true;
    case "pane.focus-left":            h.onFocusPane("left"); return true;
    case "pane.focus-right":           h.onFocusPane("right"); return true;
    case "pane.focus-up":              h.onFocusPane("up"); return true;
    case "pane.focus-down":            h.onFocusPane("down"); return true;
    case "tab.new":                    h.onNewTab(); return true;
    case "tab.prev":                   h.onSwitchTab(-1); return true;
    case "tab.next":                   h.onSwitchTab(1); return true;
    case "toggleTaskSidebar":          h.onToggleTaskSidebar?.(); return true;
    case "sidebar.focus-search":       h.onFocusSidebarSearch?.(); return true;
    case "application.toggle-maximize":
      if (!h.onToggleWindowMaximize) return false;
      h.onToggleWindowMaximize();
      return true;
    case "terminal.search":            h.onTerminalSearch?.(); return true;
    case "terminal.prompt-history":    return h.onPromptHistory?.() === true;
    case "terminal.font-size-decrease":
      if (!h.onDecreaseFontSize) return false;
      h.onDecreaseFontSize();
      return true;
  }
  return false;
}

export function useTerminalShortcuts(
  handlers: ShortcutHandlers,
  opts: ShortcutOptions = {},
): void {
  const mod = opts.mod ?? detectMod();

  const route = computed(() => {
    const overrides = opts.bindings?.value ?? {};
    return buildRoutingTable(overrides);
  });

  function handler(e: KeyboardEvent) {
    const key = serialize(e, mod);
    if (key === null) return;
    const actionId = route.value[key];
    if (actionId) {
      if (!dispatch(actionId, handlers)) return;
      e.preventDefault();
      e.stopPropagation();
      return;
    }

    // Keep Mod+1..9 as a compact built-in fallback instead of adding nine
    // rows to Settings. Routing-table bindings win, so users can still assign
    // any of these chords to a configurable action.
    const tabMatch = key.match(/^Mod\+Digit([1-9])$/);
    if (!tabMatch || !handlers.onSwitchTabTo) return;
    handlers.onSwitchTabTo(Number(tabMatch[1]));
    e.preventDefault();
    e.stopPropagation();
  }

  document.addEventListener("keydown", handler, { capture: true });
  onScopeDispose(() => {
    document.removeEventListener("keydown", handler, { capture: true } as EventListenerOptions);
  });
}
