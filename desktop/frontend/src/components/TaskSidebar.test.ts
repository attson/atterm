import { beforeEach, describe, expect, test, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import TaskSidebar from "./TaskSidebar.vue";
import type { RemoteSession } from "../platform/types";
import * as api from "../lib/api";

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
  beforeEach(() => {
    vi.spyOn(api, "getTaskSidebarWidth").mockResolvedValue(240);
  });

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

test("drag handle emits pointerdown→move→up and persists width", async () => {
  const setSpy = vi.fn().mockResolvedValue(undefined);
  vi.spyOn(api, "getTaskSidebarWidth").mockResolvedValue(240);
  vi.spyOn(api, "setTaskSidebarWidth").mockImplementation(setSpy);

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
  await flushPromises();

  const handle = w.find('[data-test="sidebar-resize-handle"]');
  expect(handle.exists()).toBe(true);

  // pointerdown at x=240, move to x=340, pointerup at x=340 → width=340.
  await handle.trigger("pointerdown", { clientX: 240, pointerId: 1 });
  await handle.trigger("pointermove", { clientX: 340, pointerId: 1 });
  await handle.trigger("pointerup", { clientX: 340, pointerId: 1 });
  await flushPromises();

  expect(setSpy).toHaveBeenCalledTimes(1);
  expect(setSpy).toHaveBeenCalledWith(340);
});

test("drag handle clamps to bounds [180, 480]", async () => {
  const setSpy = vi.fn().mockResolvedValue(undefined);
  vi.spyOn(api, "getTaskSidebarWidth").mockResolvedValue(240);
  vi.spyOn(api, "setTaskSidebarWidth").mockImplementation(setSpy);

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
  await flushPromises();

  const handle = w.find('[data-test="sidebar-resize-handle"]');
  await handle.trigger("pointerdown", { clientX: 240, pointerId: 1 });
  await handle.trigger("pointermove", { clientX: 1000, pointerId: 1 });
  await handle.trigger("pointerup", { clientX: 1000, pointerId: 1 });
  await flushPromises();

  expect(setSpy).toHaveBeenCalledWith(480);
});

test("collapsed sidebar does not render drag handle", () => {
  vi.spyOn(api, "getTaskSidebarWidth").mockResolvedValue(240);
  const w = mount(TaskSidebar, {
    props: {
      collapsed: true,
      byHost: {},
      unreadByHost: {},
      primaryStateForHost: () => "idle",
      completedSeen: [],
      totalUnread: 0,
    },
  });
  expect(w.find('[data-test="sidebar-resize-handle"]').exists()).toBe(false);
});
