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
    };

    const wrapper = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", token: "token" }),
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
