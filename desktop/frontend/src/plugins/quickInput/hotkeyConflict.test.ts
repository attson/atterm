import { describe, expect, it } from "vitest";
import { parseHotkey, conflictsWith, BUILT_IN_RESERVED } from "./hotkeyConflict";

describe("parseHotkey", () => {
  it("parses Alt+1", () => {
    expect(parseHotkey("Alt+1")).toEqual({ alt: true, shift: false, key: "1" });
  });
  it("parses Alt+Shift+P", () => {
    expect(parseHotkey("Alt+Shift+P")).toEqual({ alt: true, shift: true, key: "P" });
  });
  it("rejects empty", () => {
    expect(parseHotkey("")).toBeNull();
  });
  it("rejects modifier-only", () => {
    expect(parseHotkey("Alt+")).toBeNull();
  });
  it("rejects without Alt", () => {
    expect(parseHotkey("Ctrl+1")).toBeNull();
  });
});

describe("conflictsWith", () => {
  it("flags duplicates within button list", () => {
    const buttons = [
      { id: "a", label: "x", send: "x", appendNewline: true, hotkey: "Alt+1" },
      { id: "b", label: "y", send: "y", appendNewline: true, hotkey: "Alt+1" },
    ];
    expect(conflictsWith(buttons, "Alt+1", "c")).toBe(true);
  });
  it("does not flag the same button entry as conflicting with itself", () => {
    const buttons = [
      { id: "a", label: "x", send: "x", appendNewline: true, hotkey: "Alt+1" },
    ];
    expect(conflictsWith(buttons, "Alt+1", "a")).toBe(false);
  });
  it("flags built-in reserved combos", () => {
    expect(BUILT_IN_RESERVED.has("Alt+ArrowLeft")).toBe(true);
  });
});
