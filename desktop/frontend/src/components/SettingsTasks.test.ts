import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsTasks from "./SettingsTasks.vue";
import * as api from "../lib/api";
import { __resetForTests as resetPreset } from "../composables/useTaskPreset";
import {
  DEFAULT_SESSION_LABEL_COLORS,
  __resetForTests as resetLabelAppearance,
  useSessionLabelAppearance,
} from "../composables/useSessionLabelAppearance";

beforeEach(() => {
  resetPreset();
  resetLabelAppearance();
  localStorage.clear();
  vi.restoreAllMocks();
});
afterEach(() => {
  resetPreset();
  resetLabelAppearance();
  vi.restoreAllMocks();
});

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

  test("offers neutral and category modes with three color controls", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("iconOnly");
    vi.spyOn(api, "getTaskSidebarCollapsed").mockResolvedValue(false);
    const w = mount(SettingsTasks);
    await flushPromises();

    expect(w.findAll('[data-test^="session-label-mode-"]')).toHaveLength(2);
    expect(w.findAll('input[type="color"][data-test^="session-label-color-"]')).toHaveLength(3);
    expect(w.get('[data-test="session-label-preview-ssh"]').text()).toBe("SSH");
  });

  test("neutral mode disables color editing without clearing colors", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("iconOnly");
    vi.spyOn(api, "getTaskSidebarCollapsed").mockResolvedValue(false);
    const w = mount(SettingsTasks);
    await flushPromises();

    await w.get('[data-test="session-label-color-ssh"]').setValue("#112233");
    await w.get('[data-test="session-label-mode-neutral"]').trigger("click");
    expect(w.get('[data-test="session-label-color-ssh"]').attributes("disabled")).toBeDefined();
    expect(useSessionLabelAppearance().colors.value.ssh).toBe("#112233");
    await w.get('[data-test="session-label-mode-category"]').trigger("click");
    expect(w.get('[data-test="session-label-color-ssh"]').attributes("disabled")).toBeUndefined();
    expect(w.get('[data-test="session-label-preview-ssh"]').attributes("style")).toContain("#112233");
  });

  test("supports per-color and reset-all actions", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("iconOnly");
    vi.spyOn(api, "getTaskSidebarCollapsed").mockResolvedValue(false);
    const w = mount(SettingsTasks);
    await flushPromises();

    await w.get('[data-test="session-label-color-ssh"]').setValue("#112233");
    await w.get('[data-test="session-label-color-codex"]').setValue("#445566");
    await w.get('[data-test="session-label-reset-ssh"]').trigger("click");
    expect(useSessionLabelAppearance().colors.value.ssh).toBe(DEFAULT_SESSION_LABEL_COLORS.ssh);
    expect(useSessionLabelAppearance().colors.value.codex).toBe("#445566");
    await w.get('[data-test="session-label-reset-all"]').trigger("click");
    expect(useSessionLabelAppearance().colors.value).toEqual(DEFAULT_SESSION_LABEL_COLORS);
  });

  test("rolls the color input back and shows an error when saving fails", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("iconOnly");
    vi.spyOn(api, "getTaskSidebarCollapsed").mockResolvedValue(false);
    const w = mount(SettingsTasks);
    await flushPromises();
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });

    const input = w.get<HTMLInputElement>('[data-test="session-label-color-ssh"]');
    await input.setValue("#112233");
    expect(input.element.value.toUpperCase()).toBe(DEFAULT_SESSION_LABEL_COLORS.ssh);
    expect(useSessionLabelAppearance().colors.value.ssh).toBe(DEFAULT_SESSION_LABEL_COLORS.ssh);
    expect(w.get('[role="alert"]').text()).not.toBe("");
  });
});
