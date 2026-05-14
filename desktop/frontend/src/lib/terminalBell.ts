import type { SessionInfo } from "./connection";

const DEFAULT_THROTTLE_MS = 3000;

/**
 * Decides whether a BEL event should produce a system notification.
 *
 * - Returns false when the window is focused (user already sees the visual bell).
 * - Returns false when the last notification for this stream was within
 *   throttleMs (default 3000) — avoids notification storms from zsh
 *   autocomplete misfires or tools that bell repeatedly.
 */
export function shouldNotify(
  now: number,
  lastBellAt: number,
  focused: boolean,
  throttleMs: number = DEFAULT_THROTTLE_MS,
): boolean {
  if (focused) return false;
  if (now - lastBellAt < throttleMs) return false;
  return true;
}

/**
 * Produces a short human label for the notification body. Prefers the cwd
 * basename, falls back to the command basename, then "" if neither is set.
 */
export function extractSessionLabel(info: SessionInfo | null): string {
  if (!info) return "";
  const fromCwd = basename(info.cwd ?? "");
  if (fromCwd) return fromCwd;
  const fromCommand = basename(info.command ?? "");
  if (fromCommand) return fromCommand;
  return "";
}

function basename(p: string): string {
  if (!p) return "";
  const idx = p.lastIndexOf("/");
  return idx === -1 ? p : p.slice(idx + 1);
}
