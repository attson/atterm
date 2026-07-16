import type { DirEntry, FileContent, FileMetaInfo } from "../../platform/types";
import type { FSChunkPayload, FSResponse, SessionConnection } from "../../lib/connection";
import type { FileSystemBridge } from "./fsBridge";

const DEFAULT_READ_MAX_BYTES = 2 * 1024 * 1024;
const ASSET_CHUNK_BYTES = 256 * 1024;
const MAX_ASSET_BYTES = 50 * 1024 * 1024;
const DEFAULT_ASSET_CONTENT_TYPE = "application/octet-stream";

export interface RemoteFileSystemBridge extends FileSystemBridge {
  assetUrlFor(path: string): Promise<string>;
  revokeAssetUrl(path: string): void;
}

function ensureOK(resp: FSResponse): FSResponse {
  if (!resp.ok) throw new Error(resp.error || "remote filesystem request failed");
  return resp;
}

function base64ToBytes(data: string): Uint8Array {
  try {
    const binary = atob(data);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return bytes;
  } catch {
    throw new Error("remote filesystem response contained invalid base64 data");
  }
}

function requireField<T>(value: T | undefined, field: string): T {
  if (value === undefined) throw new Error(`remote filesystem response missing ${field}`);
  return value;
}

export function createRemoteSessionFS(conn: SessionConnection, identity = "remote"): RemoteFileSystemBridge {
  const assetURLs = new Map<string, string>();
  const pendingAssetURLs = new Map<string, { generation: number; promise: Promise<string> }>();
  const assetGenerations = new Map<string, number>();
  const dirChangedHandlers = new Set<{ handler: (path: string) => void }>();
  let stopFSEvents: (() => void) | null = null;
  let disposed = false;

  async function listDir(path: string): Promise<DirEntry[]> {
    return requireField(ensureOK(await conn.sendFSRequest({ op: "list_dir", path })).entries, "entries");
  }

  async function watchDir(path: string): Promise<string> {
    return requireField(ensureOK(await conn.sendFSRequest({ op: "watch_dir", path })).watch_id, "watch_id");
  }

  async function unwatchDir(id: number | string): Promise<void> {
    await ensureOK(await conn.sendFSRequest({ op: "unwatch_dir", watch_id: String(id) }));
  }

  async function readFile(path: string, maxBytes = DEFAULT_READ_MAX_BYTES): Promise<FileContent> {
    const content = requireField(
      ensureOK(await conn.sendFSRequest({ op: "read_file", path, max_bytes: maxBytes })).content,
      "content",
    );
    return { ...content, data: Array.from(base64ToBytes(content.data)) };
  }

  async function fileMeta(path: string): Promise<FileMetaInfo> {
    return requireField(ensureOK(await conn.sendFSRequest({ op: "file_meta", path })).meta, "meta");
  }

  async function openExternal(path: string): Promise<void> {
    await ensureOK(await conn.sendFSRequest({ op: "open_external", path }));
  }

  async function fetchAssetURL(path: string): Promise<string> {
    const chunks: ArrayBuffer[] = [];
    let contentType = DEFAULT_ASSET_CONTENT_TYPE;
    let offset = 0;
    let firstChunk = true;

    for (;;) {
      const chunk = requireField(
        ensureOK(await conn.sendFSRequest({ op: "read_chunk", path, offset, length: ASSET_CHUNK_BYTES })).chunk,
        "chunk",
      );
      if (chunk.path !== path || chunk.offset !== offset) {
        throw new Error("remote filesystem response returned an unexpected chunk");
      }
      const declaredEnd = chunk.offset + chunk.length;
      if (declaredEnd > MAX_ASSET_BYTES) {
        throw new Error("remote asset exceeds the 50 MiB size limit");
      }
      const bytes = decodeChunk(chunk);
      if (offset + bytes.byteLength > MAX_ASSET_BYTES) {
        throw new Error("remote asset exceeds the 50 MiB size limit");
      }
      if (firstChunk) {
        contentType = chunk.contentType || DEFAULT_ASSET_CONTENT_TYPE;
        firstChunk = false;
      }
      chunks.push(copyToArrayBuffer(bytes));
      offset += bytes.byteLength;
      if (chunk.eof) break;
      if (bytes.byteLength === 0) throw new Error("remote filesystem response returned an empty non-final chunk");
    }

    const url = URL.createObjectURL(new Blob(chunks, { type: contentType }));
    return url;
  }

  function assetUrlFor(path: string): Promise<string> {
    if (disposed) return Promise.reject(new Error("remote filesystem bridge is disposed"));
    const cached = assetURLs.get(path);
    if (cached) return Promise.resolve(cached);
    const generation = assetGenerations.get(path) ?? 0;
    const pending = pendingAssetURLs.get(path);
    if (pending?.generation === generation) return pending.promise;
    const request = fetchAssetURL(path).then((url) => {
      if (disposed) {
        URL.revokeObjectURL(url);
        throw new Error("remote filesystem bridge is disposed");
      }
      if ((assetGenerations.get(path) ?? 0) !== generation) {
        URL.revokeObjectURL(url);
        throw new Error("remote filesystem asset request was invalidated");
      }
      assetURLs.set(path, url);
      return url;
    });
    pendingAssetURLs.set(path, { generation, promise: request });
    const clearPending = () => {
      if (pendingAssetURLs.get(path)?.promise === request) pendingAssetURLs.delete(path);
    };
    request.then(clearPending, clearPending);
    return request;
  }

  function revokeAssetUrl(path: string): void {
    assetGenerations.set(path, (assetGenerations.get(path) ?? 0) + 1);
    const url = assetURLs.get(path);
    if (url) {
      URL.revokeObjectURL(url);
      assetURLs.delete(path);
    }
  }

  function onDirChanged(handler: (path: string) => void): () => void {
    if (disposed) return () => {};
    const subscription = { handler };
    dirChangedHandlers.add(subscription);
    if (!stopFSEvents) {
      stopFSEvents = conn.onFSEvent((event) => {
        if (disposed || event.event !== "changed" || !event.path) return;
        for (const { handler: nextHandler } of Array.from(dirChangedHandlers)) {
          try {
            nextHandler(event.path);
          } catch {
            // One consumer must not block other file explorer subscribers.
          }
        }
      });
    }
    return () => {
      dirChangedHandlers.delete(subscription);
      if (dirChangedHandlers.size === 0 && stopFSEvents) {
        stopFSEvents();
        stopFSEvents = null;
      }
    };
  }

  function dispose(): void {
    if (disposed) return;
    disposed = true;
    for (const url of assetURLs.values()) URL.revokeObjectURL(url);
    assetURLs.clear();
    pendingAssetURLs.clear();
    assetGenerations.clear();
    dirChangedHandlers.clear();
    if (stopFSEvents) {
      stopFSEvents();
      stopFSEvents = null;
    }
  }

  return { identity, listDir, watchDir, unwatchDir, readFile, fileMeta, openExternal, assetUrlFor, onDirChanged, revokeAssetUrl, dispose };
}

function decodeChunk(chunk: FSChunkPayload): Uint8Array {
  if (typeof chunk.data !== "string") throw new Error("remote filesystem response chunk missing data");
  if (!Number.isSafeInteger(chunk.length) || chunk.length < 0) {
    throw new Error("remote filesystem response chunk has invalid length");
  }
  const bytes = base64ToBytes(chunk.data);
  if (chunk.length !== bytes.byteLength) {
    throw new Error("remote filesystem response chunk length does not match data");
  }
  return bytes;
}

function copyToArrayBuffer(bytes: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  return copy.buffer;
}
