import type { Status } from "./connection";
import { stripC1Controls } from "./stripC1Controls";

export type EffectiveRemotePermission = "view" | "control" | "full";

export function effectiveRemotePermission(permission?: string): EffectiveRemotePermission {
  switch (permission) {
    case "view":
    case "control":
    case "full":
      return permission;
    default:
      return "full";
  }
}

export function isPasteAllowed(status: Status, permission?: string): boolean {
  return status === "attached" && effectiveRemotePermission(permission) !== "view";
}

export interface CanSendSelectionInput {
  hasSelection: boolean;
  status: Status;
  permission?: string;
  isDriver: boolean;
}

export function canSendSelection(input: CanSendSelectionInput): boolean {
  if (!input.hasSelection) return false;
  if (!input.isDriver) return false;
  return isPasteAllowed(input.status, input.permission);
}

export function clampContextMenuPosition(
  x: number,
  y: number,
  width: number,
  height: number,
  viewportWidth = window.innerWidth,
  viewportHeight = window.innerHeight,
): { left: number; top: number } {
  const margin = 8;
  return {
    left: Math.min(Math.max(x, margin), Math.max(margin, viewportWidth - width - margin)),
    top: Math.min(Math.max(y, margin), Math.max(margin, viewportHeight - height - margin)),
  };
}

// prepareSendPayload shapes a raw xterm selection into bytes ready for
// SessionConnection.sendInput: strips C1 controls (same as typed input),
// normalizes every newline shape to \r (PTY input semantics), collapses
// any run of trailing \r down to one. Returns null if nothing meaningful
// remains — callers should treat null as "no-op".
export function prepareSendPayload(selection: string): string | null {
  const { cleaned } = stripC1Controls(selection);
  if (!cleaned) return null;
  const normalized = cleaned.replace(/\r\n?|\n/g, "\r").replace(/\r+$/, "");
  if (!normalized) return null;
  return normalized + "\r";
}
