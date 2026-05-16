import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import QuickInputBar from "./QuickInputBar.vue";

vi.mock("../../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(),
  SetPluginConfig: vi.fn(),
}));
vi.mock("../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => undefined),
}));

import { GetPluginConfig } from "../../../wailsjs/go/main/App";

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
  setActivePinia(createPinia());
  vi.mocked(GetPluginConfig).mockResolvedValue({
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

describe("QuickInputBar", () => {
  it("renders one button per config entry", async () => {
    const ctx = makeContext();
    const w = mount(QuickInputBar, { props: { context: ctx as any } });
    await flushPromises();
    expect(w.findAll("button.quick-input-btn")).toHaveLength(2);
  });

  it("clicking sends text with newline when appendNewline=true", async () => {
    const ctx = makeContext();
    const w = mount(QuickInputBar, { props: { context: ctx as any } });
    await flushPromises();
    await w.findAll("button.quick-input-btn")[0].trigger("click");
    expect(ctx.send).toHaveBeenCalledWith("ok\n");
  });

  it("clicking sends raw text when appendNewline=false", async () => {
    const ctx = makeContext();
    const w = mount(QuickInputBar, { props: { context: ctx as any } });
    await flushPromises();
    await w.findAll("button.quick-input-btn")[1].trigger("click");
    expect(ctx.send).toHaveBeenCalledWith("raw");
  });
});
