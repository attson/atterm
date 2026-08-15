import { afterEach, describe, expect, it, test, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { nextTick } from "vue";
import TaskGroupedList from "./TaskGroupedList.vue";
import source from "./TaskGroupedList.vue?raw";
import TaskStateIcon from "./TaskStateIcon.vue";
import type { RemoteSession } from "../platform/types";
import { __resetForTests as resetPins } from "../composables/useSessionPins";
import { __resetForTests as resetSel, useSessionSelection } from "../composables/useSessionSelection";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";

vi.mock("../lib/api", () => ({
  getUserHomeDir: vi.fn().mockResolvedValue("/Users/attson"),
}));

// TaskGroupedList mounts useSessionPins() (via SessionDetailsPopover /
// SessionRowMenu) unconditionally on setup, so every test needs a platform —
// pins now flow through platform.sessions.getPins/setPins instead of the old
// lib/api mock.
let platform: ReturnType<typeof createFakePlatform>;
beforeEach(() => {
  platform = createFakePlatform();
  __setPlatformForTests(platform);
});
afterEach(() => {
  vi.useRealTimers();
  __setPlatformForTests(null);
});

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

// Small factory so multi-select tests (and any future ones) don't repeat
// `mount(TaskGroupedList, { props: ... })` boilerplate. Existing describe
// blocks above mount inline and are left as-is.
function mountList(props: Record<string, unknown>) {
  return mount(TaskGroupedList, { props: props as any });
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

  // Per-row mark-read is gone: unread clears itself on attach, and the group
  // header's mark-all is the remaining manual dismissal (covered below).
  test("rows offer no mark-read control", async () => {
    const sess = mk({ session_id: "s1", host: "mac", unread: true, attention_at: 1 });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [sess] },
        unreadByHost: { h: 1 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    expect(w.find('[data-test="row-mark-read"]').exists()).toBe(false);
    expect(w.find('[data-test="host-mark-all"]').exists()).toBe(true);
  });

  test("clicking row close emits close without opening the session", async () => {
    const sess = mk({ session_id: "s1", host: "mac", title: "claude" });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [sess] },
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
        openSessionIds: ["s1"],
      },
    });

    await w.find('[data-test="row-close"]').trigger("click");

    expect(w.emitted("close")?.[0]?.[0]).toEqual(sess);
    expect(w.emitted("open")).toBeUndefined();
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

  test("completed fold preserves the same last-output second row", async () => {
    const w = mount(TaskGroupedList, {
      props: {
        byHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [mk({
          session_id: "x",
          task_state: "completed",
          cwd: "",
          last_output_at: Math.floor(Date.now() / 1000) - 2 * 60 * 60,
        })],
      },
    });
    await w.get('[data-test="completed-fold-toggle"]').trigger("click");
    const row = w.get('[data-test="completed-fold-row"]');
    expect(row.find(".cwd").exists()).toBe(false);
    expect(row.get('[data-test="last-output"]').text()).toBe("2h");
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

describe("TaskGroupedList local host", () => {
  test("pins the local host group to the top, ignoring alphabetical order", () => {
    const byHost = {
      "zzz-remote": [mk({ session_id: "r1", host: "remote-z" })],
      "aaa-remote": [mk({ session_id: "r2", host: "remote-a" })],
      "mac-local": [mk({ session_id: "l1", host: "mac" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        localHostId: "mac-local",
      },
    });
    const headers = w.findAll('[data-test="host-header"]');
    expect(headers[0].text()).toContain("mac");
    // Remaining alphabetical: aaa- then zzz-.
    expect(headers[1].text()).toContain("remote-a");
    expect(headers[2].text()).toContain("remote-z");
  });

  test("shows the 'this machine' chip only on the local host header", () => {
    const byHost = {
      "mac-local": [mk({ session_id: "l1", host: "mac" })],
      "remote": [mk({ session_id: "r1", host: "rem" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        localHostId: "mac-local",
      },
    });
    const chips = w.findAll('[data-test="local-chip"]');
    expect(chips.length).toBe(1);
    // The single chip is inside the first (local) host header.
    const firstHeader = w.find('[data-test="host-header"]');
    expect(firstHeader.find('[data-test="local-chip"]').exists()).toBe(true);
  });

  test("no chip and no reorder when localHostId is empty", () => {
    const byHost = {
      "zzz": [mk({ session_id: "z", host: "zzz-name" })],
      "aaa": [mk({ session_id: "a", host: "aaa-name" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        // localHostId omitted → default ""
      },
    });
    expect(w.findAll('[data-test="local-chip"]').length).toBe(0);
    const headers = w.findAll('[data-test="host-header"]');
    expect(headers[0].text()).toContain("aaa-name");
    expect(headers[1].text()).toContain("zzz-name");
  });

  test("no chip in state grouping mode even when localHostId matches a state name", () => {
    // Defensive: if some user named a host "running" it'd collide, but the
    // chip should only render under groupBy=host.
    const w = mount(TaskGroupedList, {
      props: {
        byHost: {},
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        groupBy: "state",
        byState: { running: [mk({ session_id: "x", task_state: "running" })] },
        unreadByState: { running: 0 },
        localHostId: "running",
      },
    });
    expect(w.findAll('[data-test="local-chip"]').length).toBe(0);
  });
});

describe("TaskGroupedList AI title", () => {
  test("shows AI session OSC title in the row when present", () => {
    const sess = mk({
      session_id: "s1",
      host: "mac",
      task_state: "running",
      title: "Remove token auth from relay login",
      current_command: "/usr/local/bin/claude --foo",
      type: "ai",
    });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [sess] },
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    const cmd = w.find('[data-test="task-row"] .cmd');
    expect(cmd.text()).toBe("Remove token auth from relay login");
  });

  test("shows OSC title for non-ai shell sessions when it carries signal", () => {
    // Real remote shell sessions drive OSC 2 to a meaningful label
    // (project dir or user-set); the sidebar should surface it instead
    // of collapsing every zsh row to "zsh".
    const sess = mk({
      session_id: "s1",
      host: "mac",
      task_state: "running",
      title: "user@host: ~/proj",
      current_command: "zsh",
      type: "shell",
    });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [sess] },
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    const cmd = w.find('[data-test="task-row"] .cmd');
    expect(cmd.text()).toBe("user@host: ~/proj");
  });

  test("suppresses title == command basename to avoid duplicated \"zsh\"", () => {
    const sess = mk({
      session_id: "s1",
      host: "mac",
      task_state: "running",
      title: "zsh",
      current_command: "zsh",
      type: "shell",
    });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [sess] },
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    const cmd = w.find('[data-test="task-row"] .cmd');
    expect(cmd.text()).toBe("zsh");
  });

  test("clicking host-header collapses the group; clicking again expands", async () => {
    const byHost = {
      h: [
        mk({ session_id: "s1", host: "mac", title: "a" }),
        mk({ session_id: "s2", host: "mac", title: "b" }),
      ],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "idle",
        completedSeen: [],
      },
    });
    expect(w.findAll('[data-test="task-row"]').length).toBe(2);
    // Caret is now an inline SVG (iOS 26.3 dropped the ▶/▼ text glyphs).
    // Detect direction by aria-expanded + the presence of the caret SVG.
    expect(w.get('[data-test="host-header"]').attributes("aria-expanded")).toBe("true");
    expect(w.find('[data-test="host-header"] .caret svg').exists()).toBe(true);

    await w.get('[data-test="host-header"]').trigger("click");
    expect(w.findAll('[data-test="task-row"]').length).toBe(0);
    expect(w.get('[data-test="host-header"]').attributes("aria-expanded")).toBe("false");

    await w.get('[data-test="host-header"]').trigger("click");
    expect(w.findAll('[data-test="task-row"]').length).toBe(2);
    expect(w.get('[data-test="host-header"]').attributes("aria-expanded")).toBe("true");
  });

  test("mark-all button inside header does NOT toggle collapse (click.stop)", async () => {
    const byHost = {
      h: [mk({ session_id: "s1", host: "mac", title: "a", unread: true })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: { h: 1 },
        primaryStateForHost: () => "idle",
        completedSeen: [],
      },
    });
    expect(w.findAll('[data-test="task-row"]').length).toBe(1);
    await w.get('[data-test="host-mark-all"]').trigger("click");
    // Rows still visible — collapse was not triggered.
    expect(w.findAll('[data-test="task-row"]').length).toBe(1);
    // markSeen emitted once with the unread ids.
    expect(w.emitted("markSeen")?.[0]).toEqual([{ ids: ["s1"] }]);
  });
});

