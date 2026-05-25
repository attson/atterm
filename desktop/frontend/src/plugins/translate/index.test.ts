import { describe, expect, it } from "vitest";
import { translateDescriptor } from "./index";
import { en } from "../../i18n/messages/en";

describe("translateDescriptor", () => {
  it("declares id, slot, title, description, defaultEnabled=false", () => {
    expect(translateDescriptor.id).toBe("translate");
    expect(translateDescriptor.slot).toBe("context-menu");
    expect(translateDescriptor.titleKey).toBe("plugins.translate.title");
    expect(en.plugins.translate.description.length).toBeGreaterThan(0);
    expect(translateDescriptor.defaultEnabled).toBe(false);
  });

  it("load() returns a ContextMenuPlugin with getMenuItems", async () => {
    const mod = await translateDescriptor.load();
    const ctxMenu = (mod as { default: { getMenuItems: Function } }).default;
    expect(typeof ctxMenu.getMenuItems).toBe("function");
  });

  it("getMenuItems returns empty array for blank selection", async () => {
    const mod = await translateDescriptor.load();
    const ctxMenu = (mod as { default: { getMenuItems: Function } }).default;
    const items = ctxMenu.getMenuItems({}, "");
    expect(items).toEqual([]);
  });

  it("getMenuItems returns one item for non-blank selection", async () => {
    const mod = await translateDescriptor.load();
    const ctxMenu = (mod as { default: { getMenuItems: Function } }).default;
    const items = ctxMenu.getMenuItems({}, "hello");
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ id: "translate-selection", label: en.plugins.translate.selection });
    expect(typeof items[0].onClick).toBe("function");
  });
});
