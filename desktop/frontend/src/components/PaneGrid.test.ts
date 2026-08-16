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

describe("PaneGrid terminal identity", () => {
  test("keys each pane by session identity with an empty-cell fallback", () => {
    expect(source).toMatch(/function paneKey\(pane: Pane, idx: number\): string/);
    expect(source).toMatch(/return pane\.sessionId \?\? `empty-\$\{idx\}`/);
    expect(source).toContain(':key="paneKey(pane, idx)"');
    expect(source).not.toContain(':key="idx"');
  });
});

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
  test("keeps close controls above the transparent splitter hit targets", () => {
    expect(source).toMatch(/\.cell-controls\s*\{[\s\S]*?z-index:\s*6;/);
  });

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

describe("PaneGrid session drop target", () => {
  function mkTab(): Tab {
    return {
      id: "t",
      layout: "vertical",
      activePaneIdx: 0,
      colRatio: 0.5,
      rowRatio: 0.5,
      panes: [
        { sessionId: "a", remote: false },
        { sessionId: "b", remote: false },
      ],
    };
  }

  const baseProps = {
    endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "t" }),
    sessionInfoFor: () => null,
    active: true,
    terminalTheme: {},
    commandNotifyThresholdSec: 10,
  } as const;

  // Minimal stand-in for DataTransfer: jsdom's DragEvent does not build one.
  function dt(types: string[], data: Record<string, string> = {}) {
    return {
      types,
      dropEffect: "none",
      getData: (k: string) => data[k] ?? "",
    };
  }

  const MIME = "application/x-atterm-session";

  function mountGrid() {
    return mount(PaneGrid, { props: { tab: mkTab(), ...baseProps } });
  }

  test("highlights the hovered pane while a session drag is over it", async () => {
    const w = mountGrid();
    const cells = w.findAll('[data-test="pane-cell"]');
    await cells[1].trigger("dragover", { dataTransfer: dt([MIME]) });
    expect(cells[1].classes()).toContain("drop-target");
    expect(cells[0].classes()).not.toContain("drop-target");
  });

  test("clears the highlight when the drag leaves the pane", async () => {
    const w = mountGrid();
    const cells = w.findAll('[data-test="pane-cell"]');
    await cells[1].trigger("dragover", { dataTransfer: dt([MIME]) });
    await cells[1].trigger("dragleave", { relatedTarget: null });
    expect(cells[1].classes()).not.toContain("drop-target");
  });

  test("emits drop-session with the pane index and dragged session", async () => {
    const w = mountGrid();
    const cells = w.findAll('[data-test="pane-cell"]');
    await cells[1].trigger("drop", { dataTransfer: dt([MIME], { [MIME]: "dragged" }) });
    expect(w.emitted("drop-session")).toEqual([[{ paneIdx: 1, sessionId: "dragged" }]]);
  });

  // A file dragged from the OS, or selected text, must pass straight through:
  // the pane neither lights up nor claims the drop.
  test("ignores drags that are not one of our sessions", async () => {
    const w = mountGrid();
    const cells = w.findAll('[data-test="pane-cell"]');
    await cells[1].trigger("dragover", { dataTransfer: dt(["Files"]) });
    expect(cells[1].classes()).not.toContain("drop-target");
    await cells[1].trigger("drop", { dataTransfer: dt(["Files"]) });
    expect(w.emitted("drop-session")).toBeUndefined();
  });

  test("ignores a drop carrying our type but an empty id", async () => {
    const w = mountGrid();
    const cells = w.findAll('[data-test="pane-cell"]');
    await cells[0].trigger("drop", { dataTransfer: dt([MIME], { [MIME]: "" }) });
    expect(w.emitted("drop-session")).toBeUndefined();
  });
});

describe("PaneGrid drop target stays armed", () => {
  const baseProps = {
    endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "t" }),
    sessionInfoFor: () => null,
    active: true,
    terminalTheme: {},
    commandNotifyThresholdSec: 10,
  } as const;

  const MIME = "application/x-atterm-session";

  // dragover fires continuously while the pointer sits still, and EVERY one of
  // them must preventDefault: the browser reads a single un-prevented dragover
  // as "this element does not accept the drop" and then silently refuses to
  // fire drop at all. A guard that only ran on the first event looked correct
  // in a one-shot test and broke dropping entirely in the app.
  test("preventDefaults every dragover, not just the first", () => {
    const tab: Tab = {
      id: "t", layout: "vertical", activePaneIdx: 0, colRatio: 0.5, rowRatio: 0.5,
      panes: [{ sessionId: "a", remote: false }, { sessionId: "b", remote: false }],
    };
    const w = mount(PaneGrid, { props: { tab, ...baseProps } });
    const cell = w.findAll('[data-test="pane-cell"]')[1].element;

    const fire = () => {
      const ev = new Event("dragover", { cancelable: true, bubbles: true });
      Object.defineProperty(ev, "dataTransfer", {
        value: { types: [MIME], dropEffect: "none" },
      });
      cell.dispatchEvent(ev);
      return ev.defaultPrevented;
    };

    expect(fire()).toBe(true);
    expect(fire()).toBe(true); // pointer has not moved — still a valid target
    expect(fire()).toBe(true);
  });
});

describe("PaneGrid detach affordances", () => {
  const baseProps = {
    endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "t" }),
    sessionInfoFor: () => null,
    active: true,
    terminalTheme: {},
    commandNotifyThresholdSec: 10,
  } as const;

  function mkTab(layout: Tab["layout"], panes: Tab["panes"]): Tab {
    return { id: "t", layout, panes, activePaneIdx: 0, colRatio: 0.5, rowRatio: 0.5 };
  }

  // The terminal itself cannot be draggable — xterm needs the mouse for text
  // selection — so the grip is the only way to pick a pane up.
  test("split panes expose a drag grip carrying the session id", async () => {
    const w = mount(PaneGrid, {
      props: {
        tab: mkTab("vertical", [
          { sessionId: "a", remote: false },
          { sessionId: "b", remote: false },
        ]),
        ...baseProps,
      },
    });
    const grip = w.findAll('[data-test="pane-grip"]');
    expect(grip).toHaveLength(2);
    expect(grip[0].attributes("draggable")).toBe("true");

    const setData = vi.fn();
    await grip[0].trigger("dragstart", { dataTransfer: { setData, effectAllowed: "none" } });
    expect(setData).toHaveBeenCalledWith("application/x-atterm-session", "a");
  });

  // Nowhere to detach to: the session already owns the tab.
  test("a single-pane tab shows no grip", () => {
    const w = mount(PaneGrid, {
      props: { tab: mkTab("single", [{ sessionId: "a", remote: false }]), ...baseProps },
    });
    expect(w.find('[data-test="pane-grip"]').exists()).toBe(false);
  });

  // TerminalView is stubbed here, so assert the wiring at the source: the
  // context-menu item must only offer to detach when the tab is actually split.
  test("tells the terminal whether detaching is possible", () => {
    expect(source).toContain(`:can-detach="tab.layout !== 'single'"`);
    expect(source).toMatch(/@detach="pane\.sessionId && emit\('detach-session', pane\.sessionId\)"/);
  });
});
