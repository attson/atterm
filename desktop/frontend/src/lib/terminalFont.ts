// Font stack for the xterm terminal canvas. Single source of truth for
// TerminalView across desktop, web, and Capacitor.
//
// The chain is ASCII-mono families FIRST (Menlo / Consolas / Liberation Mono /
// DejaVu Sans Mono) so plain code/text keeps its true monospace appearance,
// then CJK families (PingFang SC / Microsoft YaHei / Noto Sans Mono CJK SC)
// so Chinese / Japanese / Korean characters render with predictable widths.
//
// Per-glyph fallback in WebKit/Chromium walks this list character-by-character:
// ASCII finds Menlo immediately, CJK falls past the Latin-only mono fonts and
// resolves to the first CJK family that has the glyph. Crucially the chain
// does NOT start with ui-monospace / system-ui / -apple-system — those declare
// universal Unicode coverage on iOS 26 (and silently fail to render CJK while
// blocking further fallback), which was the root cause of the garbled
// double-width terminal output users hit when CJK glyphs rendered at the wrong
// width and overlapped neighbor cells.
//
// Keep these names quoted-when-needed and in this exact order — both renderers
// (DOM + WebGL) read it through xterm's `fontFamily` option, which is then
// fed into a canvas2d context's `font` property.
export const TERMINAL_FONT_FAMILY =
  'Menlo, Monaco, Consolas, "Liberation Mono", "DejaVu Sans Mono", ' +
  '"PingFang SC", "Microsoft YaHei", "Noto Sans Mono CJK SC", monospace';

// User-selectable heads for the font chain. The user only ever chooses what
// goes in FRONT of TERMINAL_FONT_FAMILY — the CJK-aware tail above is always
// appended, so redline #13's fallback order cannot be bypassed by a setting.
// An id of "" means "no head": use the built-in chain as-is.
//
// Labels are font names, not translated strings: a font is called
// "JetBrains Mono" in every locale.
export const TERMINAL_FONT_PRESETS: readonly { id: string; label: string }[] = [
  { id: "", label: "System default" },
  { id: "SF Mono", label: "SF Mono" },
  { id: "JetBrains Mono", label: "JetBrains Mono" },
  { id: "Fira Code", label: "Fira Code" },
  { id: "Cascadia Code", label: "Cascadia Code" },
  { id: "Source Code Pro", label: "Source Code Pro" },
  { id: "IBM Plex Mono", label: "IBM Plex Mono" },
] as const;

// composeFontFamily prepends the user's chosen family to the built-in chain.
// A missing font simply falls through to the chain at per-glyph fallback time,
// so no availability probing is needed (and document.fonts.check reports false
// positives for monospace families on WebKit anyway).
export function composeFontFamily(head: string): string {
  const trimmed = head.trim();
  if (!trimmed) return TERMINAL_FONT_FAMILY;
  const quoted = trimmed.startsWith('"') ? trimmed : `"${trimmed}"`;
  return `${quoted}, ${TERMINAL_FONT_FAMILY}`;
}
