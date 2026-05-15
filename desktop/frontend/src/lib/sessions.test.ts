import { describe, expect, it } from "vitest";

import type { SessionInfo } from "./connection";
import { groupSessionsByHost } from "./sessions";

function makeSession(overrides: Partial<SessionInfo> = {}): SessionInfo {
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

describe("groupSessionsByHost", () => {
  it("returns [] for empty input", () => {
    expect(groupSessionsByHost([])).toEqual([]);
  });

  it("groups by host_id and sorts groups by hostname ascending", () => {
    const sessions: SessionInfo[] = [
      makeSession({ id: "a1", host_id: "hidA", host: "mac-mini", started_at: 1 }),
      makeSession({ id: "b1", host_id: "hidB", host: "attson-air", started_at: 1 }),
      makeSession({ id: "a2", host_id: "hidA", host: "mac-mini", started_at: 2 }),
    ];
    const groups = groupSessionsByHost(sessions);
    expect(groups.map((g) => g.hostname)).toEqual(["attson-air", "mac-mini"]);
    expect(groups[0].sessions.map((s) => s.id)).toEqual(["b1"]);
    expect(groups[1].sessions.map((s) => s.id)).toEqual(["a1", "a2"]);
  });

  it("places sessions with empty host_id into a trailing __unknown__ group", () => {
    const sessions: SessionInfo[] = [
      makeSession({ id: "u1", host_id: "", host: "" }),
      makeSession({ id: "z1", host_id: "hidZ", host: "zeta" }),
      makeSession({ id: "a1", host_id: "hidA", host: "alpha" }),
    ];
    const groups = groupSessionsByHost(sessions);
    expect(groups.map((g) => g.key)).toEqual(["hidA", "hidZ", "__unknown__"]);
    expect(groups[2].hostname).toBe("unknown host");
    expect(groups[2].sessions.map((s) => s.id)).toEqual(["u1"]);
  });

  it("picks display hostname from the entry with the largest started_at", () => {
    const sessions: SessionInfo[] = [
      makeSession({ id: "old", host_id: "hidX", host: "old-name", started_at: 100 }),
      makeSession({ id: "new", host_id: "hidX", host: "new-name", started_at: 200 }),
    ];
    const groups = groupSessionsByHost(sessions);
    expect(groups).toHaveLength(1);
    expect(groups[0].hostname).toBe("new-name");
  });

  it("falls back to 'unknown host' when host is empty across the bucket", () => {
    const sessions: SessionInfo[] = [
      makeSession({ id: "x1", host_id: "hidX", host: "" }),
      makeSession({ id: "x2", host_id: "hidX", host: "" }),
    ];
    const groups = groupSessionsByHost(sessions);
    expect(groups).toHaveLength(1);
    expect(groups[0].key).toBe("hidX");
    expect(groups[0].hostname).toBe("unknown host");
  });

  it("collapses every empty-host_id session into a single __unknown__ group", () => {
    const sessions: SessionInfo[] = [
      makeSession({ id: "u1", host_id: "" }),
      makeSession({ id: "u2", host_id: "" }),
    ];
    const groups = groupSessionsByHost(sessions);
    expect(groups).toHaveLength(1);
    expect(groups[0].key).toBe("__unknown__");
    expect(groups[0].sessions.map((s) => s.id)).toEqual(["u1", "u2"]);
  });
});
