import { afterEach, describe, expect, it } from "vitest";
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
});
