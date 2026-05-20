import { beforeEach, describe, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";

vi.mock("../../wailsjs/runtime/runtime", () => ({
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn().mockResolvedValue(false),
  Quit: vi.fn(),
}));

import {
  WindowMinimise,
  WindowToggleMaximise,
  Quit,
} from "../../wailsjs/runtime/runtime";
import { setMaximized } from "../composables/useWindowMaximized";
import WindowControls from "./WindowControls.vue";

beforeEach(() => {
  vi.clearAllMocks();
  setMaximized(false);
});

describe("WindowControls", () => {
  it("renders three buttons: minimise, maximise/restore, close", () => {
    const w = mount(WindowControls);
    expect(w.find('[data-testid="window-min"]').exists()).toBe(true);
    expect(w.find('[data-testid="window-max"]').exists()).toBe(true);
    expect(w.find('[data-testid="window-close"]').exists()).toBe(true);
  });

  it("min button calls WindowMinimise", async () => {
    const w = mount(WindowControls);
    await w.get('[data-testid="window-min"]').trigger("click");
    expect(WindowMinimise).toHaveBeenCalledTimes(1);
  });

  it("max button calls WindowToggleMaximise and flips the shared ref", async () => {
    const w = mount(WindowControls);
    await w.get('[data-testid="window-max"]').trigger("click");
    expect(WindowToggleMaximise).toHaveBeenCalledTimes(1);
    await flushPromises();
    expect(w.get('[data-testid="window-max"]').attributes("aria-label")).toBe("Restore");
  });

  it("close button calls Quit", async () => {
    const w = mount(WindowControls);
    await w.get('[data-testid="window-close"]').trigger("click");
    expect(Quit).toHaveBeenCalledTimes(1);
  });

  it("when started maximized, max button starts in restore variant", async () => {
    setMaximized(true);
    const w = mount(WindowControls);
    await flushPromises();
    expect(w.get('[data-testid="window-max"]').attributes("aria-label")).toBe("Restore");
  });

  it("if a runtime call throws, the button does not propagate the error", async () => {
    vi.mocked(WindowMinimise).mockImplementation(() => {
      throw new Error("runtime gone");
    });
    const w = mount(WindowControls);
    await expect(
      w.get('[data-testid="window-min"]').trigger("click"),
    ).resolves.toBeUndefined();
  });
});
