import { describe, it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import LogLines from "./LogLines.vue";

const content = [
  "2026/06/22 15:04:05.123 DEBUG [pty-input] write n=1 hex=61",
  "2026/06/22 15:04:05.200 WARN  [relay] dropping frame",
  "    raw continuation line",
].join("\n");

describe("LogLines", () => {
  it("renders every line at minLevel DEBUG", () => {
    const w = mount(LogLines, { props: { content, minLevel: "DEBUG" } });
    expect(w.findAll(".log-line").length).toBe(3);
    expect(w.find(".lvl-DEBUG").exists()).toBe(true);
    expect(w.find(".lvl-WARN").exists()).toBe(true);
  });

  it("hides below-threshold structured lines but keeps raw lines", () => {
    const w = mount(LogLines, { props: { content, minLevel: "WARN" } });
    // WARN line + raw line remain; DEBUG line filtered out
    expect(w.find(".lvl-DEBUG").exists()).toBe(false);
    expect(w.find(".lvl-WARN").exists()).toBe(true);
    expect(w.text()).toContain("raw continuation line");
  });

  it("shows the tag for structured lines", () => {
    const w = mount(LogLines, { props: { content, minLevel: "DEBUG" } });
    expect(w.text()).toContain("[pty-input]");
  });
});
