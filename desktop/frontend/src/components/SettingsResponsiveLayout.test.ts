import { describe, expect, it } from "vitest";
import profilesSource from "./SettingsProfiles.vue?raw";
import receivedFilesSource from "./SettingsReceivedFiles.vue?raw";
import shortcutsSource from "./SettingsShortcuts.vue?raw";
import templatesSource from "./SettingsTemplates.vue?raw";

describe("settings narrow layout", () => {
  it("stacks template row content and actions inside the pane", () => {
    expect(templatesSource).toContain("@media (max-width: 640px)");
    expect(templatesSource).toMatch(/\.row\s*\{[^}]*grid-template-columns:\s*minmax\(0, 1fr\) auto/s);
    expect(templatesSource).toMatch(/\.actions\s*\{[^}]*grid-column:\s*1 \/ -1/s);
  });

  it("keeps profile details and actions inside the pane", () => {
    expect(profilesSource).toContain("@media (max-width: 640px)");
    expect(profilesSource).toMatch(/\.row\s*\{[^}]*grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\)/s);
    expect(profilesSource).toMatch(/\.actions\s*\{[^}]*grid-column:\s*1 \/ -1/s);
  });

  it("allows shortcut capture cells to shrink on narrow screens", () => {
    expect(shortcutsSource).toContain("@media (max-width: 640px)");
    expect(shortcutsSource).toMatch(/:deep\(\.hotkey-cell\)\s*\{[^}]*min-width:\s*0/s);
  });

  it("wraps received-file toolbars and rows on narrow screens", () => {
    expect(receivedFilesSource).toContain("@media (max-width: 640px)");
    expect(receivedFilesSource).toMatch(/\.header\s*\{[^}]*flex-wrap:\s*wrap/s);
    expect(receivedFilesSource).toMatch(/\.session-row,\s*\.files li\s*\{[^}]*flex-wrap:\s*wrap/s);
  });
});