describe("TaskGroupedList co-resident numbering", () => {
  test("appends #N to local-host groups when 2+ host_ids share the local hostname", () => {
    const byHost = {
      "h-b": [mk({ session_id: "s1", host: "mac" })],
      "h-a": [mk({ session_id: "s2", host: "mac" })],
      "remote-x": [mk({ session_id: "s3", host: "other-mac" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        localHostId: "h-b",
        localHost: "mac",
      },
    });
    const headers = w.findAll('[data-test="host-header"]');
    // localHostId is pinned to top: h-b (#2) first.
    expect(headers[0].text()).toContain("mac #2");
    expect(headers[1].text()).toContain("mac #1");
    expect(headers[2].text()).toContain("other-mac");
    expect(headers[2].text()).not.toContain("#");
  });

  test("no #N suffix when only one local host_id is present", () => {
    const byHost = {
      "h-a": [mk({ session_id: "s1", host: "mac" })],
      "remote": [mk({ session_id: "s2", host: "other" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        localHostId: "h-a",
        localHost: "mac",
      },
    });
    const headers = w.findAll('[data-test="host-header"]');
    expect(headers[0].text()).toContain("mac");
    expect(headers[0].text()).not.toContain("#");
  });

  test("'this machine' chip stays on current-process group only, even with #N suffix", () => {
    const byHost = {
      "h-a": [mk({ session_id: "s1", host: "mac" })],
      "h-b": [mk({ session_id: "s2", host: "mac" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        localHostId: "h-b",
        localHost: "mac",
      },
    });
    const chips = w.findAll('[data-test="local-chip"]');
    expect(chips.length).toBe(1);
  });

  test("remote machines with multiple host_ids are NOT renumbered", () => {
    const byHost = {
      "h-local": [mk({ session_id: "l", host: "my-mac" })],
      "h-r1": [mk({ session_id: "r1", host: "other-mac" })],
      "h-r2": [mk({ session_id: "r2", host: "other-mac" })],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        localHostId: "h-local",
        localHost: "my-mac",
      },
    });
    const headers = w.findAll('[data-test="host-header"]');
    for (const h of headers) {
      expect(h.text()).not.toContain("#");
    }
  });
});

describe("TaskGroupedList pinned group", () => {
  function seededProps(pinnedIds: string[] = []) {
    platform.sessions.getPins = vi.fn().mockResolvedValue(pinnedIds);
    platform.sessions.setPins = vi.fn().mockResolvedValue(undefined);
    return {
      byHost: {
        h1: [
          mk({ session_id: "a", host_id: "h1", host: "h1", task_state: "running" }),
          mk({ session_id: "b", host_id: "h1", host: "h1", task_state: "running" }),
        ],
        h2: [
          mk({ session_id: "c", host_id: "h2", host: "h2", task_state: "running" }),
          mk({ session_id: "d", host_id: "h2", host: "h2", task_state: "running" }),
        ],
      },
      unreadByHost: { h1: 0, h2: 0 },
      primaryStateForHost: () => "running" as const,
      completedSeen: [],
      groupBy: "host" as const,
    };
  }

  beforeEach(() => {
    resetPins();
    vi.restoreAllMocks();
  });

  test("renders pinned group at the top when at least one pinned session exists", async () => {
    const w = mount(TaskGroupedList, { props: seededProps(["b"]) });
    await flushPromises();
    await nextTick();
    const header = w.find("[data-test=pinned-group-header]");
    expect(header.exists()).toBe(true);
  });

  test("pinned session does not appear in its original host group", async () => {
    const w = mount(TaskGroupedList, { props: seededProps(["b"]) });
    await flushPromises();
    await nextTick();
    const rows = w.findAll("[data-test=task-row]");
    const ids = rows.map((r) => r.attributes("data-session-id"));
    // "b" should appear exactly once (in pinned group), not twice.
    const count = ids.filter((id) => id === "b").length;
    expect(count).toBe(1);
  });

  test("pinned group is absent when no session is pinned", async () => {
    const w = mount(TaskGroupedList, { props: seededProps([]) });
    await flushPromises();
    await nextTick();
    expect(w.find("[data-test=pinned-group-header]").exists()).toBe(false);
  });

  test("sorts pinned rows by unread, activity, and session id", async () => {
    const props = seededProps(["a", "b"]);
    props.byHost.h1 = [
      mk({ session_id: "b", host_id: "h1", host: "h1", task_state: "running", started_at: 100 }),
      mk({ session_id: "a", host_id: "h1", host: "h1", task_state: "running", started_at: 100, unread: true }),
    ];
    const w = mount(TaskGroupedList, { props });
    await flushPromises();
    await nextTick();
    const ids = w.findAll("[data-test=task-row]").map((r) => r.attributes("data-session-id"));
    expect(ids.slice(0, 2)).toEqual(["a", "b"]);
  });

  test("@contextmenu on a task row opens the SessionRowMenu", async () => {
    const w = mount(TaskGroupedList, { props: seededProps([]) });
    await flushPromises();
    await nextTick();
    const firstRow = w.find("[data-test=task-row]");
    await firstRow.trigger("contextmenu");
    expect(w.find("[data-test=session-row-menu]").exists()).toBe(true);
  });

  test("task rows disable native text selection and iOS touch callout", () => {
    const taskRowRule = source.match(/\.task-row\s*\{[^}]*\}/)?.[0] ?? "";
    expect(taskRowRule).toContain("user-select: none");
    expect(taskRowRule).toContain("-webkit-user-select: none");
    expect(taskRowRule).toContain("-webkit-touch-callout: none");
  });

  test("touch long-press for 1s opens the SessionRowMenu without opening the row", async () => {
    vi.useFakeTimers();
    const w = mount(TaskGroupedList, { props: seededProps([]) });
    await flushPromises();
    await nextTick();
    const firstRow = w.find("[data-test=task-row]");

    await firstRow.trigger("pointerdown", {
      pointerType: "touch",
      button: 0,
      clientX: 42,
      clientY: 64,
    });
    vi.advanceTimersByTime(999);
    await nextTick();
    expect(w.find("[data-test=session-row-menu]").exists()).toBe(false);

    vi.advanceTimersByTime(1);
    await nextTick();
    expect(w.find("[data-test=session-row-menu]").exists()).toBe(true);

    await firstRow.trigger("pointerup", { pointerType: "touch" });
    await firstRow.trigger("click");
    expect(w.emitted("open")).toBeUndefined();
    vi.useRealTimers();
  });

  test("touch long-press cancels when released before 1s", async () => {
    vi.useFakeTimers();
    const w = mount(TaskGroupedList, { props: seededProps([]) });
    await flushPromises();
    await nextTick();
    const firstRow = w.find("[data-test=task-row]");

    await firstRow.trigger("pointerdown", {
      pointerType: "touch",
      button: 0,
      clientX: 42,
      clientY: 64,
    });
    vi.advanceTimersByTime(999);
    await firstRow.trigger("pointerup", { pointerType: "touch" });
    vi.advanceTimersByTime(1);
    await nextTick();

    expect(w.find("[data-test=session-row-menu]").exists()).toBe(false);
    vi.useRealTimers();
  });

  test("touch long-press cancels when the finger moves like a scroll gesture", async () => {
    vi.useFakeTimers();
    const w = mount(TaskGroupedList, { props: seededProps([]) });
    await flushPromises();
    await nextTick();
    const firstRow = w.find("[data-test=task-row]");

    await firstRow.trigger("pointerdown", {
      pointerType: "touch",
      button: 0,
      clientX: 42,
      clientY: 64,
    });
    await firstRow.trigger("pointermove", {
      pointerType: "touch",
      clientX: 42,
      clientY: 80,
    });
    vi.advanceTimersByTime(1000);
    await nextTick();

    expect(w.find("[data-test=session-row-menu]").exists()).toBe(false);
    vi.useRealTimers();
  });

  test("host header count/unread/state reflect filtered rows after pinning", async () => {
    // h1 has 'a' (idle, read) and 'b' (failed, unread). Pin 'b' — it moves
    // into the virtual pinned group and should no longer influence h1's
    // header count, unread badge/mark-all button, or state icon.
    platform.sessions.getPins = vi.fn().mockResolvedValue(["b"]);
    platform.sessions.setPins = vi.fn().mockResolvedValue(undefined);
    const a = mk({ session_id: "a", host_id: "h1", host: "h1", task_state: "idle", unread: false });
    const b = mk({ session_id: "b", host_id: "h1", host: "h1", task_state: "failed", unread: true, attention_at: 1 });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h1: [a, b] },
        unreadByHost: { h1: 1 },
        primaryStateForHost: () => "failed" as const,
        completedSeen: [],
        groupBy: "host" as const,
      },
    });
    await flushPromises();
    await nextTick();

    const header = w.find('[data-test="host-group-h1"] [data-test="host-header"]');
    expect(header.exists()).toBe(true);

    // Count badge reflects only the visible (non-pinned) row.
    expect(header.find(".count").text()).toBe("1");

    // No unread badge/mark-all button — the only visible row ('a') is read.
    expect(header.find(".unread-badge").exists()).toBe(false);
    expect(header.find('[data-test="host-mark-all"]').exists()).toBe(false);

    // State icon reflects 'a' (idle), not the pinned-away 'b' (failed).
    const icon = header.findComponent(TaskStateIcon);
    expect(icon.props("state")).toBe("idle");
  });
});

