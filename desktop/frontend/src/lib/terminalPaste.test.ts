import { describe, expect, it, vi } from "vitest";
import { pasteFromClipboard } from "./terminalPaste";

describe("pasteFromClipboard", () => {
  it("routes text payloads through term.paste", async () => {
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };

    const result = await pasteFromClipboard({
      term,
      conn,
      status: "attached",
      remotePermission: "full",
      getPayload: async () => ({ kind: "text", text: "echo hi\n" }),
    });

    expect(result).toEqual({ ok: true, kind: "text" });
    expect(term.paste).toHaveBeenCalledWith("echo hi\n");
    expect(conn.sendPasteImage).not.toHaveBeenCalled();
  });

  it("rejects image payloads for control permission sessions", async () => {
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };

    const result = await pasteFromClipboard({
      term,
      conn,
      status: "attached",
      remotePermission: "control",
      getPayload: async () => ({
        kind: "image",
        filename: "clipboard-image.png",
        content_type: "image/png",
        data_base64: "iVBORw0K",
      }),
    });

    expect(result.ok).toBe(false);
    expect(result.reason).toMatch(/full remote permission/);
    expect(conn.sendPasteImage).not.toHaveBeenCalled();
  });

  it("turns image payloads into blobs and reuses sendPasteImage", async () => {
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };

    const result = await pasteFromClipboard({
      term,
      conn,
      status: "attached",
      remotePermission: "full",
      getPayload: async () => ({
        kind: "image",
        filename: "clipboard-image.png",
        content_type: "image/png",
        data_base64: "iVBORw0K",
      }),
    });

    expect(result).toEqual({ ok: true, kind: "image" });
    expect(conn.sendPasteImage).toHaveBeenCalledTimes(1);
    expect(term.paste).not.toHaveBeenCalled();
  });
});
