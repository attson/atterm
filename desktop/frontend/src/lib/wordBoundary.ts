// wordBoundaryAt returns the {start, len} of the word at col within line.
//
// Word classes:
//   - alnum-underscore run: matches /[A-Za-z0-9_]/ plus any CJK / non-ASCII
//     letter-like codepoint (anything that is NOT whitespace AND NOT an
//     ASCII punctuation char). Single CJK characters count as a word — this
//     matches what iOS system long-press does on CJK text.
//   - punctuation run: a contiguous sequence of ASCII punctuation
//     (anything in `!"#$%&'()*+,-./:;<=>?@[\]^_\`{|}~` minus underscore).
//   - whitespace: yields len=0 (no word).
//
// col is a 0-based offset into the line's codepoints. If col is past the line
// length, returns { start: line.length, len: 0 }.
export function wordBoundaryAt(line: string, col: number): { start: number; len: number } {
  if (col >= line.length) return { start: line.length, len: 0 }
  const ch = line[col]
  if (isWhitespace(ch)) return { start: col, len: 0 }
  const isAlnum = isAlnumLike(ch)
  let start = col
  while (start > 0 && classOf(line[start - 1]) === (isAlnum ? 'alnum' : 'punct')) start--
  let end = col + 1
  while (end < line.length && classOf(line[end]) === (isAlnum ? 'alnum' : 'punct')) end++
  return { start, len: end - start }
}

function isWhitespace(ch: string): boolean {
  return /\s/.test(ch)
}

// ASCII punctuation set minus underscore (underscore joins alnum, like JS \w).
const ASCII_PUNCT = new Set(`!"#$%&'()*+,-./:;<=>?@[\\]^\`{|}~`.split(''))

function isAlnumLike(ch: string): boolean {
  if (isWhitespace(ch)) return false
  if (ASCII_PUNCT.has(ch)) return false
  return true
}

function classOf(ch: string): 'alnum' | 'punct' | 'ws' {
  if (isWhitespace(ch)) return 'ws'
  if (ASCII_PUNCT.has(ch)) return 'punct'
  return 'alnum'
}
