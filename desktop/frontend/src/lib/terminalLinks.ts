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

// Absolute path: starts at start-of-line or after a delimiter char, with '/'
// or '~/'; body chars exclude whitespace + control. We capture the leading
// delimiter in group 1 instead of using a lookbehind: older WebKit (e.g. the
// Safari shipped with macOS 12) rejects lookbehind with "Invalid regular
// expression: invalid group specifier name", which throws at module-eval time
// and blanks the whole app. detectLinks() strips group 1 back off so the
// reported span still excludes the delimiter. The leading anchor keeps us from
// treating 12/24 as a path (digit before slash is neither ^ nor a delimiter).
const PATH_RE = /(^|[\s(){}\[\]<>"'`])(~\/|\/)([^\s\x00-\x1f]*)/g;

const TRAILING_TRIM = new Set([".", ",", ";", ":", "!", "?", '"', "'"]);

function trimTrailing(text: string): string {
  let end = text.length;
  while (end > 0) {
    const ch = text[end - 1];
    if (TRAILING_TRIM.has(ch)) {
      end--;
      continue;
    }
    if (ch === ")" || ch === "]") {
      const open = ch === ")" ? "(" : "[";
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

  PATH_RE.lastIndex = 0;
  while ((m = PATH_RE.exec(line)) !== null) {
    // Group 1 is the consumed leading delimiter (empty at start-of-line);
    // drop it so start/text describe just the path itself.
    const lead = m[1];
    const raw = m[0].slice(lead.length);
    const trimmed = trimTrailing(raw);
    if (!trimmed) continue;
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
 * Decide whether a click on a detected link should open it. A plain click opens
 * the link (single-click to open); shift/alt clicks never open (reserved for
 * selection); and a click preceded by a mousedown that moved more than
 * DRAG_THRESHOLD_PX is treated as a text drag-select, not an activation. isMac
 * is currently unused but kept for signature stability / future per-OS tweaks.
 */
export function shouldActivateLink(
  e: Pick<MouseEvent, "shiftKey" | "altKey" | "clientX" | "clientY">,
  downPos: PointerPos | null,
  _isMac: boolean,
): boolean {
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
  /** cellY[i] = 0-based physical row index (0 = firstY) for text[i]. */
  cellY: number[];
  /** Number of physical rows this logical line spans. */
  rowCount: number;
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
        cellY.push(row);
      }
      text += chars;
    }
  }
  return { text, cellStart, cellY, rowCount };
}
