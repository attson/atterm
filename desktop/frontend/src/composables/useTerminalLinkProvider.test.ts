import { describe, expect, it, vi } from "vitest";
import { useTerminalLinkProvider } from "./useTerminalLinkProvider";

// A char counts as 2 cells (wide) when it's in the CJK ideograph block — enough
// for these tests; production width comes from xterm's real cell metadata.
function cellWidth(ch: string): number {
  const cp = ch.codePointAt(0) ?? 0;
  return cp >= 0x1100 && cp <= 0x9fff ? 2 : 1;
}

// Build an xterm-like cell grid from a string: a wide glyph occupies one cell of
// width 2 followed by a width-0 spacer cell, exactly like xterm's buffer.
function cellsFromString(s: string): Array<{ chars: string; width: number }> {
  const cells: Array<{ chars: string; width: number }> = [];
  for (const ch of s) {
    const w = cellWidth(ch);
    cells.push({ chars: ch, width: w });
    for (let i = 1; i < w; i++) cells.push({ chars: "", width: 0 });
  }
  return cells;
}

// A minimal DOM-like element that records mousedown listeners so tests can
// emit a mousedown before invoking activate (drag-vs-click detection).
function fakeElement() {
  const listeners: Record<string, ((e: any) => void)[]> = {};
  return {
    addEventListener: (type: string, cb: (e: any) => void) => {
      (listeners[type] ||= []).push(cb);
    },
    removeEventListener: (type: string, cb: (e: any) => void) => {
      listeners[type] = (listeners[type] || []).filter((f) => f !== cb);
    },
    __emit: (type: string, e: any) => (listeners[type] || []).forEach((f) => f(e)),
  };
}

function makeFakeTerm(lineText: string) {
  let provider: {
    provideLinks: (y: number, cb: (links: unknown[] | undefined) => void) => void;
  } | null = null;
  const dispose = vi.fn();
  const clearSelection = vi.fn();
  const cells = cellsFromString(lineText);
  const element = fakeElement();
  return {
    term: {
      cols: cells.length,
      element,
      registerLinkProvider(p: typeof provider) {
        provider = p;
        return { dispose };
      },
      clearSelection,
      buffer: {
        active: {
          getLine(_y: number) {
            return {
              isWrapped: false,
              translateToString: (_trim: boolean) => lineText,
              getCell(x: number) {
                const c = cells[x];
                if (!c) return undefined;
                return { getChars: () => c.chars, getWidth: () => c.width };
              },
            };
          },
        },
      },
    } as unknown as import("xterm").Terminal,
    element,
    getProvider: () => provider!,
    dispose,
    clearSelection,
  };
}

// Build a fake term whose buffer holds multiple physical rows with isWrapped
// flags, for exercising soft-wrapped link stitching.
function makeWrappedTerm(rows: Array<{ text: string; wrapped: boolean }>, cols: number) {
  let provider: {
    provideLinks: (y: number, cb: (links: unknown[] | undefined) => void) => void;
  } | null = null;
  const dispose = vi.fn();
  function lineAt(idx: number) {
    const row = rows[idx];
    if (!row) return undefined;
    const cells = cellsFromString(row.text);
    return {
      isWrapped: row.wrapped,
      translateToString: () => row.text,
      getCell(x: number) {
        const c = cells[x];
        if (!c) return undefined;
        return { getChars: () => c.chars, getWidth: () => c.width };
      },
    };
  }
  const element = fakeElement();
  return {
    term: {
      cols,
      element,
      registerLinkProvider(p: typeof provider) {
        provider = p;
        return { dispose };
      },
      clearSelection: vi.fn(),
      buffer: { active: { getLine: (y: number) => lineAt(y) } },
    } as unknown as import("xterm").Terminal,
    getProvider: () => provider!,
  };
}

