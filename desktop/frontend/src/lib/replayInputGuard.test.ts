import { describe, expect, test } from "vitest";
import { createReplayInputGuard } from "./replayInputGuard";

describe("createReplayInputGuard", () => {
  test("blocks xterm responses from replay until the write queue drains", () => {
    const guard = createReplayInputGuard();
    let release: (() => void) | undefined;

    guard.onProgress("start");
    expect(guard.shouldForward()).toBe(false);

    guard.onProgress("end", (done) => {
      release = done;
    });
    expect(guard.shouldForward()).toBe(false);

    release?.();
    expect(guard.shouldForward()).toBe(true);
  });

  test("keeps the guard active across replay chunks", () => {
    const guard = createReplayInputGuard();
    guard.onProgress("start");
    guard.onProgress("chunk");
    expect(guard.shouldForward()).toBe(false);
  });
});

describe("createReplayInputGuard user input", () => {
  // The guard exists to swallow xterm's automatic replies to escape sequences
  // inside replayed scrollback — not the human's keystrokes. A runaway command
  // is exactly when the scrollback is huge AND you need Ctrl-C to land, so
  // dropping real keys during replay left closing the tab as the only way out.
  test("lets user keystrokes through while replay is in flight", () => {
    const guard = createReplayInputGuard();
    guard.onProgress("start");
    expect(guard.shouldForward()).toBe(false);
    expect(guard.shouldForward(true)).toBe(true);
  });

  test("lets user keystrokes through while the write queue drains", () => {
    const guard = createReplayInputGuard();
    guard.onProgress("start");
    guard.onProgress("end", () => {});
    expect(guard.shouldForward()).toBe(false);
    expect(guard.shouldForward(true)).toBe(true);
  });

  test("still forwards everything once idle", () => {
    const guard = createReplayInputGuard();
    expect(guard.shouldForward()).toBe(true);
    expect(guard.shouldForward(true)).toBe(true);
  });
});
