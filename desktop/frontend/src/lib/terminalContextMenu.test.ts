import { describe, expect, it } from "vitest";
import {
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
});
