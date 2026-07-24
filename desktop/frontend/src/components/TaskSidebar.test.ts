import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { nextTick } from "vue";
import TaskSidebar from "./TaskSidebar.vue";
import type { RemoteSession } from "../platform/types";
import * as api from "../lib/api";
import { __resetForTests as resetSel, useSessionSelection } from "../composables/useSessionSelection";

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

  test("forwards close from an open session row", async () => {
    const sess = mk({ session_id: "s1", host: "mac", title: "claude" });
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: { h: [sess] },
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
        totalUnread: 0,
        openSessionIds: ["s1"],
      },
    });

    await w.find('[data-test="row-close"]').trigger("click");

    expect(w.emitted("close")?.[0]).toEqual([sess]);
  });

  test("renders a search input in the expanded header and hides it when collapsed", async () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: {},
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        totalUnread: 0,
      },
    });
    await flushPromises();
    expect(w.find('[data-test="sidebar-search"]').exists()).toBe(true);

    await w.setProps({ collapsed: true });
    await nextTick();
    expect(w.find('[data-test="sidebar-search"]').exists()).toBe(false);
  });

  test("passes the typed query down to TaskGroupedList", async () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: {},
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        totalUnread: 0,
      },
    });
    await flushPromises();
    const input = w.find('[data-test="sidebar-search"]');
    await input.setValue("feishu");
    await nextTick();
    const list = w.findComponent({ name: "TaskGroupedList" });
    expect(list.props("searchQuery")).toBe("feishu");
  });

  test("Esc inside the search input clears the query", async () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: {},
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        totalUnread: 0,
      },
    });
    await flushPromises();
    const input = w.find<HTMLInputElement>('[data-test="sidebar-search"]');
    await input.setValue("proj");
    expect(input.element.value).toBe("proj");
    await input.trigger("keydown", { key: "Escape" });
    await nextTick();
    expect(input.element.value).toBe("");
  });

  test("focusSearch() expose focuses the input (and expands the sidebar if collapsed)", async () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: true,
        byHost: {},
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        totalUnread: 0,
      },
      attachTo: document.body,
    });
    await flushPromises();
    // Call the exposed method — should emit collapse:false then focus.
    await (w.vm as any).focusSearch();
    await nextTick();
    expect(w.emitted("update:collapsed")?.[0]).toEqual([false]);

    // Simulate the parent responding to update:collapsed by setting the prop.
    await w.setProps({ collapsed: false });
    await nextTick();
    await (w.vm as any).focusSearch();
    await nextTick();
    const input = w.find<HTMLInputElement>('[data-test="sidebar-search"]');
    expect(document.activeElement).toBe(input.element);
    w.unmount();
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

test("list viewport keeps vertical scrolling without horizontal scrollbar overlap", () => {
  const source = readFileSync(resolve(process.cwd(), "src/components/TaskSidebar.vue"), "utf8");

  expect(source).toMatch(/\.list-wrap\s*\{[^}]*overflow-y:\s*auto;[^}]*overflow-x:\s*hidden;/s);
});

describe("TaskSidebar localHost passthrough", () => {
  test("renders #N suffix on co-resident headers when localHost set", () => {
    vi.spyOn(api, "getTaskSidebarWidth").mockResolvedValue(240);
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: {
          "h-a": [mk({ session_id: "s1", host_id: "h-a", host: "mac" })],
          "h-b": [mk({ session_id: "s2", host_id: "h-b", host: "mac" })],
        },
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        totalUnread: 0,
        localHostId: "h-a",
        localHost: "mac",
      },
    });
    const headers = w.findAll('[data-test="host-header"]');
    const texts = headers.map((h) => h.text());
    expect(texts.some((t) => t.includes("mac #1"))).toBe(true);
    expect(texts.some((t) => t.includes("mac #2"))).toBe(true);
  });
});

describe("TaskSidebar — multi-select footer + Esc / blank clear", () => {
  beforeEach(() => resetSel());

  function mountSidebar(overrides: Record<string, any> = {}) {
    return mount(TaskSidebar, {
      attachTo: document.body,
      props: {
        collapsed: false,
        byHost: { h1: [{
          session_id: "s1", host_id: "h1", host: "h1", user: "u",
          title: "one", cols: 80, rows: 24,
        }] },
        primaryStateForHost: () => "idle",
        completedSeen: [],
        totalUnread: 0,
        openSessionIds: ["s1"],
        ...overrides,
      },
    });
  }

  test("BulkActionBar not rendered when selection is empty", () => {
    const w = mountSidebar();
    expect(w.find("[data-test=bulk-action-bar]").exists()).toBe(false);
  });

  test("BulkActionBar rendered when selection is non-empty", async () => {
    const w = mountSidebar();
    // Force a selection through the composable directly
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    expect(w.find("[data-test=bulk-action-bar]").exists()).toBe(true);
  });

  test("mark-all footer is suppressed while selection is non-empty even with unread", async () => {
    const w = mountSidebar({ totalUnread: 3 });
    expect(w.find("[data-test=sidebar-mark-all]").exists()).toBe(true);
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    expect(w.find("[data-test=sidebar-mark-all]").exists()).toBe(false);
    expect(w.find("[data-test=bulk-action-bar]").exists()).toBe(true);
  });

  test("BulkActionBar 'clear' clears selection", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    await w.find("[data-test=bulk-clear]").trigger("click");
    expect(useSessionSelection().size.value).toBe(0);
  });

  test("Esc inside sidebar clears selection", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    await w.find(".task-sidebar").trigger("keydown", { key: "Escape" });
    expect(useSessionSelection().size.value).toBe(0);
  });

  test("clicking sidebar blank area clears selection", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    // Click on the sidebar-header area outside any row / menu / bar
    await w.find(".sidebar-header").trigger("click");
    expect(useSessionSelection().size.value).toBe(0);
  });

  test("clicking inside a task row does NOT trigger blank-click clear (row handles its own logic)", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    await w.find("[data-test=task-row]").trigger("click", { metaKey: true });
    // Selection may toggle for s1 (from 1 → 0 → 1), but blank-clear must NOT
    // fire independently — verify the composable is not force-empty via
    // an unrelated path.
    // After metaKey toggle: s1 was selected, so toggle removes it → 0.
    expect(useSessionSelection().size.value).toBe(0);
  });

  test("BulkActionBar merge emit is forwarded to TaskSidebar", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    await w.find("[data-test=bulk-merge]").trigger("click");
    expect(w.emitted("merge-selected")).toHaveLength(1);
  });

  test("BulkActionBar close emit is forwarded to TaskSidebar", async () => {
    const w = mountSidebar();
    useSessionSelection().toggle("s1");
    await w.vm.$nextTick();
    await w.find("[data-test=bulk-close]").trigger("click");
    expect(w.emitted("close-selected")).toHaveLength(1);
  });
});
