// stripC1Controls removes C1 control codepoints (U+0080..U+009F) from a string
// produced by xterm.js's onData callback before it reaches the PTY.
//
// Why: with a macOS Chinese IME composing, WebKit can leak C1 bytes (e.g.
// U+0093 = Ctrl-S | 0x80, U+0089 = Tab | 0x80, U+008D = Enter | 0x80) into
// xterm.js's hidden textarea via the composition / InputEvent path. xterm.js
// then forwards them through onData → IN frame → host.Write, and zsh renders
// them as `<0093>` etc. Legitimate keyboard input never produces C1, so
// stripping at this boundary is a safe defense and fully recovers normal
// typing without waiting for the user to repeatedly press Enter.
export function stripC1Controls(s: string): { cleaned: string; dropped: number[] } {
  let cleaned = "";
  const dropped: number[] = [];
  for (let i = 0; i < s.length; i++) {
    const cp = s.charCodeAt(i);
    if (cp >= 0x80 && cp <= 0x9f) {
      dropped.push(cp);
    } else {
      cleaned += s[i];
    }
  }
  return { cleaned, dropped };
}
