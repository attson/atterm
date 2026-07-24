import { describe, expect, it } from "vitest";
import { applyTabReorder } from "./lib/tabReorder";

interface T { id: string }

describe("applyTabReorder", () => {
  const base = (): T[] => [{ id: "a" }, { id: "b" }, { id: "c" }, { id: "d" }];

  it("moves a tab after a later target", () => {
    expect(applyTabReorder(base(), "a", "c", "after").map((t) => t.id))
      .toEqual(["b", "c", "a", "d"]);
  });

  it("moves a tab before a later target", () => {
    expect(applyTabReorder(base(), "a", "c", "before").map((t) => t.id))
      .toEqual(["b", "a", "c", "d"]);
  });

  it("moves a tab before an earlier target", () => {
    expect(applyTabReorder(base(), "d", "b", "before").map((t) => t.id))
      .toEqual(["a", "d", "b", "c"]);
  });

  it("moves a tab after an earlier target", () => {
    expect(applyTabReorder(base(), "d", "b", "after").map((t) => t.id))
      .toEqual(["a", "b", "d", "c"]);
  });

  it("is a no-op when fromId equals targetId", () => {
    expect(applyTabReorder(base(), "b", "b", "after").map((t) => t.id))
      .toEqual(["a", "b", "c", "d"]);
  });

  it("is a no-op when fromId is missing", () => {
    expect(applyTabReorder(base(), "zz", "b", "after").map((t) => t.id))
      .toEqual(["a", "b", "c", "d"]);
  });

  it("appends when targetId is missing (target disappeared mid-drag)", () => {
    expect(applyTabReorder(base(), "a", "zz", "after").map((t) => t.id))
      .toEqual(["b", "c", "d", "a"]);
  });
});
