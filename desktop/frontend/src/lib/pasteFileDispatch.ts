import { posixShellQuote } from "./shellQuote";
import { logWarn } from "./log";

// dispatchPastedFile is the routing brain for a "Cmd+V of a File" event. It
// takes the clipboard's DataTransferItems plus injectable side-effect handles
// (paste-into-terminal, upload-to-remote-host, toast, native-pasteboard probe)
// and picks one of four outcomes:
//
//   'path-injected' — local session, the OS pasteboard carries file URLs
//     (e.g. Finder Copy). The absolute paths are shell-quoted and pasted into
//     xterm as if the user typed them. NO trailing CR — the user presses
//     Enter themselves. See feedback_template_send_split_cr.md.
//
//   'image-sent'    — no local file URLs available, but the clipboard has an
//     image blob (screenshot flow, Cmd+Shift+Ctrl+4). Uploaded via
//     conn.sendPasteImage; desktop side saves to paste-files/<sid>/ and
//     injects the cache path. Unchanged from prior behavior.
//
//   'file-sent'     — no local URLs, non-image file blob. Uploaded via
//     conn.sendPasteFile. This is the only branch that fires the "Received
//     files" toast (via onFileToast), matching the remote-inbox semantics of
//     paste-files/.
//
//   'skipped'       — no file item, oversized (>maxBytes), or getAsFile()
//     returned null. No side effects.
//
// Design note: the local-path probe (getLocalPaths) is only invoked when
// isLocalSession is true, so remote sessions never do the Wails IPC.
export type DispatchResult = "path-injected" | "image-sent" | "file-sent" | "skipped";

type PasteConn = {
  sendPasteImage(blob: Blob, filename: string): Promise<unknown>;
  sendPasteFile(blob: Blob, filename: string): Promise<unknown>;
};

export interface DispatchOptions {
  items: DataTransferItem[];
  isLocalSession: boolean;
  conn: PasteConn;
  paste(text: string): void;
  getLocalPaths: () => Promise<string[]>;
  onFileToast: (name: string, size: number) => void;
  maxBytes?: number;
}

const DEFAULT_MAX_BYTES = 10 * 1024 * 1024;

export async function dispatchPastedFile(opts: DispatchOptions): Promise<DispatchResult> {
  const maxBytes = opts.maxBytes ?? DEFAULT_MAX_BYTES;
  const imageItem = opts.items.find((i) => i.kind === "file" && i.type.startsWith("image/"));
  const anyFileItem = imageItem ?? opts.items.find((i) => i.kind === "file");
  if (!anyFileItem) return "skipped";

  const file = anyFileItem.getAsFile();
  if (!file) return "skipped";

  if (file.size > maxBytes) {
    logWarn("paste", "pasted file too large", { name: file.name, bytes: file.size });
    return "skipped";
  }

  if (opts.isLocalSession) {
    const paths = await opts.getLocalPaths();
    if (paths.length > 0) {
      opts.paste(paths.map(posixShellQuote).join(" "));
      return "path-injected";
    }
  }

  if (imageItem && anyFileItem === imageItem) {
    await opts.conn.sendPasteImage(file, file.name || "clipboard-image");
    return "image-sent";
  }

  const name = file.name || "file";
  opts.onFileToast(name, file.size);
  await opts.conn.sendPasteFile(file, name);
  return "file-sent";
}
