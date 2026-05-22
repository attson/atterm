import { describe, it, expect } from "vitest";
import { ACTIONS, ACTION_BY_ID, DEFAULT_BINDINGS } from "./shortcutBindings";
import { serialize, parse, type Mod } from "./shortcutBindings";

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

function ev(opts: KeyboardEventInit & { key: string; code: string }): KeyboardEvent {
  return new KeyboardEvent("keydown", { ...opts, bubbles: true, cancelable: true });
}

describe("serialize", () => {
  it("Ctrl+N (mod=Control) -> 'Mod+KeyN'", () => {
    const e = ev({ key: "n", code: "KeyN", ctrlKey: true });
    expect(serialize(e, "Control")).toBe("Mod+KeyN");
  });

  it("Meta+N (mod=Meta) -> 'Mod+KeyN'", () => {
    const e = ev({ key: "n", code: "KeyN", metaKey: true });
    expect(serialize(e, "Meta")).toBe("Mod+KeyN");
  });

  it("Ctrl+Alt+Shift+N preserves token order Mod,Alt,Shift,code", () => {
    const e = ev({ key: "N", code: "KeyN", ctrlKey: true, altKey: true, shiftKey: true });
    expect(serialize(e, "Control")).toBe("Mod+Alt+Shift+KeyN");
  });

  it("Alt+Shift+ArrowLeft (no Mod) -> 'Alt+Shift+ArrowLeft'", () => {
    const e = ev({ key: "ArrowLeft", code: "ArrowLeft", altKey: true, shiftKey: true });
    expect(serialize(e, "Control")).toBe("Alt+Shift+ArrowLeft");
  });

  it("wrong modifier (Meta on Control platform) returns null", () => {
    const e = ev({ key: "n", code: "KeyN", metaKey: true });
    expect(serialize(e, "Control")).toBeNull();
  });

  it("modifier-only keypress returns null", () => {
    // Ctrl alone — code is ControlLeft (not in whitelist) and there's
    // effectively no key. We model "modifier-only" by passing a code that
    // is not in the whitelist.
    const e = ev({ key: "Control", code: "ControlLeft", ctrlKey: true });
    expect(serialize(e, "Control")).toBeNull();
  });

  it("bare letter (no modifier) returns null", () => {
    const e = ev({ key: "n", code: "KeyN" });
    expect(serialize(e, "Control")).toBeNull();
  });

  it("mac dead key (key='˜', code='KeyN') still produces 'Mod+Alt+KeyN'", () => {
    const e = ev({ key: "˜", code: "KeyN", ctrlKey: true, altKey: true });
    expect(serialize(e, "Control")).toBe("Mod+Alt+KeyN");
  });

  it("code outside the whitelist returns null", () => {
    const e = ev({ key: "Tab", code: "Tab", ctrlKey: true });
    expect(serialize(e, "Control")).toBeNull();
  });
});

describe("parse", () => {
  it("round-trips a normal binding", () => {
    expect(parse("Mod+Alt+Shift+KeyN")).toEqual({
      mod: true, alt: true, shift: true, code: "KeyN",
    });
  });

  it("parses no-modifier-error correctly (returns null)", () => {
    expect(parse("KeyN")).toBeNull();
  });

  it("parses empty as empty (sentinel for disabled)", () => {
    expect(parse("")).toEqual({ mod: false, alt: false, shift: false, code: null });
  });

  it("rejects unknown tokens", () => {
    expect(parse("Hyper+KeyN")).toBeNull();
  });

  it("rejects out-of-order tokens", () => {
    expect(parse("Alt+Mod+KeyN")).toBeNull();
  });

  it("rejects two codes", () => {
    expect(parse("Mod+KeyN+KeyM")).toBeNull();
  });

  // Mod type is exported for callers; this just ensures it compiles.
  const _m: Mod = "Control";
  void _m;
});

import { conflictsWith, resolvedBindings } from "./shortcutBindings";

describe("resolvedBindings", () => {
  it("returns defaults for unset actions", () => {
    const r = resolvedBindings({});
    expect(r["pane.split-vertical-new"]).toBe("Mod+KeyN");
    expect(r["tab.next"]).toBe("Mod+Shift+BracketRight");
  });

  it("user overrides win", () => {
    const r = resolvedBindings({ "tab.next": "Mod+KeyL" });
    expect(r["tab.next"]).toBe("Mod+KeyL");
    expect(r["tab.prev"]).toBe("Mod+Shift+BracketLeft"); // unchanged
  });

  it("user can disable an action with empty string", () => {
    const r = resolvedBindings({ "pane.close": "" });
    expect(r["pane.close"]).toBe("");
  });
});

describe("conflictsWith", () => {
  it("detects a conflict between two actions on the same binding", () => {
    const bindings = resolvedBindings({ "pane.close": "Mod+KeyT" });
    // pane.close now collides with tab.new (default Mod+KeyT)
    const c = conflictsWith(bindings, "pane.close");
    expect(c).toEqual(["tab.new"]);
  });

  it("excludes the action being checked", () => {
    const bindings = resolvedBindings({});
    expect(conflictsWith(bindings, "pane.close")).toEqual([]);
  });

  it("ignores empty bindings (disabled actions)", () => {
    const bindings = resolvedBindings({ "pane.close": "", "tab.new": "" });
    expect(conflictsWith(bindings, "pane.close")).toEqual([]);
  });

  it("returns multiple conflicting action ids if 3 collide", () => {
    const bindings = resolvedBindings({
      "pane.close": "Mod+KeyT",
      "pane.focus-left": "Mod+KeyT",
    });
    const c = conflictsWith(bindings, "pane.close").sort();
    expect(c).toEqual(["pane.focus-left", "tab.new"]);
  });
});
