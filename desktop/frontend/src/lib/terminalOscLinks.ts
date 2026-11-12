import type { IBufferRange, ILinkHandler } from "xterm";
import { shouldActivateLink, type LinkMatch, type PointerPos } from "./terminalLinks";

export interface TerminalOscLinkControllerDeps {
  isMac: boolean;
  getDownPos: () => PointerPos | null;
  resolveVisibleLink: (event: MouseEvent) => LinkMatch | null;
  openLink: (match: LinkMatch) => void | Promise<void>;
  clearSelection: () => void;
  onError: () => void;
}

export interface TerminalOscLinkController {
  handler: ILinkHandler;
  linkAtBufferCell(row: number, col: number): LinkMatch | null;
}

export function createTerminalOscLinkController(
  deps: TerminalOscLinkControllerDeps,
): TerminalOscLinkController {
  let hovered: { match: LinkMatch; range: IBufferRange } | null = null;
  return {
    handler: {
      activate(event, text) {
        if (!shouldActivateLink(event, deps.getDownPos(), deps.isMac)) return;
        const match = deps.resolveVisibleLink(event) ?? httpMatch(text);
        if (!match) return;
        event.preventDefault();
        event.stopPropagation();
        event.stopImmediatePropagation();
        deps.clearSelection();
        try {
          void Promise.resolve(deps.openLink(match)).catch(() => deps.onError());
        } catch {
          deps.onError();
        }
      },
      hover(_event, text, range) {
        const match = httpMatch(text);
        hovered = match ? { match, range } : null;
      },
      leave() {
        hovered = null;
      },
    },
    linkAtBufferCell(row, col) {
      if (!hovered) return null;
      const startRow = hovered.range.start.y - 1;
      const endRow = hovered.range.end.y - 1;
      if (row < startRow || row > endRow) return null;
      const startCol = row === startRow ? hovered.range.start.x - 1 : 0;
      const endCol = row === endRow ? hovered.range.end.x : Number.POSITIVE_INFINITY;
      return col >= startCol && col < endCol ? hovered.match : null;
    },
  };
}

function httpMatch(text: string): LinkMatch | null {
  try {
    const url = new URL(text);
    if (url.protocol !== "http:" && url.protocol !== "https:") return null;
  } catch {
    return null;
  }
  return { start: 0, end: text.length, text, kind: "http" };
}
