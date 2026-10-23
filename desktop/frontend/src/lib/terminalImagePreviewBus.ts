import { errText, logWarn } from "./log";

export interface TerminalImagePreviewQueueEvent {
  kind: "queue";
  id: string;
  file: Blob;
  name: string;
  sessionId: string;
}

export interface TerminalImagePreviewCancelEvent {
  kind: "cancel";
  id: string;
  sessionId: string;
}

export type TerminalImagePreviewEvent =
  | TerminalImagePreviewQueueEvent
  | TerminalImagePreviewCancelEvent;

type Handler = (event: TerminalImagePreviewEvent) => void;
const handlers = new Set<Handler>();

// This bus is intentionally separate from pasteImageBus: pasteImageBus drives
// the immediate toast, while this one reserves the current input-line position
// before transport and cancels that reservation if the send fails.
export const terminalImagePreviewBus = {
  queue(file: Blob, name: string, sessionId: string): string {
    const event: TerminalImagePreviewQueueEvent = {
      kind: "queue",
      id: crypto.randomUUID(),
      file,
      name: name || "clipboard-image",
      sessionId,
    };
    for (const handler of handlers) {
      try {
        handler(event);
      } catch (err) {
        logWarn("paste", "terminal image preview handler threw", { error: errText(err) });
      }
    }
    return event.id;
  },
  cancel(id: string, sessionId: string): void {
    const event: TerminalImagePreviewCancelEvent = { kind: "cancel", id, sessionId };
    for (const handler of handlers) {
      try {
        handler(event);
      } catch (err) {
        logWarn("paste", "terminal image preview handler threw", { error: errText(err) });
      }
    }
  },
  on(handler: Handler): () => void {
    handlers.add(handler);
    return () => handlers.delete(handler);
  },
};

export async function sendWithTerminalImagePreview(
  file: Blob,
  name: string,
  sessionId: string | undefined,
  send: () => Promise<boolean>,
): Promise<boolean> {
  if (!sessionId) return send();
  const previewID = terminalImagePreviewBus.queue(file, name, sessionId);
  try {
    const sent = await send();
    if (!sent) terminalImagePreviewBus.cancel(previewID, sessionId);
    return sent;
  } catch (err) {
    terminalImagePreviewBus.cancel(previewID, sessionId);
    throw err;
  }
}

// Test-only: clear all subscribers. Do not call from production code.
export function _resetTerminalImagePreviewHandlersForTest(): void {
  handlers.clear();
}

