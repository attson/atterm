import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { __setBindingsForTest } from "../../lib/api";
import { createSSHHostFS } from "./sshHostFS";

function bindingsWith(overrides: Record<string, unknown>) {
  __setBindingsForTest({
    SFTPListDir: vi.fn().mockResolvedValue({ path: "/", entries: [], truncated: false, total: 0 }),
    SFTPFileMeta: vi.fn().mockResolvedValue({ path: "/f", size: 1, modTime: 2, isBinary: false }),
    SFTPReadFile: vi.fn().mockResolvedValue({ path: "/f", data: "", isBinary: false }),
    SFTPWriteFile: vi.fn().mockResolvedValue({ path: "/f", size: 1, modTime: 2, isBinary: false }),
    SFTPCreateFile: vi.fn(),
    SFTPMkdir: vi.fn(),
    SFTPRename: vi.fn(),
    SFTPRemove: vi.fn().mockResolvedValue(undefined),
    SFTPDisconnect: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  } as never);
}

describe("createSSHHostFS", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });
  afterEach(() => __setBindingsForTest(undefined));

  it("carries a truncated listing through instead of dropping it", async () => {
    bindingsWith({
      SFTPListDir: vi.fn().mockResolvedValue({
        path: "/srv",
        entries: [{ name: "a", isDir: false }, { name: "b", isDir: false }],
        truncated: true,
        total: 3000,
      }),
    });
    const fs = createSSHHostFS("h1");

    const listing = await fs.listDirDetailed("/srv");
    expect(listing.truncated).toBe(true);
    expect(listing.total).toBe(3000);
    expect(listing.entries).toHaveLength(2);

    // The plain listDir still works for callers that do not care.
    expect(await fs.listDir("/srv")).toHaveLength(2);
  });

  it("turns the upload refusal into a sentence, not a protocol token", async () => {
    bindingsWith({
      SFTPWriteFile: vi.fn().mockRejectedValue(new Error("already_exists: /srv/app.conf")),
    });
    const fs = createSSHHostFS("h1");

    await expect(fs.writeFile("/srv/app.conf", new Uint8Array([1]), null)).rejects.toThrow(
      /already exists on the remote host/,
    );
    // And it must not be the raw token — that reads as a crash, not a refusal.
    await expect(fs.writeFile("/srv/app.conf", new Uint8Array([1]), null)).rejects.not.toThrow(
      /^already_exists/,
    );
  });

  it("passes an upload through as create-if-missing with no expected modtime", async () => {
    const SFTPWriteFile = vi.fn().mockResolvedValue({ path: "/srv/new", size: 3, modTime: 1, isBinary: false });
    bindingsWith({ SFTPWriteFile });
    const fs = createSSHHostFS("h1");

    await fs.writeFile("/srv/new", new Uint8Array([1, 2, 3]), null);
    expect(SFTPWriteFile).toHaveBeenCalledWith("h1", "/srv/new", [1, 2, 3], 0, true);
  });

  it("rejects trash with the sentinel the tree falls back on", async () => {
    // There is no trash on a remote host. The tree answers this exact message
    // by re-prompting with the permanent-delete confirmation; any other
    // wording turns "Move to Trash" into a dead menu entry.
    bindingsWith({});
    const fs = createSSHHostFS("h1");
    await expect(fs.trash("/srv/x")).rejects.toThrow("no platform trash command available");
  });

  it("releases the host's connection when the source is disposed", async () => {
    const SFTPDisconnect = vi.fn().mockResolvedValue(undefined);
    bindingsWith({ SFTPDisconnect });
    const fs = createSSHHostFS("h1");

    fs.dispose!();
    expect(SFTPDisconnect).toHaveBeenCalledWith("h1");

    // A disposed bridge must stop talking to the host rather than reopening it.
    await expect(fs.listDir("/srv")).rejects.toThrow();
  });

  it("says what it cannot do in a sentence, with no identifiers in it", async () => {
    bindingsWith({});
    const fs = createSSHHostFS("h1");
    await expect(fs.openExternal("/srv/x")).rejects.toThrow(/SSH host/);
    await expect(fs.assetUrlFor("/srv/x")).rejects.toThrow(/SSH host/);
    // "(openExternal)" is a symbol from this file, not something a user can do
    // anything with.
    await expect(fs.openExternal("/srv/x")).rejects.not.toThrow(/openExternal/);
    await expect(fs.assetUrlFor("/srv/x")).rejects.not.toThrow(/assetUrlFor/);
  });

  it("deletes a single remote file but refuses a directory", async () => {
    const SFTPRemove = vi.fn().mockResolvedValue(undefined);
    bindingsWith({ SFTPRemove });
    const fs = createSSHHostFS("h1");

    await fs.remove("/srv/one.log", false);
    expect(SFTPRemove).toHaveBeenCalledWith("h1", "/srv/one.log", false);

    // A recursive delete on a host with no trash is the one action here that
    // nothing can undo, and roadmap item 28 does not claim it. The tree gates
    // it too; this is the gate that does not depend on the UI asking nicely.
    await expect(fs.remove("/srv", true)).rejects.toThrow(/permanently/i);
    expect(SFTPRemove).toHaveBeenCalledTimes(1);
  });

  it("declares a search budget sized for round trips, not for a page cache", async () => {
    bindingsWith({});
    const fs = createSSHHostFS("h1");
    // The search walks one directory per network round trip and re-runs per
    // keystroke burst; the local default of 2000 would be thousands of them.
    expect(fs.searchMaxDirs).toBeLessThanOrEqual(200);
    expect(fs.dirRemovalRefusal?.("/srv")).toMatch(/permanently/i);
  });
});
