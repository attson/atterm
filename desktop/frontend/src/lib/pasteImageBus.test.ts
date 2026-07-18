import { beforeEach, describe, expect, it, vi } from "vitest";
import { _resetHandlersForTest, pasteImageBus } from "./pasteImageBus";

describe("pasteImageBus", () => {
  beforeEach(() => {
    _resetHandlersForTest();
  });

  it("delivers events to all subscribers", () => {
    const a = vi.fn();
    const b = vi.fn();
    const offA = pasteImageBus.on(a);
    const offB = pasteImageBus.on(b);
    const blob = new Blob(["x"], { type: "image/png" });

    pasteImageBus.emit(blob, "shot.png");

    expect(a).toHaveBeenCalledTimes(1);
    expect(b).toHaveBeenCalledTimes(1);
    const event = a.mock.calls[0][0];
    expect(event.file).toBe(blob);
    expect(event.name).toBe("shot.png");
    expect(typeof event.id).toBe("string");
    expect(event.id.length).toBeGreaterThan(0);

    offA();
    offB();
  });

  it("does not deliver to unsubscribed handlers", () => {
    const h = vi.fn();
    const off = pasteImageBus.on(h);
    off();

    pasteImageBus.emit(new Blob(["x"], { type: "image/png" }), "x.png");

    expect(h).not.toHaveBeenCalled();
  });

  it("defaults the name to 'clipboard-image' when given an empty string", () => {
    const h = vi.fn();
    const off = pasteImageBus.on(h);

    pasteImageBus.emit(new Blob(["x"], { type: "image/png" }), "");

    expect(h.mock.calls[0][0].name).toBe("clipboard-image");
    off();
  });

  it("isolates handler errors so one failure does not break the others", () => {
    const bad = vi.fn(() => { throw new Error("boom"); });
    const good = vi.fn();
    const offBad = pasteImageBus.on(bad);
    const offGood = pasteImageBus.on(good);

    expect(() =>
      pasteImageBus.emit(new Blob(["x"], { type: "image/png" }), "x.png"),
    ).not.toThrow();
    expect(bad).toHaveBeenCalledTimes(1);
    expect(good).toHaveBeenCalledTimes(1);

    offBad();
    offGood();
  });
});
