import { describe, expect, it } from "vitest";
import {
  canSendSelection,
  clampContextMenuPosition,
  effectiveRemotePermission,
  imagePasteBlockedReason,
  isPasteAllowed,
} from "./terminalContextMenu";

describe("terminal context menu helpers", () => {
  it("defaults missing permission to full", () => {
    expect(effectiveRemotePermission(undefined)).toBe("full");
    expect(effectiveRemotePermission("")).toBe("full");
  });

  it("disables paste for detached or read-only sessions", () => {
    expect(isPasteAllowed("connecting", "full")).toBe(false);
    expect(isPasteAllowed("attached", "view")).toBe(false);
    expect(isPasteAllowed("attached", "control")).toBe(true);
  });

  it("requires full permission for image paste", () => {
    expect(imagePasteBlockedReason("control")).toMatch(/full remote permission/);
    expect(imagePasteBlockedReason("full")).toBeNull();
  });

  it("clamps the menu inside the viewport", () => {
    expect(clampContextMenuPosition(790, 590, 160, 80, 800, 600)).toEqual({
      left: 632,
      top: 512,
    });
  });

  it("allows send only when selection + writeable + driver", () => {
    expect(
      canSendSelection({ hasSelection: true, status: "attached", permission: "full", isDriver: true }),
    ).toBe(true);
  });

  it("blocks send with no selection", () => {
    expect(
      canSendSelection({ hasSelection: false, status: "attached", permission: "full", isDriver: true }),
    ).toBe(false);
  });

  it("blocks send for read-only or detached sessions", () => {
    expect(
      canSendSelection({ hasSelection: true, status: "attached", permission: "view", isDriver: true }),
    ).toBe(false);
    expect(
      canSendSelection({ hasSelection: true, status: "connecting", permission: "full", isDriver: true }),
    ).toBe(false);
  });

  it("blocks send for non-driver clients even when permission allows writes", () => {
    expect(
      canSendSelection({ hasSelection: true, status: "attached", permission: "control", isDriver: false }),
    ).toBe(false);
  });
});
