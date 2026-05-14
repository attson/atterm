/**
 * commandFinish — pure helpers for OSC 133 command boundary tracking and
 * command-finished notification gating. Consumed by TerminalView via
 * xterm's parser.registerOscHandler(133, …).
 *
 * The xterm parser strips the leading "OSC 133;" and the terminator (BEL or
 * ST), so handlers see payloads like "A", "B", "C", "D", or "D;<exit>".
 */

export interface CommandEvent {
  kind: "finished";
  exitCode: number;
  elapsedMs: number;
}

type State =
  | { phase: "idle" }
  | { phase: "running"; startedAt: number };

export class CommandTracker {
  private state: State = { phase: "idle" };

  /**
   * Update tracker state from a single OSC 133 payload. Returns a
   * CommandEvent when the payload signals "command finished" and a prior C
   * was seen; otherwise returns null.
   */
  onOsc133(payload: string, nowMs: number): CommandEvent | null {
    const marker = payload.charAt(0);
    switch (marker) {
      case "A":
      case "B":
        return null;
      case "C":
        this.state = { phase: "running", startedAt: nowMs };
        return null;
      case "D": {
        if (this.state.phase !== "running") return null;
        const elapsedMs = Math.max(0, nowMs - this.state.startedAt);
        const exitCode = parseExitCode(payload);
        this.state = { phase: "idle" };
        return { kind: "finished", exitCode, elapsedMs };
      }
      default:
        return null;
    }
  }
}

function parseExitCode(payload: string): number {
  // payload examples: "D", "D;0", "D;127", "D;abc"
  const idx = payload.indexOf(";");
  if (idx === -1) return 0;
  const raw = payload.slice(idx + 1).trim();
  if (raw === "") return 0;
  const n = Number(raw);
  return Number.isFinite(n) && Number.isInteger(n) ? n : -1;
}

export interface NotifyGate {
  focused: boolean;
  thresholdSec: number;
  isLocal: boolean;
}

/**
 * Returns true when an unfocused, local-session command finish meets the
 * threshold. Threshold below 1s is clamped to 1s.
 */
export function shouldNotifyCommand(ev: CommandEvent, opts: NotifyGate): boolean {
  if (!opts.isLocal) return false;
  if (opts.focused) return false;
  const thresholdMs = Math.max(1, opts.thresholdSec) * 1000;
  return ev.elapsedMs >= thresholdMs;
}

/**
 * "12s" for sub-minute durations, "MmSs" otherwise. Always integer seconds.
 */
export function formatElapsed(ms: number): string {
  const totalSec = Math.floor(ms / 1000);
  if (totalSec < 60) return `${totalSec}s`;
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return `${m}m${s}s`;
}
