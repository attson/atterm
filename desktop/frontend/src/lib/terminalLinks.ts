export type LinkKind = "http" | "file" | "path";

export interface LinkMatch {
  /** Inclusive column index in the source line. */
  start: number;
  /** Exclusive column index in the source line. */
  end: number;
  /** Matched text (already trimmed of trailing punctuation). */
  text: string;
  kind: LinkKind;
}

// URL scheme regex: http(s)://… and file://…
// Body chars: anything that is not whitespace or a control character. Trailing
// punctuation is trimmed in a second pass so we can keep balanced `()` inside
// (e.g. Wikipedia titles) while still dropping a stray sentence-end `)`.
const URL_RE = /\b(https?|file):\/\/[^\s\x00-\x1f]+/g;

// Path candidates start at the line boundary or after a delimiter. Capturing
// the delimiter avoids lookbehind, which older WebKit rejects at module load.
// Classification below keeps this token sweep conservative for bare relative
// paths while still accepting explicit ./ and ../ forms.
const PATH_TOKEN_RE = /(^|[\s(){}\[\]<>"'`])([^\s\x00-\x1f]+)/g;

const TRAILING_TRIM = new Set([
  ".", ",", ";", ":", "!", "?", '"', "'",
  "\u3002", "\uff0c", "\uff1b", "\uff1a", "\uff01", "\uff1f",
  "\u201d", "\u2019", "\u300d", "\u300f",
]);

const BALANCED_CLOSERS: Record<string, string> = {
  ")": "(",
  "]": "[",
  "\uff09": "\uff08",
  "\u3011": "\u3010",
};

function trimTrailing(text: string): string {
  let end = text.length;
  while (end > 0) {
    const ch = text[end - 1];
    if (TRAILING_TRIM.has(ch)) {
      end--;
      continue;
    }
    const open = BALANCED_CLOSERS[ch];
    if (open) {
      const opens = countChar(text.slice(0, end - 1), open);
      const closes = countChar(text.slice(0, end - 1), ch);
      if (opens <= closes) {
        end--;
        continue;
      }
    }
    break;
  }
  return text.slice(0, end);
}

function countChar(s: string, ch: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) if (s[i] === ch) n++;
  return n;
}

function isPathCandidate(text: string): boolean {
  if (
    text.startsWith("/") ||
    text.startsWith("~/") ||
    text.startsWith("./") ||
    text.startsWith("../")
  ) {
    return true;
  }
  if (!text.includes("/") || text.includes("://")) return false;

  const segments = text.split("/").filter(Boolean);
  if (segments.length < 2 || segments.every((segment) => /^\d+$/.test(segment))) {
    return false;
  }
  const last = segments[segments.length - 1];
  const slashCount = countChar(text, "/");
  return slashCount >= 2 || (last.includes(".") && last !== "." && last !== "..");
}

export function detectLinks(line: string | null | undefined): LinkMatch[] {
  if (!line) return [];
  const out: LinkMatch[] = [];

  URL_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = URL_RE.exec(line)) !== null) {
    const raw = m[0];
    const trimmed = trimTrailing(raw);
    if (!trimmed) continue;
    const start = m.index;
    out.push({
      start,
      end: start + trimmed.length,
      text: trimmed,
      kind: m[1] === "file" ? "file" : "http",
    });
  }

  PATH_TOKEN_RE.lastIndex = 0;
  while ((m = PATH_TOKEN_RE.exec(line)) !== null) {
    // Group 1 is the consumed leading delimiter (empty at start-of-line);
    // drop it so start/text describe just the path itself.
    const lead = m[1];
    const raw = m[2];
    const trimmed = trimTrailing(raw);
    if (!trimmed || !isPathCandidate(trimmed)) continue;
    const start = m.index + lead.length;
    const end = start + trimmed.length;
    // Skip if this overlaps with any URL match already produced.
    if (out.some((u) => start < u.end && end > u.start)) continue;
    out.push({ start, end, text: trimmed, kind: "path" });
  }

  out.sort((a, b) => a.start - b.start);
  return out;
}

/** Minimal slice of xterm's IBufferCell the cell mapper needs. */
export interface CellLike {
  getChars(): string;
  getWidth(): number;
}

/** Minimal slice of xterm's IBufferLine the cell mapper needs. */
export interface BufferLineLike {
  getCell(x: number): CellLike | undefined;
}

export interface MappedLine {
  /** Visible text, one substring per non-spacer cell (a wide glyph -> its char). */
  text: string;
  /**
   * cellStart[i] = 0-based terminal cell column where text[i] begins;
   * cellStart[text.length] = the column just past the last cell. A link
   * spanning text[a, b) therefore occupies cells [cellStart[a], cellStart[b]).
   */
  cellStart: number[];
}

