import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushPromises } from "@vue/test-utils";

vi.mock("../../wailsjs/runtime/runtime", () => ({
  WindowIsMaximised: vi.fn(),
}));

import { WindowIsMaximised } from "../../wailsjs/runtime/runtime";

describe("useWindowMaximized", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.resetModules();
  });

  it("returns the same ref instance on every call (module-level singleton)", async () => {
    vi.mocked(WindowIsMaximised).mockResolvedValue(false);
    const { useWindowMaximized } = await import("./useWindowMaximized");
    const a = useWindowMaximized();
    const b = useWindowMaximized();
    expect(a).toBe(b);
  });

  it("initializes ref from WindowIsMaximised()", async () => {
    vi.mocked(WindowIsMaximised).mockResolvedValue(true);
    const { useWindowMaximized } = await import("./useWindowMaximized");
    const r = useWindowMaximized();
    await flushPromises();
    expect(r.value).toBe(true);
  });

  it("defaults to false when WindowIsMaximised throws", async () => {
    vi.mocked(WindowIsMaximised).mockRejectedValue(new Error("nope"));
    const { useWindowMaximized } = await import("./useWindowMaximized");
    const r = useWindowMaximized();
    await flushPromises();
    expect(r.value).toBe(false);
  });

  it("setMaximized flips the shared ref", async () => {
    vi.mocked(WindowIsMaximised).mockResolvedValue(false);
    const { useWindowMaximized, setMaximized } = await import("./useWindowMaximized");
    const r = useWindowMaximized();
    setMaximized(true);
    expect(r.value).toBe(true);
    setMaximized(false);
    expect(r.value).toBe(false);
  });
});
