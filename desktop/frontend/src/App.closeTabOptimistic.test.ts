import { describe, expect, it } from "vitest";
import { computeCloseTabState } from "./lib/closeTabOptimistic";
import type { Tab } from "./lib/types";
import type { SessionInfo } from "./lib/connection";

function tab(id: string, sessionId: string | null, remote = false): Tab {
  return {
    id,
    layout: "single",
    panes: [{ sessionId, remote }],
    activePaneIdx: 0,
    colRatio: 0.5,
    rowRatio: 0.5,
  };
}

function sess(id: string): SessionInfo {
  return {
    id, command: "zsh", cwd: "/", title: "",
    cols: 80, rows: 24, started_at: 0,
  } as SessionInfo;
}

describe("computeCloseTabState", () => {
  it("drops the tab and its local session from both lists", () => {
    const tabs: Tab[] = [tab("t1", "s1"), tab("t2", "s2")];
    const localList = [sess("s1"), sess("s2")];
    const plan = computeCloseTabState(tabs, localList, "t1", "t1")!;
    expect(plan.nextTabs.map((t) => t.id)).toEqual(["t2"]);
    expect(plan.nextLocalList.map((s) => s.id)).toEqual(["s2"]);
    expect(plan.localIdsToClose).toEqual(["s1"]);
    expect(plan.wasCurrent).toBe(true);
  });

  it("keeps localList untouched for remote-session tabs", () => {
    const tabs: Tab[] = [tab("t1", "sR", true)];
    const localList = [sess("sX")];
    const plan = computeCloseTabState(tabs, localList, "t1", "t1")!;
    expect(plan.nextTabs).toEqual([]);
    expect(plan.nextLocalList).toBe(localList);
    expect(plan.localIdsToClose).toEqual([]);
  });

  it("returns null when the tab id is unknown", () => {
    expect(computeCloseTabState([tab("t1", "s1")], [sess("s1")], "t1", "zz"))
      .toBeNull();
  });

  it("detachOnly skips the RPC list but still removes the tab", () => {
    const tabs: Tab[] = [tab("t1", "s1"), tab("t2", "s2")];
    const localList = [sess("s1"), sess("s2")];
    const plan = computeCloseTabState(tabs, localList, "t2", "t1",
      { detachOnly: true })!;
    expect(plan.localIdsToClose).toEqual([]);
    expect(plan.nextTabs.map((t) => t.id)).toEqual(["t2"]);
    // detachOnly must leave localList alone — s1 is being moved, not killed.
    expect(plan.nextLocalList).toBe(localList);
    expect(plan.wasCurrent).toBe(false);
  });

  it("wasCurrent is false when a non-active tab is closed", () => {
    const tabs: Tab[] = [tab("t1", "s1"), tab("t2", "s2")];
    const plan = computeCloseTabState(tabs, [sess("s1"), sess("s2")], "t1", "t2")!;
    expect(plan.wasCurrent).toBe(false);
  });

  it("handles multi-pane tabs — all local sessions dropped together", () => {
    const t: Tab = {
      id: "t1", layout: "vertical",
      panes: [
        { sessionId: "s1", remote: false },
        { sessionId: "s2", remote: false },
      ],
      activePaneIdx: 0, colRatio: 0.5, rowRatio: 0.5,
    };
    const localList = [sess("s1"), sess("s2"), sess("s3")];
    const plan = computeCloseTabState([t], localList, "t1", "t1")!;
    expect(plan.localIdsToClose.sort()).toEqual(["s1", "s2"]);
    expect(plan.nextLocalList.map((s) => s.id)).toEqual(["s3"]);
  });
});
