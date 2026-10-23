import type { IBuffer, IDisposable, IMarker, Terminal } from "xterm";
import { terminalImagePreviewBus } from "../lib/terminalImagePreviewBus";
import { cellCoordsAt } from "../lib/terminalCellCoords";
import {
  mapWrappedLogicalLine,
  type BufferLike,
} from "../lib/terminalLinks";

const MAX_LOGICAL_ROWS = 50;
const MAX_CACHED_IMAGES = 32;
const MAX_CACHED_BYTES = 64 * 1024 * 1024;
const PENDING_REANCHOR_MS = 5_000;
const IMAGE_PLACEHOLDER_RE = /\[Image #\d+\]/g;

export interface ImagePlaceholderMatch {
  start: number;
  end: number;
  text: string;
}

export interface ImagePlaceholderSegment {
  match: ImagePlaceholderMatch;
  occurrence: number;
  logicalFirstRow: number;
  row: number;
  startCell: number;
  endCell: number;
}

export function detectImagePlaceholders(text: string): ImagePlaceholderMatch[] {
  const matches: ImagePlaceholderMatch[] = [];
  IMAGE_PLACEHOLDER_RE.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = IMAGE_PLACEHOLDER_RE.exec(text)) !== null) {
    matches.push({
      start: match.index,
      end: match.index + match[0].length,
      text: match[0],
    });
  }
  return matches;
}

function logicalFirstRow(buffer: BufferLike, row: number, maxRows: number): number {
  let first = row;
  for (let steps = 0; steps < maxRows && first > 0; steps++) {
    const line = buffer.getLine(first);
    if (!line?.isWrapped) break;
    first--;
  }
  return first;
}

export function imagePlaceholderSegmentsAtBufferRow(
  buffer: BufferLike,
  row: number,
  cols: number,
  maxRows: number,
): ImagePlaceholderSegment[] {
  const firstRow = logicalFirstRow(buffer, row, maxRows);
  const mapped = mapWrappedLogicalLine(buffer, firstRow, cols, maxRows);
  const wantedRelativeRow = row - firstRow;
  const segments: ImagePlaceholderSegment[] = [];

  detectImagePlaceholders(mapped.text).forEach((match, occurrence) => {
    let start = match.start;
    while (start < match.end) {
      const relativeRow = mapped.cellY[start];
      if (relativeRow === undefined) break;
      let end = start + 1;
      while (end < match.end && mapped.cellY[end] === relativeRow) end++;
      if (relativeRow === wantedRelativeRow) {
        segments.push({
          match,
          occurrence,
          logicalFirstRow: firstRow,
          row,
          startCell: mapped.cellStart[start],
          // Image placeholders are ASCII, so every visible character is one
          // cell even when the logical line also contains wide glyphs.
          endCell: mapped.cellStart[end - 1] + 1,
        });
      }
      start = end;
    }
  });

  return segments;
}

interface CachedImage {
  id: string;
  file: Blob;
  name: string;
  marker: IMarker | null;
  occurrence: number;
  bound: boolean;
  queuedAt: number;
  lineSignature: string;
  lastLine: number;
  reanchorUntil: number;
}

export interface UseTerminalImagePreviewDeps {
  term: Terminal;
  sessionId: () => string;
}

interface XtermMouseInternals {
  screenElement?: HTMLElement;
  _mouseService?: {
    getCoords(
      event: { clientX: number; clientY: number },
      element: HTMLElement,
      cols: number,
      rows: number,
    ): [number, number] | undefined;
  };
}

function placeholdersOnLogicalLine(
  buffer: IBuffer,
  firstRow: number,
  cols: number,
): ImagePlaceholderMatch[] {
  if (firstRow < 0 || firstRow >= buffer.length) return [];
  return detectImagePlaceholders(
    mapWrappedLogicalLine(buffer as unknown as BufferLike, firstRow, cols, MAX_LOGICAL_ROWS).text,
  );
}

function logicalLineText(buffer: IBuffer, firstRow: number, cols: number): string {
  if (firstRow < 0 || firstRow >= buffer.length) return "";
  return mapWrappedLogicalLine(
    buffer as unknown as BufferLike,
    firstRow,
    cols,
    MAX_LOGICAL_ROWS,
  ).text.trimEnd();
}

