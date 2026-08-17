import { bindings } from "./_bindings";
import type {
  NewSessionResp,
  SSHConfigImportPreview,
  SSHConnectReq,
  SSHCredential,
  SSHHost,
  SSHKey,
} from "./_bindings";

export type {
  SSHConfigImportPreview,
  SSHConfigImportSkipped,
  SSHConnectReq,
  SSHCredential,
  SSHHost,
  SSHKey,
} from "./_bindings";

export function newSshSession(req: SSHConnectReq): Promise<NewSessionResp> {
  return bindings().NewSshSession(req);
}

export function newSshSessionByID(id: string): Promise<NewSessionResp> {
  return bindings().NewSshSessionByID(id);
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
