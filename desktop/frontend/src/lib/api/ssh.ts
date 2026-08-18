import { bindings } from "./_bindings";
import type {
  AcceptedHostKey,
  ActiveForward,
  NewSessionResp,
  SFTPFileContent,
  SFTPFileMetaInfo,
  SFTPListing,
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
  SFTPDirEntry,
  SFTPFileContent,
  SFTPFileMetaInfo,
  SFTPListing,
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

// --- the file explorer's SSH data source (roadmap item 28) ------------------

// listSFTPHosts is the file explorer's source list: the saved hosts atterm
// will actually dial.
//
// A ProxyCommand host is absent rather than present-and-broken. That is not a
// cosmetic filter — atterm never runs an arbitrary proxy command, so such a
// host cannot be connected at all, and offering it would produce a failure the
// user has no way to act on. The Go side derives this list from the same gate
// the browse path runs, so the two cannot disagree.
export function listSFTPHosts(): Promise<SSHHost[]> {
  return bindings().ListSFTPHosts();
}

export function sftpListDir(hostID: string, path: string): Promise<SFTPListing> {
  return bindings().SFTPListDir(hostID, path);
}

export function sftpFileMeta(hostID: string, path: string): Promise<SFTPFileMetaInfo> {
  return bindings().SFTPFileMeta(hostID, path);
}

export function sftpReadFile(hostID: string, path: string, maxBytes: number): Promise<SFTPFileContent> {
  return bindings().SFTPReadFile(hostID, path, maxBytes);
}

// sftpWriteFile uploads to a remote path. Uploading onto a path that already
// exists is refused unless expectedModTime is the ModTime the caller last saw:
// there is no trash and no versioning on the far side, so a mistaken overwrite
// cannot be undone. The refusal arrives as "already_exists".
export function sftpWriteFile(
  hostID: string,
  path: string,
  data: Uint8Array | number[],
  expectedModTime: number,
  createIfMissing: boolean,
): Promise<SFTPFileMetaInfo> {
  return bindings().SFTPWriteFile(hostID, path, Array.from(data), expectedModTime, createIfMissing);
}

export function sftpCreateFile(hostID: string, path: string): Promise<SFTPFileMetaInfo> {
  return bindings().SFTPCreateFile(hostID, path);
}

export function sftpMkdir(hostID: string, path: string): Promise<SFTPFileMetaInfo> {
  return bindings().SFTPMkdir(hostID, path);
}

export function sftpRename(hostID: string, from: string, to: string): Promise<SFTPFileMetaInfo> {
  return bindings().SFTPRename(hostID, from, to);
}

// sftpRemove is a real delete: there is no trash on the far side to fall back
// on, so the caller has to have said so before getting here.
export function sftpRemove(hostID: string, path: string, recursive: boolean): Promise<void> {
  return bindings().SFTPRemove(hostID, path, recursive);
}

// sftpDisconnect releases the browser's share of the host's SSH connection.
// Without it, having browsed a host once holds a login open on it until the
// app quits.
export function sftpDisconnect(hostID: string): Promise<void> {
  return bindings().SFTPDisconnect(hostID);
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
