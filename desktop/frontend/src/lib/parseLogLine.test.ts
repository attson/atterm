import { describe, it, test, expect } from "vitest";
import { TAG_FILTER_ALL, levelAtLeast, logTagOptions, parseLogLine, tagMatches } from "./parseLogLine";

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

describe("tag filtering", () => {
  const SAMPLE = [
    "2026/08/08 15:04:05.100 INFO  [uplink] connected",
    "2026/08/08 15:04:05.101 DEBUG [uplink-stream] stream_out_first",
    "2026/08/08 15:04:05.102 WARN  [feishu] askform submit no-anchor",
    "2026/08/08 15:04:05.103 WARN  [feishu-anchor] patch gave up",
    "2026/08/08 15:04:05.104 ERROR [ui-boot] step failed",
    "  at someFunction (file.ts:1)",
  ].join("\n");

  test("tagMatches: empty filter keeps everything", () => {
    expect(tagMatches("uplink", TAG_FILTER_ALL)).toBe(true);
    expect(tagMatches("anything", "")).toBe(true);
  });

  test("tagMatches: exact tag does not leak into its family", () => {
    expect(tagMatches("feishu", "feishu")).toBe(true);
    expect(tagMatches("feishu-anchor", "feishu")).toBe(false);
  });

  test("tagMatches: family covers the bare prefix and its dashed children", () => {
    expect(tagMatches("feishu", "feishu*")).toBe(true);
    expect(tagMatches("feishu-anchor", "feishu*")).toBe(true);
    expect(tagMatches("feishuish", "feishu*")).toBe(false);
    expect(tagMatches("uplink", "feishu*")).toBe(false);
  });

  test("logTagOptions derives choices from the content, with counts", () => {
    const opts = logTagOptions(SAMPLE, "All");
    // The unparseable stack line contributes no tag and is not counted.
    expect(opts[0]).toEqual({ value: "", label: "All (5)" });

    const values = opts.map((o) => o.value);
    expect(values).toContain("uplink");
    expect(values).toContain("feishu-anchor");
    expect(values).toContain("ui-boot");
    // Families appear only where 2+ tags share a prefix.
    expect(values).toContain("feishu*");
    expect(values).toContain("uplink*");
    expect(values).not.toContain("ui*");
  });

  test("logTagOptions family count sums its members", () => {
    const opts = logTagOptions(SAMPLE, "All");
    const family = opts.find((o) => o.value === "feishu*");
    expect(family?.label).toBe("feishu* (2)");
  });

  test("logTagOptions on empty content offers only the all-entry", () => {
    expect(logTagOptions("", "All")).toEqual([{ value: "", label: "All (0)" }]);
  });
});
