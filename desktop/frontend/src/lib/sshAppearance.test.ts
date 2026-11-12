import { describe, expect, test } from "vitest";
import type { TerminalAppearance } from "./types";
import { normalizeSSHAppearance, resolveSSHAppearance, resolveSSHTheme } from "./sshAppearance";

const globalAppearance: TerminalAppearance = {
  fontHead: "JetBrains Mono",
  fontSize: 13,
  lineHeight: 1,
  cursorStyle: "block",
  cursorBlink: true,
  scrollback: 5000,
};

describe("saved SSH terminal appearance", () => {
  test("merges independent host overrides over current global values", () => {
    expect(resolveSSHAppearance(globalAppearance, { font_head: "Source Code Pro", font_size: 14 }))
      .toEqual({ ...globalAppearance, fontHead: "Source Code Pro", fontSize: 14 });
    expect(resolveSSHTheme({ background: "#000" }, { theme_id: "nord" }).background).toBe("#2e3440");
  });

  test("keeps explicit built-in font distinct from inherit", () => {
    expect(resolveSSHAppearance(globalAppearance, { font_head: "" }).fontHead).toBe("");
    expect(resolveSSHAppearance(globalAppearance, {}).fontHead).toBe("JetBrains Mono");
  });

  test("drops invalid members without poisoning valid overrides", () => {
    expect(normalizeSSHAppearance({ theme_id: "future-theme", font_head: "Comic Sans", font_size: 99 }))
      .toEqual({});
    expect(normalizeSSHAppearance({ theme_id: "nord", font_head: "Source Code Pro", font_size: 14 }))
      .toEqual({ theme_id: "nord", font_head: "Source Code Pro", font_size: 14 });
  });
});
