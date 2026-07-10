import { beforeEach, describe, expect, it, vi } from "vitest";
import { _resetHandlersForTest, pasteFileBus } from "./pasteFileBus";

describe("pasteFileBus", () => {
  beforeEach(() => {
    _resetHandlersForTest();
  });

  it("delivers events to all subscribers", () => {
    const a = vi.fn();
    const b = vi.fn();
    const offA = pasteFileBus.on(a);
    const offB = pasteFileBus.on(b);

    pasteFileBus.emit("notes.pdf", 1234);

    expect(a).toHaveBeenCalledTimes(1);
    expect(b).toHaveBeenCalledTimes(1);
    const event = a.mock.calls[0][0];
    expect(event.name).toBe("notes.pdf");
    expect(event.size).toBe(1234);
    expect(typeof event.id).toBe("string");
    expect(event.id.length).toBeGreaterThan(0);

    offA();
    offB();
  });

  it("does not deliver to unsubscribed handlers", () => {
    const h = vi.fn();
    const off = pasteFileBus.on(h);
    off();

    pasteFileBus.emit("x.log", 10);

    expect(h).not.toHaveBeenCalled();
  });

  it("defaults the name to 'file' when given an empty string", () => {
    const h = vi.fn();
    pasteFileBus.on(h);
    pasteFileBus.emit("", 0);
    expect(h.mock.calls[0][0].name).toBe("file");
  });
});
