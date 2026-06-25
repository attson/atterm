// Focus-report stripper.
//
// When a child TUI enables DEC focus reporting (DECSET 1004 — Claude Code and
// other full-screen apps do), xterm forwards a `\x1b[O` on blur and `\x1b[I`
// on focus through term.onData straight into the PTY. The leading ESC of a
// stray focus-out cancels the child's in-flight turn (see the ESC-LEAD
// pty-input diagnostic on the Go side), which surfaces as "the AI paused on
// its own with no operation".
//
// In a native terminal "focus" is a NSWindow.key transition, which only fires
// on Cmd+Tab away or window minimize. In atterm (Wails / WKWebView + xterm.js)
// "focus" is a DOM textarea blur, which fires on *every* intra-window focus
// move — clicking another pane, opening a dialog or dropdown, a macOS-level
// WKWebView key flap, even clicking the TabBar. Every one of those sent the
// leading-ESC focus-out into the PTY and cancelled the AI's turn.
//
// We tried gating on `document.hasFocus()` first (PR for #N) which fixed all
// intra-window cases but still let real Cmd+Tab away pause the focused pane's
// AI. atterm's primary workload is long-running AI sessions, so the trade-off
// "stop forwarding focus events at all" is the right one: vim users lose
// `FocusGained` / `FocusLost` autocmds (uncommon and easy to substitute with
// `BufLeave` / `CursorHold`), every AI / shell / general-CLI workload gains
// stability. So the coalescer is now a focus-report stripper: it drops both
// directions unconditionally and forwards everything else.
//
// `term.onData` only sees what xterm wants to send back into the PTY, so other
// `\x1b[` CSI sequences (cursor keys, mouse, paste, etc.) flow through
// untouched — the equality compare against the exact 3-byte focus reports is
// safe.

export const FOCUS_IN = "\x1b[I";
export const FOCUS_OUT = "\x1b[O";

export interface FocusReportCoalescerDeps {
  // Forward a chunk to the PTY. Receives every chunk that is NOT a focus
  // report.
  send: (data: string) => void;
}

export interface FocusReportCoalescer {
  // Call for every onData chunk (post C1-strip).
  handle: (data: string) => void;
  // No-op kept for call-site symmetry; previously cleared the held-focus-out
  // timer.
  dispose: () => void;
}

export function createFocusReportCoalescer(
  deps: FocusReportCoalescerDeps,
): FocusReportCoalescer {
  return {
    handle(data) {
      if (data === FOCUS_OUT || data === FOCUS_IN) return;
      deps.send(data);
    },
    dispose() {
      /* nothing to clean up */
    },
  };
}