describe("TaskGroupedList search filter", () => {
  beforeEach(() => {
    resetPins();
    vi.restoreAllMocks();
  });

  it("filters rows by searchQuery across title/cwd/current_command", async () => {
    const s1 = { session_id: "a", host_id: "h", title: "Feishu Gateway", cwd: "/proj/api", cols: 80, rows: 24, started_at: 0 } as any;
    const s2 = { session_id: "b", host_id: "h", title: "Web app", cwd: "/proj/web", cols: 80, rows: 24, started_at: 0 } as any;
    const s3 = { session_id: "c", host_id: "h", title: "Runner", cwd: "/tmp", current_command: "npm run build", cols: 80, rows: 24, started_at: 0 } as any;
    const wrapper = mount(TaskGroupedList, {
      props: {
        byHost: { h: [s1, s2, s3] },
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        groupBy: "host",
        byState: {},
        searchQuery: "feishu",
      },
    });
    await nextTick();
    const rows = wrapper.findAll('[data-test="task-row"]');
    expect(rows).toHaveLength(1);
    expect(rows[0].attributes("data-session-id")).toBe("a");
  });

  it("auto-expands a collapsed group when a match lives inside it", async () => {
    const s1 = { session_id: "a", host_id: "h", title: "Feishu Gateway", cwd: "", cols: 80, rows: 24, started_at: 0 } as any;
    const wrapper = mount(TaskGroupedList, {
      props: {
        byHost: { h: [s1] },
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        groupBy: "host",
        byState: {},
        searchQuery: "",
      },
    });
    await nextTick();
    // Collapse the group first.
    await wrapper.find('[data-test="host-header"]').trigger("click");
    await nextTick();
    expect(wrapper.findAll('[data-test="task-row"]')).toHaveLength(0);
    // Now type a matching query — the collapse should short-circuit and the
    // row should reappear without us having to click to expand.
    await wrapper.setProps({ searchQuery: "feishu" });
    await nextTick();
    expect(wrapper.findAll('[data-test="task-row"]')).toHaveLength(1);
    // Clearing the query restores the original (still-collapsed) state.
    await wrapper.setProps({ searchQuery: "" });
    await nextTick();
    expect(wrapper.findAll('[data-test="task-row"]')).toHaveLength(0);
  });

  it("hides groups whose filtered list is empty and shows an empty-state hint when nothing matches", async () => {
    const s1 = { session_id: "a", host_id: "h", title: "Feishu Gateway", cwd: "", cols: 80, rows: 24, started_at: 0 } as any;
    const wrapper = mount(TaskGroupedList, {
      props: {
        byHost: { h: [s1] },
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        groupBy: "host",
        byState: {},
        searchQuery: "no-such-thing",
      },
    });
    await nextTick();
    expect(wrapper.findAll('[data-test="task-row"]')).toHaveLength(0);
    expect(wrapper.findAll('[data-test="host-header"]')).toHaveLength(0);
    const empty = wrapper.find('[data-test="search-empty"]');
    expect(empty.exists()).toBe(true);
    expect(empty.text()).toContain("no-such-thing");
  });

  test("pinned session disappears from the pinned group when the query doesn't match it", async () => {
    platform.sessions.getPins = vi.fn().mockResolvedValue(["b"]);
    platform.sessions.setPins = vi.fn().mockResolvedValue(undefined);
    const a = mk({ session_id: "a", host_id: "h1", host: "h1", task_state: "running", title: "Feishu Gateway" });
    const b = mk({ session_id: "b", host_id: "h1", host: "h1", task_state: "running", title: "Web app" });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h1: [a, b] },
        unreadByHost: { h1: 0 },
        primaryStateForHost: () => "running" as const,
        completedSeen: [],
        groupBy: "host" as const,
        searchQuery: "",
      },
    });
    await flushPromises();
    await nextTick();
    expect(w.find("[data-test=pinned-group-header]").exists()).toBe(true);

    await w.setProps({ searchQuery: "feishu" });
    await nextTick();
    // The pinned group vanishes entirely — its only member ("b") doesn't
    // match the query, even though "a" (unpinned) does.
    expect(w.find("[data-test=pinned-group-header]").exists()).toBe(false);
    const rows = w.findAll("[data-test=task-row]");
    expect(rows.map((r) => r.attributes("data-session-id"))).toEqual(["a"]);
  });

  test("completed fold is filtered by search query", async () => {
    const done1 = mk({ session_id: "d1", host_id: "h", host: "h", title: "Feishu Gateway" });
    const done2 = mk({ session_id: "d2", host_id: "h", host: "h", title: "Web app" });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: {},
        primaryStateForHost: () => "idle" as const,
        completedSeen: [done1, done2],
        groupBy: "host" as const,
        searchQuery: "feishu",
      },
    });
    await flushPromises();
    await nextTick();

    // Fold header count reflects only the matching completed row.
    const toggle = w.find('[data-test="completed-fold-toggle"]');
    expect(toggle.exists()).toBe(true);
    expect(toggle.text()).toContain("1");

    // No groups/pinned matches, but the completed fold has one — the
    // empty-state hint must not appear.
    expect(w.find('[data-test="search-empty"]').exists()).toBe(false);

    // Expanding the fold renders only the matching row.
    await toggle.trigger("click");
    await nextTick();
    const rows = w.findAll('[data-test="completed-fold-row"]');
    expect(rows).toHaveLength(1);
    expect(rows[0].text()).toContain("Feishu");
  });
});

