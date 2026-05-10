import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { effectScope } from "vue";
import { useTerminalShortcuts } from "./useTerminalShortcuts";

function fireKey(opts: KeyboardEventInit & { key: string }) {
  const ev = new KeyboardEvent("keydown", { ...opts, bubbles: true, cancelable: true });
  document.dispatchEvent(ev);
  return ev;
}

describe("useTerminalShortcuts", () => {
  let scope: ReturnType<typeof effectScope>;
  const handlers = {
    onSplitVertical: vi.fn(),
    onSplitHorizontal: vi.fn(),
    onClosePane: vi.fn(),
    onFocusPane: vi.fn(),
    onNewTab: vi.fn(),
    onSwitchTab: vi.fn(),
  };

  beforeEach(() => {
    Object.values(handlers).forEach((h) => h.mockReset());
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control" }));
  });

  afterEach(() => {
    scope.stop();
  });

  it("Ctrl+D → onSplitVertical('new')", () => {
    fireKey({ key: "d", code: "KeyD", ctrlKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledWith("new");
  });

  it("Ctrl+Shift+D → onSplitHorizontal('new')", () => {
    fireKey({ key: "D", code: "KeyD", ctrlKey: true, shiftKey: true });
    expect(handlers.onSplitHorizontal).toHaveBeenCalledWith("new");
  });

  it("Ctrl+Alt+D → onSplitVertical('pick')", () => {
    fireKey({ key: "d", code: "KeyD", ctrlKey: true, altKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledWith("pick");
  });

  it("Ctrl+Alt+Shift+D → onSplitHorizontal('pick')", () => {
    fireKey({ key: "D", code: "KeyD", ctrlKey: true, altKey: true, shiftKey: true });
    expect(handlers.onSplitHorizontal).toHaveBeenCalledWith("pick");
  });

  it("macOS ⌘⌥D synthesizes key='∂' but code='KeyD' → still triggers pick", () => {
    fireKey({ key: "∂", code: "KeyD", ctrlKey: true, altKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledWith("pick");
  });

  it("Ctrl+W → onClosePane", () => {
    fireKey({ key: "w", code: "KeyW", ctrlKey: true });
    expect(handlers.onClosePane).toHaveBeenCalled();
  });

  it("Ctrl+T → onNewTab", () => {
    fireKey({ key: "t", code: "KeyT", ctrlKey: true });
    expect(handlers.onNewTab).toHaveBeenCalled();
  });

  it("Ctrl+Alt+ArrowLeft → onFocusPane('left')", () => {
    fireKey({ key: "ArrowLeft", code: "ArrowLeft", ctrlKey: true, altKey: true });
    expect(handlers.onFocusPane).toHaveBeenCalledWith("left");
  });

  it("Ctrl+Alt+ArrowRight → onFocusPane('right')", () => {
    fireKey({ key: "ArrowRight", code: "ArrowRight", ctrlKey: true, altKey: true });
    expect(handlers.onFocusPane).toHaveBeenCalledWith("right");
  });

  it("Ctrl+Shift+] (key='}') → onSwitchTab(+1)", () => {
    // Shift+] produces "}" on US layout — handler must use code, not key.
    fireKey({ key: "}", code: "BracketRight", ctrlKey: true, shiftKey: true });
    expect(handlers.onSwitchTab).toHaveBeenCalledWith(1);
  });

  it("Ctrl+Shift+[ (key='{') → onSwitchTab(-1)", () => {
    fireKey({ key: "{", code: "BracketLeft", ctrlKey: true, shiftKey: true });
    expect(handlers.onSwitchTab).toHaveBeenCalledWith(-1);
  });

  it("plain D (no modifier) → ignored", () => {
    fireKey({ key: "d", code: "KeyD" });
    expect(handlers.onSplitVertical).not.toHaveBeenCalled();
  });

  it("Ctrl+D preventDefault + stopPropagation", () => {
    const ev = fireKey({ key: "d", code: "KeyD", ctrlKey: true });
    expect(ev.defaultPrevented).toBe(true);
  });

  it("scope.stop unbinds the listener", () => {
    scope.stop();
    fireKey({ key: "d", code: "KeyD", ctrlKey: true });
    expect(handlers.onSplitVertical).not.toHaveBeenCalled();
  });
});
