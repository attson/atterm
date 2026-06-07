import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import TaskStateIcon from "./TaskStateIcon.vue";
import { presets } from "../lib/taskState";

describe("TaskStateIcon", () => {
  test("renders the glyph for a static state", () => {
    const w = mount(TaskStateIcon, {
      props: { state: "waiting_input", preset: presets.iconOnly },
    });
    expect(w.text()).toContain("◐");
    expect(w.attributes("style")).toContain("color: rgb(245, 158, 11)"); // #f59e0b
  });
  test("renders an SVG spinner for running", () => {
    const w = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.iconOnly },
    });
    expect(w.find("svg.task-spinner").exists()).toBe(true);
    expect(w.find("svg.task-spinner").attributes("style")).toContain(
      "animation-duration: 1500ms",
    );
  });
  test("both presets share spinner duration (visual differs only by label)", () => {
    const a = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.iconOnly },
    });
    const b = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.iconLabel },
    });
    expect(a.find("svg.task-spinner").attributes("style")).toContain("1500ms");
    expect(b.find("svg.task-spinner").attributes("style")).toContain("1500ms");
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
