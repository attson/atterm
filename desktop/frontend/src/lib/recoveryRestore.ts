import type { RecoveryPaneSnapshot, NewSessionReq } from "./api";
import type { SessionInfo } from "./connection";

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
  };
}

// synthSessionInfoFromSnapshot stamps the recovered remote pane's tab label
// (title / cwd / host) while we wait for the relay's session list push to
// resolve the real SessionInfo for snap.session_id. Without this the tab
// reads "(空)" between mount and the first relay poll — and stays empty
// forever if the remote session has gone away. lastSeenInfo is the same
// hook the local sweep uses for the disconnected-display path, so the
// remote-disconnected case lands in the same place visually.
export function synthSessionInfoFromSnapshot(
  pane: RecoveryPaneSnapshot,
): SessionInfo {
  return {
    id: pane.session_id ?? "",
    command: pane.shell ?? "",
    cwd: pane.last_cwd ?? "",
    title: pane.title ?? "",
    cols: 0,
    rows: 0,
    started_at: 0,
    host_id: pane.host_id ?? "",
    type: pane.session_type ?? "",
    current_command: pane.last_command_line ?? "",
  };
}
