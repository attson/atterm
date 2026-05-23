import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import QuickInputBar from "./QuickInputBar.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;

function makeContext() {
  return {
    activePane: { value: null },
    activeSessionId: { value: null },
    activeEndpoint: { value: null },
    activeCwd: { value: null },
    terminalThemeId: { value: "classic" },
    send: vi.fn(),
    showToast: vi.fn(),
  };
}

beforeEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);

  setActivePinia(createPinia());
  (platform.pluginHost!.getPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
    quickInput: {
      enabled: true,
      buttons: [
        { id: "a", label: "ok", send: "ok", appendNewline: true },
        { id: "b", label: "raw", send: "raw", appendNewline: false },
      ],
    },
    fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: true, innerTreeRatio: 0.3, showHidden: false },
  } as any);
});

afterEach(() => {
  vi.useRealTimers();
  __setPlatformForTests(null);
});

describe("QuickInputBar", () => {
  it("renders one button per config entry", async () => {
    const ctx = makeContext();
    const w = mount(QuickInputBar, { props: { context: ctx as any } });
    await flushPromises();
    expect(w.findAll("button.quick-input-btn")).toHaveLength(2);
  });

  it("clicking sends text first, then a standalone Enter key when appendNewline=true", async () => {
    vi.useFakeTimers();
    const ctx = makeContext();
    const w = mount(QuickInputBar, { props: { context: ctx as any } });
    await flushPromises();
    await w.findAll("button.quick-input-btn")[0].trigger("click");
    expect(ctx.send).toHaveBeenCalledTimes(1);
    expect(ctx.send).toHaveBeenLastCalledWith("ok");

    vi.runOnlyPendingTimers();
    expect(ctx.send).toHaveBeenCalledTimes(2);
    expect(ctx.send).toHaveBeenLastCalledWith("\r");
  });

  it("clicking sends raw text when appendNewline=false", async () => {
    const ctx = makeContext();
    const w = mount(QuickInputBar, { props: { context: ctx as any } });
    await flushPromises();
    await w.findAll("button.quick-input-btn")[1].trigger("click");
    expect(ctx.send).toHaveBeenCalledWith("raw");
  });
});
