import { describe, it, expect } from "vitest";
import {
  RATIO_DEFAULT,
  RATIO_MAX,
  RATIO_MIN,
  clampRatio,
  closePane,
  findPaneLocation,
  focusNeighbor,
  transitionLayout,
} from "./layout";
import type { Pane } from "./types";

const P = (id: string): Pane => ({ sessionId: id, remote: false });
const E: Pane = { sessionId: null, remote: false };
const D = RATIO_DEFAULT;

describe("transitionLayout", () => {
  describe("from single", () => {
    it("vertical dir → vertical layout, new pane on right", () => {
      const r = transitionLayout("single", [P("a")], 0, "vertical", D, D);
      expect(r.layout).toBe("vertical");
      expect(r.panes).toHaveLength(2);
      expect(r.panes[0].sessionId).toBe("a");
      expect(r.panes[1].sessionId).toBeNull();
      expect(r.newPaneIdx).toBe(1);
      expect(r.activePaneIdx).toBe(1);
      expect(r.noop).toBeFalsy();
    });

    it("horizontal dir → horizontal layout, new pane on bottom", () => {
      const r = transitionLayout("single", [P("a")], 0, "horizontal", D, D);
      expect(r.layout).toBe("horizontal");
      expect(r.panes).toHaveLength(2);
      expect(r.panes[0].sessionId).toBe("a");
      expect(r.panes[1].sessionId).toBeNull();
      expect(r.newPaneIdx).toBe(1);
      expect(r.activePaneIdx).toBe(1);
    });
  });

  describe("from vertical (direction-agnostic)", () => {
    it("active=left + vertical → grid2x2, new at idx 2 (BL)", () => {
      const r = transitionLayout("vertical", [P("a"), P("b")], 0, "vertical", D, D);
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", null, null]);
      expect(r.newPaneIdx).toBe(2);
      expect(r.activePaneIdx).toBe(2);
    });

    it("active=left + horizontal → same as vertical (direction ignored)", () => {
      const r = transitionLayout("vertical", [P("a"), P("b")], 0, "horizontal", D, D);
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", null, null]);
      expect(r.newPaneIdx).toBe(2);
    });

    it("active=right → new at idx 3 (BR)", () => {
      const r = transitionLayout("vertical", [P("a"), P("b")], 1, "vertical", D, D);
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", null, null]);
      expect(r.newPaneIdx).toBe(3);
      expect(r.activePaneIdx).toBe(3);
    });
  });

  describe("from horizontal (direction-agnostic)", () => {
    it("active=top → grid2x2 [TL=top, BL=bottom], new at TR=1", () => {
      const r = transitionLayout("horizontal", [P("a"), P("b")], 0, "vertical", D, D);
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", null, "b", null]);
      expect(r.newPaneIdx).toBe(1);
      expect(r.activePaneIdx).toBe(1);
    });

    it("active=top + horizontal dir → same as vertical dir", () => {
      const r = transitionLayout("horizontal", [P("a"), P("b")], 0, "horizontal", D, D);
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", null, "b", null]);
      expect(r.newPaneIdx).toBe(1);
    });

    it("active=bottom → new at BR=3", () => {
      const r = transitionLayout("horizontal", [P("a"), P("b")], 1, "vertical", D, D);
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", null, "b", null]);
      expect(r.newPaneIdx).toBe(3);
      expect(r.activePaneIdx).toBe(3);
    });
  });

  describe("from grid2x2", () => {
    it("all 4 filled → noop", () => {
      const r = transitionLayout(
        "grid2x2",
        [P("a"), P("b"), P("c"), P("d")],
        0,
        "vertical",
        D,
        D,
      );
      expect(r.noop).toBe(true);
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", "c", "d"]);
      expect(r.newPaneIdx).toBe(-1);
    });

    it("with empty slot → fill lowest-idx empty, set active to it", () => {
      const r = transitionLayout(
        "grid2x2",
        [P("a"), P("b"), E, P("d")],
        0,
        "vertical",
        D,
        D,
      );
      expect(r.noop).toBeFalsy();
      expect(r.newPaneIdx).toBe(2);
      expect(r.activePaneIdx).toBe(2);
      expect(r.panes[2].sessionId).toBeNull(); // still empty, caller fills
    });

    it("with multiple empty slots → fill the lowest", () => {
      const r = transitionLayout(
        "grid2x2",
        [P("a"), E, E, P("d")],
        3,
        "horizontal",
        D,
        D,
      );
      expect(r.newPaneIdx).toBe(1);
    });
  });
});

