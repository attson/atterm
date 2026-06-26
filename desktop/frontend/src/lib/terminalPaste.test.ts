import { describe, expect, it, vi } from "vitest";
import { isMacCtrlVPaste, pasteFromClipboard } from "./terminalPaste";

function ev(parts: Partial<Pick<KeyboardEvent, "altKey" | "code" | "ctrlKey" | "key" | "metaKey" | "shiftKey">>) {
  return {
    altKey: false, ctrlKey: false, metaKey: false, shiftKey: false,
    code: "KeyV", key: "v",
    ...parts,
  };
}

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

  it("emits pasteImageBus before sending the image", async () => {
    const { pasteImageBus } = await import("./pasteImageBus");
    const emitSpy = vi.spyOn(pasteImageBus, "emit");
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };

    await pasteFromClipboard({
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

    expect(emitSpy).toHaveBeenCalledTimes(1);
    const [blob, name] = emitSpy.mock.calls[0];
    expect(blob).toBeInstanceOf(Blob);
    expect((blob as Blob).type).toBe("image/png");
    expect(name).toBe("clipboard-image.png");
    expect(conn.sendPasteImage).toHaveBeenCalledTimes(1);
    emitSpy.mockRestore();
  });

  it("does not emit pasteImageBus on text paste", async () => {
    const { pasteImageBus } = await import("./pasteImageBus");
    const emitSpy = vi.spyOn(pasteImageBus, "emit");
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };

    await pasteFromClipboard({
      term,
      conn,
      status: "attached",
      remotePermission: "full",
      getPayload: async () => ({ kind: "text", text: "hello" }),
    });

    expect(emitSpy).not.toHaveBeenCalled();
    emitSpy.mockRestore();
  });
});

describe("isMacCtrlVPaste", () => {
  it("matches Ctrl+V on mac (closes the gap where browser paste event does not fire)", () => {
    expect(isMacCtrlVPaste(ev({ ctrlKey: true }), "mac")).toBe(true);
  });

  it("does not match Cmd+V on mac (browser already fires paste event)", () => {
    expect(isMacCtrlVPaste(ev({ metaKey: true }), "mac")).toBe(false);
  });

  it("does not match Ctrl+Shift+V on mac (different shortcut, often a side-paste)", () => {
    expect(isMacCtrlVPaste(ev({ ctrlKey: true, shiftKey: true }), "mac")).toBe(false);
  });

  it("does not match Ctrl+Alt+V on mac", () => {
    expect(isMacCtrlVPaste(ev({ ctrlKey: true, altKey: true }), "mac")).toBe(false);
  });

  it("does not match Ctrl+Cmd+V on mac", () => {
    expect(isMacCtrlVPaste(ev({ ctrlKey: true, metaKey: true }), "mac")).toBe(false);
  });

  it("does not match Ctrl+V on non-mac (Win/Linux already get paste event natively)", () => {
    expect(isMacCtrlVPaste(ev({ ctrlKey: true }), "other")).toBe(false);
  });

  it("does not match other keys with Ctrl on mac", () => {
    expect(isMacCtrlVPaste(ev({ ctrlKey: true, code: "KeyC", key: "c" }), "mac")).toBe(false);
  });

  it("recognizes the v key regardless of letter case", () => {
    expect(isMacCtrlVPaste(ev({ ctrlKey: true, key: "V" }), "mac")).toBe(true);
  });
});
