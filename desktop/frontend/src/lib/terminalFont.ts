// Font stack for the xterm terminal canvas. Single source of truth for
// TerminalView across desktop, web, and Capacitor.
//
// AT Term Mono is a bundled Latin subset of JetBrains Mono. Keeping a real,
// always-present monospace face first is important on Linux: WebKitGTK and
// fontconfig may resolve a missing named family (for example Menlo) to the
// proportional Noto Sans instead of advancing to the next CSS fallback.
// xterm then measures the wide "W" as the cell width and renders narrow glyphs
// into that grid, which looks like large letter spacing.
//
// Per-glyph fallback in WebKit/Chromium walks this list character-by-character:
// ASCII resolves to the bundled face, while CJK falls through to the native
// CJK families. Crucially the chain does NOT put ui-monospace / system-ui /
// -apple-system before those CJK faces — they declare universal Unicode
// coverage on iOS 26 but fail to render it, blocking later fallback and
// producing missing or misaligned double-width glyphs.
//
// Keep these names quoted-when-needed and in this exact order — both renderers
// (DOM + WebGL) read it through xterm's `fontFamily` option, which is then
// fed into a canvas2d context's `font` property.
export const BUNDLED_TERMINAL_FONT_FAMILY = "AT Term Mono";

export const TERMINAL_FONT_FAMILY =
  `"${BUNDLED_TERMINAL_FONT_FAMILY}", ` +
  '"PingFang SC", "Microsoft YaHei", "Noto Sans Mono CJK SC", monospace';

// User-selectable heads for the font chain. The user only ever chooses what
// goes in FRONT of TERMINAL_FONT_FAMILY — the CJK-aware tail above is always
// appended, so redline #13's fallback order cannot be bypassed by a setting.
// An id of "" means "no head": use the built-in chain as-is.
//
// Labels are font names, not translated strings: a font is called
// "JetBrains Mono" in every locale.
export const TERMINAL_FONT_PRESETS: readonly { id: string; label: string }[] = [
  { id: "", label: "AT Term Mono (built-in)" },
  { id: "SF Mono", label: "SF Mono" },
  { id: "JetBrains Mono", label: "JetBrains Mono" },
  { id: "Fira Code", label: "Fira Code" },
  { id: "Cascadia Code", label: "Cascadia Code" },
  { id: "Source Code Pro", label: "Source Code Pro" },
  { id: "IBM Plex Mono", label: "IBM Plex Mono" },
] as const;

// composeFontFamily prepends the user's chosen family to the built-in chain.
// resolveTerminalFontFamily below verifies the rendered metrics because some
// Linux fontconfig paths substitute a missing family instead of falling through.
export function composeFontFamily(head: string): string {
  const trimmed = head.trim();
  if (!trimmed) return TERMINAL_FONT_FAMILY;
  const quoted = trimmed.startsWith('"') ? trimmed : `"${trimmed}"`;
  return `${quoted}, ${TERMINAL_FONT_FAMILY}`;
}

export interface TerminalFontRuntime {
  load(font: string): Promise<unknown>;
  measure(font: string, text: string): number | null;
}

const browserRuntime: TerminalFontRuntime = {
  async load(font) {
    if (typeof document === "undefined" || !document.fonts) return;
    await document.fonts.load(font);
  },
  measure(font, text) {
    if (typeof document === "undefined") return null;
    try {
      const canvas = document.createElement("canvas");
      const context = canvas.getContext("2d");
      if (!context) return null;
      context.font = font;
      return context.measureText(text).width;
    } catch {
      return null;
    }
  },
};

let browserFontLoad: Promise<void> | null = null;

function widthsMatch(a: number | null, b: number | null): boolean {
  if (a === null || b === null || !Number.isFinite(a) || !Number.isFinite(b)) return false;
  return Math.abs(a - b) <= Math.max(a, b) * 0.01;
}

function isMeasuredMonospace(fontFamily: string, runtime: TerminalFontRuntime): boolean {
  try {
    for (const weight of [400, 700]) {
      const font = `${weight} 16px ${fontFamily}`;
      if (!widthsMatch(runtime.measure(font, "iiiiiiiiii"), runtime.measure(font, "WWWWWWWWWW"))) {
        return false;
      }
    }
    return true;
  } catch {
    return false;
  }
}

function loadBundledFont(runtime: TerminalFontRuntime): Promise<void> {
  const load = async () => {
    await Promise.allSettled([
      runtime.load(`400 13px "${BUNDLED_TERMINAL_FONT_FAMILY}"`),
      runtime.load(`700 13px "${BUNDLED_TERMINAL_FONT_FAMILY}"`),
    ]);
  };
  if (runtime !== browserRuntime) return load();
  browserFontLoad ??= load();
  return browserFontLoad;
}

// Resolve the actual xterm stack only after the bundled face is ready. A
// selected local family is retained when the browser proves that both its
// regular and bold faces are monospace. This measures the rendered result
// instead of using document.fonts.check(), which reports false positives for
// fontconfig substitutions in WebKitGTK.
export async function resolveTerminalFontFamily(
  head: string,
  runtime: TerminalFontRuntime = browserRuntime,
): Promise<string> {
  await loadBundledFont(runtime);

  const candidate = composeFontFamily(head);
  if (!head.trim()) return candidate;
  return isMeasuredMonospace(candidate, runtime) ? candidate : TERMINAL_FONT_FAMILY;
}
