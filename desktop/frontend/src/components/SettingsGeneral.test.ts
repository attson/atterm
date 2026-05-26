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
    expect(source).toContain("settings.general.terminalTheme");
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
    expect(source).toContain("settings.general.notificationsBell");
    expect(source).toContain("settings.general.notificationsHint");
  });

  test("wires checkbox change handler to setNotificationsEnabled", () => {
    expect(source).toMatch(/onNotificationsToggle/);
    expect(source).toContain('@change="onNotificationsToggle"');
  });

  test("renders shell integration toggle wired to setShellIntegrationEnabled", () => {
    expect(source).toContain("settings.general.shellIntegration");
    expect(source).toMatch(/setShellIntegrationEnabled\(/);
  });

  test("renders command-notify threshold number input wired to setCommandNotifyThresholdSeconds", () => {
    expect(source).toContain("settings.general.commandNotifyThreshold");
    expect(source).toMatch(/setCommandNotifyThresholdSeconds\(/);
    expect(source).toContain('min="1"');
    expect(source).toContain('max="600"');
  });

  test("loads shell integration and threshold on mount", () => {
    expect(source).toMatch(/getShellIntegrationEnabled\(\)/);
    expect(source).toMatch(/getCommandNotifyThresholdSeconds\(\)/);
  });

  test("emits command-notify-threshold-changed when threshold saves", () => {
    expect(source).toContain('"command-notify-threshold-changed"');
  });
});

describe("SettingsGeneral default shell preference", () => {
  test("imports default shell bindings and listShells", () => {
    expect(source).toContain("getDefaultShell");
    expect(source).toContain("setDefaultShell");
    expect(source).toContain("listShells");
  });

  test("renders automatic and custom shell controls", () => {
    expect(source).toContain("settings.general.defaultShell");
    expect(source).toContain('value="auto"');
    expect(source).toContain("settings.general.customShellPath");
    expect(source).toMatch(/onDefaultShellChange/);
    expect(source).toMatch(/onCustomShellSave/);
  });
});

describe("SettingsGeneral WebGL renderer toggle", () => {
  test("imports WebGL getter and setter", () => {
    expect(source).toContain("getWebglRendererEnabled");
    expect(source).toContain("setWebglRendererEnabled");
  });

  test("renders the WebGL renderer toggle wired to setWebglRendererEnabled", () => {
    expect(source).toContain("settings.general.webglRenderer");
    expect(source).toMatch(/onWebglRendererToggle/);
    expect(source).toContain('@change="onWebglRendererToggle"');
  });

  test("hint mentions the typing-lag trade-off so Linux users can find it", () => {
    expect(source).toContain("settings.general.webglHint");
  });

  test("loads WebGL preference on mount", () => {
    expect(source).toMatch(/getWebglRendererEnabled\(\)/);
  });
});

describe("SettingsGeneral language preference", () => {
  test("imports i18n and locale preference bindings", () => {
    expect(source).toContain("useI18n");
    expect(source).toContain("getLocalePreference");
    expect(source).toContain("setRuntimeLocalePreference");
    expect(source).not.toContain("setLocalePreference as saveLocalePreference");
  });

  test("renders language selector with translated label and hint", () => {
    expect(source).toContain("settings.general.languageLabel");
    expect(source).toContain("settings.general.languageHint");
    expect(source).toContain("languageOptions");
  });
});
