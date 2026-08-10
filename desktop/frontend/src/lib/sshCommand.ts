// Renders the ssh(1) invocation equivalent to a saved host, for the Hosts
// panel's "Copy SSH command" action. Auth is deliberately left out — the point
// is a command the user can paste into any other terminal.

export type SshCommandTarget = {
  user: string;
  host: string;
  port?: string;
};

const DEFAULT_PORT = "22";

export function sshCommandFor(target: SshCommandTarget): string {
  const user = target.user.trim();
  const host = target.host.trim();
  const port = (target.port ?? "").trim();
  const portFlag = port !== "" && port !== DEFAULT_PORT ? `-p ${port} ` : "";
  const dest = user === "" ? host : `${user}@${host}`;
  return `ssh ${portFlag}${dest}`;
}
