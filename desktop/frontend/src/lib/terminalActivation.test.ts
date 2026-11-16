import { afterEach, describe, expect, test, vi } from "vitest";
import { scheduleTerminalActivation } from "./terminalActivation";

function createFrameScheduler() {
  let nextID = 1;
  const callbacks = new Map<number, FrameRequestCallback>();
  return {
    frames: {
      request(callback: FrameRequestCallback) {
        const id = nextID++;
        callbacks.set(id, callback);
        return id;
      },
      cancel(id: number) {
        callbacks.delete(id);
      },
    },
    runNext() {
      const entry = callbacks.entries().next().value as [number, FrameRequestCallback] | undefined;
      if (!entry) throw new Error("no animation frame is pending");
      callbacks.delete(entry[0]);
      entry[1](0);
    },
    pending: () => callbacks.size,
  };
}

afterEach(() => { vi.restoreAllMocks(); });

describe("terminal activation", () => {
  test.each([0, 1])("finishes activation if the page hides after %i frames instead of waiting for a frozen RAF", (framesRun) => {
    const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(false);
    const scheduler = createFrameScheduler();
    const resume = vi.fn();
    scheduleTerminalActivation(() => undefined, resume, scheduler.frames);
    for (let i = 0; i < framesRun; i++) scheduler.runNext();
    hidden.mockReturnValue(true);
    document.dispatchEvent(new Event("visibilitychange"));
    expect(resume).toHaveBeenCalledTimes(1);
    expect(scheduler.pending()).toBe(0);
    document.dispatchEvent(new Event("visibilitychange"));
    expect(resume).toHaveBeenCalledTimes(1);
  });

  test("reveals the cached terminal for one frame before resuming output", () => {
    const scheduler = createFrameScheduler();
    const events: string[] = [];

    scheduleTerminalActivation(
      () => events.push("reveal"),
      () => events.push("resume"),
      scheduler.frames,
    );

    expect(events).toEqual([]);
    scheduler.runNext();
    expect(events).toEqual(["reveal"]);
    scheduler.runNext();
    expect(events).toEqual(["reveal", "resume"]);
  });

  test("cancels a pending resume when the tab is hidden again", () => {
    const scheduler = createFrameScheduler();
    const resume = vi.fn();
    const cancel = scheduleTerminalActivation(() => undefined, resume, scheduler.frames);

    scheduler.runNext();
    cancel();

    expect(scheduler.pending()).toBe(0);
    expect(resume).not.toHaveBeenCalled();
  });
});
