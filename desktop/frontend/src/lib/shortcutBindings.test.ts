import { describe, it, expect } from "vitest";
import { ACTIONS, ACTION_BY_ID, DEFAULT_BINDINGS } from "./shortcutBindings";

describe("shortcutBindings registry", () => {
  it("declares 12 actions", () => {
    expect(ACTIONS).toHaveLength(12);
  });

  it("has unique action IDs", () => {
    const ids = ACTIONS.map((a) => a.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("groups actions under pane or tab", () => {
    for (const a of ACTIONS) {
      expect(["pane", "tab"]).toContain(a.group);
    }
  });

  it("ACTION_BY_ID looks up actions by id", () => {
    expect(ACTION_BY_ID["pane.split-vertical-new"]?.defaultBinding).toBe("Mod+KeyN");
    expect(ACTION_BY_ID["tab.next"]?.defaultBinding).toBe("Mod+Shift+BracketRight");
  });

  it("DEFAULT_BINDINGS reverse-maps binding -> actionId for every action", () => {
    for (const a of ACTIONS) {
      expect(DEFAULT_BINDINGS[a.defaultBinding]).toBe(a.id);
    }
    expect(Object.keys(DEFAULT_BINDINGS)).toHaveLength(12);
  });
});
