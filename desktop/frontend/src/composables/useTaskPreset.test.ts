import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { effectScope, nextTick } from "vue";
import { flushPromises } from "@vue/test-utils";
import { useTaskPreset, __resetForTests } from "./useTaskPreset";
import * as api from "../lib/api";

describe("useTaskPreset", () => {
  let scope: ReturnType<typeof effectScope>;
  beforeEach(() => {
    __resetForTests();
    vi.restoreAllMocks();
    scope = effectScope();
  });
  afterEach(() => scope.stop());

  test("loads preset from Wails", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("iconLabel");
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => {
      preset = useTaskPreset();
    });
    await flushPromises();
    await nextTick();
    expect(preset!.activeId.value).toBe("iconLabel");
  });

  test("setPreset writes through Wails and updates activeId", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("iconOnly");
    const setSpy = vi.spyOn(api, "setTaskPreset").mockResolvedValue(undefined);
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => (preset = useTaskPreset()));
    await flushPromises();
    await preset!.setPreset("iconLabel");
    expect(setSpy).toHaveBeenCalledWith("iconLabel");
    expect(preset!.activeId.value).toBe("iconLabel");
  });

  test("falls back to localStorage when bindings missing", async () => {
    vi.spyOn(api, "getTaskPreset").mockRejectedValue(new Error("no bindings"));
    localStorage.setItem("taskPreset", "iconLabel");
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => (preset = useTaskPreset()));
    await flushPromises();
    await nextTick();
    expect(preset!.activeId.value).toBe("iconLabel");
  });

  test("defaults to iconOnly when nothing is stored anywhere", async () => {
    vi.spyOn(api, "getTaskPreset").mockRejectedValue(new Error("no bindings"));
    localStorage.removeItem("taskPreset");
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => (preset = useTaskPreset()));
    await flushPromises();
    await nextTick();
    expect(preset!.activeId.value).toBe("iconOnly");
  });

  test("rejects unknown legacy preset ids and stays on default", async () => {
    // Old "vivid"/"quiet" values left in config should not be applied.
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("vivid");
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => (preset = useTaskPreset()));
    await flushPromises();
    await nextTick();
    expect(preset!.activeId.value).toBe("iconOnly");
  });
});
