import type { IDisposable, Terminal } from "xterm";
import {
  detectLinks,
  isModClickEvent,
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
      const line =
        term.buffer.active.getLine(y - 1)?.translateToString(true) ?? "";
      const matches = detectLinks(line);
      if (matches.length === 0) {
        callback(undefined);
        return;
      }
      callback(
        matches.map((m) => toILink(m, y, isMac, getHomeDir, openURL, onError)),
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
  isMac: boolean,
  getHomeDir: () => string,
  openURL: (url: string) => Promise<void>,
  onError: (key: LinkErrorKey) => void,
) {
  return {
    range: {
      start: { x: m.start + 1, y },
      end: { x: m.end, y },
    },
    text: m.text,
    decorations: { underline: true, pointerCursor: true },
    activate: async (event: MouseEvent) => {
      if (!isModClickEvent(event, isMac)) return;
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
