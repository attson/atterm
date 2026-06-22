import { describe, it, expect } from "vitest";
import { parseLogLine, levelAtLeast } from "./parseLogLine";

describe("parseLogLine", () => {
  it("parses a structured line", () => {
    const r = parseLogLine("2026/06/22 15:04:05.123 DEBUG [pty-input] write n=1 hex=1b LONE-ESC");
    expect(r).toEqual({
      kind: "structured",
      ts: "2026/06/22 15:04:05.123",
      level: "DEBUG",
      tag: "pty-input",
      msg: "write n=1 hex=1b LONE-ESC",
    });
  });

  it("parses padded INFO with extra spaces", () => {
    const r = parseLogLine("2026/06/22 15:04:05.123 INFO  [app] hello");
    expect(r.kind).toBe("structured");
    if (r.kind === "structured") {
      expect(r.level).toBe("INFO");
      expect(r.tag).toBe("app");
      expect(r.msg).toBe("hello");
    }
  });

  it("falls back to raw for non-matching lines", () => {
    const r = parseLogLine("    at someStackFrame (file.go:10)");
    expect(r).toEqual({ kind: "raw", text: "    at someStackFrame (file.go:10)" });
  });
});

describe("levelAtLeast", () => {
  it("compares severity", () => {
    expect(levelAtLeast("WARN", "INFO")).toBe(true);
    expect(levelAtLeast("DEBUG", "INFO")).toBe(false);
    expect(levelAtLeast("ERROR", "ERROR")).toBe(true);
  });
});
