import { afterEach, describe, expect, it, vi } from "vitest";
import { __setBindingsForTest } from "./_bindings";
import type { AppBindings } from "./_bindings";
import {
  cancelFileTransfer,
  listFileTransfers,
  openSFTPSource,
  startFileTransfer,
  type StartFileTransferReq,
} from "./ssh";

afterEach(() => {
  __setBindingsForTest(undefined);
});

describe("SFTP workspace bindings", () => {
  it("opens a source fail-closed and forwards an explicit host-key pair", async () => {
    const OpenSFTPSource = vi.fn().mockResolvedValue({ host_id: "h1", initial_dir: "/home/u" });
    __setBindingsForTest({ OpenSFTPSource } as unknown as AppBindings);

    await expect(openSFTPSource("h1")).resolves.toEqual({ host_id: "h1", initial_dir: "/home/u" });
    expect(OpenSFTPSource).toHaveBeenNthCalledWith(1, "h1", { host: "", fingerprint: "" });

    await openSFTPSource("h1", { host: "host.example:22", fingerprint: "SHA256:abc" });
    expect(OpenSFTPSource).toHaveBeenNthCalledWith(2, "h1", {
      host: "host.example:22",
      fingerprint: "SHA256:abc",
    });
  });

  it("forwards transfer start, cancel, and list arguments without rewriting them", async () => {
    const StartFileTransfer = vi.fn().mockResolvedValue("job-1");
    const CancelFileTransfer = vi.fn().mockResolvedValue(undefined);
    const ListFileTransfers = vi.fn().mockResolvedValue([]);
    __setBindingsForTest({ StartFileTransfer, CancelFileTransfer, ListFileTransfers } as unknown as AppBindings);
    const request: StartFileTransferReq = {
      source: { kind: "ssh", host_id: "left" },
      destination: { kind: "ssh", host_id: "right" },
      paths: ["/src/a", "/src/b"],
      destination_dir: "/dst",
      conflict: "rename",
      accepted_host_key: { host: "jump.example:22", fingerprint: "SHA256:def" },
    };

    await expect(startFileTransfer(request)).resolves.toBe("job-1");
    expect(StartFileTransfer).toHaveBeenCalledWith(request);
    await cancelFileTransfer("job-1");
    expect(CancelFileTransfer).toHaveBeenCalledWith("job-1");
    await expect(listFileTransfers()).resolves.toEqual([]);
    expect(ListFileTransfers).toHaveBeenCalledWith();
  });
});
