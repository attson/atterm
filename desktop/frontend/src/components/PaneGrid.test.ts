import { describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import PaneGrid from "./PaneGrid.vue";
import source from "./PaneGrid.vue?raw";
import type { Tab } from "../lib/types";

vi.mock("./TerminalView.vue", () => ({
  default: {
    name: "TerminalView",
    template: "<div />",
  },
}));

describe("PaneGrid viewers badge", () => {
  test("accepts a viewerCountFor prop", () => {
    expect(source).toMatch(/viewerCountFor\?: \(sessionId: string\) => number/);
  });
  test("renders a viewers badge on local panes when count > 0", () => {
    expect(source).toMatch(/class="viewers-badge"/);
    expect(source).toMatch(/!pane\.remote/);
    expect(source).toMatch(/viewerCountFor\?\.\(pane\.sessionId\)/);
  });
  test("lets the SP1 overlay dodge the viewers badge too", () => {
    expect(source).toMatch(/avoid-top-right-badge="pane\.remote \|\|/);
  });
});

describe("PaneGrid close button", () => {
  test("clicking an inactive pane close button does not first activate that pane", async () => {
    const tab: Tab = {
      id: "tab-1",
      layout: "vertical",
      activePaneIdx: 0,
      panes: [
        { sessionId: "left", remote: false },
        { sessionId: "right", remote: false },
      ],
      colRatio: 0.5,
      rowRatio: 0.5,
    };

    const wrapper = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "token" }),
        sessionInfoFor: () => null,
        active: true,
        terminalTheme: {},
        commandNotifyThresholdSec: 10,
      },
    });

    const rightClose = wrapper.findAll("button.close-pane")[1];
    await rightClose.trigger("mousedown");
    await rightClose.trigger("click");

    expect(wrapper.emitted("set-active-pane")).toBeUndefined();
    expect(wrapper.emitted("close-pane")).toEqual([[1]]);
  });
});

describe("PaneGrid ratio rendering", () => {
  function mkTab(partial: Partial<Tab> & Pick<Tab, "layout" | "panes">): Tab {
    return {
      id: "t",
      activePaneIdx: 0,
      colRatio: 0.5,
      rowRatio: 0.5,
      ...partial,
    };
  }

  const baseProps = {
    endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "t" }),
    sessionInfoFor: () => null,
    active: true,
    terminalTheme: {},
    commandNotifyThresholdSec: 10,
  } as const;

  test("vertical applies colRatio to grid-template", () => {
    const tab = mkTab({
      layout: "vertical",
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
      colRatio: 0.3,
    });
    const w = mount(PaneGrid, { props: { tab, ...baseProps } });
    const root = w.find(".pane-grid").element as HTMLElement;
    expect(root.style.gridTemplate).toMatch(/0\.3\d*fr 0\.7\d*fr/);
  });

  test("horizontal applies rowRatio to grid-template", () => {
    const tab = mkTab({
      layout: "horizontal",
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
      rowRatio: 0.25,
    });
    const w = mount(PaneGrid, { props: { tab, ...baseProps } });
    const root = w.find(".pane-grid").element as HTMLElement;
    expect(root.style.gridTemplate).toMatch(/0\.25\d*fr/);
  });

  test("grid2x2 renders 2 splitters", () => {
    const tab = mkTab({
      layout: "grid2x2",
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
        { sessionId: "c", remote: false },
        { sessionId: "d", remote: false },
      ],
    });
    const w = mount(PaneGrid, { props: { tab, ...baseProps } });
    expect(w.findAll(".pane-splitter")).toHaveLength(2);
  });

  test("vertical renders 1 col splitter only", () => {
    const tab = mkTab({
      layout: "vertical",
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
    });
    const w = mount(PaneGrid, { props: { tab, ...baseProps } });
    expect(w.findAll(".pane-splitter.col")).toHaveLength(1);
    expect(w.findAll(".pane-splitter.row")).toHaveLength(0);
  });

  test("horizontal renders 1 row splitter only", () => {
    const tab = mkTab({
      layout: "horizontal",
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
    });
    const w = mount(PaneGrid, { props: { tab, ...baseProps } });
    expect(w.findAll(".pane-splitter.col")).toHaveLength(0);
    expect(w.findAll(".pane-splitter.row")).toHaveLength(1);
  });

  test("single renders no splitters", () => {
    const tab = mkTab({
      layout: "single",
      panes: [{ sessionId: "a", remote: false }],
    });
    const w = mount(PaneGrid, { props: { tab, ...baseProps } });
    expect(w.findAll(".pane-splitter")).toHaveLength(0);
  });

  test("col splitter update:ratio bubbles to update:col-ratio", () => {
    const tab = mkTab({
      layout: "vertical",
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
    });
    const w = mount(PaneGrid, { props: { tab, ...baseProps } });
    const splitter = w.findAllComponents({ name: "PaneSplitter" })[0];
    splitter.vm.$emit("update:ratio", 0.42);
    expect(w.emitted("update:col-ratio")).toEqual([[0.42]]);
  });

  test("col splitter reset emits update:col-ratio 0.5", () => {
    const tab = mkTab({
      layout: "vertical",
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
      colRatio: 0.3,
    });
    const w = mount(PaneGrid, { props: { tab, ...baseProps } });
    const splitter = w.findAllComponents({ name: "PaneSplitter" })[0];
    splitter.vm.$emit("reset");
    expect(w.emitted("update:col-ratio")).toEqual([[0.5]]);
  });

  test("grid2x2 row splitter emits update:row-ratio", () => {
    const tab = mkTab({
      layout: "grid2x2",
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
        { sessionId: "c", remote: false },
        { sessionId: "d", remote: false },
      ],
    });
    const w = mount(PaneGrid, { props: { tab, ...baseProps } });
    const splitters = w.findAllComponents({ name: "PaneSplitter" });
    // [col, row] is the declared template order
    splitters[1].vm.$emit("update:ratio", 0.7);
    expect(w.emitted("update:row-ratio")).toEqual([[0.7]]);
  });
});
