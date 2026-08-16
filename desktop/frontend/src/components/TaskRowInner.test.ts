import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import TaskRowInner from "./TaskRowInner.vue";
import source from "./TaskRowInner.vue?raw";
import type { RemoteSession } from "../platform/types";

const NOW = 1_800_000_000_000;

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
  return mount(TaskRowInner, { props: { session: mk(session), showClose, nowMs: NOW } });
}

describe("TaskRowInner right-hand affordances", () => {
  // Unread is carried by the state icon (star), by unread-first sorting, and by
  // the group header's count + "mark all read". The row itself no longer spends
  // ~22px of a ~224px width restating it, and attaching to a session clears
  // unread on its own, so there is nothing per-row left to click.
  test("an unread row carries no dot and no mark-read control", () => {
    const w = factory({ unread: true, title: "build", task_state: "failed" });
    expect(w.find('[data-test="unread-dot"]').exists()).toBe(false);
    expect(w.find('[data-test="row-mark-read"]').exists()).toBe(false);
  });

  test("an unread completed row fills the state icon and keeps the check mark", () => {
    const w = factory({ unread: true, task_state: "completed", title: "build" });
    expect(w.find('.task-state-icon[data-state="completed"][data-unread="true"]').exists()).toBe(true);
    expect(w.find("circle.task-unread-disc").attributes("fill")).toBe("#22c55e");
    // The glyph survives, knocked out of the disc — an unread row still says
    // *which* state it is, which the old star could not.
    expect(w.find("path.task-completed-check").exists()).toBe(true);
  });

  test("an unread waiting row keeps its prompt glyph and adds no second control", () => {
    const w = factory({ unread: true, task_state: "waiting_input", title: "build" });
    expect(w.find('.task-state-icon[data-state="waiting_input"][data-unread="true"]').exists()).toBe(true);
    expect(w.find("path.task-waiting-prompt").exists()).toBe(true);
    expect(w.find("circle.task-unread-dot").attributes("fill")).toBe("#f59e0b");
    expect(w.find('[data-test="row-mark-read"]').exists()).toBe(false);
  });

  test("a read completed row shows the completed state icon", () => {
    const w = factory({ unread: false, task_state: "completed", title: "build" });
    expect(w.find('.task-state-icon[data-state="completed"][data-unread="true"]').exists()).toBe(false);
    expect(w.find("path.task-completed-check").exists()).toBe(true);
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
  // The close × is absolute again, but for a different reason than the version
  // that got reverted: back then it floated over the title line and the row
  // reserved a permanent 32px gutter for it. Now it sits in the tail column's
  // kind-badge slot and the badge yields while × is showing, so it costs no
  // layout width and overlaps nothing.
  test("the close button overlays the tail rather than claiming a column", () => {
    expect(source).toMatch(/\.row-close\s*\{[^}]*position\s*:\s*absolute/);
  });

  test("the title line no longer reserves padding for an overlapping close button", () => {
    expect(source).not.toMatch(/\.row-top\.has-close\s*\{[^}]*padding-right/);
  });

  test("the tail holds the slot open even with no kind and no timestamp", () => {
    const w = factory({ cwd: "/tmp", last_output_at: 0 });
    expect(w.find('[data-test="row-tail"]').exists()).toBe(true);
    expect(w.find('[data-test="row-kind"]').exists()).toBe(false);
    expect(w.find('[data-test="last-output"]').exists()).toBe(false);
    // min-width, not width: a fixed tail would take the slack out of a title
    // that is already ellipsising.
    expect(source).toMatch(/\.row-tail\s*\{[^}]*min-width:\s*46px/);
  });

  test("names the CLI for ai sessions only", () => {
    const ai = factory({ type: "ai", current_command: "claude --resume", cwd: "/tmp" });
    expect(ai.get('[data-test="row-kind"]').text()).toBe("claude");
    const shell = factory({ type: "shell", current_command: "zsh", cwd: "/tmp" });
    expect(shell.find('[data-test="row-kind"]').exists()).toBe(false);
  });

  test("stacks kind above last output in the tail, matching the desk widget", () => {
    const w = factory({
      type: "ai",
      current_command: "codex",
      cwd: "/tmp",
      task_state: "idle",
      last_output_at: NOW / 1000 - 60,
    });
    const tail = w.get('[data-test="row-tail"]');
    expect(tail.get('[data-test="row-kind"]').text()).toBe("codex");
    expect(tail.get('[data-test="last-output"]').text()).toBe("1m");
    expect(tail.classes()).not.toContain("time-only");
  });

  // Without a badge to pin to the top, space-between would float the lone
  // timestamp up onto the title line.
  test("drops the timestamp to the cwd line when there is no kind badge", () => {
    const w = factory({ cwd: "/tmp", task_state: "idle", last_output_at: NOW / 1000 - 60 });
    expect(w.get('[data-test="row-tail"]').classes()).toContain("time-only");
    expect(source).toMatch(/\.row-tail\.time-only\s*\{[^}]*justify-content:\s*flex-end/);
  });

  test("the truncated title carries the full text as a tooltip", () => {
    const w = factory({ title: "a very long session title that will not fit", cwd: "/tmp" });
    expect(w.find(".cmd-and-cwd").attributes("title")).toContain(
      "a very long session title that will not fit",
    );
  });

  test("keeps cwd flexible and last output out of its way in the tail", () => {
    const w = factory({
      title: "a very long title",
      cwd: "/a/very/long/path/that/must/ellipsis",
      task_state: "idle",
      last_output_at: NOW / 1000 - 17 * 60,
    });
    expect(w.get('[data-test="row-meta"]').get('[data-test="row-cwd"]').text()).toContain("ellipsis");
    // The timestamp is no longer a sibling competing with cwd for the line.
    expect(w.get('[data-test="row-meta"]').find('[data-test="last-output"]').exists()).toBe(false);
    expect(w.get('[data-test="row-tail"]').get('[data-test="last-output"]').text()).toBe("17m");
    expect(source).toMatch(/\.cwd\s*\{[^}]*flex:\s*1 1 auto/);
  });

  test("shows last output when cwd is empty", () => {
    const w = factory({ cwd: "", task_state: "idle", last_output_at: NOW / 1000 - 60 });
    expect(w.find('[data-test="row-cwd"]').exists()).toBe(false);
    expect(w.get('[data-test="last-output"]').text()).toBe("1m");
  });

  test("hides the cwd line when there is no cwd", () => {
    const w = factory({ cwd: "", last_output_at: 0 });
    expect(w.find('[data-test="row-meta"]').exists()).toBe(false);
  });

  test("renders live with accessible output status", () => {
    const w = factory({ task_state: "running", last_output_at: NOW / 1000 - 1 });
    const indicator = w.get('[data-test="last-output"]');
    expect(indicator.text()).toBe("live");
    expect(indicator.classes()).toContain("live");
    expect(indicator.attributes("aria-label")).toBe("Output active");
  });
});
