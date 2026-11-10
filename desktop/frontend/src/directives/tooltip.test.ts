import { afterEach, describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import { defineComponent } from "vue";
import { tooltipDirective } from "./tooltip";

function mountTooltip(value = "Exact time") {
  return mount(defineComponent({
    data: () => ({ value }),
    template: '<button v-tooltip="value">Target</button>',
  }), {
    attachTo: document.body,
    global: { directives: { tooltip: tooltipDirective } },
  });
}

describe("v-tooltip", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  test("shows the project tooltip on hover without a native title", async () => {
    vi.useFakeTimers();
    const wrapper = mountTooltip();
    const target = wrapper.get("button");

    expect(target.attributes("title")).toBeUndefined();
    await target.trigger("pointerenter", { pointerType: "mouse" });
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
    await vi.advanceTimersByTimeAsync(350);

    const tooltip = document.querySelector<HTMLElement>('[role="tooltip"]');
    expect(tooltip?.textContent).toBe("Exact time");
    expect(target.attributes("aria-describedby")).toBe(tooltip?.id);

    await target.trigger("pointerleave", { pointerType: "mouse" });
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
  });

  test("supports keyboard focus and Escape", async () => {
    vi.useFakeTimers();
    const wrapper = mountTooltip("Keyboard hint");
    const target = wrapper.get("button");

    await target.trigger("focus");
    await vi.advanceTimersByTimeAsync(350);
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe("Keyboard hint");

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
    expect(target.attributes("aria-describedby")).toBeUndefined();
  });

  test("does not render empty values and closes when a visible value becomes empty", async () => {
    vi.useFakeTimers();
    const wrapper = mountTooltip("Visible");
    await wrapper.get("button").trigger("pointerenter", { pointerType: "mouse" });
    await vi.advanceTimersByTimeAsync(350);
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    await wrapper.setData({ value: "" });
    expect(document.querySelector('[role="tooltip"]')).toBeNull();

    await wrapper.get("button").trigger("pointerenter", { pointerType: "mouse" });
    await vi.advanceTimersByTimeAsync(350);
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
  });

  test("flips below targets near the top and clamps to the viewport edge", async () => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      if (this.classList.contains("atterm-tooltip")) {
        return { left: 0, top: 0, right: 120, bottom: 40, width: 120, height: 40, x: 0, y: 0, toJSON() {} };
      }
      return { left: -80, top: 2, right: -70, bottom: 22, width: 10, height: 20, x: -80, y: 2, toJSON() {} };
    });
    const wrapper = mountTooltip("Positioned");

    await wrapper.get("button").trigger("focus");

    const tooltip = document.querySelector<HTMLElement>('[role="tooltip"]');
    expect(tooltip?.dataset.placement).toBe("bottom");
    expect(tooltip?.style.left).toBe("8px");
  });
});
