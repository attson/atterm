import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import TaskSidebar from "./TaskSidebar.vue";
import type { RemoteSession } from "../platform/types";

function mk(over: Partial<RemoteSession>): RemoteSession {
  return {
    session_id: "s",
    host_id: "h",
    host: "mac",
    user: "u",
    title: "",
    cols: 80,
    rows: 24,
    ...over,
  };
}

describe("TaskSidebar", () => {
  test("expanded shows TaskGroupedList and Mark-all-read button when unread > 0", () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: { h: [mk({ unread: true, attention_at: 1 })] },
        unreadByHost: { h: 1 },
        primaryStateForHost: () => "running",
        completedSeen: [],
        totalUnread: 1,
      },
    });
    expect(w.find('[data-test="task-grouped-list"]').exists()).toBe(true);
    expect(w.find('[data-test="sidebar-mark-all"]').exists()).toBe(true);
  });

  test("collapsed shows narrow rail with icons and total-unread badge", () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: true,
        byHost: {
          h: [
            mk({ session_id: "s1", task_state: "waiting_input", attention_at: 1, unread: true }),
            mk({ session_id: "s2", task_state: "running" }),
          ],
        },
        unreadByHost: { h: 1 },
        primaryStateForHost: () => "waiting_input",
        completedSeen: [],
        totalUnread: 1,
      },
    });
    expect(w.find('[data-test="sidebar-rail"]').exists()).toBe(true);
    expect(w.find('[data-test="sidebar-rail-badge"]').text()).toBe("1");
    expect(w.findAll('[data-test="sidebar-rail-icon"]').length).toBe(2);
  });

  test("collapsed rail orders icons by urgency", () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: true,
        byHost: {
          h: [
            mk({ session_id: "running1", task_state: "running" }),
            mk({ session_id: "waiting", task_state: "waiting_input", attention_at: 1 }),
            mk({ session_id: "failed1", task_state: "failed" }),
          ],
        },
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "waiting_input",
        completedSeen: [],
        totalUnread: 0,
      },
    });
    const icons = w.findAll('[data-test="sidebar-rail-icon"]');
    expect(icons.length).toBe(3);
    // Order: waiting_input → failed → running
    expect(icons[0].attributes("aria-label")).toContain("waiting");
    expect(icons[1].attributes("aria-label")).toContain("failed1");
    expect(icons[2].attributes("aria-label")).toContain("running1");
  });

  test("collapse button emits update:collapsed=true", async () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: {},
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        totalUnread: 0,
      },
    });
    await w.find('[data-test="collapse-button"]').trigger("click");
    expect(w.emitted("update:collapsed")?.[0]).toEqual([true]);
  });

  test("Mark-all-read button emits markSeen all", async () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: { h: [mk({ unread: true, attention_at: 1 })] },
        unreadByHost: { h: 1 },
        primaryStateForHost: () => "running",
        completedSeen: [],
        totalUnread: 1,
      },
    });
    await w.find('[data-test="sidebar-mark-all"]').trigger("click");
    expect(w.emitted("markSeen")?.[0]?.[0]).toEqual({ all: true });
  });
});
