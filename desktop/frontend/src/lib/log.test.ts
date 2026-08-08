import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FrontendLogRecord } from "./api/_bindings";
import {
  __resetLogBufferForTest,
  errText,
  flushLogs,
  logDebug,
  logError,
  logInfo,
  logWarn,
} from "./log";

function installBinding(impl?: (records: FrontendLogRecord[]) => Promise<number>) {
  const calls: FrontendLogRecord[][] = [];
  const fn = vi.fn(async (records: FrontendLogRecord[]) => {
    calls.push(records);
    return impl ? impl(records) : records.length;
  });
  (window as any).go = { main: { App: { AppendFrontendLogs: fn } } };
  return { calls, fn };
}

function removeBinding() {
  delete (window as any).go;
}

beforeEach(() => {
  __resetLogBufferForTest();
  vi.useFakeTimers();
  // The module mirrors to console in dev; silence it so test output stays readable.
  vi.spyOn(console, "info").mockImplementation(() => {});
  vi.spyOn(console, "warn").mockImplementation(() => {});
  vi.spyOn(console, "error").mockImplementation(() => {});
  vi.spyOn(console, "debug").mockImplementation(() => {});
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  removeBinding();
  __resetLogBufferForTest();
});

describe("buffering", () => {
  it("holds records until the flush interval elapses", async () => {
    const { fn } = installBinding();

    logInfo("boot", "step one");
    logInfo("boot", "step two");
    expect(fn).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(1000);
    expect(fn).toHaveBeenCalledTimes(1);
    expect(fn.mock.calls[0][0]).toHaveLength(2);
  });

  it("flushes immediately once 64 records queue up", async () => {
    const { fn } = installBinding();

    for (let i = 0; i < 64; i++) logInfo("term", `line ${i}`);
    await vi.advanceTimersByTimeAsync(0);

    expect(fn).toHaveBeenCalledTimes(1);
    expect(fn.mock.calls[0][0]).toHaveLength(64);
  });

  // An error is often immediately followed by the renderer dying, which would
  // take an unflushed buffer with it.
  it("flushes an ERROR without waiting", async () => {
    const { fn } = installBinding();

    logInfo("boot", "before");
    logError("boot", "step failed");
    await vi.advanceTimersByTimeAsync(0);

    expect(fn).toHaveBeenCalledTimes(1);
    const batch = fn.mock.calls[0][0];
    expect(batch.map((r) => r.message)).toEqual(["before", "step failed"]);
  });

  it("stamps each record when it is created, not when it is flushed", async () => {
    const { fn } = installBinding();
    vi.setSystemTime(new Date("2026-08-08T10:00:00Z"));
    logInfo("boot", "first");
    vi.setSystemTime(new Date("2026-08-08T10:00:05Z"));
    logInfo("boot", "second");

    await vi.advanceTimersByTimeAsync(1000);
    const batch = fn.mock.calls[0][0];
    expect(batch[1].timestamp_ms - batch[0].timestamp_ms).toBe(5000);
  });
});

describe("levels", () => {
  it("tags each record with its level", async () => {
    const { fn } = installBinding();

    logDebug("term", "d");
    logInfo("term", "i");
    logWarn("term", "w");
    logError("term", "e");
    await vi.advanceTimersByTimeAsync(1000);

    const levels = fn.mock.calls.flatMap((c) => c[0]).map((r) => r.level);
    expect(levels).toEqual(["DEBUG", "INFO", "WARN", "ERROR"]);
  });
});

