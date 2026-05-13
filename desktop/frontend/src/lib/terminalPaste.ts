import { getClipboardPastePayload, type ClipboardPastePayload } from "./api";
import type { SessionConnection, Status } from "./connection";
import { imagePasteBlockedReason, isPasteAllowed } from "./terminalContextMenu";

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
  reason?: string;
}

function base64ToBlob(dataBase64: string, contentType: string): Blob {
  const raw = atob(dataBase64);
  const bytes = Uint8Array.from(raw, (char) => char.charCodeAt(0));
  return new Blob([bytes], { type: contentType });
}

export async function pasteFromClipboard(opts: PasteFromClipboardOptions): Promise<PasteResult> {
  if (!isPasteAllowed(opts.status, opts.remotePermission)) {
    return { ok: false, reason: "session is not writable right now" };
  }

  const payload = await (opts.getPayload ? opts.getPayload() : getClipboardPastePayload());
  if (payload.kind === "text" && payload.text) {
    opts.term.paste(payload.text);
    return { ok: true, kind: "text" };
  }

  if (payload.kind === "image" && payload.content_type && payload.data_base64) {
    const blocked = imagePasteBlockedReason(opts.remotePermission);
    if (blocked) return { ok: false, reason: blocked };
    await opts.conn.sendPasteImage(
      base64ToBlob(payload.data_base64, payload.content_type),
      payload.filename || "clipboard-image",
    );
    return { ok: true, kind: "image" };
  }

  return { ok: false, reason: payload.reason || "clipboard has no text or image" };
}
