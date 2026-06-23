import { describe, it, expect, beforeAll } from "vitest";
import { Terminal } from "xterm";
import { isBareModifierKeydown, installModifierScrollGuard } from "./terminalKeyGuard";

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
}

describe("installModifierScrollGuard (real xterm)", () => {
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
    installModifierScrollGuard(term);
    term.scrollToTop();
    fireKeydown(term, 229, "Control"); // IME-routed bare Ctrl
    expect(term.buffer.active.viewportY).toBe(0);
  });
});
