import type { Status } from "./connection";
import { t } from "../i18n";

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

export function imagePasteBlockedReason(permission?: string): string | null {
  const effective = effectiveRemotePermission(permission);
  if (effective === "full") return null;
  if (effective === "control") return t("terminal.imagePasteRequiresFull");
  return t("terminal.readOnlySession");
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
