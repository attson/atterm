import { describe, expect, it } from "vitest";
import {
  TERMINAL_FONT_FAMILY,
  TERMINAL_FONT_PRESETS,
  composeFontFamily,
} from "./terminalFont";

describe("composeFontFamily", () => {
  it("returns the built-in chain unchanged for the system default", () => {
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

  it("never lets the head displace the ASCII-mono families", () => {
    const chain = composeFontFamily("JetBrains Mono");
    expect(chain.indexOf("JetBrains Mono")).toBeLessThan(chain.indexOf("Menlo"));
    expect(chain.indexOf("Menlo")).toBeLessThan(chain.indexOf("PingFang SC"));
  });

  it("offers a system-default preset with an empty id", () => {
    expect(TERMINAL_FONT_PRESETS.some((p) => p.id === "")).toBe(true);
  });
});
