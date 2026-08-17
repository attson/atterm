import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ShortcutHints from "./ShortcutHints.vue";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";

beforeEach(() => {
  vi.clearAllMocks();
  __setPlatformForTests(createFakePlatform());
});

afterEach(() => {
  __setPlatformForTests(null);
});

function fireKey(type: "keydown" | "keyup", init: KeyboardEventInit & { key: string }) {
  document.dispatchEvent(new KeyboardEvent(type, { ...init, bubbles: true, cancelable: true }));
}

describe("ShortcutHints", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  it("renders nothing initially (overlay hidden)", async () => {
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100, bindings: {} } });
    await flushPromises();
    expect(wrapper.find(".hints-backdrop").exists()).toBe(false);
  });

  it("after 100ms long-press of Control, shows 11 rows in 2 groups with Ctrl+* chords", async () => {
    const wrapper = mount(ShortcutHints, {
      props: { mod: "Control", thresholdMs: 100, bindings: {} },
      attachTo: document.body,
    });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.find(".hints-backdrop").exists()).toBe(true);
    const rows = wrapper.findAll(".hint-row");
    expect(rows).toHaveLength(11);
    expect(wrapper.text()).toContain("Pane");
    expect(wrapper.text()).toContain("Tab");
    expect(wrapper.text()).toContain("Ctrl+N");
    expect(wrapper.text()).toContain("Ctrl+T");
    wrapper.unmount();
  });

  it("mac variant uses ⌘ symbols", async () => {
    const wrapper = mount(ShortcutHints, {
      props: { mod: "Meta", thresholdMs: 100, bindings: {} },
      attachTo: document.body,
    });
    await flushPromises();
    fireKey("keydown", { key: "Meta", metaKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.text()).toContain("⌘N");
    expect(wrapper.text()).toContain("⌘T");
    wrapper.unmount();
  });

  it("releasing the mod hides the overlay", async () => {
    const wrapper = mount(ShortcutHints, {
      props: { mod: "Control", thresholdMs: 100, bindings: {} },
      attachTo: document.body,
    });
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
    const wrapper = mount(ShortcutHints, {
      props: { mod: "Control", thresholdMs: 100, bindings: { "pane.close": "" } },
      attachTo: document.body,
    });
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
    const wrapper = mount(ShortcutHints, {
      props: { mod: "Control", thresholdMs: 100, bindings: { "tab.new": "Mod+KeyL" } },
      attachTo: document.body,
    });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.text()).toContain("Ctrl+L");
    expect(wrapper.text()).not.toContain("Ctrl+T");
    wrapper.unmount();
  });

  it("bindings prop is reactive — updating it while visible changes the rendered chord", async () => {
    const wrapper = mount(ShortcutHints, {
      props: { mod: "Control", thresholdMs: 100, bindings: {} },
      attachTo: document.body,
    });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.text()).toContain("Ctrl+T");
    await wrapper.setProps({ bindings: { "tab.new": "Mod+KeyL" } });
    expect(wrapper.text()).toContain("Ctrl+L");
    expect(wrapper.text()).not.toContain("Ctrl+T");
    wrapper.unmount();
  });
});
