import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";

import RemoteSessionsDialog from "./RemoteSessionsDialog.vue";
import type { SessionInfo } from "../lib/connection";

function s(overrides: Partial<SessionInfo>): SessionInfo {
  return {
    id: "s",
    command: "bash",
    cwd: "/",
    title: "",
    cols: 80,
    rows: 24,
    started_at: 0,
    host_id: "",
    host: "",
    user: "",
    remote_permission: "",
    ...overrides,
  };
}

describe("RemoteSessionsDialog", () => {
  test("renders one host-group section per host_id in hostname order", () => {
    const sessions = [
      s({ id: "a", host_id: "hidB", host: "mac-mini", started_at: 1 }),
      s({ id: "b", host_id: "hidA", host: "attson-air", started_at: 1 }),
      s({ id: "c", host_id: "hidB", host: "mac-mini", started_at: 2 }),
    ];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    const groups = wrapper.findAll(".host-group");
    expect(groups).toHaveLength(2);
    // TaskGroupedList sorts hostKeys alphabetically (hidA < hidB)
    expect(groups[0].find(".host-name").text()).toBe("attson-air");
    expect(groups[1].find(".host-name").text()).toBe("mac-mini");
  });

  test("header shows session count", () => {
    const sessions = [
      s({ id: "a", host_id: "3f9a2c1d11112222", host: "mac-mini" }),
      s({ id: "b", host_id: "3f9a2c1d11112222", host: "mac-mini" }),
    ];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    const header = wrapper.get(".host-group header");
    expect(header.get(".count").text()).toBe("2");
  });

  test("singular count for groups with exactly one session", () => {
    const sessions = [s({ id: "a", host_id: "hidA", host: "alpha" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    expect(wrapper.get(".host-group header .count").text()).toBe("1");
  });

  test("unknown host group renders with unknown host label", () => {
    const sessions = [s({ id: "u", host_id: "", host: "" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    const header = wrapper.get(".host-group header");
    expect(header.get(".host-name").text()).toBe("unknown host");
  });

  test("task rows render without a per-card host line", () => {
    const sessions = [s({ id: "a", host_id: "hidA", host: "alpha", user: "alice" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    expect(wrapper.find(".card .host").exists()).toBe(false);
  });

  test("clicking a card emits 'open' with the session id", () => {
    const sessions = [s({ id: "abc", host_id: "hidA", host: "alpha" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    wrapper.get('[data-test="task-row"]').trigger("click");
    expect(wrapper.emitted("open")).toEqual([["abc"]]);
  });

  test("shows existing empty state when sessions is empty", () => {
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions: [] } });
    expect(wrapper.find(".host-group").exists()).toBe(false);
    expect(wrapper.find(".empty").exists()).toBe(true);
  });
});

test("renders via TaskGroupedList and emits markSeen all on the top button", async () => {
  const sessions: SessionInfo[] = [
    {
      id: "s1",
      command: "claude",
      cwd: "/",
      title: "claude",
      cols: 80,
      rows: 24,
      started_at: Date.now(),
      host_id: "h",
      host: "mac",
      user: "u",
      task_state: "running",
      unread: true,
    } as any,
  ];
  const w = mount(RemoteSessionsDialog, {
    props: { sessions, open: true },
  });
  expect(w.find('[data-test="task-row"]').exists()).toBe(true);
  await w.find('[data-test="dialog-mark-all"]').trigger("click");
  expect(w.emitted("markSeen")?.[0]?.[0]).toEqual({ all: true });
});

test("clicking a row still emits open with the session id", async () => {
  const session: SessionInfo = {
    id: "s1",
    command: "claude",
    cwd: "/",
    title: "claude",
    cols: 80,
    rows: 24,
    started_at: Date.now(),
    host_id: "h",
    host: "mac",
    user: "u",
  } as any;
  const w = mount(RemoteSessionsDialog, {
    props: { sessions: [session], open: true },
  });
  await w.find('[data-test="task-row"]').trigger("click");
  expect(w.emitted("open")?.[0]?.[0]).toBe("s1");
});
