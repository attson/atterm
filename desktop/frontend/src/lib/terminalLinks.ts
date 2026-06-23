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

/**
 * 1-based, inclusive cell-column range for a detected link, in the shape
 * xterm's ILink.range expects. `cellStart` comes from mapBufferLineCells().
 */
export function linkCellRange(
  m: Pick<LinkMatch, "start" | "end">,
  cellStart: number[],
): { startX: number; endX: number } {
  return { startX: cellStart[m.start] + 1, endX: cellStart[m.end] };
}

/** True when a 0-based cell column falls within a detected link's cell span. */
export function cellInLink(
  col: number,
  m: Pick<LinkMatch, "start" | "end">,
  cellStart: number[],
): boolean {
  return col >= cellStart[m.start] && col < cellStart[m.end];
}

export function isModClickEvent(e: MouseEvent, isMac: boolean): boolean {
  if (e.shiftKey || e.altKey) return false;
  return isMac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey;
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
