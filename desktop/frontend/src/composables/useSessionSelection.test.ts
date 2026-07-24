import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { effectScope } from "vue";
import { __resetForTests, useSessionSelection } from "./useSessionSelection";

describe("useSessionSelection", () => {
  let scope: ReturnType<typeof effectScope>;
  beforeEach(() => {
    __resetForTests();
    scope = effectScope();
  });
  afterEach(() => scope.stop());

  test("starts empty, size=0, anchor null", () => {
    scope.run(() => {
      const s = useSessionSelection();
      expect(s.size.value).toBe(0);
      expect(s.anchorId.value).toBeNull();
      expect(s.isSelected("a")).toBe(false);
    });
  });

  test("toggle adds then removes an id and sets anchor", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("a");
      expect(s.isSelected("a")).toBe(true);
      expect(s.size.value).toBe(1);
      expect(s.anchorId.value).toBe("a");
      s.toggle("a");
      expect(s.isSelected("a")).toBe(false);
      expect(s.size.value).toBe(0);
      // anchor stays on last-touched even when set becomes empty
      expect(s.anchorId.value).toBe("a");
    });
  });

  test("selectOnly replaces existing selection and re-anchors", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("a");
      s.toggle("b");
      expect(s.size.value).toBe(2);
      s.selectOnly("c");
      expect(s.size.value).toBe(1);
      expect(s.isSelected("c")).toBe(true);
      expect(s.isSelected("a")).toBe(false);
      expect(s.anchorId.value).toBe("c");
    });
  });

  test("selectRange fills anchor→id range from orderedIds", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("b"); // anchor = b
      s.selectRange("d", ["a", "b", "c", "d", "e"]);
      expect(s.isSelected("b")).toBe(true);
      expect(s.isSelected("c")).toBe(true);
      expect(s.isSelected("d")).toBe(true);
      expect(s.isSelected("a")).toBe(false);
      expect(s.isSelected("e")).toBe(false);
      // anchor unchanged after a range select
      expect(s.anchorId.value).toBe("b");
    });
  });

  test("selectRange without anchor falls back to toggle", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.selectRange("b", ["a", "b", "c"]);
      expect(s.isSelected("b")).toBe(true);
      expect(s.size.value).toBe(1);
      expect(s.anchorId.value).toBe("b");
    });
  });

  test("selectRange with anchor missing from orderedIds falls back to toggle", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("zzz");
      s.selectRange("b", ["a", "b", "c"]);
      // "zzz" not in ordered list → fallback = toggle "b"
      expect(s.isSelected("b")).toBe(true);
      expect(s.isSelected("zzz")).toBe(true);
      expect(s.size.value).toBe(2);
    });
  });

  test("selectRange handles reversed order (id before anchor)", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("d");
      s.selectRange("b", ["a", "b", "c", "d", "e"]);
      expect(s.isSelected("b")).toBe(true);
      expect(s.isSelected("c")).toBe(true);
      expect(s.isSelected("d")).toBe(true);
      expect(s.isSelected("a")).toBe(false);
    });
  });

  test("clear() empties selection and null-anchors", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("a");
      s.toggle("b");
      s.clear();
      expect(s.size.value).toBe(0);
      expect(s.anchorId.value).toBeNull();
    });
  });

  test("state is shared across multiple useSessionSelection() calls", () => {
    scope.run(() => {
      const a = useSessionSelection();
      const b = useSessionSelection();
      a.toggle("x");
      expect(b.isSelected("x")).toBe(true);
      expect(b.size.value).toBe(1);
    });
  });

  test("insertion order is preserved", () => {
    scope.run(() => {
      const s = useSessionSelection();
      s.toggle("b");
      s.toggle("a");
      s.toggle("c");
      expect(Array.from(s.selectedIds.value)).toEqual(["b", "a", "c"]);
    });
  });
});
