import { describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import SessionRowMenu from "./SessionRowMenu.vue";

function factory(overrides: Partial<InstanceType<typeof SessionRowMenu>["$props"]> = {}) {
  return mount(SessionRowMenu, {
    attachTo: document.body,
    props: {
      open: true,
      x: 100,
      y: 100,
      pinned: false,
      labelPin: "Pin to top",
      labelUnpin: "Unpin",
      ...overrides,
    },
  });
}

describe("SessionRowMenu", () => {
  test("does not render when open=false", () => {
    const w = factory({ open: false });
    expect(w.find("[data-test=session-row-menu]").exists()).toBe(false);
  });

  test("shows Pin label when pinned=false", () => {
    const w = factory({ pinned: false });
    const item = w.find("[data-test=session-row-menu-item]");
    expect(item.text()).toBe("Pin to top");
  });

  test("shows Unpin label when pinned=true", () => {
    const w = factory({ pinned: true });
    const item = w.find("[data-test=session-row-menu-item]");
    expect(item.text()).toBe("Unpin");
  });

  test("clicking the item emits togglePin then close", async () => {
    const w = factory();
    await w.find("[data-test=session-row-menu-item]").trigger("click");
    expect(w.emitted("togglePin")).toHaveLength(1);
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("Escape emits close", async () => {
    const w = factory();
    await w.trigger("keydown", { key: "Escape" });
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("outside click emits close", async () => {
    const w = factory();
    // Simulate click outside the menu root.
    document.body.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    expect(w.emitted("close")).toHaveLength(1);
    w.unmount();
  });

  test("focusout to a target outside the menu emits close; inside does not", async () => {
    const w = factory();
    const menu = w.find("[data-test=session-row-menu]").element as HTMLElement;
    const outside = document.createElement("button");
    document.body.appendChild(outside);

    menu.dispatchEvent(
      new FocusEvent("focusout", { bubbles: true, relatedTarget: outside }),
    );
    expect(w.emitted("close")).toHaveLength(1);

    const insideItem = w.find("[data-test=session-row-menu-item]").element;
    menu.dispatchEvent(
      new FocusEvent("focusout", { bubbles: true, relatedTarget: insideItem }),
    );
    expect(w.emitted("close")).toHaveLength(1);

    outside.remove();
    w.unmount();
  });

  test("positions to (x, y) via inline style", () => {
    const w = factory({ x: 200, y: 300 });
    const root = w.find("[data-test=session-row-menu]");
    const style = (root.element as HTMLElement).style;
    expect(style.left).toBe("200px");
    expect(style.top).toBe("300px");
  });
});
