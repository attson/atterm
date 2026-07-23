import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { effectScope, nextTick } from "vue";
import { flushPromises } from "@vue/test-utils";
import { useSessionPins, __resetForTests } from "./useSessionPins";
import * as api from "../lib/api";

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
  });

  test("loads pinned ids from Wails on first use", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a", "b"]);
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
    const spy = vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue([]);
    scope.run(() => {
      useSessionPins();
      useSessionPins();
      useSessionPins();
    });
    await flushPromises();
    expect(spy).toHaveBeenCalledTimes(1);
  });

  test("pin/unpin/toggle mutate pinnedIds", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue([]);
    vi.spyOn(api, "setPinnedSessionIds").mockResolvedValue(undefined);
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

  test("rapid toggles debounce into a single setPinnedSessionIds call", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue([]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();
    pins.pin("a");
    pins.pin("b");
    pins.pin("c");
    pins.unpin("b");
    expect(setSpy).not.toHaveBeenCalled();
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).toHaveBeenCalledTimes(1);
    expect(setSpy).toHaveBeenCalledWith(["a", "c"]);
  });

  test("survives getPinnedSessionIds rejection", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockRejectedValue(new Error("boom"));
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();
    expect(pins.isPinned("a")).toBe(false);
  });

  test("rename replaces old id with new id and schedules one persist", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a", "b"]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("a", "a2");
    expect(pins.isPinned("a")).toBe(false);
    expect(pins.isPinned("a2")).toBe(true);
    expect(pins.isPinned("b")).toBe(true);
    expect(setSpy).not.toHaveBeenCalled();

    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).toHaveBeenCalledTimes(1);
    expect(new Set(setSpy.mock.calls[0][0])).toEqual(new Set(["a2", "b"]));
  });

  test("rename is a no-op when old id is not pinned", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a"]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
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
    expect(setSpy).not.toHaveBeenCalled();
  });

  test("rename(x, x) is a no-op", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a"]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("a", "a");
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).not.toHaveBeenCalled();
    expect(pins.isPinned("a")).toBe(true);
  });

  test("rename with empty ids is a no-op", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a"]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("", "b");
    pins.rename("a", "");
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).not.toHaveBeenCalled();
    expect(pins.isPinned("a")).toBe(true);
  });

  test("rename onto an already-pinned id is idempotent (Set dedup)", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a", "b"]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
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
    expect(setSpy).toHaveBeenCalledTimes(1);
    expect(setSpy.mock.calls[0][0]).toEqual(["b"]);
  });

  test("flushNow cancels the debounce and persists immediately", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue([]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.pin("a");
    pins.rename("a", "b");
    expect(setSpy).not.toHaveBeenCalled();
    await pins.flushNow();
    expect(setSpy).toHaveBeenCalledTimes(1);
    expect(setSpy.mock.calls[0][0]).toEqual(["b"]);

    // No stale timer fires after flushNow (would double-persist).
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).toHaveBeenCalledTimes(1);
  });
});