describe("TaskGroupedList — multi-select", () => {
  beforeEach(() => {
    resetSel();
    resetPins();
    vi.restoreAllMocks();
  });

  function mkSessions(): RemoteSession[] {
    // Use whatever your existing helpers produce; if none, inline:
    return [
      { session_id: "s1", host_id: "h1", host: "h1", user: "u", title: "one", cols: 80, rows: 24, cwd: "/a" },
      { session_id: "s2", host_id: "h1", host: "h1", user: "u", title: "two", cols: 80, rows: 24, cwd: "/b" },
      { session_id: "s3", host_id: "h1", host: "h1", user: "u", title: "three", cols: 80, rows: 24, cwd: "/c" },
    ] as RemoteSession[];
  }

  function mount3() {
    return mountList({
      byHost: { h1: mkSessions() },
      primaryStateForHost: () => "idle",
      completedSeen: [],
      openSessionIds: ["s1"],
    });
  }

  test("Cmd+click on a row toggles selection, does not emit open", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[1].trigger("click", { metaKey: true });
    expect(w.emitted("open")).toBeUndefined();
    expect(rows[1].attributes("data-selected")).toBe("true");
    // Toggle off
    await rows[1].trigger("click", { metaKey: true });
    expect(w.findAll("[data-test=task-row][data-selected=true]").length).toBe(0);
  });

  test("Ctrl+click on a row toggles selection (Windows/Linux)", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[2].trigger("click", { ctrlKey: true });
    expect(rows[2].attributes("data-selected")).toBe("true");
  });

  test("plain click clears selection and emits open", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true });
    await rows[1].trigger("click", { metaKey: true });
    await rows[2].trigger("click"); // plain click
    expect(w.emitted("open")).toHaveLength(1);
    expect(w.findAll("[data-test=task-row][data-selected=true]").length).toBe(0);
  });

  test("Shift+click extends selection from anchor to target in visible order", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true }); // anchor s1
    await rows[2].trigger("click", { shiftKey: true }); // range s1..s3
    expect(rows[0].attributes("data-selected")).toBe("true");
    expect(rows[1].attributes("data-selected")).toBe("true");
    expect(rows[2].attributes("data-selected")).toBe("true");
  });

  test("Shift+range skips hidden pinned rows when the pinned group is collapsed", async () => {
    // s1 is pinned; collapse the pinned group so it's hidden; then anchor
    // on a visible row and Shift+click another — s1 must NOT be swept in.
    resetPins();
    platform.sessions.getPins = vi.fn().mockResolvedValue(["s1"]);
    platform.sessions.setPins = vi.fn().mockResolvedValue(undefined);

    const w = mount3();
    await flushPromises();
    await nextTick();

    // Collapse the pinned group header. Its header has data-test="pinned-group-header".
    await w.find("[data-test=pinned-group-header]").trigger("click");

    const rows = w.findAll("[data-test=task-row]");
    // With pinned collapsed, only s2 & s3 should render as rows.
    expect(rows.length).toBe(2);
    await rows[0].trigger("click", { metaKey: true }); // anchor s2
    await rows[1].trigger("click", { shiftKey: true }); // range s2..s3
    expect(rows[0].attributes("data-selected")).toBe("true");
    expect(rows[1].attributes("data-selected")).toBe("true");
    // s1 is pinned+collapsed → not rendered, not in orderedVisibleIds, not selected
    const sel = useSessionSelection();
    expect(sel.isSelected("s1")).toBe(false);
  });

  test("contextmenu with sel.size>=2 shows merge/close/details items", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true });
    await rows[1].trigger("click", { metaKey: true });
    await rows[0].trigger("contextmenu");
    expect(w.find("[data-test=session-row-menu-item-merge]").exists()).toBe(true);
    expect(w.find("[data-test=session-row-menu-item-close]").exists()).toBe(true);
    expect(
      (w.find("[data-test=session-row-menu-item-details]").element as HTMLButtonElement).disabled,
    ).toBe(true);
  });

  test("contextmenu with sel.size<=1 shows details/pin items only", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("contextmenu");
    expect(w.find("[data-test=session-row-menu-item-details]").exists()).toBe(true);
    expect(w.find("[data-test=session-row-menu-item-pin]").exists()).toBe(true);
    expect(w.find("[data-test=session-row-menu-item-merge]").exists()).toBe(false);
    expect(w.findAll("[data-test=session-row-menu] .menu-item").map((b) => b.attributes("data-test"))).toEqual([
      "session-row-menu-item-pin",
      "session-row-menu-item-details",
    ]);
  });

  test("contextmenu on unselected row preserves the current selection", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true }); // select s1
    await rows[2].trigger("contextmenu"); // right-click s3 (not selected)
    // Menu should target s3 without changing the existing selected set.
    expect(w.find("[data-test=session-row-menu-item-details]").exists()).toBe(true);
    expect(w.find("[data-test=session-row-menu-item-merge]").exists()).toBe(false);
    // s1 remains selected; s3 is only the menu target, not newly selected.
    expect(w.findAll("[data-test=task-row][data-selected=true]").length).toBe(1);
    expect(rows[0].attributes("data-selected")).toBe("true");
    expect(rows[2].attributes("data-selected")).toBeUndefined();
  });

  test("selecting merge from menu emits merge-selected", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true });
    await rows[1].trigger("click", { metaKey: true });
    await rows[0].trigger("contextmenu");
    await w.find("[data-test=session-row-menu-item-merge]").trigger("click");
    expect(w.emitted("merge-selected")).toHaveLength(1);
  });

  test("selecting close-selected from menu emits close-selected", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[0].trigger("click", { metaKey: true }); // s1 is open
    await rows[1].trigger("click", { metaKey: true }); // s2 not open
    await rows[0].trigger("contextmenu");
    await w.find("[data-test=session-row-menu-item-close]").trigger("click");
    expect(w.emitted("close-selected")).toHaveLength(1);
  });

  test("selecting details from menu opens SessionDetailsPopover with the right-clicked session", async () => {
    const w = mount3();
    const rows = w.findAll("[data-test=task-row]");
    await rows[1].trigger("contextmenu");
    await w.find("[data-test=session-row-menu-item-details]").trigger("click");
    const popover = w.find("[data-test=session-details-popover]");
    expect(popover.exists()).toBe(true);
    expect(popover.find("[data-test=details-field-sessionId] .value").text()).toBe("s2");
  });
});

