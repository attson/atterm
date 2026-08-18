import type { DirEntry, FileContent, FileMetaInfo } from "../../platform/types";
import type { DirListing, FileSystemBridge } from "./fsBridge";
import {
  sftpCreateFile,
  sftpDisconnect,
  sftpFileMeta,
  sftpListDir,
  sftpMkdir,
  sftpReadFile,
  sftpRemove,
  sftpRename,
  sftpWriteFile,
} from "../../lib/api";
import { errText, logWarn } from "../../lib/log";
import { t } from "../../i18n";

const DEFAULT_READ_MAX_BYTES = 2 * 1024 * 1024;

/** The path a newly-selected SSH host opens at. There is no cheap way to ask
 *  an SFTP server for the login directory through this executor, and guessing
 *  "/home/<user>" would be wrong on macOS, on BSD, and for root. Root is the
 *  one answer that is never a lie. */
export const SSH_HOST_ROOT = "/";

/** ALREADY_EXISTS is the sentinel internal/sftpfs returns when an upload would
 *  land on an occupied path. It is matched rather than shown: the raw string is
 *  a protocol token, and the user needs a sentence. */
const ALREADY_EXISTS = "already_exists";

/** The local tree answers a failed trash by re-prompting with a hard delete,
 *  keyed on this exact sentence (FileTree.resolveDeleteConfirm). Reusing it is
 *  what makes "Move to Trash" on a remote host degrade into "Delete
 *  permanently?" instead of a silent no-op — there is no trash on the far side
 *  and the user has to be told before the file goes. */
const NO_TRASH = "no platform trash command available";

export interface SSHHostFileSystemBridge extends FileSystemBridge {
  listDirDetailed(path: string): Promise<DirListing>;
  dispose(): void;
}

/** unsupported rejects an operation this source cannot perform, in a sentence
 *  rather than a stack trace. Browsing over SFTP is not a superset of the local
 *  filesystem: there is no change notification without polling, no OS to hand a
 *  file to, and no same-origin URL to hang a preview on. */
function unsupported(what: string): Promise<never> {
  return Promise.reject(new Error(t("plugins.fileExplorer.sshUnsupportedOp", { op: what })));
}

/** describeWriteError turns the executor's refusal into something a user can
 *  act on. The default refusal is the one that matters: it is not an error in
 *  the usual sense but a deliberate stop, and phrasing it as "already_exists"
 *  would read as a bug rather than as the safety it is. */
export function describeWriteError(path: string, err: unknown): Error {
  const message = errText(err);
  if (message.includes(ALREADY_EXISTS)) {
    return new Error(t("plugins.fileExplorer.sshUploadExists", { path }));
  }
  return new Error(message);
}

/**
 * createSSHHostFS is the file explorer's third data source: a saved SSH host,
 * browsed over SFTP on the connection that host already has.
 *
 * It is desktop-only by construction — every call below goes through the Wails
 * binding surface — which is also why it needs no session: a host is browsable
 * because it is saved, not because a terminal happens to be open on it.
 */
export function createSSHHostFS(hostID: string): SSHHostFileSystemBridge {
  let disposed = false;

  function ensureLive(): void {
    if (disposed) throw new Error(t("plugins.fileExplorer.sshBridgeDisposed"));
  }

  async function listDirDetailed(path: string): Promise<DirListing> {
    ensureLive();
    const listing = await sftpListDir(hostID, path);
    return {
      entries: (listing.entries ?? []) as DirEntry[],
      truncated: !!listing.truncated,
      total: listing.total ?? (listing.entries ?? []).length,
    };
  }

  async function listDir(path: string): Promise<DirEntry[]> {
    return (await listDirDetailed(path)).entries;
  }

  async function fileMeta(path: string): Promise<FileMetaInfo> {
    ensureLive();
    return (await sftpFileMeta(hostID, path)) as FileMetaInfo;
  }

  async function readFile(path: string, maxBytes = DEFAULT_READ_MAX_BYTES): Promise<FileContent> {
    ensureLive();
    return (await sftpReadFile(hostID, path, maxBytes)) as unknown as FileContent;
  }

  async function writeFile(
    path: string,
    data: Uint8Array,
    expectedModTime: number | null,
  ): Promise<FileMetaInfo> {
    ensureLive();
    try {
      return (await sftpWriteFile(
        hostID,
        path,
        data,
        expectedModTime ?? 0,
        expectedModTime === null,
      )) as FileMetaInfo;
    } catch (err) {
      throw describeWriteError(path, err);
    }
  }

  async function createFile(path: string): Promise<FileMetaInfo> {
    ensureLive();
    try {
      return (await sftpCreateFile(hostID, path)) as FileMetaInfo;
    } catch (err) {
      throw describeWriteError(path, err);
    }
  }

  async function mkdir(path: string): Promise<FileMetaInfo> {
    ensureLive();
    try {
      return (await sftpMkdir(hostID, path)) as FileMetaInfo;
    } catch (err) {
      throw describeWriteError(path, err);
    }
  }

  async function rename(from: string, to: string): Promise<FileMetaInfo> {
    ensureLive();
    try {
      return (await sftpRename(hostID, from, to)) as FileMetaInfo;
    } catch (err) {
      throw describeWriteError(to, err);
    }
  }

  async function remove(path: string, recursive: boolean): Promise<void> {
    ensureLive();
    await sftpRemove(hostID, path, recursive);
  }

  function dispose(): void {
    if (disposed) return;
    disposed = true;
    // Give the host's SSH connection back. Skipping this would hold a login
    // (and its keepalive) open on the remote for the rest of the app's life,
    // for a panel the user has already navigated away from.
    //
    // Nothing here may throw. dispose runs from onBeforeUnmount and from the
    // source switch; an exception there aborts the teardown of everything
    // after it, and the binding call can fail synchronously (a window already
    // being torn down has no bindings left).
    try {
      void sftpDisconnect(hostID).catch((err) => {
        logWarn("file-explorer", "releasing the ssh file source failed", {
          hostID,
          error: errText(err),
        });
      });
    } catch (err) {
      logWarn("file-explorer", "releasing the ssh file source failed", {
        hostID,
        error: errText(err),
      });
    }
  }

  return {
    identity: `ssh:${hostID}`,
    listDir,
    listDirDetailed,
    fileMeta,
    readFile,
    writeFile,
    createFile,
    mkdir,
    rename,
    remove,
    // SFTP has no change notification, and polling a remote directory tree over
    // an SSH round trip is a cost the user did not ask for. The tree simply
    // does not auto-refresh on this source.
    watchDir: () => Promise.resolve(`ssh:${hostID}`),
    unwatchDir: () => Promise.resolve(),
    onDirChanged: () => () => {},
    openExternal: () => unsupported("openExternal"),
    assetUrlFor: () => unsupported("assetUrlFor"),
    // Deliberately the local tree's sentinel: it makes "Move to Trash" fall
    // through to the permanent-delete confirmation instead of failing silently.
    trash: () => Promise.reject(new Error(NO_TRASH)),
    dispose,
  };
}
