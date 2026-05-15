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
    expect(groups[0].find("header .hostname").text()).toBe("attson-air");
    expect(groups[1].find("header .hostname").text()).toBe("mac-mini");
  });

  test("header shows short host_id, full host_id in title, and session count", () => {
    const sessions = [
      s({ id: "a", host_id: "3f9a2c1d11112222", host: "mac-mini" }),
      s({ id: "b", host_id: "3f9a2c1d11112222", host: "mac-mini" }),
    ];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    const header = wrapper.get(".host-group header");
    const hostid = header.get(".hostid");
    expect(hostid.text()).toBe("3f9a2c1d");
    expect(hostid.attributes("title")).toBe("host_id 3f9a2c1d11112222");
    expect(header.get(".count").text()).toBe("2 sessions");
  });

  test("singular count for groups with exactly one session", () => {
    const sessions = [s({ id: "a", host_id: "hidA", host: "alpha" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    expect(wrapper.get(".host-group header .count").text()).toBe("1 session");
  });

  test("unknown host group renders without a host_id chip", () => {
    const sessions = [s({ id: "u", host_id: "", host: "" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    const header = wrapper.get(".host-group header");
    expect(header.get(".hostname").text()).toBe("unknown host");
    expect(header.find(".hostid").exists()).toBe(false);
  });

  test("cards no longer render the per-card host line", () => {
    const sessions = [s({ id: "a", host_id: "hidA", host: "alpha", user: "alice" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    expect(wrapper.find(".card .host").exists()).toBe(false);
  });

  test("clicking a card emits 'open' with the session id", () => {
    const sessions = [s({ id: "abc", host_id: "hidA", host: "alpha" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    wrapper.get(".card").trigger("click");
    expect(wrapper.emitted("open")).toEqual([["abc"]]);
  });

  test("shows existing empty state when sessions is empty", () => {
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions: [] } });
    expect(wrapper.find(".host-group").exists()).toBe(false);
    expect(wrapper.find(".empty").exists()).toBe(true);
  });
});
