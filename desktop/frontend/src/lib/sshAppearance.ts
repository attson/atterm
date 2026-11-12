import type { ITheme } from "xterm";
import type { TerminalAppearance } from "./types";
import type { TerminalAppearanceOverride } from "../platform/types";
import { TERMINAL_FONT_PRESETS } from "./terminalFont";
import { TERMINAL_THEMES, getTerminalTheme } from "./terminalThemes";

const validThemeIDs = new Set<string>(TERMINAL_THEMES.map((theme) => theme.id));
const validFontHeads = new Set(TERMINAL_FONT_PRESETS.map((font) => font.id));

export function normalizeSSHAppearance(
  input: TerminalAppearanceOverride | null | undefined,
): TerminalAppearanceOverride {
  if (!input) return {};
  const out: TerminalAppearanceOverride = {};
  if (input.theme_id && validThemeIDs.has(input.theme_id)) out.theme_id = input.theme_id;
  if (input.font_head !== undefined && validFontHeads.has(input.font_head)) out.font_head = input.font_head;
  if (Number.isInteger(input.font_size) && input.font_size! >= 8 && input.font_size! <= 32) {
    out.font_size = input.font_size;
  }
  return out;
}

export function resolveSSHAppearance(
  global: TerminalAppearance,
  input: TerminalAppearanceOverride | null | undefined,
): TerminalAppearance {
  const override = normalizeSSHAppearance(input);
  return {
    ...global,
    ...(override.font_head !== undefined ? { fontHead: override.font_head } : {}),
    ...(override.font_size !== undefined ? { fontSize: override.font_size } : {}),
  };
}

export function resolveSSHTheme(
  global: ITheme,
  input: TerminalAppearanceOverride | null | undefined,
): ITheme {
  const themeID = normalizeSSHAppearance(input).theme_id;
  return themeID ? getTerminalTheme(themeID).xtermTheme : global;
}
