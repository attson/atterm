import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { ref } from "vue";
import { useQuickInputHotkeys } from "./useQuickInputHotkeys";
import type { QuickInputButton } from "../configStore";

describe("useQuickInputHotkeys", () => {
  let onFire: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    onFire = vi.fn();
  });

  afterEach(() => {
    document.removeEventListener("keydown", () => undefined, true);
  });

  it("fires when Alt+digit matches a button hotkey", () => {
    const buttons = ref<QuickInputButton[]>([
      { id: "a", label: "x", send: "x", appendNewline: true, hotkey: "Alt+1" },
    ]);
    useQuickInputHotkeys(buttons, onFire);
    const ev = new KeyboardEvent("keydown", { key: "1", code: "Digit1", altKey: true });
    document.dispatchEvent(ev);
    expect(onFire).toHaveBeenCalledWith(buttons.value[0]);
  });

  it("ignores non-Alt keypresses", () => {
    const buttons = ref<QuickInputButton[]>([
      { id: "a", label: "x", send: "x", appendNewline: true, hotkey: "Alt+1" },
    ]);
    useQuickInputHotkeys(buttons, onFire);
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "1", code: "Digit1" }));
    expect(onFire).not.toHaveBeenCalled();
  });
});
