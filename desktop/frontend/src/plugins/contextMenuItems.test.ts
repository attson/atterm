import { describe, expect, it, vi } from "vitest";
import { collectContextMenuItems } from "./contextMenuItems";
import type { ContextMenuPlugin, PluginContext } from "./types";

const fakeCtx = {} as PluginContext;

describe("collectContextMenuItems", () => {
  it("returns empty when no plugins registered", async () => {
    const items = await collectContextMenuItems([], fakeCtx, "sel");
    expect(items).toEqual([]);
  });

  it("merges items from multiple plugins in registration order", async () => {
    const a: ContextMenuPlugin = {
      getMenuItems: () => [{ id: "a1", label: "A1", onClick: () => {} }],
    };
    const b: ContextMenuPlugin = {
      getMenuItems: () => [
        { id: "b1", label: "B1", onClick: () => {} },
        { id: "b2", label: "B2", onClick: () => {} },
      ],
    };
    const items = await collectContextMenuItems([a, b], fakeCtx, "sel");
    expect(items.map((i) => i.id)).toEqual(["a1", "b1", "b2"]);
  });

  it("skips a plugin whose getMenuItems throws, logs the error", async () => {
    const consoleSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const ok: ContextMenuPlugin = {
      getMenuItems: () => [{ id: "ok1", label: "OK", onClick: () => {} }],
    };
    const bad: ContextMenuPlugin = {
      getMenuItems: () => { throw new Error("boom"); },
    };
    const items = await collectContextMenuItems([ok, bad, ok], fakeCtx, "sel");
    expect(items.map((i) => i.id)).toEqual(["ok1", "ok1"]);
    expect(consoleSpy).toHaveBeenCalled();
    consoleSpy.mockRestore();
  });

  it("passes selection through to each plugin", async () => {
    const seen: string[] = [];
    const probe: ContextMenuPlugin = {
      getMenuItems: (_ctx, sel) => {
        seen.push(sel);
        return [];
      },
    };
    await collectContextMenuItems([probe], fakeCtx, "hello world");
    expect(seen).toEqual(["hello world"]);
  });
});
