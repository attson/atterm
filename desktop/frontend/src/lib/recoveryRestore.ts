import type { RecoveryPaneSnapshot, NewSessionReq } from "./api";

// buildRestoreSessionReq turns a snapshot pane into the NewSessionReq we'll
// send to the Go side. If the snapshot didn't record a shell (the pane's
// SessionInfo wasn't resolved yet when the snapshot was saved), fall back to
// the caller-supplied default shell — NOT a hardcoded /bin/sh, which would
// drop the user into a bare sh-3.2$ prompt. /bin/sh is only the last resort
// when no default shell could be resolved at all.
//
// AI panes carry ai_kind + initial_ai_session_id; the Go side injects the
// resume command (e.g. `claude --resume <id>`) on the restored shell's first
// prompt — see relay_host SetOnFirstPrompt. There is no frontend resume path.
// initial_ai_command_line carries the original launch line so Go can preserve
// claude's flags (e.g. --permission-mode) onto the resume command.
export function buildRestoreSessionReq(
  pane: RecoveryPaneSnapshot,
  cols: number,
  rows: number,
  defaultShell: string,
): NewSessionReq {
  return {
    command: pane.shell || defaultShell || "/bin/sh",
    args: pane.shell_args ?? [],
    cwd: pane.last_cwd ?? "",
    cols,
    rows,
    ai_kind: (pane.ai?.kind ?? "") as NewSessionReq["ai_kind"],
    initial_ai_session_id: pane.ai?.session_id ?? "",
    initial_ai_command_line: pane.last_command_line ?? "",
  };
}
