import { describe, it, expect, beforeAll } from "vitest";
import { Terminal } from "xterm";
import { isBareModifierKeydown, installTerminalKeyHandler, ime229Payload } from "./terminalKeyGuard";

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

function textareaOf(term: Terminal): HTMLTextAreaElement {
  return (term as unknown as { textarea: HTMLTextAreaElement }).textarea;
}

function fireKeydown(term: Terminal, keyCode: number, key: string) {
  const ev = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true });
  Object.defineProperty(ev, "keyCode", { get: () => keyCode });
  textareaOf(term).dispatchEvent(ev);
}

// Behavioral regression tests against the real xterm build.
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

  // The swallowed-keystroke bug: xterm 5.3 reads keyCode-229 characters by
  // diffing the hidden textarea's value in setTimeout(0) callbacks
  // (CompositionHelper._handleAnyTextareaChanges), and those timers race the
  // keyup handler that clears the textarea — fast sequences merge, duplicate,
  // or drop characters. First prove the racy path exists, then that the
  // takeover keeps it from being scheduled at all.
  it("without takeover, a 229 keydown schedules xterm's textarea diff, which emits data", async () => {
    const term = await makeScrolledTerm();
    const got: string[] = [];
    term.onData((d) => got.push(d));
    fireKeydown(term, 229, "c");
    textareaOf(term).value = "c"; // browser inserts the char after keydown
    await new Promise((r) => setTimeout(r, 20)); // let the diff timer fire
    expect(got).toEqual(["c"]);
  });

  it("with takeover, a 229 keydown never reaches xterm's diff path and emits nothing", async () => {
    const term = await makeScrolledTerm();
    installTerminalKeyHandler(term, { ime229Takeover: true });
    const got: string[] = [];
    term.onData((d) => got.push(d));
    fireKeydown(term, 229, "c");
    textareaOf(term).value = "c";
    await new Promise((r) => setTimeout(r, 20));
    expect(got).toEqual([]); // delivery is the input-event takeover's job now
  });

  it("fires onRegularKeydown only for keydowns xterm will process", async () => {
    const term = await makeScrolledTerm();
    const calls: string[] = [];
    installTerminalKeyHandler(term, {
      ime229Takeover: true,
      hooks: {
        onRegularKeydown: () => calls.push("regular"),
      },
    });
    fireKeydown(term, 229, "c"); // blocked: IME-routed, takeover owns it
    fireKeydown(term, 68, "d");
    fireKeydown(term, 229, "Control"); // blocked: IME-routed bare modifier
    fireKeydown(term, 17, "Control"); // blocked: plain bare modifier
    expect(calls).toEqual(["regular"]);
  });
});

describe("ime229Payload", () => {
  const ev = (over: Partial<Pick<InputEvent, "inputType" | "data" | "isComposing">>) => ({
    inputType: "insertText",
    data: "d",
    isComposing: false,
    ...over,
  });

  it("returns the event's own data for a non-composing insertText", () => {
    expect(ime229Payload(ev({}))).toBe("d");
  });

  it("returns null mid-composition (pinyin candidates are xterm's)", () => {
    expect(ime229Payload(ev({ isComposing: true }))).toBeNull();
  });

  it("maps deleteContentBackward to DEL", () => {
    expect(ime229Payload(ev({ inputType: "deleteContentBackward", data: null }))).toBe("\x7f");
  });

  it("maps insertLineBreak and insertParagraph to CR", () => {
    expect(ime229Payload(ev({ inputType: "insertLineBreak", data: null }))).toBe("\r");
    expect(ime229Payload(ev({ inputType: "insertParagraph", data: null }))).toBe("\r");
  });

  it("returns null for composition commits and unmapped inputTypes and empty data", () => {
    expect(ime229Payload(ev({ inputType: "insertCompositionText" }))).toBeNull();
    expect(ime229Payload(ev({ inputType: "insertFromComposition" }))).toBeNull();
    expect(ime229Payload(ev({ inputType: "insertFromPaste" }))).toBeNull();
    expect(ime229Payload(ev({ data: "" }))).toBeNull();
    expect(ime229Payload(ev({ data: null }))).toBeNull();
  });
});
