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
    },
  } as unknown as PluginHostBridge;
}

describe("createLocalFSBridge", () => {
  it("rejects non-numeric watch IDs without calling the Wails binding", async () => {
    const host = pluginHost();

    await expect(createLocalFSBridge(host).unwatchDir("remote-watch")).rejects.toThrow(/must be a number/);
    expect(host.fs.unwatchDir).not.toHaveBeenCalled();
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
