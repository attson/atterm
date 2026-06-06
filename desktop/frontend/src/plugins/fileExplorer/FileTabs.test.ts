import { describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import FileTabs from "./FileTabs.vue";

describe("FileTabs", () => {
  const tabs = [
    { path: "/a.txt", persistent: true, lastActiveAt: 1, viewMode: "code" as const },
    { path: "/b.txt", persistent: false, lastActiveAt: 2, viewMode: "code" as const },
  ];

  it("renders one tab per entry, styled by persistent flag", () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0, viewMode: "code" } });
    expect(w.findAll(".tab")).toHaveLength(2);
    expect(w.findAll(".tab")[1].classes()).toContain("preview");
  });

  it("clicking a tab emits select", async () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0, viewMode: "code" } });
    await w.findAll(".tab")[1].trigger("click");
    expect(w.emitted("select")?.[0]).toEqual([1]);
  });

  it("clicking close emits close with idx", async () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0, viewMode: "code" } });
    await w.findAll(".tab .close")[1].trigger("click");
    expect(w.emitted("close")?.[0]).toEqual([1]);
  });

  it("shows the SVG view toggle when the active tab is an .svg", () => {
    const svgTabs = [{ path: "/x/logo.svg", persistent: true, lastActiveAt: 1, viewMode: "code" as const }];
    const w = mount(FileTabs, { props: { tabs: svgTabs, activeIdx: 0, viewMode: "code" } });
    expect(w.find(".view-toggle").exists()).toBe(true);
  });

  it("emits toggle-view-mode on click", async () => {
    const svgTabs = [{ path: "/x/logo.svg", persistent: true, lastActiveAt: 1, viewMode: "code" as const }];
    const w = mount(FileTabs, { props: { tabs: svgTabs, activeIdx: 0, viewMode: "code" } });
    await w.find(".view-toggle").trigger("click");
    expect(w.emitted("toggle-view-mode")?.length).toBe(1);
  });

  it("hides the toggle for non-svg active tab", () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0, viewMode: "code" } });
    expect(w.find(".view-toggle").exists()).toBe(false);
  });
});
