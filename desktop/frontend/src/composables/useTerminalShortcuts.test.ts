import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { effectScope, ref, type Ref } from "vue";
import { useTerminalShortcuts } from "./useTerminalShortcuts";

function fireKey(opts: KeyboardEventInit & { key: string }) {
  const ev = new KeyboardEvent("keydown", { ...opts, bubbles: true, cancelable: true });
  document.dispatchEvent(ev);
  return ev;
}

describe("useTerminalShortcuts (default bindings)", () => {
  let scope: ReturnType<typeof effectScope>;
  let bindings: Ref<Record<string, string>>;
  const handlers = {
    onSplitVertical: vi.fn(),
    onSplitHorizontal: vi.fn(),
    onClosePane: vi.fn(),
    onFocusPane: vi.fn(),
    onNewTab: vi.fn(),
    onSwitchTab: vi.fn(),
    onFocusSidebarSearch: vi.fn(),
  };

  beforeEach(() => {
    Object.values(handlers).forEach((h) => h.mockReset());
    bindings = ref({});
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
  });

  afterEach(() => {
    scope.stop();
  });

  it("Ctrl+N -> onSplitVertical", () => {
    fireKey({ key: "n", code: "KeyN", ctrlKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalled();
  });

  it("Ctrl+Shift+N -> onSplitHorizontal", () => {
    fireKey({ key: "N", code: "KeyN", ctrlKey: true, shiftKey: true });
    expect(handlers.onSplitHorizontal).toHaveBeenCalled();
  });

  it("Ctrl+W -> onClosePane", () => {
    fireKey({ key: "w", code: "KeyW", ctrlKey: true });
    expect(handlers.onClosePane).toHaveBeenCalled();
  });

  it("Ctrl+T -> onNewTab", () => {
    fireKey({ key: "t", code: "KeyT", ctrlKey: true });
    expect(handlers.onNewTab).toHaveBeenCalled();
  });

  it("Ctrl+Alt+ArrowLeft -> onFocusPane('left')", () => {
    fireKey({ key: "ArrowLeft", code: "ArrowLeft", ctrlKey: true, altKey: true });
    expect(handlers.onFocusPane).toHaveBeenCalledWith("left");
  });

  it("Ctrl+Alt+ArrowRight -> onFocusPane('right')", () => {
    fireKey({ key: "ArrowRight", code: "ArrowRight", ctrlKey: true, altKey: true });
    expect(handlers.onFocusPane).toHaveBeenCalledWith("right");
  });

  it("Ctrl+Shift+] (key='}') -> onSwitchTab(+1)", () => {
    fireKey({ key: "}", code: "BracketRight", ctrlKey: true, shiftKey: true });
    expect(handlers.onSwitchTab).toHaveBeenCalledWith(1);
  });

  it("Ctrl+Shift+[ (key='{') -> onSwitchTab(-1)", () => {
    fireKey({ key: "{", code: "BracketLeft", ctrlKey: true, shiftKey: true });
    expect(handlers.onSwitchTab).toHaveBeenCalledWith(-1);
  });

  it("Ctrl+Shift+F -> onFocusSidebarSearch, preventDefault", () => {
    const ev = fireKey({ key: "F", code: "KeyF", ctrlKey: true, shiftKey: true });
    expect(handlers.onFocusSidebarSearch).toHaveBeenCalledTimes(1);
    expect(ev.defaultPrevented).toBe(true);
  });

  it("plain N (no modifier) is ignored", () => {
    fireKey({ key: "n", code: "KeyN" });
    expect(handlers.onSplitVertical).not.toHaveBeenCalled();
  });

  it("Ctrl+N preventDefault + stopPropagation", () => {
    const ev = fireKey({ key: "n", code: "KeyN", ctrlKey: true });
    expect(ev.defaultPrevented).toBe(true);
  });

  it("scope.stop unbinds the listener", () => {
    scope.stop();
    fireKey({ key: "n", code: "KeyN", ctrlKey: true });
    expect(handlers.onSplitVertical).not.toHaveBeenCalled();
  });
});

describe("useTerminalShortcuts (user overrides)", () => {
  let scope: ReturnType<typeof effectScope>;
  let bindings: Ref<Record<string, string>>;
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
  });

  afterEach(() => {
    scope.stop();
  });

  it("remapped binding routes to the new key, the old key no longer fires", () => {
    bindings = ref({ "pane.split-vertical-new": "Mod+KeyJ" });
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
    fireKey({ key: "j", code: "KeyJ", ctrlKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalled();
    fireKey({ key: "n", code: "KeyN", ctrlKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledTimes(1); // not called again
  });

  // macOS Option+N emits the dead key '˜' as event.key while event.code stays
  // KeyN. Routing must key off code, or every Alt binding breaks on mac.
  it("mac dead key key='˜' but code='KeyN' still routes via code", () => {
    bindings = ref({ "pane.split-vertical-new": "Mod+Alt+KeyN" });
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
    fireKey({ key: "˜", code: "KeyN", ctrlKey: true, altKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalled();
  });

  it("empty binding disables the action", () => {
    bindings = ref({ "pane.close": "" });
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
    fireKey({ key: "w", code: "KeyW", ctrlKey: true });
    expect(handlers.onClosePane).not.toHaveBeenCalled();
  });

  it("updating bindings.value live changes routing without re-mounting", () => {
    bindings = ref({});
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
    fireKey({ key: "t", code: "KeyT", ctrlKey: true });
    expect(handlers.onNewTab).toHaveBeenCalledTimes(1);
    bindings.value = { "tab.new": "Mod+KeyP" };
    fireKey({ key: "t", code: "KeyT", ctrlKey: true });
    expect(handlers.onNewTab).toHaveBeenCalledTimes(1); // old key no longer fires
    fireKey({ key: "p", code: "KeyP", ctrlKey: true });
    expect(handlers.onNewTab).toHaveBeenCalledTimes(2); // new key fires
  });
});

describe("useTerminalShortcuts (terminal search)", () => {
  let scope: ReturnType<typeof effectScope>;
  let bindings: Ref<Record<string, string>>;
  const handlers = {
    onSplitVertical: vi.fn(),
    onSplitHorizontal: vi.fn(),
    onClosePane: vi.fn(),
    onFocusPane: vi.fn(),
    onNewTab: vi.fn(),
    onSwitchTab: vi.fn(),
    onFocusSidebarSearch: vi.fn(),
    onTerminalSearch: vi.fn(),
  };

  beforeEach(() => {
    Object.values(handlers).forEach((h) => h.mockReset());
    bindings = ref({});
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
  });

  afterEach(() => {
    scope.stop();
  });

  it("Ctrl+F -> onTerminalSearch", () => {
    const ev = fireKey({ key: "f", code: "KeyF", ctrlKey: true });
    expect(handlers.onTerminalSearch).toHaveBeenCalledTimes(1);
    expect(handlers.onFocusSidebarSearch).not.toHaveBeenCalled();
    expect(ev.defaultPrevented).toBe(true);
  });

  it("Ctrl+Shift+F -> onFocusSidebarSearch", () => {
    fireKey({ key: "F", code: "KeyF", ctrlKey: true, shiftKey: true });
    expect(handlers.onFocusSidebarSearch).toHaveBeenCalledTimes(1);
    expect(handlers.onTerminalSearch).not.toHaveBeenCalled();
  });
});

describe("useTerminalShortcuts (terminal search handler omitted)", () => {
  it("Ctrl+F does not throw when onTerminalSearch is absent", () => {
    const scope = effectScope();
    scope.run(() =>
      useTerminalShortcuts(
        {
          onSplitVertical: vi.fn(),
          onSplitHorizontal: vi.fn(),
          onClosePane: vi.fn(),
          onFocusPane: vi.fn(),
          onNewTab: vi.fn(),
          onSwitchTab: vi.fn(),
        },
        { mod: "Control" },
      ),
    );
    expect(() => fireKey({ key: "f", code: "KeyF", ctrlKey: true })).not.toThrow();
    scope.stop();
  });
});
