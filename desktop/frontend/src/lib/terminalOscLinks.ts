import type { IBufferRange, ILinkHandler } from "xterm";
import {
  detectLinks,
  shouldActivateLink,
  type LinkMatch,
  type PointerPos,
} from "./terminalLinks";

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
  const match = detectLinks(stripEncodedMarkdownSuffix(text))
    .find((candidate) => candidate.start === 0);
  return match?.kind === "http" ? match : null;
}

function stripEncodedMarkdownSuffix(text: string): string {
  let parenDepth = 0;
  for (let index = 0; index < text.length;) {
    const encoded = text.slice(index, index + 3).toLowerCase();
    const tokenLength = encoded === "%28" || encoded === "%29" ? 3 : 1;
    const ch = encoded === "%28" ? "(" : encoded === "%29" ? ")" : text[index];
    if (ch === "(") {
      parenDepth++;
    } else if (ch === ")") {
      if (parenDepth > 0) {
        parenDepth--;
      } else if (startsEncodedProseSuffix(text.slice(index + tokenLength))) {
        return text.slice(0, index);
      }
    }
    index += tokenLength;
  }
  return text;
}

function startsEncodedProseSuffix(suffix: string): boolean {
  let decoded: string;
  try {
    decoded = decodeURIComponent(suffix);
  } catch {
    return false;
  }
  if (!decoded) return true;
  const punctuation = /^[.,;:!?"'\u3002\uff0c\uff1b\uff1a\uff01\uff1f\u201d\u2019\u300d\u300f]*/;
  const afterPunctuation = decoded.slice(decoded.match(punctuation)?.[0].length ?? 0);
  return /^\s/.test(afterPunctuation);
}