function imageLineSignature(text: string): string {
  const first = detectImagePlaceholders(text)[0];
  return first ? text.slice(first.start).trimEnd() : "";
}

export function useTerminalImagePreview(
  deps: UseTerminalImagePreviewDeps,
): IDisposable {
  const { term, sessionId } = deps;
  let entries: CachedImage[] = [];
  let disposed = false;
  let hoverEntryID = "";
  let hoverURL = "";
  let hoverElement: HTMLDivElement | null = null;

  function hideHover(entryID?: string): void {
    if (entryID && hoverEntryID !== entryID) return;
    hoverElement?.remove();
    hoverElement = null;
    if (hoverURL) URL.revokeObjectURL(hoverURL);
    hoverURL = "";
    hoverEntryID = "";
  }

  function showHover(entry: CachedImage, event: MouseEvent): void {
    hideHover();
    const host = term.element;
    if (!host) return;

    let url: string;
    try {
      url = URL.createObjectURL(entry.file);
    } catch {
      return;
    }

    const tooltip = document.createElement("div");
    tooltip.className = "xterm-hover atterm-image-hover";
    tooltip.setAttribute("role", "tooltip");
    Object.assign(tooltip.style, {
      position: "absolute",
      zIndex: "30",
      maxWidth: "min(320px, calc(100% - 16px))",
      maxHeight: "260px",
      padding: "6px",
      border: "1px solid rgba(255, 255, 255, 0.2)",
      borderRadius: "6px",
      background: "rgba(18, 18, 20, 0.96)",
      boxShadow: "0 8px 24px rgba(0, 0, 0, 0.45)",
      pointerEvents: "none",
    });
    const image = document.createElement("img");
    image.src = url;
    image.alt = entry.name;
    Object.assign(image.style, {
      display: "block",
      maxWidth: "300px",
      maxHeight: "240px",
      objectFit: "contain",
    });
    tooltip.appendChild(image);

    const rect = host.getBoundingClientRect();
    const left = Math.max(8, Math.min(event.clientX - rect.left + 12, Math.max(8, rect.width - 320)));
    const top = Math.max(8, Math.min(event.clientY - rect.top + 12, Math.max(8, rect.height - 260)));
    tooltip.style.left = `${left}px`;
    tooltip.style.top = `${top}px`;
    host.appendChild(tooltip);
    hoverEntryID = entry.id;
    hoverURL = url;
    hoverElement = tooltip;
  }

  function removeEntry(entry: CachedImage, disposeMarker: boolean): void {
    entries = entries.filter((candidate) => candidate !== entry);
    hideHover(entry.id);
    const marker = entry.marker;
    entry.marker = null;
    if (disposeMarker && marker && !marker.isDisposed) marker.dispose();
  }

  function removeEntryByID(id: string): void {
    const entry = entries.find((candidate) => candidate.id === id);
    if (entry) removeEntry(entry, true);
  }

  function enforceCacheLimit(): void {
    let bytes = entries.reduce((sum, entry) => sum + entry.file.size, 0);
    while (entries.length > MAX_CACHED_IMAGES || bytes > MAX_CACHED_BYTES) {
      const oldest = entries[0];
      if (!oldest) break;
      bytes -= oldest.file.size;
      removeEntry(oldest, true);
    }
  }

  function anchorEntry(entry: CachedImage, firstRow: number, cursorRow: number): boolean {
    const marker = term.registerMarker(firstRow - cursorRow);
    if (!marker) return false;
    entry.marker = marker;
    entry.lastLine = firstRow;
    marker.onDispose(() => {
      if (entry.marker !== marker) return;
      entry.marker = null;
      entry.reanchorUntil = Date.now() + PENDING_REANCHOR_MS;
    });
    return true;
  }

  function findRedrawnLine(buffer: IBuffer, entry: CachedImage): number | null {
    if (!entry.lineSignature) return null;
    const rows = Math.max(1, term.rows);
    const start = Math.max(0, Math.min(buffer.viewportY, entry.lastLine) - rows);
    const end = Math.min(
      buffer.length - 1,
      Math.max(buffer.viewportY + rows - 1, entry.lastLine) + rows,
    );
    let best: number | null = null;
    let bestDistance = Number.POSITIVE_INFINITY;
    for (let row = start; row <= end; row++) {
      const line = buffer.getLine(row);
      if (!line || line.isWrapped) continue;
      const signature = imageLineSignature(logicalLineText(buffer, row, term.cols));
      if (signature !== entry.lineSignature) continue;
      const distance = Math.abs(row - entry.lastLine);
      if (distance < bestDistance) {
        best = row;
        bestDistance = distance;
      }
    }
    return best;
  }

  function refreshBindings(): void {
    if (disposed) return;
    const buffer = term.buffer.active;
    if (buffer.type !== "normal") return;

    const cursorRow = buffer.baseY + buffer.cursorY;
    for (const entry of [...entries]) {
      let marker = entry.marker;
      if (!marker || marker.isDisposed || marker.line < 0) {
        if (Date.now() > entry.reanchorUntil) {
          removeEntry(entry, false);
          continue;
        }
        const firstRow = entry.bound
          ? findRedrawnLine(buffer, entry)
          : logicalFirstRow(buffer as unknown as BufferLike, cursorRow, MAX_LOGICAL_ROWS);
        if (firstRow === null) continue;
        if (!anchorEntry(entry, firstRow, cursorRow)) continue;
        marker = entry.marker;
      }
      if (!marker) continue;
      const firstRow = logicalFirstRow(
        buffer as unknown as BufferLike,
        marker.line,
        MAX_LOGICAL_ROWS,
      );
      const matches = placeholdersOnLogicalLine(buffer, firstRow, term.cols);
      if (!entry.bound && entry.occurrence < matches.length) {
        entry.bound = true;
      }
      if (entry.bound) {
        if (entry.occurrence < matches.length) {
          // While the user keeps typing after the placeholder, evolve the
          // signature so it represents the complete line that will be
          // submitted and redrawn into history.
          entry.lastLine = firstRow;
          entry.lineSignature = imageLineSignature(logicalLineText(buffer, firstRow, term.cols));
          continue;
        }

        // Full-screen TUIs can erase/reuse a physical row without disposing
        // its marker. Preserve the last line signature and relocate just as we
        // do for a disposed marker; replacing it with an empty signature here
        // would make the submitted history line impossible to recover.
        entry.marker = null;
        entry.reanchorUntil = Date.now() + PENDING_REANCHOR_MS;
        if (!marker.isDisposed) marker.dispose();
        const redrawnRow = findRedrawnLine(buffer, entry);
        if (redrawnRow !== null) anchorEntry(entry, redrawnRow, cursorRow);
      } else {
        entry.lastLine = firstRow;
      }
    }
  }

  function queueImage(file: Blob, name: string, id: string): void {
    if (disposed) return;
    const buffer = term.buffer.active;
    if (buffer.type !== "normal") return;

    // A stale binding can share the current physical row after a TUI erases
    // and reuses it. Drop it before counting the new prompt's occurrences.
    for (const entry of [...entries]) {
      const marker = entry.marker;
      if (!entry.bound || !marker || marker.isDisposed) continue;
      if (entry.occurrence >= placeholdersOnLogicalLine(buffer, marker.line, term.cols).length) {
        removeEntry(entry, true);
      }
    }

    const cursorRow = buffer.baseY + buffer.cursorY;
    const firstRow = logicalFirstRow(buffer as unknown as BufferLike, cursorRow, MAX_LOGICAL_ROWS);
    const existingCount = placeholdersOnLogicalLine(buffer, firstRow, term.cols).length;
    const reservedCount = entries.filter((entry) =>
      !entry.bound
      && entry.marker !== null
      && !entry.marker.isDisposed
      && entry.marker.line === firstRow,
    ).length;

    const entry: CachedImage = {
      id,
      file,
      name,
      marker: null,
      occurrence: existingCount + reservedCount,
      bound: false,
      queuedAt: Date.now(),
      lineSignature: "",
      lastLine: firstRow,
      reanchorUntil: Date.now() + PENDING_REANCHOR_MS,
    };
    if (!anchorEntry(entry, firstRow, cursorRow)) return;
    entries.push(entry);
    enforceCacheLimit();
    refreshBindings();
  }

  function entryAtCell(bufferRow: number, col: number): CachedImage | null {
    const buffer = term.buffer.active;
    if (buffer.type !== "normal") return null;
    const segments = imagePlaceholderSegmentsAtBufferRow(
      buffer as unknown as BufferLike,
      bufferRow,
      term.cols,
      MAX_LOGICAL_ROWS,
    ).filter((segment) => col >= segment.startCell && col < segment.endCell);
    for (const segment of segments) {
      const entry = entries.find((candidate) => {
        const marker = candidate.marker;
        return candidate.bound
          && marker !== null
          && !marker.isDisposed
          && marker.line === segment.logicalFirstRow
          && candidate.occurrence === segment.occurrence;
      });
      if (entry) return entry;
    }
    return null;
  }

  function onTerminalMouseMove(event: MouseEvent): void {
    const host = term.element;
    const internals = (term as unknown as { _core?: XtermMouseInternals })._core;
    const screen = internals?.screenElement
      ?? host?.querySelector<HTMLElement>(".xterm-screen");
    const rendererCoords = screen
      ? internals?._mouseService?.getCoords(event, screen, term.cols, term.rows)
      : undefined;
    const grid = screen ?? host?.querySelector<HTMLElement>(".xterm-rows");
    if (!grid) {
      hideHover();
      return;
    }
    // xterm applies renderer-specific padding, DPR and cell rounding here.
    // Its screen is covered by our scrollable viewport, so the link provider
    // cannot receive the event itself, but the same mouse service can still
    // resolve coordinates from the capture listener on the terminal root.
    const hit = rendererCoords
      ? { col: rendererCoords[0] - 1, row: rendererCoords[1] - 1 }
      : cellCoordsAt(event.clientX, event.clientY, term, grid);
    if (!hit) {
      hideHover();
      return;
    }
    const entry = entryAtCell(term.buffer.active.viewportY + hit.row, hit.col);
    if (!entry) {
      hideHover();
      return;
    }
    if (hoverEntryID !== entry.id) showHover(entry, event);
  }

  function onTerminalMouseLeave(): void {
    hideHover();
  }

  const offPaste = terminalImagePreviewBus.on((event) => {
    if (event.sessionId !== sessionId()) return;
    if (event.kind === "cancel") {
      removeEntryByID(event.id);
      return;
    }
    queueImage(event.file, event.name, event.id);
  });
  const parsedDisposable = term.onWriteParsed(refreshBindings);
  term.element?.addEventListener("mousemove", onTerminalMouseMove, true);
  term.element?.addEventListener("mouseleave", onTerminalMouseLeave, true);

  const providerDisposable = term.registerLinkProvider({
    provideLinks(y, callback) {
      if (disposed) {
        callback(undefined);
        return;
      }
      const buffer = term.buffer.active;
      if (buffer.type !== "normal") {
        callback(undefined);
        return;
      }
      const row = y - 1;
      const segments = imagePlaceholderSegmentsAtBufferRow(
        buffer as unknown as BufferLike,
        row,
        term.cols,
        MAX_LOGICAL_ROWS,
      );
      const links = entries.flatMap((entry) => {
        const marker = entry.marker;
        if (!entry.bound || !marker || marker.isDisposed) return [];
        return segments
          .filter((segment) =>
            segment.logicalFirstRow === marker.line
            && segment.occurrence === entry.occurrence,
          )
          .map((segment) => ({
            range: {
              start: { x: segment.startCell + 1, y },
              end: { x: segment.endCell, y },
            },
            text: segment.match.text,
            decorations: { underline: true, pointerCursor: true },
            activate() {},
            hover(event: MouseEvent) { showHover(entry, event); },
            leave() { hideHover(entry.id); },
            dispose() { hideHover(entry.id); },
          }));
      });
      callback(links.length ? links : undefined);
    },
  });

  return {
    dispose() {
      if (disposed) return;
      disposed = true;
      offPaste();
      parsedDisposable.dispose();
      providerDisposable.dispose();
      term.element?.removeEventListener("mousemove", onTerminalMouseMove, true);
      term.element?.removeEventListener("mouseleave", onTerminalMouseLeave, true);
      hideHover();
      const remaining = entries;
      entries = [];
      for (const entry of remaining) {
        const marker = entry.marker;
        entry.marker = null;
        if (marker && !marker.isDisposed) marker.dispose();
      }
    },
  };
}

