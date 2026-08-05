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
    vi.mocked(api.listShells).mockResolvedValue(["/bin/zsh"]);
    vi.mocked(api.newSession).mockResolvedValue({ session_id: "new-local" });
    vi.mocked(api.newSshSessionByID).mockResolvedValue({ session_id: "new-ssh" } as any);
  });

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

    const pins: UseSessionPins = {
      pinnedIds: ref(new Set()),
      isPinned: () => false,
      pin: vi.fn(),
      unpin: vi.fn(),
      toggle: vi.fn(),
      rename: vi.fn(),
      ready: vi.fn(async () => { marks.push("pins.ready"); }),
      reload: vi.fn(async () => {}),
      flushNow: vi.fn(async () => { marks.push("pins.flushNow"); }),
    };

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
});
