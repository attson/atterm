import { describe, expect, it } from "vitest";
import type { PluginSlot, MenuItem, ContextMenuPlugin, PluginID } from "./types";

describe("plugin types", () => {
  it("allows 'context-menu' as a PluginSlot", () => {
    const s: PluginSlot = "context-menu";
    expect(s).toBe("context-menu");
  });

  it("allows 'translate' as a PluginID", () => {
    const id: PluginID = "translate";
    expect(id).toBe("translate");
  });

  it("ContextMenuPlugin.getMenuItems returns MenuItem[]", () => {
    const fake: ContextMenuPlugin = {
      getMenuItems: () => [{ id: "x", label: "X", onClick: () => {} }],
    };
    const items: MenuItem[] = fake.getMenuItems(
      {} as never,
      "selection",
    );
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ id: "x", label: "X" });
  });
});
