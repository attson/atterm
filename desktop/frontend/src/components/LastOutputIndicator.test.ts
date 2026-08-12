import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import LastOutputIndicator from "./LastOutputIndicator.vue";
import source from "./LastOutputIndicator.vue?raw";

const NOW = 1_800_000_000_000;

describe("LastOutputIndicator", () => {
  it("renders the compact clock label and tooltip", () => {
    const w = mount(LastOutputIndicator, {
      props: { lastOutputAt: NOW / 1000 - 60, taskState: "idle", nowMs: NOW },
    });
    const indicator = w.get('[data-test="last-output"]');
    expect(indicator.text()).toBe("1m");
    expect(indicator.attributes("title")).toBe("Last output 1m ago");
    expect(indicator.find("svg").exists()).toBe(true);
  });

  it("keeps live blue but disables animation for reduced motion", () => {
    const w = mount(LastOutputIndicator, {
      props: { lastOutputAt: NOW / 1000 - 1, taskState: "running", nowMs: NOW },
    });
    expect(w.get('[data-test="last-output"]').classes()).toContain("live");
    expect(source).toContain("@media (prefers-reduced-motion: reduce)");
    expect(source).toMatch(/\.last-output\.live \.last-output-clock \{ animation: none; \}/);
  });
});
