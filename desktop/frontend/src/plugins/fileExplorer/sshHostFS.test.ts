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

  it("says what it cannot do rather than throwing an undefined", async () => {
    bindingsWith({});
    const fs = createSSHHostFS("h1");
    await expect(fs.openExternal("/srv/x")).rejects.toThrow(/SSH host/);
    await expect(fs.assetUrlFor("/srv/x")).rejects.toThrow(/SSH host/);
  });
});
