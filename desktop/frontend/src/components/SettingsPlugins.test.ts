import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import SettingsPlugins from "./SettingsPlugins.vue";
import { PLUGINS } from "../plugins/registry";

vi.mock("../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(),
  SetPluginConfig: vi.fn(),
}));
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => undefined),
}));

import { GetPluginConfig, SetPluginConfig } from "../../wailsjs/go/main/App";

beforeEach(() => {
  setActivePinia(createPinia());
  PLUGINS.length = 0;
  PLUGINS.push({
    id: "quick-input",
    slot: "bottom-toolbar",
    title: "Quick Input",
    description: "x",
    load: () => Promise.reject(new Error("not used")),
    defaultEnabled: true,
  });
  vi.mocked(GetPluginConfig).mockResolvedValue({
    quickInput: { enabled: false, buttons: [] },
    fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
  } as any);
  vi.mocked(SetPluginConfig).mockResolvedValue(undefined as unknown as any);
});

describe("SettingsPlugins", () => {
  it("lists registered plugins with checkboxes reflecting enable state", async () => {
    const w = mount(SettingsPlugins);
    await flushPromises();
    const cb = w.find<HTMLInputElement>("input[type=checkbox]").element;
    expect(cb.checked).toBe(false);
  });

  it("calls SetPluginConfig on toggle", async () => {
    const w = mount(SettingsPlugins);
    await flushPromises();
    await w.find("input[type=checkbox]").setValue(true);
    await flushPromises();
    expect(SetPluginConfig).toHaveBeenCalled();
  });
});
