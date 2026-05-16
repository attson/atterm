import { describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import FileTabs from "./FileTabs.vue";

describe("FileTabs", () => {
  const tabs = [
    { path: "/a.txt", persistent: true, lastActiveAt: 1 },
    { path: "/b.txt", persistent: false, lastActiveAt: 2 },
  ];

  it("renders one tab per entry, styled by persistent flag", () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0 } });
    expect(w.findAll(".tab")).toHaveLength(2);
    expect(w.findAll(".tab")[1].classes()).toContain("preview");
  });

  it("clicking a tab emits select", async () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0 } });
    await w.findAll(".tab")[1].trigger("click");
    expect(w.emitted("select")?.[0]).toEqual([1]);
  });

  it("clicking close emits close with idx", async () => {
    const w = mount(FileTabs, { props: { tabs, activeIdx: 0 } });
    await w.findAll(".tab .close")[1].trigger("click");
    expect(w.emitted("close")?.[0]).toEqual([1]);
  });
});
