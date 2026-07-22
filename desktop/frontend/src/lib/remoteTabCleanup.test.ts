import { describe, expect, it } from "vitest";
import type { SessionInfo } from "./connection";
import type { Tab } from "./types";
import { pruneStaleRemoteTabs } from "./remoteTabCleanup";

const remoteSession = (id: string): SessionInfo => ({
  id,
  command: "claude",
  cwd: "/tmp",
  title: id,
  cols: 120,
  rows: 40,
  started_at: 1,
  host_id: "remote-host",
});

function singleRemote(id: string): Tab {
  return {
    id: `tab-${id}`,
    layout: "single",
    panes: [{ sessionId: id, remote: true }],
    activePaneIdx: 0,
    colRatio: 0.5,
    rowRatio: 0.5,
  };
}

describe("pruneStaleRemoteTabs", () => {
  it("keeps a missing single-pane remote tab during the grace window, then prunes it", () => {
    const missingSince = new Map<string, number>();
    const first = pruneStaleRemoteTabs({
      tabs: [singleRemote("remote-1")],
      remoteSessions: [],
      missingSince,
      nowMs: 1_000,
      graceMs: 60_000,
    });

    expect(first.tabs.map((t) => t.id)).toEqual(["tab-remote-1"]);
    expect(missingSince.get("remote-1")).toBe(1_000);

    const second = pruneStaleRemoteTabs({
      tabs: first.tabs,
      remoteSessions: [],
      missingSince,
      nowMs: 61_000,
      graceMs: 60_000,
    });

    expect(second.tabs).toEqual([]);
    expect(missingSince.has("remote-1")).toBe(false);
  });

  it("does not prune split tabs or local tabs", () => {
    const split: Tab = {
      id: "split",
      layout: "vertical",
      panes: [
        { sessionId: "remote-1", remote: true },
        { sessionId: "local-1", remote: false },
      ],
      activePaneIdx: 0,
      colRatio: 0.5,
      rowRatio: 0.5,
    };
    const local: Tab = {
      ...singleRemote("local-only"),
      panes: [{ sessionId: "local-only", remote: false }],
    };

    const got = pruneStaleRemoteTabs({
      tabs: [split, local],
      remoteSessions: [],
      missingSince: new Map([
        ["remote-1", 0],
        ["local-only", 0],
      ]),
      nowMs: 60_000,
      graceMs: 60_000,
    });

    expect(got.tabs.map((t) => t.id)).toEqual(["split", "tab-local-only"]);
  });

  it("clears stale tracking when a remote session appears again", () => {
    const missingSince = new Map<string, number>([["remote-1", 1_000]]);

    const got = pruneStaleRemoteTabs({
      tabs: [singleRemote("remote-1")],
      remoteSessions: [remoteSession("remote-1")],
      missingSince,
      nowMs: 90_000,
      graceMs: 60_000,
    });

    expect(got.tabs.map((t) => t.id)).toEqual(["tab-remote-1"]);
    expect(missingSince.has("remote-1")).toBe(false);
  });
});
