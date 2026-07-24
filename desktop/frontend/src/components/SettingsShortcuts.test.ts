import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import SettingsShortcuts from "./SettingsShortcuts.vue";
import { usePluginConfigStore } from "../plugins/configStore";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;

beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  // Override the default getPluginConfig with the test-specific shape.
  (platform.pluginHost!.getPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
    fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: false, innerTreeRatio: 0.3, showHidden: false, showLineNumbers: false },
    translate: { enabled: false, provider: "openai-compatible", baseUrl: "", apiKey: "", model: "gpt-4o-mini", defaultTargetLang: "zh-CN" },
    shortcuts: { bindings: {} },
  });
  __setPlatformForTests(platform);
});

afterEach(() => {
  __setPlatformForTests(null);
});

async function setupStore(initial: Record<string, string>) {
  setActivePinia(createPinia());
  const store = usePluginConfigStore();
  await store.load();
  store.cfg!.shortcuts = { bindings: initial };
  return store;
}

describe("SettingsShortcuts", () => {
  it("renders 14 rows grouped under 'pane', 'tab', and 'sidebar'", async () => {
    await setupStore({});
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    const rows = wrapper.findAll(".shortcut-row");
    expect(rows).toHaveLength(14);
    expect(wrapper.text()).toContain("Pane");
    expect(wrapper.text()).toContain("Tab");
    expect(wrapper.text()).toContain("Sidebar");
  });

  it("each row's hotkey cell shows the current binding (default when not overridden)", async () => {
    await setupStore({});
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    expect(wrapper.text()).toContain("Mod+KeyN");
    expect(wrapper.text()).toContain("Mod+KeyT");
  });

  it("editing a row to collide with another shows a conflict notice and disables Save", async () => {
    await setupStore({});
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    // Simulate emitting update on the first row's HotkeyCaptureCell — easier
    // to drive the data flow than to fake a real capture.
    const cells = wrapper.findAllComponents({ name: "HotkeyCaptureCell" });
    await cells[0].vm.$emit("update", "Mod+KeyT"); // collides with tab.new (default)
    await flushPromises();
    expect(wrapper.text().toLowerCase()).toContain("conflicts with");
    const saveBtn = wrapper.find("button.save");
    expect((saveBtn.element as HTMLButtonElement).disabled).toBe(true);
  });

  it("Reset all marks draft empty (defaults shown) and sets dirty", async () => {
    const store = await setupStore({ "pane.close": "Mod+KeyL" });
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    // Before reset, pane.close shows the override
    expect(wrapper.text()).toContain("Mod+KeyL");
    await wrapper.find("button.reset-all").trigger("click");
    await flushPromises();
    // Now pane.close shows the default
    expect(wrapper.text()).toContain("Mod+KeyW");
    const saveBtn = wrapper.find("button.save");
    expect((saveBtn.element as HTMLButtonElement).disabled).toBe(false);
    void store;
  });

  it("Save calls store.save with only the modified entries (defaults stripped)", async () => {
    const store = await setupStore({});
    const saveSpy = vi.spyOn(store, "save").mockResolvedValue();
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    const cells = wrapper.findAllComponents({ name: "HotkeyCaptureCell" });
    // Index 9 is tab.new (after pane.* 0..8). Change it to a non-default.
    await cells[9].vm.$emit("update", "Mod+KeyP");
    await flushPromises();
    await wrapper.find("button.save").trigger("click");
    await flushPromises();
    expect(saveSpy).toHaveBeenCalledTimes(1);
    const payload = saveSpy.mock.calls[0][0];
    expect(payload.shortcuts.bindings).toEqual({ "tab.new": "Mod+KeyP" });
  });

  it("Discard reverts draft to current store value, clears dirty", async () => {
    await setupStore({ "tab.new": "Mod+KeyP" });
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    const cells = wrapper.findAllComponents({ name: "HotkeyCaptureCell" });
    await cells[9].vm.$emit("update", "Mod+KeyQ");
    await flushPromises();
    expect(wrapper.text()).toContain("Mod+KeyQ");
    await wrapper.find("button.discard").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("Mod+KeyP");
    expect(wrapper.text()).not.toContain("Mod+KeyQ");
  });
});
