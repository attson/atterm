import { describe, expect, it, vi, beforeEach } from "vitest";
import { ref } from "vue";

import { useRecoveryRestore } from "./useRecoveryRestore";
import * as api from "../lib/api";
import type { SessionInfo } from "../lib/connection";
import type { RecoveryTabSnapshot } from "../lib/api";
import type { Tab } from "../lib/types";
import type { UseSessionPins } from "./useSessionPins";

vi.mock("../lib/api", () => ({
  discardRecoverySnapshot: vi.fn().mockResolvedValue(undefined),
  listShells: vi.fn(),
  newSession: vi.fn(),
  newSshSessionByID: vi.fn(),
}));

describe("useRecoveryRestore", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(api.listShells).mockResolvedValue(["/bin/zsh"]);
    vi.mocked(api.newSession).mockResolvedValue({ session_id: "new-local" });
    vi.mocked(api.newSshSessionByID).mockResolvedValue({ session_id: "new-ssh" } as any);
  });

  function makePins(overrides: Partial<UseSessionPins> = {}): UseSessionPins {
    return {
      pinnedIds: ref(new Set()),
      isPinned: () => false,
      pin: vi.fn(),
      unpin: vi.fn(),
      toggle: vi.fn(),
      rename: vi.fn(),
      ready: vi.fn(async () => {}),
      reload: vi.fn(async () => {}),
      flushNow: vi.fn(async () => {}),
      ...overrides,
    };
  }

  it("pauses recovery snapshot persistence while executing restore and resumes after the final flush", async () => {
    const marks: string[] = [];
    const tabs = ref<Tab[]>([]);
    const localList = ref<SessionInfo[]>([]);
    const pauseRecoverySnapshot = vi.fn(() => {
      marks.push("pause");
      return () => marks.push("resume");
    });
    vi.mocked(api.listShells).mockImplementation(async () => {
      marks.push("listShells");
      return ["/bin/zsh"];
    });

    const pins = makePins({
      ready: vi.fn(async () => { marks.push("pins.ready"); }),
      flushNow: vi.fn(async () => { marks.push("pins.flushNow"); }),
    });

    const restore = useRecoveryRestore({
      tabs,
      localList,
      localHostID: ref("local-host"),
      pendingLocalIds: new Set(),
      pins,
      hasLocalPty: true,
      newId: () => "new-tab",
      gotoTab: (id) => { marks.push(`goto:${id}`); },
      startNewTab: vi.fn(),
      predictCellDims: () => ({ cols: 80, rows: 24 }),
      pauseRecoverySnapshot,
    });

    restore.recoveryDialogState.value = {
      open: true,
      snapshot: { version: 1, host_id: "", clean_shutdown: false, saved_at_unix: 0, active_tab_id: "saved", tabs: [] },
    };
    const picks: RecoveryTabSnapshot[] = [{
      id: "saved",
      layout: "single",
      active_pane_idx: 0,
      col_ratio: 0.5,
      row_ratio: 0.5,
      panes: [{ slot: 0, remote: true, session_id: "remote-1", host_id: "remote-host", shell: "" }],
    }];

    await restore.onRecoveryRestore(picks);

    expect(pauseRecoverySnapshot).toHaveBeenCalledTimes(1);
    expect(marks).toEqual([
      "pause",
      "pins.ready",
      "listShells",
      "goto:new-tab",
      "pins.flushNow",
      "resume",
    ]);
  });

  it("starts restore panes concurrently and commits tabs/localList once after all panes finish", async () => {
    const tabs = ref<Tab[]>([]);
    const localList = ref<SessionInfo[]>([]);
    const pendingLocalIds = new Set<string>();
    const gotoTab = vi.fn();
    const pins = makePins();
    let nextTab = 1;
    const resolves: Array<(id: string) => void> = [];
    vi.mocked(api.newSession).mockImplementation(
      () =>
        new Promise((resolve) => {
          resolves.push((id) => resolve({ session_id: id }));
        }),
    );

    const restore = useRecoveryRestore({
      tabs,
      localList,
      localHostID: ref("local-host"),
      pendingLocalIds,
      pins,
      hasLocalPty: true,
      newId: () => `tab-${nextTab++}`,
      gotoTab,
      startNewTab: vi.fn(),
      predictCellDims: () => ({ cols: 80, rows: 24 }),
    });

    restore.recoveryDialogState.value = {
      open: true,
      snapshot: { version: 1, host_id: "", clean_shutdown: false, saved_at_unix: 0, active_tab_id: "saved-2", tabs: [] },
    };
    const picks: RecoveryTabSnapshot[] = [
      {
        id: "saved-1",
        layout: "single",
        active_pane_idx: 0,
        col_ratio: 0.5,
        row_ratio: 0.5,
        panes: [{ slot: 0, remote: false, session_id: "old-1", shell: "/bin/zsh", last_cwd: "/one" }],
      },
      {
        id: "saved-2",
        layout: "single",
        active_pane_idx: 0,
        col_ratio: 0.5,
        row_ratio: 0.5,
        panes: [{ slot: 0, remote: false, session_id: "old-2", shell: "/bin/zsh", last_cwd: "/two" }],
      },
    ];

    const done = restore.onRecoveryRestore(picks);
    await new Promise<void>((resolve) => setTimeout(resolve, 0));

    expect(api.newSession).toHaveBeenCalledTimes(2);
    expect(tabs.value).toHaveLength(0);
    expect(localList.value).toHaveLength(0);

    resolves[0]("new-1");
    await Promise.resolve();
    expect(tabs.value).toHaveLength(0);
    expect(localList.value).toHaveLength(0);

    resolves[1]("new-2");
    await done;

    expect(tabs.value.map((t) => t.id)).toEqual(["tab-1", "tab-2"]);
    expect(tabs.value.map((t) => t.panes[0].sessionId)).toEqual(["new-1", "new-2"]);
    expect(localList.value.map((s) => s.id)).toEqual(["new-1", "new-2"]);
    expect([...pendingLocalIds].sort()).toEqual(["new-1", "new-2"]);
    expect(gotoTab).toHaveBeenCalledWith("tab-2");
    expect(pins.flushNow).toHaveBeenCalledOnce();
  });
});
