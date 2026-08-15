import { describe, expect, test } from "vitest";
import { ref } from "vue";
import type { RemoteSession } from "../platform/types";
import { useSessions } from "./useSessions";

function mk(over: Partial<RemoteSession>): RemoteSession {
  return {
    session_id: "s",
    host_id: "h",
    host: "host",
    user: "u",
    title: "",
    cols: 80,
    rows: 24,
    ...over,
  };
}

describe("useSessions", () => {
  test("merges local + remote; relay wins on same session_id", () => {
    const local = ref<RemoteSession[]>([
      mk({ session_id: "s1", host_id: "h1", task_state: "running" }),
    ]);
    const remote = ref<RemoteSession[]>([
      mk({
        session_id: "s1",
        host_id: "h1",
        task_state: "completed",
        unread: true,
        attention_at: 1000,
      }),
    ]);
    const { all } = useSessions(local, remote);
    expect(all.value.length).toBe(1);
    expect(all.value[0].task_state).toBe("completed"); // relay wins
    expect(all.value[0].unread).toBe(true);
  });

  test("byHost groups by host_id; remoteByHost excludes local-only hosts", () => {
    const local = ref<RemoteSession[]>([
      mk({ session_id: "L1", host_id: "h-local", host: "mac" }),
    ]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "R1", host_id: "h-remote", host: "server-1" }),
    ]);
    const { byHost, remoteByHost } = useSessions(local, remote, {
      localHostId: "h-local",
    });
    expect(Object.keys(byHost.value).sort()).toEqual(["h-local", "h-remote"]);
    expect(Object.keys(remoteByHost.value)).toEqual(["h-remote"]);
  });

  test("unreadByHost counts only sessions with unread===true", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "a", host_id: "h", attention_at: 1, unread: true }),
      mk({ session_id: "b", host_id: "h", attention_at: 1, unread: false }),
      mk({ session_id: "c", host_id: "h" }),
    ]);
    const { unreadByHost, totalUnread } = useSessions(local, remote);
    expect(unreadByHost.value["h"]).toBe(1);
    expect(totalUnread.value).toBe(1);
  });

  test("primaryStateForHost respects urgency order", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "a", host_id: "h", task_state: "completed" }),
      mk({ session_id: "b", host_id: "h", task_state: "running" }),
      mk({ session_id: "c", host_id: "h", task_state: "waiting_input" }),
    ]);
    const { primaryStateForHost } = useSessions(local, remote);
    expect(primaryStateForHost("h")).toBe("waiting_input");
  });

  test("completedSeen is sessions completed/failed with unread===false", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "a", host_id: "h", task_state: "completed", unread: false }),
      mk({ session_id: "b", host_id: "h", task_state: "failed", unread: false }),
      mk({ session_id: "c", host_id: "h", task_state: "completed", unread: true }),
      mk({ session_id: "d", host_id: "h", task_state: "running" }),
    ]);
    const { completedSeen } = useSessions(local, remote);
    expect(completedSeen.value.map((s) => s.session_id).sort()).toEqual(["a", "b"]);
  });

  // Running sessions belong above finished ones regardless of clock order: the
  // list is read top-down for "what needs me", and both grouping modes have to
  // agree on that.
  test("rows within a host sort by state urgency first", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "done", host_id: "h", task_state: "completed", command_ended_at: 9000 }),
      mk({ session_id: "busy", host_id: "h", task_state: "running", command_started_at: 100 }),
      mk({ session_id: "asks", host_id: "h", task_state: "waiting_input", attention_at: 50 }),
    ]);
    const { byHost } = useSessions(local, remote);
    expect(byHost.value["h"].map((s) => s.session_id)).toEqual(["asks", "busy", "done"]);
  });

  // Within one state the order is still the stable interaction stamp, so two
  // running AI sessions do not trade places while they stream.
  test("running sessions hold their order among themselves", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "older", host_id: "h", task_state: "running", command_started_at: 100, last_output_at: 9999 }),
      mk({ session_id: "newer", host_id: "h", task_state: "running", command_started_at: 900, last_output_at: 1 }),
    ]);
    const { byHost } = useSessions(local, remote);
    expect(byHost.value["h"].map((s) => s.session_id)).toEqual(["newer", "older"]);
  });

  test("rows within a host sort by latest activity descending", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({
        session_id: "a",
        host_id: "h",
        task_state: "running",
        last_output_at: 999,
        started_at: 100,
      }),
      mk({
        session_id: "b",
        host_id: "h",
        task_state: "completed",
        unread: true,
        attention_at: 1,
        command_ended_at: 1200,
        started_at: 200,
      }),
      mk({
        session_id: "c",
        host_id: "h",
        task_state: "waiting_input",
        attention_at: 800,
        started_at: 300,
      }),
    ]);
    const { byHost } = useSessions(local, remote);
    // Urgency first: c is waiting_input, a is running, b has completed. Within
    // a state the interaction stamp decides — `a` streams output at 999 but
    // carries no interaction stamp, and no longer outranks anyone on output
    // alone, which is the leapfrogging that used to churn the list.
    expect(byHost.value["h"].map((s) => s.session_id)).toEqual(["c", "a", "b"]);
  });

  test("a running session ranks by when its command started, not by its output", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "old", host_id: "h", task_state: "running", command_started_at: 500, last_output_at: 9999 }),
      mk({ session_id: "new", host_id: "h", task_state: "running", command_started_at: 900, last_output_at: 1 }),
    ]);
    const { byHost } = useSessions(local, remote);
    expect(byHost.value["h"].map((s) => s.session_id)).toEqual(["new", "old"]);
  });

  test("rows with identical latest activity fall back to session_id for deterministic order", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "z", host_id: "h", last_output_at: 100 }),
      mk({ session_id: "a", host_id: "h", last_output_at: 100 }),
      mk({ session_id: "m", host_id: "h", last_output_at: 100 }),
    ]);
    const { byHost } = useSessions(local, remote);
    expect(byHost.value["h"].map((s) => s.session_id)).toEqual(["a", "m", "z"]);
  });

  test("rows without activity timestamps sort last but stay deterministic via session_id", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "withTime", host_id: "h", started_at: 500 }),
      mk({ session_id: "noTimeB", host_id: "h" }),
      mk({ session_id: "noTimeA", host_id: "h" }),
    ]);
    const { byHost } = useSessions(local, remote);
    expect(byHost.value["h"].map((s) => s.session_id))
      .toEqual(["withTime", "noTimeA", "noTimeB"]);
  });
});
