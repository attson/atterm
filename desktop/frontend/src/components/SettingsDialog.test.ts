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
