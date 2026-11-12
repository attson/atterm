import { describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import PaneGrid from "./PaneGrid.vue";
import SelectDropdown from "./SelectDropdown.vue";
import source from "./PaneGrid.vue?raw";
import type { Tab } from "../lib/types";
import type { SessionInfo } from "../lib/connection";

vi.mock("./TerminalView.vue", () => ({
  default: {
    name: "TerminalView",
    props: ["currentCwd", "appearance", "theme"],
    template: "<div />",
  },
}));

describe("PaneGrid terminal identity", () => {
  test("keeps the preview menu aligned with the pane theme trigger field", () => {
    expect(source).toMatch(/\.pane-theme-menu\s+:deep\(\.menu-with-previews\)\s*\{\s*width:\s*100%;\s*\}/);
  });

  test("keys each pane by session identity with an empty-cell fallback", () => {
    expect(source).toMatch(/function paneKey\(pane: Pane, idx: number\): string/);
    expect(source).toMatch(/return pane\.sessionId \?\? `empty-\$\{idx\}`/);
    expect(source).toContain(':key="paneKey(pane, idx)"');
    expect(source).not.toContain(':key="idx"');
  });

  test("forwards the session cwd to the terminal", () => {
    const tab: Tab = {
      id: "tab-1",
      layout: "single",
      activePaneIdx: 0,
      panes: [{ sessionId: "session-1", remote: false }],
      colRatio: 0.5,
      rowRatio: 0.5,
    };
    const info: SessionInfo = {
      id: "session-1",
      command: "zsh",
      cwd: "/work/atterm",
      title: "",
      cols: 80,
      rows: 24,
      started_at: 0,
    };
    const wrapper = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "token" }),
        sessionInfoFor: () => info,
        active: true,
        terminalTheme: {},
        commandNotifyThresholdSec: 10,
      },
    });

    expect(wrapper.findComponent({ name: "TerminalView" }).props("currentCwd")).toBe(
      "/work/atterm",
    );
  });

  test("resolves saved SSH appearance per pane without changing app chrome", () => {
    const tab: Tab = {
      id: "tab-1", layout: "single", activePaneIdx: 0,
      panes: [{ sessionId: "session-1", remote: true }], colRatio: 0.5, rowRatio: 0.5,
    };
    const info: SessionInfo = {
      id: "session-1", command: "ssh root@host", cwd: "", title: "", cols: 80, rows: 24, started_at: 0,
      ssh_appearance: { theme_id: "nord", font_head: "Source Code Pro", font_size: 14 },
    };
    const globalAppearance = {
      fontHead: "JetBrains Mono", fontSize: 13, lineHeight: 1,
      cursorStyle: "block" as const, cursorBlink: true, scrollback: 5000,
    };
    const wrapper = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "token" }),
        sessionInfoFor: () => info,
        active: true,
        terminalTheme: { background: "#000" },
        appearance: globalAppearance,
        commandNotifyThresholdSec: 10,
      },
    });
    const terminal = wrapper.findComponent({ name: "TerminalView" });
    expect(terminal.props("appearance")).toEqual({
      ...globalAppearance, fontHead: "Source Code Pro", fontSize: 14,
    });
    expect(terminal.props("theme").background).toBe("#2e3440");
  });

  test("requests a global app theme change from the pane toolbar", async () => {
    const tab: Tab = {
      id: "tab-1", layout: "vertical", activePaneIdx: 0,
      panes: [
        { sessionId: "left", remote: true },
        { sessionId: "right", remote: true },
      ],
      colRatio: 0.5, rowRatio: 0.5,
    };
    const infoFor = (id: string): SessionInfo => ({
      id, command: "ssh root@host", cwd: "", title: "", cols: 80, rows: 24, started_at: 0,
      ...(id === "left" ? { ssh_appearance: { theme_id: "solarized-dark" } } : {}),
    });
    const wrapper = mount(PaneGrid, {
      props: {
        tab,
        endpointFor: () => ({ url: "ws://127.0.0.1:1", session_token: "token" }),
        sessionInfoFor: (pane) => infoFor(pane.sessionId ?? ""),
        active: true,
        terminalThemeId: "classic",
        terminalTheme: { background: "#000000" },
        commandNotifyThresholdSec: 10,
      },
    });

    const terminals = () => wrapper.findAllComponents({ name: "TerminalView" });
    expect(terminals()[0].props("theme").background).toBe("#002b36");
    expect(terminals()[1].props("theme").background).toBe("#000000");

    await wrapper.get('[data-test="terminal-theme-trigger-left"]').trigger("click");
    expect(wrapper.find('[data-test="terminal-theme-menu-left"] select').exists()).toBe(false);
    const themeDropdown = wrapper.findAllComponents(SelectDropdown)
      .find((item) => item.attributes("data-test") === "terminal-theme-select-left")!;
    const themeOptions = themeDropdown.props("options") as Array<{
      value: string;
      description?: string;
      preview?: { background: string; foreground: string; colors: string[] };
    }>;
    const nordOption = themeOptions.find(
      (option) => option.value === "nord",
    );
    expect(nordOption?.description).toBeTruthy();
    expect(nordOption?.preview?.background).toBe("#2e3440");
    expect(themeOptions.find((option) => option.value === "__session__")).toBeUndefined();
    themeDropdown.vm.$emit("update:modelValue", "nord");
    await wrapper.vm.$nextTick();
    expect(wrapper.emitted("terminal-theme-changed")).toEqual([["nord"]]);
    // PaneGrid only requests the global mutation. App owns the persisted
    // preference and feeds the resulting palette back through props.
    expect(terminals()[0].props("theme").background).toBe("#002b36");
    expect(terminals()[1].props("theme").background).toBe("#000000");

    await wrapper.setProps({
      terminalThemeId: "nord",
      terminalTheme: { background: "#2e3440" },
    });
    // A saved SSH terminal override remains terminal-specific; panes without
    // one and the surrounding app use the new global theme.
    expect(terminals()[0].props("theme").background).toBe("#002b36");
    expect(terminals()[1].props("theme").background).toBe("#2e3440");
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

  test("renders pane actions only for the focused pane without hidden layout placeholders", async () => {
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

    expect(wrapper.findAll(".session-input-history-controls-slot")).toHaveLength(1);
    expect(wrapper.find(".session-input-history-controls-slot").attributes("id")).toBe(
      "session-input-history-controls-left",
    );
    expect(wrapper.findAll('[data-test="pane-grip"]')).toHaveLength(1);
    expect(wrapper.findAll("button.close-pane")).toHaveLength(1);
    expect(source).not.toMatch(/\.close-pane\s*\{[^}]*opacity:\s*0/);
    expect(source).not.toMatch(/\.pane-grip\s*\{[^}]*opacity:\s*0/);

    await wrapper.setProps({
      tab: { ...tab, activePaneIdx: 1 },
    });

    expect(wrapper.findAll(".session-input-history-controls-slot")).toHaveLength(1);
    expect(wrapper.find(".session-input-history-controls-slot").attributes("id")).toBe(
      "session-input-history-controls-right",
    );

    await wrapper.find("button.close-pane").trigger("mousedown");
    await wrapper.find("button.close-pane").trigger("click");

    expect(wrapper.emitted("set-active-pane")).toBeUndefined();
    expect(wrapper.emitted("close-pane")).toEqual([[1]]);
  });

  test("hides pane actions when the tab itself is not focused", () => {
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
        active: false,
        terminalTheme: {},
        commandNotifyThresholdSec: 10,
      },
    });

    expect(wrapper.find(".session-input-history-controls-slot").exists()).toBe(false);
    expect(wrapper.find('[data-test="pane-grip"]').exists()).toBe(false);
    expect(wrapper.find("button.close-pane").exists()).toBe(false);
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
    expect(grip).toHaveLength(1);
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