describe("TaskGroupedList row close affordance", () => {
  // The active row is the one being read, not the one being managed: a × parked
  // on it is noise, and it is the easiest to hit by accident.
  test("the active row does not reveal its close button", () => {
    expect(source).not.toMatch(/\.task-row\.active :deep\(\.row-close\)/);
  });

  test("hover and keyboard focus both reveal it", () => {
    expect(source).toMatch(/\.task-row:hover :deep\(\.row-close\)/);
    expect(source).toMatch(/\.task-row:focus-within :deep\(\.row-close\)/);
  });

  // Whatever reveals ×, the kind badge it overlays must yield in the same
  // breath — otherwise the two render on top of each other.
  test("the kind badge yields exactly when the close button appears", () => {
    // Compare only the interaction states (:hover / :focus-within); the bare
    // rules are the resting opacity and the touch fallback, which are not
    // reveals and are asserted separately.
    const states = (target: string) =>
      (source.match(new RegExp(`\\.task-row(:[a-z-]+) :deep\\(\\.${target}\\)`, "g")) ?? [])
        .map((x) => x.slice(".task-row".length).split(" ")[0])
        .sort();
    expect(states("kind")).toEqual([":focus-within", ":hover"]);
    expect(states("kind")).toEqual(states("row-close"));
  });
});