/**
 * Walk a terminal buffer line cell by cell, reproducing the visible string the
 * way xterm's translateToString() does while recording which cell column each
 * string char lands on.
 *
 * This is what lets us translate detectLinks()'s *string-index* spans into the
 * *cell columns* that xterm's link ranges and mouse hit-testing speak. The two
 * diverge whenever the line holds wide glyphs (CJK, emoji): a wide glyph is one
 * string char but two cells, so without this mapping a link's underline drifts
 * left one column per preceding wide glyph.
 */
export function mapBufferLineCells(
  line: BufferLineLike,
  cols: number,
): MappedLine {
  let text = "";
  const cellStart: number[] = [];
  let endExclusive = 0;
  for (let x = 0; x < cols; x++) {
    const cell = line.getCell(x);
    if (!cell) continue;
    const width = cell.getWidth();
    if (width === 0) continue; // spacer cell trailing a wide glyph
    const chars = cell.getChars() || " "; // an unwritten cell renders as a space
    for (let k = 0; k < chars.length; k++) cellStart.push(x);
    text += chars;
    endExclusive = x + width;
  }
  cellStart.push(endExclusive);
  return { text, cellStart };
}

/** True when a 0-based cell column falls within a detected link's cell span. */
export function cellInLink(
  col: number,
  m: Pick<LinkMatch, "start" | "end">,
  cellStart: number[],
): boolean {
  return col >= cellStart[m.start] && col < cellStart[m.end];
}

export interface PointerPos {
  x: number;
  y: number;
}

const DRAG_THRESHOLD_PX = 5;

/**
 * Decide whether a click on a detected link should open it. Only Ctrl+click
 * opens the link; plain clicks stay available for normal terminal selection and
 * focus. Shift/alt clicks never open, and a click preceded by a mousedown that
 * moved more than DRAG_THRESHOLD_PX is treated as a text drag-select, not an
 * activation. isMac is currently unused because the product intentionally uses
 * Ctrl+click rather than Cmd-click here.
 */
export function shouldActivateLink(
  e: Pick<MouseEvent, "shiftKey" | "altKey" | "ctrlKey" | "clientX" | "clientY">,
  downPos: PointerPos | null,
  _isMac: boolean,
): boolean {
  if (!e.ctrlKey) return false;
  if (e.shiftKey || e.altKey) return false;
  if (downPos) {
    const dx = e.clientX - downPos.x;
    const dy = e.clientY - downPos.y;
    if (Math.hypot(dx, dy) > DRAG_THRESHOLD_PX) return false;
  }
  return true;
}

export function normalizeForOpen(
  match: LinkMatch,
  homeDir: string | undefined,
): string | null {
  const t = match.text;
  if (match.kind === "http" || match.kind === "file") return t;
  if (t.startsWith("~/") || t === "~/") {
    if (!homeDir) return null;
    const home = homeDir.replace(/\/+$/, "");
    return `file://${home}${t.slice(1)}`;
  }
  return `file://${t}`;
}

/** Resolve a terminal path to the absolute path consumed by file reveal. */
export function resolvePathForReveal(
  match: LinkMatch,
  homeDir: string | undefined,
  cwd: string | undefined,
): string | null {
  if (match.kind === "http") return null;
  const raw = match.text;
  if (match.kind === "file") {
    return raw.startsWith("file://") ? raw.slice("file://".length) : raw;
  }
  if (raw.startsWith("/")) return raw;
  if (raw.startsWith("~/") || raw === "~/") {
    if (!homeDir) return null;
    return homeDir.replace(/\/+$/, "") + raw.slice(1);
  }
  if (!cwd?.startsWith("/")) return null;
  return normalizeAbsolutePath(`${cwd}/${raw}`);
}

/** True when path is the root itself or one of its descendants. */
export function isPathWithinRoot(path: string, root: string | null | undefined): boolean {
  if (!path.startsWith("/") || !root?.startsWith("/")) return false;
  const normalizedRoot = root === "/" ? "/" : root.replace(/\/+$/, "");
  if (normalizedRoot === "/") return true;
  return path === normalizedRoot || path.startsWith(`${normalizedRoot}/`);
}

function normalizeAbsolutePath(path: string): string {
  const parts: string[] = [];
  for (const part of path.split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") {
      parts.pop();
      continue;
    }
    parts.push(part);
  }
  return `/${parts.join("/")}`;
}

