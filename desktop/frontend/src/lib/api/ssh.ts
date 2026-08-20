import { bindings } from "./_bindings";
import type {
  AcceptedHostKey,
  ActiveForward,
  NewSessionResp,
  SSHConfigImportPreview,
  SSHConnectReq,
  SSHCredential,
  SSHHost,
  SSHKey,
} from "./_bindings";

export type {
  AcceptedHostKey,
  ActiveForward,
  ForwardRule,
  SSHConfigImportPreview,
  SSHConfigImportSkipped,
  SSHConnectReq,
  SSHCredential,
  SSHHost,
  SSHKey,
} from "./_bindings";

// ACCEPT_NO_HOST_KEY is what a connection that has not been through a TOFU
// dialog sends: it matches nothing, so an unknown key is reported rather than
// trusted. It is the default below so a caller that knows nothing about host
// keys — background recovery restore, say — fails closed by construction
// instead of by remembering to.
export const ACCEPT_NO_HOST_KEY: AcceptedHostKey = { host: "", fingerprint: "" };

export function newSshSession(req: SSHConnectReq): Promise<NewSessionResp> {
  return bindings().NewSshSession(req);
}

// newSshSessionByID connects a saved host. accepted is the one host key the
// user confirmed in a TOFU dialog, echoed back verbatim from the rejection that
// produced it — both halves, exactly as they arrived. It travels as one object
// rather than two arguments because two adjacent strings can be passed in the
// wrong order and still typecheck, and the symptom of that would be an
// acceptance that matches nothing: the dialog asks again, forever.
export function newSshSessionByID(
  id: string,
  accepted: AcceptedHostKey = ACCEPT_NO_HOST_KEY,
): Promise<NewSessionResp> {
  return bindings().NewSshSessionByID(id, accepted);
}

export function listSSHHosts(): Promise<SSHHost[]> {
  return bindings().ListSSHHosts();
}

export function addSSHHost(h: SSHHost, cred: SSHCredential): Promise<SSHHost> {
  return bindings().AddSSHHost(h, cred);
}

export function updateSSHHost(h: SSHHost, cred: SSHCredential | null): Promise<void> {
  return bindings().UpdateSSHHost(h, cred);
}

export function deleteSSHHost(id: string): Promise<void> {
  return bindings().DeleteSSHHost(id);
}

// previewSSHConfigImport parses ~/.ssh/config on the Go side and returns
// importable entries + skipped entries (with reasons) + a coverage note,
// without writing anything. A missing/unreadable config rejects with a
// readable message rather than resolving to an empty list.
export function previewSSHConfigImport(): Promise<SSHConfigImportPreview> {
  return bindings().PreviewSSHConfigImport();
}

// importSSHHosts writes the given (user-selected) hosts into the store,
// merging by alias. Returns the count written.
export function importSSHHosts(hosts: SSHHost[]): Promise<number> {
  return bindings().ImportSSHHosts(hosts);
}

// startForward brings up one saved rule. Tunnels are only ever started
// explicitly — a tunnel occupies a local port, so nothing auto-starts one on
// connect. A ProxyJump host is fine as of roadmap item 27 — the tunnel rides
// the same chain the terminal does. Rejects for a ProxyCommand host, for an
// invalid rule, for a local port that is already taken, and for an unknown host
// key on any hop (there is no dialog behind this call, so nothing is accepted
// here — the message says to accept the fingerprints in a terminal first).
export function startForward(hostID: string, ruleID: string): Promise<void> {
  return bindings().StartForward(hostID, ruleID);
}

// stopForward closes a running tunnel, and also clears an entry that already
// stopped by itself (see ActiveForward).
export function stopForward(hostID: string, ruleID: string): Promise<void> {
  return bindings().StopForward(hostID, ruleID);
}

// listActiveForwards returns every tunnel the app is holding — running *and*
// stopped-with-an-error. Callers must branch on `running`.
export function listActiveForwards(): Promise<ActiveForward[]> {
  return bindings().ListActiveForwards();
}

export function listSSHKeys(): Promise<SSHKey[]> {
  return bindings().ListSSHKeys();
}

export function addSSHKey(name: string, privateKeyPEM: string, passphrase: string): Promise<SSHKey> {
  return bindings().AddSSHKey(name, privateKeyPEM, passphrase);
}

export function updateSSHKey(id: string, name: string, privateKeyPEM: string, passphrase: string): Promise<void> {
  return bindings().UpdateSSHKey(id, name, privateKeyPEM, passphrase);
}

export function deleteSSHKey(id: string): Promise<void> {
  return bindings().DeleteSSHKey(id);
}
