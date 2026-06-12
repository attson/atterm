import { afterEach, describe, expect, it, test } from "vitest";
import { mount } from "@vue/test-utils";
import { initI18n, resetI18nForTest } from "../i18n";
import TabBar from "./TabBar.vue";

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

