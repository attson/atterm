import { describe, it, expect, beforeEach, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import ShortcutHints from "./ShortcutHints.vue";
import { usePluginConfigStore } from "../plugins/configStore";

vi.mock("../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(async () => ({
    quickInput: { enabled: true, buttons: [] },
    fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: false, innerTreeRatio: 0.3, showHidden: false, showLineNumbers: false },
    translate: { enabled: false, provider: "openai-compatible", baseUrl: "", apiKey: "", model: "gpt-4o-mini", defaultTargetLang: "zh-CN" },
    shortcuts: { bindings: {} },
  })),
  SetPluginConfig: vi.fn(async () => {}),
}));

vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => {}),
}));

async function setupStore(initial: Record<string, string>) {
  setActivePinia(createPinia());
  const store = usePluginConfigStore();
  await store.load();
  store.cfg!.shortcuts = { bindings: initial };
  return store;
}

function fireKey(type: "keydown" | "keyup", init: KeyboardEventInit & { key: string }) {
  document.dispatchEvent(new KeyboardEvent(type, { ...init, bubbles: true, cancelable: true }));
}

describe("ShortcutHints", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  it("renders nothing initially (overlay hidden)", async () => {
    await setupStore({});
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100 } });
    await flushPromises();
    expect(wrapper.find(".hints-backdrop").exists()).toBe(false);
  });

  it("after 100ms long-press of Control, shows 12 rows in 2 groups with Ctrl+* chords", async () => {
    await setupStore({});
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100 }, attachTo: document.body });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.find(".hints-backdrop").exists()).toBe(true);
    const rows = wrapper.findAll(".hint-row");
    expect(rows).toHaveLength(12);
    expect(wrapper.text()).toContain("Pane");
    expect(wrapper.text()).toContain("Tab");
    expect(wrapper.text()).toContain("Ctrl+N");
    expect(wrapper.text()).toContain("Ctrl+T");
    wrapper.unmount();
  });

  it("mac variant uses ⌘ symbols", async () => {
    await setupStore({});
    const wrapper = mount(ShortcutHints, { props: { mod: "Meta", thresholdMs: 100 }, attachTo: document.body });
    await flushPromises();
    fireKey("keydown", { key: "Meta", metaKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.text()).toContain("⌘N");
    expect(wrapper.text()).toContain("⌘T");
    wrapper.unmount();
  });

  it("releasing the mod hides the overlay", async () => {
    await setupStore({});
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100 }, attachTo: document.body });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.find(".hints-backdrop").exists()).toBe(true);
    fireKey("keyup", { key: "Control" });
    await flushPromises();
    expect(wrapper.find(".hints-backdrop").exists()).toBe(false);
    wrapper.unmount();
  });

  it("disabled action renders em-dash and dimmed class", async () => {
    await setupStore({ "pane.close": "" });
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100 }, attachTo: document.body });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    const disabledRow = wrapper.findAll(".hint-row").find((r) => r.text().includes("Close pane"));
    expect(disabledRow).toBeTruthy();
    expect(disabledRow!.classes()).toContain("disabled");
    expect(disabledRow!.text()).toContain("—");
    wrapper.unmount();
  });

  it("user-overridden binding is rendered using formatChord", async () => {
    await setupStore({ "tab.new": "Mod+KeyL" });
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100 }, attachTo: document.body });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.text()).toContain("Ctrl+L");
    expect(wrapper.text()).not.toContain("Ctrl+T");
    wrapper.unmount();
  });
});
