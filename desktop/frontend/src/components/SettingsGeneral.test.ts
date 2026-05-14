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

  test("renders a SelectDropdown bound to the local selected ref", () => {
    expect(source).toContain("import SelectDropdown");
    expect(source).toContain("<SelectDropdown");
    expect(source).toContain('v-model="selected"');
    expect(source).toContain('@update:modelValue="onChange"');
    expect(source).toContain("terminal theme");
  });

  test("reverts to previous theme on save error and surfaces it", () => {
    expect(source).toContain('selected.value = previous');
    expect(source).toContain('emit("terminal-theme-changed", previous)');
    expect(source).toMatch(/error\.value\s*=/);
  });
});

describe("SettingsGeneral notification toggle", () => {
  test("imports notification getter and setter", () => {
    expect(source).toContain("getNotificationsEnabled");
    expect(source).toContain("setNotificationsEnabled");
  });

  test("renders the checkbox with label and focus-only hint", () => {
    expect(source).toContain("Show system notifications on terminal bell");
    expect(source).toContain("Only fires when the AT Term window is not focused.");
  });

  test("wires checkbox change handler to setNotificationsEnabled", () => {
    expect(source).toMatch(/onNotificationsToggle/);
    expect(source).toContain('@change="onNotificationsToggle"');
  });
});
