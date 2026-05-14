import { describe, expect, test } from "vitest";
import { stripC1Controls } from "./stripC1Controls";

describe("stripC1Controls", () => {
  test("passes ASCII through untouched", () => {
    const { cleaned, dropped } = stripC1Controls("ls -la\n");
    expect(cleaned).toBe("ls -la\n");
    expect(dropped).toEqual([]);
  });

  test("passes the Chinese IME commit result through untouched", () => {
    const { cleaned, dropped } = stripC1Controls("你好 world");
    expect(cleaned).toBe("你好 world");
    expect(dropped).toEqual([]);
  });

  test("keeps DEL (0x7F) and 0xA0+ since those are not C1 controls", () => {
    const { cleaned, dropped } = stripC1Controls("\x7F a");
    expect(cleaned).toBe("\x7F a");
    expect(dropped).toEqual([]);
  });

  test("drops the U+0080..U+009F range and reports each codepoint", () => {
    const { cleaned, dropped } = stripC1Controls("");
    expect(cleaned).toBe("");
    expect(dropped).toEqual([0x93, 0x89, 0x8d]);
  });

  test("drops every byte in the full C1 range", () => {
    let s = "";
    for (let cp = 0x80; cp <= 0x9f; cp++) s += String.fromCharCode(cp);
    const { cleaned, dropped } = stripC1Controls(s);
    expect(cleaned).toBe("");
    expect(dropped).toHaveLength(0x20);
  });

  test("interleaved C1 controls are removed while normal chars are kept", () => {
    const { cleaned, dropped } = stripC1Controls("abc");
    expect(cleaned).toBe("abc");
    expect(dropped).toEqual([0x93, 0x89]);
  });

  test("empty input returns empty result", () => {
    const { cleaned, dropped } = stripC1Controls("");
    expect(cleaned).toBe("");
    expect(dropped).toEqual([]);
  });
});
