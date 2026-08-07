import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { effect, effectScope, nextTick } from "vue";
import { flushPromises } from "@vue/test-utils";
import { useSessionPins, __resetForTests } from "./useSessionPins";
import { __setPlatformForTests } from "../platform";
import {
  createFakePlatform as fakePlatform,
  fakeEventBus,
} from "../platform/__tests__/_fakePlatform";

describe("useSessionPins", () => {
  let scope: ReturnType<typeof effectScope>;
  beforeEach(() => {
    __resetForTests();
    vi.restoreAllMocks();
    vi.useFakeTimers();
    scope = effectScope();
  });
  afterEach(() => {
    vi.useRealTimers();
    scope.stop();
    __setPlatformForTests(null);
  });

  test("loads pinned ids from platform on first use", async () => {
    const getPins = vi.fn().mockResolvedValue(["a", "b"]);
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();
    await nextTick();
    expect(pins.isPinned("a")).toBe(true);
    expect(pins.isPinned("b")).toBe(true);
    expect(pins.isPinned("c")).toBe(false);
  });

  test("only fetches once across multiple useSessionPins() calls", async () => {
    const getPins = vi.fn().mockResolvedValue([]);
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins },
    } as any);
    scope.run(() => {
      useSessionPins();
      useSessionPins();
      useSessionPins();
    });
    await flushPromises();
    expect(getPins).toHaveBeenCalledTimes(1);
  });

  test("pin/unpin/toggle mutate pinnedIds", async () => {
    __setPlatformForTests(fakePlatform());
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();
    pins.pin("a");
    expect(pins.isPinned("a")).toBe(true);
    pins.toggle("a");
    expect(pins.isPinned("a")).toBe(false);
    pins.toggle("b");
    expect(pins.isPinned("b")).toBe(true);
    pins.unpin("b");
    expect(pins.isPinned("b")).toBe(false);
  });

  test("pin only invalidates reactive has() subscribers for the touched id", async () => {
    __setPlatformForTests(fakePlatform());
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await pins.ready();

    let aRuns = 0;
    let bRuns = 0;
    let sizeRuns = 0;
    scope.run(() => {
      effect(() => {
        pins.isPinned("a");
        aRuns++;
      });
      effect(() => {
        pins.isPinned("b");
        bRuns++;
      });
      effect(() => {
        pins.pinnedIds.value.size;
        sizeRuns++;
      });
    });

    pins.pin("b");

    expect(aRuns).toBe(1);
    expect(bRuns).toBe(2);
    expect(sizeRuns).toBe(2);
  });

  test("rapid toggles debounce into a single setPins call", async () => {
    const setPins = vi.fn().mockResolvedValue(undefined);
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, setPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();
    pins.pin("a");
    pins.pin("b");
    pins.pin("c");
    pins.unpin("b");
    expect(setPins).not.toHaveBeenCalled();
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setPins).toHaveBeenCalledTimes(1);
    expect(setPins).toHaveBeenCalledWith(["a", "c"]);
  });

  test("survives getPins rejection", async () => {
    const getPins = vi.fn().mockRejectedValue(new Error("boom"));
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();
    expect(pins.isPinned("a")).toBe(false);
  });

  test("rename replaces old id with new id and schedules one persist", async () => {
    const getPins = vi.fn().mockResolvedValue(["a", "b"]);
    const setPins = vi.fn().mockResolvedValue(undefined);
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins, setPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("a", "a2");
    expect(pins.isPinned("a")).toBe(false);
    expect(pins.isPinned("a2")).toBe(true);
    expect(pins.isPinned("b")).toBe(true);
    expect(setPins).not.toHaveBeenCalled();

    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setPins).toHaveBeenCalledTimes(1);
    expect(new Set(setPins.mock.calls[0][0])).toEqual(new Set(["a2", "b"]));
  });

  test("rename is a no-op when old id is not pinned", async () => {
    const getPins = vi.fn().mockResolvedValue(["a"]);
    const setPins = vi.fn().mockResolvedValue(undefined);
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins, setPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("x", "y");
    expect(pins.isPinned("a")).toBe(true);
    expect(pins.isPinned("y")).toBe(false);
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setPins).not.toHaveBeenCalled();
  });

  test("rename(x, x) is a no-op", async () => {
    const getPins = vi.fn().mockResolvedValue(["a"]);
    const setPins = vi.fn().mockResolvedValue(undefined);
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins, setPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("a", "a");
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setPins).not.toHaveBeenCalled();
    expect(pins.isPinned("a")).toBe(true);
  });

  test("rename with empty ids is a no-op", async () => {
    const getPins = vi.fn().mockResolvedValue(["a"]);
    const setPins = vi.fn().mockResolvedValue(undefined);
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins, setPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("", "b");
    pins.rename("a", "");
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setPins).not.toHaveBeenCalled();
    expect(pins.isPinned("a")).toBe(true);
  });

  test("rename onto an already-pinned id is idempotent (Set dedup)", async () => {
    const getPins = vi.fn().mockResolvedValue(["a", "b"]);
    const setPins = vi.fn().mockResolvedValue(undefined);
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins, setPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("a", "b");
    expect(pins.isPinned("a")).toBe(false);
    expect(pins.isPinned("b")).toBe(true);
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setPins).toHaveBeenCalledTimes(1);
    expect(setPins.mock.calls[0][0]).toEqual(["b"]);
  });

  test("flushNow cancels the debounce and persists immediately", async () => {
    const setPins = vi.fn().mockResolvedValue(undefined);
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, setPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.pin("a");
    pins.rename("a", "b");
    expect(setPins).not.toHaveBeenCalled();
    await pins.flushNow();
    expect(setPins).toHaveBeenCalledTimes(1);
    expect(setPins.mock.calls[0][0]).toEqual(["b"]);

    // No stale timer fires after flushNow (would double-persist).
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setPins).toHaveBeenCalledTimes(1);
  });

  test("ready() resolves once the initial load settles, unblocking isPinned", async () => {
    const getPins = vi.fn().mockResolvedValue(["a"]);
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    // No flushPromises before ready() — this is the race fix #1 closes:
    // isPinned would see the empty default here without awaiting ready().
    await pins.ready();
    expect(pins.isPinned("a")).toBe(true);
  });

  test("ready() still resolves (to the empty default) when the load rejects", async () => {
    const getPins = vi.fn().mockRejectedValue(new Error("boom"));
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins },
    } as any);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await pins.ready();
    expect(pins.isPinned("a")).toBe(false);
  });

  test("flushNow warns (but still resolves) when the persist call rejects", async () => {
    const setPins = vi.fn().mockRejectedValue(new Error("disk full"));
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, setPins },
    } as any);
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.pin("a");
    await expect(pins.flushNow()).resolves.toBeUndefined();
    expect(warnSpy).toHaveBeenCalledWith(
      "[pins] flushNow persist failed",
      expect.any(Error),
    );
  });

  test("reload re-reads from platform on demand", async () => {
    const getPins = vi.fn()
      .mockResolvedValueOnce(['a'])
      .mockResolvedValueOnce(['a', 'b'])
    __setPlatformForTests({
      ...fakePlatform(),
      sessions: { ...fakePlatform().sessions, getPins, setPins: vi.fn().mockResolvedValue(undefined) },
    } as any)

    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await pins.ready()
    expect(Array.from(pins.pinnedIds.value)).toEqual(['a'])

    await pins.reload()
    expect(Array.from(pins.pinnedIds.value)).toEqual(['a', 'b'])
    expect(getPins).toHaveBeenCalledTimes(2)
  });

  test("prefs:changed event triggers reload", async () => {
    const getPins = vi.fn()
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce(['new'])
    const events = fakeEventBus()
    __setPlatformForTests({
      ...fakePlatform(),
      events,
      sessions: { ...fakePlatform().sessions, getPins, setPins: vi.fn().mockResolvedValue(undefined) },
    } as any)

    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await pins.ready()
    expect(pins.pinnedIds.value.size).toBe(0)

    events.emit('prefs:changed', undefined)
    await vi.waitFor(() => expect(pins.pinnedIds.value.has('new')).toBe(true))
  });
});
