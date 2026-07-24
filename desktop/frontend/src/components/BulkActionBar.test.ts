import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import BulkActionBar from "./BulkActionBar.vue";

function factory(overrides: {
  count?: number;
  openCount?: number;
  canMerge?: boolean;
} = {}) {
  return mount(BulkActionBar, {
    props: {
      count: 1,
      openCount: 0,
      canMerge: true,
      ...overrides,
    },
  });
}

describe("BulkActionBar", () => {
  test("renders count in the counter label", () => {
    const w = factory({ count: 3 });
    expect(w.find("[data-test=bulk-selected-count]").text()).toContain("3");
  });

  test("merge button enabled when canMerge=true", () => {
    const w = factory({ count: 2, canMerge: true });
    const btn = w.find("[data-test=bulk-merge]");
    expect((btn.element as HTMLButtonElement).disabled).toBe(false);
  });

  test("merge button disabled when canMerge=false", () => {
    const w = factory({ count: 5, canMerge: false });
    const btn = w.find("[data-test=bulk-merge]");
    expect((btn.element as HTMLButtonElement).disabled).toBe(true);
  });

  test("close button disabled when openCount=0", () => {
    const w = factory({ openCount: 0 });
    const btn = w.find("[data-test=bulk-close]");
    expect((btn.element as HTMLButtonElement).disabled).toBe(true);
  });

  test("close button enabled when openCount>0", () => {
    const w = factory({ openCount: 2 });
    const btn = w.find("[data-test=bulk-close]");
    expect((btn.element as HTMLButtonElement).disabled).toBe(false);
  });

  test("clicking merge emits 'merge'", async () => {
    const w = factory({ count: 2, canMerge: true });
    await w.find("[data-test=bulk-merge]").trigger("click");
    expect(w.emitted("merge")).toHaveLength(1);
  });

  test("clicking close emits 'close-selected'", async () => {
    const w = factory({ openCount: 2 });
    await w.find("[data-test=bulk-close]").trigger("click");
    expect(w.emitted("close-selected")).toHaveLength(1);
  });

  test("clicking cancel emits 'clear'", async () => {
    const w = factory();
    await w.find("[data-test=bulk-clear]").trigger("click");
    expect(w.emitted("clear")).toHaveLength(1);
  });

  test("disabled merge does not emit", async () => {
    const w = factory({ count: 5, canMerge: false });
    await w.find("[data-test=bulk-merge]").trigger("click");
    expect(w.emitted("merge")).toBeUndefined();
  });
});
