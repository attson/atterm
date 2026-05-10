// Document-level capture-phase keydown router. Listens before xterm.js so
// Ctrl/Cmd combos we care about never reach the terminal. See spec §"Shortcuts".

import { onScopeDispose } from "vue";
import type { FocusDir } from "../lib/types";

export type SplitMode = "new" | "pick";

export interface ShortcutHandlers {
  onSplitVertical: (mode: SplitMode) => void;
  onSplitHorizontal: (mode: SplitMode) => void;
  onClosePane: () => void;
  onFocusPane: (dir: FocusDir) => void;
  onNewTab: () => void;
  onSwitchTab: (delta: number) => void;
}

export interface ShortcutOptions {
  // Override the modifier-key detection. Default: "Meta" on Mac, "Control"
  // elsewhere. Tests use this to force "Control" for portability.
  mod?: "Meta" | "Control";
}

function detectMod(): "Meta" | "Control" {
  if (typeof navigator === "undefined") return "Control";
  return navigator.platform?.toLowerCase().includes("mac") ? "Meta" : "Control";
}

const ARROW_TO_DIR: Record<string, FocusDir> = {
  ArrowLeft: "left",
  ArrowRight: "right",
  ArrowUp: "up",
  ArrowDown: "down",
};

export function useTerminalShortcuts(
  h: ShortcutHandlers,
  opts: ShortcutOptions = {},
): void {
  const mod = opts.mod ?? detectMod();
  const isMod = (e: KeyboardEvent) => (mod === "Meta" ? e.metaKey : e.ctrlKey);
  // Guard against the wrong modifier accidentally double-binding (Cmd on
  // Linux/Win or Ctrl on Mac) — we only want exactly the platform's modifier.
  const wrongMod = (e: KeyboardEvent) => (mod === "Meta" ? e.ctrlKey : e.metaKey);

  function handler(e: KeyboardEvent) {
    if (!isMod(e) || wrongMod(e)) return;
    // Letter / bracket keys: match e.code (physical key, layout-independent).
    // e.key is unreliable here on macOS — when Option is held it produces
    // dead-key characters (⌥D → "∂"), and Shift turns "[" into "{". e.code
    // stays "KeyD" / "BracketLeft" regardless.
    const code = e.code;

    if (code === "KeyN") {
      // Split shortcut. ⌥⌘D was the original choice but macOS has hardcoded
      // ⌥⌘D = "Toggle Dock auto-hide" at the OS level — WKWebView never sees
      // the keydown. N (for "new pane") is unclaimed by any menu we register.
      e.preventDefault();
      e.stopPropagation();
      const mode: SplitMode = e.altKey ? "pick" : "new";
      if (e.shiftKey) h.onSplitHorizontal(mode);
      else h.onSplitVertical(mode);
      return;
    }
    if (code === "KeyW" && !e.altKey) {
      e.preventDefault();
      e.stopPropagation();
      h.onClosePane();
      return;
    }
    if (code === "KeyT" && !e.altKey && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      h.onNewTab();
      return;
    }
    if (e.altKey && ARROW_TO_DIR[e.key]) {
      e.preventDefault();
      e.stopPropagation();
      h.onFocusPane(ARROW_TO_DIR[e.key]);
      return;
    }
    if (e.shiftKey && (code === "BracketLeft" || code === "BracketRight")) {
      e.preventDefault();
      e.stopPropagation();
      h.onSwitchTab(code === "BracketRight" ? 1 : -1);
      return;
    }
  }

  document.addEventListener("keydown", handler, { capture: true });
  onScopeDispose(() => {
    document.removeEventListener("keydown", handler, { capture: true } as EventListenerOptions);
  });
}
