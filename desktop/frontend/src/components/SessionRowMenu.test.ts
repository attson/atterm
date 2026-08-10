import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import SessionRowMenu, { type MenuItem } from "./SessionRowMenu.vue";

function factory(overrides: {
  open?: boolean;
  items?: MenuItem[];
} = {}) {
  return mount(SessionRowMenu, {
    attachTo: document.body,
    props: {
      open: true,
      x: 100,
      y: 100,
      items: overrides.items ?? [
        { key: "pin", label: "Pin to top" },
      ],
      ...(overrides.open !== undefined ? { open: overrides.open } : {}),
    },
  });
}

describe("SessionRowMenu (items-driven)", () => {
  test("does not render when open=false", () => {
    const w = factory({ open: false });
    expect(w.find("[data-test=session-row-menu]").exists()).toBe(false);
  });

  test("renders each item with its label and key-scoped data-test", () => {
    const w = factory({
      items: [
        { key: "details", label: "View details" },
        { key: "pin", label: "Pin to top" },
      ],
    });
    expect(w.find("[data-test=session-row-menu-item-details]").text()).toBe("View details");
    expect(w.find("[data-test=session-row-menu-item-pin]").text()).toBe("Pin to top");
  });

  test("clicking an enabled item emits select(key) then close", async () => {
    const w = factory({
      items: [{ key: "pin", label: "Pin to top" }],
    });
    await w.find("[data-test=session-row-menu-item-pin]").trigger("click");
    expect(w.emitted("select")).toEqual([["pin"]]);
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("clicking a disabled item does not emit select or close", async () => {
    const w = factory({
      items: [{ key: "details", label: "View details", disabled: true }],
    });
    await w.find("[data-test=session-row-menu-item-details]").trigger("click");
    expect(w.emitted("select")).toBeUndefined();
    expect(w.emitted("close")).toBeUndefined();
  });

  test("Escape emits close", async () => {
    const w = factory();
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(w.emitted("close")).toHaveLength(1);
    w.unmount();
  });

  test("outside mousedown emits close", async () => {
    const w = factory();
    document.body.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    expect(w.emitted("close")).toHaveLength(1);
    w.unmount();
  });
});

describe("SessionRowMenu separators", () => {
  const items: MenuItem[] = [
    { key: "edit", label: "Edit" },
    { key: "remove", label: "Remove", separatorBefore: true },
  ];

  test("renders a divider above an item marked separatorBefore", () => {
    const w = factory({ items });
    expect(w.find("[data-test=session-row-menu-separator-remove]").exists()).toBe(true);
  });

  test("renders no divider for an unmarked item", () => {
    const w = factory({ items });
    expect(w.find("[data-test=session-row-menu-separator-edit]").exists()).toBe(false);
  });

  test("a separator does not change how the item itself behaves", async () => {
    const w = factory({ items });
    await w.find("[data-test=session-row-menu-item-remove]").trigger("click");
    expect(w.emitted("select")).toEqual([["remove"]]);
    expect(w.emitted("close")).toHaveLength(1);
  });
});
