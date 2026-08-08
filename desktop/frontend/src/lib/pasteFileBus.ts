import { errText, logWarn } from "./log";
export type PasteFileEvent = {
  id: string;
  name: string;
  size: number;
};

type Handler = (event: PasteFileEvent) => void;

const handlers = new Set<Handler>();

export const pasteFileBus = {
  emit(name: string, size: number): void {
    const event: PasteFileEvent = {
      id: crypto.randomUUID(),
      name: name || "file",
      size,
    };
    for (const h of handlers) {
      try {
        h(event);
      } catch (err) {
        logWarn("paste", "file bus handler threw", { error: errText(err) });
      }
    }
  },
  on(handler: Handler): () => void {
    handlers.add(handler);
    return () => {
      handlers.delete(handler);
    };
  },
};

// Test-only: clear all subscribers. Do not call from production code.
export function _resetHandlersForTest(): void {
  handlers.clear();
}
