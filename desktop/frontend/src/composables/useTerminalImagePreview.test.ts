import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  detectImagePlaceholders,
  imagePlaceholderSegmentsAtBufferRow,
  useTerminalImagePreview,
} from "./useTerminalImagePreview";
import {
  _resetTerminalImagePreviewHandlersForTest,
  terminalImagePreviewBus,
} from "../lib/terminalImagePreviewBus";

type Row = { text: string; wrapped?: boolean };

function cellsFromString(text: string, cols: number) {
  return Array.from({ length: cols }, (_, x) => ({
    getChars: () => text[x] ?? "",
    getWidth: () => 1,
  }));
}

function fakeBuffer(rows: Row[], cols: number) {
  return {
    type: "normal" as const,
    baseY: 0,
    cursorY: 0,
    cursorX: 0,
    viewportY: 0,
    get length() { return rows.length; },
    getLine(y: number) {
      const row = rows[y];
      if (!row) return undefined;
      const cells = cellsFromString(row.text, cols);
      return {
        isWrapped: row.wrapped ?? false,
        getCell(x: number) { return cells[x]; },
        translateToString: () => row.text,
      };
    },
  };
}

class FakeMarker {
  isDisposed = false;
  private handlers = new Set<() => void>();

  constructor(public line: number) {}

  onDispose(handler: () => void) {
    this.handlers.add(handler);
    return { dispose: () => this.handlers.delete(handler) };
  }

  dispose() {
    if (this.isDisposed) return;
    this.isDisposed = true;
    this.line = -1;
    for (const handler of this.handlers) handler();
    this.handlers.clear();
  }
}

function makeFakeTerm(initialRows: Row[], initialCols = 40) {
  let rows = initialRows;
  let cols = initialCols;
  let cursorY = 0;
  let provider: { provideLinks(y: number, cb: (links: any[] | undefined) => void): void } | null = null;
  const parsed = new Set<() => void>();
  const markers: FakeMarker[] = [];
  const element = document.createElement("div");
  const grid = document.createElement("div");
  grid.className = "xterm-rows";
  const screen = document.createElement("div");
  screen.className = "xterm-screen";
  grid.getBoundingClientRect = () => ({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: cols * 10,
    bottom: Math.max(1, rows.length) * 20,
    width: cols * 10,
    height: Math.max(1, rows.length) * 20,
    toJSON: () => ({}),
  });
  screen.appendChild(grid);
  element.appendChild(screen);
  document.body.appendChild(element);

  const active = {
    type: "normal" as const,
    baseY: 0,
    get cursorY() { return cursorY; },
    cursorX: 0,
    viewportY: 0,
    get length() { return rows.length; },
    getLine(y: number) { return fakeBuffer(rows, cols).getLine(y); },
  };

  const term = {
    get cols() { return cols; },
    get rows() { return Math.max(1, rows.length); },
    element,
    _core: {
      screenElement: screen,
      _mouseService: {
        getCoords(event: { clientX: number; clientY: number }) {
          return [Math.floor(event.clientX / 10) + 1, Math.floor(event.clientY / 20) + 1];
        },
      },
      _renderService: {
        dimensions: { css: { cell: { width: 10, height: 20 } } },
      },
    },
    buffer: { active },
    registerMarker(offset = 0) {
      const marker = new FakeMarker(active.baseY + active.cursorY + offset);
      markers.push(marker);
      return marker;
    },
    registerLinkProvider(next: typeof provider) {
      provider = next;
      return { dispose: vi.fn() };
    },
    onWriteParsed(handler: () => void) {
      parsed.add(handler);
      return { dispose: () => parsed.delete(handler) };
    },
  };

  return {
    term: term as unknown as import("xterm").Terminal,
    markers,
    setRows(next: Row[], nextCols = cols) { rows = next; cols = nextCols; },
    setCursorY(y: number) { cursorY = y; },
    parse() { for (const handler of parsed) handler(); },
    linksAt(y: number) {
      let links: any[] | undefined;
      provider!.provideLinks(y, (next) => { links = next; });
      return links ?? [];
    },
    cleanup() { element.remove(); },
  };
}

describe("terminal image placeholder matching", () => {
  it("treats repeated placeholder numbers as separate occurrences", () => {
    expect(detectImagePlaceholders("[Image #1] and [Image #1]")).toEqual([
      { start: 0, end: 10, text: "[Image #1]" },
      { start: 15, end: 25, text: "[Image #1]" },
    ]);
  });

  it("recomputes physical segments after a placeholder reflows", () => {
    const wide = fakeBuffer([{ text: "prompt [Image #1]" }], 20);
    expect(imagePlaceholderSegmentsAtBufferRow(wide, 0, 20, 50)).toEqual([
      expect.objectContaining({ occurrence: 0, row: 0, startCell: 7, endCell: 17 }),
    ]);

    const narrow = fakeBuffer([
      { text: "prompt [I", wrapped: false },
      { text: "mage #1]", wrapped: true },
    ], 9);
    expect(imagePlaceholderSegmentsAtBufferRow(narrow, 0, 9, 50)).toEqual([
      expect.objectContaining({ occurrence: 0, row: 0, startCell: 7, endCell: 9 }),
    ]);
    expect(imagePlaceholderSegmentsAtBufferRow(narrow, 1, 9, 50)).toEqual([
      expect.objectContaining({ occurrence: 0, row: 1, startCell: 0, endCell: 8 }),
    ]);
  });
});

