import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import TaskStateIcon from "./TaskStateIcon.vue";
import { presets } from "../lib/taskState";

describe("TaskStateIcon", () => {
  test("renders the glyph for a static state under vivid", () => {
    const w = mount(TaskStateIcon, {
      props: { state: "waiting_input", preset: presets.vivid },
    });
    expect(w.text()).toContain("◐");
    expect(w.attributes("style")).toContain("color: rgb(245, 158, 11)"); // #f59e0b
  });
  test("renders an SVG spinner for running", () => {
    const w = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.vivid },
    });
    expect(w.find("svg.task-spinner").exists()).toBe(true);
    expect(w.find("svg.task-spinner").attributes("style")).toContain(
      "animation-duration: 1500ms",
    );
  });
  test("running spinner duration differs between presets", () => {
    const v = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.vivid },
    });
    const q = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.quiet },
    });
    expect(v.find("svg.task-spinner").attributes("style")).toContain("1500ms");
    expect(q.find("svg.task-spinner").attributes("style")).toContain("2500ms");
  });
  test("waiting_input pulses in vivid, not in quiet", () => {
    const v = mount(TaskStateIcon, {
      props: { state: "waiting_input", preset: presets.vivid },
    });
    const q = mount(TaskStateIcon, {
      props: { state: "waiting_input", preset: presets.quiet },
    });
    expect(v.classes()).toContain("pulse");
    expect(q.classes()).not.toContain("pulse");
  });
  test("vivid renders type icon when type is provided; quiet does not", () => {
    const v = mount(TaskStateIcon, {
      props: { state: "running", type: "ai", preset: presets.vivid },
    });
    const q = mount(TaskStateIcon, {
      props: { state: "running", type: "ai", preset: presets.quiet },
    });
    expect(v.find("svg.task-type").exists()).toBe(true);
    expect(q.find("svg.task-type").exists()).toBe(false);
  });
});
