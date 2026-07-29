import { describe, expect, test } from "vitest";
import { readFileSync } from "node:fs";
import paneSource from "./components/PaneGrid.vue?raw";
import appSource from "./App.vue?raw";
import settingsDialogSource from "./components/SettingsDialog.vue?raw";

const styleSource = readFileSync("src/style.css", "utf8");

describe("theme css variables", () => {
  test("root defines terminal fallback variables", () => {
    expect(styleSource).toContain("--terminal-bg: #000000");
    expect(styleSource).toContain("--terminal-grid: #11161d");
    expect(styleSource).toContain("--terminal-overlay: rgba(13, 17, 23, 0.85)");
  });

  test("pane grid and cells use theme terminal backgrounds", () => {
    expect(paneSource).toContain("background: var(--terminal-grid)");
    expect(paneSource).toContain("background: var(--terminal-bg)");
  });
});

describe("narrow viewport shell styles", () => {
  test("shared App reserves mobile safe areas on narrow viewports", () => {
    expect(appSource).toContain("@media (max-width: 767px)");
    expect(appSource).toContain("height: 100dvh");
    expect(appSource).toContain("padding-top: env(safe-area-inset-top)");
    expect(appSource).toContain("padding-bottom: env(safe-area-inset-bottom)");
  });

  test("settings dialog switches to a narrow-screen layout", () => {
    expect(settingsDialogSource).toContain("@media (max-width: 640px)");
    expect(settingsDialogSource).toContain("max-height: 100dvh");
    expect(settingsDialogSource).toContain("overflow-x: auto");
  });
});
