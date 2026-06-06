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

  test("rows within a host sorted: unread-first then urgency then last_output_at desc", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({
        session_id: "a",
        host_id: "h",
        task_state: "running",
        last_output_at: 100,
      }),
      mk({
        session_id: "b",
        host_id: "h",
        task_state: "completed",
        unread: true,
        attention_at: 1,
      }),
      mk({
        session_id: "c",
        host_id: "h",
        task_state: "waiting_input",
        attention_at: 1,
      }),
    ]);
    const { byHost } = useSessions(local, remote);
    expect(byHost.value["h"].map((s) => s.session_id)).toEqual(["c", "b", "a"]);
  });
});
