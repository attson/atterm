import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h } from "vue";
import PluginHost from "./PluginHost.vue";
import { PLUGINS } from "./registry";

vi.mock("../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(),
  SetPluginConfig: vi.fn(),
}));
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => undefined),
}));

import { GetPluginConfig } from "../../wailsjs/go/main/App";

const fakeContext = {
  activePane: { value: null },
  activeSessionId: { value: null },
  activeEndpoint: { value: null },
  activeCwd: { value: null },
  terminalThemeId: { value: "classic" },
  send: vi.fn(),
  showToast: vi.fn(),
} as any;

const DummyPlugin = defineComponent({
  name: "DummyPlugin",
  setup() {
    return () => h("div", { class: "dummy" }, "dummy");
  },
});

describe("PluginHost", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    PLUGINS.length = 0;
    PLUGINS.push({
      id: "quick-input",
      slot: "bottom-toolbar",
      title: "Test",
      description: "test",
      load: () => Promise.resolve({ default: DummyPlugin }),
      defaultEnabled: true,
    });
    vi.mocked(GetPluginConfig).mockResolvedValue({
      quickInput: { enabled: true, buttons: [] },
      fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
    } as any);
  });

  it("loads and mounts enabled plugin matching slot", async () => {
    const w = mount(PluginHost, {
      props: { slotId: "bottom-toolbar", context: fakeContext },
    });
    await flushPromises();
    await flushPromises();
    expect(w.find(".dummy").exists()).toBe(true);
  });

  it("does not load when plugin is disabled", async () => {
    vi.mocked(GetPluginConfig).mockResolvedValue({
      quickInput: { enabled: false, buttons: [] },
      fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
    } as any);
    const w = mount(PluginHost, {
      props: { slotId: "bottom-toolbar", context: fakeContext },
    });
    await flushPromises();
    expect(w.find(".dummy").exists()).toBe(false);
  });

  it("falls back to disabled when load() rejects", async () => {
    PLUGINS[0].load = () => Promise.reject(new Error("boom"));
    const w = mount(PluginHost, {
      props: { slotId: "bottom-toolbar", context: fakeContext },
    });
    await flushPromises();
    await flushPromises();
    expect(w.find(".dummy").exists()).toBe(false);
    expect(fakeContext.showToast).toHaveBeenCalled();
  });
});
