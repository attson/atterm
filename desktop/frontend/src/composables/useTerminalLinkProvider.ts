import type { IDisposable, Terminal } from "xterm";
import {
  linkSegmentsAtBufferRow,
  shouldActivateLink,
  type BufferLike,
  type LinkMatch,
  type PointerPos,
} from "../lib/terminalLinks";
import { errText, logWarn } from "../lib/log";

export type LinkErrorKey =
  | "terminal.link.openFailed"
  | "terminal.link.openFailedNoHome";

export interface UseTerminalLinkProviderDeps {
  term: Terminal;
  isMac: boolean;
  getHomeDir: () => string;
  // Called when a link is activated; the caller decides how to open the match
  // (external URL vs. reveal a local path in the file explorer).
  openLink: (match: LinkMatch) => Promise<void>;
  onError: (key: LinkErrorKey) => void;
}

// A logical line never realistically spans more physical rows than this; the
// cap guards the upward walk against a malformed buffer that reports isWrapped
// forever.
const MAX_LOGICAL_ROWS = 50;

export function useTerminalLinkProvider(
  deps: UseTerminalLinkProviderDeps,
): IDisposable {
  const { term, isMac, openLink, onError } = deps;

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
      const segments = linkSegmentsAtBufferRow(active, y - 1, term.cols, MAX_LOGICAL_ROWS);
      if (segments.length === 0) {
        callback(undefined);
        return;
      }
      callback(segments.map((segment) => makeLink(
        segment.match,
        segment.row + 1,
        segment.startCell + 1,
        segment.endCell,
        term,
        isMac,
        () => lastDownPos,
        openLink,
        onError,
      )));
    },
  };

  let providerDisposable: IDisposable = { dispose() {} };
  try {
    providerDisposable = term.registerLinkProvider(
      provider as unknown as Parameters<Terminal["registerLinkProvider"]>[0],
    );
  } catch (err) {
    logWarn("term-link", "registerLinkProvider failed", { error: errText(err) });
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
  openLink: (match: LinkMatch) => Promise<void>,
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
      // The caller decides http (external URL) vs. local path (reveal). Passing
      // the raw match keeps normalizeForOpen / home-dir resolution on that side.
      try {
        await openLink(m);
      } catch (err) {
        logWarn("term-link", "openLink failed", { error: errText(err) });
        onError("terminal.link.openFailed");
      }
    },
  };
}
