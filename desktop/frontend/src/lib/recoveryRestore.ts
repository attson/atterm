import type { RecoveryAIInfo, RecoveryPaneSnapshot, NewSessionReq } from "./api";

// computeResumeLine produces the exact text (including trailing newline)
// to PTY.Write into a freshly forked shell so the AI session continues.
// Returns null when no resume should be injected. Mirrors the Go-side
// computeResumeArgs but expresses the result as a single line ready to
// write to the PTY.
//
// No fallback: we only resume when we have a precise captured session id.
// Without one we do NOT re-run a recorded command (that would start a fresh
// conversation, or — for cwd-based tools — risk resuming the wrong one); the
// pane is restored as a plain shell instead.
export function computeResumeLine(
  ai: RecoveryAIInfo | undefined,
): string | null {
  if (!ai) return null;
  if (ai.kind === "claude" && ai.session_id) return `claude --resume ${ai.session_id}\n`;
  if (ai.kind === "codex" && ai.session_id) return `codex resume ${ai.session_id}\n`;
  return null;
}

// buildRestoreSessionReq turns a snapshot pane into the NewSessionReq we'll
// send to the Go side. Defaults: shell falls back to /bin/sh if the snapshot
// didn't record one (shouldn't happen, but handle it).
export function buildRestoreSessionReq(
  pane: RecoveryPaneSnapshot,
  cols: number,
  rows: number,
): NewSessionReq {
  return {
    command: pane.shell || "/bin/sh",
    args: pane.shell_args ?? [],
    cwd: pane.last_cwd ?? "",
    cols,
    rows,
    ai_kind: (pane.ai?.kind ?? "") as NewSessionReq["ai_kind"],
    initial_ai_session_id: pane.ai?.session_id ?? "",
  };
}

// awaitFirstPromptReady waits for SessionInfo.task_state to become
// `waiting_input` (post-OSC-133;A state). Returns "ready" on first
// transition, "timeout" after timeoutMs. The caller's `get` reads task_state
// out of whatever store/connection it owns; we don't bind to any specific
// data source so the helper stays testable in isolation.
export function awaitFirstPromptReady(
  get: () => string | undefined,
  timeoutMs: number = 5000,
  intervalMs: number = 80,
): Promise<"ready" | "timeout"> {
  return new Promise((resolve) => {
    const start = Date.now();
    const tick = () => {
      if (get() === "waiting_input") {
        resolve("ready");
        return;
      }
      if (Date.now() - start >= timeoutMs) {
        resolve("timeout");
        return;
      }
      setTimeout(tick, intervalMs);
    };
    tick();
  });
}
