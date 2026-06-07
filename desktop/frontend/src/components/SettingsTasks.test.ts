import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsTasks from "./SettingsTasks.vue";
import * as api from "../lib/api";
import { __resetForTests as resetPreset } from "../composables/useTaskPreset";

beforeEach(() => {
  resetPreset();
  vi.restoreAllMocks();
});
afterEach(() => resetPreset());

describe("SettingsTasks", () => {
  test("loads current preset and renders two radio options", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("iconOnly");
    vi.spyOn(api, "getTaskSidebarCollapsed").mockResolvedValue(false);
    const w = mount(SettingsTasks);
    await flushPromises();
    expect(w.findAll('input[type="radio"][name="preset"]').length).toBe(2);
  });

  test("selecting Icon+label calls setTaskPreset", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("iconOnly");
    vi.spyOn(api, "getTaskSidebarCollapsed").mockResolvedValue(false);
    const set = vi.spyOn(api, "setTaskPreset").mockResolvedValue(undefined);
    const w = mount(SettingsTasks);
    await flushPromises();
    await w.find('input[value="iconLabel"]').setValue(true);
    expect(set).toHaveBeenCalledWith("iconLabel");
  });

  test("toggling 'expand by default' calls setTaskSidebarCollapsed(true)", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("iconOnly");
    vi.spyOn(api, "getTaskSidebarCollapsed").mockResolvedValue(false);
    const set = vi.spyOn(api, "setTaskSidebarCollapsed").mockResolvedValue(undefined);
    const w = mount(SettingsTasks);
    await flushPromises();
    // Uncheck the "Expand by default" checkbox → collapsed = true
    await w.find('input[type="checkbox"]').setValue(false);
    expect(set).toHaveBeenCalledWith(true);
  });
});
