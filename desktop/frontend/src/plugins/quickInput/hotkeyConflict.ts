import type { QuickInputButton } from "../configStore";

export interface ParsedHotkey {
  alt: boolean;
  shift: boolean;
  key: string;
}

// Existing useTerminalShortcuts uses Alt+ArrowLeft/Right/Up/Down for pane
// focus. Any future built-in must be added here.
export const BUILT_IN_RESERVED = new Set<string>([
  "Alt+ArrowLeft",
  "Alt+ArrowRight",
  "Alt+ArrowUp",
  "Alt+ArrowDown",
]);

export function parseHotkey(s: string): ParsedHotkey | null {
  if (!s) return null;
  const parts = s.split("+").map((p) => p.trim());
  if (parts.length < 2) return null;
  let alt = false;
  let shift = false;
  for (let i = 0; i < parts.length - 1; i++) {
    const mod = parts[i];
    if (mod === "Alt") alt = true;
    else if (mod === "Shift") shift = true;
    else return null;
  }
  if (!alt) return null;
  const key = parts[parts.length - 1];
  if (!key) return null;
  if (/^[A-Za-z0-9]$/.test(key)) return { alt, shift, key: key.toUpperCase() };
  if (/^Arrow(Left|Right|Up|Down)$/.test(key)) return { alt, shift, key };
  return null;
}

export function normalizeHotkey(s: string): string | null {
  const p = parseHotkey(s);
  if (!p) return null;
  const parts: string[] = [];
  if (p.alt) parts.push("Alt");
  if (p.shift) parts.push("Shift");
  parts.push(p.key);
  return parts.join("+");
}

export function conflictsWith(
  buttons: QuickInputButton[],
  hotkey: string,
  selfID: string,
): boolean {
  if (!hotkey) return false;
  const n = normalizeHotkey(hotkey);
  if (!n) return false;
  if (BUILT_IN_RESERVED.has(n)) return true;
  return buttons.some(
    (b) => b.id !== selfID && b.hotkey && normalizeHotkey(b.hotkey) === n,
  );
}
