import { describe, it, expect } from "vitest";
import { closePane, transitionLayout } from "./layout";
import type { Pane } from "./types";

const P = (id: string): Pane => ({ sessionId: id, remote: false });
const E: Pane = { sessionId: null, remote: false };

describe("transitionLayout", () => {
  describe("from single", () => {
    it("vertical dir → vertical layout, new pane on right", () => {
      const r = transitionLayout("single", [P("a")], 0, "vertical");
      expect(r.layout).toBe("vertical");
      expect(r.panes).toHaveLength(2);
      expect(r.panes[0].sessionId).toBe("a");
      expect(r.panes[1].sessionId).toBeNull();
      expect(r.newPaneIdx).toBe(1);
      expect(r.activePaneIdx).toBe(1);
      expect(r.noop).toBeFalsy();
    });

    it("horizontal dir → horizontal layout, new pane on bottom", () => {
      const r = transitionLayout("single", [P("a")], 0, "horizontal");
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
      const r = transitionLayout("vertical", [P("a"), P("b")], 0, "vertical");
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", null, null]);
      expect(r.newPaneIdx).toBe(2);
      expect(r.activePaneIdx).toBe(2);
    });

    it("active=left + horizontal → same as vertical (direction ignored)", () => {
      const r = transitionLayout("vertical", [P("a"), P("b")], 0, "horizontal");
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", null, null]);
      expect(r.newPaneIdx).toBe(2);
    });

    it("active=right → new at idx 3 (BR)", () => {
      const r = transitionLayout("vertical", [P("a"), P("b")], 1, "vertical");
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b", null, null]);
      expect(r.newPaneIdx).toBe(3);
      expect(r.activePaneIdx).toBe(3);
    });
  });

  describe("from horizontal (direction-agnostic)", () => {
    it("active=top → grid2x2 [TL=top, BL=bottom], new at TR=1", () => {
      const r = transitionLayout("horizontal", [P("a"), P("b")], 0, "vertical");
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", null, "b", null]);
      expect(r.newPaneIdx).toBe(1);
      expect(r.activePaneIdx).toBe(1);
    });

    it("active=top + horizontal dir → same as vertical dir", () => {
      const r = transitionLayout("horizontal", [P("a"), P("b")], 0, "horizontal");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", null, "b", null]);
      expect(r.newPaneIdx).toBe(1);
    });

    it("active=bottom → new at BR=3", () => {
      const r = transitionLayout("horizontal", [P("a"), P("b")], 1, "vertical");
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
      );
      expect(r.newPaneIdx).toBe(1);
    });
  });
});

describe("closePane", () => {
  it("single → closeTab flag", () => {
    const r = closePane("single", [P("a")], 0);
    expect(r.closeTab).toBe(true);
  });

  it("vertical close left → single with right survivor", () => {
    const r = closePane("vertical", [P("a"), P("b")], 0);
    expect(r.layout).toBe("single");
    expect(r.panes).toHaveLength(1);
    expect(r.panes[0].sessionId).toBe("b");
    expect(r.activePaneIdx).toBe(0);
  });

  it("vertical close right → single with left survivor", () => {
    const r = closePane("vertical", [P("a"), P("b")], 1);
    expect(r.layout).toBe("single");
    expect(r.panes[0].sessionId).toBe("a");
  });

  it("horizontal close top → single with bottom survivor", () => {
    const r = closePane("horizontal", [P("a"), P("b")], 0);
    expect(r.layout).toBe("single");
    expect(r.panes[0].sessionId).toBe("b");
  });

  describe("grid2x2", () => {
    it("4 filled, close 1 → grid2x2 with 3 filled (one null)", () => {
      const r = closePane("grid2x2", [P("a"), P("b"), P("c"), P("d")], 0);
      expect(r.layout).toBe("grid2x2");
      expect(r.panes.map((p) => p.sessionId)).toEqual([null, "b", "c", "d"]);
      // active follows lowest filled idx
      expect(r.activePaneIdx).toBe(1);
    });

    it("3 filled (TL,TR,BL), close BL → 2 filled top row → vertical", () => {
      const r = closePane("grid2x2", [P("a"), P("b"), P("c"), E], 2);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "b"]);
      expect(r.activePaneIdx).toBe(0); // first survivor
    });

    it("3 filled (TL,BL,BR), close TL → 2 filled bottom row → vertical", () => {
      const r = closePane("grid2x2", [P("a"), E, P("c"), P("d")], 0);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["c", "d"]);
    });

    it("3 filled, after close left column remains → horizontal", () => {
      // panes: TL=a, TR=null, BL=c, BR=d → close BR → {0,2} remain → horizontal
      const r = closePane("grid2x2", [P("a"), E, P("c"), P("d")], 3);
      expect(r.layout).toBe("horizontal");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "c"]);
    });

    it("3 filled, after close right column remains → horizontal", () => {
      // panes: TL=null, TR=b, BL=c, BR=d → close BL → {1,3} remain → horizontal
      const r = closePane("grid2x2", [E, P("b"), P("c"), P("d")], 2);
      expect(r.layout).toBe("horizontal");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["b", "d"]);
    });

    it("diagonal {0,3} → vertical sorted by index", () => {
      // panes after a sequence of closes: TL=a, TR=null, BL=null, BR=d
      // Now close one of the empties (no-op session-wise) — but exercise via
      // the path: simulate by starting from {a,_,_,d} and closing idx 1 (E).
      // Closing an empty slot still routes through closePane.
      const r = closePane("grid2x2", [P("a"), E, E, P("d")], 1);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["a", "d"]);
    });

    it("diagonal {1,2} → vertical sorted by index", () => {
      const r = closePane("grid2x2", [E, P("b"), P("c"), E], 0);
      expect(r.layout).toBe("vertical");
      expect(r.panes.map((p) => p.sessionId)).toEqual(["b", "c"]);
    });

    it("closing the last filled pane → closeTab signal", () => {
      const r = closePane("grid2x2", [P("a"), E, E, E], 0);
      expect(r.closeTab).toBe(true);
      expect(r.layout).toBe("single");
      expect(r.panes[0].sessionId).toBeNull();
    });

    it("closing the last filled (different slot) → still closeTab", () => {
      const r = closePane("grid2x2", [E, P("b"), E, E], 1);
      expect(r.closeTab).toBe(true);
    });
  });
});