/** A buffer line that also reports xterm's soft-wrap flag. */
export interface WrappedLineLike {
  isWrapped: boolean;
  getCell(x: number): CellLike | undefined;
}

/** Minimal slice of xterm's active buffer the logical-line mapper needs. */
export interface BufferLike {
  getLine(y: number): WrappedLineLike | undefined;
}

export interface MappedLogicalLine {
  /** Joined visible text across all soft-wrapped physical rows. */
  text: string;
  /** cellStart[i] = 0-based cell column within its physical row for text[i]. */
  cellStart: number[];
  /** cellEnd[i] = end-exclusive terminal cell column for text[i]. */
  cellEnd: number[];
  /** cellY[i] = 0-based physical row index (0 = firstY) for text[i]. */
  cellY: number[];
  /** Number of physical rows this logical line spans. */
  rowCount: number;
}

export interface PhysicalLinkSegment {
  match: LinkMatch;
  /** Absolute 0-based buffer row containing this visible segment. */
  row: number;
  /** 0-based terminal cell range on row, end-exclusive. */
  startCell: number;
  endCell: number;
}

interface AbsoluteLogicalLine {
  text: string;
  cellStart: number[];
  cellEnd: number[];
  cellRow: number[];
  firstRow: number;
  rowCount: number;
}

function mapAbsoluteLogicalLine(
  buffer: BufferLike,
  firstRow: number,
  cols: number,
  maxRows: number,
): AbsoluteLogicalLine {
  let text = "";
  const cellStart: number[] = [];
  const cellEnd: number[] = [];
  const cellRow: number[] = [];
  let rowCount = 0;
  for (let offset = 0; offset < maxRows; offset++) {
    const row = firstRow + offset;
    const line = buffer.getLine(row);
    if (!line || (offset > 0 && !line.isWrapped)) break;
    rowCount = offset + 1;
    for (let x = 0; x < cols; x++) {
      const cell = line.getCell(x);
      if (!cell) continue;
      const width = cell.getWidth();
      if (width === 0) continue;
      const chars = cell.getChars() || " ";
      for (let index = 0; index < chars.length; index++) {
        cellStart.push(x);
        cellEnd.push(x + width);
        cellRow.push(row);
      }
      text += chars;
    }
  }
  return { text, cellStart, cellEnd, cellRow, firstRow, rowCount };
}

function segmentsForLogicalRange(
  logical: AbsoluteLogicalLine,
  start: number,
  end: number,
  match: LinkMatch,
  wantedRow: number,
): PhysicalLinkSegment[] {
  const out: PhysicalLinkSegment[] = [];
  while (start < end) {
    const row = logical.cellRow[start];
    let partEnd = start + 1;
    while (partEnd < end && logical.cellRow[partEnd] === row) partEnd++;
    if (row === wantedRow) {
      out.push({
        match,
        row,
        startCell: logical.cellStart[start],
        endCell: logical.cellEnd[partEnd - 1],
      });
    }
    start = partEnd;
  }
  return out;
}

/**
 * Walk a soft-wrapped logical line starting at physical row firstY, joining each
 * continuation row (the row whose isWrapped is true) into one string while
 * recording, per character, its cell column and which physical row it lands on.
 * Stops at the first non-wrapped continuation or after maxRows rows.
 */
export function mapWrappedLogicalLine(
  buffer: BufferLike,
  firstY: number,
  cols: number,
  maxRows: number,
): MappedLogicalLine {
  let text = "";
  const cellStart: number[] = [];
  const cellEnd: number[] = [];
  const cellY: number[] = [];
  let rowCount = 0;
  for (let row = 0; row < maxRows; row++) {
    const line = buffer.getLine(firstY + row);
    if (!line) break;
    if (row > 0 && !line.isWrapped) break; // next row is a hard line, stop
    rowCount = row + 1;
    for (let x = 0; x < cols; x++) {
      const cell = line.getCell(x);
      if (!cell) continue;
      const width = cell.getWidth();
      if (width === 0) continue;
      const chars = cell.getChars() || " ";
      for (let k = 0; k < chars.length; k++) {
        cellStart.push(x);
        cellEnd.push(x + width);
        cellY.push(row);
      }
      text += chars;
    }
  }
  return { text, cellStart, cellEnd, cellY, rowCount };
}

/**
 * Return every detected link segment that intersects one physical buffer row.
 * Besides xterm soft wraps, this recognizes the hard wraps produced by terminal
 * Markdown renderers for `label (https://...)`: those renderers align every URL
 * continuation with the start of the table cell and close it with `)`.
 * Requiring both delimiters and a stable continuation column avoids joining an
 * ordinary URL with unrelated output from the following command line.
 */
