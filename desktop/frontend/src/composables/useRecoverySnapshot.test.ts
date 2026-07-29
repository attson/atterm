import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { ref, nextTick, effectScope } from "vue";
import { useRecoverySnapshot } from "./useRecoverySnapshot";
import * as api from "../lib/api";
import type { Tab } from "../lib/types";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform, fakeEventBus } from "../platform/__tests__/_fakePlatform";

describe("useRecoverySnapshot", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.spyOn(api, "saveRecoverySnapshot").mockResolvedValue(undefined);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    __setPlatformForTests(null);
  });

  it("mounts via platform.events.on with no onEvent override and no Wails runtime stub", async () => {
    // Regression: the composable used to default onEvent to a raw Wails
    // EventsOn call, which crashes on the web target because
    // window.runtime doesn't exist there. Omitting onEvent here (as App.vue
    // does in production) and never touching window.runtime proves the
    // default path now goes through platform.events.on instead, which has a
    // real implementation on every platform including web.
    const events = fakeEventBus();
    __setPlatformForTests({ ...createFakePlatform(), events });

    const tabs = ref<Tab[]>([
      {
        id: "t1",
        layout: "single",
        panes: [{ sessionId: "s1", remote: false }],
        activePaneIdx: 0,
        colRatio: 0.5,
        rowRatio: 0.5,
      },
    ]);
    const currentTabId = ref<string | null>("t1");
    const sessionInfoFor = vi.fn().mockImplementation((sid: string) => ({
      id: sid,
      command: "claude",
      cwd: "/x",
      title: "t",
      type: "ai",
      cols: 80,
      rows: 24,
      started_at: 0,
      host_id: "h",
    }));

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref("") });
    });

    // The subscription must have gone through the fake platform's event bus.
    events.emit("recovery:ai-sid", { session_id: "s1", kind: "claude", ai_session_id: "abc-uuid-xyz" });

    vi.advanceTimersByTime(600);
    await Promise.resolve();

    const calls = vi.mocked(api.saveRecoverySnapshot).mock.calls;
    expect(calls.length).toBeGreaterThan(0);
    const last = calls[calls.length - 1][0];
    expect(last.tabs[0].panes[0].ai?.session_id).toBe("abc-uuid-xyz");
    scope.stop();
  });

  it("does not persist stale AI metadata after the session type downgrades to shell", async () => {
    const events = fakeEventBus();
    __setPlatformForTests({ ...createFakePlatform(), events });

    const info = ref<any>({
      id: "s1",
      command: "zsh",
      cwd: "/x",
      title: "百度是什么",
      type: "ai",
      current_command: "codex",
      cols: 80,
      rows: 24,
      started_at: 0,
      host_id: "h",
    });
    const tabs = ref<Tab[]>([
      {
        id: "t1",
        layout: "single",
        panes: [{ sessionId: "s1", remote: false }],
        activePaneIdx: 0,
        colRatio: 0.5,
        rowRatio: 0.5,
      },
    ]);
    const currentTabId = ref<string | null>("t1");
    const sessionInfoFor = (sid: string) => (sid === "s1" ? info.value : undefined);

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref("") });
    });

    events.emit("recovery:ai-sid", { session_id: "s1", kind: "codex", ai_session_id: "019faea7-292e-7ad3-a408-4faf2bb8a848" });
    vi.advanceTimersByTime(600);
    await Promise.resolve();
    let calls = vi.mocked(api.saveRecoverySnapshot).mock.calls;
    expect(calls[calls.length - 1][0].tabs[0].panes[0].ai?.session_id).toBe("019faea7-292e-7ad3-a408-4faf2bb8a848");

    info.value = { ...info.value, type: "shell", current_command: "ls -G" };
    await nextTick();
    vi.advanceTimersByTime(600);
    await Promise.resolve();

    calls = vi.mocked(api.saveRecoverySnapshot).mock.calls;
    const pane = calls[calls.length - 1][0].tabs[0].panes[0];
    expect(pane.session_type).toBe("shell");
    expect(pane.ai).toBeUndefined();
    scope.stop();
  });

  it("debounces structural changes around 500ms", async () => {
    const tabs = ref<Tab[]>([]);
    const currentTabId = ref<string | null>(null);
    const sessionInfoFor = vi.fn().mockReturnValue(undefined);

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({
        tabs,
        currentTabId,
        sessionInfoFor,
        localHostID: ref(""),
        onEvent: () => () => {},
      });
    });

    tabs.value.push({
      id: "t1",
      layout: "single",
      panes: [{ sessionId: "s1", remote: false }],
      activePaneIdx: 0,
      colRatio: 0.5,
      rowRatio: 0.5,
    });
    await nextTick();
    vi.advanceTimersByTime(490);
    expect(api.saveRecoverySnapshot).not.toHaveBeenCalled();
    vi.advanceTimersByTime(20);
    await Promise.resolve();
    expect(api.saveRecoverySnapshot).toHaveBeenCalledTimes(1);
    scope.stop();
  });

  it("integrates recovery:ai-sid event into pane.ai", async () => {
    const tabs = ref<Tab[]>([
      {
        id: "t1",
        layout: "single",
        panes: [{ sessionId: "s1", remote: false }],
        activePaneIdx: 0,
        colRatio: 0.5,
        rowRatio: 0.5,
      },
    ]);
    const currentTabId = ref<string | null>("t1");
    const sessionInfoFor = vi.fn().mockImplementation((sid: string) => ({
      id: sid,
      command: "claude",
      cwd: "/x",
      title: "t",
      type: "ai",
      cols: 80,
      rows: 24,
      started_at: 0,
      host_id: "h",
    }));
    const handlers = new Map<string, (payload: any) => void>();
    const onEvent = (name: string, cb: (payload: any) => void) => {
      handlers.set(name, cb);
      return () => handlers.delete(name);
    };

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent });
    });

    // Flush the initial save triggered by the immediate ref setup.
    vi.advanceTimersByTime(600);
    await Promise.resolve();
    const before = (api.saveRecoverySnapshot as any).mock.calls.length;

    const handler = handlers.get("recovery:ai-sid")!;
    expect(handler).toBeDefined();
    handler({ session_id: "s1", kind: "claude", ai_session_id: "abc-uuid-xyz" });

    vi.advanceTimersByTime(600);
    await Promise.resolve();
    const after = (api.saveRecoverySnapshot as any).mock.calls.length;
    expect(after).toBeGreaterThan(before);
    const lastCall = (api.saveRecoverySnapshot as any).mock.calls[after - 1][0];
    expect(lastCall.tabs[0].panes[0].ai?.session_id).toBe("abc-uuid-xyz");
    expect(lastCall.tabs[0].panes[0].ai?.kind).toBe("claude");
    scope.stop();
  });

  it("persists a pane meta change (type shell→ai) with no structural change", async () => {
    const info = ref<any>({
      id: "s1", command: "zsh", cwd: "/x", title: "t", type: "shell",
      cols: 80, rows: 24, started_at: 0, host_id: "h",
    });
    const tabs = ref<Tab[]>([
      {
        id: "t1",
        layout: "single",
        panes: [{ sessionId: "s1", remote: false }],
        activePaneIdx: 0,
        colRatio: 0.5,
        rowRatio: 0.5,
      },
    ]);
    const currentTabId = ref<string | null>("t1");
    const sessionInfoFor = (sid: string) => (sid === "s1" ? info.value : undefined);

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent: () => () => {} });
    });

    vi.advanceTimersByTime(600);
    await Promise.resolve();
    const before = (api.saveRecoverySnapshot as any).mock.calls.length;

    // Meta-only change: shell→ai. No tab/pane structural change, sessionId
    // unchanged — only the per-pane meta watcher can catch this.
    info.value = { ...info.value, type: "ai" };
    await nextTick();
    vi.advanceTimersByTime(600);
    await Promise.resolve();

    const after = (api.saveRecoverySnapshot as any).mock.calls.length;
    expect(after).toBeGreaterThan(before);
    const lastCall = (api.saveRecoverySnapshot as any).mock.calls[after - 1][0];
    expect(lastCall.tabs[0].panes[0].session_type).toBe("ai");
    scope.stop();
  });

  it("does not trigger an immediate (500ms) save when only current_command changes", async () => {
    // Regression: current_command is the live OSC 133 command line, which an AI
    // session (Claude Code spinner/status) rewrites every second. Routing it
    // through the 500ms structural debounce made recovery fsync the snapshot to
    // disk once a second. It must instead ride the 5s heartbeat.
    const info = ref<any>({
      id: "s1", command: "claude", cwd: "/x", title: "t", type: "ai",
      current_command: "claude --foo", cols: 80, rows: 24, started_at: 0, host_id: "h",
    });
    const tabs = ref<Tab[]>([
      {
        id: "t1", layout: "single",
        panes: [{ sessionId: "s1", remote: false }],
        activePaneIdx: 0, colRatio: 0.5, rowRatio: 0.5,
      },
    ]);
    const currentTabId = ref<string | null>("t1");
    const sessionInfoFor = (sid: string) => (sid === "s1" ? info.value : undefined);

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent: () => () => {} });
    });

    // Flush the initial setup save.
    vi.advanceTimersByTime(600);
    await Promise.resolve();
    const before = (api.saveRecoverySnapshot as any).mock.calls.length;

    // Only current_command changes (a spinner tick).
    info.value = { ...info.value, current_command: "claude --bar" };
    await nextTick();
    // Within the structural window: must NOT have saved yet.
    vi.advanceTimersByTime(600);
    await Promise.resolve();
    expect((api.saveRecoverySnapshot as any).mock.calls.length).toBe(before);

    // It still rides the 5s heartbeat eventually.
    vi.advanceTimersByTime(5000);
    await Promise.resolve();
    expect((api.saveRecoverySnapshot as any).mock.calls.length).toBeGreaterThan(before);
    scope.stop();
  });

  it("still triggers an immediate (500ms) save when title changes", async () => {
    const info = ref<any>({
      id: "s1", command: "claude", cwd: "/x", title: "old", type: "ai",
      current_command: "claude", cols: 80, rows: 24, started_at: 0, host_id: "h",
    });
    const tabs = ref<Tab[]>([
      {
        id: "t1", layout: "single",
        panes: [{ sessionId: "s1", remote: false }],
        activePaneIdx: 0, colRatio: 0.5, rowRatio: 0.5,
      },
    ]);
    const currentTabId = ref<string | null>("t1");
    const sessionInfoFor = (sid: string) => (sid === "s1" ? info.value : undefined);

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent: () => () => {} });
    });
    vi.advanceTimersByTime(600);
    await Promise.resolve();
    const before = (api.saveRecoverySnapshot as any).mock.calls.length;

    info.value = { ...info.value, title: "new" };
    await nextTick();
    vi.advanceTimersByTime(510);
    await Promise.resolve();
    expect((api.saveRecoverySnapshot as any).mock.calls.length).toBeGreaterThan(before);
    scope.stop();
  });

  it("periodic safety timer does not write when idle (clean)", async () => {
    const tabs = ref<Tab[]>([]);
    const currentTabId = ref<string | null>(null);
    const sessionInfoFor = vi.fn().mockReturnValue(undefined);

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent: () => () => {} });
    });

    // No changes → nothing dirty. Several safety intervals must not spam saves.
    vi.advanceTimersByTime(35000);
    await Promise.resolve();
    expect(api.saveRecoverySnapshot).not.toHaveBeenCalled();
    scope.stop();
  });

  it("captures remote=true panes with session_id + host_id", async () => {
    // Regression for "remote session recovery becomes a local default session":
    // without these three fields the snapshot can't tell a remote pane apart
    // from a local one, so executeRestore forks a fresh local shell instead
    // of re-binding to the still-alive remote session.
    const tabs = ref<Tab[]>([]);
    const currentTabId = ref<string | null>(null);
    const sessionInfoFor = (sid: string) =>
      sid === "remote-sid-42"
        ? {
            id: sid,
            command: "zsh",
            cwd: "/home/u/proj",
            title: "proj — vim",
            cols: 80,
            rows: 24,
            started_at: 0,
            host_id: "host-B365",
          }
        : undefined;

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent: () => () => {} });
    });

    tabs.value.push({
      id: "t1",
      layout: "single",
      panes: [{ sessionId: "remote-sid-42", remote: true }],
      activePaneIdx: 0,
      colRatio: 0.5,
      rowRatio: 0.5,
    });
    await nextTick();
    vi.advanceTimersByTime(600);
    await Promise.resolve();

    const calls = (api.saveRecoverySnapshot as any).mock.calls;
    expect(calls.length).toBeGreaterThan(0);
    const last = calls[calls.length - 1][0];
    const pane = last.tabs[0].panes[0];
    expect(pane.remote).toBe(true);
    expect(pane.session_id).toBe("remote-sid-42");
    expect(pane.host_id).toBe("host-B365");
    expect(pane.last_cwd).toBe("/home/u/proj");
    expect(pane.title).toBe("proj — vim");
    scope.stop();
  });

  it("persists a remote=true viewer pane as local when its host is the local host", async () => {
    // Sidebar-opened local sessions get pane.remote=true (viewer mode), but the
    // session itself lives on this host. Persisting it as remote bakes in a
    // sessionID that dies on every dev restart; on restore executeRestore takes
    // the rebind-no-spawn branch and the pane sticks to the dead sid forever,
    // surfacing as "(空)". Snapshot must look-through info.host_id and save the
    // pane as plain-local so restore re-spawns a fresh shell at last_cwd.
    const localHostID = ref<string>("local-host");
    const tabs = ref<Tab[]>([]);
    const currentTabId = ref<string | null>(null);
    const sessionInfoFor = (sid: string) =>
      sid === "local-sid"
        ? {
            id: sid,
            command: "zsh",
            cwd: "/Users/u/proj",
            title: "proj",
            cols: 80,
            rows: 24,
            started_at: 0,
            host_id: "local-host",
          }
        : undefined;

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({
        tabs,
        currentTabId,
        sessionInfoFor,
        localHostID,
        onEvent: () => () => {},
      });
    });

    tabs.value.push({
      id: "t1",
      layout: "single",
      panes: [{ sessionId: "local-sid", remote: true }],
      activePaneIdx: 0,
      colRatio: 0.5,
      rowRatio: 0.5,
    });
    await nextTick();
    vi.advanceTimersByTime(600);
    await Promise.resolve();

    const calls = (api.saveRecoverySnapshot as any).mock.calls;
    expect(calls.length).toBeGreaterThan(0);
    const last = calls[calls.length - 1][0];
    const pane = last.tabs[0].panes[0];
    expect(pane.remote).toBeUndefined();
    expect(pane.session_id).toBeUndefined();
    expect(pane.host_id).toBeUndefined();
    expect(pane.last_cwd).toBe("/Users/u/proj");
    expect(pane.shell).toBe("zsh");
    scope.stop();
  });

  it("writes session_id for local panes (needed for pin migration) but keeps remote/host_id undefined", async () => {
    // Regression: pin state is keyed by session_id. On restart the local pane
    // gets a fresh sid from newSession(); executeRestore needs the previous
    // generation's sid to remap the pin set. See
    // docs/superpowers/specs/2026-07-23-pinned-session-recovery-design.md §4.1.
    const tabs = ref<Tab[]>([]);
    const currentTabId = ref<string | null>(null);
    const sessionInfoFor = (sid: string) =>
      sid === "local-sid"
        ? {
            id: sid,
            command: "zsh",
            cwd: "/tmp",
            title: "tmp",
            cols: 80,
            rows: 24,
            started_at: 0,
            host_id: "local-host",
          }
        : undefined;

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent: () => () => {} });
    });

    tabs.value.push({
      id: "t1",
      layout: "single",
      panes: [{ sessionId: "local-sid", remote: false }],
      activePaneIdx: 0,
      colRatio: 0.5,
      rowRatio: 0.5,
    });
    await nextTick();
    vi.advanceTimersByTime(600);
    await Promise.resolve();

    const calls = (api.saveRecoverySnapshot as any).mock.calls;
    expect(calls.length).toBeGreaterThan(0);
    const last = calls[calls.length - 1][0];
    const pane = last.tabs[0].panes[0];
    expect(pane.session_id).toBe("local-sid");
    expect(pane.remote).toBeUndefined();
    expect(pane.host_id).toBeUndefined();
    scope.stop();
  });

  it("ignores recovery:ai-sid payloads with missing fields", async () => {
    const tabs = ref<Tab[]>([
      {
        id: "t1",
        layout: "single",
        panes: [{ sessionId: "s1", remote: false }],
        activePaneIdx: 0,
        colRatio: 0.5,
        rowRatio: 0.5,
      },
    ]);
    const currentTabId = ref<string | null>("t1");
    const sessionInfoFor = vi.fn().mockReturnValue(undefined);
    const handlers = new Map<string, (payload: any) => void>();
    const onEvent = (name: string, cb: (payload: any) => void) => {
      handlers.set(name, cb);
      return () => handlers.delete(name);
    };

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent });
    });

    vi.advanceTimersByTime(600);
    await Promise.resolve();
    const before = (api.saveRecoverySnapshot as any).mock.calls.length;

    const handler = handlers.get("recovery:ai-sid")!;
    handler({ session_id: "s1", kind: "", ai_session_id: "x" }); // empty kind → skip
    handler({ session_id: "", kind: "claude", ai_session_id: "x" }); // empty sid → skip
    handler({ session_id: "s1", kind: "claude", ai_session_id: "" }); // empty aiSid → skip

    vi.advanceTimersByTime(600);
    await Promise.resolve();
    expect((api.saveRecoverySnapshot as any).mock.calls.length).toBe(before);
    scope.stop();
  });

  it("flushNow swallows a synchronous throw from saveRecoverySnapshot (bindings not ready)", async () => {
    // Regression: lib/api's saveRecoverySnapshot calls bindings() internally,
    // which THROWS synchronously (not a rejected promise) when window.go is
    // absent (the web build). App.vue now gates construction of this
    // composable on caps.wailsBindings, but flushNow must also defend itself
    // in depth — a synchronous throw here used to escape uncaught out of the
    // setTimeout callback that calls flushNow.
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.mocked(api.saveRecoverySnapshot).mockImplementation(() => {
      throw new Error("Wails 绑定尚未就绪");
    });

    const tabs = ref<Tab[]>([]);
    const currentTabId = ref<string | null>(null);
    const sessionInfoFor = vi.fn().mockReturnValue(undefined);

    const scope = effectScope();
    let composable!: ReturnType<typeof useRecoverySnapshot>;
    scope.run(() => {
      composable = useRecoverySnapshot({
        tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent: () => () => {},
      });
    });

    expect(() => composable.flushNow()).not.toThrow();
    expect(warnSpy).toHaveBeenCalled();
    scope.stop();
  });

  it("flushNow swallows an async rejection from saveRecoverySnapshot", async () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.mocked(api.saveRecoverySnapshot).mockRejectedValue(new Error("disk full"));

    const tabs = ref<Tab[]>([]);
    const currentTabId = ref<string | null>(null);
    const sessionInfoFor = vi.fn().mockReturnValue(undefined);

    const scope = effectScope();
    let composable!: ReturnType<typeof useRecoverySnapshot>;
    scope.run(() => {
      composable = useRecoverySnapshot({
        tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent: () => () => {},
      });
    });

    expect(() => composable.flushNow()).not.toThrow();
    for (let i = 0; i < 10 && warnSpy.mock.calls.length === 0; i++) {
      await Promise.resolve();
    }
    expect(warnSpy).toHaveBeenCalled();
    scope.stop();
  });
});
