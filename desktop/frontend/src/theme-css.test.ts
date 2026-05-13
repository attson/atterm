import { describe, expect, test } from "vitest";
import { readFileSync } from "node:fs";
import paneSource from "./components/PaneGrid.vue?raw";

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
