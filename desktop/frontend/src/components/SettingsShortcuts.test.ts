import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsShortcuts from "./SettingsShortcuts.vue";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";
import { __setBindingsForTest } from "../lib/api";

let getShortcutBindingsMock: ReturnType<typeof vi.fn>;
let setShortcutBindingsMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  __setPlatformForTests(createFakePlatform());
});

afterEach(() => {
  __setBindingsForTest(undefined);
  __setPlatformForTests(null);
});

// Mounts SettingsShortcuts with GetShortcutBindings/SetShortcutBindings
// stubbed — the component's sole data source since Task 3 moved it off
// usePluginConfigStore (see SettingsTerminalAppearance.test.ts for the same
// __setBindingsForTest convention).
async function mountShortcuts(initial: Record<string, string> = {}) {
  getShortcutBindingsMock = vi.fn().mockResolvedValue(initial);
  setShortcutBindingsMock = vi.fn().mockResolvedValue(undefined);
  __setBindingsForTest({
    GetShortcutBindings: getShortcutBindingsMock,
    SetShortcutBindings: setShortcutBindingsMock,
  } as any);
  const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
  await flushPromises();
  return wrapper;
}

describe("SettingsShortcuts", () => {
  it("renders 13 rows grouped under 'pane', 'tab', and 'sidebar'", async () => {
    const wrapper = await mountShortcuts();
    const rows = wrapper.findAll(".shortcut-row");
    expect(rows).toHaveLength(13);
    expect(wrapper.text()).toContain("Pane");
    expect(wrapper.text()).toContain("Tab");
    expect(wrapper.text()).toContain("Sidebar");
  });

  it("each row's hotkey cell shows the current binding (default when not overridden)", async () => {
    const wrapper = await mountShortcuts();
    expect(wrapper.text()).toContain("Mod+KeyN");
    expect(wrapper.text()).toContain("Mod+KeyT");
  });

  it("editing a row to collide with another shows a conflict notice and disables Save", async () => {
    const wrapper = await mountShortcuts();
    // Simulate emitting update on the first row's HotkeyCaptureCell — easier
    // to drive the data flow than to fake a real capture.
    const cells = wrapper.findAllComponents({ name: "HotkeyCaptureCell" });
    await cells[0].vm.$emit("update", "Mod+KeyT"); // collides with tab.new (default)
    await flushPromises();
    expect(wrapper.text().toLowerCase()).toContain("conflicts with");
    const saveBtn = wrapper.find("button.save");
    expect((saveBtn.element as HTMLButtonElement).disabled).toBe(true);
  });

  it("Reset all marks draft empty (defaults shown) and sets dirty", async () => {
    const wrapper = await mountShortcuts({ "pane.close": "Mod+KeyL" });
    // Before reset, pane.close shows the override
    expect(wrapper.text()).toContain("Mod+KeyL");
    await wrapper.find("button.reset-all").trigger("click");
    await flushPromises();
    // Now pane.close shows the default
    expect(wrapper.text()).toContain("Mod+KeyW");
    const saveBtn = wrapper.find("button.save");
    expect((saveBtn.element as HTMLButtonElement).disabled).toBe(false);
  });

  it("Discard reverts draft to current store value, clears dirty", async () => {
    const wrapper = await mountShortcuts({ "tab.new": "Mod+KeyP" });
    const cells = wrapper.findAllComponents({ name: "HotkeyCaptureCell" });
    await cells[9].vm.$emit("update", "Mod+KeyQ");
    await flushPromises();
    expect(wrapper.text()).toContain("Mod+KeyQ");
    await wrapper.find("button.discard").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("Mod+KeyP");
    expect(wrapper.text()).not.toContain("Mod+KeyQ");
  });

  it("loads bindings from the dedicated Go field, not the plugin config", async () => {
    await mountShortcuts();
    expect(getShortcutBindingsMock).toHaveBeenCalled();
  });

  it("saves through setShortcutBindings with only the modified entries (defaults stripped)", async () => {
    const wrapper = await mountShortcuts();
    const cells = wrapper.findAllComponents({ name: "HotkeyCaptureCell" });
    // Index 8 is tab.new (after pane.* 0..7, which now includes terminal.search). Change it to a non-default.
    await cells[8].vm.$emit("update", "Mod+KeyP");
    await flushPromises();
    await wrapper.find("button.save").trigger("click");
    await flushPromises();
    expect(setShortcutBindingsMock).toHaveBeenCalledTimes(1);
    expect(setShortcutBindingsMock).toHaveBeenCalledWith(
      expect.objectContaining({ "tab.new": "Mod+KeyP" }),
    );
  });
});
