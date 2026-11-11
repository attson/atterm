import { describe, expect, test, vi } from "vitest";
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

describe("terminal activation", () => {
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
