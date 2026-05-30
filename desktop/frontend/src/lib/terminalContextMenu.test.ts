import { describe, expect, it } from "vitest";
import {
  canSendSelection,
  clampContextMenuPosition,
  effectiveRemotePermission,
  imagePasteBlockedReason,
  isPasteAllowed,
  prepareSendPayload,
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

  it("appends a single CR to a one-line selection", () => {
    expect(prepareSendPayload("ls -la")).toBe("ls -la\r");
  });

  it("converts internal LF and CRLF newlines to CR", () => {
    expect(prepareSendPayload("a\nb\r\nc")).toBe("a\rb\rc\r");
  });

  it("collapses trailing newlines to a single CR", () => {
    expect(prepareSendPayload("ls -la\n")).toBe("ls -la\r");
    expect(prepareSendPayload("ls -la\r\n\n")).toBe("ls -la\r");
  });

  it("strips C1 controls before normalizing", () => {
    // U+0093 = Ctrl-S | 0x80 — see stripC1Controls.ts.
    expect(prepareSendPayload("ls -la")).toBe("ls -la\r");
  });

  it("returns null for empty or whitespace-only-after-strip input", () => {
    expect(prepareSendPayload("")).toBeNull();
    expect(prepareSendPayload("\n\n")).toBeNull();
    expect(prepareSendPayload("")).toBeNull();
  });
});
