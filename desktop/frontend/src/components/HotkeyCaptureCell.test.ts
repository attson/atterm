import { describe, it, expect } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import HotkeyCaptureCell from "./HotkeyCaptureCell.vue";

function makeKey(opts: KeyboardEventInit & { key: string; code: string }) {
  return new KeyboardEvent("keydown", { ...opts, bubbles: true, cancelable: true });
}

describe("HotkeyCaptureCell", () => {
  it("renders the current binding (with default mod injected)", () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
    });
    expect(wrapper.text()).toContain("Mod+KeyN");
  });

  it("displays placeholder text for empty binding", () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "", mod: "Control" },
    });
    expect(wrapper.text().toLowerCase()).toContain("disabled");
  });

  it("clicking enters capturing state", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    expect(wrapper.find(".hotkey-cell").classes()).toContain("capturing");
    expect(wrapper.text().toLowerCase()).toContain("press");
  });

  it("captures Ctrl+Shift+T and emits update", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
      attachTo: document.body,
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    document.dispatchEvent(makeKey({ key: "T", code: "KeyT", ctrlKey: true, shiftKey: true }));
    await flushPromises();
    expect(wrapper.emitted("update")).toBeTruthy();
    expect(wrapper.emitted("update")![0]).toEqual(["Mod+Shift+KeyT"]);
    // Exits capturing
    expect(wrapper.find(".hotkey-cell").classes()).not.toContain("capturing");
    wrapper.unmount();
  });

  it("Esc cancels capture without emitting update", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
      attachTo: document.body,
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    document.dispatchEvent(makeKey({ key: "Escape", code: "Escape" }));
    await flushPromises();
    expect(wrapper.emitted("update")).toBeFalsy();
    expect(wrapper.emitted("cancel")).toBeTruthy();
    expect(wrapper.find(".hotkey-cell").classes()).not.toContain("capturing");
    wrapper.unmount();
  });

  it("Backspace emits update with empty string (disables)", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
      attachTo: document.body,
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    document.dispatchEvent(makeKey({ key: "Backspace", code: "Backspace" }));
    await flushPromises();
    expect(wrapper.emitted("update")![0]).toEqual([""]);
    wrapper.unmount();
  });

  it("modifier-only press does not emit", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
      attachTo: document.body,
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    document.dispatchEvent(makeKey({ key: "Control", code: "ControlLeft", ctrlKey: true }));
    await flushPromises();
    expect(wrapper.emitted("update")).toBeFalsy();
    expect(wrapper.find(".hotkey-cell").classes()).toContain("capturing");
    wrapper.unmount();
  });

  it("bare letter without modifier does not emit", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
      attachTo: document.body,
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    document.dispatchEvent(makeKey({ key: "t", code: "KeyT" }));
    await flushPromises();
    expect(wrapper.emitted("update")).toBeFalsy();
    wrapper.unmount();
  });
});
