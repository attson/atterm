import { describe, it, expect } from "vitest";
import { buildRestoreSessionReq, synthSessionInfoFromSnapshot } from "./recoveryRestore";
import type { RecoveryPaneSnapshot } from "./api";

describe("buildRestoreSessionReq", () => {
  it("forwards shell + ai_kind + initial_ai_session_id", () => {
    const pane: RecoveryPaneSnapshot = {
      slot: 0,
      shell: "/bin/zsh",
      shell_args: ["-l"],
      last_cwd: "/x",
      session_type: "ai",
      ai: { kind: "claude", session_id: "sid-1" },
    };
    const req = buildRestoreSessionReq(pane, 80, 24, "/opt/homebrew/bin/zsh");
    expect(req.command).toBe("/bin/zsh");
    expect(req.args).toEqual(["-l"]);
    expect(req.cwd).toBe("/x");
    expect(req.cols).toBe(80);
    expect(req.rows).toBe(24);
    expect(req.ai_kind).toBe("claude");
    expect(req.initial_ai_session_id).toBe("sid-1");
  });

  it("falls back to the user's default shell when the snapshot has no shell", () => {
    // Regression: a snapshot saved with an empty shell (the per-pane
    // SessionInfo wasn't resolved yet at save time) must NOT collapse to
    // /bin/sh — that lands the user in sh-3.2$ instead of their real shell.
    const pane: RecoveryPaneSnapshot = { slot: 0, shell: "" };
    const req = buildRestoreSessionReq(pane, 100, 30, "/bin/zsh");
    expect(req.command).toBe("/bin/zsh");
    expect(req.args).toEqual([]);
    expect(req.cwd).toBe("");
    expect(req.ai_kind).toBe("");
    expect(req.initial_ai_session_id).toBe("");
  });

  it("falls back to /bin/sh only when no default shell is known", () => {
    const pane: RecoveryPaneSnapshot = { slot: 0, shell: "" };
    const req = buildRestoreSessionReq(pane, 100, 30, "");
    expect(req.command).toBe("/bin/sh");
  });
});

describe("synthSessionInfoFromSnapshot", () => {
  it("stamps the remote pane's title/cwd/host so the tab label isn't '(空)' before the relay catches up", () => {
    const pane: RecoveryPaneSnapshot = {
      slot: 0,
      remote: true,
      host_id: "host-B365",
      session_id: "remote-sid",
      shell: "zsh",
      last_cwd: "/home/u/proj",
      session_type: "ai",
      title: "proj — claude",
      last_command_line: "claude --resume xyz",
    };
    const info = synthSessionInfoFromSnapshot(pane);
    expect(info.id).toBe("remote-sid");
    expect(info.title).toBe("proj — claude");
    expect(info.cwd).toBe("/home/u/proj");
    expect(info.host_id).toBe("host-B365");
    expect(info.type).toBe("ai");
    expect(info.current_command).toBe("claude --resume xyz");
  });

  it("tolerates a sparse snapshot (no optional fields set)", () => {
    const pane: RecoveryPaneSnapshot = { slot: 0, shell: "" };
    const info = synthSessionInfoFromSnapshot(pane);
    expect(info.id).toBe("");
    expect(info.title).toBe("");
    expect(info.host_id).toBe("");
  });
});