describe("TaskGroupedList drag source", () => {
  test("rows are draggable and carry the session id under our private MIME", async () => {
    const sess = mk({ session_id: "s1", host: "mac", title: "claude" });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [sess] },
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    const row = w.get('[data-test="task-row"]');
    expect(row.attributes("draggable")).toBe("true");

    const setData = vi.fn();
    const dataTransfer = { setData, effectAllowed: "none" };
    await row.trigger("dragstart", { dataTransfer });
    // text/plain would be typed into xterm's hidden textarea by any drop we
    // fail to intercept; the private type is inert there.
    expect(setData).toHaveBeenCalledWith("application/x-atterm-session", "s1");
    expect(setData).not.toHaveBeenCalledWith("text/plain", expect.anything());
    expect(dataTransfer.effectAllowed).toBe("move");
  });
});

describe("TaskGroupedList detach menu item", () => {
  function mountWith(canDetach: (id: string) => boolean) {
    return mount(TaskGroupedList, {
      props: {
        byHost: { h: [mk({ session_id: "s1", host: "mac", title: "claude" })] },
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
        canDetachSession: canDetach,
      },
    });
  }

  test("offers 'move to its own tab' only when the session shares a tab", async () => {
    const w = mountWith(() => true);
    await w.get('[data-test="task-row"]').trigger("contextmenu");
    expect(w.find('[data-test="session-row-menu-item-detach"]').exists()).toBe(true);
  });

  // A session that already owns its tab (or is not open) has nothing to detach
  // from; showing the item would make it a dead entry.
  test("hides it when there is nothing to detach from", async () => {
    const w = mountWith(() => false);
    await w.get('[data-test="task-row"]').trigger("contextmenu");
    expect(w.find('[data-test="session-row-menu-item-detach"]').exists()).toBe(false);
  });

  test("puts details above the detach item", async () => {
    const w = mountWith(() => true);
    await w.get('[data-test="task-row"]').trigger("contextmenu");
    const keys = w.findAll("[data-test^='session-row-menu-item-']")
      .map((n) => n.attributes("data-test")!.replace("session-row-menu-item-", ""));
    expect(keys.indexOf("details")).toBeLessThan(keys.indexOf("detach"));
  });

  test("emits detach-session with the right-clicked session", async () => {
    const w = mountWith(() => true);
    await w.get('[data-test="task-row"]').trigger("contextmenu");
    await w.get('[data-test="session-row-menu-item-detach"]').trigger("click");
    expect(w.emitted("detach-session")).toEqual([["s1"]]);
  });
});
