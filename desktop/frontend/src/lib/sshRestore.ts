import type { RecoveryPaneSnapshot } from "./api";

export type SSHRestoreKind =
  | { kind: "reconnect"; hostId: string } // saved-host SSH → NewSshSessionByID
  | { kind: "adhoc" } // ad-hoc SSH → cannot reconnect, leave empty + hint
  | { kind: "local" }; // ordinary local shell → normal respawn

// classifySSHRestore decides how a recovered pane's session should be rebuilt.
//
// - A pane carrying ssh_host_id came from a saved host: reconnect by id.
// - A pane whose command/title starts with "ssh " (the fixed slice-1 title
//   format "ssh user@host") but has no ssh_host_id is an ad-hoc SSH session:
//   its credentials were used-once and discarded, so it cannot be reconnected.
// - Everything else is a local shell and follows the existing respawn path.
//
// The command-prefix heuristic is safe because local shells record their shell
// binary (bash/zsh/sh/...) as the command, never "ssh ".
export function classifySSHRestore(snap: RecoveryPaneSnapshot): SSHRestoreKind {
  if (snap.ssh_host_id) {
    return { kind: "reconnect", hostId: snap.ssh_host_id };
  }
  const cmd = (snap.shell || snap.title || snap.last_command_line || "").trim();
  if (cmd === "ssh" || cmd.startsWith("ssh ")) {
    return { kind: "adhoc" };
  }
  return { kind: "local" };
}
