import { getClipboardPastePayload, type ClipboardPastePayload } from "./api";
import type { SessionConnection, Status } from "./connection";
import type { MessageKey } from "../i18n";
import { pasteImageBus } from "./pasteImageBus";
import { effectiveRemotePermission, isPasteAllowed } from "./terminalContextMenu";

export type PastePlatform = "mac" | "linux" | "other";

function currentPastePlatform(): PastePlatform {
  if (typeof navigator === "undefined") return "other";
  const p = navigator.platform?.toLowerCase() ?? "";
  if (p.includes("mac")) return "mac";
  if (p.includes("linux")) return "linux";
  return "other";
}

// Neither macOS nor Wails-on-Linux (WebKit2GTK) reliably fires the browser
// `paste` event for Ctrl+V: it is not the OS paste shortcut on mac, and
// WebKit2GTK only fires it sporadically and never exposes the bitmap when it
// does. In both cases xterm forwards the raw `\x16` byte to the PTY and the TUI
// (Claude Code, Codex, ...) reads the system clipboard itself — so the image
// lands in the TUI but our preview toast never fires. This helper detects that
// gap so the keydown handler in TerminalView can intercept Ctrl+V and route it
// through pasteFromClipboard (which reads the clipboard via the backend, emits
// pasteImageBus for the preview, and sends the image once). The handler
// preventDefault()s the keydown so xterm does NOT also forward `\x16`, which
// would make the TUI read the clipboard a second time and double-paste.
//
// Cmd+V on mac and Ctrl+V on Windows already fire a working native paste event,
// so those return false here and are handled by the paste-event listener.
export function isCtrlVKeydownPaste(
  e: Pick<KeyboardEvent, "altKey" | "code" | "ctrlKey" | "key" | "metaKey" | "shiftKey">,
  platform: PastePlatform = currentPastePlatform(),
): boolean {
  if (platform !== "mac" && platform !== "linux") return false;
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
