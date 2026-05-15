import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";

import SessionPickerDialog from "./SessionPickerDialog.vue";
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

describe("SessionPickerDialog", () => {
  test("local section remains flat (no host groups)", () => {
    const local = [s({ id: "L1", cwd: "/home/me/work" })];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: [], localSessions: local, remoteSessions: [] },
    });
    const localSection = wrapper.get("section.local");
    expect(localSection.find(".host-group").exists()).toBe(false);
    expect(localSection.findAll(".card")).toHaveLength(1);
  });

  test("remote section renders one host-group per host_id", () => {
    const remote = [
      s({ id: "r1", host_id: "hidA", host: "alpha", user: "alice" }),
      s({ id: "r2", host_id: "hidB", host: "beta", user: "bob" }),
      s({ id: "r3", host_id: "hidA", host: "alpha", user: "alice" }),
    ];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: [], localSessions: [], remoteSessions: remote },
    });
    const remoteSection = wrapper.get("section.remote");
    const groups = remoteSection.findAll(".host-group");
    expect(groups).toHaveLength(2);
    expect(groups[0].get("header .hostname").text()).toBe("alpha");
    expect(groups[1].get("header .hostname").text()).toBe("beta");
  });

  test("remote card meta shows user only (not user@host)", () => {
    const remote = [s({ id: "r1", host_id: "hidA", host: "alpha", user: "alice" })];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: [], localSessions: [], remoteSessions: remote },
    });
    const who = wrapper.get("section.remote .card .who");
    expect(who.text()).toBe("alice");
    expect(who.text()).not.toContain("@");
  });

  test("remote card hides .who span when user is empty", () => {
    const remote = [s({ id: "r1", host_id: "hidA", host: "alpha", user: "" })];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: [], localSessions: [], remoteSessions: remote },
    });
    expect(wrapper.find("section.remote .card .who").exists()).toBe(false);
  });

  test("excludeSessionIds filters out matching sessions from both sections", () => {
    const local = [s({ id: "L1" })];
    const remote = [s({ id: "r1", host_id: "hidA", host: "alpha" })];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: ["L1", "r1"], localSessions: local, remoteSessions: remote },
    });
    expect(wrapper.find(".empty").exists()).toBe(true);
    expect(wrapper.find(".host-group").exists()).toBe(false);
  });

  test("clicking a remote card emits pick with remote:true", () => {
    const remote = [s({ id: "rid", host_id: "hidA", host: "alpha" })];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: [], localSessions: [], remoteSessions: remote },
    });
    wrapper.get("section.remote .card").trigger("click");
    expect(wrapper.emitted("pick")).toEqual([[{ sessionId: "rid", remote: true }]]);
  });
});