describe("closePane", () => {
  it("single → closeTab flag", () => {
    const r = closePane("single", [P("a")], 0, D, D);
    expect(r.closeTab).toBe(true);
  });

  it("vertical close left → single with right survivor", () => {
    const r = closePane("vertical", [P("a"), P("b")], 0, D, D);
    expect(r.layout).toBe("single");
    expect(r.panes).toHaveLength(1);
    expect(r.panes[0].sessionId).toBe("b");
    expect(r.activePaneIdx).toBe(0);
  });

  it("vertical close right → single with left survivor", () => {
    const r = closePane("vertical", [P("a"), P("b")], 1, D, D);
    expect(r.layout).toBe("single");
    expect(r.panes[0].sessionId).toBe("a");
  });

  it("horizontal close top → single with bottom survivor", () => {
    const r = closePane("horizontal", [P("a"), P("b")], 0, D, D);
    expect(r.layout).toBe("single");
    expect(r.panes[0].sessionId).toBe("b");
  });

  describe("grid2x2", () => {
    it("4 filled, close 1 → grid2x2 with 3 filled (one null)", () => {
      const r = closePane("grid2x2", [P("a"), P("b"), P("c"), P("d")], 0, D, D);
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual([null, "b", "c", "d"]);
      // active follows lowest filled idx
      expect(r.activePaneIdx).toBe(1);
    });

    it("3 filled (TL,TR,BL), close BL → 2 filled top row → vertical", () => {
      const r = closePane("grid2x2", [P("a"), P("b"), P("c"), E], 2, D, D);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b"]);
      expect(r.activePaneIdx).toBe(0); // first survivor
    });

    it("3 filled (TL,BL,BR), close TL → 2 filled bottom row → vertical", () => {
      const r = closePane("grid2x2", [P("a"), E, P("c"), P("d")], 0, D, D);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["c", "d"]);
    });

    it("3 filled, after close left column remains → horizontal", () => {
      // panes: TL=a, TR=null, BL=c, BR=d → close BR → {0,2} remain → horizontal
      const r = closePane("grid2x2", [P("a"), E, P("c"), P("d")], 3, D, D);
      expect(r.layout).toBe("horizontal");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "c"]);
    });

    it("3 filled, after close right column remains → horizontal", () => {
      // panes: TL=null, TR=b, BL=c, BR=d → close BL → {1,3} remain → horizontal
      const r = closePane("grid2x2", [E, P("b"), P("c"), P("d")], 2, D, D);
      expect(r.layout).toBe("horizontal");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["b", "d"]);
    });

    it("diagonal {0,3} → vertical sorted by index", () => {
      // panes after a sequence of closes: TL=a, TR=null, BL=null, BR=d
      // Now close one of the empties (no-op session-wise) — but exercise via
      // the path: simulate by starting from {a,_,_,d} and closing idx 1 (E).
      // Closing an empty slot still routes through closePane.
      const r = closePane("grid2x2", [P("a"), E, E, P("d")], 1, D, D);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "d"]);
    });

    it("diagonal {1,2} → vertical sorted by index", () => {
      const r = closePane("grid2x2", [E, P("b"), P("c"), E], 0, D, D);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["b", "c"]);
    });

    it("closing the last filled pane → closeTab signal", () => {
      const r = closePane("grid2x2", [P("a"), E, E, E], 0, D, D);
      expect(r.closeTab).toBe(true);
      expect(r.layout).toBe("single");
      expect(r.panes[0].sessionId).toBeNull();
    });

    it("closing the last filled (different slot) → still closeTab", () => {
      const r = closePane("grid2x2", [E, P("b"), E, E], 1, D, D);
      expect(r.closeTab).toBe(true);
    });
  });
});

describe("focusNeighbor", () => {
  it("single → all dirs return null", () => {
    expect(focusNeighbor("single", 0, "left")).toBeNull();
    expect(focusNeighbor("single", 0, "right")).toBeNull();
    expect(focusNeighbor("single", 0, "up")).toBeNull();
    expect(focusNeighbor("single", 0, "down")).toBeNull();
  });

  it("vertical: 0 ↔ 1 horizontal only", () => {
    expect(focusNeighbor("vertical", 0, "right")).toBe(1);
    expect(focusNeighbor("vertical", 1, "left")).toBe(0);
    expect(focusNeighbor("vertical", 0, "left")).toBeNull();
    expect(focusNeighbor("vertical", 0, "up")).toBeNull();
    expect(focusNeighbor("vertical", 0, "down")).toBeNull();
  });

  it("horizontal: 0 ↔ 1 vertical only", () => {
    expect(focusNeighbor("horizontal", 0, "down")).toBe(1);
    expect(focusNeighbor("horizontal", 1, "up")).toBe(0);
    expect(focusNeighbor("horizontal", 0, "left")).toBeNull();
    expect(focusNeighbor("horizontal", 0, "right")).toBeNull();
  });

  it("grid2x2: every quadrant has the right two neighbors", () => {
    // 0 (TL): right=1, down=2
    expect(focusNeighbor("grid2x2", 0, "right")).toBe(1);
    expect(focusNeighbor("grid2x2", 0, "down")).toBe(2);
    expect(focusNeighbor("grid2x2", 0, "left")).toBeNull();
    expect(focusNeighbor("grid2x2", 0, "up")).toBeNull();
    // 1 (TR): left=0, down=3
    expect(focusNeighbor("grid2x2", 1, "left")).toBe(0);
    expect(focusNeighbor("grid2x2", 1, "down")).toBe(3);
    // 2 (BL): up=0, right=3
    expect(focusNeighbor("grid2x2", 2, "up")).toBe(0);
    expect(focusNeighbor("grid2x2", 2, "right")).toBe(3);
    // 3 (BR): up=1, left=2
    expect(focusNeighbor("grid2x2", 3, "up")).toBe(1);
    expect(focusNeighbor("grid2x2", 3, "left")).toBe(2);
  });
});

