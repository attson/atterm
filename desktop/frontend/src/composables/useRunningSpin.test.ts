import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// A single shared animation clock drives every running indicator (task-state
// icons + the titlebar sweep). PR #373 removed the per-icon CSS animations
// because each mounted copy scheduled its own repaint loop, which pins the
// renderer CPU on machines without GPU compositing (software-rendered CSS
// animations run on the main thread, once per copy per frame). One rAF loop,
// throttled to ~8fps, shared by N subscribers, keeps that cost flat — and the
// loop is fully stopped whenever no running indicator is mounted.

beforeEach(() => {
  vi.resetModules();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

// rAF drives the clock off a monotonic timestamp; fake timers advance it.
// vitest's fake timers advance Date but not performance.now, so point the
// clock's timestamp source at the faked Date and feed rAF callbacks the same
// value — this keeps the throttle's time base consistent under fake timers.
function installFakeRaf(): { cancels: number } {
  const state = { cancels: 0 };
  let nextId = 1;
  const handles = new Map<number, ReturnType<typeof setTimeout>>();
  vi.stubGlobal("performance", { now: () => Date.now() });
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
    const id = nextId++;
    const t = setTimeout(() => cb(Date.now()), 16);
    handles.set(id, t);
    return id;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => {
    const t = handles.get(id);
    if (t !== undefined) {
      clearTimeout(t);
      handles.delete(id);
      state.cancels++;
    }
  });
  return state;
}

describe("useRunningSpin", () => {
  it("does not advance the phase until a subscriber acquires", async () => {
    installFakeRaf();
    const { useRunningSpin } = await import("./useRunningSpin");
    const { phase } = useRunningSpin();
    const before = phase.value;
    vi.advanceTimersByTime(1000);
    expect(phase.value).toBe(before);
  });

  it("advances the phase once a subscriber acquires the clock", async () => {
    installFakeRaf();
    const { useRunningSpin } = await import("./useRunningSpin");
    const { phase, acquire } = useRunningSpin();
    const before = phase.value;
    acquire();
    vi.advanceTimersByTime(1000);
    expect(phase.value).not.toBe(before);
  });

  it("stops the clock when the last subscriber releases", async () => {
    const raf = installFakeRaf();
    const { useRunningSpin } = await import("./useRunningSpin");
    const { acquire, release } = useRunningSpin();
    acquire();
    vi.advanceTimersByTime(100);
    expect(raf.cancels).toBe(0);
    release();
    // Releasing the last subscriber cancels the pending frame; no further
    // frames are scheduled, so the phase is frozen.
    expect(raf.cancels).toBe(1);
  });

  it("keeps a single clock running while any subscriber remains", async () => {
    const raf = installFakeRaf();
    const { useRunningSpin } = await import("./useRunningSpin");
    const { phase, acquire, release } = useRunningSpin();
    acquire();
    acquire();
    vi.advanceTimersByTime(500);
    release();
    // One of two subscribers left — the clock must keep running.
    expect(raf.cancels).toBe(0);
    const mid = phase.value;
    vi.advanceTimersByTime(500);
    expect(phase.value).not.toBe(mid);
    release();
    expect(raf.cancels).toBe(1);
  });

  it("shares one clock across separate useRunningSpin() callers", async () => {
    installFakeRaf();
    const { useRunningSpin } = await import("./useRunningSpin");
    const a = useRunningSpin();
    const b = useRunningSpin();
    expect(a.phase).toBe(b.phase);
    a.acquire();
    vi.advanceTimersByTime(500);
    // b sees the same advancing phase without acquiring itself.
    expect(b.phase.value).not.toBe(0);
  });

  it("throttles updates to roughly 8fps rather than every animation frame", async () => {
    installFakeRaf();
    const { useRunningSpin } = await import("./useRunningSpin");
    const { phase, acquire } = useRunningSpin();
    acquire();
    // One ~16ms frame is below the ~125ms (8fps) step, so the phase must not
    // move yet — proving the clock is throttled, not every-frame.
    const start = phase.value;
    vi.advanceTimersByTime(16);
    expect(phase.value).toBe(start);
    // Past one 125ms step it advances.
    vi.advanceTimersByTime(125);
    expect(phase.value).not.toBe(start);
  });
});
