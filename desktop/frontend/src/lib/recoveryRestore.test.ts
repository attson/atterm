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
    const req = buildRestoreSessionReq(pane, 80, 24);
    expect(req.command).toBe("/bin/zsh");
    expect(req.args).toEqual(["-l"]);
    expect(req.cwd).toBe("/x");
    expect(req.cols).toBe(80);
    expect(req.rows).toBe(24);
    expect(req.ai_kind).toBe("claude");
    expect(req.initial_ai_session_id).toBe("sid-1");
  });

  it("defaults missing fields", () => {
    const pane: RecoveryPaneSnapshot = { slot: 0, shell: "" };
    const req = buildRestoreSessionReq(pane, 100, 30);
    expect(req.command).toBe("/bin/sh");
    expect(req.args).toEqual([]);
    expect(req.cwd).toBe("");
    expect(req.ai_kind).toBe("");
    expect(req.initial_ai_session_id).toBe("");
  });
});
