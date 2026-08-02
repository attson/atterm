import { describe, expect, test } from "vitest";
import source from "./TaskRowInner.vue?raw";

describe("TaskRowInner close affordance", () => {
  test("pins the close button to the outer card's top-right corner with reserved text space", () => {
    // .row-top must NOT be a positioning ancestor — otherwise `.row-close`
    // anchors to the first line only and drifts inward from the true card
    // corner. Positioning is delegated to the outer `.task-row` (in
    // TaskGroupedList.vue), which spans both title and cwd rows.
    expect(source).not.toMatch(/\.row-top\s*\{[^}]*position\s*:\s*relative/);
    expect(source).toMatch(/\.row-top\.has-close\s*\{[^}]*padding-right\s*:\s*24px/);
    expect(source).toMatch(/\.row-close\s*\{[^}]*position\s*:\s*absolute/);
    expect(source).toMatch(/\.row-close\s*\{[^}]*top\s*:\s*0/);
    expect(source).toMatch(/\.row-close\s*\{[^}]*right\s*:\s*0/);
  });
});
