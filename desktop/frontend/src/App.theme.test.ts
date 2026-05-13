import { describe, expect, test } from "vitest";
import appSource from "./App.vue?raw";
import apiSource from "./lib/api.ts?raw";
import paneSource from "./components/PaneGrid.vue?raw";

describe("App terminal theme wiring", () => {
  test("api exposes terminal theme preference bindings", () => {
    expect(apiSource).toContain("GetTerminalTheme(): Promise<string>");
    expect(apiSource).toContain("SetTerminalTheme(themeID: string): Promise<void>");
    expect(apiSource).toContain("export function getTerminalThemePreference()");
    expect(apiSource).toContain("export function setTerminalThemePreference(themeID: string)");
  });

  test("App loads theme preference and applies app variables", () => {
    expect(appSource).toContain("getTerminalThemePreference");
    expect(appSource).toContain("currentTerminalThemeID");
    expect(appSource).toContain("currentTerminalTheme");
    expect(appSource).toContain("themeStyle");
    expect(appSource).toContain(":style=\"themeStyle\"");
  });

  test("App passes the xterm theme to PaneGrid and PaneGrid forwards it", () => {
    expect(appSource).toContain(":terminal-theme=\"currentTerminalTheme.xtermTheme\"");
    expect(paneSource).toContain("terminalTheme: TerminalThemeDefinition[\"xtermTheme\"]");
    expect(paneSource).toContain(":theme=\"terminalTheme\"");
  });
});
