import { describe, expect, test } from "vitest";
import {
  DEFAULT_TERMINAL_THEME_ID,
  TERMINAL_THEMES,
  getTerminalTheme,
  isTerminalThemeID,
  type TerminalThemeID,
} from "./terminalThemes";

describe("terminal themes", () => {
  test("registers the built-in themes in settings order", () => {
    expect(TERMINAL_THEMES.map((theme) => theme.id)).toEqual([
      "classic",
      "nord",
      "solarized-dark",
      "daylight",
    ]);
  });

  test("falls back to classic for unknown ids", () => {
    expect(DEFAULT_TERMINAL_THEME_ID).toBe("classic");
    expect(getTerminalTheme("gruvbox").id).toBe("classic");
    expect(getTerminalTheme("").id).toBe("classic");
  });

  test("identifies valid theme ids", () => {
    expect(isTerminalThemeID("nord")).toBe(true);
    expect(isTerminalThemeID("nord ")).toBe(false);
    expect(isTerminalThemeID("unknown")).toBe(false);

    const typed: TerminalThemeID = getTerminalTheme("solarized-dark").id;
    expect(typed).toBe("solarized-dark");
  });

  test("themes include xterm colors and app variables", () => {
    for (const theme of TERMINAL_THEMES) {
      expect(theme.xtermTheme.background).toMatch(/^#/);
      expect(theme.xtermTheme.foreground).toMatch(/^#/);
      expect(theme.xtermTheme.cursor).toMatch(/^#/);
      expect(theme.appVars["--bg"]).toMatch(/^#/);
      expect(theme.appVars["--panel"]).toMatch(/^#/);
      expect(theme.appVars["--terminal-bg"]).toMatch(/^#/);
    }
  });
});
