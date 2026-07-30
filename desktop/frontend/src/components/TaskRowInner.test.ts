import { describe, expect, test } from "vitest";
import source from "./TaskRowInner.vue?raw";

describe("TaskRowInner close affordance", () => {
  test("pins the close button to the row's top-right corner with reserved text space", () => {
    expect(source).toMatch(/\.row-top\s*\{[^}]*position\s*:\s*relative/);
    expect(source).toMatch(/\.row-top\.has-close\s*\{[^}]*padding-right\s*:\s*24px/);
    expect(source).toMatch(/\.row-close\s*\{[^}]*position\s*:\s*absolute/);
    expect(source).toMatch(/\.row-close\s*\{[^}]*top\s*:\s*0/);
    expect(source).toMatch(/\.row-close\s*\{[^}]*right\s*:\s*0/);
  });
});
