import { describe, it, expect, beforeAll } from "vitest";
import { Terminal } from "xterm";
import {
  desktopImeInputMatchesKeydown,
  desktopImeInputPayload,
  isBareModifierKeydown,
  installTerminalKeyHandler,
} from "./terminalKeyGuard";

beforeAll(() => {
  // jsdom lacks matchMedia; xterm's ScreenDprMonitor needs it on open().
  if (!window.matchMedia) {
    // @ts-expect-error minimal test stub
    window.matchMedia = () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    });
  }
});

describe("isBareModifierKeydown", () => {
  const bare = ["Control", "Meta", "Alt", "Shift"];
  for (const key of bare) {
    it(`is true for bare ${key} keydown`, () => {
      expect(isBareModifierKeydown(new KeyboardEvent("keydown", { key }))).toBe(true);
    });
  }

  it("is false for a character key", () => {
    expect(isBareModifierKeydown(new KeyboardEvent("keydown", { key: "a" }))).toBe(false);
  });

  it("is false for a modifier+key chord (e.g. Ctrl+C)", () => {
    expect(
      isBareModifierKeydown(new KeyboardEvent("keydown", { key: "c", ctrlKey: true })),
    ).toBe(false);
  });

  it("is false for keyup of a modifier", () => {
    expect(isBareModifierKeydown(new KeyboardEvent("keyup", { key: "Control" }))).toBe(false);
  });
});

// Behavioral regression test against the real xterm build: proves that an
// IME-routed (keyCode 229) modifier keydown no longer yanks the viewport to the
// bottom once the guard is installed.
async function makeScrolledTerm() {
  const el = document.createElement("div");
  document.body.appendChild(el);
  const term = new Terminal({ scrollback: 1000, scrollOnUserInput: true, rows: 10, cols: 40 });
  term.open(el);
  let payload = "";
  for (let i = 0; i < 200; i++) payload += "line " + i + "\r\n";
  await new Promise<void>((resolve) => term.write(payload, () => resolve()));
  return term;
}

function fireKeydown(term: Terminal, keyCode: number, key: string) {
  const ta = (term as unknown as { textarea: HTMLTextAreaElement }).textarea;
  const ev = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true });
  Object.defineProperty(ev, "keyCode", { get: () => keyCode });
  ta.dispatchEvent(ev);
  return ev;
}

describe("installTerminalKeyHandler (real xterm)", () => {
  it("reproduces the bug: a keyCode-229 keydown scrolls to bottom without the guard", async () => {
    const term = await makeScrolledTerm();
    const base = term.buffer.active.baseY;
    expect(base).toBeGreaterThan(0);
    term.scrollToTop();
    expect(term.buffer.active.viewportY).toBe(0);
    fireKeydown(term, 229, "Control");
    expect(term.buffer.active.viewportY).toBe(base); // jumped to bottom (the bug)
  });

  it("keeps the viewport put when a bare modifier is pressed under an IME", async () => {
    const term = await makeScrolledTerm();
    installTerminalKeyHandler(term, { ime229Takeover: false });
    term.scrollToTop();
    fireKeydown(term, 229, "Control"); // IME-routed bare Ctrl
    expect(term.buffer.active.viewportY).toBe(0);
  });

  it("blocks the racy xterm textarea-diff path when desktop takeover is enabled", async () => {
    const term = await makeScrolledTerm();
    installTerminalKeyHandler(term, { ime229Takeover: true });
    const got: string[] = [];
    term.onData((data) => got.push(data));
    fireKeydown(term, 229, "c");
    const textarea = (term as unknown as { textarea: HTMLTextAreaElement }).textarea;
    textarea.value = "c";
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(got).toEqual([]);
  });

  it("composes caller interception with guards in the same xterm handler", async () => {
    const term = await makeScrolledTerm();
    const accepted: string[] = [];
    installTerminalKeyHandler(term, {
      ime229Takeover: true,
      onKeydown: (event) => event.key !== "Tab",
      onAcceptedKeydown: (event) => accepted.push(event.key),
    });
    term.focus();
    const textarea = (term as unknown as { textarea: HTMLTextAreaElement }).textarea;
    expect(document.activeElement).toBe(textarea);
    const handled = fireKeydown(term, 9, "Tab");
    fireKeydown(term, 229, "c");
    fireKeydown(term, 68, "d");
    expect(accepted).toEqual(["d"]);
    expect(handled.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(textarea);
  });
});

describe("desktopImeInputPayload", () => {
  const input = (overrides: Partial<Pick<InputEvent, "inputType" | "data" | "isComposing">>) => ({
    inputType: "insertText",
    data: "d",
    isComposing: false,
    ...overrides,
  });

  it("maps direct text, backspace, and enter", () => {
    expect(desktopImeInputPayload(input({}))).toBe("d");
    expect(desktopImeInputPayload(input({ inputType: "deleteContentBackward", data: null }))).toBe("\x7f");
    expect(desktopImeInputPayload(input({ inputType: "insertLineBreak", data: null }))).toBe("\r");
    expect(desktopImeInputPayload(input({ inputType: "insertParagraph", data: null }))).toBe("\r");
  });

  it("leaves composition, paste, and empty text to their existing paths", () => {
    expect(desktopImeInputPayload(input({ isComposing: true }))).toBeNull();
    expect(desktopImeInputPayload(input({ inputType: "insertCompositionText" }))).toBeNull();
    expect(desktopImeInputPayload(input({ inputType: "insertFromComposition" }))).toBeNull();
    expect(desktopImeInputPayload(input({ inputType: "insertFromPaste" }))).toBeNull();
    expect(desktopImeInputPayload(input({ data: "" }))).toBeNull();
  });
});

describe("desktopImeInputMatchesKeydown", () => {
  it("matches the physical-keyboard echo for text and space", () => {
    expect(desktopImeInputMatchesKeydown("c", "insertText", "c")).toBe(true);
    expect(desktopImeInputMatchesKeydown(" ", "insertText", " ")).toBe(true);
    expect(desktopImeInputMatchesKeydown(" ", "insertText", "Spacebar")).toBe(true);
    expect(desktopImeInputMatchesKeydown("你", "insertText", "229")).toBe(false);
  });

  it("matches editing input types with their physical keys", () => {
    expect(desktopImeInputMatchesKeydown("\x7f", "deleteContentBackward", "Backspace")).toBe(true);
    expect(desktopImeInputMatchesKeydown("\r", "insertLineBreak", "Enter")).toBe(true);
    expect(desktopImeInputMatchesKeydown("x", "insertFromPaste", "x")).toBe(false);
  });
});
