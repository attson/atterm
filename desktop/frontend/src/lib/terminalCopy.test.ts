import { describe, expect, it, vi } from "vitest";
import { copyTerminalSelection, copyTextToClipboard, isTerminalCopyShortcut } from "./terminalCopy";

function key(opts: KeyboardEventInit) {
  return new KeyboardEvent("keydown", {
    key: "c",
    code: "KeyC",
    bubbles: true,
    cancelable: true,
    ...opts,
  });
}

describe("terminal copy shortcuts", () => {
  it("uses Cmd+C for copy on macOS", () => {
    expect(isTerminalCopyShortcut(key({ metaKey: true }), "mac")).toBe(true);
  });

  it("uses Ctrl+Shift+C for copy off macOS", () => {
    expect(isTerminalCopyShortcut(key({ ctrlKey: true, shiftKey: true }), "other")).toBe(true);
  });

  it("keeps plain Ctrl+C available for terminal interrupt", () => {
    expect(isTerminalCopyShortcut(key({ ctrlKey: true }), "other")).toBe(false);
    expect(isTerminalCopyShortcut(key({ ctrlKey: true }), "mac")).toBe(false);
  });

  it("copies selected terminal text to the clipboard", async () => {
    const writeText = vi.fn(async (_text: string) => {});

    await expect(
      copyTerminalSelection({ getSelection: () => "selected output" }, { writeText }),
    ).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("selected output");
  });

  it("does not touch the clipboard when there is no selection", async () => {
    const writeText = vi.fn(async (_text: string) => {});

    await expect(
      copyTerminalSelection({ getSelection: () => "" }, { writeText }),
    ).resolves.toBe(false);
    expect(writeText).not.toHaveBeenCalled();
  });

  it("falls back when the Clipboard API exists but rejects the write", async () => {
    const denied = new DOMException("clipboard write denied", "NotAllowedError");
    const writeText = vi.fn().mockRejectedValue(denied);
    const fallback = vi.fn(() => true);

    await expect(copyTextToClipboard("selected output", { writeText }, fallback)).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("selected output");
    expect(fallback).toHaveBeenCalledWith("selected output");
  });

  it("preserves the Clipboard API error when the fallback also fails", async () => {
    const denied = new DOMException("clipboard write denied", "NotAllowedError");
    const fallback = vi.fn(() => false);

    await expect(
      copyTextToClipboard("selected output", { writeText: vi.fn().mockRejectedValue(denied) }, fallback),
    ).rejects.toBe(denied);
  });

  it("preserves the Clipboard API error when the fallback throws", async () => {
    const denied = new DOMException("clipboard write denied", "NotAllowedError");

    await expect(
      copyTextToClipboard(
        "selected output",
        { writeText: vi.fn().mockRejectedValue(denied) },
        () => { throw new TypeError("execCommand unavailable"); },
      ),
    ).rejects.toBe(denied);
  });
});
