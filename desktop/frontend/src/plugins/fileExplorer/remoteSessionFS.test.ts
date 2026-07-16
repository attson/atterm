import { afterEach, describe, expect, it, vi } from "vitest";
import { createRemoteSessionFS } from "./remoteSessionFS";
import type { FSRequest, FSResponse, SessionConnection } from "../../lib/connection";

function response(overrides: Partial<FSResponse> = {}): FSResponse {
  return { request_id: "request", ok: true, ...overrides };
}

function connection(responses: FSResponse[]) {
  const sendFSRequest = vi.fn<[FSRequest, number?], Promise<FSResponse>>();
  for (const item of responses) sendFSRequest.mockResolvedValueOnce(item);
  return { sendFSRequest } as unknown as SessionConnection;
}

describe("RemoteFileSystemBridge", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("maps listDir to list_dir and returns entries", async () => {
    const conn = connection([response({ entries: [{ name: "src", isDir: true }] })]);

    await expect(createRemoteSessionFS(conn).listDir("/project")).resolves.toEqual([
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
    const fs = createRemoteSessionFS(conn);

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
      response({ chunk: { path: "/image.png", data: "IQ==", offset: 2, length: 1, eof: true } }),
    ]);
    const fs = createRemoteSessionFS(conn);

    await expect(fs.assetUrlFor("/image.png")).resolves.toBe("blob:asset");
    await expect(fs.assetUrlFor("/image.png")).resolves.toBe("blob:asset");
    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(createObjectURL.mock.calls[0][0]).toMatchObject({ type: "image/png", size: 3 });
    expect(conn.sendFSRequest).toHaveBeenNthCalledWith(1, { op: "read_chunk", path: "/image.png", offset: 0, length: 256 * 1024 });
    expect(conn.sendFSRequest).toHaveBeenNthCalledWith(2, { op: "read_chunk", path: "/image.png", offset: 2, length: 256 * 1024 });

    fs.revokeAssetUrl("/image.png");
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:asset");
  });

  it("rejects assets larger than 50 MiB", async () => {
    const conn = connection([
      response({ chunk: { path: "/large.bin", data: "", offset: 0, length: 50 * 1024 * 1024 + 1, eof: true } }),
    ]);

    await expect(createRemoteSessionFS(conn).assetUrlFor("/large.bin")).rejects.toThrow(/50 MiB/);
  });
});
