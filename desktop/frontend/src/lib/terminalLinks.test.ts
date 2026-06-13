import { describe, expect, it } from "vitest";
import { detectLinks } from "./terminalLinks";

describe("detectLinks — URL schemes", () => {
  it("matches a bare https URL", () => {
    expect(detectLinks("see https://example.com today")).toEqual([
      { start: 4, end: 23, text: "https://example.com", kind: "http" },
    ]);
  });

  it("matches http URL", () => {
    expect(detectLinks("http://x.test")).toEqual([
      { start: 0, end: 13, text: "http://x.test", kind: "http" },
    ]);
  });

  it("matches file:// URI", () => {
    expect(detectLinks("open file:///tmp/x.log please")).toEqual([
      { start: 5, end: 22, text: "file:///tmp/x.log", kind: "file" },
    ]);
  });

  it("matches multiple URLs on one line", () => {
    const line = "a https://a.test and https://b.test end";
    const got = detectLinks(line);
    expect(got.map((m) => m.text)).toEqual(["https://a.test", "https://b.test"]);
  });

  it("preserves URL query + fragment", () => {
    const line = "go https://x.test/p?a=1&b=2#z!";
    expect(detectLinks(line)).toEqual([
      { start: 3, end: 29, text: "https://x.test/p?a=1&b=2#z", kind: "http" },
    ]);
  });

  it("returns empty for non-URL line", () => {
    expect(detectLinks("nothing here")).toEqual([]);
  });

  it("returns empty for empty / nullish input", () => {
    expect(detectLinks("")).toEqual([]);
    expect(detectLinks(null as unknown as string)).toEqual([]);
    expect(detectLinks(undefined as unknown as string)).toEqual([]);
  });
});
