export type PasteImageEvent = {
  id: string;
  file: Blob;
  name: string;
};

type Handler = (event: PasteImageEvent) => void;

const handlers = new Set<Handler>();

export const pasteImageBus = {
  emit(file: Blob, name: string): void {
    const event: PasteImageEvent = {
      id: crypto.randomUUID(),
      file,
      name: name || "clipboard-image",
    };
    for (const h of handlers) {
      try {
        h(event);
      } catch (err) {
        console.warn("[pasteImageBus] handler threw", err);
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