describe("fields", () => {
  it("appends primitives as key=value pairs", async () => {
    const { fn } = installBinding();

    logInfo("conn", "attached", { session: "abc", seq: 12, driver: true });
    await vi.advanceTimersByTimeAsync(1000);

    expect(fn.mock.calls[0][0][0].message).toBe(
      'attached session="abc" seq="12" driver="true"',
    );
  });

  it("omits undefined fields", async () => {
    const { fn } = installBinding();

    logInfo("conn", "attached", { session: "abc", missing: undefined });
    await vi.advanceTimersByTimeAsync(1000);

    expect(fn.mock.calls[0][0][0].message).toBe('attached session="abc"');
  });

  // AGENTS.md red line #21: the account_key and friends must never reach disk.
  it("redacts secret-looking field names", async () => {
    const { fn } = installBinding();

    logWarn("relay", "login failed", {
      email: "a@b.c",
      password: "hunter2",
      account_key: "deadbeef",
      session_token: "zzz111",
      credential: "qqq222",
      authHeader: "Bearer xyz789",
    });
    await vi.advanceTimersByTimeAsync(1000);

    const msg = fn.mock.calls[0][0][0].message;
    expect(msg).toContain('email="a@b.c"');
    for (const secret of ["hunter2", "deadbeef", "zzz111", "qqq222", "xyz789"]) {
      expect(msg).not.toContain(secret);
    }
    expect(msg).toContain('password="***"');
    expect(msg).toContain('account_key="***"');
  });
});

describe("overflow", () => {
  it("drops the oldest records past the cap and reports the loss", async () => {
    // Hold the first flush in flight so the buffer genuinely has to absorb the
    // overflow; later flushes resolve normally so the drain can complete.
    let release: (v: number) => void = () => {};
    let firstCall = true;
    const { fn } = installBinding(() => {
      if (!firstCall) return Promise.resolve(0);
      firstCall = false;
      return new Promise<number>((resolve) => (release = resolve));
    });

    for (let i = 0; i < 1200; i++) logInfo("term", `line ${i}`);
    release(0);
    // Let the stuck flush settle, then drain what piled up behind it. The
    // auto-flush path doesn't re-arm the timer, so this has to be explicit.
    await vi.advanceTimersByTimeAsync(0);
    await flushLogs();

    const all = fn.mock.calls.flatMap((c) => c[0]);
    const dropNotice = all.find((r) => r.message.includes("dropped"));
    expect(dropNotice).toBeDefined();
    expect(dropNotice!.level).toBe("WARN");
    expect(dropNotice!.tag).toBe("log");
  });
});

describe("degradation", () => {
  it("stays console-only and never throws without a binding", async () => {
    removeBinding();

    expect(() => logWarn("capacitor", "no bindings here")).not.toThrow();
    await expect(flushLogs()).resolves.toBeUndefined();
    expect(console.warn).toHaveBeenCalled();
  });

  it("retries a failed flush once, then stops trying", async () => {
    const fn = vi.fn(async (_records: FrontendLogRecord[]): Promise<number> => {
      throw new Error("bridge down");
    });
    (window as any).go = { main: { App: { AppendFrontendLogs: fn } } };

    logInfo("boot", "first");
    await vi.advanceTimersByTimeAsync(1000);
    expect(fn).toHaveBeenCalledTimes(1);

    // Next flush re-sends the held-back record alongside the new one.
    logInfo("boot", "second");
    await vi.advanceTimersByTimeAsync(1000);
    expect(fn).toHaveBeenCalledTimes(2);
    expect(fn.mock.calls[1][0].map((r: FrontendLogRecord) => r.message)).toEqual([
      "first",
      "second",
    ]);

    // A third failure gives up rather than growing the queue forever.
    logInfo("boot", "third");
    await vi.advanceTimersByTimeAsync(1000);
    expect(fn.mock.calls[2][0].length).toBeLessThanOrEqual(3);
  });

  it("does nothing when there is nothing queued", async () => {
    const { fn } = installBinding();
    await flushLogs();
    expect(fn).not.toHaveBeenCalled();
  });
});

describe("errText", () => {
  it("renders Errors, strings and anything else", () => {
    expect(errText(new TypeError("bad"))).toBe("TypeError: bad");
    expect(errText("plain")).toBe("plain");
    expect(errText(42)).toBe("42");
    expect(errText(null)).toBe("null");
  });
});
