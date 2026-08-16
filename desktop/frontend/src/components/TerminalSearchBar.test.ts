import { describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import TerminalSearchBar from "./TerminalSearchBar.vue";

function factory(props: Partial<{
  open: boolean;
  focusSeq: number;
  resultIndex: number;
  resultCount: number;
}> = {}) {
  return mount(TerminalSearchBar, {
    attachTo: document.body,
    props: {
      open: true,
      focusSeq: 1,
      resultIndex: -1,
      resultCount: 0,
      ...props,
    },
  });
}

describe("TerminalSearchBar", () => {
  test("renders nothing when closed", () => {
    const w = factory({ open: false });
    expect(w.find('[data-test="terminal-search"]').exists()).toBe(false);
  });

  test("renders the input when open", () => {
    const w = factory();
    expect(w.find('[data-test="terminal-search-input"]').exists()).toBe(true);
  });

  test("typing emits an incremental next-direction find", async () => {
    const w = factory();
    const input = w.find('[data-test="terminal-search-input"]');
    await input.setValue("needle");

    const events = w.emitted("find");
    expect(events).toBeTruthy();
    expect(events!.at(-1)).toEqual(["needle", "next", true]);
  });

  test("Enter emits a non-incremental next find", async () => {
    const w = factory();
    await w.find('[data-test="terminal-search-input"]').setValue("needle");
    await w.find('[data-test="terminal-search-input"]').trigger("keydown", { key: "Enter" });

    expect(w.emitted("find")!.at(-1)).toEqual(["needle", "next", false]);
  });

  test("Shift+Enter emits a non-incremental prev find", async () => {
    const w = factory();
    await w.find('[data-test="terminal-search-input"]').setValue("needle");
    await w.find('[data-test="terminal-search-input"]').trigger("keydown", { key: "Enter", shiftKey: true });

    expect(w.emitted("find")!.at(-1)).toEqual(["needle", "prev", false]);
  });

  test("the next and prev buttons emit non-incremental finds", async () => {
    const w = factory({ resultIndex: 0, resultCount: 3 });
    await w.find('[data-test="terminal-search-input"]').setValue("needle");

    await w.find('[data-test="terminal-search-next"]').trigger("click");
    expect(w.emitted("find")!.at(-1)).toEqual(["needle", "next", false]);

    await w.find('[data-test="terminal-search-prev"]').trigger("click");
    expect(w.emitted("find")!.at(-1)).toEqual(["needle", "prev", false]);
  });

  test("Escape emits close", async () => {
    const w = factory();
    await w.find('[data-test="terminal-search-input"]').trigger("keydown", { key: "Escape" });
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("the close button emits close", async () => {
    const w = factory();
    await w.find('[data-test="terminal-search-close"]').trigger("click");
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("shows a one-based position over the total", () => {
    const w = factory({ resultIndex: 2, resultCount: 12 });
    expect(w.find('[data-test="terminal-search-count"]').text()).toBe("3/12");
  });

  test("shows the no-results label once a query has been typed", async () => {
    const w = factory({ resultIndex: -1, resultCount: 0 });
    await w.find('[data-test="terminal-search-input"]').setValue("nope");
    expect(w.find('[data-test="terminal-search-count"]').text()).toBe("No results");
  });

  test("shows no counter while the query is still empty", () => {
    const w = factory({ resultIndex: -1, resultCount: 0 });
    expect(w.find('[data-test="terminal-search-count"]').text()).toBe("");
  });

  test("disables navigation buttons when there are no matches", () => {
    const w = factory({ resultIndex: -1, resultCount: 0 });
    expect(w.find('[data-test="terminal-search-next"]').attributes("disabled")).toBeDefined();
    expect(w.find('[data-test="terminal-search-prev"]').attributes("disabled")).toBeDefined();
  });

  test("focuses and selects the input when focusSeq changes while open", async () => {
    const w = factory({ focusSeq: 1 });
    const el = w.find('[data-test="terminal-search-input"]').element as HTMLInputElement;
    el.value = "old";
    const focus = vi.spyOn(el, "focus");
    const select = vi.spyOn(el, "select");

    await w.setProps({ focusSeq: 2 });
    await w.vm.$nextTick();

    expect(focus).toHaveBeenCalled();
    expect(select).toHaveBeenCalled();
  });

  test("reopening with a retained query re-runs the search instead of showing a stale no-results state", async () => {
    // Regression: `query` is a component-internal ref that survives a close
    // (v-if only tears down the DOM), but the parent resets its counters on
    // close. Without re-emitting `find` on reopen, the bar would show "no
    // results" for a query that still has matches in the scrollback.
    const w = factory({ open: true });
    await w.find('[data-test="terminal-search-input"]').setValue("needle");

    await w.setProps({ open: false });
    const findsBeforeReopen = w.emitted("find")?.length ?? 0;

    await w.setProps({ open: true });
    await w.vm.$nextTick();

    const events = w.emitted("find");
    expect(events).toBeTruthy();
    expect(events!.length).toBeGreaterThan(findsBeforeReopen);
    expect(events!.at(-1)).toEqual(["needle", "next", true]);
  });

  test("shows a capped total with a plus suffix once the addon's highlight limit is exceeded", () => {
    // xterm-addon-search caps at `highlightLimit` (default 1000): past that,
    // resultIndex becomes -1 while resultCount reports the capped count, not
    // the true total. Rendering resultIndex + 1 would show "0/1000".
    const w = factory({ resultIndex: -1, resultCount: 1000 });
    expect(w.find('[data-test="terminal-search-count"]').text()).toBe("1000+");
  });
});
