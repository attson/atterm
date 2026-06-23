import type { RecoveryPaneSnapshot, NewSessionReq } from "./api";

// buildRestoreSessionReq turns a snapshot pane into the NewSessionReq we'll
// send to the Go side. Defaults: shell falls back to /bin/sh if the snapshot
// didn't record one (shouldn't happen, but handle it).
//
// AI panes carry ai_kind + initial_ai_session_id; the Go side injects the
// resume command (e.g. `claude --resume <id>`) on the restored shell's first
// prompt — see relay_host SetOnFirstPrompt. There is no frontend resume path.
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
