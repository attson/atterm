import { afterEach, describe, expect, it, vi } from "vitest";
import { createRemoteSessionFS } from "./remoteSessionFS";
import type { FSEvent, FSRequest, FSResponse, SessionConnection } from "../../lib/connection";

function response(overrides: Partial<FSResponse> = {}): FSResponse {
  return { request_id: "request", ok: true, ...overrides };
}

function connection(responses: FSResponse[]) {
  const sendFSRequest = vi.fn<[FSRequest, number?], Promise<FSResponse>>();
  for (const item of responses) sendFSRequest.mockResolvedValueOnce(item);
  const fsEventHandlers = new Set<(event: FSEvent) => void>();
  const fsEventUnsubscribes: Array<() => void> = [];
  const onFSEvent = vi.fn((handler: (event: FSEvent) => void) => {
    fsEventHandlers.add(handler);
    const unsubscribe = vi.fn(() => fsEventHandlers.delete(handler));
    fsEventUnsubscribes.push(unsubscribe);
    return unsubscribe;
  });
  return {
    sendFSRequest,
    onFSEvent,
    fsEventUnsubscribes,
    emitFSEvent: (event: FSEvent) => fsEventHandlers.forEach((handler) => handler(event)),
  };
}

describe("RemoteFileSystemBridge", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("maps listDir to list_dir and returns entries", async () => {
    const conn = connection([response({ entries: [{ name: "src", isDir: true }] })]);

    await expect(createRemoteSessionFS(conn as unknown as SessionConnection).listDir("/project")).resolves.toEqual([
      { name: "src", isDir: true },
    ]);
    expect(conn.sendFSRequest).toHaveBeenCalledWith({ op: "list_dir", path: "/project" });
  });

  it("maps file operations and surfaces remote errors", async () => {
    const conn = connection([
      response({ meta: { path: "/a", size: 4, modTime: 1, isBinary: false } }),
      response({ content: { path: "/a", data: "aGVsbG8=", isBinary: false } }),
      response({ watch_id: "watch-1" }),
      response(),
      response(),
      response({ ok: false, error: "denied" }),
    ]);
    const fs = createRemoteSessionFS(conn as unknown as SessionConnection);

    await expect(fs.fileMeta("/a")).resolves.toMatchObject({ size: 4 });
    await expect(fs.readFile("/a")).resolves.toMatchObject({ data: [104, 101, 108, 108, 111] });
    await expect(fs.watchDir("/a")).resolves.toBe("watch-1");
    await expect(fs.unwatchDir("watch-1")).resolves.toBeUndefined();
    await expect(fs.openExternal("/a")).resolves.toBeUndefined();
    await expect(fs.fileMeta("/denied")).rejects.toThrow("denied");

    expect(conn.sendFSRequest).toHaveBeenNthCalledWith(1, { op: "file_meta", path: "/a" });
    expect(conn.sendFSRequest).toHaveBeenNthCalledWith(2, { op: "read_file", path: "/a", max_bytes: 2 * 1024 * 1024 });
    expect(conn.sendFSRequest).toHaveBeenNthCalledWith(3, { op: "watch_dir", path: "/a" });
    expect(conn.sendFSRequest).toHaveBeenNthCalledWith(4, { op: "unwatch_dir", watch_id: "watch-1" });
    expect(conn.sendFSRequest).toHaveBeenNthCalledWith(5, { op: "open_external", path: "/a" });
  });

  it("assembles chunked assets into a cached Blob URL and revokes it", async () => {
    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:asset");
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    const conn = connection([
      response({ chunk: { path: "/image.png", data: "aGk=", offset: 0, length: 2, eof: false, contentType: "image/png" } }),
      response({ chunk: { path: "/image.png", data: "IQ==", offset: 2, length: 1, eof: true, contentType: "application/octet-stream" } }),
    ]);
    const fs = createRemoteSessionFS(conn as unknown as SessionConnection);

    await expect(fs.assetUrlFor("/image.png")).resolves.toBe("blob:asset");
    await expect(fs.assetUrlFor("/image.png")).resolves.toBe("blob:asset");
    expect(createObjectURL).toHaveBeenCalledTimes(1);
    const imageBlob = createObjectURL.mock.calls[0][0] as Blob;
    expect(imageBlob.type).toBe("image/png");
    expect(imageBlob.size).toBe(3);
    expect(conn.sendFSRequest).toHaveBeenNthCalledWith(1, { op: "read_chunk", path: "/image.png", offset: 0, length: 256 * 1024 });
    expect(conn.sendFSRequest).toHaveBeenNthCalledWith(2, { op: "read_chunk", path: "/image.png", offset: 2, length: 256 * 1024 });

    fs.revokeAssetUrl("/image.png");
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:asset");
  });

  it("rejects assets larger than 50 MiB", async () => {
    const conn = connection([
      response({ chunk: { path: "/large.bin", data: "", offset: 0, length: 50 * 1024 * 1024 + 1, eof: true } }),
    ]);

    await expect(createRemoteSessionFS(conn as unknown as SessionConnection).assetUrlFor("/large.bin")).rejects.toThrow(/50 MiB/);
  });

  it("rejects a chunk whose declared length differs from its decoded bytes", async () => {
    const conn = connection([
      response({ chunk: { path: "/bad.bin", data: "aGk=", offset: 0, length: 3, eof: true } }),
    ]);

    await expect(createRemoteSessionFS(conn as unknown as SessionConnection).assetUrlFor("/bad.bin")).rejects.toThrow(/length/i);
  });

  it("revokes an asset URL that completes after it was invalidated", async () => {
    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:slow");
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    const conn = connection([]);
    let resolveRequest!: (value: FSResponse) => void;
    conn.sendFSRequest.mockImplementationOnce(
      () => new Promise<FSResponse>((resolve) => { resolveRequest = resolve; }),
    );
    const fs = createRemoteSessionFS(conn as unknown as SessionConnection);

    const pending = fs.assetUrlFor("/slow.png");
    fs.revokeAssetUrl("/slow.png");
    resolveRequest(response({ chunk: { path: "/slow.png", data: "aGk=", offset: 0, length: 2, eof: true } }));

    await expect(pending).rejects.toThrow(/invalidated/i);
    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:slow");
  });

  it("uses the default MIME type when the first chunk omits it", async () => {
    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:default-type");
    const conn = connection([
      response({ chunk: { path: "/unknown", data: "aGk=", offset: 0, length: 2, eof: false } }),
      response({ chunk: { path: "/unknown", data: "IQ==", offset: 2, length: 1, eof: true, contentType: "image/png" } }),
    ]);

    await createRemoteSessionFS(conn as unknown as SessionConnection).assetUrlFor("/unknown");
    expect((createObjectURL.mock.calls[0][0] as Blob).type).toBe("application/octet-stream");
  });

  it("forwards changed filesystem events and unsubscribes when unused", () => {
    const conn = connection([]);
    const fs = createRemoteSessionFS(conn as unknown as SessionConnection);
    const changed = vi.fn();

    const unsubscribe = fs.onDirChanged(changed);
    conn.emitFSEvent({ watch_id: "watch-1", path: "/project", event: "changed" });
    conn.emitFSEvent({ watch_id: "watch-1", path: "/project", event: "other" });
    expect(changed).toHaveBeenCalledTimes(1);
    expect(changed).toHaveBeenCalledWith("/project");

    unsubscribe();
    expect(conn.onFSEvent).toHaveBeenCalledTimes(1);
    expect(conn.fsEventUnsubscribes[0]).toHaveBeenCalledTimes(1);
    conn.emitFSEvent({ watch_id: "watch-1", path: "/project", event: "changed" });
    expect(changed).toHaveBeenCalledTimes(1);
  });

  it("keeps duplicate directory-change subscriptions independent", () => {
    const conn = connection([]);
    const fs = createRemoteSessionFS(conn as unknown as SessionConnection);
    const changed = vi.fn();
    const first = fs.onDirChanged(changed);
    const second = fs.onDirChanged(changed);

    first();
    conn.emitFSEvent({ watch_id: "watch-1", path: "/project", event: "changed" });
    expect(changed).toHaveBeenCalledTimes(1);
    expect(conn.fsEventUnsubscribes[0]).not.toHaveBeenCalled();

    second();
    expect(conn.fsEventUnsubscribes[0]).toHaveBeenCalledTimes(1);
  });

  it("isolates throwing directory-change handlers", () => {
    const conn = connection([]);
    const fs = createRemoteSessionFS(conn as unknown as SessionConnection);
    const throwing = vi.fn(() => { throw new Error("handler failed"); });
    const later = vi.fn();
    fs.onDirChanged(throwing);
    fs.onDirChanged(later);

    conn.emitFSEvent({ watch_id: "watch-1", path: "/project", event: "changed" });
    expect(throwing).toHaveBeenCalledTimes(1);
    expect(later).toHaveBeenCalledWith("/project");
  });

  it("disposes cached and pending assets and stops filesystem events", async () => {
    const createObjectURL = vi.spyOn(URL, "createObjectURL")
      .mockReturnValueOnce("blob:cached")
      .mockReturnValueOnce("blob:pending");
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    const conn = connection([
      response({ chunk: { path: "/cached", data: "aGk=", offset: 0, length: 2, eof: true } }),
    ]);
    const fs = createRemoteSessionFS(conn as unknown as SessionConnection);
    const changed = vi.fn();
    fs.onDirChanged(changed);
    await fs.assetUrlFor("/cached");

    let resolveRequest!: (value: FSResponse) => void;
    conn.sendFSRequest.mockImplementationOnce(
      () => new Promise<FSResponse>((resolve) => { resolveRequest = resolve; }),
    );
    const pending = fs.assetUrlFor("/pending");
    fs.dispose();
    resolveRequest(response({ chunk: { path: "/pending", data: "aGk=", offset: 0, length: 2, eof: true } }));

    await expect(pending).rejects.toThrow(/disposed/i);
    await expect(fs.assetUrlFor("/after-dispose")).rejects.toThrow(/disposed/i);
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:cached");
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:pending");
    expect(conn.fsEventUnsubscribes[0]).toHaveBeenCalledTimes(1);
    conn.emitFSEvent({ watch_id: "watch-1", path: "/project", event: "changed" });
    expect(changed).not.toHaveBeenCalled();
    expect(createObjectURL).toHaveBeenCalledTimes(2);
  });
});
