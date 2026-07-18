import { describe, expect, it, vi } from "vitest";
import { createLocalFSBridge } from "./fsBridge";
import type { EventBus, PluginHostBridge } from "../../platform/types";

function pluginHost(): PluginHostBridge {
  return {
    fs: {
      listDir: vi.fn(),
      watchDir: vi.fn(),
      unwatchDir: vi.fn(),
      readFile: vi.fn(),
      fileMeta: vi.fn(),
      openExternal: vi.fn(),
      assetUrlFor: vi.fn(),
      writeFile: vi.fn().mockResolvedValue({ path: "/x", size: 0, modTime: 0, isBinary: false }),
      createFile: vi.fn().mockResolvedValue({ path: "/x", size: 0, modTime: 0, isBinary: false }),
      rename: vi.fn().mockResolvedValue({ path: "/x", size: 0, modTime: 0, isBinary: false }),
      remove: vi.fn().mockResolvedValue(undefined),
      mkdir: vi.fn().mockResolvedValue({ path: "/x", size: 0, modTime: 0, isBinary: false }),
      trash: vi.fn().mockResolvedValue(undefined),
    },
  } as unknown as PluginHostBridge;
}

describe("createLocalFSBridge", () => {
  it("rejects non-numeric watch IDs without calling the Wails binding", async () => {
    const host = pluginHost();

    await expect(createLocalFSBridge(host).unwatchDir("remote-watch")).rejects.toThrow(/must be a number/);
    expect(host.fs.unwatchDir).not.toHaveBeenCalled();
  });

  it("writeFile forwards data + expectedModTime + createIfMissing=false", async () => {
    const host = pluginHost();
    const bridge = createLocalFSBridge(host);
    await bridge.writeFile("/x", new Uint8Array([1, 2, 3]), 42);
    expect(host.fs.writeFile).toHaveBeenCalledWith("/x", [1, 2, 3], 42, false);
  });

  it("writeFile with null expectedModTime sends createIfMissing=true and expected_modtime=0", async () => {
    const host = pluginHost();
    const bridge = createLocalFSBridge(host);
    await bridge.writeFile("/x", new Uint8Array(), null);
    expect(host.fs.writeFile).toHaveBeenCalledWith("/x", [], 0, true);
  });

  it("remove forwards recursive flag", async () => {
    const host = pluginHost();
    const bridge = createLocalFSBridge(host);
    await bridge.remove("/x", true);
    expect(host.fs.remove).toHaveBeenCalledWith("/x", true);
  });

  it("forwards plugin filesystem directory-change events", () => {
    let handler: ((data: unknown) => void) | undefined;
    const events = {
      on: vi.fn((_event: string, nextHandler: (data: unknown) => void) => {
        handler = nextHandler;
        return vi.fn();
      }),
    } as unknown as EventBus;
    const changed = vi.fn();

    createLocalFSBridge(pluginHost(), events).onDirChanged(changed);
    handler?.("/project");
    handler?.({ path: "/ignored" });

    expect(changed).toHaveBeenCalledTimes(1);
    expect(changed).toHaveBeenCalledWith("/project");
  });
});
