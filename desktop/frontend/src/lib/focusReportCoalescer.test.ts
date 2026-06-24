import { describe, expect, test, vi } from "vitest";
import {
  createFocusReportCoalescer,
  FOCUS_IN,
  FOCUS_OUT,
} from "./focusReportCoalescer";

function setup() {
  const send = vi.fn();
  const coalescer = createFocusReportCoalescer({ send });
  return { send, coalescer };
}

describe("createFocusReportCoalescer", () => {
  test("drops focus-out unconditionally", () => {
    const { send, coalescer } = setup();
    coalescer.handle(FOCUS_OUT);
    expect(send).not.toHaveBeenCalled();
  });

  test("drops focus-in unconditionally", () => {
    const { send, coalescer } = setup();
    coalescer.handle(FOCUS_IN);
    expect(send).not.toHaveBeenCalled();
  });

  test("forwards ordinary input unchanged", () => {
    const { send, coalescer } = setup();
    coalescer.handle("ls\r");
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith("ls\r");
  });

  test("forwards non-focus CSI sequences (e.g. cursor key)", () => {
    // \x1b[A is the up-arrow report — same CSI family as the focus reports
    // but must pass through. Guards against a regression where the strip
    // matched the CSI prefix instead of the exact 3-byte sequences.
    const { send, coalescer } = setup();
    coalescer.handle("\x1b[A");
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith("\x1b[A");
  });

  test("dispose is a no-op", () => {
    const { send, coalescer } = setup();
    coalescer.handle("x");
    coalescer.dispose();
    expect(send).toHaveBeenCalledTimes(1);
  });
});
