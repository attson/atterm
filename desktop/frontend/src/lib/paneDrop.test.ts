import { describe, expect, it } from "vitest";
import {
  SESSION_DND_MIME,
  carriesSessionDrag,
  clearDraggingSession,
  draggingSession,
  planDetachToTab,
  resolveDroppedSession,
  swapPanes,
  setDraggingSession,
} from "./paneDrop";
import type { Pane } from "./types";

const pane = (sessionId: string | null, remote = true): Pane => ({ sessionId, remote });

describe("carriesSessionDrag", () => {
  it("recognises our own drag", () => {
    expect(carriesSessionDrag([SESSION_DND_MIME])).toBe(true);
    expect(carriesSessionDrag([SESSION_DND_MIME, "text/plain"])).toBe(true);
  });

  // Anything we did not start — a file from Finder, selected text — must fall
  // through untouched, or the pane would swallow drops it cannot handle.
  it("ignores drags we did not start", () => {
    expect(carriesSessionDrag(["Files"])).toBe(false);
    expect(carriesSessionDrag(["text/plain"])).toBe(false);
    expect(carriesSessionDrag([])).toBe(false);
    expect(carriesSessionDrag(undefined)).toBe(false);
  });
});

describe("resolveDroppedSession", () => {
  const tabs = [
    { id: "t1", panes: [pane("a"), { sessionId: "local", remote: false }] },
    { id: "t2", panes: [pane("c")] },
  ];

  it("reports where an already-visible session is showing", () => {
    expect(resolveDroppedSession(tabs, "c")?.from).toEqual({ tabId: "t2", paneIdx: 0 });
  });

  // The remote flag is not ours to invent: it picks the endpoint the pane
  // attaches to and the list its title resolves against, so a locally-spawned
  // shell re-minted as remote resolves in neither and renders empty.
  it("carries the existing pane across whole, keeping its remote flag", () => {
    const got = resolveDroppedSession(tabs, "local");
    expect(got?.pane).toEqual({ sessionId: "local", remote: false });
    expect(got?.from).toEqual({ tabId: "t1", paneIdx: 1 });
  });

  it("copies the pane rather than aliasing it", () => {
    const got = resolveDroppedSession(tabs, "a");
    expect(got?.pane).not.toBe(tabs[0].panes[0]);
    expect(got?.pane).toEqual(tabs[0].panes[0]);
  });

  it("mints a remote pane for a session that is not on screen", () => {
    const got = resolveDroppedSession(tabs, "fresh");
    expect(got?.pane).toEqual({ sessionId: "fresh", remote: true });
    expect(got?.from).toBeUndefined();
  });

  it("returns null for an empty id", () => {
    expect(resolveDroppedSession(tabs, "")).toBeNull();
  });
});

// WebKit (which is what the desktop app's webview runs) restricts reading
// dataTransfer for custom MIME types: `types` stays visible — so the drop
// target still lights up — but getData can come back empty, which silently
// dropped the whole gesture. The id is stashed at dragstart instead; a drag
// never leaves this document, so the module-level copy is authoritative and
// dataTransfer is left to do nothing but announce the type.
describe("draggingSession fallback", () => {
  it("remembers the id across the gesture", () => {
    setDraggingSession("sid-1");
    expect(draggingSession()).toBe("sid-1");
  });

  it("clears when the gesture ends, so a later stray drop places nothing", () => {
    setDraggingSession("sid-1");
    clearDraggingSession();
    expect(draggingSession()).toBe("");
  });
});

describe("planDetachToTab", () => {
  const tabs = [
    { id: "split", panes: [pane("a"), { sessionId: "local", remote: false }] },
    { id: "alone", panes: [pane("solo")] },
  ];

  it("locates the pane to pull out", () => {
    expect(planDetachToTab(tabs, "a")).toEqual({
      tabId: "split", paneIdx: 0, pane: { sessionId: "a", remote: true },
    });
  });

  // The new tab attaches through whatever endpoint the pane already used;
  // re-minting a local shell as remote would leave it resolvable in neither
  // list and the tab would render empty.
  it("carries the pane's remote flag to the new tab", () => {
    expect(planDetachToTab(tabs, "local")?.pane).toEqual({ sessionId: "local", remote: false });
  });

  it("declines a session that already owns its tab", () => {
    expect(planDetachToTab(tabs, "solo")).toBeNull();
  });

  it("declines a session that is not on screen, and an empty id", () => {
    expect(planDetachToTab(tabs, "ghost")).toBeNull();
    expect(planDetachToTab(tabs, "")).toBeNull();
  });

  it("copies the pane rather than aliasing it", () => {
    expect(planDetachToTab(tabs, "a")?.pane).not.toBe(tabs[0].panes[0]);
  });
});

describe("swapPanes", () => {
  const panes = [pane("a"), { sessionId: "local", remote: false }, pane("c")];

  it("trades two panes, each keeping its own flags", () => {
    const next = swapPanes(panes, 0, 1);
    expect(next[0]).toEqual({ sessionId: "local", remote: false });
    expect(next[1]).toEqual({ sessionId: "a", remote: true });
    expect(next[2]).toEqual({ sessionId: "c", remote: true });
  });

  it("leaves the input untouched", () => {
    swapPanes(panes, 0, 2);
    expect(panes.map((p) => p.sessionId)).toEqual(["a", "local", "c"]);
  });

  it("is a no-op for the same index or an out-of-range one", () => {
    expect(swapPanes(panes, 1, 1).map((p) => p.sessionId)).toEqual(["a", "local", "c"]);
    expect(swapPanes(panes, 0, 9).map((p) => p.sessionId)).toEqual(["a", "local", "c"]);
    expect(swapPanes(panes, -1, 0).map((p) => p.sessionId)).toEqual(["a", "local", "c"]);
  });
});
