import { describe, expect, it } from "vitest";
import {
  TERMINAL_FONT_FAMILY,
  TERMINAL_FONT_PRESETS,
  composeFontFamily,
  resolveTerminalFontFamily,
  type TerminalFontRuntime,
} from "./terminalFont";

describe("composeFontFamily", () => {
  it("returns the built-in chain unchanged for the bundled default", () => {
    expect(composeFontFamily("")).toBe(TERMINAL_FONT_FAMILY);
    expect(composeFontFamily("   ")).toBe(TERMINAL_FONT_FAMILY);
  });

  it("prepends the chosen head to the built-in chain", () => {
    expect(composeFontFamily("JetBrains Mono")).toBe(
      `"JetBrains Mono", ${TERMINAL_FONT_FAMILY}`,
    );
  });

  it("does not double-quote a head that is already quoted", () => {
    expect(composeFontFamily('"Fira Code"')).toBe(`"Fira Code", ${TERMINAL_FONT_FAMILY}`);
  });

  it("keeps the CJK tail present for every preset — redline #13", () => {
    for (const p of TERMINAL_FONT_PRESETS) {
      const chain = composeFontFamily(p.id);
      expect(chain).toContain("PingFang SC");
      expect(chain).toContain("Microsoft YaHei");
      expect(chain).toContain("Noto Sans Mono CJK SC");
      expect(chain.endsWith("monospace")).toBe(true);
    }
  });

  it("keeps the selected head ahead of the bundled ASCII face", () => {
    const chain = composeFontFamily("JetBrains Mono");
    expect(chain.indexOf("JetBrains Mono")).toBeLessThan(chain.indexOf("AT Term Mono"));
    expect(chain.indexOf("AT Term Mono")).toBeLessThan(chain.indexOf("PingFang SC"));
  });

  it("offers a bundled-default preset with an empty id", () => {
    expect(TERMINAL_FONT_PRESETS.some((p) => p.id === "")).toBe(true);
  });
});

describe("resolveTerminalFontFamily", () => {
  function runtime(widths: Record<string, number>): TerminalFontRuntime {
    return {
      load: async () => undefined,
      measure: (font, text) => {
        const weight = font.startsWith("700") ? "bold" : "regular";
        const glyph = text.startsWith("i") ? "narrow" : "wide";
        return widths[`${weight}:${glyph}`] ?? null;
      },
    };
  }

  it("keeps an available monospace selection", async () => {
    const resolved = await resolveTerminalFontFamily("Fira Code", runtime({
      "regular:narrow": 80,
      "regular:wide": 80,
      "bold:narrow": 81,
      "bold:wide": 81,
    }));
    expect(resolved).toBe(composeFontFamily("Fira Code"));
  });

  it("falls back when fontconfig substitutes a proportional face", async () => {
    const resolved = await resolveTerminalFontFamily("Missing Mono", runtime({
      "regular:narrow": 45,
      "regular:wide": 125,
      "bold:narrow": 48,
      "bold:wide": 130,
    }));
    expect(resolved).toBe(TERMINAL_FONT_FAMILY);
  });

  it("falls back when the browser cannot measure the selected face", async () => {
    const resolved = await resolveTerminalFontFamily("Missing Mono", runtime({}));
    expect(resolved).toBe(TERMINAL_FONT_FAMILY);
  });

  it("falls back when a restricted canvas throws during measurement", async () => {
    const resolved = await resolveTerminalFontFamily("Missing Mono", {
      load: async () => undefined,
      measure: () => { throw new Error("canvas disabled"); },
    });
    expect(resolved).toBe(TERMINAL_FONT_FAMILY);
  });
});
