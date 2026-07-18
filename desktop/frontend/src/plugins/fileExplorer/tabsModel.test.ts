import { describe, expect, it } from "vitest";
import { openPath, closeTab, setViewMode, setDirty, defaultViewMode, type TabsState } from "./tabsModel";

function empty(): TabsState {
  return { tabs: [], activeIdx: -1 };
}

describe("tabsModel.openPath", () => {
  it("single-click opens a preview tab", () => {
    let s = empty();
    s = openPath(s, "/a.txt", "preview");
    expect(s.tabs).toHaveLength(1);
    expect(s.tabs[0].persistent).toBe(false);
    expect(s.activeIdx).toBe(0);
  });

  it("subsequent single-click on different file replaces the preview tab", () => {
    let s = empty();
    s = openPath(s, "/a.txt", "preview");
    s = openPath(s, "/b.txt", "preview");
    expect(s.tabs).toHaveLength(1);
    expect(s.tabs[0].path).toBe("/b.txt");
  });

  it("double-click promotes existing preview to persistent", () => {
    let s = empty();
    s = openPath(s, "/a.txt", "preview");
    s = openPath(s, "/a.txt", "persistent");
    expect(s.tabs).toHaveLength(1);
    expect(s.tabs[0].persistent).toBe(true);
  });

  it("single-click on a different file when a persistent tab exists adds a preview tab", () => {
    let s = empty();
    s = openPath(s, "/a.txt", "persistent");
    s = openPath(s, "/b.txt", "preview");
    expect(s.tabs.map((t) => t.path)).toEqual(["/a.txt", "/b.txt"]);
  });

  it("over 8 tabs evicts the oldest non-persistent tab", () => {
    let s = empty();
    for (let i = 0; i < 8; i++) s = openPath(s, `/p${i}`, "persistent");
    s = openPath(s, "/p8", "preview");
    expect(s.tabs).toHaveLength(8);
    expect(s.tabs.map((t) => t.path)).toContain("/p8");
    expect(s.tabs.map((t) => t.path)).not.toContain("/p0");
  });
});

describe("tabsModel.closeTab", () => {
  it("removes the tab and refocuses neighbor", () => {
    let s = empty();
    s = openPath(s, "/a.txt", "persistent");
    s = openPath(s, "/b.txt", "persistent");
    s = closeTab(s, 0);
    expect(s.tabs).toHaveLength(1);
    expect(s.tabs[0].path).toBe("/b.txt");
    expect(s.activeIdx).toBe(0);
  });
});

describe("tabsModel.setViewMode", () => {
  it("updates the active tab's viewMode", () => {
    const s = openPath({ tabs: [], activeIdx: -1 }, "/x/logo.svg", "persistent");
    expect(s.tabs[0].viewMode).toBe("code");
    const next = setViewMode(s, "render");
    expect(next.tabs[0].viewMode).toBe("render");
  });

  it("is a no-op when there's no active tab", () => {
    const s: TabsState = { tabs: [], activeIdx: -1 };
    expect(setViewMode(s, "render")).toBe(s);
  });
});

describe("tabsModel.defaultViewMode", () => {
  it("returns render for .md", () => {
    expect(defaultViewMode("/x/README.md")).toBe("render");
  });
  it("returns render for .markdown", () => {
    expect(defaultViewMode("/x/notes.markdown")).toBe("render");
  });
  it("returns render for .MD (case insensitive)", () => {
    expect(defaultViewMode("/x/CHANGELOG.MD")).toBe("render");
  });
  it("returns code for .svg (svg defaults to source view)", () => {
    expect(defaultViewMode("/x/icon.svg")).toBe("code");
  });
  it("returns code for plain .txt", () => {
    expect(defaultViewMode("/x/a.txt")).toBe("code");
  });
});

describe("tabsModel.openPath × defaultViewMode", () => {
  it("a new markdown tab opens in render mode", () => {
    const s = openPath({ tabs: [], activeIdx: -1 }, "/x/README.md", "persistent");
    expect(s.tabs[0].viewMode).toBe("render");
  });

  it("a new svg tab opens in code mode", () => {
    const s = openPath({ tabs: [], activeIdx: -1 }, "/x/logo.svg", "persistent");
    expect(s.tabs[0].viewMode).toBe("code");
  });
});

describe("tabsModel.setDirty", () => {
  it("marks the matching tab dirty by path", () => {
    let s: TabsState = { tabs: [], activeIdx: -1 };
    s = openPath(s, "/a", "persistent");
    s = openPath(s, "/b", "persistent");
    const next = setDirty(s, "/a", true);
    expect(next.tabs.find((t) => t.path === "/a")?.dirty).toBe(true);
    expect(next.tabs.find((t) => t.path === "/b")?.dirty).toBe(false);
  });

  it("no-ops when path is not open", () => {
    const s = openPath({ tabs: [], activeIdx: -1 }, "/a", "persistent");
    const next = setDirty(s, "/missing", true);
    expect(next).toBe(s);
  });

  it("no-ops when dirty already at target value", () => {
    const s = openPath({ tabs: [], activeIdx: -1 }, "/a", "persistent");
    const next = setDirty(s, "/a", false);
    expect(next).toBe(s);
  });

  it("openPath creates tabs with dirty=false", () => {
    const s = openPath({ tabs: [], activeIdx: -1 }, "/a", "persistent");
    expect(s.tabs[0].dirty).toBe(false);
  });
});
