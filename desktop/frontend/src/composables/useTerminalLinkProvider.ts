import type { IDisposable, Terminal } from "xterm";
import {
  detectLinks,
  shouldActivateLink,
  mapWrappedLogicalLine,
  normalizeForOpen,
  type BufferLike,
  type LinkMatch,
  type PointerPos,
} from "../lib/terminalLinks";

export type LinkErrorKey =
  | "terminal.link.openFailed"
  | "terminal.link.openFailedNoHome";

export interface UseTerminalLinkProviderDeps {
  term: Terminal;
  isMac: boolean;
  getHomeDir: () => string;
  openURL: (url: string) => Promise<void>;
  onError: (key: LinkErrorKey) => void;
}

// A logical line never realistically spans more physical rows than this; the
// cap guards the upward walk against a malformed buffer that reports isWrapped
// forever.
const MAX_LOGICAL_ROWS = 50;

export function useTerminalLinkProvider(
  deps: UseTerminalLinkProviderDeps,
): IDisposable {
  const { term, isMac, getHomeDir, openURL, onError } = deps;

  // Track the most recent mousedown so activate() can tell a plain click from
  // the tail of a drag-select (which must not open the link).
  let lastDownPos: PointerPos | null = null;
  const onMouseDown = (e: MouseEvent) => {
    lastDownPos = { x: e.clientX, y: e.clientY };
  };
  const el = (term as unknown as { element?: HTMLElement }).element;
  el?.addEventListener("mousedown", onMouseDown);

  const provider = {
    provideLinks(y: number, callback: (links: unknown[] | undefined) => void) {
      const active = term.buffer.active as unknown as BufferLike;
      // Walk up to the first row of this logical line — the row that is not
      // itself a soft-wrap continuation. y is 1-based; getLine is 0-based.
      let firstIdx = y - 1;
      for (let steps = 0; steps < MAX_LOGICAL_ROWS; steps++) {
        const line = active.getLine(firstIdx);
        if (!line || !line.isWrapped) break;
        firstIdx--;
      }
      const mapped = mapWrappedLogicalLine(active, firstIdx, term.cols, MAX_LOGICAL_ROWS);
      const matches = detectLinks(mapped.text);
      if (matches.length === 0) {
        callback(undefined);
        return;
      }
      const wantRow = y - 1 - firstIdx; // 0-based physical row within this logical line
      const links: unknown[] = [];
      for (const m of matches) {
        // A match may span several physical rows; emit one ILink per row, but
        // keep only the segment on the requested row so each physical line
        // renders its own clickable piece (and we never emit duplicates).
        let segStart = m.start;
        while (segStart < m.end) {
          const row = mapped.cellY[segStart];
          let segEnd = segStart;
          while (segEnd < m.end && mapped.cellY[segEnd] === row) segEnd++;
          if (row === wantRow) {
            const startX = mapped.cellStart[segStart] + 1;
            const endX = mapped.cellStart[segEnd - 1] + 1;
            links.push(
              makeLink(m, firstIdx + row + 1, startX, endX, term, isMac, () => lastDownPos, getHomeDir, openURL, onError),
            );
          }
          segStart = segEnd;
        }
      }
      callback(links.length ? links : undefined);
    },
  };

  let providerDisposable: IDisposable = { dispose() {} };
  try {
    providerDisposable = term.registerLinkProvider(
      provider as unknown as Parameters<Terminal["registerLinkProvider"]>[0],
    );
  } catch (err) {
    console.warn("[AT Term] registerLinkProvider failed", err);
  }
  return {
    dispose() {
      el?.removeEventListener("mousedown", onMouseDown);
      providerDisposable.dispose();
    },
  };
}

function makeLink(
  m: LinkMatch,
  y: number,
  startX: number,
  endX: number,
  term: Terminal,
  isMac: boolean,
  getDownPos: () => PointerPos | null,
  getHomeDir: () => string,
  openURL: (url: string) => Promise<void>,
  onError: (key: LinkErrorKey) => void,
) {
  return {
    range: {
      start: { x: startX, y },
      end: { x: endX, y },
    },
    text: m.text,
    decorations: { underline: true, pointerCursor: true },
    activate: async (event: MouseEvent) => {
      if (!shouldActivateLink(event, getDownPos(), isMac)) return;
      event.preventDefault();
      event.stopPropagation();
      event.stopImmediatePropagation();
      // xterm's selection is set on the mousedown that precedes this click;
      // preventDefault on the click can't undo that. Clear it up front so both
      // the success and error paths leave the user with a clean terminal — a
      // click that opens a link means "open it", never "select it".
      term.clearSelection();
      const url = normalizeForOpen(m, getHomeDir());
      if (!url) {
        onError("terminal.link.openFailedNoHome");
        return;
      }
      try {
        await openURL(url);
      } catch (err) {
        console.warn("[AT Term] openURL failed", err);
        onError("terminal.link.openFailed");
      }
    },
  };
}
