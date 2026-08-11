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