describe("useTerminalLinkProvider", () => {
  it("provides one ILink per detectLinks match on the requested line", () => {
    const f = makeFakeTerm("see https://x.test now");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "/Users/me",
      openURL,
      onError: vi.fn(),
    });

    let received: any[] | undefined;
    f.getProvider().provideLinks(1, (links) => (received = links as any[]));
    expect(received).toHaveLength(1);
    expect(received![0].text).toBe("https://x.test");
    expect(received![0].range.start.y).toBe(1);
    expect(received![0].range.end.y).toBe(1);
    // All-ASCII line: cell columns equal string indices (1-based, inclusive).
    expect(received![0].range.start.x).toBe(5);
    expect(received![0].range.end.x).toBe(18);
    expect(received![0].decorations).toEqual({ underline: true, pointerCursor: true });
  });

  it("places the link range at cell columns, not string indices, past wide glyphs", () => {
    // "填" is a 2-cell CJK glyph: cells 0-1. Space at cell 2. The URL begins at
    // string index 2 but cell 3, so the underline must start at 1-based col 4.
    const f = makeFakeTerm("填 https://x.test");
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL: vi.fn(),
      onError: vi.fn(),
    });

    let received: any[] | undefined;
    f.getProvider().provideLinks(1, (links) => (received = links as any[]));
    expect(received).toHaveLength(1);
    expect(received![0].text).toBe("https://x.test");
    expect(received![0].range.start.x).toBe(4); // not 3 (the buggy string-index value)
    expect(received![0].range.end.x).toBe(17); // 14 cells: columns 4..17 inclusive
  });

  it("activate opens on a plain click (no modifier, no drag)", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError: vi.fn(),
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    f.element.__emit("mousedown", { clientX: 5, clientY: 5 });
    await links![0].activate(
      new MouseEvent("click", { clientX: 5, clientY: 5 }),
      "https://x.test",
    );
    expect(openURL).toHaveBeenCalledWith("https://x.test");
  });

  it("activate does not open when the click followed a drag", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError: vi.fn(),
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    f.element.__emit("mousedown", { clientX: 5, clientY: 5 });
    await links![0].activate(
      new MouseEvent("click", { clientX: 60, clientY: 5 }),
      "https://x.test",
    );
    expect(openURL).not.toHaveBeenCalled();
  });

  it("stitches a soft-wrapped URL and returns a segment on each physical row", () => {
    const cols = 20;
    const f = makeWrappedTerm(
      [
        { text: "http://ex.com/aaaaa", wrapped: false },
        { text: "bbb/ccc", wrapped: true },
      ],
      cols,
    );
    useTerminalLinkProvider({
      term: f.term,
      isMac: false,
      getHomeDir: () => "",
      openURL: vi.fn(),
      onError: vi.fn(),
    });
    let row1: any[] | undefined;
    let row2: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (row1 = l as any[]));
    f.getProvider().provideLinks(2, (l) => (row2 = l as any[]));
    expect(row1).toHaveLength(1);
    expect(row2).toHaveLength(1);
    expect(row1![0].text).toBe("http://ex.com/aaaaabbb/ccc");
    expect(row2![0].text).toBe("http://ex.com/aaaaabbb/ccc");
    expect(row1![0].range.start.y).toBe(1);
    expect(row2![0].range.start.y).toBe(2);
  });

  it("activate with Mod clears the terminal selection", async () => {
    // xterm.js starts a selection on the mousedown that leads into the
    // click; the linkProvider's activate() must clear it after opening the
    // URL so the user isn't left staring at a blue highlight.
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError: vi.fn(),
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(
      new MouseEvent("click", { metaKey: true }),
      "https://x.test",
    );
    expect(f.clearSelection).toHaveBeenCalledOnce();
  });

  it("activate clears selection even when openURL rejects", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockRejectedValue(new Error("boom"));
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError: vi.fn(),
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(
      new MouseEvent("click", { metaKey: true }),
      "https://x.test",
    );
    expect(f.clearSelection).toHaveBeenCalledOnce();
  });

  it("activate with Mod opens URL", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError: vi.fn(),
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(
      new MouseEvent("click", { metaKey: true }),
      "https://x.test",
    );
    expect(openURL).toHaveBeenCalledWith("https://x.test");
  });

  it("activate with Mod consumes the click so the WebView does not navigate", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError: vi.fn(),
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    const event = new MouseEvent("click", {
      metaKey: true,
      bubbles: true,
      cancelable: true,
    });
    const stopPropagation = vi.spyOn(event, "stopPropagation");
    const stopImmediatePropagation = vi.spyOn(event, "stopImmediatePropagation");

    await links![0].activate(event, "https://x.test");

    expect(event.defaultPrevented).toBe(true);
    expect(stopPropagation).toHaveBeenCalledOnce();
    expect(stopImmediatePropagation).toHaveBeenCalledOnce();
    expect(openURL).toHaveBeenCalledWith("https://x.test");
  });

  it("activate for ~/ without homeDir calls onError, not openURL", async () => {
    const f = makeFakeTerm("cd ~/Projects/foo");
    const openURL = vi.fn().mockResolvedValue(undefined);
    const onError = vi.fn();
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError,
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(
      new MouseEvent("click", { metaKey: true }),
      "~/Projects/foo",
    );
    expect(openURL).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith("terminal.link.openFailedNoHome");
  });

  it("activate surfaces openURL rejection via onError", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockRejectedValue(new Error("boom"));
    const onError = vi.fn();
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL,
      onError,
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(
      new MouseEvent("click", { metaKey: true }),
      "https://x.test",
    );
    expect(onError).toHaveBeenCalledWith("terminal.link.openFailed");
  });

  it("returns a disposable that forwards to xterm", () => {
    const f = makeFakeTerm("");
    const d = useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL: vi.fn(),
      onError: vi.fn(),
    });
    d.dispose();
    expect(f.dispose).toHaveBeenCalled();
  });

  it("calls back with undefined when no matches on the line", () => {
    const f = makeFakeTerm("nothing here");
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "",
      openURL: vi.fn(),
      onError: vi.fn(),
    });
    const cb = vi.fn();
    f.getProvider().provideLinks(1, cb);
    expect(cb).toHaveBeenCalledWith(undefined);
  });
});
