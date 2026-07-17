import { describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import FileTabs from "./FileTabs.vue";

describe("FileTabs", () => {
  const tabs = [
    { path: "/a.txt", persistent: true, lastActiveAt: 1, viewMode: "code" as const, dirty: false },
    { path: "/b.txt", persistent: false, lastActiveAt: 2, viewMode: "code" as const, dirty: false },
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

  it("clicking close emits close-request with idx", async () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0, viewMode: "code" } });
    await w.findAll(".tab .close")[1].trigger("click");
    expect(w.emitted("close-request")?.[0]).toEqual([1]);
  });

  it("shows the dirty dot when tab.dirty=true", () => {
    const dirtyTabs = [
      { path: "/a.txt", persistent: true, lastActiveAt: 1, viewMode: "code" as const, dirty: true },
    ];
    const w = mount(FileTabs, { props: { tabs: dirtyTabs, activeIdx: 0, viewMode: "code" } });
    expect(w.find('[data-test="dirty-dot"]').exists()).toBe(true);
  });

  it("shows the SVG view toggle when the active tab is an .svg", () => {
    const svgTabs = [{ path: "/x/logo.svg", persistent: true, lastActiveAt: 1, viewMode: "code" as const, dirty: false }];
    const w = mount(FileTabs, { props: { tabs: svgTabs, activeIdx: 0, viewMode: "code" } });
    expect(w.find(".view-toggle").exists()).toBe(true);
  });

  it("emits toggle-view-mode on click", async () => {
    const svgTabs = [{ path: "/x/logo.svg", persistent: true, lastActiveAt: 1, viewMode: "code" as const, dirty: false }];
    const w = mount(FileTabs, { props: { tabs: svgTabs, activeIdx: 0, viewMode: "code" } });
    await w.find(".view-toggle").trigger("click");
    expect(w.emitted("toggle-view-mode")?.length).toBe(1);
  });

  it("hides the toggle for non-dual-mode active tab", () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0, viewMode: "code" } });
    expect(w.find(".view-toggle").exists()).toBe(false);
  });

  it("shows the view toggle when the active tab is a .md", () => {
    const mdTabs = [{ path: "/x/README.md", persistent: true, lastActiveAt: 1, viewMode: "render" as const, dirty: false }];
    const w = mount(FileTabs, { props: { tabs: mdTabs, activeIdx: 0, viewMode: "render" } });
    expect(w.find(".view-toggle").exists()).toBe(true);
  });

  it("shows the view toggle when the active tab is a .markdown", () => {
    const mdTabs = [{ path: "/x/notes.markdown", persistent: true, lastActiveAt: 1, viewMode: "render" as const, dirty: false }];
    const w = mount(FileTabs, { props: { tabs: mdTabs, activeIdx: 0, viewMode: "render" } });
    expect(w.find(".view-toggle").exists()).toBe(true);
  });
});
