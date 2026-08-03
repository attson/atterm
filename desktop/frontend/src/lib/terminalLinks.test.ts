import { describe, expect, it } from "vitest";
import {
  cellInLink,
  detectLinks,
  shouldActivateLink,
  mapBufferLineCells,
  mapWrappedLogicalLine,
  normalizeForOpen,
  type BufferLineLike,
  type LinkMatch,
} from "./terminalLinks";

// Build a BufferLineLike from a string; CJK ideographs count as 2-cell glyphs
// (one width-2 cell + a width-0 spacer), mirroring xterm's buffer layout.
function fakeLine(s: string): { line: BufferLineLike; cols: number } {
  const cells: Array<{ chars: string; width: number }> = [];
  for (const ch of s) {
    const cp = ch.codePointAt(0) ?? 0;
    const w = cp >= 0x1100 && cp <= 0x9fff ? 2 : 1;
    cells.push({ chars: ch, width: w });
    for (let i = 1; i < w; i++) cells.push({ chars: "", width: 0 });
  }
  return {
    cols: cells.length,
    line: {
      getCell(x: number) {
        const c = cells[x];
        if (!c) return undefined;
        return { getChars: () => c.chars, getWidth: () => c.width };
      },
    },
  };
}

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

describe("shouldActivateLink", () => {
  const ev = (o: Partial<MouseEvent>) =>
    ({ shiftKey: false, altKey: false, ctrlKey: false, metaKey: false, clientX: 0, clientY: 0, ...o }) as MouseEvent;

  it("opens on a plain click with no drag", () => {
    expect(shouldActivateLink(ev({ clientX: 10, clientY: 10 }), { x: 10, y: 10 }, false)).toBe(true);
  });

  it("opens on a plain click when there is no mousedown record", () => {
    expect(shouldActivateLink(ev({}), null, false)).toBe(true);
  });

  it("does not open when the pointer dragged more than the threshold", () => {
    expect(shouldActivateLink(ev({ clientX: 40, clientY: 10 }), { x: 10, y: 10 }, false)).toBe(false);
  });

  it("opens on a small sub-threshold movement", () => {
    expect(shouldActivateLink(ev({ clientX: 12, clientY: 11 }), { x: 10, y: 10 }, false)).toBe(true);
  });

  it("does not open when shift is held", () => {
    expect(shouldActivateLink(ev({ shiftKey: true }), null, false)).toBe(false);
  });

  it("does not open when alt is held", () => {
    expect(shouldActivateLink(ev({ altKey: true }), null, false)).toBe(false);
  });

  it("opens on ctrl-click (non-mac)", () => {
    expect(shouldActivateLink(ev({ ctrlKey: true }), null, false)).toBe(true);
  });
});

describe("mapBufferLineCells", () => {
  it("maps string indices 1:1 to cells for an all-ASCII line", () => {
    const { line, cols } = fakeLine("ab cd");
    const { text, cellStart } = mapBufferLineCells(line, cols);
    expect(text).toBe("ab cd");
    expect(cellStart).toEqual([0, 1, 2, 3, 4, 5]); // includes the past-end sentinel
  });

  it("advances two cells per wide glyph", () => {
    // 填(2) space(1) A(1)  ->  cells: 填@0, spacer@1, space@2, A@3
    const { line, cols } = fakeLine("填 A");
    const { text, cellStart } = mapBufferLineCells(line, cols);
    expect(text).toBe("填 A");
    // 填@cell0, space@cell2, A@cell3, sentinel past A = cell4
    expect(cellStart).toEqual([0, 2, 3, 4]);
  });

  it("renders unwritten cells as spaces", () => {
    const cells = [{ chars: "x", width: 1 }, { chars: "", width: 1 }];
    const line: BufferLineLike = {
      getCell(x) {
        const c = cells[x];
        return c ? { getChars: () => c.chars, getWidth: () => c.width } : undefined;
      },
    };
    expect(mapBufferLineCells(line, 2).text).toBe("x ");
  });
});

describe("cellInLink past wide glyphs", () => {
  it("hit-tests by cell column, not string index", () => {
    const { line, cols } = fakeLine("填 https://x.test");
    const { text, cellStart } = mapBufferLineCells(line, cols);
    const m = detectLinks(text)[0];
    expect(cellInLink(2, m, cellStart)).toBe(false); // would be a false hit pre-fix
    expect(cellInLink(3, m, cellStart)).toBe(true); // first cell of the URL
    expect(cellInLink(16, m, cellStart)).toBe(true); // last cell of the URL
    expect(cellInLink(17, m, cellStart)).toBe(false); // just past the URL
  });
});

// Build a fake xterm buffer from an array of {text, wrapped} physical lines.
function fakeBuffer(rows: Array<{ text: string; wrapped: boolean }>, cols: number) {
  function lineAt(idx: number) {
    const row = rows[idx];
    if (!row) return undefined;
    const cells: Array<{ chars: string; width: number }> = [];
    for (const ch of row.text) {
      const cp = ch.codePointAt(0) ?? 0;
      const w = cp >= 0x1100 && cp <= 0x9fff ? 2 : 1;
      cells.push({ chars: ch, width: w });
      for (let i = 1; i < w; i++) cells.push({ chars: "", width: 0 });
    }
    return {
      isWrapped: row.wrapped,
      getCell(x: number) {
        const c = cells[x];
        if (!c) return undefined;
        return { getChars: () => c.chars, getWidth: () => c.width };
      },
    };
  }
  return { getLine: (y: number) => lineAt(y) };
}

describe("mapWrappedLogicalLine", () => {
  it("joins a URL split across two soft-wrapped physical lines", () => {
    const cols = 20;
    const buf = fakeBuffer(
      [
        { text: "http://ex.com/aaaaa", wrapped: false }, // firstY = 0
        { text: "bbb/ccc", wrapped: true },
      ],
      cols,
    );
    const { text, cellY } = mapWrappedLogicalLine(buf, 0, cols, 50);
    expect(text.startsWith("http://ex.com/aaaaa")).toBe(true);
    expect(text).toContain("bbb/ccc");
    // the joined text detects as one link
    expect(detectLinks(text).map((m) => m.text)).toEqual(["http://ex.com/aaaaabbb/ccc"]);
    // characters from the second physical line report physical row 1
    const idx = text.indexOf("bbb");
    expect(cellY[idx]).toBe(1);
  });

  it("does not join when the next line is a hard newline (isWrapped=false)", () => {
    const cols = 20;
    const buf = fakeBuffer(
      [
        { text: "http://ex.com/aaaaa", wrapped: false },
        { text: "bbb/ccc", wrapped: false },
      ],
      cols,
    );
    const { text } = mapWrappedLogicalLine(buf, 0, cols, 50);
    expect(text).toBe("http://ex.com/aaaaa");
  });
});
