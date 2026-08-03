// AuxKey is one button in the browser/Capacitor terminal's control-key bar. Unlike a
// QuickTemplate (label + text + trailing CR + preview dialog), an aux key
// sends its raw `seq` bytes verbatim on tap — it models a control keypress
// (esc, ctrl-c, arrows), not a line of input.
export interface AuxKey {
  id: string
  label: string
  seq: string
}

// DEFAULT_AUX_KEYS mirrors the keys the old mobile terminal hardcoded.
// Stable string ids (not UUIDs) so re-seeding after a reset is churn-free.
export const DEFAULT_AUX_KEYS: AuxKey[] = [
  { id: 'aux-up', label: '↑', seq: '\x1b[A' },
  { id: 'aux-down', label: '↓', seq: '\x1b[B' },
  { id: 'aux-left', label: '←', seq: '\x1b[D' },
  { id: 'aux-right', label: '→', seq: '\x1b[C' },
  { id: 'aux-enter', label: 'enter', seq: '\r' },
  { id: 'aux-esc', label: 'esc', seq: '\x1b' },
  { id: 'aux-tab', label: 'tab', seq: '\t' },
  { id: 'aux-ctrl-c', label: '⌃C', seq: '\x03' },
  { id: 'aux-ctrl-d', label: '⌃D', seq: '\x04' },
]

// effectiveAuxKeys returns the persisted list if non-empty, else the
// defaults — same seed-on-empty contract as effectiveTemplates. Storage
// stays empty until the user edits, so "reset to defaults" is just clear().
export async function effectiveAuxKeys(
  bridge: { load: () => Promise<AuxKey[]> },
): Promise<AuxKey[]> {
  const stored = await bridge.load()
  return stored.length > 0 ? stored : DEFAULT_AUX_KEYS
}

// parseSeq decodes the escape notation the editor accepts into the real
// bytes stored in AuxKey.seq:
//   \r \n \t  → CR / LF / TAB
//   \e        → ESC (0x1b)
//   \xNN      → the byte NN (two hex digits)
//   \\        → a literal backslash
//   ^X        → control char (X & 0x1f); ^@ ^A..^Z ^[ ^\ ^] ^^ ^_
// Anything else is taken literally so typing plain text still works.
export function parseSeq(input: string): string {
  let out = ''
  for (let i = 0; i < input.length; i++) {
    const c = input[i]
    if (c === '\\' && i + 1 < input.length) {
      const n = input[i + 1]
      if (n === 'r') { out += '\r'; i++; continue }
      if (n === 'n') { out += '\n'; i++; continue }
      if (n === 't') { out += '\t'; i++; continue }
      if (n === 'e') { out += '\x1b'; i++; continue }
      if (n === '\\') { out += '\\'; i++; continue }
      if (n === 'x' && i + 3 < input.length) {
        const hex = input.slice(i + 2, i + 4)
        if (/^[0-9a-fA-F]{2}$/.test(hex)) {
          out += String.fromCharCode(parseInt(hex, 16))
          i += 3
          continue
        }
      }
      out += c
      continue
    }
    if (c === '^' && i + 1 < input.length) {
      const code = input[i + 1].toUpperCase().charCodeAt(0)
      if (code >= 64 && code <= 95) {
        out += String.fromCharCode(code & 0x1f)
        i++
        continue
      }
    }
    out += c
  }
  return out
}

