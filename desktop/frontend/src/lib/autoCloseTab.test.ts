import { describe, it, expect } from "vitest";
import { tabsToAutoCloseOnExit } from "./autoCloseTab";
import type { Tab } from "./types";

function tab(id: string, panes: { sessionId: string | null; remote?: boolean }[]): Tab {
  return {
    id,
    layout: "single",
    activePaneIdx: 0,
    colRatio: 0.5,
    rowRatio: 0.5,
    panes: panes.map((p) => ({ sessionId: p.sessionId, remote: p.remote ?? false })),
  } as Tab;
}

describe("tabsToAutoCloseOnExit", () => {
  it("closes a single-pane tab whose session just exited", () => {
    const tabs = [tab("t1", [{ sessionId: null }])]; // pane already nulled by sweep
    const out = tabsToAutoCloseOnExit(tabs, new Set(), ["t1"]);
    expect(out).toEqual(["t1"]);
  });

  it("does NOT close a tab that still has a live local session in another pane", () => {
    const tabs = [tab("t1", [{ sessionId: null }, { sessionId: "live" }])];
    const out = tabsToAutoCloseOnExit(tabs, new Set(["live"]), ["t1"]);
    expect(out).toEqual([]);
  });

  it("does NOT close a tab that still has a live remote pane", () => {
    const tabs = [tab("t1", [{ sessionId: null }, { sessionId: "r1", remote: true }])];
    const out = tabsToAutoCloseOnExit(tabs, new Set(), ["t1"]);
    expect(out).toEqual([]);
  });

  it("ignores tabs that did not lose a session this sweep", () => {
    // A freshly-opened empty tab (all panes null) that was NOT in clearedTabIds
    // must survive — it wasn't a terminal that exited.
    const tabs = [tab("fresh", [{ sessionId: null }])];
    const out = tabsToAutoCloseOnExit(tabs, new Set(), []);
    expect(out).toEqual([]);
  });

  it("closes only the cleared tab, leaving other empty-but-untouched tabs alone", () => {
    const tabs = [
      tab("exited", [{ sessionId: null }]),
      tab("alsoEmpty", [{ sessionId: null }]),
    ];
    const out = tabsToAutoCloseOnExit(tabs, new Set(), ["exited"]);
    expect(out).toEqual(["exited"]);
  });

  it("handles an SSH session (remote:false) exiting the same as a local shell", () => {
    // Adopted SSH sessions are remote:false; when the id leaves localIds the
    // pane is nulled and the tab should close like any local terminal.
    const tabs = [tab("ssh-tab", [{ sessionId: null }])];
    const out = tabsToAutoCloseOnExit(tabs, new Set(["other"]), ["ssh-tab"]);
    expect(out).toEqual(["ssh-tab"]);
  });
});
