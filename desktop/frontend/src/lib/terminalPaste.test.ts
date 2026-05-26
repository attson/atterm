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
    expect(result.reasonKey).toBe("terminal.imagePasteRequiresFull");
    expect(conn.sendPasteImage).not.toHaveBeenCalled();
  });

  it("returns a stable i18n reason key when paste is not writable", async () => {
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };
    const getPayload = vi.fn(async () => ({ kind: "text" as const, text: "echo hi\n" }));

    const result = await pasteFromClipboard({
      term,
      conn,
      status: "connecting",
      remotePermission: "full",
      getPayload,
    });

    expect(result).toEqual({ ok: false, reasonKey: "terminal.pasteSessionNotWritable" });
    expect(getPayload).not.toHaveBeenCalled();
    expect(term.paste).not.toHaveBeenCalled();
  });

  it("maps the backend empty clipboard reason to a stable i18n key", async () => {
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };

    const result = await pasteFromClipboard({
      term,
      conn,
      status: "attached",
      remotePermission: "full",
      getPayload: async () => ({ kind: "none", reason: "clipboard has no text or image" }),
    });

    expect(result).toEqual({ ok: false, reasonKey: "terminal.clipboardEmpty" });
    expect(term.paste).not.toHaveBeenCalled();
  });

  it("maps known backend clipboard image reasons to stable i18n keys", async () => {
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };

    await expect(
      pasteFromClipboard({
        term,
        conn,
        status: "attached",
        remotePermission: "full",
        getPayload: async () => ({ kind: "none", reason: "clipboard image too large" }),
      }),
    ).resolves.toEqual({ ok: false, reasonKey: "terminal.clipboardImageTooLarge" });

    await expect(
      pasteFromClipboard({
        term,
        conn,
        status: "attached",
        remotePermission: "full",
        getPayload: async () => ({ kind: "none", reason: "install xclip, wl-paste, or xsel to paste images" }),
      }),
    ).resolves.toEqual({ ok: false, reasonKey: "terminal.clipboardImageToolsMissing" });
  });

  it("keeps unknown backend clipboard reasons for actionable diagnostics", async () => {
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };

    const result = await pasteFromClipboard({
      term,
      conn,
      status: "attached",
      remotePermission: "full",
      getPayload: async () => ({ kind: "none", reason: "pbpaste exited 1" }),
    });

    expect(result).toEqual({ ok: false, reason: "pbpaste exited 1" });
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
