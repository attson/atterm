import type { EventBus, PluginHostBridge, DirEntry, FileContent, FileMetaInfo } from "../../platform/types";

// DirListing is what a source that can *cap* a listing returns. The local and
// remote-session sources never cap, so they only implement listDir; the SSH
// source does (internal/sftpfs MaxEntries = 2000), and a cap that the tree
// cannot see is a cap the user cannot see — showing 12 entries out of 3000
// with no indication is the exact failure the cap exists to prevent.
export interface DirListing {
  entries: DirEntry[];
  truncated: boolean;
  /** The real number of entries on the far side, so the panel can say
   *  "showing 2000 of 3000" rather than only that something is missing. */
  total: number;
}

export interface FileSystemBridge {
  readonly identity: string;
  listDir(path: string): Promise<DirEntry[]>;
  /** Optional richer listing for sources that cap. When present the tree
   *  prefers it, so truncation reaches the UI instead of being silently
   *  dropped on the way through listDir's plain array. */
  listDirDetailed?(path: string): Promise<DirListing>;
  watchDir(path: string): Promise<number | string>;
  unwatchDir(id: number | string): Promise<void>;
  readFile(path: string, maxBytes?: number): Promise<FileContent>;
  fileMeta(path: string): Promise<FileMetaInfo>;
  openExternal(path: string): Promise<void>;
  assetUrlFor(path: string): Promise<string>;
  onDirChanged(handler: (path: string) => void): () => void;
  revokeAssetUrl?(path: string): void;
  dispose?(): void;
  /** Writes data to path. When expectedModTime is a number, the server compares it
   *  to the on-disk modTime and rejects with a stale_modtime error on mismatch.
   *  When expectedModTime is null, the file is created if it doesn't exist. */
  writeFile(path: string, data: Uint8Array, expectedModTime: number | null): Promise<FileMetaInfo>;
  createFile(path: string): Promise<FileMetaInfo>;
  rename(from: string, to: string): Promise<FileMetaInfo>;
  remove(path: string, recursive: boolean): Promise<void>;
  mkdir(path: string): Promise<FileMetaInfo>;
  trash(path: string): Promise<void>;
}

export function createLocalFSBridge(pluginHost: PluginHostBridge, events?: EventBus): FileSystemBridge {
  return {
    identity: "local",
    listDir: (path) => pluginHost.fs.listDir(path),
    watchDir: (path) => pluginHost.fs.watchDir(path),
    unwatchDir: async (id) => {
      if (typeof id !== "number") throw new Error("local filesystem watch ID must be a number");
      await pluginHost.fs.unwatchDir(id);
    },
    readFile: (path, maxBytes) => pluginHost.fs.readFile(path, maxBytes),
    fileMeta: (path) => pluginHost.fs.fileMeta(path),
    openExternal: (path) => pluginHost.fs.openExternal(path),
    assetUrlFor: (path) => Promise.resolve(pluginHost.fs.assetUrlFor(path)),
    onDirChanged: (handler) => events?.on("plugin-fs:dir-changed", (data) => {
      if (typeof data === "string") handler(data);
    }) ?? (() => {}),
    dispose: () => {},
    writeFile: (path, data, expectedModTime) =>
      pluginHost.fs.writeFile(path, Array.from(data), expectedModTime ?? 0, expectedModTime === null),
    createFile: (path) => pluginHost.fs.createFile(path),
    rename: (from, to) => pluginHost.fs.rename(from, to),
    remove: (path, recursive) => pluginHost.fs.remove(path, recursive),
    mkdir: (path) => pluginHost.fs.mkdir(path),
    trash: (path) => pluginHost.fs.trash(path),
  };
}
