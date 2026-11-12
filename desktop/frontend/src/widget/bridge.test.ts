import { afterEach, describe, expect, it, vi } from "vitest";
import { widgetBridge } from "./bridge";

describe("widgetBridge.resize", () => {
  afterEach(() => {
    delete (window as unknown as { go?: unknown }).go;
  });

  it("returns the native resize promise so callers can wait for the window", () => {
    const nativeResize = vi.fn(() => new Promise<void>(() => {}));
    (window as unknown as { go: unknown }).go = {
      main: {
        WidgetBridge: {
          Resize: nativeResize,
        },
      },
    };

    const result: unknown = widgetBridge.resize(252, 200);

    expect(nativeResize).toHaveBeenCalledWith(252, 200);
    expect(result).toBeInstanceOf(Promise);
  });
});
