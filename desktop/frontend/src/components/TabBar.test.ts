import { afterEach, describe, expect, it, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import { initI18n, resetI18nForTest } from "../i18n";
import TabBar from "./TabBar.vue";

if (typeof (globalThis as any).PointerEvent === "undefined") {
  class PointerEventShim extends MouseEvent {
    pointerId: number;
    pointerType: string;
    constructor(type: string, init: any = {}) {
      super(type, init);
      this.pointerId = init.pointerId ?? 0;
      this.pointerType = init.pointerType ?? "mouse";
      // Vue Test Utils sets clientX/clientY after construction; MouseEvent's
      // clientX/clientY are read-only getters on the prototype. Shadow them
      // with writable instance properties so VTU's assignment succeeds.
      for (const k of ["clientX", "clientY", "button", "buttons"] as const) {
        Object.defineProperty(this, k, {
          value: (init as any)[k] ?? 0,
          writable: true, enumerable: true, configurable: true,
        });
      }
    }
  }
  (globalThis as any).PointerEvent = PointerEventShim;
}

const splitTab = {
  id: "tab-1",
  layout: "vertical" as const,
  activeSession: {
    id: "s1",
    command: "bash",
    cwd: "/tmp",
    title: "",
    cols: 80,
    rows: 24,
    started_at: 0,
  },
  activeRemote: false,
  paneCount: 2,
};

afterEach(() => {
  resetI18nForTest();
});

describe("TabBar", () => {
  it("renders the lastSeenInfo title with a disconnected style after a remote drop", async () => {
    await initI18n({
      loadPreference: async () => "zh-CN",
      getLanguages: () => ["zh-CN"],
      listenLanguageChange: () => () => undefined,
    });

    const wrapper = mount(TabBar, {
      props: {
        tabs: [{
          id: "tab-9",
          layout: "single" as const,
          activeSession: {
            id: "stale-id", command: "powershell", cwd: "C:\\Users\\xianj",
            title: "", cols: 80, rows: 24, started_at: 0,
          },
          activeRemote: true,
          paneCount: 1,
          disconnected: true,
        }],
        currentId: "tab-9",
        starting: false,
      },
    });

    // Title falls back to the last-known cwd basename, not "(空)".
    expect(wrapper.text()).toContain("xianj");
    expect(wrapper.text()).not.toContain("(空)");
    // The tab itself is marked disconnected for styling.
    expect(wrapper.get(".tab").classes()).toContain("disconnected");
  });

  it("localizes split layout icon titles", async () => {
    await initI18n({
      loadPreference: async () => "zh-CN",
      getLanguages: () => ["zh-CN"],
      listenLanguageChange: () => () => undefined,
    });

    const wrapper = mount(TabBar, {
      props: {
        tabs: [splitTab],
        currentId: "tab-1",
        starting: false,
      },
    });

    expect(wrapper.get(".layout-icon").attributes("title")).toBe("左右分屏");
    expect(wrapper.get(".layout-icon").attributes("title")).not.toBe("vertical");
  });

  test("type icon is no longer rendered for any session type", () => {
    const aiTab = {
      ...baseTab,
      activeSession: {
        id: "s1",
        command: "claude",
        cwd: "/tmp",
        title: "claude",
        cols: 80,
        rows: 24,
        started_at: 0,
        host_id: "h",
        host: "mac",
        user: "u",
        task_state: "running" as const,
        type: "ai",
      },
    };
    const w = mount(TabBar, { props: { tabs: [aiTab], currentId: "t1", starting: false } });
    expect(w.find(".type-icon").exists()).toBe(false);
  });
});

const baseTab = {
  id: "t1",
  layout: "single" as const,
  activeRemote: false,
  paneCount: 1,
  disconnected: false,
};

describe("TabBar settings button", () => {
  // Settings lives on TabBar (not TitleBar) specifically so it stays
  // reachable when caps.windowControls=false hides TitleBar entirely
  // (web/mobile). TabBar itself is only gated on `!startupFatal` in
  // App.vue, so it's always present alongside a mounted app.
  it("renders a settings button and emits open-settings when clicked", async () => {
    const w = mount(TabBar, { props: { tabs: [], currentId: null, starting: false } });
    const btn = w.get('[data-test="tabbar-settings"]');
    await btn.trigger("click");
    expect(w.emitted("open-settings")).toBeTruthy();
  });

  it("renders no update dot by default", () => {
    const w = mount(TabBar, { props: { tabs: [], currentId: null, starting: false } });
    expect(w.find('[data-test="tabbar-settings"] .dot').exists()).toBe(false);
  });

  it("renders an update dot on the settings button when updateBadge=true", () => {
    const w = mount(TabBar, {
      props: { tabs: [], currentId: null, starting: false, updateBadge: true },
    });
    expect(w.find('[data-test="tabbar-settings"] .dot').exists()).toBe(true);
  });
});

describe("TabBar admin button", () => {
  it("renders the admin button when isAdmin=true", () => {
    const w = mount(TabBar, {
      props: { tabs: [], currentId: null, starting: false, isAdmin: true, adminOpen: false },
    });
    expect(w.find('[data-test="admin-button"]').exists()).toBe(true);
  });

  it("does not render the admin button when isAdmin=false", () => {
    const w = mount(TabBar, {
      props: { tabs: [], currentId: null, starting: false, isAdmin: false, adminOpen: false },
    });
    expect(w.find('[data-test="admin-button"]').exists()).toBe(false);
  });

  it("adds the active class when adminOpen=true", () => {
    const w = mount(TabBar, {
      props: { tabs: [], currentId: null, starting: false, isAdmin: true, adminOpen: true },
    });
    expect(w.get('[data-test="admin-button"]').classes()).toContain("active");
  });

  it("emits toggle-admin when clicked", async () => {
    const w = mount(TabBar, {
      props: { tabs: [], currentId: null, starting: false, isAdmin: true, adminOpen: false },
    });
    await w.get('[data-test="admin-button"]').trigger("click");
    expect(w.emitted("toggle-admin")).toBeTruthy();
  });
});

describe("TabBar state icon + unread", () => {
  test("uses TaskStateIcon for the active session's task_state", () => {
    const tab = {
      ...baseTab,
      activeSession: {
        id: "s1",
        command: "claude",
        cwd: "/",
        title: "",
        cols: 80,
        rows: 24,
        started_at: 0,
        task_state: "waiting_input" as const,
      },
    };
    const w = mount(TabBar, {
      props: { tabs: [tab], currentId: "t1", starting: false },
    });
    expect(w.find(".task-state-icon").exists()).toBe(true);
    expect(w.find(".task-state-icon").text()).toContain("◐");
  });

  test("renders unread dot when activeSession.unread is true", () => {
    const tab = {
      ...baseTab,
      activeSession: {
        id: "s1",
        command: "claude",
        cwd: "/",
        title: "",
        cols: 80,
        rows: 24,
        started_at: 0,
        task_state: "completed" as const,
        unread: true,
      },
    };
    const w = mount(TabBar, {
      props: { tabs: [tab], currentId: "t1", starting: false },
    });
    expect(w.find('[data-test="tab-unread-dot"]').exists()).toBe(true);
  });
});

describe("TabBar AI title", () => {
  test("shows OSC title for ai-typed session", () => {
    const tab = {
      ...baseTab,
      activeSession: {
        id: "s1",
        command: "/usr/local/bin/claude",
        cwd: "/Users/me/proj",
        title: "Remove token auth from relay login",
        cols: 80,
        rows: 24,
        started_at: 0,
        type: "ai",
        task_state: "running" as const,
      },
    };
    const w = mount(TabBar, { props: { tabs: [tab], currentId: "t1", starting: false } });
    expect(w.get(".title").text()).toBe("Remove token auth from relay login");
  });

  test("falls back to cwd basename when ai session has no title yet", () => {
    const tab = {
      ...baseTab,
      activeSession: {
        id: "s1",
        command: "claude",
        cwd: "/Users/me/proj",
        title: "",
        cols: 80,
        rows: 24,
        started_at: 0,
        type: "ai",
      },
    };
    const w = mount(TabBar, { props: { tabs: [tab], currentId: "t1", starting: false } });
    expect(w.get(".title").text()).toBe("proj");
  });

  test("shell session ignores title and uses cwd basename", () => {
    const tab = {
      ...baseTab,
      activeSession: {
        id: "s1",
        command: "zsh",
        cwd: "/Users/me/proj",
        title: "should-not-show",
        cols: 80,
        rows: 24,
        started_at: 0,
        type: "shell",
      },
    };
    const w = mount(TabBar, { props: { tabs: [tab], currentId: "t1", starting: false } });
    expect(w.get(".title").text()).toBe("proj");
  });
});

describe("TabBar drag reorder", () => {
  function mountThreeTabs() {
    return mount(TabBar, {
      props: {
        tabs: [
          { id: "tab-1", layout: "single" as const, activeSession: null, activeRemote: false, paneCount: 1 },
          { id: "tab-2", layout: "single" as const, activeSession: null, activeRemote: false, paneCount: 1 },
          { id: "tab-3", layout: "single" as const, activeSession: null, activeRemote: false, paneCount: 1 },
        ],
        currentId: "tab-1",
        starting: false,
      },
      attachTo: document.body,
    });
  }

  function stubRect(el: Element, rect: Partial<DOMRect>) {
    (el as HTMLElement).getBoundingClientRect = () => ({
      x: 0, y: 0, width: 0, height: 0, top: 0, left: 0, right: 0, bottom: 0,
      toJSON: () => ({}), ...rect,
    } as DOMRect);
  }

  function stubThreeTabRects(wrapper: ReturnType<typeof mountThreeTabs>) {
    const tabsEls = wrapper.findAll(".tab");
    stubRect(wrapper.get(".tabs").element, { left: 0, right: 900, width: 900 });
    stubRect(tabsEls[0].element, { left: 0, right: 100, width: 100 });
    stubRect(tabsEls[1].element, { left: 100, right: 200, width: 100 });
    stubRect(tabsEls[2].element, { left: 200, right: 300, width: 100 });
    return tabsEls;
  }

  it("emits reorder when a tab is dragged past another tab's midline", async () => {
    const wrapper = mountThreeTabs();
    const tabsEls = stubThreeTabRects(wrapper);

    await tabsEls[0].trigger("pointerdown", { clientX: 50, pointerId: 1, button: 0 });
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 55, pointerId: 1 } as any));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 260, pointerId: 1 } as any));
    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 260, pointerId: 1 } as any));

    const events = wrapper.emitted("reorder") as unknown[][] | undefined;
    expect(events).toBeTruthy();
    expect(events![events!.length - 1]).toEqual(["tab-1", "tab-3", "after"]);
    wrapper.unmount();
  });

  it("does not emit reorder for sub-threshold movement and still activates on click", async () => {
    const wrapper = mountThreeTabs();
    const tabsEls = stubThreeTabRects(wrapper);

    await tabsEls[1].trigger("pointerdown", { clientX: 150, pointerId: 2, button: 0 });
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 152, pointerId: 2 } as any));
    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 152, pointerId: 2 } as any));
    await tabsEls[1].trigger("click");

    expect(wrapper.emitted("reorder")).toBeUndefined();
    expect(wrapper.emitted("activate")).toBeTruthy();
    wrapper.unmount();
  });

  it("debounces reorder while cursor stays over the same slot", async () => {
    const wrapper = mountThreeTabs();
    const tabsEls = stubThreeTabRects(wrapper);

    await tabsEls[0].trigger("pointerdown", { clientX: 50, pointerId: 3, button: 0 });
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 55, pointerId: 3 } as any));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 130, pointerId: 3 } as any));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 140, pointerId: 3 } as any));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 145, pointerId: 3 } as any));
    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 145, pointerId: 3 } as any));

    const events = wrapper.emitted("reorder") as unknown[][] | undefined;
    expect(events).toBeTruthy();
    expect(events!.length).toBe(1);
    expect(events![0]).toEqual(["tab-1", "tab-2", "before"]);
    wrapper.unmount();
  });

  it("does not emit reorder while the cursor stays over the source tab", async () => {
    const wrapper = mountThreeTabs();
    const tabsEls = stubThreeTabRects(wrapper);

    await tabsEls[0].trigger("pointerdown", { clientX: 20, pointerId: 4, button: 0 });
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 40, pointerId: 4 } as any));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 70, pointerId: 4 } as any));
    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 70, pointerId: 4 } as any));

    expect(wrapper.emitted("reorder")).toBeUndefined();
    wrapper.unmount();
  });

  it("stops emitting after pointercancel", async () => {
    const wrapper = mountThreeTabs();
    const tabsEls = stubThreeTabRects(wrapper);

    await tabsEls[0].trigger("pointerdown", { clientX: 50, pointerId: 5, button: 0 });
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 60, pointerId: 5 } as any));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 160, pointerId: 5 } as any));
    const beforeCancel = (wrapper.emitted("reorder") ?? []).length;
    window.dispatchEvent(new PointerEvent("pointercancel", { clientX: 160, pointerId: 5 } as any));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 260, pointerId: 5 } as any));
    const afterCancel = (wrapper.emitted("reorder") ?? []).length;
    expect(afterCancel).toBe(beforeCancel);
    wrapper.unmount();
  });

  it("does not initiate drag when pointerdown lands on the close button", async () => {
    const wrapper = mountThreeTabs();
    const tabsEls = stubThreeTabRects(wrapper);

    const closeBtn = tabsEls[0].get(".close");
    await closeBtn.trigger("pointerdown", { clientX: 90, pointerId: 6, button: 0 });
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 260, pointerId: 6 } as any));
    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 260, pointerId: 6 } as any));
    await closeBtn.trigger("click");

    expect(wrapper.emitted("reorder")).toBeUndefined();
    expect(wrapper.emitted("close")).toBeTruthy();
    wrapper.unmount();
  });

  it("ignores non-primary buttons for drag", async () => {
    const wrapper = mountThreeTabs();
    const tabsEls = stubThreeTabRects(wrapper);

    await tabsEls[0].trigger("pointerdown", { clientX: 50, pointerId: 7, button: 2 });
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 260, pointerId: 7 } as any));
    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 260, pointerId: 7 } as any));

    expect(wrapper.emitted("reorder")).toBeUndefined();
    wrapper.unmount();
  });

  it("auto-scrolls the tabs container when dragging near the right edge", async () => {
    const wrapper = mountThreeTabs();
    const tabsEls = wrapper.findAll(".tab");
    const tabsContainer = wrapper.get(".tabs").element as HTMLElement;
    stubRect(tabsContainer, { left: 0, right: 300, width: 300 });
    stubRect(tabsEls[0].element, { left: 0, right: 100, width: 100 });
    stubRect(tabsEls[1].element, { left: 100, right: 200, width: 100 });
    stubRect(tabsEls[2].element, { left: 200, right: 300, width: 100 });
    tabsContainer.scrollLeft = 0;

    const rafSpy = vi.spyOn(window, "requestAnimationFrame").mockImplementation((cb) => {
      return setTimeout(() => cb(0), 0) as unknown as number;
    });

    try {
      await tabsEls[0].trigger("pointerdown", { clientX: 50, pointerId: 8, button: 0 });
      window.dispatchEvent(new PointerEvent("pointermove", { clientX: 60, pointerId: 8 } as any));
      window.dispatchEvent(new PointerEvent("pointermove", { clientX: 290, pointerId: 8 } as any));
      await new Promise((r) => setTimeout(r, 30));
      expect(tabsContainer.scrollLeft).toBeGreaterThan(0);
      window.dispatchEvent(new PointerEvent("pointerup", { clientX: 290, pointerId: 8 } as any));
    } finally {
      rafSpy.mockRestore();
      wrapper.unmount();
    }
  });
});
