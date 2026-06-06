import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { effectScope, nextTick } from "vue";
import { flushPromises } from "@vue/test-utils";
import { useTaskPreset, __resetForTests } from "./useTaskPreset";
import * as api from "../lib/api";

describe("useTaskPreset", () => {
  let scope: ReturnType<typeof effectScope>;
  beforeEach(() => {
    __resetForTests();
    document.documentElement.dataset.taskPreset = undefined;
    vi.restoreAllMocks();
    scope = effectScope();
  });
  afterEach(() => scope.stop());

  test("loads preset from Wails and writes html dataset", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("quiet");
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => {
      preset = useTaskPreset();
    });
    await flushPromises();
    await nextTick();
    expect(preset!.activeId.value).toBe("quiet");
    expect(document.documentElement.dataset.taskPreset).toBe("quiet");
  });

  test("setPreset writes through Wails and updates dataset", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("vivid");
    const setSpy = vi.spyOn(api, "setTaskPreset").mockResolvedValue(undefined);
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => (preset = useTaskPreset()));
    await flushPromises();
    await preset!.setPreset("quiet");
    expect(setSpy).toHaveBeenCalledWith("quiet");
    expect(preset!.activeId.value).toBe("quiet");
    expect(document.documentElement.dataset.taskPreset).toBe("quiet");
  });

  test("falls back to localStorage when bindings missing", async () => {
    vi.spyOn(api, "getTaskPreset").mockRejectedValue(new Error("no bindings"));
    localStorage.setItem("taskPreset", "quiet");
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => (preset = useTaskPreset()));
    await flushPromises();
    await nextTick();
    expect(preset!.activeId.value).toBe("quiet");
  });

  test("defaults to vivid when nothing is stored anywhere", async () => {
    vi.spyOn(api, "getTaskPreset").mockRejectedValue(new Error("no bindings"));
    localStorage.removeItem("taskPreset");
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => (preset = useTaskPreset()));
    await flushPromises();
    await nextTick();
    expect(preset!.activeId.value).toBe("vivid");
  });
});
