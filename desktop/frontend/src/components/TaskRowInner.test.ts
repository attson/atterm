import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import TaskRowInner from "./TaskRowInner.vue";
import source from "./TaskRowInner.vue?raw";
import type { RemoteSession } from "../platform/types";

function mk(over: Partial<RemoteSession>): RemoteSession {
  return {
    session_id: "s",
    host_id: "h",
    host: "host",
    user: "u",
    title: "",
    cols: 80,
    rows: 24,
    ...over,
  } as RemoteSession;
}

function factory(session: Partial<RemoteSession>, showClose = false) {
  return mount(TaskRowInner, { props: { session: mk(session), showClose } });
}

describe("TaskRowInner right-hand affordances", () => {
  // The row is ~224px wide in a default sidebar. A separate unread dot, a
  // mark-read check, and a close × left the title only ~55% of that, so the
  // dot and the check are now one control.
  test("an unread row exposes a single mark-read control", () => {
    const w = factory({ unread: true, title: "build" });
    expect(w.findAll('[data-test="row-mark-read"]')).toHaveLength(1);
  });

  test("the unread dot lives inside the mark-read control, not beside it", () => {
    const w = factory({ unread: true, title: "build" });
    const button = w.find('[data-test="row-mark-read"]');
    expect(button.find('[data-test="unread-dot"]').exists()).toBe(true);
  });

  test("clicking the merged control emits markRead", async () => {
    const w = factory({ unread: true, title: "build" });
    await w.find('[data-test="row-mark-read"]').trigger("click");
    expect(w.emitted("markRead")).toHaveLength(1);
  });

  test("a read row shows neither the dot nor the mark-read control", () => {
    const w = factory({ unread: false, title: "build" });
    expect(w.find('[data-test="unread-dot"]').exists()).toBe(false);
    expect(w.find('[data-test="row-mark-read"]').exists()).toBe(false);
  });

  test("the close button still emits close", async () => {
    const w = factory({ title: "build" }, true);
    await w.find('[data-test="row-close"]').trigger("click");
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("no close button when the session is not open as a pane", () => {
    const w = factory({ title: "build" }, false);
    expect(w.find('[data-test="row-close"]').exists()).toBe(false);
  });
});

describe("TaskRowInner layout", () => {
  test("the close button sits in normal flow so it aligns with the mark-read control", () => {
    // Previously absolute at the card's top-right, which put it on a different
    // baseline than the in-flow check beside it and left a dead gap between.
    expect(source).not.toMatch(/\.row-close\s*\{[^}]*position\s*:\s*absolute/);
  });

  test("the title line no longer reserves padding for an overlapping close button", () => {
    expect(source).not.toMatch(/\.row-top\.has-close\s*\{[^}]*padding-right/);
  });

  test("the truncated title carries the full text as a tooltip", () => {
    const w = factory({ title: "a very long session title that will not fit", cwd: "/tmp" });
    expect(w.find(".cmd-and-cwd").attributes("title")).toContain(
      "a very long session title that will not fit",
    );
  });
});