export function linkSegmentsAtBufferRow(
  buffer: BufferLike,
  row: number,
  cols: number,
  maxRows: number,
): PhysicalLinkSegment[] {
  const markdownSegments = dedupeSegments([
    ...hardWrappedMarkdownSegmentsAtRow(buffer, row, cols, maxRows),
    ...logicalMarkdownSegmentsAtRow(buffer, row, cols, maxRows),
  ]);
  const occupied = markdownSegments.map((s) => [s.startCell, s.endCell] as const);

  let firstRow = row;
  for (let steps = 0; steps < maxRows; steps++) {
    const line = buffer.getLine(firstRow);
    if (!line || !line.isWrapped) break;
    firstRow--;
  }
  const mapped = mapWrappedLogicalLine(buffer, firstRow, cols, maxRows);
  const wantedRelativeRow = row - firstRow;
  const ordinary: PhysicalLinkSegment[] = [];
  for (const match of detectLinks(mapped.text)) {
    let start = match.start;
    while (start < match.end) {
      const relativeRow = mapped.cellY[start];
      let end = start + 1;
      while (end < match.end && mapped.cellY[end] === relativeRow) end++;
      if (relativeRow === wantedRelativeRow) {
        const startCell = mapped.cellStart[start];
        const endCell = mapped.cellEnd[end - 1];
        if (!occupied.some(([a, b]) => startCell < b && endCell > a)) {
          ordinary.push({ match, row, startCell, endCell });
        }
      }
      start = end;
    }
  }
  return dedupeSegments([...markdownSegments, ...ordinary]);
}

function dedupeSegments(segments: PhysicalLinkSegment[]): PhysicalLinkSegment[] {
  const seen = new Set<string>();
  return segments
    .filter((segment) => {
      const key = [
        segment.row,
        segment.startCell,
        segment.endCell,
        segment.match.kind,
        segment.match.text,
      ].join("\0");
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    })
    .sort((a, b) => a.startCell - b.startCell || b.endCell - a.endCell);
}

interface PendingLogicalSegment {
  logical: AbsoluteLogicalLine;
  start: number;
  end: number;
}

