import { describe, it, expect, vi } from "vitest";
import {
  computeResumeLine,
  buildRestoreSessionReq,
  awaitFirstPromptReady,
} from "./recoveryRestore";
import type { RecoveryAIInfo, RecoveryPaneSnapshot } from "./api";

describe("computeResumeLine", () => {
  it("claude with sid", () => {
    expect(
      computeResumeLine({ kind: "claude", session_id: "abc" } as RecoveryAIInfo, ""),
    ).toBe("claude --resume abc\n");
  });
  it("codex with sid", () => {
    expect(
      computeResumeLine({ kind: "codex", session_id: "xyz" } as RecoveryAIInfo, ""),
    ).toBe("codex resume xyz\n");
  });
  it("aider sends last_command_line", () => {
    expect(
      computeResumeLine({ kind: "aider" } as RecoveryAIInfo, "aider --model gpt-4"),
    ).toBe("aider --model gpt-4\n");
  });
  it("returns null when sid missing for claude", () => {
    expect(computeResumeLine({ kind: "claude" } as RecoveryAIInfo, "")).toBeNull();
  });
  it("returns null when codex sid missing", () => {
    expect(computeResumeLine({ kind: "codex" } as RecoveryAIInfo, "")).toBeNull();
  });
  it("returns null when aider has no last command", () => {
    expect(computeResumeLine({ kind: "aider" } as RecoveryAIInfo, "")).toBeNull();
  });
  it("returns null when ai is undefined", () => {
    expect(computeResumeLine(undefined, "anything")).toBeNull();
  });
});

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

describe("awaitFirstPromptReady", () => {
  it("resolves ready when waiting_input is observed", async () => {
    vi.useFakeTimers();
    let state: string | undefined = "running";
    const promise = awaitFirstPromptReady(() => state, 1000, 50);
    setTimeout(() => { state = "waiting_input"; }, 100);
    await vi.advanceTimersByTimeAsync(160);
    const result = await promise;
    expect(result).toBe("ready");
    vi.useRealTimers();
  });

  it("resolves timeout when never ready", async () => {
    vi.useFakeTimers();
    const promise = awaitFirstPromptReady(() => "running", 200, 50);
    await vi.advanceTimersByTimeAsync(260);
    const result = await promise;
    expect(result).toBe("timeout");
    vi.useRealTimers();
  });
});
