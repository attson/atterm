import { describe, expect, it } from "vitest";
import { openPath, closeTab, type TabsState } from "./tabsModel";

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
