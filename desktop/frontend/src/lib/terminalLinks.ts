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

// Absolute path: starts at word boundary with '/' or '~/', body chars exclude
// whitespace + control. The lookbehind ensures we don't treat 12/24 as a path
// (digit before slash) and we only trigger at start-of-line, whitespace, or one
// of the common surrounding punctuation chars.
const PATH_RE = /(?:(?<=^)|(?<=[\s(){}\[\]<>"'`]))(~\/|\/)([^\s\x00-\x1f]*)/g;

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
    const raw = m[0];
    const trimmed = trimTrailing(raw);
    if (!trimmed) continue;
    const start = m.index;
    const end = start + trimmed.length;
    // Skip if this overlaps with any URL match already produced.
    if (out.some((u) => start < u.end && end > u.start)) continue;
    out.push({ start, end, text: trimmed, kind: "path" });
  }

  out.sort((a, b) => a.start - b.start);
  return out;
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
