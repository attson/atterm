import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import TaskStateIcon from "./TaskStateIcon.vue";
import { presets } from "../lib/taskState";

describe("TaskStateIcon", () => {
  test("renders the SVG shape for a static state", () => {
    // Icons are now pure SVG (no text glyphs) — iOS 26.3 was rendering the
    // legacy ◐ / ✓ / ✗ / · unicode symbols as .notdef "?" boxes because
    // the CJK-first font stack couldn't resolve them.
    const w = mount(TaskStateIcon, {
      props: { state: "waiting_input", preset: presets.iconOnly },
    });
    expect(w.find(`[data-state="waiting_input"]`).exists()).toBe(true);
    // Read waiting_input renders a terminal prompt.
    expect(w.find("path.task-waiting-prompt").exists()).toBe(true);
    expect(w.find("svg circle").exists()).toBe(false);
    expect(w.attributes("style")).toContain("color: rgb(245, 158, 11)"); // #f59e0b
  });
  test("renders an SVG spinner for running", () => {
    const w = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.iconOnly },
    });
    // Spinner is now a <path class="task-spinner"> inside the shared <svg>.
    expect(w.find("path.task-spinner").exists()).toBe(true);
    expect(w.find("path.task-spinner").attributes("style")).toContain(
      "animation-duration: 1500ms",
    );
  });
  // Unread is drawn as a filled version of the *same* state glyph — a solid
  // state-colored disc with the glyph knocked out in the background colour —
  // plus a state-colored corner dot. The old four-point star replaced the
  // glyph outright, which meant an unread row could no longer say whether it
  // was waiting or finished.
  test("renders a solid state disc with the glyph knocked out for unread completed sessions", () => {
    const w = mount(TaskStateIcon, {
      props: { state: "completed", unread: true, preset: presets.iconOnly },
    });
    expect(w.find('.task-state-icon[data-state="completed"][data-unread="true"]').exists()).toBe(true);
    expect(w.find("circle.task-unread-disc").attributes("fill")).toBe("#22c55e");
    // The check mark survives — knocked out in the background colour.
    expect(w.find("path.task-completed-check").attributes("stroke")).toBe("#0d1117");
    expect(w.find("path.task-unread-star").exists()).toBe(false);
  });
  test("renders a corner dot in the state colour for unread sessions", () => {
    const w = mount(TaskStateIcon, {
      props: { state: "waiting_input", unread: true, preset: presets.iconOnly },
    });
    expect(w.find("circle.task-unread-dot").attributes("fill")).toBe("#f59e0b");
    expect(w.find("path.task-waiting-prompt").attributes("stroke")).toBe("#0d1117");
    expect(w.classes()).not.toContain("pulse");
  });
  test("marks unread for every state, not just waiting and completed", () => {
    // The old rule only honoured unread on waiting_input/completed, which is
    // why TabBar had to carry a second, separate dot for the rest.
    const cases = [
      ["running", "#06b6d4"],
      ["failed", "#ef4444"],
      ["idle", "#6b7280"],
    ] as const;
    for (const [state, color] of cases) {
      const w = mount(TaskStateIcon, {
        props: { state, unread: true, preset: presets.iconOnly },
      });
      expect(w.find(`.task-state-icon[data-state="${state}"][data-unread="true"]`).exists()).toBe(true);
      expect(w.find("circle.task-unread-dot").attributes("fill")).toBe(color);
      expect(w.find("circle.task-unread-disc").attributes("fill")).toBe(color);
    }
  });
  test("renders completed check when a completed session is already read", () => {
    const w = mount(TaskStateIcon, {
      props: { state: "completed", unread: false, preset: presets.iconOnly },
    });
    expect(w.find("path.task-completed-check").exists()).toBe(true);
    expect(w.find("path.task-unread-star").exists()).toBe(false);
  });
  test("both presets share spinner duration (visual differs only by label)", () => {
    const a = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.iconOnly },
    });
    const b = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.iconLabel },
    });
    expect(a.find("path.task-spinner").attributes("style")).toContain("1500ms");
    expect(b.find("path.task-spinner").attributes("style")).toContain("1500ms");
  });
  test("waiting_input pulses in both presets", () => {
    const a = mount(TaskStateIcon, {
      props: { state: "waiting_input", preset: presets.iconOnly },
    });
    const b = mount(TaskStateIcon, {
      props: { state: "waiting_input", preset: presets.iconLabel },
    });
    expect(a.classes()).toContain("pulse");
    expect(b.classes()).toContain("pulse");
  });
  test("neither preset renders a type icon when type is provided", () => {
    const a = mount(TaskStateIcon, {
      props: { state: "running", type: "ai", preset: presets.iconOnly },
    });
    const b = mount(TaskStateIcon, {
      props: { state: "running", type: "ai", preset: presets.iconLabel },
    });
    expect(a.find("svg.task-type").exists()).toBe(false);
    expect(b.find("svg.task-type").exists()).toBe(false);
  });
});
