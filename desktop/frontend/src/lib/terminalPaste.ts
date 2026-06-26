import { getClipboardPastePayload, type ClipboardPastePayload } from "./api";
import type { SessionConnection, Status } from "./connection";
import type { MessageKey } from "../i18n";
import { pasteImageBus } from "./pasteImageBus";
import { effectiveRemotePermission, isPasteAllowed } from "./terminalContextMenu";

export type PastePlatform = "mac" | "other";

function currentPastePlatform(): PastePlatform {
  if (typeof navigator === "undefined") return "other";
  return navigator.platform?.toLowerCase().includes("mac") ? "mac" : "other";
}

// macOS does not fire the browser `paste` event for Ctrl+V (that key is not
// the OS paste shortcut). xterm forwards the raw `\x16` byte to the PTY,
// and the TUI (Claude Code, Codex, ...) intercepts it to read the system
// clipboard itself — so the image lands but our preview toast never fires.
// This helper detects that exact gap so the keydown handler in TerminalView
// can route Ctrl+V through pasteFromClipboard (which emits pasteImageBus).
// Cmd+V on mac and Ctrl+V on Win/Linux already fire the native paste event
// and are handled there — those return false here to avoid double-paste.
export function isMacCtrlVPaste(
  e: Pick<KeyboardEvent, "altKey" | "code" | "ctrlKey" | "key" | "metaKey" | "shiftKey">,
  platform: PastePlatform = currentPastePlatform(),
): boolean {
  if (platform !== "mac") return false;
  if (!e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return false;
  return e.code === "KeyV" || e.key.toLowerCase() === "v";
}

interface TerminalLike {
  paste: (text: string) => void;
}

export interface PasteFromClipboardOptions {
  term: TerminalLike;
  conn: Pick<SessionConnection, "sendPasteImage">;
  status: Status;
  remotePermission?: string;
  getPayload?: () => Promise<ClipboardPastePayload>;
}

export interface PasteResult {
  ok: boolean;
  kind?: "text" | "image";
  reasonKey?: MessageKey;
  reason?: string;
}

const clipboardReasonKeys: Record<string, MessageKey> = {
  "clipboard has no text or image": "terminal.clipboardEmpty",
  "clipboard image too large": "terminal.clipboardImageTooLarge",
  "install xclip, wl-paste, or xsel to paste images": "terminal.clipboardImageToolsMissing",
};

function base64ToBlob(dataBase64: string, contentType: string): Blob {
  const raw = atob(dataBase64);
  const bytes = Uint8Array.from(raw, (char) => char.charCodeAt(0));
  return new Blob([bytes], { type: contentType });
}

export async function pasteFromClipboard(opts: PasteFromClipboardOptions): Promise<PasteResult> {
  if (!isPasteAllowed(opts.status, opts.remotePermission)) {
    return { ok: false, reasonKey: "terminal.pasteSessionNotWritable" };
  }

  const payload = await (opts.getPayload ? opts.getPayload() : getClipboardPastePayload());
  if (payload.kind === "text" && payload.text) {
    opts.term.paste(payload.text);
    return { ok: true, kind: "text" };
  }

  if (payload.kind === "image" && payload.content_type && payload.data_base64) {
    if (effectiveRemotePermission(opts.remotePermission) === "control") {
      return { ok: false, reasonKey: "terminal.imagePasteRequiresFull" };
    }
    const blob = base64ToBlob(payload.data_base64, payload.content_type);
    const name = payload.filename || "clipboard-image";
    pasteImageBus.emit(blob, name);
    await opts.conn.sendPasteImage(blob, name);
    return { ok: true, kind: "image" };
  }

  if (payload.reason) {
    const reasonKey = clipboardReasonKeys[payload.reason];
    return reasonKey ? { ok: false, reasonKey } : { ok: false, reason: payload.reason };
  }
  return { ok: false, reasonKey: "terminal.clipboardEmpty" };
}
