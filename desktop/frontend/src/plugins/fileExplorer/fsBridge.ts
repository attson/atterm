import type { EventBus, PluginHostBridge, DirEntry, FileContent, FileMetaInfo } from "../../platform/types";

export interface FileSystemBridge {
  readonly identity: string;
  listDir(path: string): Promise<DirEntry[]>;
  watchDir(path: string): Promise<number | string>;
  unwatchDir(id: number | string): Promise<void>;
  readFile(path: string, maxBytes?: number): Promise<FileContent>;
  fileMeta(path: string): Promise<FileMetaInfo>;
  openExternal(path: string): Promise<void>;
  assetUrlFor(path: string): string | Promise<string>;
  onDirChanged(handler: (path: string) => void): () => void;
  revokeAssetUrl?(path: string): void;
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
    assetUrlFor: (path) => pluginHost.fs.assetUrlFor(path),
    onDirChanged: (handler) => events?.on("plugin-fs:dir-changed", (data) => {
      if (typeof data === "string") handler(data);
    }) ?? (() => {}),
  };
}
