import type { PluginHostBridge, DirEntry, FileContent, FileMetaInfo } from "../../platform/types";

export interface FileSystemBridge {
  readonly identity: string;
  listDir(path: string): Promise<DirEntry[]>;
  watchDir(path: string): Promise<number | string>;
  unwatchDir(id: number | string): Promise<void>;
  readFile(path: string, maxBytes?: number): Promise<FileContent>;
  fileMeta(path: string): Promise<FileMetaInfo>;
  openExternal(path: string): Promise<void>;
  assetUrlFor(path: string): string | Promise<string>;
  revokeAssetUrl?(path: string): void;
}

export function createLocalFSBridge(pluginHost: PluginHostBridge): FileSystemBridge {
  return {
    identity: "local",
    listDir: (path) => pluginHost.fs.listDir(path),
    watchDir: (path) => pluginHost.fs.watchDir(path),
    unwatchDir: (id) => pluginHost.fs.unwatchDir(id as number),
    readFile: (path, maxBytes) => pluginHost.fs.readFile(path, maxBytes),
    fileMeta: (path) => pluginHost.fs.fileMeta(path),
    openExternal: (path) => pluginHost.fs.openExternal(path),
    assetUrlFor: (path) => pluginHost.fs.assetUrlFor(path),
  };
}
