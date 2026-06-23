import { describe, expect, test, vi } from "vitest";
import {
  createFocusReportCoalescer,
  FOCUS_IN,
  FOCUS_OUT,
} from "./focusReportCoalescer";

// Deterministic timer harness: captures scheduled callbacks so a test can fire
// them on demand (or assert that none remain).
function makeClock() {
  let nextId = 1;
  const timers = new Map<number, () => void>();
  return {
    setTimer: (fn: () => void, _ms: number) => {
      const id = nextId++;
      timers.set(id, fn);
      return id;
    },
    clearTimer: (h: unknown) => {
      timers.delete(h as number);
    },
    fireAll: () => {
      for (const [id, fn] of [...timers]) {
        timers.delete(id);
        fn();
      }
    },
    pending: () => timers.size,
  };
}

function setup() {
  const send = vi.fn();
  const clock = makeClock();
  const coalescer = createFocusReportCoalescer({
    send,
    setTimer: clock.setTimer,
    clearTimer: clock.clearTimer,
  });
  return { send, clock, coalescer };
}

describe("createFocusReportCoalescer", () => {
  test("drops a blur that is immediately reversed by a refocus", () => {
    const { send, clock, coalescer } = setup();
    coalescer.handle(FOCUS_OUT);
    coalescer.handle(FOCUS_IN);
    expect(send).not.toHaveBeenCalled();
    expect(clock.pending()).toBe(0);
  });

  test("reports a genuine blur once the flap window elapses", () => {
    const { send, clock, coalescer } = setup();
    coalescer.handle(FOCUS_OUT);
    expect(send).not.toHaveBeenCalled();
    clock.fireAll();
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith(FOCUS_OUT);
  });

  test("passes a focus-in through when no blur is pending", () => {
    const { send, coalescer } = setup();
    coalescer.handle(FOCUS_IN);
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith(FOCUS_IN);
  });

  test("forwards ordinary input unchanged", () => {
    const { send, coalescer } = setup();
    coalescer.handle("ls\r");
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith("ls\r");
  });

  test("drops a pending blur when real input proves the terminal is focused", () => {
    const { send, clock, coalescer } = setup();
    coalescer.handle(FOCUS_OUT);
    coalescer.handle("x");
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith("x");
    expect(clock.pending()).toBe(0);
  });

  test("coalesces a second blur while one is already pending", () => {
    const { send, clock, coalescer } = setup();
    coalescer.handle(FOCUS_OUT);
    coalescer.handle(FOCUS_OUT);
    expect(clock.pending()).toBe(1);
    clock.fireAll();
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith(FOCUS_OUT);
  });

  test("dispose clears a held blur without sending it", () => {
    const { send, clock, coalescer } = setup();
    coalescer.handle(FOCUS_OUT);
    coalescer.dispose();
    clock.fireAll();
    expect(send).not.toHaveBeenCalled();
    expect(clock.pending()).toBe(0);
  });
});
