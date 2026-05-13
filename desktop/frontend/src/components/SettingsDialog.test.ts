import { describe, expect, test } from "vitest";
import source from "./SettingsDialog.vue?raw";

function styleBlockFor(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  return match?.[1] ?? "";
}

describe("SettingsDialog layout", () => {
  test("keeps the settings dialog inside the viewport and scrollable", () => {
    const dialogStyle = styleBlockFor(".dialog");

    expect(dialogStyle).toMatch(/max-height\s*:/);
    expect(dialogStyle).toMatch(/overflow-y\s*:\s*auto/);
  });
});

describe("SettingsDialog terminal theme selector", () => {
  test("declares theme props and change event", () => {
    expect(source).toContain("terminalThemeId: string");
    expect(source).toContain('(e: "terminal-theme-changed", themeID: string): void');
  });

  test("loads theme registry and saves theme changes immediately", () => {
    expect(source).toContain("TERMINAL_THEMES");
    expect(source).toContain("setTerminalThemePreference");
    expect(source).toContain("async function onTerminalThemeChange");
    expect(source).toContain('emit("terminal-theme-changed", nextTheme)');
  });

  test("renders a terminal theme select independent of relay save", () => {
    expect(source).toContain("terminal theme");
    expect(source).toContain('v-model="selectedTerminalTheme"');
    expect(source).toContain('@change="onTerminalThemeChange"');
  });
});