describe("useTerminalImagePreview", () => {
  beforeEach(() => {
    _resetTerminalImagePreviewHandlersForTest();
    document.body.innerHTML = "";
    vi.stubGlobal("URL", {
      createObjectURL: vi.fn(() => "blob:hover-preview"),
      revokeObjectURL: vi.fn(),
    });
  });

  it("only binds pasted images emitted for its own session", () => {
    const f = makeFakeTerm([{ text: "" }]);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "session-a" });

    terminalImagePreviewBus.queue(new Blob(["wrong"], { type: "image/png" }), "wrong.png", "session-b");
    terminalImagePreviewBus.queue(new Blob(["right"], { type: "image/png" }), "right.png", "session-a");
    f.setRows([{ text: "[Image #1]" }]);
    f.parse();

    const links = f.linksAt(1);
    expect(links).toHaveLength(1);
    links[0].hover(new MouseEvent("mousemove", { clientX: 20, clientY: 20 }));
    expect(f.term.element?.querySelector("img")?.getAttribute("alt")).toBe("right.png");

    preview.dispose();
    f.cleanup();
  });

  it("shows the preview from terminal mouse hit-testing without link-provider hover", () => {
    const f = makeFakeTerm([{ text: "" }]);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "s" });
    const image = new Blob(["image"], { type: "image/png" });

    terminalImagePreviewBus.queue(image, "mouse.png", "s");
    f.setRows([{ text: "[Image #1]" }]);
    f.parse();
    f.term.element?.dispatchEvent(new MouseEvent("mousemove", {
      clientX: 5,
      clientY: 5,
      bubbles: true,
    }));

    expect((URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0]).toBe(image);
    expect(f.term.element?.querySelector("img")?.getAttribute("alt")).toBe("mouse.png");

    preview.dispose();
    f.cleanup();
  });

  it("uses xterm renderer mouse coordinates when DOM row geometry is misleading", () => {
    const f = makeFakeTerm([{ text: "" }]);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "s" });
    const image = new Blob(["image"], { type: "image/png" });
    const rows = f.term.element?.querySelector<HTMLElement>(".xterm-rows");
    if (!rows) throw new Error("missing fake xterm rows");
    rows.getBoundingClientRect = () => ({
      x: 500,
      y: 500,
      left: 500,
      top: 500,
      right: 501,
      bottom: 501,
      width: 1,
      height: 1,
      toJSON: () => ({}),
    });

    terminalImagePreviewBus.queue(image, "renderer.png", "s");
    f.setRows([{ text: "[Image #1]" }]);
    f.parse();
    f.term.element?.dispatchEvent(new MouseEvent("mousemove", {
      clientX: 5,
      clientY: 5,
      bubbles: true,
    }));

    expect((URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0]).toBe(image);
    expect(f.term.element?.querySelector("img")?.getAttribute("alt")).toBe("renderer.png");

    preview.dispose();
    f.cleanup();
  });

  it("keeps identical Image #1 labels on different input lines independent", () => {
    const f = makeFakeTerm([{ text: "" }, { text: "" }]);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "s" });
    const first = new Blob(["first"], { type: "image/png" });
    const second = new Blob(["second"], { type: "image/png" });

    terminalImagePreviewBus.queue(first, "first.png", "s");
    f.setRows([{ text: "[Image #1]" }, { text: "" }]);
    f.parse();

    f.setCursorY(1);
    terminalImagePreviewBus.queue(second, "second.png", "s");
    f.setRows([{ text: "[Image #1]" }, { text: "[Image #1]" }]);
    f.parse();

    const row1 = f.linksAt(1)[0];
    row1.hover(new MouseEvent("mousemove", { clientX: 10, clientY: 10 }));
    expect((URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0]).toBe(first);
    row1.leave();

    const row2 = f.linksAt(2)[0];
    row2.hover(new MouseEvent("mousemove", { clientX: 10, clientY: 10 }));
    expect((URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0]).toBe(second);

    preview.dispose();
    f.cleanup();
  });

  it("binds multiple images on one input line in paste order", () => {
    const f = makeFakeTerm([{ text: "" }]);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "s" });
    const first = new Blob(["first"], { type: "image/png" });
    const second = new Blob(["second"], { type: "image/png" });

    terminalImagePreviewBus.queue(first, "first.png", "s");
    terminalImagePreviewBus.queue(second, "second.png", "s");
    f.setRows([{ text: "[Image #1] [Image #2]" }]);
    f.parse();

    const links = f.linksAt(1);
    expect(links).toHaveLength(2);
    links[0].hover(new MouseEvent("mousemove", { clientX: 10, clientY: 10 }));
    expect((URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0]).toBe(first);
    links[0].leave();
    links[1].hover(new MouseEvent("mousemove", { clientX: 10, clientY: 10 }));
    expect((URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0]).toBe(second);

    preview.dispose();
    f.cleanup();
  });

  it("uses the marker's reflowed line and releases the hover URL", () => {
    const f = makeFakeTerm([{ text: "prompt " }], 20);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "s" });
    terminalImagePreviewBus.queue(new Blob(["image"], { type: "image/png" }), "resize.png", "s");
    f.setRows([{ text: "prompt [Image #1]" }], 20);
    f.parse();

    f.setRows([
      { text: "prompt [I", wrapped: false },
      { text: "mage #1]", wrapped: true },
    ], 9);
    const secondSegment = f.linksAt(2)[0];
    expect(secondSegment).toBeTruthy();
    secondSegment.hover(new MouseEvent("mousemove", { clientX: 10, clientY: 10 }));
    expect(f.term.element?.querySelector("img")?.getAttribute("alt")).toBe("resize.png");
    secondSegment.leave();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:hover-preview");

    preview.dispose();
    f.cleanup();
  });

  it("reanchors an unbound image when a TUI redraw disposes its marker", () => {
    const f = makeFakeTerm([{ text: "" }]);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "s" });
    const image = new Blob(["image"], { type: "image/png" });

    terminalImagePreviewBus.queue(image, "redrawn.png", "s");
    f.markers[0].dispose();
    f.setRows([{ text: "[Image #1]" }]);
    f.parse();

    expect(f.markers).toHaveLength(2);
    const link = f.linksAt(1)[0];
    expect(link).toBeTruthy();
    link.hover(new MouseEvent("mousemove", { clientX: 10, clientY: 10 }));
    expect((URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0]).toBe(image);

    preview.dispose();
    f.cleanup();
  });

  it("reanchors a bound image when a TUI redraws the submitted prompt", () => {
    const f = makeFakeTerm([{ text: "prompt " }]);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "s" });
    const image = new Blob(["image"], { type: "image/png" });

    terminalImagePreviewBus.queue(image, "submitted.png", "s");
    f.setRows([{ text: "prompt [Image #1] explain this" }]);
    f.parse();
    expect(f.linksAt(1)).toHaveLength(1);

    f.markers[0].dispose();
    f.setRows([{ text: "› [Image #1] explain this" }]);
    f.parse();

    expect(f.markers).toHaveLength(2);
    const link = f.linksAt(1)[0];
    expect(link).toBeTruthy();
    link.hover(new MouseEvent("mousemove", { clientX: 10, clientY: 10 }));
    expect((URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0]).toBe(image);

    preview.dispose();
    f.cleanup();
  });

  it("reanchors a bound image when its live marker row is erased and reused", () => {
    const f = makeFakeTerm([{ text: "prompt " }, { text: "" }]);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "s" });
    const image = new Blob(["image"], { type: "image/png" });

    terminalImagePreviewBus.queue(image, "moved.png", "s");
    f.setRows([{ text: "prompt [Image #1] explain this" }, { text: "" }]);
    f.parse();
    expect(f.linksAt(1)).toHaveLength(1);

    // A full-screen TUI may leave the marker alive while reusing its physical
    // row, then draw the submitted prompt elsewhere in the same parse batch.
    f.setRows([{ text: "" }, { text: "› [Image #1] explain this" }]);
    f.setCursorY(0);
    f.parse();

    expect(f.markers[0].isDisposed).toBe(true);
    expect(f.markers).toHaveLength(2);
    const link = f.linksAt(2)[0];
    expect(link).toBeTruthy();
    link.hover(new MouseEvent("mousemove", { clientX: 10, clientY: 10 }));
    expect((URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[0]).toBe(image);

    preview.dispose();
    f.cleanup();
  });

  it("drops the image when its marker leaves scrollback", () => {
    const f = makeFakeTerm([{ text: "" }]);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "s" });
    terminalImagePreviewBus.queue(new Blob(["image"], { type: "image/png" }), "gone.png", "s");
    f.setRows([{ text: "[Image #1]" }]);
    f.parse();
    expect(f.linksAt(1)).toHaveLength(1);

    f.markers[0].dispose();
    expect(f.linksAt(1)).toHaveLength(0);

    preview.dispose();
    f.cleanup();
  });

  it("does not bind a placeholder after the image send is cancelled", () => {
    const f = makeFakeTerm([{ text: "" }]);
    const preview = useTerminalImagePreview({ term: f.term, sessionId: () => "s" });
    const id = terminalImagePreviewBus.queue(
      new Blob(["image"], { type: "image/png" }),
      "failed.png",
      "s",
    );

    terminalImagePreviewBus.cancel(id, "s");
    f.setRows([{ text: "[Image #1]" }]);
    f.parse();

    expect(f.linksAt(1)).toHaveLength(0);
    preview.dispose();
    f.cleanup();
  });
});

