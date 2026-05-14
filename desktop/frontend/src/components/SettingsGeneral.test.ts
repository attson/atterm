import { describe, expect, test } from "vitest";
import source from "./SettingsGeneral.vue?raw";

describe("SettingsGeneral", () => {
  test("declares terminalThemeId prop and terminal-theme-changed emit", () => {
    expect(source).toContain("terminalThemeId: string");
    expect(source).toContain('(e: "terminal-theme-changed", themeID: string): void');
  });

  test("uses terminal theme registry and saves changes via setTerminalThemePreference", () => {
    expect(source).toContain("TERMINAL_THEMES");
    expect(source).toContain("setTerminalThemePreference");
    expect(source).toMatch(/async\s+function\s+onChange\s*\(\s*\)/);
    expect(source).toContain('emit("terminal-theme-changed", nextTheme)');
  });

  test("renders a theme select bound to the local selected ref", () => {
    expect(source).toContain('v-model="selected"');
    expect(source).toContain('@change="onChange"');
    expect(source).toContain("terminal theme");
  });

  test("reverts to previous theme on save error and surfaces it", () => {
    expect(source).toContain('selected.value = previous');
    expect(source).toContain('emit("terminal-theme-changed", previous)');
    expect(source).toMatch(/error\.value\s*=/);
  });
});
