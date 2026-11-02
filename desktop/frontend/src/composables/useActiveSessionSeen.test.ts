import { computed, nextTick, ref } from "vue";
import { describe, expect, it, vi } from "vitest";
import type { RemoteSession } from "../platform/types";
import { useActiveSessionSeen } from "./useActiveSessionSeen";

function session(
  id: string,
  overrides: Partial<RemoteSession> = {},
): RemoteSession {
  return {
    session_id: id,
    cwd: "/work",
    title: id,
    cols: 80,
    rows: 24,
    started_at: 1,
    host_id: "host",
    host: "host",
    user: "user",
    unread: false,
    ...overrides,
  };
}

describe("useActiveSessionSeen", () => {
  it("marks an unread session when its tab becomes active", async () => {
    const activeSessionId = ref<string | null>("read");
    const sessions = ref([
      session("read"),
      session("unread", { unread: true, attention_at: 10 }),
    ]);
    const markSeen = vi.fn().mockResolvedValue(undefined);

    useActiveSessionSeen({
      activeSessionId: computed(() => activeSessionId.value),
      sessions: computed(() => sessions.value),
      markSeen,
    });
    expect(markSeen).not.toHaveBeenCalled();

    activeSessionId.value = "unread";
    await nextTick();

    expect(markSeen).toHaveBeenCalledOnce();
    expect(markSeen).toHaveBeenCalledWith("unread");
  });

  it("marks new attention that arrives while the tab stays active", async () => {
    const activeSessionId = ref<string | null>("active");
    const sessions = ref([session("active")]);
    const markSeen = vi.fn().mockResolvedValue(undefined);

    useActiveSessionSeen({
      activeSessionId: computed(() => activeSessionId.value),
      sessions: computed(() => sessions.value),
      markSeen,
    });

    sessions.value = [session("active", { unread: true, attention_at: 20 })];
    await nextTick();

    expect(markSeen).toHaveBeenCalledOnce();
    expect(markSeen).toHaveBeenCalledWith("active");
  });

  it("ignores unchanged snapshots and peer-only rows", async () => {
    const activeSessionId = ref<string | null>("relay");
    const sessions = ref([session("relay", { unread: true, attention_at: 30 })]);
    const markSeen = vi.fn().mockResolvedValue(undefined);

    useActiveSessionSeen({
      activeSessionId: computed(() => activeSessionId.value),
      sessions: computed(() => sessions.value),
      markSeen,
    });
    expect(markSeen).toHaveBeenCalledOnce();

    sessions.value = [session("relay", { unread: true, attention_at: 30, title: "updated" })];
    await nextTick();
    expect(markSeen).toHaveBeenCalledOnce();

    sessions.value = [session("relay", { unread: true, attention_at: 31, peer_direct: true })];
    await nextTick();
    expect(markSeen).toHaveBeenCalledOnce();
  });
});
