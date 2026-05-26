import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import SettingsPlugins from "./SettingsPlugins.vue";
import { PLUGINS } from "../plugins/registry";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;

beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);

  setActivePinia(createPinia());
  PLUGINS.length = 0;
  PLUGINS.push({
    id: "quick-input",
    slot: "bottom-toolbar",
    titleKey: "plugins.quickInput.title",
    descriptionKey: "plugins.quickInput.description",
    load: () => Promise.reject(new Error("not used")),
    defaultEnabled: true,
  });
  (platform.pluginHost!.getPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
    quickInput: { enabled: false, buttons: [] },
    fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
  } as any);
  (platform.pluginHost!.setPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValue(undefined as unknown as any);
});

afterEach(() => {
  __setPlatformForTests(null);
});

describe("SettingsPlugins", () => {
  it("lists registered plugins with checkboxes reflecting enable state", async () => {
    const w = mount(SettingsPlugins);
    await flushPromises();
    const cb = w.find<HTMLInputElement>("input[type=checkbox]").element;
    expect(cb.checked).toBe(false);
  });

  it("calls setPluginConfig on toggle", async () => {
    const w = mount(SettingsPlugins);
    await flushPromises();
    await w.find("input[type=checkbox]").setValue(true);
    await flushPromises();
    expect(platform.pluginHost!.setPluginConfig).toHaveBeenCalled();
  });
});
