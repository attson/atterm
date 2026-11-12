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

function mountTooltipPair() {
  return mount(defineComponent({
    data: () => ({ tick: 0 }),
    template: `
      <div>
        <button data-test="first" v-tooltip="'First target'">First</button>
        <button data-test="second" v-tooltip="'Second target'">Second</button>
        <span>{{ tick }}</span>
      </div>
    `,
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

    await wrapper.setData({ value: "Updated while away" });
    await vi.advanceTimersByTimeAsync(350);
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

    await wrapper.setData({ value: "Updated after dismissal" });
    await vi.advanceTimersByTimeAsync(350);
    expect(document.querySelector('[role="tooltip"]')).toBeNull();

    await target.trigger("blur");
    await target.trigger("focus");
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe("Updated after dismissal");
  });

  test("updates a visible tooltip when its binding changes", async () => {
    const wrapper = mountTooltip("Before");
    const target = wrapper.get("button");

    await target.trigger("focus");
    const tooltip = document.querySelector<HTMLElement>('[role="tooltip"]');
    expect(tooltip?.textContent).toBe("Before");

    await wrapper.setData({ value: "After" });

    expect(document.querySelector('[role="tooltip"]')).toBe(tooltip);
    expect(tooltip?.textContent).toBe("After");
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

  test("does not let stale hover state reclaim the tooltip after an unrelated update", async () => {
    vi.useFakeTimers();
    const wrapper = mountTooltipPair();
    const first = wrapper.get('[data-test="first"]');
    const second = wrapper.get('[data-test="second"]');

    await first.trigger("pointerenter", { pointerType: "mouse" });
    await vi.advanceTimersByTimeAsync(350);
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe("First target");

    // WebKit can miss pointerleave when reactive layout moves a target. A new
    // pointerenter must still make the second target the sole tooltip owner.
    await second.trigger("pointerenter", { pointerType: "mouse" });
    await vi.advanceTimersByTimeAsync(350);
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe("Second target");

    await wrapper.setData({ tick: 1 });
    await vi.advanceTimersByTimeAsync(350);

    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe("Second target");
  });

  test("closes a hover tooltip when pointer movement proves the pointer left the target", async () => {
    vi.useFakeTimers();
    const wrapper = mountTooltip();
    const target = wrapper.get("button");

    await target.trigger("pointerenter", { pointerType: "mouse" });
    await vi.advanceTimersByTimeAsync(350);
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    // WebKit can omit pointerleave when reactive layout moves the hovered
    // element. The next pointer move still identifies the real target.
    document.body.dispatchEvent(new Event("pointermove", { bubbles: true }));

    expect(document.querySelector('[role="tooltip"]')).toBeNull();
  });

  test("remembers pointer departure while focus keeps the tooltip visible", async () => {
    const wrapper = mountTooltip();
    const target = wrapper.get("button");

    await target.trigger("pointerenter", { pointerType: "mouse" });
    await target.trigger("focus");
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    document.body.dispatchEvent(new Event("pointermove", { bubbles: true }));
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    await target.trigger("blur");
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
  });

  test.each([
    ["scroll", () => document.dispatchEvent(new Event("scroll"))],
    ["resize", () => window.dispatchEvent(new Event("resize"))],
  ])("keeps the tooltip closed after %s and a reactive update", async (_name, dismiss) => {
    vi.useFakeTimers();
    const wrapper = mountTooltip("Before dismissal");
    const target = wrapper.get("button");

    await target.trigger("pointerenter", { pointerType: "mouse" });
    await vi.advanceTimersByTimeAsync(350);
    expect(document.querySelector('[role="tooltip"]')).not.toBeNull();

    dismiss();
    expect(document.querySelector('[role="tooltip"]')).toBeNull();

    await wrapper.setData({ value: "After dismissal" });
    await vi.advanceTimersByTimeAsync(350);
    expect(document.querySelector('[role="tooltip"]')).toBeNull();
  });
});
