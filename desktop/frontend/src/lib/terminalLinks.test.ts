import { describe, expect, it } from "vitest";
import {
  detectLinks,
  isModClickEvent,
  normalizeForOpen,
  type LinkMatch,
} from "./terminalLinks";

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

describe("detectLinks — trailing punctuation", () => {
  it("strips trailing period", () => {
    expect(detectLinks("Visit https://x.test.")).toEqual([
      { start: 6, end: 20, text: "https://x.test", kind: "http" },
    ]);
  });

  it("strips trailing comma and semicolon", () => {
    expect(detectLinks("a https://x.test, then b")).toEqual([
      { start: 2, end: 16, text: "https://x.test", kind: "http" },
    ]);
    expect(detectLinks("https://x.test;")).toEqual([
      { start: 0, end: 14, text: "https://x.test", kind: "http" },
    ]);
  });

  it("strips closing paren when URL is inside (parens)", () => {
    expect(detectLinks("(see https://x.test)")).toEqual([
      { start: 5, end: 19, text: "https://x.test", kind: "http" },
    ]);
  });

  it("keeps closing paren when it balances a paren inside the URL", () => {
    const line = "https://en.wikipedia.org/wiki/Foo_(bar)";
    expect(detectLinks(line)).toEqual([
      {
        start: 0,
        end: line.length,
        text: "https://en.wikipedia.org/wiki/Foo_(bar)",
        kind: "http",
      },
    ]);
  });

  it("strips trailing quote", () => {
    expect(detectLinks(`hit "https://x.test"`)).toEqual([
      { start: 5, end: 19, text: "https://x.test", kind: "http" },
    ]);
  });
});

describe("detectLinks — paths", () => {
  it("matches an absolute POSIX path", () => {
    expect(detectLinks("log: /var/log/x.log here")).toEqual([
      { start: 5, end: 19, text: "/var/log/x.log", kind: "path" },
    ]);
  });

  it("matches a home-anchored path", () => {
    expect(detectLinks("cd ~/Projects/foo")).toEqual([
      { start: 3, end: 17, text: "~/Projects/foo", kind: "path" },
    ]);
  });

  it("matches '~/' alone", () => {
    expect(detectLinks("home is ~/")).toEqual([
      { start: 8, end: 10, text: "~/", kind: "path" },
    ]);
  });

  it("matches '/' alone (root)", () => {
    expect(detectLinks("disk / is full")).toEqual([
      { start: 5, end: 6, text: "/", kind: "path" },
    ]);
  });

  it("does NOT match relative paths", () => {
    expect(detectLinks("see ./foo and src/main.go")).toEqual([]);
  });

  it("does NOT match shorthand ~user", () => {
    expect(detectLinks("cd ~foo")).toEqual([]);
  });

  it("does NOT match numeric expressions like 12/24", () => {
    expect(detectLinks("date 12/24/2025")).toEqual([]);
  });

  it("strips trailing punct on paths too", () => {
    expect(detectLinks("see /var/log,")).toEqual([
      { start: 4, end: 12, text: "/var/log", kind: "path" },
    ]);
  });

  it("does not double-match path inside a URL", () => {
    const got = detectLinks("file:///tmp/x and /etc/hosts");
    expect(got.map((m) => m.text)).toEqual(["file:///tmp/x", "/etc/hosts"]);
  });
});

describe("normalizeForOpen", () => {
  const http: LinkMatch = { start: 0, end: 10, text: "http://x.t", kind: "http" };
  const file: LinkMatch = { start: 0, end: 17, text: "file:///tmp/x.log", kind: "file" };
  const abs: LinkMatch = { start: 0, end: 9, text: "/usr/local", kind: "path" };
  const home: LinkMatch = { start: 0, end: 14, text: "~/Projects/foo", kind: "path" };

  it("returns http(s) untouched", () => {
    expect(normalizeForOpen(http, "")).toBe("http://x.t");
  });

  it("returns file:// untouched", () => {
    expect(normalizeForOpen(file, "")).toBe("file:///tmp/x.log");
  });

  it("wraps absolute path in file://", () => {
    expect(normalizeForOpen(abs, "")).toBe("file:///usr/local");
  });

  it("expands ~ when homeDir provided", () => {
    expect(normalizeForOpen(home, "/Users/me")).toBe("file:///Users/me/Projects/foo");
  });

  it("returns null when ~ encountered with no homeDir", () => {
    expect(normalizeForOpen(home, "")).toBeNull();
    expect(normalizeForOpen(home, undefined)).toBeNull();
  });

  it("strips trailing slash from homeDir before joining", () => {
    expect(normalizeForOpen(home, "/Users/me/")).toBe("file:///Users/me/Projects/foo");
  });
});

describe("isModClickEvent", () => {
  function ev(opts: MouseEventInit): MouseEvent {
    return new MouseEvent("click", opts);
  }

  it("mac requires metaKey only", () => {
    expect(isModClickEvent(ev({ metaKey: true }), true)).toBe(true);
    expect(isModClickEvent(ev({ ctrlKey: true }), true)).toBe(false);
    expect(isModClickEvent(ev({}), true)).toBe(false);
  });

  it("non-mac requires ctrlKey only", () => {
    expect(isModClickEvent(ev({ ctrlKey: true }), false)).toBe(true);
    expect(isModClickEvent(ev({ metaKey: true }), false)).toBe(false);
    expect(isModClickEvent(ev({}), false)).toBe(false);
  });

  it("rejects combinations with alt/shift", () => {
    expect(isModClickEvent(ev({ metaKey: true, shiftKey: true }), true)).toBe(false);
    expect(isModClickEvent(ev({ metaKey: true, altKey: true }), true)).toBe(false);
    expect(isModClickEvent(ev({ ctrlKey: true, shiftKey: true }), false)).toBe(false);
    expect(isModClickEvent(ev({ ctrlKey: true, altKey: true }), false)).toBe(false);
  });

  it("rejects when both ctrl and meta are pressed (ambiguous)", () => {
    expect(isModClickEvent(ev({ metaKey: true, ctrlKey: true }), true)).toBe(false);
    expect(isModClickEvent(ev({ metaKey: true, ctrlKey: true }), false)).toBe(false);
  });
});
