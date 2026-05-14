import { describe, expect, test } from "vitest";
import { extractSessionLabel, shouldNotify } from "./terminalBell";

describe("shouldNotify", () => {
  test("returns false when window is focused", () => {
    expect(shouldNotify(1_000_000, 0, true)).toBe(false);
  });

  test("returns true on first bell with window unfocused", () => {
    expect(shouldNotify(1_000_000, 0, false)).toBe(true);
  });

  test("returns false if last bell was within the throttle window", () => {
    expect(shouldNotify(1_000_000, 998_000, false)).toBe(false);
  });

  test("returns true after the throttle window elapses", () => {
    expect(shouldNotify(1_000_000 + 3_000, 1_000_000, false)).toBe(true);
  });

  test("honors a custom throttleMs argument", () => {
    expect(shouldNotify(1_500, 1_000, false, 1_000)).toBe(false);
    expect(shouldNotify(2_001, 1_000, false, 1_000)).toBe(true);
  });
});

describe("extractSessionLabel", () => {
  test("prefers the cwd basename", () => {
    const info = {
      id: "s",
      command: "/bin/zsh",
      cwd: "/Users/me/code/atterm",
      title: "",
      cols: 80,
      rows: 24,
      started_at: 0,
    };
    expect(extractSessionLabel(info)).toBe("atterm");
  });

  test("falls back to the command basename when cwd is empty", () => {
    const info = {
      id: "s",
      command: "/usr/local/bin/fish",
      cwd: "",
      title: "",
      cols: 80,
      rows: 24,
      started_at: 0,
    };
    expect(extractSessionLabel(info)).toBe("fish");
  });

  test("returns empty string when both cwd and command are empty", () => {
    const info = {
      id: "s",
      command: "",
      cwd: "",
      title: "",
      cols: 80,
      rows: 24,
      started_at: 0,
    };
    expect(extractSessionLabel(info)).toBe("");
  });

  test("returns empty string when info is null", () => {
    expect(extractSessionLabel(null)).toBe("");
  });
});