describe("ratio threading", () => {
  it("transitionLayout passes through colRatio/rowRatio", () => {
    const r = transitionLayout("single", [P("a")], 0, "vertical", 0.3, 0.7);
    expect(r.colRatio).toBe(0.3);
    expect(r.rowRatio).toBe(0.7);
  });

  it("transitionLayout single→horizontal keeps both ratios", () => {
    const r = transitionLayout("single", [P("a")], 0, "horizontal", 0.4, 0.6);
    expect(r.colRatio).toBe(0.4);
    expect(r.rowRatio).toBe(0.6);
  });

  it("transitionLayout vertical→grid2x2 keeps both ratios", () => {
    const r = transitionLayout(
      "vertical",
      [P("a"), P("b")],
      0,
      "vertical",
      0.25,
      0.75,
    );
    expect(r.layout).toBe("grid2x2");
    expect(r.colRatio).toBe(0.25);
    expect(r.rowRatio).toBe(0.75);
  });

  it("transitionLayout grid2x2 noop keeps ratios", () => {
    const r = transitionLayout(
      "grid2x2",
      [P("a"), P("b"), P("c"), P("d")],
      0,
      "vertical",
      0.4,
      0.6,
    );
    expect(r.noop).toBe(true);
    expect(r.colRatio).toBe(0.4);
    expect(r.rowRatio).toBe(0.6);
  });

  it("closePane grid2x2→vertical keeps both ratios", () => {
    const r = closePane(
      "grid2x2",
      [P("a"), P("b"), E, E],
      2,
      0.3,
      0.7,
    );
    expect(r.layout).toBe("vertical");
    expect(r.colRatio).toBe(0.3);
    expect(r.rowRatio).toBe(0.7);
  });

  it("closePane vertical→single keeps both ratios", () => {
    const r = closePane("vertical", [P("a"), P("b")], 1, 0.2, 0.8);
    expect(r.layout).toBe("single");
    expect(r.colRatio).toBe(0.2);
    expect(r.rowRatio).toBe(0.8);
  });

  it("closePane single→closeTab keeps both ratios", () => {
    const r = closePane("single", [P("a")], 0, 0.15, 0.85);
    expect(r.closeTab).toBe(true);
    expect(r.colRatio).toBe(0.15);
    expect(r.rowRatio).toBe(0.85);
  });

  it("ratio constants are exported with expected values", () => {
    expect(RATIO_DEFAULT).toBe(0.5);
    expect(RATIO_MIN).toBe(0.1);
    expect(RATIO_MAX).toBe(0.9);
  });

  describe("clampRatio", () => {
    it("returns value inside [MIN, MAX]", () => {
      expect(clampRatio(0.5)).toBe(0.5);
      expect(clampRatio(0.3)).toBe(0.3);
    });
    it("clamps below MIN", () => {
      expect(clampRatio(-0.5)).toBe(RATIO_MIN);
      expect(clampRatio(0.05)).toBe(RATIO_MIN);
    });
    it("clamps above MAX", () => {
      expect(clampRatio(1.2)).toBe(RATIO_MAX);
      expect(clampRatio(0.95)).toBe(RATIO_MAX);
    });
    it("falls back to default on NaN/Infinity", () => {
      expect(clampRatio(Number.NaN)).toBe(RATIO_DEFAULT);
      expect(clampRatio(Number.POSITIVE_INFINITY)).toBe(RATIO_DEFAULT);
    });
  });
});

describe("findPaneLocation", () => {
  const tab = (id: string, ...sids: (string | null)[]) => ({
    id,
    panes: sids.map((sessionId) => ({ sessionId })),
  });

  it("returns null for an empty tab list", () => {
    expect(findPaneLocation([], "anything")).toBeNull();
  });

  it("returns null when no pane holds the session", () => {
    expect(findPaneLocation([tab("t1", "a", "b")], "missing")).toBeNull();
  });

  it("finds a session in a single-pane tab at idx 0", () => {
    expect(findPaneLocation([tab("t1", "a")], "a")).toEqual({
      tabId: "t1",
      paneIdx: 0,
    });
  });

  it("finds a session in the second pane of a multi-pane tab", () => {
    expect(findPaneLocation([tab("t1", "a", "b")], "b")).toEqual({
      tabId: "t1",
      paneIdx: 1,
    });
  });

  it("returns the first matching tab when the session appears in multiple", () => {
    // openRemoteAsTab dedupes at insert time, so this case shouldn't happen
    // in practice — but the contract is "first match wins" for determinism.
    expect(findPaneLocation([tab("t1", "x"), tab("t2", "x")], "x")).toEqual({
      tabId: "t1",
      paneIdx: 0,
    });
  });

  it("skips empty (null sessionId) pane slots", () => {
    expect(findPaneLocation([tab("t1", null, "found")], "found")).toEqual({
      tabId: "t1",
      paneIdx: 1,
    });
  });
});
