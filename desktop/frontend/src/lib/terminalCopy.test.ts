import { describe, expect, it, vi } from "vitest";
import { copyTerminalSelection, isTerminalCopyShortcut } from "./terminalCopy";

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
});
