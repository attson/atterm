import { beforeEach, describe, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";

vi.mock("../../wailsjs/runtime/runtime", () => ({
  Environment: vi.fn(),
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn().mockResolvedValue(false),
  Quit: vi.fn(),
}));

import { Environment, WindowToggleMaximise } from "../../wailsjs/runtime/runtime";
import TitleBar from "./TitleBar.vue";
import titleBarSource from "./TitleBar.vue?raw";
import windowControlsSource from "./WindowControls.vue?raw";

const baseProps = {
  status: "ready" as const,
  errorMsg: "",
  sessionCount: 0,
  remoteEndpoint: null,
  availableRemoteCount: 0,
  updateBadge: false,
};

beforeEach(() => {
  vi.clearAllMocks();
});

async function mountForPlatform(platform: string, props = {}) {
  vi.mocked(Environment).mockResolvedValue({
    platform,
    arch: "x64",
    buildType: "dev",
  });
  const w = mount(TitleBar, { props: { ...baseProps, ...props } });
  await flushPromises();
  return w;
}

describe("TitleBar platform variants", () => {
  it("on darwin, root has padding-left: 80px and no WindowControls", async () => {
    const w = await mountForPlatform("darwin");
    expect(w.get('[data-testid="titlebar-root"]').attributes("style")).toContain(
      "padding-left: 80px",
    );
    expect(w.find('[data-testid="window-min"]').exists()).toBe(false);
  });

  it("on windows, renders WindowControls and no left padding", async () => {
    const w = await mountForPlatform("windows");
    const style = w.get('[data-testid="titlebar-root"]').attributes("style") ?? "";
    expect(style).not.toContain("padding-left: 80px");
    expect(w.find('[data-testid="window-min"]').exists()).toBe(true);
    expect(w.find('[data-testid="window-max"]').exists()).toBe(true);
    expect(w.find('[data-testid="window-close"]').exists()).toBe(true);
  });

  it("on linux, renders WindowControls and no left padding", async () => {
    const w = await mountForPlatform("linux");
    const style = w.get('[data-testid="titlebar-root"]').attributes("style") ?? "";
    expect(style).not.toContain("padding-left: 80px");
    expect(w.find('[data-testid="window-min"]').exists()).toBe(true);
  });

  it("falls back to linux rendering if Environment rejects", async () => {
    vi.mocked(Environment).mockRejectedValue(new Error("no runtime"));
    const w = mount(TitleBar, { props: baseProps });
    await flushPromises();
    expect(w.find('[data-testid="window-min"]').exists()).toBe(true);
  });
});

describe("TitleBar status rendering", () => {
  it("status=loading renders 'starting…'", async () => {
    const w = await mountForPlatform("darwin", { status: "loading" });
    expect(w.text()).toContain("starting");
  });

  it("status=error renders errorMsg in bad span", async () => {
    const w = await mountForPlatform("darwin", { status: "error", errorMsg: "boom" });
    expect(w.find(".bad").text()).toBe("boom");
  });

  it("renders '3 sessions' (plural) for sessionCount=3", async () => {
    const w = await mountForPlatform("darwin", { sessionCount: 3 });
    expect(w.text()).toContain("3 sessions");
  });

  it("renders '1 session' (singular) for sessionCount=1", async () => {
    const w = await mountForPlatform("darwin", { sessionCount: 1 });
    expect(w.text()).toContain("1 session");
    expect(w.text()).not.toContain("1 sessions");
  });

  it("renders '· uplink on' when remoteEndpoint is truthy", async () => {
    const w = await mountForPlatform("darwin", {
      remoteEndpoint: { url: "wss://x", token: "t" },
    });
    expect(w.text()).toContain("uplink on");
  });
});

describe("TitleBar buttons", () => {
  it("remote button is disabled when remoteEndpoint is null", async () => {
    const w = await mountForPlatform("darwin", { remoteEndpoint: null });
    const btn = w.get('[data-testid="titlebar-remote"]');
    expect((btn.element as HTMLButtonElement).disabled).toBe(true);
  });

  it("remote button emits open-remote when clicked", async () => {
    const w = await mountForPlatform("darwin", {
      remoteEndpoint: { url: "wss://x", token: "t" },
    });
    await w.get('[data-testid="titlebar-remote"]').trigger("click");
    expect(w.emitted("open-remote")).toBeTruthy();
  });

  it("settings button emits open-settings when clicked", async () => {
    const w = await mountForPlatform("darwin");
    await w.get('[data-testid="titlebar-settings"]').trigger("click");
    expect(w.emitted("open-settings")).toBeTruthy();
  });

  it("renders availableRemoteCount badge when > 0", async () => {
    const w = await mountForPlatform("darwin", {
      remoteEndpoint: { url: "wss://x", token: "t" },
      availableRemoteCount: 4,
    });
    expect(w.find(".badge").text()).toBe("4");
  });

  it("renders update dot when updateBadge=true", async () => {
    const w = await mountForPlatform("darwin", { updateBadge: true });
    expect(w.find(".dot").exists()).toBe(true);
  });
});

describe("TitleBar drag region (frameless windows)", () => {
  // Wails v2 uses --wails-draggable on Linux/Windows frameless webviews
  // (-webkit-app-region is the Electron/Chromium property; Wails' webkit2gtk
  // and WebView2 do not honor it). Mac TitleBarHiddenInset has native drag
  // and ignores both. These guards keep us from regressing back to the
  // Electron-style property.
  it("titlebar root uses --wails-draggable: drag", () => {
    expect(titleBarSource).toContain("--wails-draggable: drag");
  });

  it("titlebar source has no -webkit-app-region CSS rule", () => {
    // Match the CSS-rule form (property followed by colon), so the
    // explanatory comment block above (which mentions the name without a
    // colon) doesn't trip this guard.
    expect(titleBarSource).not.toMatch(/-webkit-app-region\s*:/);
  });

  it("WindowControls uses --wails-draggable: no-drag", () => {
    expect(windowControlsSource).toContain("--wails-draggable: no-drag");
  });

  it("WindowControls source has no -webkit-app-region CSS rule", () => {
    expect(windowControlsSource).not.toMatch(/-webkit-app-region\s*:/);
  });
});

describe("TitleBar double-click maximize (Win/Linux only)", () => {
  it("on windows, double-click on root calls WindowToggleMaximise", async () => {
    const w = await mountForPlatform("windows");
    await w.get('[data-testid="titlebar-root"]').trigger("dblclick");
    expect(WindowToggleMaximise).toHaveBeenCalledTimes(1);
  });

  it("on darwin, double-click on root calls WindowToggleMaximise (system zoom doesn't fire under TitleBarHiddenInset)", async () => {
    const w = await mountForPlatform("darwin");
    await w.get('[data-testid="titlebar-root"]').trigger("dblclick");
    expect(WindowToggleMaximise).toHaveBeenCalledTimes(1);
  });
});
