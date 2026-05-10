import { describe, it, expect } from "vitest";
import { transitionLayout } from "./layout";
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
