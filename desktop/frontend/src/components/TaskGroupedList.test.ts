import { describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import TaskGroupedList from "./TaskGroupedList.vue";
import type { RemoteSession } from "../platform/types";

vi.mock("../lib/api", () => ({
  getUserHomeDir: vi.fn().mockResolvedValue("/Users/attson"),
}));

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

describe("TaskGroupedList", () => {
  test("renders a row per session under host header", () => {
    const byHost = {
      h: [
        mk({ session_id: "s1", host: "mac", task_state: "running", title: "claude" }),
        mk({ session_id: "s2", host: "mac", task_state: "waiting_input", title: "test" }),
      ],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    expect(w.findAll('[data-test="task-row"]').length).toBe(2);
    expect(w.find('[data-test="host-header"]').text()).toContain("mac");
  });

  test("clicking a row emits open with the session", async () => {
    const sess = mk({ session_id: "s1", host: "mac", title: "claude" });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [sess] },
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    await w.find('[data-test="task-row"]').trigger("click");
    expect(w.emitted("open")?.[0]?.[0]).toEqual(sess);
  });

  test("clicking row mark-read emits markSeen ids", async () => {
    const sess = mk({ session_id: "s1", host: "mac", unread: true, attention_at: 1 });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [sess] },
        unreadByHost: { h: 1 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    await w.find('[data-test="row-mark-read"]').trigger("click");
    expect(w.emitted("markSeen")?.[0]?.[0]).toEqual({ ids: ["s1"] });
  });

  test("host header mark-all emits markSeen ids for that host's unread", async () => {
    const a = mk({ session_id: "a", host: "mac", unread: true, attention_at: 1 });
    const b = mk({ session_id: "b", host: "mac", unread: true, attention_at: 1 });
    const c = mk({ session_id: "c", host: "mac", unread: false });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [a, b, c] },
        unreadByHost: { h: 2 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    await w.find('[data-test="host-mark-all"]').trigger("click");
    expect(w.emitted("markSeen")?.[0]?.[0]).toEqual({ ids: ["a", "b"] });
  });

  test("completed fold is collapsed by default and expands on click", async () => {
    const w = mount(TaskGroupedList, {
      props: {
        byHost: {},
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [mk({ session_id: "x", task_state: "completed" })],
      },
    });
    expect(w.find('[data-test="completed-fold-row"]').exists()).toBe(false);
    await w.find('[data-test="completed-fold-toggle"]').trigger("click");
    expect(w.find('[data-test="completed-fold-row"]').exists()).toBe(true);
  });

  test("row renders the session cwd alongside the command", () => {
    const byHost = {
      h: [
        mk({
          session_id: "s1",
          host: "mac",
          task_state: "running",
          current_command: "claude",
          cwd: "/Users/attson/code/atterm",
        }),
      ],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    const row = w.find('[data-test="task-row"]');
    const text = row.text();
    expect(text).toContain("claude");
    expect(text).toMatch(/atterm/); // last segment always present after shortenCwd
  });

  test("row shows only the executable basename, full command lives in title", () => {
    const byHost = {
      h: [
        mk({
          session_id: "s1",
          host: "mac",
          task_state: "running",
          current_command: "/usr/local/bin/claude --permission-mode bypassPermissions",
          cwd: "/tmp",
        }),
      ],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    // Visible cmd span shows only "claude" — no flags, no leading path.
    const cmd = w.find('[data-test="task-row"] .cmd');
    expect(cmd.text()).toBe("claude");
    // Full command + cwd live in the row's title attribute (hover tooltip).
    const wrap = w.find('[data-test="task-row"] .cmd-and-cwd');
    const title = wrap.attributes("title") || "";
    expect(title).toContain("/usr/local/bin/claude --permission-mode bypassPermissions");
    expect(title).toContain("/tmp");
  });
});
