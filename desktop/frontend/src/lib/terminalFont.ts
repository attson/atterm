// Font stack for the xterm terminal canvas. Single source of truth for both
// the desktop TerminalView and mobile MobileTerminal.
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