function logicalMarkdownSegmentsAtRow(
  buffer: BufferLike,
  wantedRow: number,
  cols: number,
  maxRows: number,
): PhysicalLinkSegment[] {
  const out: PhysicalLinkSegment[] = [];
  let firstCandidateRow = Math.max(0, wantedRow - maxRows + 1);
  for (let steps = 0; steps < maxRows; steps++) {
    const line = buffer.getLine(firstCandidateRow);
    if (!line?.isWrapped || firstCandidateRow === 0) break;
    firstCandidateRow--;
  }

  for (let startRow = firstCandidateRow; startRow <= wantedRow;) {
    const firstLine = buffer.getLine(startRow);
    if (!firstLine) break;
    if (firstLine.isWrapped) {
      startRow++;
      continue;
    }
    const first = mapAbsoluteLogicalLine(buffer, startRow, cols, maxRows);
    if (first.rowCount === 0) break;
    const opener = /\((https?|file):\/\//g;
    let open: RegExpExecArray | null;
    while ((open = opener.exec(first.text)) !== null) {
      const urlStart = open.index + 1;
      const firstEnd = tokenEnd(first.text, urlStart);
      const firstPart = first.text.slice(urlStart, firstEnd);
      if (hasMarkdownClosingDelimiter(firstPart)) continue;

      for (const laneStart of possibleLaneStarts(first.text, open.index)) {
        const pending: PendingLogicalSegment[] = [
          { logical: first, start: urlStart, end: firstEnd },
        ];
        let joined = firstPart;
        let complete = false;
        let continuationRow = first.firstRow + first.rowCount;
        while (continuationRow < first.firstRow + maxRows) {
          const logical = mapAbsoluteLogicalLine(
            buffer,
            continuationRow,
            cols,
            first.firstRow + maxRows - continuationRow,
          );
          if (logical.rowCount === 0) break;
          if (logical.text[laneStart] === undefined || /\s/.test(logical.text[laneStart])) break;
          if (laneStart > 0 && !/\s/.test(logical.text[laneStart - 1])) break;
          const end = tokenEnd(logical.text, laneStart);
          const part = logical.text.slice(laneStart, end);
          pending.push({ logical, start: laneStart, end });
          joined += part;
          if (hasMarkdownClosingDelimiter(joined)) {
            complete = true;
            break;
          }
          continuationRow += logical.rowCount;
        }
        if (!complete) continue;
        const match = detectLinks(joined).find((candidate) => candidate.start === 0);
        if (!match || match.kind === "path" || match.end >= joined.length) continue;
        const last = pending[pending.length - 1];
        last.end -= joined.length - match.end;
        const normalized = { ...match, start: 0, end: match.text.length };
        for (const segment of pending) {
          out.push(...segmentsForLogicalRange(
            segment.logical,
            segment.start,
            segment.end,
            normalized,
            wantedRow,
          ));
        }
        break;
      }
    }
    startRow += first.rowCount;
  }
  return out;
}

interface PendingHardSegment {
  row: number;
  mapped: MappedLine;
  start: number;
  end: number;
}

function hardWrappedMarkdownSegmentsAtRow(
  buffer: BufferLike,
  wantedRow: number,
  cols: number,
  maxRows: number,
): PhysicalLinkSegment[] {
  const out: PhysicalLinkSegment[] = [];
  const firstCandidateRow = Math.max(0, wantedRow - maxRows + 1);
  for (let startRow = firstCandidateRow; startRow <= wantedRow; startRow++) {
    const firstLine = buffer.getLine(startRow);
    if (!firstLine || firstLine.isWrapped) continue;
    const firstMapped = mapBufferLineCells(firstLine, cols);
    const opener = /\((https?|file):\/\//g;
    let open: RegExpExecArray | null;
    while ((open = opener.exec(firstMapped.text)) !== null) {
      const urlStart = open.index + 1;
      const firstEnd = tokenEnd(firstMapped.text, urlStart);
      const firstPart = firstMapped.text.slice(urlStart, firstEnd);
      // A closing Markdown delimiter on the same row needs no reconstruction;
      // the ordinary detector below already handles it exactly.
      if (hasMarkdownClosingDelimiter(firstPart)) continue;

      for (const laneStart of possibleLaneStarts(firstMapped.text, open.index)) {
        const pending: PendingHardSegment[] = [
          { row: startRow, mapped: firstMapped, start: urlStart, end: firstEnd },
        ];
        let joined = firstPart;
        let complete = false;
        for (let continuationRow = startRow + 1;
          continuationRow < startRow + maxRows;
          continuationRow++) {
          const line = buffer.getLine(continuationRow);
          if (!line) break;
          const mapped = mapBufferLineCells(line, cols);
          if (mapped.text[laneStart] === undefined || /\s/.test(mapped.text[laneStart])) break;
          if (laneStart > 0 && !/\s/.test(mapped.text[laneStart - 1])) break;
          const end = tokenEnd(mapped.text, laneStart);
          const part = mapped.text.slice(laneStart, end);
          pending.push({ row: continuationRow, mapped, start: laneStart, end });
          joined += part;
          if (hasMarkdownClosingDelimiter(joined)) {
            complete = true;
            break;
          }
        }
        if (!complete) continue;
        const match = detectLinks(joined).find((candidate) => candidate.start === 0);
        if (!match || match.kind === "path" || match.end >= joined.length) continue;
        const trim = joined.length - match.end;
        const last = pending[pending.length - 1];
        last.end -= trim;
        for (const segment of pending) {
          if (segment.row !== wantedRow || segment.end <= segment.start) continue;
          out.push({
            match: { ...match, start: 0, end: match.text.length },
            row: segment.row,
            startCell: segment.mapped.cellStart[segment.start],
            endCell: segment.mapped.cellStart[segment.end],
          });
        }
        break;
      }
    }
  }
  return out;
}

function tokenEnd(text: string, start: number): number {
  let end = start;
  while (end < text.length && !/\s/.test(text[end])) end++;
  return end;
}

function hasMarkdownClosingDelimiter(text: string): boolean {
  const match = detectLinks(text).find((candidate) => candidate.start === 0);
  return !!match && match.end < text.length && text[match.end] === ")";
}

function possibleLaneStarts(text: string, openIndex: number): number[] {
  // A Markdown table renderer commonly emits `!123 (https://...)` and aligns
  // continuation rows with `!123`. Do not cross a two-space cell separator.
  let laneFloor = openIndex;
  let spaces = 0;
  for (let i = openIndex - 1; i >= 0; i--) {
    if (/\s/.test(text[i])) {
      spaces++;
      if (spaces >= 2) {
        laneFloor = i + 2;
        break;
      }
    } else {
      spaces = 0;
      laneFloor = i;
    }
  }
  const starts = [openIndex];
  for (let i = openIndex - 1; i >= laneFloor; i--) {
    if (!/\s/.test(text[i]) && (i === laneFloor || /\s/.test(text[i - 1]))) starts.push(i);
  }
  return starts;
}
