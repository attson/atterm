// Focus-report coalescer.
//
// When a child TUI enables DEC focus reporting (DECSET 1004 — Claude Code and
// other full-screen apps do), xterm forwards a `\x1b[O` on blur and `\x1b[I`
// on focus through term.onData straight into the PTY. On macOS the WKWebView
// window intermittently resigns and immediately regains key status with no
// user action; the focused xterm textarea blurs and refocuses within a few
// milliseconds, so xterm emits a `\x1b[O` / `\x1b[I` pair. The leading ESC of
// that stray focus-out cancels the child's in-flight turn (see the
// ESC-LEAD pty-input diagnostic on the Go side), which surfaces as "the AI
// paused on its own with no operation".
//
// This coalescer sits at the onData boundary: it holds a focus-out for a short
// flap window and, if a focus-in arrives within it, drops both — the blur was
// spurious and the child never sees it. A blur that is NOT reversed within the
// window is a genuine focus loss and is reported as usual.

export const FOCUS_IN = "\x1b[I";
export const FOCUS_OUT = "\x1b[O";

// A blur reversed by a refocus within this window is treated as a spurious
// window-level flap. Real alt-tab away-and-back takes longer; reporting a
// genuine blur this much later is imperceptible for focus tracking.
export const FOCUS_FLAP_WINDOW_MS = 80;

export interface FocusReportCoalescerDeps {
  // Forward a chunk to the PTY.
  send: (data: string) => void;
  delayMs?: number;
  // Injectable for tests; default to the DOM timers.
  setTimer?: (fn: () => void, ms: number) => unknown;
  clearTimer?: (handle: unknown) => void;
}

export interface FocusReportCoalescer {
  // Call for every onData chunk (post C1-strip).
  handle: (data: string) => void;
  // Clear any held focus-out without sending it (call on unmount).
  dispose: () => void;
}

export function createFocusReportCoalescer(
  deps: FocusReportCoalescerDeps,
): FocusReportCoalescer {
  const delayMs = deps.delayMs ?? FOCUS_FLAP_WINDOW_MS;
  const setTimer =
    deps.setTimer ?? ((fn, ms) => setTimeout(fn, ms) as unknown);
  const clearTimer = deps.clearTimer ?? ((h) => clearTimeout(h as number));

  // Handle of the in-flight focus-out timer, or null when none is held.
  let pendingOut: unknown = null;

  function clearPending() {
    if (pendingOut !== null) {
      clearTimer(pendingOut);
      pendingOut = null;
    }
  }

  return {
    handle(data) {
      if (data === FOCUS_OUT) {
        // Hold it. A focus-in within the flap window cancels it; otherwise the
        // timer delivers it as a genuine blur. A second blur while one is
        // already held coalesces into the first.
        if (pendingOut !== null) return;
        pendingOut = setTimer(() => {
          pendingOut = null;
          deps.send(FOCUS_OUT);
        }, delayMs);
        return;
      }
      if (data === FOCUS_IN) {
        if (pendingOut !== null) {
          // Spurious flap: the blur was reversed before it was reported. Drop
          // both so the child TUI never sees a focus change.
          clearPending();
          return;
        }
        deps.send(FOCUS_IN);
        return;
      }
      // Any other input proves the terminal is focused, so a held blur was a
      // spurious flap — drop it rather than cancelling the child's turn.
      clearPending();
      deps.send(data);
    },
    dispose() {
      clearPending();
    },
  };
}
