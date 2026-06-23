import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { ref, nextTick, effectScope } from "vue";
import { useRecoverySnapshot } from "./useRecoverySnapshot";
import * as api from "../lib/api";
import type { Tab } from "../lib/types";

describe("useRecoverySnapshot", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.spyOn(api, "saveRecoverySnapshot").mockResolvedValue(undefined);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
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
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, onEvent });
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
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, onEvent: () => () => {} });
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

  it("periodic safety timer does not write when idle (clean)", async () => {
    const tabs = ref<Tab[]>([]);
    const currentTabId = ref<string | null>(null);
    const sessionInfoFor = vi.fn().mockReturnValue(undefined);

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, onEvent: () => () => {} });
    });

    // No changes → nothing dirty. Several safety intervals must not spam saves.
    vi.advanceTimersByTime(35000);
    await Promise.resolve();
    expect(api.saveRecoverySnapshot).not.toHaveBeenCalled();
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
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, onEvent });
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
});
