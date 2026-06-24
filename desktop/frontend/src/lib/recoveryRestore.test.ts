import { describe, it, expect } from "vitest";
import { buildRestoreSessionReq } from "./recoveryRestore";
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

  it("forwards last_command_line as initial_ai_command_line so Go can preserve claude flags", () => {
    const pane: RecoveryPaneSnapshot = {
      slot: 0,
      shell: "/bin/zsh",
      session_type: "ai",
      last_command_line: "claude --permission-mode bypassPermissions",
      ai: { kind: "claude", session_id: "sid-1" },
    };
    const req = buildRestoreSessionReq(pane, 80, 24, "/bin/zsh");
    expect(req.initial_ai_command_line).toBe(
      "claude --permission-mode bypassPermissions",
    );
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
    expect(req.initial_ai_command_line).toBe("");
  });

  it("falls back to /bin/sh only when no default shell is known", () => {
    const pane: RecoveryPaneSnapshot = { slot: 0, shell: "" };
    const req = buildRestoreSessionReq(pane, 100, 30, "");
    expect(req.command).toBe("/bin/sh");
  });
});
