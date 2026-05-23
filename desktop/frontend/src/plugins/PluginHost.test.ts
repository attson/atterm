import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h } from "vue";
import PluginHost from "./PluginHost.vue";
import { PLUGINS } from "./registry";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;

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
    vi.clearAllMocks();
    platform = createFakePlatform();
    __setPlatformForTests(platform);

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
    (platform.pluginHost!.getPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      quickInput: { enabled: true, buttons: [] },
      fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
    } as any);
  });

  afterEach(() => {
    __setPlatformForTests(null);
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
    (platform.pluginHost!.getPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
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
