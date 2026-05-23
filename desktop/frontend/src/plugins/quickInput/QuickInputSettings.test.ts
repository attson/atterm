import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import QuickInputSettings from "./QuickInputSettings.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;

const initial = () => ({
  quickInput: {
    enabled: true,
    buttons: [
      { id: "a", label: "ok", send: "ok", appendNewline: true },
    ],
  },
  fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
});

beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);

  setActivePinia(createPinia());
  (platform.pluginHost!.getPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValue(initial() as any);
  (platform.pluginHost!.setPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValue(undefined as unknown as any);
});

afterEach(() => {
  __setPlatformForTests(null);
});

describe("QuickInputSettings", () => {
  it("starts non-dirty and shows existing buttons", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    expect(w.findAll("tr.button-row")).toHaveLength(1);
    expect((w.find<HTMLButtonElement>("button.save").element as HTMLButtonElement).disabled).toBe(true);
  });

  it("editing label marks dirty and enables Save", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    await w.find("input.label").setValue("yes");
    expect((w.find<HTMLButtonElement>("button.save").element as HTMLButtonElement).disabled).toBe(false);
  });

  it("clicking Add appends a button and goes dirty", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    await w.find("button.add").trigger("click");
    expect(w.findAll("tr.button-row")).toHaveLength(2);
  });

  it("clicking Delete removes the row", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    await w.find("button.delete").trigger("click");
    expect(w.findAll("tr.button-row")).toHaveLength(0);
  });

  it("Save calls setPluginConfig with edited buttons", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    await w.find("input.label").setValue("yes");
    await w.find("button.save").trigger("click");
    await flushPromises();
    expect(platform.pluginHost!.setPluginConfig).toHaveBeenCalled();
    const arg = (platform.pluginHost!.setPluginConfig as ReturnType<typeof vi.fn>).mock.calls[0][0] as any;
    expect(arg.quickInput.buttons[0].label).toBe("yes");
  });

  it("rejects save when a hotkey conflicts", async () => {
    const w = mount(QuickInputSettings);
    await flushPromises();
    await w.find("button.add").trigger("click");
    const hotkeys = w.findAll("input.hotkey");
    await hotkeys[0].setValue("Alt+1");
    await hotkeys[1].setValue("Alt+1");
    await w.find("button.save").trigger("click");
    await flushPromises();
    expect(platform.pluginHost!.setPluginConfig).not.toHaveBeenCalled();
    expect(w.text()).toContain("conflict");
  });
});
