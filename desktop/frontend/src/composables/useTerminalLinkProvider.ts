import type { IDisposable, Terminal } from "xterm";
import {
  detectLinks,
  isModClickEvent,
  linkCellRange,
  mapBufferLineCells,
  normalizeForOpen,
  type LinkMatch,
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

export function useTerminalLinkProvider(
  deps: UseTerminalLinkProviderDeps,
): IDisposable {
  const { term, isMac, getHomeDir, openURL, onError } = deps;

  const provider = {
    provideLinks(y: number, callback: (links: unknown[] | undefined) => void) {
      const line = term.buffer.active.getLine(y - 1);
      if (!line) {
        callback(undefined);
        return;
      }
      // Map through cells so wide glyphs (CJK, emoji) don't shift the underline
      // left of the actual link text.
      const { text, cellStart } = mapBufferLineCells(line, term.cols);
      const matches = detectLinks(text);
      if (matches.length === 0) {
        callback(undefined);
        return;
      }
      callback(
        matches.map((m) =>
          toILink(m, y, cellStart, term, isMac, getHomeDir, openURL, onError),
        ),
      );
    },
  };

  try {
    return term.registerLinkProvider(provider as unknown as Parameters<Terminal["registerLinkProvider"]>[0]);
  } catch (err) {
    console.warn("[AT Term] registerLinkProvider failed", err);
    return { dispose() {} };
  }
}

function toILink(
  m: LinkMatch,
  y: number,
  cellStart: number[],
  term: Terminal,
  isMac: boolean,
  getHomeDir: () => string,
  openURL: (url: string) => Promise<void>,
  onError: (key: LinkErrorKey) => void,
) {
  const { startX, endX } = linkCellRange(m, cellStart);
  return {
    range: {
      start: { x: startX, y },
      end: { x: endX, y },
    },
    text: m.text,
    decorations: { underline: true, pointerCursor: true },
    activate: async (event: MouseEvent) => {
      if (!isModClickEvent(event, isMac)) return;
      event.preventDefault();
      event.stopPropagation();
      event.stopImmediatePropagation();
      // xterm's selection is set on the mousedown that precedes this
      // click; preventDefault on the click can't undo that. Clear it up
      // front so both the success and error paths leave the user with a
      // clean terminal — Mod-click on a link means "open it", never
      // "select it".
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
