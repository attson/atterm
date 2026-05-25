import { getClipboardPastePayload, type ClipboardPastePayload } from "./api";
import type { SessionConnection, Status } from "./connection";
import type { MessageKey } from "../i18n";
import { effectiveRemotePermission, isPasteAllowed } from "./terminalContextMenu";

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
    await opts.conn.sendPasteImage(
      base64ToBlob(payload.data_base64, payload.content_type),
      payload.filename || "clipboard-image",
    );
    return { ok: true, kind: "image" };
  }

  if (payload.reason) {
    const reasonKey = clipboardReasonKeys[payload.reason];
    return reasonKey ? { ok: false, reasonKey } : { ok: false, reason: payload.reason };
  }
  return { ok: false, reasonKey: "terminal.clipboardEmpty" };
}
