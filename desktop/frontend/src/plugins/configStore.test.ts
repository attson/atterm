import { describe, expect, it, vi, beforeEach } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { usePluginConfigStore } from "./configStore";

// Mock Wails bindings.
vi.mock("../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(),
  SetPluginConfig: vi.fn(),
}));

vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => {}),
}));

import { GetPluginConfig, SetPluginConfig } from "../../wailsjs/go/main/App";

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
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.mocked(GetPluginConfig).mockResolvedValue(sample as unknown as any);
    vi.mocked(SetPluginConfig).mockResolvedValue(undefined as unknown as any);
  });

  it("load() populates cfg from binding", async () => {
    const s = usePluginConfigStore();
    await s.load();
    expect(s.cfg?.quickInput.buttons[0].label).toBe("ok");
  });

  it("save(next) writes via SetPluginConfig and updates cfg", async () => {
    const s = usePluginConfigStore();
    await s.load();
    const next = JSON.parse(JSON.stringify(sample));
    next.quickInput.buttons[0].label = "yes";
    await s.save(next);
    expect(SetPluginConfig).toHaveBeenCalledWith(next);
    expect(s.cfg?.quickInput.buttons[0].label).toBe("yes");
  });

  it("isPluginEnabled returns the live enable flag", async () => {
    const s = usePluginConfigStore();
    await s.load();
    expect(s.isPluginEnabled("quick-input")).toBe(true);
    expect(s.isPluginEnabled("file-explorer")).toBe(false);
  });
});
