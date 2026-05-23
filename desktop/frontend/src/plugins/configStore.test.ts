import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { usePluginConfigStore } from "./configStore";
import { __setPlatformForTests } from '../platform'
import { createFakePlatform } from '../platform/__tests__/_fakePlatform'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  __setPlatformForTests(platform)
  setActivePinia(createPinia());
  (platform.pluginHost!.getPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValue(sample as unknown as any);
  (platform.pluginHost!.setPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
})

afterEach(() => {
  __setPlatformForTests(null)
})

const sample = {
  quickInput: {
    enabled: true,
    buttons: [
      { id: "b1", label: "ok", send: "ok", appendNewline: true },
    ],
  },
  fileExplorer: {
    enabled: false,
    panelWidthPx: 380,
    panelCollapsed: true,
    innerTreeRatio: 0.3,
    showHidden: false,
  },
};

describe("usePluginConfigStore", () => {
  it("load() populates cfg from binding", async () => {
    const s = usePluginConfigStore();
    await s.load();
    expect(s.cfg?.quickInput.buttons[0].label).toBe("ok");
  });

  it("save(next) writes via setPluginConfig and updates cfg", async () => {
    const s = usePluginConfigStore();
    await s.load();
    const next = JSON.parse(JSON.stringify(sample));
    next.quickInput.buttons[0].label = "yes";
    await s.save(next);
    expect(platform.pluginHost!.setPluginConfig).toHaveBeenCalledWith(next);
    expect(s.cfg?.quickInput.buttons[0].label).toBe("yes");
  });

  it("isPluginEnabled returns the live enable flag", async () => {
    const s = usePluginConfigStore();
    await s.load();
    expect(s.isPluginEnabled("quick-input")).toBe(true);
    expect(s.isPluginEnabled("file-explorer")).toBe(false);
  });
});
